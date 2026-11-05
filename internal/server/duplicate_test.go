package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestDuplicatePostAPI(t *testing.T) {
	h, _ := scheduleHarness(t, nil)
	extra := "sitemap:\n  priority: 0.8\ncustom: [a, b]\n"
	if resp, out := h.do("PUT", "/api/posts/"+h.draft("Original"), map[string]any{
		"version": 1, "title": "Original", "description": "About it", "body": "Hello **world**",
		"coverUrl": "https://example.com/c.png", "coverAlt": "A cover", "extra": extra,
		"tags": []string{"news", "go"}, "author": "Ada", "seoTitle": "SEO", "seoDescription": "SEO desc",
		"canonicalUrl": "https://example.com/original", "featured": true,
	}); resp.StatusCode != 200 {
		t.Fatalf("save = %d %v", resp.StatusCode, out)
	}
	// Publishing and scheduling would copy dates and status if the copy were careless.
	if resp, out := h.do("POST", "/api/posts/original/publish", map[string]any{"version": 2}); resp.StatusCode != 200 {
		t.Fatalf("publish = %d %v", resp.StatusCode, out)
	}

	resp, out := h.do("POST", "/api/posts/original/duplicate", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("duplicate = %d %v", resp.StatusCode, out)
	}
	for field, want := range map[string]any{
		"slug": "original-copy", "title": "Original (copy)", "description": "About it", "body": "Hello **world**",
		"coverUrl": "https://example.com/c.png", "coverAlt": "A cover", "extra": extra, "author": "Ada",
		"seoTitle": "SEO", "seoDescription": "SEO desc", "canonicalUrl": "https://example.com/original",
		"status": "draft", "featured": false, "version": 1.0,
	} {
		if out[field] != want {
			t.Errorf("%s = %v, want %v", field, out[field], want)
		}
	}
	if tags, _ := out["tags"].([]any); len(tags) != 2 || tags[0] != "news" || tags[1] != "go" {
		t.Errorf("tags = %v", out["tags"])
	}
	for _, field := range []string{"publishedOn", "updatedOn", "publishAt"} {
		if out[field] != nil {
			t.Errorf("%s = %v, want null", field, out[field])
		}
	}
	// The original is untouched.
	if resp, got := h.do("GET", "/api/posts/original", nil); resp.StatusCode != 200 || got["status"] != "published" || got["version"] != 3.0 {
		t.Errorf("original = %d %v", resp.StatusCode, got)
	}
	// The copy is a normal post: it is found by search and saves again.
	if resp, got := h.do("GET", "/api/posts?q=world", nil); resp.StatusCode != 200 || !strings.Contains(got["_raw"].(string), `"original-copy"`) {
		t.Errorf("search = %d %v", resp.StatusCode, got["_raw"])
	}
}

func TestDuplicatePostSlugCollisions(t *testing.T) {
	h, _ := scheduleHarness(t, nil)
	slug := h.draft("Thing")
	for _, want := range []string{"thing-copy", "thing-copy-2", "thing-copy-3"} {
		resp, out := h.do("POST", "/api/posts/"+slug+"/duplicate", nil)
		if resp.StatusCode != http.StatusCreated || out["slug"] != want {
			t.Fatalf("duplicate = %d %v, want %s", resp.StatusCode, out["slug"], want)
		}
	}
	// A copy of a copy starts its own chain.
	if _, out := h.do("POST", "/api/posts/thing-copy/duplicate", nil); out["slug"] != "thing-copy-copy" || out["title"] != "Thing (copy) (copy)" {
		t.Errorf("copy of copy = %v %v", out["slug"], out["title"])
	}
}

func TestDuplicatePostRefusals(t *testing.T) {
	h, _ := scheduleHarness(t, nil)
	if resp, out := h.do("POST", "/api/posts/nothing-here/duplicate", nil); resp.StatusCode != 404 {
		t.Errorf("unknown post = %d %v", resp.StatusCode, out)
	}
	h.draft("Thing")
	// Browser writes need the CSRF token.
	csrf := h.csrf
	h.csrf = "wrong"
	if resp, out := h.do("POST", "/api/posts/thing/duplicate", nil); resp.StatusCode != 403 {
		t.Errorf("bad csrf = %d %v", resp.StatusCode, out)
	}
	h.csrf = csrf
	// Signed out.
	if _, out := h.do("POST", "/api/logout", nil); out["_raw"] == nil {
		t.Fatal("logout")
	}
	if resp, out := h.do("POST", "/api/posts/thing/duplicate", nil); resp.StatusCode != 401 {
		t.Errorf("anonymous = %d %v", resp.StatusCode, out)
	}
}
