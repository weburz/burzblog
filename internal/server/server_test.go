package server

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/store"
)

const password = "correct horse battery"

type harness struct {
	t      *testing.T
	ts     *httptest.Server
	store  *store.Store
	client *http.Client
	csrf   string
	token  string
}

func newHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	hash, _ := auth.HashPassword(password)
	if _, err := s.CreateUser(ctx, "jarmos", hash); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(s)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, store: s}
	h.ts = httptest.NewUnstartedServer(nil)
	h.ts.Start()
	cfg := Config{
		Origin: h.ts.URL,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Panel: fstest.MapFS{
			"index.html":  {Data: []byte(`<html><script>window.__NUXT__={}</script><script type="application/json">{}</script><script src="/_nuxt/a.js"></script></html>`)},
			"_nuxt/a.js":  {Data: []byte("console.log(1)")},
			"favicon.svg": {Data: []byte("<svg/>")},
		},
		Version: "test",
	}
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := New(cfg, s, a)
	if err != nil {
		t.Fatal(err)
	}
	h.ts.Config.Handler = srv.Handler()
	t.Cleanup(func() { srv.Close(context.Background()); h.ts.Close() })
	jar, _ := cookiejar.New(nil)
	h.client = &http.Client{Jar: jar}
	return h
}

// do sends a request as the browser panel would: same Origin, CSRF header.
func (h *harness) do(method, path string, body any) (*http.Response, map[string]any) {
	h.t.Helper()
	var r io.Reader
	if s, ok := body.(string); ok {
		r = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, r)
	if body != nil {
		if _, ok := body.(string); !ok {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	req.Header.Set("Origin", h.ts.URL)
	if h.csrf != "" {
		req.Header.Set("X-CSRF-Token", h.csrf)
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	out["_raw"] = string(raw)
	return resp, out
}

func (h *harness) login() {
	h.t.Helper()
	resp, out := h.do("POST", "/api/login", map[string]string{"username": "jarmos", "password": password})
	if resp.StatusCode != 200 {
		h.t.Fatalf("login = %d %v", resp.StatusCode, out)
	}
	h.csrf = out["csrf"].(string)
}

func errCode(out map[string]any) string {
	if e, ok := out["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func TestSessionLifecycle(t *testing.T) {
	h := newHarness(t, nil)
	if _, out := h.do("GET", "/api/session", nil); out["authenticated"] != false {
		t.Errorf("anonymous session = %v", out)
	}
	if resp, out := h.do("POST", "/api/login", map[string]string{"username": "jarmos", "password": "nope"}); resp.StatusCode != 401 || errCode(out) != "credentials" {
		t.Errorf("bad login = %d %v", resp.StatusCode, out)
	}
	h.login()
	cookie := h.client.Jar.Cookies(mustURL(h.ts.URL))
	if len(cookie) != 1 || cookie[0].Name != "burzblog_session" {
		t.Errorf("cookies = %v", cookie)
	}
	if _, out := h.do("GET", "/api/session", nil); out["authenticated"] != true || out["csrf"] != h.csrf || out["username"] != "jarmos" {
		t.Errorf("session = %v", out)
	}
	h.do("POST", "/api/logout", nil)
	if resp, _ := h.do("GET", "/api/posts", nil); resp.StatusCode != 401 {
		t.Errorf("after logout = %d", resp.StatusCode)
	}
}

func TestBrowserMutationsNeedOriginAndCSRF(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	csrf := h.csrf
	h.csrf = ""
	if resp, out := h.do("POST", "/api/posts", map[string]string{"title": "x"}); resp.StatusCode != 403 || errCode(out) != "csrf" {
		t.Errorf("no CSRF = %d %v", resp.StatusCode, out)
	}
	h.csrf = csrf

	req, _ := http.NewRequest("POST", h.ts.URL+"/api/posts", strings.NewReader(`{"title":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := h.client.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("foreign origin = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("POST", h.ts.URL+"/api/login", strings.NewReader(`{"username":"jarmos","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("login without Origin = %d", resp.StatusCode)
	}
}

func TestPostLifecycle(t *testing.T) {
	h := newHarness(t, nil)
	h.login()

	resp, out := h.do("POST", "/api/posts", map[string]string{"title": "Hello, World!"})
	if resp.StatusCode != 201 || out["slug"] != "hello-world" || out["version"] != 1.0 || out["updatedBy"] != "jarmos" {
		t.Fatalf("create = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts", map[string]string{"title": "Hello world"}); resp.StatusCode != 409 || errCode(out) != "slug_taken" {
		t.Errorf("duplicate = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts", map[string]string{"title": ""}); resp.StatusCode != 422 {
		t.Errorf("empty title = %d %v", resp.StatusCode, out)
	}

	save := map[string]any{"version": 1, "title": "Hello, World!", "body": "# Hi\n", "description": "A greeting"}
	if resp, out := h.do("PUT", "/api/posts/hello-world", save); resp.StatusCode != 200 || out["version"] != 2.0 {
		t.Fatalf("save = %d %v", resp.StatusCode, out)
	}
	resp, out = h.do("PUT", "/api/posts/hello-world", save)
	if resp.StatusCode != 409 || errCode(out) != "conflict" {
		t.Fatalf("stale save = %d %v", resp.StatusCode, out)
	}
	if cur, ok := out["current"].(map[string]any); !ok || cur["version"] != 2.0 {
		t.Errorf("conflict did not include the current post: %v", out)
	}

	if resp, out := h.do("POST", "/api/posts/hello-world/publish", map[string]any{"version": 2}); resp.StatusCode != 200 || out["status"] != "published" || out["publishedOn"] == nil {
		t.Fatalf("publish = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts/hello-world/publish", map[string]any{"version": 3}); resp.StatusCode != 422 {
		t.Errorf("publish twice = %d %v", resp.StatusCode, out)
	}
	if resp, _ := h.do("DELETE", "/api/posts/hello-world?version=3", nil); resp.StatusCode != 409 {
		t.Errorf("delete published = %d", resp.StatusCode)
	}

	_, list := h.do("GET", "/api/posts?q=hello", nil)
	items := list["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["words"] != 1.0 {
		t.Errorf("list = %v", list)
	}
	if counts := list["counts"].(map[string]any); counts["published"] != 1.0 {
		t.Errorf("counts = %v", counts)
	}

	_, revs := h.do("GET", "/api/posts/hello-world/revisions", nil)
	if n := len(revs["items"].([]any)); n != 2 {
		t.Errorf("revisions = %d, want 2", n)
	}
	if resp, out := h.do("POST", "/api/posts/hello-world/revisions/1/restore", map[string]any{"version": 3}); resp.StatusCode != 200 || out["body"] != "" || out["status"] != "published" {
		t.Errorf("restore = %d %v", resp.StatusCode, out)
	}

	h.do("POST", "/api/posts/hello-world/archive", map[string]any{"version": 4})
	if resp, out := h.do("DELETE", "/api/posts/hello-world?version=5", nil); resp.StatusCode != 200 {
		t.Errorf("delete archived = %d %v", resp.StatusCode, out)
	}
	if resp, _ := h.do("GET", "/api/posts/hello-world", nil); resp.StatusCode != 404 {
		t.Errorf("deleted post still there: %d", resp.StatusCode)
	}
}

func TestPreview(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	_, out := h.do("POST", "/api/preview", map[string]string{"markdown": "**hi** <script>x</script>"})
	if html := out["html"].(string); !strings.Contains(html, "<strong>hi</strong>") || strings.Contains(html, "<script") {
		t.Errorf("preview = %q", html)
	}
}

func TestCLITokenAndMarkdown(t *testing.T) {
	h := newHarness(t, nil)
	// No Origin, like the CLI.
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/auth/token", strings.NewReader(`{"username":"jarmos","password":"`+password+`","name":"laptop"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var tok map[string]any
	json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	if resp.StatusCode != 201 || !strings.HasPrefix(tok["secret"].(string), "bzb_") || tok["scope"] != "write" {
		t.Fatalf("token = %d %v", resp.StatusCode, tok)
	}
	h.token = tok["secret"].(string)
	h.client = http.DefaultClient

	doc := "---\ntitle: From the terminal\nsitemap:\n  priority: 1\n---\n\nWritten in vim.\n"
	if resp, out := h.do("PUT", "/api/posts/terminal/markdown", doc); resp.StatusCode != 428 {
		t.Errorf("PUT without If-Match = %d %v", resp.StatusCode, out)
	}
	req, _ = http.NewRequest("PUT", h.ts.URL+"/api/posts/terminal/markdown", strings.NewReader(doc))
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("If-None-Match", "*")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 201 || resp.Header.Get("ETag") != `"1"` {
		t.Fatalf("create from markdown = %d %v", resp.StatusCode, resp.Header)
	}

	req, _ = http.NewRequest("GET", h.ts.URL+"/api/posts/terminal/markdown", nil)
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, _ = http.DefaultClient.Do(req)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(got), "Written in vim.") || !strings.Contains(string(got), "priority: 1") || resp.Header.Get("ETag") != `"1"` {
		t.Errorf("GET markdown = %q %v", got, resp.Header)
	}

	edited := strings.Replace(string(got), "vim", "neovim", 1)
	put := func(version string) int {
		req, _ := http.NewRequest("PUT", h.ts.URL+"/api/posts/terminal/markdown", strings.NewReader(edited))
		req.Header.Set("Authorization", "Bearer "+h.token)
		req.Header.Set("If-Match", version)
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := put(`"1"`); code != 200 {
		t.Errorf("PUT markdown = %d", code)
	}
	if code := put(`"1"`); code != 409 {
		t.Errorf("stale PUT markdown = %d", code)
	}
	req, _ = http.NewRequest("PUT", h.ts.URL+"/api/posts/terminal/markdown", strings.NewReader(strings.Replace(edited, "title:", "slug: other\ntitle:", 1)))
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("If-Match", `"2"`)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Errorf("mismatched frontmatter slug = %d", resp.StatusCode)
	}
	if resp, out := h.do("PUT", "/api/posts/terminal/markdown", "no frontmatter"); resp.StatusCode != 422 {
		t.Errorf("invalid document = %d %v", resp.StatusCode, out)
	}
}

func TestExportAndScopes(t *testing.T) {
	var hits atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer hook.Close()
	deployDelay = 10 * time.Millisecond
	h := newHarness(t, func(c *Config) { c.DeployHook = hook.URL })
	h.login()
	h.do("POST", "/api/posts", map[string]string{"title": "Draft only"})
	h.do("POST", "/api/posts", map[string]string{"title": "Live"})
	h.do("POST", "/api/posts/live/publish", map[string]any{"version": 1})

	_, out := h.do("POST", "/api/tokens", map[string]string{"name": "vercel", "scope": "export"})
	secret := out["secret"].(string)

	req, _ := http.NewRequest("GET", h.ts.URL+"/api/export.tar.gz", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("export = %v %v", err, resp)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
		body, _ := io.ReadAll(tr)
		if !strings.Contains(string(body), "status: published") {
			t.Errorf("%s is not published: %s", hdr.Name, body)
		}
	}
	resp.Body.Close()
	if len(names) != 1 || names[0] != "live.md" {
		t.Errorf("export files = %v", names)
	}

	req, _ = http.NewRequest("GET", h.ts.URL+"/api/posts", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("export token listing posts = %d", resp.StatusCode)
	}

	resp, _ = http.Get(h.ts.URL + "/api/export.tar.gz")
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("anonymous export = %d", resp.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() != 1 {
		t.Errorf("deploy hook hits = %d, want 1 (only the publish)", hits.Load())
	}
	_, settings := h.do("GET", "/api/settings", nil)
	if strings.Contains(settings["_raw"].(string), hook.URL) {
		t.Error("settings leaked the deploy hook URL")
	}

	_, list := h.do("GET", "/api/tokens", nil)
	items := list["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["secret"] != nil {
		t.Errorf("token list = %v", list)
	}
	id := items[0].(map[string]any)["id"].(float64)
	if resp, _ := h.do("DELETE", "/api/tokens/"+jsonInt(id), nil); resp.StatusCode != 200 {
		t.Errorf("revoke = %d", resp.StatusCode)
	}
}

func TestPanelAndHeaders(t *testing.T) {
	h := newHarness(t, nil)
	resp, err := http.Get(h.ts.URL + "/posts/anything")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(string(body), "__NUXT__") || !strings.Contains(csp, "'sha256-") || strings.Count(csp, "sha256-") != 1 {
		t.Errorf("SPA fallback: body %q csp %q", body, csp)
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("security headers = %v", resp.Header)
	}
	resp, _ = http.Get(h.ts.URL + "/settings/index.html")
	resp.Body.Close()
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Error("a generated HTML file was served without the CSP")
	}
	resp, _ = http.Get(h.ts.URL + "/_nuxt/a.js")
	resp.Body.Close()
	if resp.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("asset caching = %q", resp.Header.Get("Cache-Control"))
	}
	resp, _ = http.Get(h.ts.URL + "/_nuxt/missing.js")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("missing asset = %d", resp.StatusCode)
	}
	resp, _ = http.Get(h.ts.URL + "/api/nope")
	resp.Body.Close()
	if resp.StatusCode != 404 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("unknown API = %d %v", resp.StatusCode, resp.Header)
	}
	resp, _ = http.Get(h.ts.URL + "/healthz")
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("healthz = %d", resp.StatusCode)
	}
}

func TestParseOrigin(t *testing.T) {
	for _, ok := range []string{"https://blog.example.com", "http://127.0.0.1:8080", "https://x.dev/"} {
		if _, err := ParseOrigin(ok); err != nil {
			t.Errorf("ParseOrigin(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "blog.example.com", "ftp://x", "https://x/sub", "https://u:p@x"} {
		if _, err := ParseOrigin(bad); err == nil {
			t.Errorf("ParseOrigin(%q) accepted", bad)
		}
	}
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func jsonInt(f float64) string {
	return strconv.FormatInt(int64(f), 10)
}

func TestMarkdownStatusRules(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/posts", map[string]string{"title": "Live"})
	h.do("POST", "/api/posts/live/publish", map[string]any{"version": 1})
	put := func(doc, version string) *http.Response {
		req, _ := http.NewRequest("PUT", h.ts.URL+"/api/posts/live/markdown", strings.NewReader(doc))
		req.Header.Set("Origin", h.ts.URL)
		req.Header.Set("X-CSRF-Token", h.csrf)
		req.Header.Set("If-Match", version)
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if resp := put("---\ntitle: Live\n---\n\nNo status line.\n", `"2"`); resp.StatusCode != 200 {
		t.Fatalf("save without status = %d", resp.StatusCode)
	}
	if _, out := h.do("GET", "/api/posts/live", nil); out["status"] != "published" {
		t.Errorf("a file without a status line unpublished the post: %v", out["status"])
	}
	h.do("POST", "/api/posts/live/archive", map[string]any{"version": 3})
	if resp := put("---\ntitle: Live\nstatus: published\n---\n", `"4"`); resp.StatusCode != 422 {
		t.Errorf("archived to published from a file = %d, want 422", resp.StatusCode)
	}
}
