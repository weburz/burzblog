package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

const legalExtra = `legal_references:
  - act: Companies Act, 2013
    sections: [2, 134]
    since: 2013-08-29
reviewed: 2026-01-02T03:04:05Z
`

func decodeJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("not JSON: %v: %s", err, raw)
	}
	return out
}

func TestContentPostExtra(t *testing.T) {
	c := newContentHarness(t)
	// The harness gives every post a sitemap mapping.
	resp, raw := c.get(c.export, "/api/content/posts/live")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d %s", resp.StatusCode, raw)
	}
	extra, _ := decodeJSON(t, raw)["extra"].(map[string]any)
	if got, _ := extra["sitemap"].(map[string]any); got["priority"] != float64(1) {
		t.Errorf("extra = %v", extra)
	}
	tagBefore := resp.Header.Get("ETag")

	if resp, out := c.do("PUT", "/api/posts/live", map[string]any{"version": 2, "title": "live", "body": "x", "extra": legalExtra}); resp.StatusCode != 200 {
		t.Fatalf("update = %d %v", resp.StatusCode, out)
	}
	resp, raw = c.get(c.export, "/api/content/posts/live")
	if resp.Header.Get("ETag") == tagBefore {
		t.Errorf("ETag %s did not change with extra", tagBefore)
	}
	extra, _ = decodeJSON(t, raw)["extra"].(map[string]any)
	refs, _ := extra["legal_references"].([]any)
	first, _ := refs[0].(map[string]any)
	if len(refs) != 1 || first["act"] != "Companies Act, 2013" || first["since"] != "2013-08-29" ||
		extra["reviewed"] != "2026-01-02T03:04:05Z" {
		t.Errorf("extra = %v", extra)
	}
	if sections, _ := first["sections"].([]any); len(sections) != 2 || sections[1] != float64(134) {
		t.Errorf("sections = %v", first["sections"])
	}
	if _, ok := extra["sitemap"]; ok {
		t.Errorf("replaced extra still has the old key: %v", extra)
	}

	// Empty extra is {} rather than missing or null.
	if resp, out := c.do("PUT", "/api/posts/live", map[string]any{"version": 3, "title": "live", "body": "x", "extra": ""}); resp.StatusCode != 200 {
		t.Fatalf("clear = %d %v", resp.StatusCode, out)
	}
	_, raw = c.get(c.export, "/api/content/posts/live")
	if !strings.Contains(raw, `"extra":{}`) {
		t.Errorf("empty extra is not {}: %s", raw)
	}
}

func TestContentExtraRoundTripsThroughMarkdown(t *testing.T) {
	c := newContentHarness(t)
	if resp, out := c.do("PUT", "/api/posts/live", map[string]any{"version": 2, "title": "live", "body": "x", "extra": legalExtra}); resp.StatusCode != 200 {
		t.Fatalf("update = %d %v", resp.StatusCode, out)
	}
	_, md := c.get(c.export, "/api/content/posts/live?format=markdown")
	doc, err := post.Decode(md)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Extra != legalExtra {
		t.Errorf("markdown extra = %q, want %q", doc.Extra, legalExtra)
	}
	_, raw := c.get(c.export, "/api/content/posts/live")
	want, err := post.ExtraValues(doc.Extra)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	got, _ := json.Marshal(decodeJSON(t, raw)["extra"])
	if !bytes.Equal(got, wantJSON) {
		t.Errorf("JSON extra = %s, markdown extra = %s", got, wantJSON)
	}
}

