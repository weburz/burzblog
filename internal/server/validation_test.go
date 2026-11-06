package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func detail(out map[string]any) string {
	if e, ok := out["error"].(map[string]any); ok {
		d, _ := e["detail"].(string)
		return d
	}
	return ""
}

func TestPostDatesAreSane(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	for name, body := range map[string]map[string]any{
		"year 3000":        {"title": "A", "publishedOn": "3000-01-01T00:00:00Z"},
		"year 1":           {"title": "A", "publishedOn": "0001-01-01T00:00:00Z"},
		"updated too late": {"title": "A", "updatedOn": "3000-01-01T00:00:00Z"},
		"updated first":    {"title": "A", "publishedOn": "2025-06-01T00:00:00Z", "updatedOn": "2025-05-01T00:00:00Z"},
	} {
		body["slug"] = "dates"
		resp, out := h.do("POST", "/api/posts", body)
		if resp.StatusCode != 422 || errCode(out) != "validation" || !strings.Contains(detail(out), "date") {
			t.Errorf("%s = %d %v", name, resp.StatusCode, out)
		}
	}
	resp, out := h.do("POST", "/api/posts", map[string]any{"slug": "dates", "title": "A",
		"publishedOn": "2025-06-01T00:00:00Z", "updatedOn": "2025-06-02T00:00:00Z"})
	if resp.StatusCode != 201 {
		t.Fatalf("sane dates = %d %v", resp.StatusCode, out)
	}
	resp, out = h.do("PUT", "/api/posts/dates", map[string]any{"version": 1, "title": "A", "publishedOn": "3000-01-01T00:00:00Z"})
	if resp.StatusCode != 422 || !strings.Contains(detail(out), "published date") {
		t.Errorf("update to year 3000 = %d %v", resp.StatusCode, out)
	}
}

func TestCanonicalURLWithSpaceIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	resp, out := h.do("POST", "/api/posts", map[string]any{"slug": "c", "title": "C", "canonicalUrl": "https://x.test/a b"})
	if resp.StatusCode != 422 || !strings.Contains(detail(out), "canonical URL") {
		t.Errorf("canonical with space = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts", map[string]any{"slug": "c", "title": "C", "canonicalUrl": "https://x.test/a%20b"}); resp.StatusCode != 201 {
		t.Errorf("encoded canonical = %d %v", resp.StatusCode, out)
	}
}

func TestUploadAltIsCleaned(t *testing.T) {
	h, _ := newMediaHarness(t, nil)
	h.login()
	resp, out := h.upload("a.png", "image/png", testPNG(t, 2, 2), map[string]string{"alt": " A red\ndot\x1b[0m‮evil⁦ "})
	if resp.StatusCode != 201 || out["alt"] != "A reddot[0mevil" {
		t.Fatalf("upload = %d %v", resp.StatusCode, out)
	}
	// The same cleaning applies to an edit.
	resp, out = h.do("PATCH", "/api/media/"+jsonNumber(out["id"]), map[string]any{"alt": "a​b‮c"})
	if resp.StatusCode != 200 || out["alt"] != "abc" {
		t.Errorf("patch = %d %v", resp.StatusCode, out)
	}
	// Joiners that real scripts need stay.
	if got := cleanAlt("क्‍ष \U0001F468‍\U0001F469"); got != "क्‍ष \U0001F468‍\U0001F469" {
		t.Errorf("cleanAlt dropped a joiner: %q", got)
	}
	if got := cleanOriginalName("dir/a‮gnp.exe"); got != "agnp.exe" {
		t.Errorf("cleanOriginalName = %q", got)
	}
}

func TestDecodeErrorsAreInPlainEnglish(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	cases := []struct{ name, body, want string }{
		{"wrong type", `{"version":"x","title":"A"}`, `The field "version" must be a whole number.`},
		{"number for text", `{"title":5}`, `The field "title" must be text.`},
		{"bad date", `{"title":"A","publishedOn":"yesterday"}`, `must look like 2026-11-03T09:00:00Z`},
		{"unknown field", `{"title":"A","nope":1}`, `"nope"`},
		{"syntax", `{"title" 1}`, `not valid JSON`},
		{"cut off", `{"title":`, `ends too early`},
		{"empty", ``, `empty`},
	}
	for _, c := range cases {
		status, raw := h.send("POST", "/api/posts", c.body, map[string]string{"Content-Type": "application/json"})
		d := detail(decodeJSON(t, raw))
		if status != 400 || !strings.Contains(d, c.want) {
			t.Errorf("%s = %d %q; want it to contain %q", c.name, status, d, c.want)
		}
		for _, leak := range []string{"Go struct", "postInput", "int64", "json:", "cannot unmarshal"} {
			if strings.Contains(d, leak) {
				t.Errorf("%s leaks %q: %q", c.name, leak, d)
			}
		}
	}
}

