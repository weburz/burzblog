package server

import (
	"strings"
	"testing"
)

func TestListPagesAndLeavesOutBodies(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	for _, slug := range []string{"a", "b", "c", "d", "e"} {
		if resp, out := h.do("POST", "/api/posts", map[string]any{"title": slug, "slug": slug, "body": "secret body text"}); resp.StatusCode != 201 {
			t.Fatalf("create %s = %d %v", slug, resp.StatusCode, out)
		}
	}
	resp, out := h.do("GET", "/api/posts", nil)
	if resp.StatusCode != 200 || len(out["items"].([]any)) != 5 || out["total"] != 5.0 {
		t.Fatalf("unpaged list = %d %v; without a limit every post is listed", resp.StatusCode, out)
	}
	if raw := out["_raw"].(string); strings.Contains(raw, "secret body text") || strings.Contains(raw, `"body"`) {
		t.Errorf("list carries post bodies: %s", raw)
	}

	_, out = h.do("GET", "/api/posts?limit=2&sort=title", nil)
	items := out["items"].([]any)
	if len(items) != 2 || out["total"] != 5.0 || items[0].(map[string]any)["slug"] != "a" {
		t.Errorf("first page = %v", out)
	}
	_, out = h.do("GET", "/api/posts?limit=2&offset=4&sort=title", nil)
	items = out["items"].([]any)
	if len(items) != 1 || out["total"] != 5.0 || items[0].(map[string]any)["slug"] != "e" {
		t.Errorf("last page = %v", out)
	}
	if counts := out["counts"].(map[string]any); counts["draft"] != 5.0 {
		t.Errorf("counts must cover every post, not the page: %v", counts)
	}
	for _, bad := range []string{"limit=0", "limit=501", "limit=x", "offset=-1", "offset=x"} {
		if resp, out := h.do("GET", "/api/posts?"+bad, nil); resp.StatusCode != 422 || errCode(out) != "validation" {
			t.Errorf("%s = %d %v, want 422 validation", bad, resp.StatusCode, out)
		}
	}
}
