package server

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/store"
)

// contentHarness has a published post "live" plus a draft, a scheduled draft
// and an archived post, and one token of each scope.
type contentHarness struct {
	*harness
	export, write string
}

func newContentHarness(t *testing.T) *contentHarness {
	t.Helper()
	return newContentHarnessWith(t, nil)
}

// newContentHarnessWith is newContentHarness with a changed Config.
func newContentHarnessWith(t *testing.T, mutate func(*Config)) *contentHarness {
	t.Helper()
	h := newHarness(t, mutate)
	h.login()
	mk := func(slug, body string) {
		if resp, out := h.do("POST", "/api/posts", map[string]any{"title": slug, "slug": slug, "body": body,
			"description": "about " + slug, "coverUrl": "https://example.com/" + slug + ".png", "coverAlt": "cover",
			"extra": "sitemap:\n  priority: 1\n"}); resp.StatusCode != 201 {
			t.Fatalf("create %s = %d %v", slug, resp.StatusCode, out)
		}
	}
	mk("live", "# Hi\n\nthere <script>alert(1)</script> friend\n")
	mk("draft", "secret draft")
	mk("later", "secret scheduled")
	mk("old", "secret archived")
	for _, slug := range []string{"live", "old"} {
		if resp, out := h.do("POST", "/api/posts/"+slug+"/publish", map[string]any{"version": 1}); resp.StatusCode != 200 {
			t.Fatalf("publish %s = %d %v", slug, resp.StatusCode, out)
		}
	}
	if resp, out := h.do("POST", "/api/posts/old/archive", map[string]any{"version": 2}); resp.StatusCode != 200 {
		t.Fatalf("archive = %d %v", resp.StatusCode, out)
	}
	at := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	if resp, out := h.do("POST", "/api/posts/later/schedule", map[string]any{"version": 1, "at": at}); resp.StatusCode != 200 {
		t.Fatalf("schedule = %d %v", resp.StatusCode, out)
	}
	return &contentHarness{harness: h, export: h.issueToken(store.ScopeExport), write: h.issueToken(store.ScopeWrite)}
}

// get sends a bearer-only GET with extra header pairs and returns the raw body.
func (c *contentHarness) get(secret, path string, headers ...string) (*http.Response, string) {
	c.t.Helper()
	req, _ := http.NewRequest("GET", c.ts.URL+path, nil)
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

func TestContentPostJSON(t *testing.T) {
	c := newContentHarness(t)
	for name, secret := range map[string]string{"export": c.export, "write": c.write} {
		resp, raw := c.get(secret, "/api/content/posts/live")
		if resp.StatusCode != 200 {
			t.Fatalf("%s token = %d %s", name, resp.StatusCode, raw)
		}
		if got := resp.Header.Get("Cache-Control"); got != "private, max-age=60" {
			t.Errorf("%s Cache-Control = %q", name, got)
		}
		if resp.Header.Get("ETag") != `"2"` || resp.Header.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") {
			t.Errorf("%s headers = %v", name, resp.Header)
		}
		for _, want := range []string{`"slug":"live"`, `"coverUrl":"https://example.com/live.png"`, `"words":`,
			`"readingMinutes":1`, `"version":2`, `"body":"# Hi\n\nthere `, `alert(1)`} {
			if !strings.Contains(raw, want) {
				t.Errorf("%s response lacks %s: %s", name, want, raw)
			}
		}
		if strings.Contains(raw, `"status"`) || strings.Contains(raw, "updatedBy") || strings.Contains(raw, "publishAt") {
			t.Errorf("response leaks editorial fields: %s", raw)
		}
	}
	if resp, _ := c.get("", "/api/content/posts/live"); resp.StatusCode != 401 {
		t.Errorf("anonymous = %d, want 401", resp.StatusCode)
	}
	// A browser session may read it too.
	if resp, out := c.do("GET", "/api/content/posts/live", nil); resp.StatusCode != 200 || out["slug"] != "live" {
		t.Errorf("session = %d %v", resp.StatusCode, out)
	}
}

