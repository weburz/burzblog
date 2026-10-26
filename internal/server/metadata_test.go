package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestPostMetadataAPI(t *testing.T) {
	h := newHarness(t, nil)
	h.login()

	resp, out := h.do("POST", "/api/posts", map[string]any{"title": "Meta", "slug": "meta", "tags": []string{"Go", "Web Dev"},
		"seoTitle": "SEO", "seoDescription": "About", "canonicalUrl": "https://example.com/meta"})
	if resp.StatusCode != 201 || out["author"] != "ada" || out["seoTitle"] != "SEO" || out["canonicalUrl"] != "https://example.com/meta" ||
		fmt.Sprint(out["tags"]) != "[go web-dev]" {
		t.Fatalf("create = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts", map[string]any{"title": "Plain", "slug": "plain"}); resp.StatusCode != 201 || fmt.Sprint(out["tags"]) != "[]" {
		t.Errorf("untagged create = %d %v, want tags []", resp.StatusCode, out)
	}
	h.do("POST", "/api/posts", map[string]any{"title": "Other", "slug": "other", "tags": []string{"go"}})

	resp, list := h.do("GET", "/api/posts?tag=go&sort=title", nil)
	items := list["items"].([]any)
	if resp.StatusCode != 200 || len(items) != 2 || list["total"] != 2.0 || items[0].(map[string]any)["slug"] != "meta" ||
		fmt.Sprint(items[0].(map[string]any)["tags"]) != "[go web-dev]" || items[0].(map[string]any)["author"] != "ada" {
		t.Errorf("tag list = %d %v", resp.StatusCode, list)
	}
	_, list = h.do("GET", "/api/posts?tag=go&sort=title&limit=1&offset=1", nil)
	if items := list["items"].([]any); len(items) != 1 || items[0].(map[string]any)["slug"] != "other" || list["total"] != 2.0 {
		t.Errorf("paged tag list = %v", list)
	}
	if _, list = h.do("GET", "/api/posts?tag=nope", nil); len(list["items"].([]any)) != 0 || list["total"] != 0.0 {
		t.Errorf("unknown tag list = %v", list)
	}

	save := func(version int, change map[string]any) (*http.Response, map[string]any) {
		body := map[string]any{"version": version, "slug": "meta", "title": "Meta", "tags": []string{"go", "web-dev"}, "author": "ada",
			"seoTitle": "SEO"}
		for k, v := range change {
			body[k] = v
		}
		return h.do("PUT", "/api/posts/meta", body)
	}
	var tooMany []string
	for i := 0; i < 25; i++ {
		tooMany = append(tooMany, fmt.Sprintf("t%d", i))
	}
	for name, change := range map[string]map[string]any{
		"too many tags":  {"tags": tooMany},
		"http canonical": {"canonicalUrl": "http://example.com"},
		"author newline": {"author": "a\nb"},
		"long seo title": {"seoTitle": strings.Repeat("x", 121)},
	} {
		if resp, out := save(1, change); resp.StatusCode != 422 || errCode(out) != "validation" {
			t.Errorf("%s = %d %v", name, resp.StatusCode, out)
		}
	}
	if resp, out := save(1, map[string]any{"tags": []string{"rust"}, "author": "grace", "seoTitle": "New"}); resp.StatusCode != 200 || out["version"] != 2.0 {
		t.Fatalf("save = %d %v", resp.StatusCode, out)
	}
	_, list = h.do("GET", "/api/posts?tag=rust", nil)
	if len(list["items"].([]any)) != 1 {
		t.Errorf("new tag not filterable: %v", list)
	}
	_, list = h.do("GET", "/api/posts?tag=web-dev", nil)
	if len(list["items"].([]any)) != 0 {
		t.Errorf("removed tag still filterable: %v", list)
	}

	// Revisions keep the earlier metadata and restoring brings it back.
	_, rev := h.do("GET", "/api/posts/meta/revisions/1", nil)
	old := rev["post"].(map[string]any)
	if fmt.Sprint(old["tags"]) != "[go web-dev]" || old["author"] != "ada" || old["seoTitle"] != "SEO" {
		t.Errorf("revision post = %v", old)
	}
	_, revs := h.do("GET", "/api/posts/meta/revisions", nil)
	if revs["items"].([]any)[0].(map[string]any)["settingsDigest"] == revs["current"].(map[string]any)["settingsDigest"] {
		t.Errorf("metadata change is invisible in the settings digest: %v", revs)
	}
	if resp, out := h.do("POST", "/api/posts/meta/revisions/1/restore", map[string]any{"version": 2}); resp.StatusCode != 200 ||
		fmt.Sprint(out["tags"]) != "[go web-dev]" || out["seoTitle"] != "SEO" || out["author"] != "ada" {
		t.Errorf("restore = %d %v", resp.StatusCode, out)
	}
}

func TestMetadataInMarkdownAndExport(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	doc := "---\ntitle: Tagged\nstatus: published\ntags:\n  - Go\n  - web dev\nauthor: Ada Lovelace\ncanonical: https://example.com/tagged\n" +
		"seo:\n  title: Tagged SEO\n  description: About tags\n  robots: noindex\nsitemap:\n  priority: 1\n---\n\nBody.\n"
	req, _ := http.NewRequest("PUT", h.ts.URL+"/api/posts/tagged/markdown", strings.NewReader(doc))
	req.Header.Set("If-None-Match", "*")
	req.Header.Set("Origin", h.ts.URL)
	req.Header.Set("X-CSRF-Token", h.csrf)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("create from markdown = %d", resp.StatusCode)
	}
	_, got := h.do("GET", "/api/posts/tagged", nil)
	if fmt.Sprint(got["tags"]) != "[go web-dev]" || got["author"] != "Ada Lovelace" || got["seoTitle"] != "Tagged SEO" ||
		got["seoDescription"] != "About tags" || got["canonicalUrl"] != "https://example.com/tagged" {
		t.Errorf("post = %v", got)
	}
	extra, _ := got["extra"].(string)
	if !strings.Contains(extra, "robots: noindex") || !strings.Contains(extra, "priority: 1") || strings.Contains(extra, "tags") {
		t.Errorf("extra = %q", extra)
	}
	_, md := h.do("GET", "/api/posts/tagged/markdown", nil)
	text, _ := md["_raw"].(string)
	for _, want := range []string{"tags:\n  - go\n  - web-dev", "author: Ada Lovelace", "canonical: https://example.com/tagged",
		"seo:\n  title: Tagged SEO\n  description: About tags\n  robots: noindex", "sitemap:"} {
		if !strings.Contains(text, want) {
			t.Errorf("markdown lacks %q:\n%s", want, text)
		}
	}

	// The export carries the same document.
	req, _ = http.NewRequest("GET", h.ts.URL+"/api/export.tar.gz", nil)
	req.Header.Set("Origin", h.ts.URL)
	resp, err = h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	files := readArchive(t, resp.Body)
	if !strings.Contains(files["tagged.md"], "canonical: https://example.com/tagged") || !strings.Contains(files["tagged.md"], "  - web-dev") {
		t.Errorf("export lacks the metadata:\n%s", files["tagged.md"])
	}
}

