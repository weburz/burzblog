package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/store"
)

func TestFeaturedPostsAPI(t *testing.T) {
	c := newContentHarness(t)
	// "live" is published at version 2; feature it through a normal save.
	resp, out := c.do("PUT", "/api/posts/live", map[string]any{"version": 2, "title": "live", "body": "text", "featured": true})
	if resp.StatusCode != 200 || out["featured"] != true || out["version"] != 3.0 {
		t.Fatalf("feature = %d %v", resp.StatusCode, out)
	}
	// A save that leaves featured out keeps it, for clients that predate it.
	resp, out = c.do("PUT", "/api/posts/live", map[string]any{"version": 3, "title": "live", "body": "more text"})
	if resp.StatusCode != 200 || out["featured"] != true {
		t.Fatalf("save without featured = %d %v", resp.StatusCode, out)
	}
	// A second published post stays plain; the draft is featured but private.
	if resp, out := c.do("POST", "/api/posts/draft/publish", map[string]any{"version": 1}); resp.StatusCode != 200 {
		t.Fatalf("publish draft = %d %v", resp.StatusCode, out)
	}
	if resp, out := c.do("PUT", "/api/posts/later", map[string]any{"version": 2, "title": "later", "featured": true}); resp.StatusCode != 200 {
		t.Fatalf("feature scheduled = %d %v", resp.StatusCode, out)
	}

	for path, want := range map[string]struct {
		total  string
		slugs  []string
		reject []string
	}{
		"/api/content/posts?featured=true":  {`"total":1`, []string{`"slug":"live"`, `"featured":true`}, []string{`"slug":"draft"`, `"slug":"later"`}},
		"/api/content/posts?featured=false": {`"total":1`, []string{`"slug":"draft"`, `"featured":false`}, []string{`"slug":"live"`, `"slug":"later"`}},
		"/api/content/posts":                {`"total":2`, []string{`"slug":"live"`, `"slug":"draft"`}, []string{`"slug":"later"`}},
	} {
		resp, raw := c.get(c.export, path)
		if resp.StatusCode != 200 || !strings.Contains(raw, want.total) {
			t.Errorf("%s = %d %s", path, resp.StatusCode, raw)
			continue
		}
		for _, w := range want.slugs {
			if !strings.Contains(raw, w) {
				t.Errorf("%s lacks %s: %s", path, w, raw)
			}
		}
		for _, w := range want.reject {
			if strings.Contains(raw, w) {
				t.Errorf("%s shows %s: %s", path, w, raw)
			}
		}
	}
	if _, raw := c.get(c.export, "/api/content/posts/live"); !strings.Contains(raw, `"featured":true`) {
		t.Errorf("single post lacks featured: %s", raw)
	}
	// The filter does not change the order: newest published first.
	_, raw := c.get(c.export, "/api/content/posts")
	if strings.Index(raw, `"slug":"draft"`) > strings.Index(raw, `"slug":"live"`) {
		t.Errorf("list is not newest first: %s", raw)
	}

	for _, bad := range []string{"yes", "1", "TRUE", "maybe", "t"} {
		for _, path := range []string{"/api/content/posts?featured=", "/api/posts?featured="} {
			var resp *http.Response
			var raw string
			if strings.HasPrefix(path, "/api/content") {
				resp, raw = c.get(c.export, path+bad)
			} else {
				var out map[string]any
				resp, out = c.do("GET", path+bad, nil)
				raw = errCode(out)
			}
			if resp.StatusCode != 422 || !strings.Contains(raw, "validation") {
				t.Errorf("%s%s = %d %s, want 422 validation", path, bad, resp.StatusCode, raw)
			}
		}
	}

	// The panel list filters too and flags featured posts.
	resp, list := c.do("GET", "/api/posts?featured=true", nil)
	items, _ := list["items"].([]any)
	if resp.StatusCode != 200 || len(items) != 2 {
		t.Fatalf("panel featured list = %d %v", resp.StatusCode, list)
	}
	for _, it := range items {
		if it.(map[string]any)["featured"] != true {
			t.Errorf("panel item not featured: %v", it)
		}
	}
	if _, list = c.do("GET", "/api/posts?featured=false", nil); len(list["items"].([]any)) != 1 {
		t.Errorf("panel not-featured list = %v", list)
	}
}

