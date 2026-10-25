// Package client is a typed client for the BurzBlog API, used by the CLI.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one BurzBlog server.
type Client struct {
	Server string
	Token  string
	HTTP   *http.Client
	// UserAgent identifies the CLI version in server logs.
	UserAgent string
}

// New returns a client for server with token.
func New(server, token, userAgent string) *Client {
	return &Client{
		Server:    strings.TrimRight(server, "/"),
		Token:     token,
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		UserAgent: userAgent,
	}
}

// Error is an API error response.
type Error struct {
	Status  int
	Code    string
	Message string
	Detail  string
	// Current is the server's version of the post on a conflict, if sent.
	Current *Post
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Message + " (" + e.Detail + ")"
	}
	return e.Message
}

// IsCode reports whether err is an API error with code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// Post is a post as the API returns it.
type Post struct {
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Body        string     `json:"body,omitempty"`
	PublishedOn *time.Time `json:"publishedOn"`
	PublishAt   *time.Time `json:"publishAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	UpdatedBy   string     `json:"updatedBy"`
	Version     int64      `json:"version"`
	Words       int        `json:"words"`
}

// List is a page of posts with counts per status.
type List struct {
	Items  []Post         `json:"items"`
	Counts map[string]int `json:"counts"`
}

// Token is a newly issued API token.
type Token struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Scope    string `json:"scope"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.Server+path, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	// Never follow a redirect: it could carry the password or token to
	// plain http or to another host. Copy so a caller's own client is covered.
	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", c.Server, err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		defer resp.Body.Close()
		return nil, redirectError(resp, path)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, readError(resp)
	}
	return resp, nil
}

// redirectError explains a 3xx answer. It names the new address but never
// follows it, and never includes the token.
func redirectError(resp *http.Response, path string) error {
	loc, err := resp.Location()
	if err != nil {
		return &Error{Status: resp.StatusCode, Code: "redirect", Message: fmt.Sprintf("The server at %s answered with a redirect without saying where. Check the address you gave to `burzblog login`.", resp.Request.URL.Host)}
	}
	loc.User, loc.RawQuery, loc.Fragment = nil, "", ""
	loc.Path = strings.TrimSuffix(loc.Path, strings.SplitN(path, "?", 2)[0])
	final := strings.TrimRight(loc.String(), "/")
	return &Error{Status: resp.StatusCode, Code: "redirect", Message: fmt.Sprintf("The server redirected to %s. Run `burzblog login %s` with the address it redirects to.", final, final)}
}

func readError(resp *http.Response) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		} `json:"error"`
		Current *Post `json:"current"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if json.Unmarshal(raw, &body) != nil || body.Error.Message == "" {
		return &Error{Status: resp.StatusCode, Code: "http", Message: fmt.Sprintf("The server answered HTTP %d. Is %s a BurzBlog server?", resp.StatusCode, resp.Request.URL.Host)}
	}
	return &Error{Status: resp.StatusCode, Code: body.Error.Code, Message: body.Error.Message, Detail: body.Error.Detail, Current: body.Current}
}

func (c *Client) json(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	header := http.Header{}
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(b)
		header.Set("Content-Type", "application/json")
	}
	resp, err := c.request(ctx, method, path, body, header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	return nil
}

// IssueToken trades a username and password for a write token.
func (c *Client) IssueToken(ctx context.Context, username, password, name string) (*Token, error) {
	var t Token
	err := c.json(ctx, http.MethodPost, "/api/auth/token", map[string]string{"username": username, "password": password, "name": name}, &t)
	return &t, err
}

// RevokeToken deletes a token by id.
func (c *Client) RevokeToken(ctx context.Context, id int64) error {
	return c.json(ctx, http.MethodDelete, "/api/tokens/"+strconv.FormatInt(id, 10), nil, nil)
}

// Whoami returns the username the token belongs to.
func (c *Client) Whoami(ctx context.Context) (string, error) {
	var s struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
	}
	if err := c.json(ctx, http.MethodGet, "/api/session", nil, &s); err != nil {
		return "", err
	}
	if !s.Authenticated {
		return "", &Error{Status: 401, Code: "authentication", Message: "This token is not valid any more. Run `burzblog login` again."}
	}
	return s.Username, nil
}

// Posts lists posts, optionally by status and search text.
func (c *Client) Posts(ctx context.Context, status, query, sort string) (*List, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if query != "" {
		q.Set("q", query)
	}
	if sort != "" {
		q.Set("sort", sort)
	}
	var l List
	err := c.json(ctx, http.MethodGet, "/api/posts?"+q.Encode(), nil, &l)
	return &l, err
}

// Post loads one post.
func (c *Client) Post(ctx context.Context, slug string) (*Post, error) {
	var p Post
	err := c.json(ctx, http.MethodGet, "/api/posts/"+url.PathEscape(slug), nil, &p)
	return &p, err
}

// CreatePost creates a draft. An empty slug is derived from the title.
func (c *Client) CreatePost(ctx context.Context, title, slug string) (*Post, error) {
	var p Post
	err := c.json(ctx, http.MethodPost, "/api/posts", map[string]string{"title": title, "slug": slug}, &p)
	return &p, err
}

// Action runs publish, unpublish, unschedule, archive or restore.
func (c *Client) Action(ctx context.Context, slug, action string, version int64) (*Post, error) {
	var p Post
	err := c.json(ctx, http.MethodPost, "/api/posts/"+url.PathEscape(slug)+"/"+action, map[string]int64{"version": version}, &p)
	return &p, err
}

// Schedule sets the time a draft publishes itself.
func (c *Client) Schedule(ctx context.Context, slug string, at time.Time, version int64) (*Post, error) {
	var p Post
	err := c.json(ctx, http.MethodPost, "/api/posts/"+url.PathEscape(slug)+"/schedule",
		map[string]any{"version": version, "at": at.UTC().Format(time.RFC3339)}, &p)
	return &p, err
}

// Markdown downloads a post as a frontmatter document with its version.
func (c *Client) Markdown(ctx context.Context, slug string) (doc string, version int64, err error) {
	resp, err := c.request(ctx, http.MethodGet, "/api/posts/"+url.PathEscape(slug)+"/markdown", nil, nil)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read post: %w", err)
	}
	version, err = strconv.ParseInt(strings.Trim(resp.Header.Get("ETag"), `"`), 10, 64)
	if err != nil {
		return "", 0, errors.New("the server did not say which version it sent")
	}
	return string(raw), version, nil
}

// SaveMarkdown uploads a frontmatter document edited from version. A
// version of 0 creates a new post instead.
func (c *Client) SaveMarkdown(ctx context.Context, slug, doc string, version int64) (*Post, error) {
	header := http.Header{}
	if version == 0 {
		header.Set("If-None-Match", "*")
	} else {
		header.Set("If-Match", `"`+strconv.FormatInt(version, 10)+`"`)
	}
	header.Set("Content-Type", "text/markdown; charset=utf-8")
	resp, err := c.request(ctx, http.MethodPut, "/api/posts/"+url.PathEscape(slug)+"/markdown", strings.NewReader(doc), header)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var p Post
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &p, nil
}

// Backup downloads every post, whatever its status, as a gzipped tar. The
// caller closes it.
func (c *Client) Backup(ctx context.Context) (io.ReadCloser, error) {
	resp, err := c.request(ctx, http.MethodGet, "/api/backup.tar.gz", nil, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// Export downloads published posts as a gzipped tar. The caller closes it.
func (c *Client) Export(ctx context.Context) (io.ReadCloser, error) {
	resp, err := c.request(ctx, http.MethodGet, "/api/export.tar.gz", nil, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