func TestContentPostHidesUnpublishedLikeMissing(t *testing.T) {
	c := newContentHarness(t)
	for _, suffix := range []string{"", "?format=markdown", "/html"} {
		resp, missing := c.get(c.export, "/api/content/posts/nope"+suffix)
		if resp.StatusCode != 404 {
			t.Errorf("missing%s = %d", suffix, resp.StatusCode)
		}
		for _, slug := range []string{"draft", "later", "old"} {
			resp, raw := c.get(c.export, "/api/content/posts/"+slug+suffix)
			if resp.StatusCode != 404 || raw != missing || resp.Header.Get("ETag") != "" ||
				resp.Header.Get("Cache-Control") != "no-store" {
				t.Errorf("%s%s = %d %q %v, want the missing-slug answer %q", slug, suffix, resp.StatusCode, raw, resp.Header, missing)
			}
		}
	}
}

func TestContentPostMarkdownMatchesExport(t *testing.T) {
	c := newContentHarness(t)
	resp, md := c.get(c.export, "/api/content/posts/live?format=markdown")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Fatalf("markdown = %d %v", resp.StatusCode, resp.Header)
	}
	req, _ := http.NewRequest("GET", c.ts.URL+"/api/export.tar.gz", nil)
	req.Header.Set("Authorization", "Bearer "+c.export)
	eresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer eresp.Body.Close()
	gz, err := gzip.NewReader(eresp.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil || hdr.Name != "live.md" {
		t.Fatalf("export entry = %v %v", hdr, err)
	}
	entry, _ := io.ReadAll(tr)
	if string(entry) != md {
		t.Errorf("markdown differs from the export entry:\n%q\n%q", md, entry)
	}
	if !strings.Contains(md, "priority: 1") || !strings.HasPrefix(md, "---\n") {
		t.Errorf("markdown lost its frontmatter or extra keys: %q", md)
	}
}

func TestContentPostBadFormat(t *testing.T) {
	c := newContentHarness(t)
	resp, raw := c.get(c.export, "/api/content/posts/live?format=xml")
	if resp.StatusCode != 422 || !strings.Contains(raw, `"validation"`) {
		t.Errorf("bad format = %d %s", resp.StatusCode, raw)
	}
}

func TestContentHTMLIsSanitized(t *testing.T) {
	c := newContentHarness(t)
	resp, raw := c.get(c.export, "/api/content/posts/live/html")
	if resp.StatusCode != 200 || !strings.Contains(raw, `\u003ch1`) || strings.Contains(raw, `\u003cscript`) {
		t.Errorf("html = %d %s", resp.StatusCode, raw)
	}
	if resp.Header.Get("ETag") == "" || resp.Header.Get("Cache-Control") != "private, max-age=60" {
		t.Errorf("html headers = %v", resp.Header)
	}
}

func TestContentNotModified(t *testing.T) {
	c := newContentHarness(t)
	for _, path := range []string{
		"/api/content/posts/live", "/api/content/posts/live?format=markdown", "/api/content/posts/live/html", "/api/content/posts",
	} {
		resp, _ := c.get(c.export, path)
		tag := resp.Header.Get("ETag")
		if tag == "" {
			t.Fatalf("%s has no ETag", path)
		}
		for _, match := range []string{tag, "W/" + tag, `"other", ` + tag, "*"} {
			resp, raw := c.get(c.export, path, "If-None-Match", match)
			if resp.StatusCode != 304 || raw != "" || resp.Header.Get("ETag") != tag ||
				resp.Header.Get("Cache-Control") != "private, max-age=60" {
				t.Errorf("%s If-None-Match %s = %d %q %v", path, match, resp.StatusCode, raw, resp.Header)
			}
		}
		if resp, _ := c.get(c.export, path, "If-None-Match", `"stale"`); resp.StatusCode != 200 {
			t.Errorf("%s with a stale tag = %d, want 200", path, resp.StatusCode)
		}
		// A 304 still needs a valid caller.
		if resp, _ := c.get("", path, "If-None-Match", tag); resp.StatusCode != 401 {
			t.Errorf("%s anonymous conditional = %d, want 401", path, resp.StatusCode)
		}
	}
}

