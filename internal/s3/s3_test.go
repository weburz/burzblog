package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The examples from AWS's Signature Version 4 documentation for S3
// ("Authenticating requests: Using the Authorization header"), with its
// example credentials.
const (
	exampleKey    = "AKIAIOSFODNN7EXAMPLE"
	exampleSecret = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
)

var exampleTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

func exampleRequest(t *testing.T, method, path, rawPath, query string, headers map[string]string) *http.Request {
	t.Helper()
	u := &url.URL{Scheme: "https", Host: "examplebucket.s3.amazonaws.com", Path: path, RawPath: rawPath, RawQuery: query}
	req, err := http.NewRequest(method, u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	sign(req, "us-east-1", exampleKey, exampleSecret, exampleTime)
	return req
}

func TestSignMatchesAWSExamples(t *testing.T) {
	cases := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			"GET object with a range",
			exampleRequest(t, "GET", "/test.txt", "", "", map[string]string{
				"Range": "bytes=0-9", "x-amz-content-sha256": EmptySHA256,
			}),
			"AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
				"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date," +
				"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41",
		},
		{
			"PUT object",
			exampleRequest(t, "PUT", "/test$file.text", "/test%24file.text", "", map[string]string{
				"Date":                 "Fri, 24 May 2013 00:00:00 GMT",
				"x-amz-storage-class":  "REDUCED_REDUNDANCY",
				"x-amz-content-sha256": "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072",
			}),
			"AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
				"SignedHeaders=date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class," +
				"Signature=98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd",
		},
		{
			"GET bucket listing",
			exampleRequest(t, "GET", "/", "", canonicalQuery(url.Values{"max-keys": {"2"}, "prefix": {"J"}}), map[string]string{
				"x-amz-content-sha256": EmptySHA256,
			}),
			"AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
				"SignedHeaders=host;x-amz-content-sha256;x-amz-date," +
				"Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7",
		},
	}
	for _, c := range cases {
		if got := c.req.Header.Get("Authorization"); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func TestEscape(t *testing.T) {
	for in, want := range map[string]string{
		"/bucket/snapshots/a b+c.tar.gz": "/bucket/snapshots/a%20b%2Bc.tar.gz",
		"/b/ünï~_-.":                     "/b/%C3%BCn%C3%AF~_-.",
	} {
		if got := escapePath(in); got != want {
			t.Errorf("escapePath(%q) = %q, want %q", in, got, want)
		}
	}
	if got := canonicalQuery(url.Values{"prefix": {"blog/media/"}, "list-type": {"2"}}); got != "list-type=2&prefix=blog%2Fmedia%2F" {
		t.Errorf("query = %q", got)
	}
}

func TestNewValidates(t *testing.T) {
	ok := Config{Endpoint: "https://s3.example.com", Bucket: "my-blog", AccessKeyID: "k", SecretAccessKey: "s"}
	if _, err := New(ok); err != nil {
		t.Errorf("valid config: %v", err)
	}
	for name, change := range map[string]func(*Config){
		"http remote":      func(c *Config) { c.Endpoint = "http://s3.example.com" },
		"path in endpoint": func(c *Config) { c.Endpoint = "https://s3.example.com/my-blog" },
		"credentials":      func(c *Config) { c.Endpoint = "https://key:secret@s3.example.com" },
		"no scheme":        func(c *Config) { c.Endpoint = "s3.example.com" },
		"bucket case":      func(c *Config) { c.Bucket = "My_Blog" },
		"no secret":        func(c *Config) { c.SecretAccessKey = "" },
	} {
		c := ok
		change(&c)
		if _, err := New(c); err == nil {
			t.Errorf("%s: accepted %+v", name, c)
		}
	}
	local := ok
	local.Endpoint = "http://127.0.0.1:9000"
	if _, err := New(local); err != nil {
		t.Errorf("local http: %v", err)
	}
}

func TestErrorsNeverHoldTheSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<Error><Code>SignatureDoesNotMatch</Code><Message>nope</Message></Error>`)
	}))
	defer srv.Close()
	c := testClient(t, srv.URL)
	err := c.Put(context.Background(), "a", strings.NewReader("x"), 1, EmptySHA256)
	var se *Error
	if !errors.As(err, &se) || se.Code != "SignatureDoesNotMatch" || !strings.Contains(err.Error(), "check the access key") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Error("the secret key is in the error")
	}
}

func TestRetriesServerErrorsButNotRefusals(t *testing.T) {
	var calls atomic.Int32
	status := http.StatusServiceUnavailable
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(status)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "payload" {
			t.Errorf("retried body = %q", body)
		}
	}))
	defer srv.Close()
	c := testClient(t, srv.URL)
	if err := c.Put(context.Background(), "a", strings.NewReader("payload"), 7, EmptySHA256); err != nil || calls.Load() != 3 {
		t.Errorf("put after two 503s: %v, %d calls", err, calls.Load())
	}
	calls.Store(0)
	status = http.StatusForbidden
	if err := c.Put(context.Background(), "a", strings.NewReader("payload"), 7, EmptySHA256); err == nil || calls.Load() != 1 {
		t.Errorf("403 was retried: %v, %d calls", err, calls.Load())
	}
}

func TestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := testClient(t, srv.URL)
	if _, err := c.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get: %v", err)
	}
	if ok, err := c.Exists(context.Background(), "missing"); ok || err != nil {
		t.Errorf("exists: %v %v", ok, err)
	}
	if err := c.Delete(context.Background(), "missing"); err != nil {
		t.Errorf("delete: %v", err)
	}
}

func TestListFollowsPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/my-blog" || r.URL.Query().Get("list-type") != "2" || r.URL.Query().Get("prefix") != "p/" {
			t.Errorf("listing request %s", r.URL)
		}
		if r.URL.Query().Get("continuation-token") == "" {
			fmt.Fprint(w, `<ListBucketResult><Contents><Key>p/b</Key><Size>2</Size><LastModified>2026-10-28T03:17:00.000Z</LastModified></Contents><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken></ListBucketResult>`)
			return
		}
		fmt.Fprint(w, `<ListBucketResult><Contents><Key>p/a</Key><Size>1</Size></Contents><IsTruncated>false</IsTruncated></ListBucketResult>`)
	}))
	defer srv.Close()
	list, err := testClient(t, srv.URL).List(context.Background(), "p/")
	if err != nil || len(list) != 2 || list[0].Key != "p/a" || list[1].Size != 2 || list[1].LastModified.IsZero() {
		t.Errorf("list = %+v, %v", list, err)
	}
}

func testClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	c, err := New(Config{Endpoint: endpoint, Bucket: "my-blog", AccessKeyID: "k", SecretAccessKey: "topsecret"})
	if err != nil {
		t.Fatal(err)
	}
	c.retryWait = time.Millisecond
	return c
}
