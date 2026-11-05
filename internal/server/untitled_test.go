package server

import (
	"testing"
)

func TestCreateUntitledDraft(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	for _, slug := range []string{"untitled", "untitled-2"} {
		resp, out := h.do("POST", "/api/posts", map[string]any{"untitled": true})
		if resp.StatusCode != 201 || out["slug"] != slug || out["title"] != "" || out["status"] != "draft" || out["version"] != 1.0 {
			t.Fatalf("create untitled = %d %v, want slug %s", resp.StatusCode, out, slug)
		}
	}
	// A slug or title alongside the flag is refused.
	for _, body := range []map[string]any{{"untitled": true, "slug": "x"}, {"untitled": true, "title": "T"}} {
		if resp, out := h.do("POST", "/api/posts", body); resp.StatusCode != 422 {
			t.Errorf("create %v = %d %v, want 422", body, resp.StatusCode, out)
		}
	}
	// Without the flag an empty title is still refused.
	if resp, out := h.do("POST", "/api/posts", map[string]any{"title": "", "slug": "plain"}); resp.StatusCode != 422 {
		t.Errorf("create without title = %d %v, want 422", resp.StatusCode, out)
	}
}

func TestUntitledDraftAutosavesRenamesAndCannotPublishWithoutTitle(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/posts", map[string]any{"untitled": true})

	// An autosave with the title still empty is accepted.
	resp, out := h.do("PUT", "/api/posts/untitled", map[string]any{"version": 1, "body": "words", "autosave": true})
	if resp.StatusCode != 200 || out["version"] != 2.0 {
		t.Fatalf("autosave untitled = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts/untitled/publish", map[string]any{"version": 2}); resp.StatusCode != 422 {
		t.Errorf("publish untitled = %d %v, want 422", resp.StatusCode, out)
	}
	future := "2099-01-01T00:00:00Z"
	if resp, out := h.do("POST", "/api/posts/untitled/schedule", map[string]any{"version": 2, "at": future}); resp.StatusCode != 422 {
		t.Errorf("schedule untitled = %d %v, want 422", resp.StatusCode, out)
	}

	// Titling it renames the draft without leaving a redirect.
	resp, out = h.do("PUT", "/api/posts/untitled", map[string]any{
		"version": 2, "slug": "my-post", "title": "My post", "body": "words", "autosave": true,
	})
	if resp.StatusCode != 200 || out["slug"] != "my-post" {
		t.Fatalf("rename = %d %v", resp.StatusCode, out)
	}
	resp, out = h.do("GET", "/api/posts/my-post/redirects", nil)
	if resp.StatusCode != 200 || len(out["items"].([]any)) != 0 {
		t.Errorf("redirects = %d %v, want none", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts/my-post/publish", map[string]any{"version": 3}); resp.StatusCode != 200 {
		t.Errorf("publish titled = %d %v", resp.StatusCode, out)
	}
}

func TestMarkdownEndpointStillRequiresATitle(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/posts", map[string]any{"untitled": true})
	status, raw := h.send("PUT", "/api/posts/untitled/markdown", "---\ntitle: \"\"\n---\n\nBody.\n", map[string]string{"If-Match": `"1"`})
	if status != 422 {
		t.Errorf("markdown save without title = %d %s, want 422", status, raw)
	}
	status, raw = h.send("PUT", "/api/posts/new-one/markdown", "---\ntitle: \"\"\n---\n\nBody.\n", map[string]string{"If-None-Match": "*"})
	if status != 422 {
		t.Errorf("markdown create without title = %d %s, want 422", status, raw)
	}
}

func TestAuthorMayDeleteOnlyAnEmptyNeverPublishedDraft(t *testing.T) {
	h := newHarness(t, nil)
	withRoles(t, h)
	seedPosts(t, h)
	h.signInAs("grace", false)
	_, out := h.do("POST", "/api/posts", map[string]any{"untitled": true})
	if out["slug"] != "untitled" {
		t.Fatalf("create untitled = %v", out)
	}

	// A draft with writing in it, and an archived post, stay admin-only.
	for _, path := range []string{"/api/posts/hello?version=1", "/api/posts/gone?version=2"} {
		if status, raw := h.send("DELETE", path, "", nil); status != 403 {
			t.Errorf("author DELETE %s = %d %s, want 403", path, status, raw)
		}
	}
	if status, raw := h.send("DELETE", "/api/posts/untitled", "", nil); status != 400 {
		t.Errorf("DELETE without version = %d %s, want 400", status, raw)
	}
	if status, raw := h.send("DELETE", "/api/posts/untitled?version=9", "", nil); status != 409 {
		t.Errorf("DELETE with stale version = %d %s, want 409", status, raw)
	}
	if status, raw := h.send("DELETE", "/api/posts/untitled?version=1", "", nil); status != 200 {
		t.Fatalf("author DELETE empty draft = %d %s, want 200", status, raw)
	}
	if status, _ := h.send("GET", "/api/posts/untitled", "", nil); status != 404 {
		t.Errorf("GET after delete = %d, want 404", status)
	}
	last := h.audit()[len(h.audit())-1]
	if last.ActorName != "grace" || last.Action != "post.delete" || last.Target != "untitled" {
		t.Errorf("last audit entry = %+v, want grace post.delete untitled", last)
	}
}
