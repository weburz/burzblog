// Package s3 is a small client for S3-compatible object storage (AWS S3,
// Cloudflare R2, Backblaze B2, Hetzner, MinIO and others): put, get, check,
// list and delete objects in one bucket, signed with AWS Signature Version 4.
// It covers what snapshots need and nothing more, on the standard library.
package s3

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// EmptySHA256 is the SHA-256 of an empty body, for requests without one.
const EmptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// ErrNotFound means the object does not exist.
var ErrNotFound = errors.New("no such object")

// Error is a refusal from the storage service.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("storage answered %d", e.Status)
	if e.Code != "" {
		msg += " " + e.Code
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	switch e.Code {
	case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
		msg += " (check the access key, its secret and its permissions on the bucket)"
	case "NoSuchBucket":
		msg += " (check the bucket name and the endpoint)"
	case "AuthorizationHeaderMalformed":
		msg += " (check the region)"
	}
	return msg
}

// Config says where the bucket is and how to sign in.
type Config struct {
	// Endpoint is the service's https address, such as
	// https://<account>.r2.cloudflarestorage.com or
	// https://s3.eu-central-1.amazonaws.com. Plain http is accepted only for a
	// loopback address (a local MinIO).
	Endpoint string
	// Region is the signing region; "us-east-1" suits R2 and MinIO too.
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	// VirtualHosted puts the bucket in the host name (bucket.endpoint)
	// instead of the path (endpoint/bucket).
	VirtualHosted bool
}

// Client talks to one bucket.
type Client struct {
	cfg  Config
	base *url.URL
	http *http.Client
	now  func() time.Time
	// retryWait is the first pause before retrying a failed request.
	retryWait time.Duration
}

var bucketRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// New checks cfg and returns a client.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return nil, fmt.Errorf("the storage endpoint must be an address like https://s3.example.com, got %q", cfg.Endpoint)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("the storage endpoint must use https (plain http only for a local test server), got %q", cfg.Endpoint)
		}
	default:
		return nil, fmt.Errorf("the storage endpoint must use https, got %q", cfg.Endpoint)
	}
	u.Path = ""
	if !bucketRE.MatchString(cfg.Bucket) {
		return nil, fmt.Errorf("%q is not a bucket name (3 to 63 lowercase letters, digits, dots and hyphens)", cfg.Bucket)
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("the storage access key ID and secret access key are both needed")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 2 * time.Minute
	return &Client{
		cfg: cfg, base: u, now: time.Now, retryWait: time.Second,
		// No overall timeout: a large upload may take long. Requests end
		// with their context.
		http: &http.Client{Transport: transport},
	}, nil
}

// String names the bucket without credentials, for logs and messages.
func (c *Client) String() string {
	return "s3://" + c.cfg.Bucket
}

// objectURL is the address of key, or of the bucket when key is "".
func (c *Client) objectURL(key string, query url.Values) *url.URL {
	u := *c.base
	path := "/"
	if c.cfg.VirtualHosted {
		u.Host = c.cfg.Bucket + "." + u.Host
		path += key
	} else {
		path += c.cfg.Bucket + "/" + key
		if key == "" {
			path = "/" + c.cfg.Bucket
		}
	}
	u.Path = path
	u.RawPath = escapePath(path)
	u.RawQuery = canonicalQuery(query)
	return &u
}

// Put stores body (size bytes, whose hex SHA-256 is sum) as key. Each attempt
// reads body afresh from the start, so a retry never shares a reader with a
// request still being sent.
func (c *Client) Put(ctx context.Context, key string, body io.ReaderAt, size int64, sum string) error {
	resp, err := c.do(ctx, http.MethodPut, key, nil, body, size, sum)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Get opens key for reading. It returns ErrNotFound when there is no such
// object. The caller closes the reader.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := c.do(ctx, http.MethodGet, key, nil, nil, 0, EmptySHA256)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// Exists reports whether key exists.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	resp, err := c.do(ctx, http.MethodHead, key, nil, nil, 0, EmptySHA256)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	return true, nil
}