func TestFeaturedRoundTripsAsMarkdown(t *testing.T) {
	c := newContentHarness(t)
	c.do("PUT", "/api/posts/live", map[string]any{"version": 2, "title": "live", "featured": true})
	_, raw := c.get(c.export, "/api/content/posts/live?format=markdown")
	if !strings.Contains(raw, "\nfeatured: true\n") {
		t.Fatalf("markdown lacks featured:\n%s", raw)
	}
	pull := func() string {
		req, _ := http.NewRequest("GET", c.ts.URL+"/api/posts/live/markdown", nil)
		req.Header.Set("Authorization", "Bearer "+c.write)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	doc := pull()
	if !strings.Contains(doc, "featured: true") {
		t.Fatalf("pull lacks featured:\n%s", doc)
	}
	push := func(version, body string) *http.Response {
		req, _ := http.NewRequest("PUT", c.ts.URL+"/api/posts/live/markdown", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+c.write)
		req.Header.Set("If-Match", version)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	// Pushing the pulled file back changes nothing; removing the key unfeatures.
	if resp := push("3", doc); resp.StatusCode != 200 {
		t.Fatalf("push = %d", resp.StatusCode)
	}
	if _, out := c.do("GET", "/api/posts/live", nil); out["featured"] != true || out["version"] != 3.0 {
		t.Errorf("after pushing the same file = %v", out)
	}
	if resp := push("3", strings.Replace(doc, "featured: true\n", "", 1)); resp.StatusCode != 200 {
		t.Fatalf("push without the key = %d", resp.StatusCode)
	}
	if _, out := c.do("GET", "/api/posts/live", nil); out["featured"] != false || out["version"] != 4.0 {
		t.Errorf("after pushing without the key = %v", out)
	}
	if resp := push("4", strings.Replace(doc, "featured: true", "featured: sure", 1)); resp.StatusCode != 422 {
		t.Errorf("push featured: sure = %d, want 422", resp.StatusCode)
	}
	// History shows the change: the revisions' settings digests differ.
	_, revs := c.do("GET", "/api/posts/live/revisions", nil)
	items := revs["items"].([]any)
	if len(items) < 2 || items[0].(map[string]any)["settingsDigest"] == revs["current"].(map[string]any)["settingsDigest"] {
		t.Errorf("revisions do not show the featured change: %v", revs)
	}
}

func TestTagLabelsAPI(t *testing.T) {
	c := newContentHarness(t)
	for slug, tags := range map[string][]string{"live": {"constitutional-law", "go"}, "draft": {"draft-only"}} {
		_, out := c.do("GET", "/api/posts/"+slug, nil)
		body := map[string]any{"version": out["version"], "title": slug, "tags": tags}
		if resp, out := c.do("PUT", "/api/posts/"+slug, body); resp.StatusCode != 200 {
			t.Fatalf("tag %s = %d %v", slug, resp.StatusCode, out)
		}
	}
	_, before := c.get(c.export, "/api/content/posts/live")
	if !strings.Contains(before, `"tagLabels":{"constitutional-law":"Constitutional Law","go":"Go"}`) ||
		!strings.Contains(before, `"tags":["constitutional-law","go"]`) {
		t.Fatalf("default labels = %s", before)
	}
	resp, _ := c.get(c.export, "/api/content/posts/live")
	oldTag := resp.Header.Get("ETag")
	resp, tagsETag := c.get(c.export, "/api/content/tags")
	tagsTag := resp.Header.Get("ETag")
	_, listBefore := c.get(c.export, "/api/content/posts")
	listResp, _ := c.get(c.export, "/api/content/posts")
	listTag := listResp.Header.Get("ETag")
	if !strings.Contains(tagsETag, `{"count":1,"label":"Constitutional Law","tag":"constitutional-law"}`) ||
		strings.Contains(tagsETag, "draft-only") || !strings.Contains(listBefore, `"tagLabels"`) {
		t.Errorf("tags = %s", tagsETag)
	}

	// Set a label.
	resp, out := c.do("PUT", "/api/tags/constitutional-law/label", map[string]any{"label": "  Verfassungsrecht  "})
	if resp.StatusCode != 200 || out["label"] != "Verfassungsrecht" || out["custom"] != true || out["tag"] != "constitutional-law" {
		t.Fatalf("set label = %d %v", resp.StatusCode, out)
	}
	_, after := c.get(c.export, "/api/content/posts/live")
	if !strings.Contains(after, `"constitutional-law":"Verfassungsrecht"`) || !strings.Contains(after, `"go":"Go"`) ||
		!strings.Contains(after, `"tags":["constitutional-law","go"]`) {
		t.Errorf("labelled post = %s", after)
	}
	// Every ETag that carries a label changes with it, and the old ones no
	// longer match.
	resp, _ = c.get(c.export, "/api/content/posts/live")
	newTag := resp.Header.Get("ETag")
	if newTag == oldTag {
		t.Errorf("post ETag %s did not change with the label", newTag)
	}
	if resp, _ := c.get(c.export, "/api/content/posts/live", "If-None-Match", oldTag); resp.StatusCode != 200 {
		t.Errorf("old post ETag = %d, want 200", resp.StatusCode)
	}
	if resp, _ := c.get(c.export, "/api/content/posts/live", "If-None-Match", newTag); resp.StatusCode != 304 {
		t.Errorf("new post ETag = %d, want 304", resp.StatusCode)
	}
	if resp, raw := c.get(c.export, "/api/content/tags", "If-None-Match", tagsTag); resp.StatusCode != 200 || !strings.Contains(raw, "Verfassungsrecht") {
		t.Errorf("tags with the old ETag = %d %s", resp.StatusCode, raw)
	}
	if resp, _ := c.get(c.export, "/api/content/posts", "If-None-Match", listTag); resp.StatusCode != 200 {
		t.Errorf("list with the old ETag = %d, want 200", resp.StatusCode)
	}
	// Changing the label again, and clearing it, change the tag again.
	c.do("PUT", "/api/tags/constitutional-law/label", map[string]any{"label": "Droit constitutionnel"})
	resp, _ = c.get(c.export, "/api/content/posts/live")
	if resp.Header.Get("ETag") == newTag {
		t.Error("post ETag did not change when the label changed again")
	}
	resp, out = c.do("DELETE", "/api/tags/constitutional-law/label", nil)
	if resp.StatusCode != 200 || out["label"] != "Constitutional Law" || out["custom"] != false {
		t.Fatalf("clear label = %d %v", resp.StatusCode, out)
	}
	resp, cleared := c.get(c.export, "/api/content/posts/live")
	if resp.Header.Get("ETag") != oldTag || cleared != before {
		t.Errorf("cleared post = ETag %s, want %s again; %s", resp.Header.Get("ETag"), oldTag, cleared)
	}
	if resp, _ := c.do("DELETE", "/api/tags/constitutional-law/label", nil); resp.StatusCode != 200 {
		t.Errorf("clearing a cleared label = %d, want 200", resp.StatusCode)
	}

	// The panel list shows labels and whether they are custom.
	c.do("PUT", "/api/tags/go/label", map[string]any{"label": "Golang"})
	_, list := c.do("GET", "/api/tags", nil)
	got := map[string]map[string]any{}
	for _, it := range list["items"].([]any) {
		m := it.(map[string]any)
		got[m["tag"].(string)] = m
	}
	if got["go"]["label"] != "Golang" || got["go"]["custom"] != true || got["draft-only"]["label"] != "Draft Only" || got["draft-only"]["custom"] != false {
		t.Errorf("panel tags = %v", got)
	}
}

func TestTagLabelsOnlyReachReadersForPublishedPosts(t *testing.T) {
	c := newContentHarness(t)
	_, out := c.do("GET", "/api/posts/draft", nil)
	c.do("PUT", "/api/posts/draft", map[string]any{"version": out["version"], "title": "draft", "tags": []string{"secret-plan"}})
	if resp, out := c.do("PUT", "/api/tags/secret-plan/label", map[string]any{"label": "Top Secret"}); resp.StatusCode != 200 {
		t.Fatalf("label = %d %v", resp.StatusCode, out)
	}
	for _, path := range []string{"/api/content/tags", "/api/content/posts", "/api/content/posts/live", "/api/content/posts/draft"} {
		if _, raw := c.get(c.export, path); strings.Contains(raw, "Top Secret") || strings.Contains(raw, "secret-plan") {
			t.Errorf("%s leaks the unpublished tag: %s", path, raw)
		}
	}
	if resp, _ := c.get("", "/api/content/tags"); resp.StatusCode != 401 {
		t.Errorf("anonymous tags = %d, want 401", resp.StatusCode)
	}
}

func TestTagLabelValidation(t *testing.T) {
	c := newContentHarness(t)
	_, out := c.do("GET", "/api/posts/live", nil)
	c.do("PUT", "/api/posts/live", map[string]any{"version": out["version"], "title": "live", "tags": []string{"go"}})
	for name, tc := range map[string]struct {
		method, path string
		body         any
		status       int
	}{
		"empty label":      {"PUT", "/api/tags/go/label", map[string]any{"label": "  "}, 422},
		"long label":       {"PUT", "/api/tags/go/label", map[string]any{"label": strings.Repeat("x", 61)}, 422},
		"multiline label":  {"PUT", "/api/tags/go/label", map[string]any{"label": "a\nb"}, 422},
		"control label":    {"PUT", "/api/tags/go/label", map[string]any{"label": "a\x1bb"}, 422},
		"unknown field":    {"PUT", "/api/tags/go/label", map[string]any{"name": "Go"}, 400},
		"unnormalized tag": {"PUT", "/api/tags/Machine%20Learning/label", map[string]any{"label": "ML"}, 422},
		"unnormalized del": {"DELETE", "/api/tags/Go/label", nil, 422},
		"unused tag":       {"PUT", "/api/tags/unused/label", map[string]any{"label": "Unused"}, 404},
		"unicode label ok": {"PUT", "/api/tags/go/label", map[string]any{"label": "गो भाषा"}, 200},
		"max-length label": {"PUT", "/api/tags/go/label", map[string]any{"label": strings.Repeat("é", 60)}, 200},
	} {
		if resp, out := c.do(tc.method, tc.path, tc.body); resp.StatusCode != tc.status {
			t.Errorf("%s = %d %v, want %d", name, resp.StatusCode, out, tc.status)
		}
	}
	// Labels are written with Origin and CSRF like every browser change.
	req, _ := http.NewRequest("PUT", c.ts.URL+"/api/tags/go/label", strings.NewReader(`{"label":"Hijack"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-CSRF-Token", c.csrf)
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("label from a foreign origin = %d, want a refusal", resp.StatusCode)
	}
	if labels, _ := c.store.TagLabels(context.Background()); labels["go"] == "Hijack" {
		t.Error("a foreign origin changed a label")
	}
	// An export token cannot edit labels.
	if resp, _ := c.as(c.export, "PUT", "/api/tags/go/label"); resp.StatusCode < 400 {
		t.Errorf("export token on the label route = %d", resp.StatusCode)
	}
}

func TestTagLabelEditsAreAuditedAndAdminOnly(t *testing.T) {
	h := newHarness(t, nil)
	withRoles(t, h)
	seedPosts(t, h)

	h.signInAs("grace", false)
	if status, raw := h.send("PUT", "/api/tags/news/label", `{"label":"Nope"}`, nil); status != 403 || !strings.Contains(raw, `"code":"role"`) {
		t.Errorf("author set label = %d %s, want 403 role", status, raw)
	}
	if status, raw := h.send("DELETE", "/api/tags/news/label", "", nil); status != 403 || !strings.Contains(raw, `"code":"role"`) {
		t.Errorf("author clear label = %d %s, want 403 role", status, raw)
	}
	if len(h.audit()) != 1 { // only grace's sign-in
		t.Errorf("a refused label edit was audited: %+v", h.audit())
	}
	if labels, _ := h.store.TagLabels(context.Background()); len(labels) != 0 {
		t.Errorf("an author changed labels: %v", labels)
	}

	h.signInAs("ada", false)
	h.do("PUT", "/api/tags/news/label", map[string]any{"label": "The News"})
	h.do("DELETE", "/api/tags/news/label", nil)
	var got []store.AuditEntry
	for _, e := range h.audit() {
		if e.Action == "tag.label" {
			got = append(got, e)
		}
	}
	if len(got) != 2 || got[0].ActorName != "ada" || got[0].Target != "news" || got[1].Target != "news (cleared)" {
		t.Errorf("audit rows = %+v, want set then clear by ada", got)
	}
	for _, e := range got {
		if strings.Contains(e.Target, "The News") {
			t.Errorf("audit target %q holds the label text", e.Target)
		}
	}
}