func TestMarkdownRejectsBadMetadata(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	for name, head := range map[string]string{
		"http canonical": "canonical: http://example.com\n",
		"tags map":       "tags: {a: 1}\n",
		"seo scalar":     "seo: text\n",
	} {
		req, _ := http.NewRequest("PUT", h.ts.URL+"/api/posts/bad/markdown", strings.NewReader("---\ntitle: Bad\n"+head+"---\n"))
		req.Header.Set("If-None-Match", "*")
		req.Header.Set("Origin", h.ts.URL)
		req.Header.Set("X-CSRF-Token", h.csrf)
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 422 {
			t.Errorf("%s = %d, want 422", name, resp.StatusCode)
		}
	}
}

func TestTagsEndpoint(t *testing.T) {
	h := newHarness(t, nil)
	if resp, _ := h.do("GET", "/api/tags", nil); resp.StatusCode != 401 {
		t.Errorf("tags without sign-in = %d, want 401", resp.StatusCode)
	}
	h.login()
	h.do("POST", "/api/posts", map[string]any{"title": "A", "slug": "a", "tags": []string{"go", "web"}})
	h.do("POST", "/api/posts", map[string]any{"title": "B", "slug": "b", "tags": []string{"go"}})
	resp, out := h.do("GET", "/api/tags", nil)
	items, _ := out["items"].([]any)
	if resp.StatusCode != 200 || fmt.Sprint(items) != "[map[count:2 tag:go] map[count:1 tag:web]]" {
		t.Errorf("tags = %d %v", resp.StatusCode, out)
	}
	token := h.issueToken("export")
	if resp, _ := h.as(token, "GET", "/api/tags"); resp.StatusCode != 403 {
		t.Errorf("export token on /api/tags = %d, want 403", resp.StatusCode)
	}
}