// Delete removes key. Removing a key that does not exist is not an error.
func (c *Client) Delete(ctx context.Context, key string) error {
	resp, err := c.do(ctx, http.MethodDelete, key, nil, nil, 0, EmptySHA256)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// MakeBucket creates the bucket. A bucket the same account already owns is
// not an error. Tests use it; on a real service the bucket is usually made
// in its console, with a key limited to it.
func (c *Client) MakeBucket(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPut, "", nil, nil, 0, EmptySHA256)
	var se *Error
	if errors.As(err, &se) && se.Code == "BucketAlreadyOwnedByYou" {
		return nil
	}
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Object is one entry of a listing.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

type listResult struct {
	Contents []struct {
		Key          string `xml:"Key"`
		Size         int64  `xml:"Size"`
		LastModified string `xml:"LastModified"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// List returns every object whose key starts with prefix, in key order.
func (c *Client) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		resp, err := c.do(ctx, http.MethodGet, "", q, nil, 0, EmptySHA256)
		if err != nil {
			return nil, err
		}
		var page listResult
		err = xml.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read the bucket listing: %w", err)
		}
		for _, o := range page.Contents {
			t, _ := time.Parse(time.RFC3339, o.LastModified)
			out = append(out, Object{Key: o.Key, Size: o.Size, LastModified: t})
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			break
		}
		token = page.NextContinuationToken
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// do sends a signed request, retrying network errors and 5xx answers a few
// times: every operation here is idempotent. A 2xx answer is returned open.
func (c *Client) do(ctx context.Context, method, key string, query url.Values, body io.ReaderAt, size int64, sum string) (*http.Response, error) {
	wait := c.retryWait
	for attempt := 1; ; attempt++ {
		resp, err := c.once(ctx, method, key, query, body, size, sum)
		retry := false
		var se *Error
		switch {
		case err == nil:
			return resp, nil
		case errors.As(err, &se):
			retry = se.Status >= 500 || se.Status == http.StatusTooManyRequests
		case errors.Is(err, ErrNotFound), ctx.Err() != nil:
		default:
			retry = true // the network
		}
		if !retry || attempt == 4 {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait *= 4
	}
}

func (c *Client) once(ctx context.Context, method, key string, query url.Values, body io.ReaderAt, size int64, sum string) (*http.Response, error) {
	u := c.objectURL(key, query)
	var rd io.Reader
	if body != nil {
		rd = io.NewSectionReader(body, 0, size)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(io.NewSectionReader(body, 0, size)), nil
		}
	}
	req.Header.Set("x-amz-content-sha256", sum)
	sign(req, c.cfg.Region, c.cfg.AccessKeyID, c.cfg.SecretAccessKey, c.now())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", c.base.Host, err)
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	defer resp.Body.Close()
	e := &Error{Status: resp.StatusCode}
	var reason struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if xml.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reason) == nil {
		e.Code, e.Message = reason.Code, reason.Message
	}
	// A missing bucket is a setup problem, not a missing object.
	if resp.StatusCode == http.StatusNotFound && key != "" && e.Code != "NoSuchBucket" {
		return nil, ErrNotFound
	}
	return nil, e
}

// sign adds the Signature Version 4 headers to req, whose
// x-amz-content-sha256 header must already be set.
func sign(req *http.Request, region, accessKey, secret string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)

	// Every header that is set is signed; host comes from the URL.
	headers := map[string]string{"host": req.URL.Host}
	for name, values := range req.Header {
		headers[strings.ToLower(name)] = strings.Join(values, ",")
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, name := range names {
		canonHeaders.WriteString(name + ":" + strings.Join(strings.Fields(headers[name]), " ") + "\n")
	}
	signed := strings.Join(names, ";")
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{
		req.Method, path, req.URL.RawQuery, canonHeaders.String(), signed, req.Header.Get("x-amz-content-sha256"),
	}, "\n")

	scope := day + "/" + region + "/s3/aws4_request"
	hashed := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(hashed[:])
	key := hmacSHA256([]byte("AWS4"+secret), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
		",SignedHeaders="+signed+",Signature="+signature)
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// escape percent-encodes everything but the unreserved characters, as
// Signature Version 4 requires; keepSlash leaves "/" for paths.
func escape(s string, keepSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case 'A' <= ch && ch <= 'Z', 'a' <= ch && ch <= 'z', '0' <= ch && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == '~', ch == '/' && keepSlash:
			b.WriteByte(ch)
		default:
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(ch)|0x100, 16)[1:]))
		}
	}
	return b.String()
}

func escapePath(p string) string { return escape(p, true) }

// canonicalQuery encodes q sorted by key, which is both the wire form and
// the canonical form Signature Version 4 signs.
func canonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string{}, q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, escape(k, false)+"="+escape(v, false))
		}
	}
	return strings.Join(parts, "&")
}