func TestContentETagFollowsChanges(t *testing.T) {
	c := newContentHarness(t)
	resp, _ := c.get(c.export, "/api/content/posts/live")
	old := resp.Header.Get("ETag")
	listResp, _ := c.get(c.export, "/api/content/posts")
	oldList := listResp.Header.Get("ETag")
	if resp, out := c.do("PUT", "/api/posts/live", map[string]any{"version": 2, "title": "Live", "body": "changed"}); resp.StatusCode != 200 {
		t.Fatalf("save = %d %v", resp.StatusCode, out)
	}
	resp, raw := c.get(c.export, "/api/content/posts/live", "If-None-Match", old)
	if resp.StatusCode != 200 || resp.Header.Get("ETag") == old || !strings.Contains(raw, "changed") {
		t.Errorf("after save = %d %v %s", resp.StatusCode, resp.Header, raw)
	}
	if resp, _ := c.get(c.export, "/api/content/posts", "If-None-Match", oldList); resp.StatusCode != 200 {
		t.Errorf("list after save with old tag = %d, want 200", resp.StatusCode)
	}
}

func TestContentList(t *testing.T) {
	c := newContentHarness(t)
	resp, raw := c.get(c.export, "/api/content/posts")
	if resp.StatusCode != 200 {
		t.Fatalf("list = %d %s", resp.StatusCode, raw)
	}
	for _, leak := range []string{`"slug":"draft"`, `"slug":"later"`, `"slug":"old"`, "secret", `"body"`, "updatedBy"} {
		if strings.Contains(raw, leak) {
			t.Errorf("list leaks %q: %s", leak, raw)
		}
	}
	if !strings.Contains(raw, `"total":1`) || !strings.Contains(raw, `"slug":"live"`) || !strings.Contains(raw, `"coverUrl"`) {
		t.Errorf("list = %s", raw)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, max-age=60" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestContentListOrderPagingAndSearch(t *testing.T) {
	c := newContentHarness(t)
	for i, slug := range []string{"a", "b", "c", "d"} {
		c.do("POST", "/api/posts", map[string]any{"title": "Post " + slug, "slug": slug,
			"publishedOn": fmt.Sprintf("2026-01-0%dT00:00:00Z", i+1)})
		c.do("POST", "/api/posts/"+slug+"/publish", map[string]any{"version": 1})
	}
	_, out := c.do("GET", "/api/content/posts", nil)
	var slugs []string
	for _, it := range out["items"].([]any) {
		slugs = append(slugs, it.(map[string]any)["slug"].(string))
	}
	// "live" was published today, after the dated posts' publishedOn.
	if got := strings.Join(slugs, ","); out["total"] != 5.0 || got != "live,d,c,b,a" {
		t.Errorf("order = %s total %v, want newest published first", got, out["total"])
	}
	_, page := c.do("GET", "/api/content/posts?limit=2&offset=1", nil)
	items := page["items"].([]any)
	if len(items) != 2 || page["total"] != 5.0 || items[0].(map[string]any)["slug"] != "d" {
		t.Errorf("page = %v", page)
	}
	if _, past := c.do("GET", "/api/content/posts?limit=2&offset=50", nil); len(past["items"].([]any)) != 0 || past["total"] != 5.0 {
		t.Errorf("past the end = %v", past)
	}
	if _, found := c.do("GET", "/api/content/posts?q=POST%20C", nil); found["total"] != 1.0 {
		t.Errorf("search = %v", found)
	}
	if _, found := c.do("GET", "/api/content/posts?q=secret", nil); found["total"] != 0.0 {
		t.Errorf("search reached unpublished posts: %v", found)
	}
	if _, found := c.do("GET", "/api/content/posts?q=later", nil); found["total"] != 0.0 {
		t.Errorf("search found a scheduled post: %v", found)
	}
	for _, q := range []string{"limit=0", "limit=501", "limit=x", "offset=-1", "offset=x"} {
		if resp, out := c.do("GET", "/api/content/posts?"+q, nil); resp.StatusCode != 422 || errCode(out) != "validation" {
			t.Errorf("%s = %d %v, want 422", q, resp.StatusCode, out)
		}
	}
	if resp, _ := c.do("GET", "/api/content/posts?limit=500", nil); resp.StatusCode != 200 {
		t.Errorf("limit=500 = %d", resp.StatusCode)
	}
}

func TestExportTokenStillRefusedOutsideContent(t *testing.T) {
	c := newContentHarness(t)
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/posts"}, {"GET", "/api/posts/live"}, {"GET", "/api/posts/live/markdown"},
		{"PUT", "/api/posts/live"}, {"POST", "/api/posts"}, {"POST", "/api/posts/live/unpublish"},
		{"DELETE", "/api/posts/live"}, {"POST", "/api/preview"}, {"GET", "/api/backup.tar.gz"},
	} {
		resp, out := c.as(c.export, rt.method, rt.path)
		if resp.StatusCode != 403 || errCode(out) != "scope" {
			t.Errorf("export token %s %s = %d %v, want 403 scope", rt.method, rt.path, resp.StatusCode, out)
		}
	}
	// Content routes only read.
	if resp, _ := c.as(c.export, "POST", "/api/content/posts/live"); resp.StatusCode == 200 || resp.StatusCode == 201 {
		t.Errorf("POST on a content route = %d", resp.StatusCode)
	}
	// A write token keeps working on the editing routes.
	if resp, _ := c.as(c.write, "GET", "/api/posts"); resp.StatusCode != 200 {
		t.Errorf("write token = %d", resp.StatusCode)
	}
}

