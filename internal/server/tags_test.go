package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestPostTagsUnusableAreRefusedNormalizedAreKept(t *testing.T) {
	c := newContentHarness(t)
	resp, out := c.do("POST", "/api/posts", map[string]any{"title": "Tagged", "slug": "tagged", "tags": []string{"#Go", "two words"}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %v", resp.StatusCode, out)
	}
	if got, _ := out["tags"].([]any); len(got) != 2 || got[0] != "go" || got[1] != "two-words" {
		t.Errorf("tags = %v, want [go two-words]", out["tags"])
	}

	resp, out = c.do("PUT", "/api/posts/tagged", map[string]any{"version": out["version"], "title": "Tagged", "slug": "tagged", "tags": []string{"go", "\U0001F600"}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("emoji tag = %d %v, want 422", resp.StatusCode, out)
	}
	if detail, _ := out["error"].(map[string]any)["detail"].(string); !strings.Contains(detail, "\U0001F600") {
		t.Errorf("detail = %q, want it to name the tag", detail)
	}

	resp, out = c.do("POST", "/api/posts", map[string]any{"title": "Empty", "slug": "empty", "tags": []string{""}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("empty tag = %d %v, want 422", resp.StatusCode, out)
	}
}
