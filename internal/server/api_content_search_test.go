package server

import (
	"strings"
	"testing"
)

func TestContentSearchBodiesSnippetsAndSyntax(t *testing.T) {
	c := newContentHarness(t)
	c.do("POST", "/api/posts", map[string]any{"title": "Garden", "slug": "garden", "tags": []string{"green"},
		"body": "Tomatoes need <b>sun</b> and water."})
	c.do("POST", "/api/posts/garden/publish", map[string]any{"version": 1})
	c.do("POST", "/api/posts", map[string]any{"title": "Hidden", "slug": "hidden", "body": "Tomatoes in a draft."})

	_, found := c.do("GET", "/api/content/posts?q=tomato", nil)
	items := found["items"].([]any)
	if found["total"] != 1.0 || len(items) != 1 {
		t.Fatalf("body search = %v, want only the published post", found)
	}
	if snippet, _ := items[0].(map[string]any)["snippet"].(string); !strings.Contains(snippet, "Tomatoes") {
		t.Errorf("snippet = %q", snippet)
	}
	if _, found := c.do("GET", "/api/content/posts?q=tomato&tag=green", nil); found["total"] != 1.0 {
		t.Errorf("search with tag = %v", found)
	}
	if _, found := c.do("GET", "/api/content/posts?q=tomato&tag=blue", nil); found["total"] != 0.0 {
		t.Errorf("search with another tag = %v", found)
	}
	for _, q := range []string{`%22`, `title:garden%20OR%20x`, `NEAR(`, `*`, `-`} {
		if resp, out := c.do("GET", "/api/content/posts?q="+q, nil); resp.StatusCode != 200 {
			t.Errorf("q=%s = %d %v, want 200", q, resp.StatusCode, out)
		}
	}
	resp, raw := c.get(c.export, "/api/content/posts")
	if resp.StatusCode != 200 || strings.Contains(raw, `"snippet"`) {
		t.Errorf("a list without q has a snippet: %s", raw)
	}
	_, panel := c.do("GET", "/api/posts?q=tomato", nil)
	if panel["total"] != 2.0 {
		t.Errorf("panel search = %v, want the published post and the draft", panel)
	}
}