func TestContentTagsAndMetadata(t *testing.T) {
	c := newContentHarness(t)
	for slug, body := range map[string]map[string]any{
		"tagged": {"tags": []string{"go", "web"}, "author": "Ada", "seoTitle": "Tagged | Blog",
			"seoDescription": "seo text", "canonicalUrl": "https://example.com/tagged"},
		"hidden": {"tags": []string{"go", "secret-plan"}},
	} {
		body["title"], body["slug"], body["body"] = slug, slug, "text"
		if resp, out := c.do("POST", "/api/posts", body); resp.StatusCode != 201 {
			t.Fatalf("create %s = %d %v", slug, resp.StatusCode, out)
		}
	}
	if resp, out := c.do("POST", "/api/posts/tagged/publish", map[string]any{"version": 1}); resp.StatusCode != 200 {
		t.Fatalf("publish = %d %v", resp.StatusCode, out)
	}

	resp, raw := c.get(c.export, "/api/content/posts?tag=Go")
	if resp.StatusCode != 200 {
		t.Fatalf("list ?tag=Go = %d %s", resp.StatusCode, raw)
	}
	for _, want := range []string{`"slug":"tagged"`, `"total":1`, `"tags":["go","web"]`, `"author":"Ada"`,
		`"seoTitle":"Tagged | Blog"`, `"seoDescription":"seo text"`, `"canonicalUrl":"https://example.com/tagged"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("list ?tag=Go lacks %s: %s", want, raw)
		}
	}
	if strings.Contains(raw, "hidden") {
		t.Errorf("list ?tag=Go shows the draft: %s", raw)
	}
	if _, raw := c.get(c.export, "/api/content/posts?tag=secret-plan"); !strings.Contains(raw, `"total":0`) {
		t.Errorf("draft-only tag lists posts: %s", raw)
	}
	if _, raw := c.get(c.export, "/api/content/posts/live"); !strings.Contains(raw, `"tags":[]`) {
		t.Errorf("untagged post lacks an empty tags list: %s", raw)
	}

	resp, raw = c.get(c.export, "/api/content/tags")
	if resp.StatusCode != 200 || resp.Header.Get("ETag") == "" {
		t.Fatalf("tags = %d etag %q %s", resp.StatusCode, resp.Header.Get("ETag"), raw)
	}
	if want := `{"items":[{"count":1,"label":"Go","tag":"go"},{"count":1,"label":"Web","tag":"web"}]}`; strings.TrimSpace(raw) != want {
		t.Errorf("tags = %s, want %s", raw, want)
	}
	if resp, _ := c.get(c.export, "/api/content/tags", "If-None-Match", resp.Header.Get("ETag")); resp.StatusCode != 304 {
		t.Errorf("tags If-None-Match = %d, want 304", resp.StatusCode)
	}
	if resp, _ := c.get("", "/api/content/tags"); resp.StatusCode != 401 {
		t.Errorf("anonymous tags = %d, want 401", resp.StatusCode)
	}
}

func TestReaderQueryAndMethodErrors(t *testing.T) {
	c := newContentHarness(t)
	for _, query := range []string{"sort=title", "tag=a&tag=b", "tag=a,b", "q=x;q=y"} {
		if resp, _ := c.get(c.export, "/api/content/posts?"+query); resp.StatusCode != 422 {
			t.Errorf("%s: %d", query, resp.StatusCode)
		}
	}
	if resp, _ := c.do("POST", "/api/content/posts", nil); resp.StatusCode != 405 || resp.Header.Get("Allow") != "GET, HEAD" {
		t.Errorf("method: %d %q", resp.StatusCode, resp.Header.Get("Allow"))
	}
	if resp, _ := c.get(c.export, "/api/no-such-endpoint"); resp.StatusCode != 404 {
		t.Errorf("unknown route: %d", resp.StatusCode)
	}
}

func TestPublishedInternalLinksAndUploadSize(t *testing.T) {
	c := newContentHarnessWith(t, withSite)
	ctx := context.Background()
	name := strings.Repeat("4", 32) + ".png"
	upload, _, err := c.store.AddMedia(ctx, store.Media{FileName: name, SHA256: strings.Repeat("4", 64), Size: 7, OriginalName: "cover.png", MIME: "image/png", Width: 80, Height: 100, UploadedBy: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	live, err := c.store.GetPost(ctx, "live")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.store.UpdatePost(ctx, "live", live.Version, "ada", func(p *post.Post) error {
		p.Body = "[local](/elsewhere) [same](https://example.com/a) [external](https://other.example/x) ![cover](/media/" + name + ") note[^x] again[^x]\n\n[^x]: reference"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/content/posts/live/html"
	resp, raw := c.get(c.export, path)
	var output struct{ HTML string }
	if err = json.Unmarshal([]byte(raw), &output); err != nil || resp.StatusCode != 200 {
		t.Fatalf("content HTML: %d %s %v", resp.StatusCode, raw, err)
	}
	for _, want := range []string{`href="/elsewhere" rel="noreferrer"`, `href="https://example.com/a"`, `href="https://other.example/x"`, `width="80" height="100"`, `id="live-fn:1"`, `href="#live-fn:1"`, `class="footnotes"`} {
		if !strings.Contains(output.HTML, want) {
			t.Errorf("missing %q in %s", want, output.HTML)
		}
	}
	if strings.Count(output.HTML, `target="_blank"`) != 1 || strings.Count(output.HTML, "nofollow") != 1 {
		t.Errorf("external link protection: %s", output.HTML)
	}
	if _, err = c.store.DeleteMedia(ctx, upload.ID); err != nil {
		t.Fatal(err)
	}
	resp, raw = c.get(c.export, path, "If-None-Match", resp.Header.Get("ETag"))
	if resp.StatusCode != 200 || strings.Contains(raw, "width=") {
		t.Fatalf("deleted image still cached: %d %s", resp.StatusCode, raw)
	}
}