func TestContentListExtraIsOptIn(t *testing.T) {
	c := newContentHarness(t)
	if resp, out := c.do("PUT", "/api/posts/live", map[string]any{"version": 2, "title": "live", "body": "x", "extra": legalExtra}); resp.StatusCode != 200 {
		t.Fatalf("update = %d %v", resp.StatusCode, out)
	}
	_, plain := c.get(c.export, "/api/content/posts")
	if strings.Contains(plain, `"extra"`) || strings.Contains(plain, "legal_references") {
		t.Errorf("list leaks extra without include: %s", plain)
	}
	for _, path := range []string{"/api/content/posts?include=extra", "/api/content/posts?include=%20extra,", "/api/content/posts?q=live&include=extra"} {
		resp, raw := c.get(c.export, path)
		if resp.StatusCode != 200 {
			t.Fatalf("%s = %d %s", path, resp.StatusCode, raw)
		}
		items, _ := decodeJSON(t, raw)["items"].([]any)
		item, _ := items[0].(map[string]any)
		extra, _ := item["extra"].(map[string]any)
		if _, ok := extra["legal_references"]; !ok {
			t.Errorf("%s: extra = %v", path, item["extra"])
		}
	}
	// A post without extra gets {} on an include list.
	if resp, out := c.do("PUT", "/api/posts/live", map[string]any{"version": 3, "title": "live", "body": "x", "extra": ""}); resp.StatusCode != 200 {
		t.Fatalf("clear = %d %v", resp.StatusCode, out)
	}
	if _, raw := c.get(c.export, "/api/content/posts?include=extra"); !strings.Contains(raw, `"extra":{}`) {
		t.Errorf("empty extra is not {}: %s", raw)
	}
	// Drafts' extra never appears: only the published post is listed.
	if _, raw := c.get(c.export, "/api/content/posts?include=extra"); strings.Contains(raw, "sitemap") {
		t.Errorf("list holds extra of an unpublished post or stale extra: %s", raw)
	}
}

func TestContentListIncludeRejectsUnknown(t *testing.T) {
	c := newContentHarness(t)
	for _, q := range []string{"include=body", "include=extra,status", "include=Extra"} {
		resp, raw := c.get(c.export, "/api/content/posts?"+q)
		if resp.StatusCode != 422 || !strings.Contains(raw, `"validation"`) {
			t.Errorf("%s = %d %s, want 422 validation", q, resp.StatusCode, raw)
		}
	}
}

func TestContentListExtraETagTracksExtra(t *testing.T) {
	c := newContentHarness(t)
	resp, _ := c.get(c.export, "/api/content/posts?include=extra")
	before := resp.Header.Get("ETag")
	if resp, out := c.do("PUT", "/api/posts/live", map[string]any{"version": 2, "title": "live", "body": "x", "extra": legalExtra}); resp.StatusCode != 200 {
		t.Fatalf("update = %d %v", resp.StatusCode, out)
	}
	resp, _ = c.get(c.export, "/api/content/posts?include=extra", "If-None-Match", before)
	if resp.StatusCode != 200 || resp.Header.Get("ETag") == before {
		t.Errorf("stale tag = %d, ETag %s", resp.StatusCode, resp.Header.Get("ETag"))
	}
}

func TestExtraJSONUnreadableIsEmptyAndLogged(t *testing.T) {
	var logs bytes.Buffer
	s := &Server{log: slog.New(slog.NewTextHandler(&logs, nil))}
	r, _ := http.NewRequest("GET", "/api/content/posts", nil)
	got := s.extraJSON(r, "old-post", "a: [1, 2\n")
	if got == nil || len(got) != 0 {
		t.Errorf("extraJSON = %v, want an empty map", got)
	}
	if !strings.Contains(logs.String(), "old-post") || !strings.Contains(logs.String(), "unreadable extra") {
		t.Errorf("no warning logged: %q", logs.String())
	}
}

// uploadCover uploads a PNG and returns its file name.
func uploadCover(t *testing.T, h *harness, w, height int) string {
	t.Helper()
	resp, out := h.upload("c.png", "image/png", testPNG(t, w, height), nil)
	if resp.StatusCode != 201 {
		t.Fatalf("upload = %d %v", resp.StatusCode, out)
	}
	return out["name"].(string)
}

