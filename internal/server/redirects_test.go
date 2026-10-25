package server

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/weburz/burzblog/internal/store"
)

// publishAs creates a published post with slug and returns its version.
func (h *harness) publishAs(slug string) int {
	h.t.Helper()
	if resp, out := h.do("POST", "/api/posts", map[string]any{"title": slug, "slug": slug}); resp.StatusCode != 201 {
		h.t.Fatalf("create %s = %d %v", slug, resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts/"+slug+"/publish", map[string]any{"version": 1}); resp.StatusCode != 200 {
		h.t.Fatalf("publish %s = %d %v", slug, resp.StatusCode, out)
	}
	return 2
}

// rename saves post slug from version under a new slug and returns the new version.
func (h *harness) rename(slug string, version int, to string) int {
	h.t.Helper()
	resp, out := h.do("PUT", "/api/posts/"+slug, map[string]any{"version": version, "slug": to, "title": to})
	if resp.StatusCode != 200 {
		h.t.Fatalf("rename %s to %s = %d %v", slug, to, resp.StatusCode, out)
	}
	return int(out["version"].(float64))
}

// noFollow is a bearer-only GET that does not follow redirects.
func (h *harness) noFollow(secret, path string) (*http.Response, string) {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.ts.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

// redirectPairs reads {from, to} pairs from the content API as "from>to".
func redirectPairs(t *testing.T, h *harness, secret string) []string {
	t.Helper()
	resp, raw := h.noFollow(secret, "/api/content/redirects")
	if resp.StatusCode != 200 {
		t.Fatalf("redirects = %d %s", resp.StatusCode, raw)
	}
	var body struct {
		Items []struct{ From, To string } `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil || body.Items == nil {
		t.Fatalf("redirects body = %s (%v)", raw, err)
	}
	pairs := []string{}
	for _, it := range body.Items {
		pairs = append(pairs, it.From+">"+it.To)
	}
	return pairs
}

func TestContentRedirectsFollowRenames(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	secret := h.issueToken(store.ScopeExport)
	if got := redirectPairs(t, h, secret); len(got) != 0 {
		t.Fatalf("redirects before any rename = %v", got)
	}

	v := h.publishAs("a")
	v = h.rename("a", v, "b")
	v = h.rename("b", v, "c")
	got := redirectPairs(t, h, secret)
	if len(got) != 2 || got[0] != "a>c" || got[1] != "b>c" {
		t.Errorf("chain = %v, want [a>c b>c]", got)
	}

	// Renaming back makes the live slug win over its own redirect.
	h.rename("c", v, "a")
	got = redirectPairs(t, h, secret)
	if len(got) != 2 || got[0] != "b>a" || got[1] != "c>a" {
		t.Errorf("after rename back = %v, want [b>a c>a]", got)
	}
}

func TestContentRedirectsHideUnpublishedTargets(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	secret := h.issueToken(store.ScopeExport)
	v := h.publishAs("old-name")
	v = h.rename("old-name", v, "new-name")

	if resp, out := h.do("POST", "/api/posts/new-name/unpublish", map[string]any{"version": v}); resp.StatusCode != 200 {
		t.Fatalf("unpublish = %d %v", resp.StatusCode, out)
	}
	if got := redirectPairs(t, h, secret); len(got) != 0 {
		t.Errorf("draft target listed: %v", got)
	}
	// The old slug answers exactly like one that never existed.
	resp, raw := h.noFollow(secret, "/api/content/posts/old-name")
	_, missing := h.noFollow(secret, "/api/content/posts/never-was")
	if resp.StatusCode != 404 || resp.Header.Get("Location") != "" || raw != missing {
		t.Errorf("old slug of a draft = %d %q %s, want the same 404 as a missing slug", resp.StatusCode, resp.Header.Get("Location"), raw)
	}
}

func TestContentPostOldSlugRedirects(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	secret := h.issueToken(store.ScopeExport)
	v := h.publishAs("first")
	h.rename("first", v, "second")

	for path, want := range map[string]string{
		"/api/content/posts/first":                 "/api/content/posts/second",
		"/api/content/posts/first/html":            "/api/content/posts/second/html",
		"/api/content/posts/first?format=markdown": "/api/content/posts/second?format=markdown",
	} {
		resp, raw := h.noFollow(secret, path)
		if resp.StatusCode != 308 || resp.Header.Get("Location") != want {
			t.Errorf("%s = %d Location %q, want 308 %q", path, resp.StatusCode, resp.Header.Get("Location"), want)
		}
		var body map[string]string
		if err := json.Unmarshal([]byte(raw), &body); err != nil || body["redirect"] != "second" {
			t.Errorf("%s body = %s", path, raw)
		}
	}
	// A client that follows redirects ends at the post.
	req, _ := http.NewRequest("GET", h.ts.URL+"/api/content/posts/first", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("followed redirect = %v %v", err, resp)
	}
	resp.Body.Close()

	// Anonymous callers still get 401, not a hint that a slug moved.
	if resp, _ := h.noFollow("", "/api/content/posts/first"); resp.StatusCode != 401 {
		t.Errorf("anonymous old slug = %d, want 401", resp.StatusCode)
	}
}

func TestRedirectSlugReuseAndDelete(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	secret := h.issueToken(store.ScopeExport)
	v := h.publishAs("one")
	h.rename("one", v, "two")

	// A new post may take a slug that is only a redirect; the redirect goes.
	h.publishAs("one")
	if got := redirectPairs(t, h, secret); len(got) != 0 {
		t.Errorf("redirects after reuse = %v", got)
	}
	resp, raw := h.noFollow(secret, "/api/content/posts/one")
	if resp.StatusCode != 200 {
		t.Errorf("reused slug = %d %s, want the new post", resp.StatusCode, raw)
	}

	// Deleting a post deletes its redirects.
	v = h.publishAs("three")
	v = h.rename("three", v, "four")
	if resp, out := h.do("POST", "/api/posts/four/archive", map[string]any{"version": v}); resp.StatusCode != 200 {
		t.Fatalf("archive = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("DELETE", "/api/posts/four?version=4", nil); resp.StatusCode != 200 {
		t.Fatalf("delete = %d %v", resp.StatusCode, out)
	}
	if _, err := h.store.RedirectTarget(t.Context(), "three"); err != store.ErrNotFound {
		t.Errorf("redirect after delete = %v, want ErrNotFound", err)
	}
}

func TestDeletePostRedirect(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	secret := h.issueToken(store.ScopeExport)
	v := h.publishAs("x")
	h.rename("x", v, "y")

	resp, out := h.do("GET", "/api/posts/y/redirects", nil)
	if items, _ := out["items"].([]any); resp.StatusCode != 200 || len(items) != 1 || items[0] != "x" {
		t.Errorf("list = %d %v", resp.StatusCode, out)
	}
	// An export token may read redirects but not remove one.
	if resp, out := h.as(secret, "DELETE", "/api/posts/y/redirects/x"); resp.StatusCode != 403 || errCode(out) != "scope" {
		t.Errorf("export token delete = %d %v, want 403 scope", resp.StatusCode, out)
	}
	// A browser without the CSRF token is refused.
	csrf := h.csrf
	h.csrf = ""
	if resp, _ := h.do("DELETE", "/api/posts/y/redirects/x", nil); resp.StatusCode != 403 {
		t.Errorf("delete without CSRF = %d, want 403", resp.StatusCode)
	}
	h.csrf = csrf
	// A redirect belongs to one post only.
	h.publishAs("other")
	if resp, _ := h.do("DELETE", "/api/posts/other/redirects/x", nil); resp.StatusCode != 404 {
		t.Errorf("delete through another post = %d, want 404", resp.StatusCode)
	}
	if resp, _ := h.do("DELETE", "/api/posts/y/redirects/x", nil); resp.StatusCode != 200 {
		t.Errorf("delete = %d, want 200", resp.StatusCode)
	}
	if resp, out := h.do("DELETE", "/api/posts/y/redirects/x", nil); resp.StatusCode != 404 || errCode(out) != "not_found" {
		t.Errorf("delete twice = %d %v, want 404", resp.StatusCode, out)
	}
	if got := redirectPairs(t, h, secret); len(got) != 0 {
		t.Errorf("redirects after delete = %v", got)
	}
	if resp, _ := h.noFollow(secret, "/api/content/posts/x"); resp.StatusCode != 404 {
		t.Errorf("removed old slug = %d, want 404", resp.StatusCode)
	}
}

func TestExportHoldsRedirects(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	secret := h.issueToken(store.ScopeExport)
	v := h.publishAs("before")
	h.rename("before", v, "after")
	// Redirects to an unpublished post stay out of the export.
	v = h.publishAs("hidden-old")
	v = h.rename("hidden-old", v, "hidden-new")
	h.do("POST", "/api/posts/hidden-new/unpublish", map[string]any{"version": v})

	req, _ := http.NewRequest("GET", h.ts.URL+"/api/export.tar.gz", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("export = %v %v", err, resp)
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var file string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "redirects.json" {
			raw, _ := io.ReadAll(tr)
			file = string(raw)
		}
	}
	var body struct {
		Items []struct{ From, To string } `json:"items"`
	}
	if err := json.Unmarshal([]byte(file), &body); err != nil || len(body.Items) != 1 ||
		body.Items[0].From != "before" || body.Items[0].To != "after" {
		t.Errorf("redirects.json = %q (%v)", file, err)
	}
}