func TestMultipartErrorsDoNotLeakParserText(t *testing.T) {
	h, _ := newMediaHarness(t, nil)
	h.login()
	body := "--b\r\nbad header line\r\n\r\ndata\r\n--b--\r\n"
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/media", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	req.Header.Set("Origin", h.ts.URL)
	req.Header.Set("X-CSRF-Token", h.csrf)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	got := string(raw)
	if resp.StatusCode != 400 || strings.Contains(got, "bad header line") || strings.Contains(got, "MIME") {
		t.Errorf("malformed multipart = %d %s", resp.StatusCode, got)
	}
	if !strings.Contains(got, "malformed") {
		t.Errorf("no plain explanation: %s", got)
	}
}

func TestActionsAndRestoreNeedAVersion(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	if resp, out := h.do("POST", "/api/posts", map[string]any{"slug": "v", "title": "V", "body": "one"}); resp.StatusCode != 201 {
		t.Fatalf("create = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("PUT", "/api/posts/v", map[string]any{"version": 1, "title": "V", "body": "two"}); resp.StatusCode != 200 {
		t.Fatalf("save = %d %v", resp.StatusCode, out)
	}
	for _, path := range []string{"/api/posts/v/publish", "/api/posts/v/archive", "/api/posts/v/unschedule", "/api/posts/v/revisions/1/restore"} {
		resp, out := h.do("POST", path, map[string]any{})
		if resp.StatusCode != 400 || errCode(out) != "bad_request" || !strings.Contains(out["_raw"].(string), "Send the version you started from") {
			t.Errorf("%s without version = %d %v", path, resp.StatusCode, out)
		}
	}
	// A stale version still conflicts.
	if resp, out := h.do("POST", "/api/posts/v/publish", map[string]any{"version": 1}); resp.StatusCode != 409 {
		t.Errorf("stale version = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts/v/publish", map[string]any{"version": 2}); resp.StatusCode != 200 {
		t.Errorf("publish = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts/v/revisions/1/restore", map[string]any{"version": 3}); resp.StatusCode != 200 {
		t.Errorf("restore = %d %v", resp.StatusCode, out)
	}
}

// A writer deleting a draft that looks empty but once held writing is told
// it is an admin's call, not to archive it first.
func TestWriterDeleteOfOnceWrittenDraftIsForbidden(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	withRoles(t, h)
	ctx := context.Background()
	p, err := h.store.CreatePost(ctx, &post.Post{Slug: "wiped", Title: "Wrote this", Body: "words", Status: post.Draft}, "grace")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.UpdatePost(ctx, "wiped", p.Version, "grace", func(p *post.Post) error {
		p.Title, p.Body = "", ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.CreatePost(ctx, &post.Post{Slug: "blank", Status: post.Draft}, "grace"); err != nil {
		t.Fatal(err)
	}
	h.signInAs("grace", false)
	resp, out := h.do("DELETE", "/api/posts/wiped?version=2", nil)
	if resp.StatusCode != 403 || errCode(out) != "role" {
		t.Errorf("writer delete of once-written draft = %d %v", resp.StatusCode, out)
	}
	// A draft that never held anything is still the writer's to delete.
	if resp, out := h.do("DELETE", "/api/posts/blank?version=1", nil); resp.StatusCode != 200 {
		t.Errorf("writer delete of blank draft = %d %v", resp.StatusCode, out)
	}
	// An admin still gets the archive-first guidance.
	h.signInAs("ada", false)
	if resp, out := h.do("DELETE", "/api/posts/wiped?version=2", nil); resp.StatusCode != 409 {
		t.Errorf("admin delete = %d %v", resp.StatusCode, out)
	}
}