func TestContentCoverSize(t *testing.T) {
	h, _ := newMediaHarness(t, nil)
	h.login()
	token := h.issueToken("export")
	c := &contentHarness{harness: h, export: token}
	name := uploadCover(t, h, 40, 30)
	pdfResp, pdf := h.upload("d.pdf", "application/pdf", testPDF, nil)
	if pdfResp.StatusCode != 201 {
		t.Fatalf("pdf upload = %d %v", pdfResp.StatusCode, pdf)
	}
	other, _ := url.Parse(h.ts.URL)
	other.Host = "elsewhere.example"
	covers := map[string]string{
		"absolute": h.ts.URL + "/media/" + name,
		"query":    h.ts.URL + "/media/" + name + "?v=1",
		"foreign":  other.String() + "/media/" + name,
		"missing":  h.ts.URL + "/media/" + strings.Repeat("a", 32) + ".png",
		"pdf":      pdf["url"].(string),
		"plain":    "https://example.com/x.png",
		"nocover":  "",
		"traverse": h.ts.URL + "/media/../" + name,
	}
	for slug, cover := range covers {
		if resp, out := h.do("POST", "/api/posts", map[string]any{"title": slug, "slug": slug, "body": "b", "coverUrl": cover}); resp.StatusCode != 201 {
			t.Fatalf("create %s = %d %v", slug, resp.StatusCode, out)
		}
		if resp, out := h.do("POST", "/api/posts/"+slug+"/publish", map[string]any{"version": 1}); resp.StatusCode != 200 {
			t.Fatalf("publish %s = %d %v", slug, resp.StatusCode, out)
		}
	}
	sized := map[string]bool{"absolute": true, "query": true}

	_, raw := c.get(token, "/api/content/posts")
	items, _ := decodeJSON(t, raw)["items"].([]any)
	if len(items) != len(covers) {
		t.Fatalf("listed %d posts, want %d", len(items), len(covers))
	}
	for _, it := range items {
		item := it.(map[string]any)
		slug := item["slug"].(string)
		w, hasW := item["coverWidth"]
		ht, hasH := item["coverHeight"]
		if sized[slug] {
			if w != float64(40) || ht != float64(30) {
				t.Errorf("list %s: cover = %v x %v, want 40 x 30", slug, w, ht)
			}
		} else if hasW || hasH {
			t.Errorf("list %s: unexpected cover size %v x %v", slug, w, ht)
		}
	}
	for slug := range covers {
		resp, raw := c.get(token, "/api/content/posts/"+slug)
		got := decodeJSON(t, raw)
		if sized[slug] {
			if got["coverWidth"] != float64(40) || got["coverHeight"] != float64(30) {
				t.Errorf("single %s: %s", slug, raw)
			}
			if tag := resp.Header.Get("ETag"); tag != `"2-40x30"` {
				t.Errorf("single %s: ETag = %s", slug, tag)
			}
		} else if _, ok := got["coverWidth"]; ok || resp.Header.Get("ETag") != `"2"` {
			t.Errorf("single %s: unexpected cover size or ETag %s: %s", slug, resp.Header.Get("ETag"), raw)
		}
	}
	if resp, _ := c.get(token, "/api/content/posts/absolute", "If-None-Match", `"2-40x30"`); resp.StatusCode != 304 {
		t.Errorf("If-None-Match with the cover tag = %d, want 304", resp.StatusCode)
	}
}

func TestCoverMediaName(t *testing.T) {
	h := newHarness(t, nil)
	name := strings.Repeat("a", 32) + ".png"
	for cover, want := range map[string]string{
		h.ts.URL + "/media/" + name:                  name,
		strings.ToUpper(h.ts.URL) + "/media/" + name: name,
		"/media/" + name:                             name,
		"/media/" + name + "?v=2#top":                name,
		"https://example.com/media/" + name:          "",
		"//example.com/media/" + name:                "",
		h.ts.URL + "/media/" + name + "/x":           "",
		h.ts.URL + "/media/../" + name:               "",
		h.ts.URL + "/media/%2e%2e%2f" + name:         "",
		h.ts.URL + "/other/" + name:                  "",
		h.ts.URL + "/media/notes.html":               "",
		"mailto:a@b.c":                               "",
		"":                                           "",
		"%zz":                                        "",
	} {
		if got := h.srv.coverMediaName(cover); got != want {
			t.Errorf("coverMediaName(%q) = %q, want %q", cover, got, want)
		}
	}
}
