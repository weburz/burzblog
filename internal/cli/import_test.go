package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

// writeFiles writes name -> content into a new temp directory.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func (e *env) getPost(slug string) *post.Post {
	e.t.Helper()
	p, err := e.store.GetPost(context.Background(), slug)
	if err != nil {
		e.t.Fatalf("GetPost(%s): %v", slug, err)
	}
	return p
}

func (e *env) hasPost(slug string) bool {
	_, err := e.store.GetPost(context.Background(), slug)
	return err == nil
}

const (
	importPublished = "---\ntitle: Live post\ndescription: Hello\nstatus: published\ntimestamps:\n  publishedOn: 2026-03-01T09:00:00Z\ncoverImage:\n  url: https://example.com/c.png\n  alt: A cover\ntags:\n  - go\n  - blog\nseo:\n  noindex: true\n---\n\nLive body.\n"
	importDraft     = "---\ntitle: A draft\n---\n\nDraft body.\n"
	importSlugged   = "---\ntitle: Named in file\nslug: custom-slug\n---\n\nBody.\n"
)

func TestImportDirectory(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	dir := writeFiles(t, map[string]string{
		"live-post.md": importPublished,
		"a-draft.md":   importDraft,
		"by-name.md":   importSlugged,
		"notes.txt":    "not markdown",
		"sub/deep.md":  importDraft,
	})

	out := e.must("", "import", dir)
	for _, want := range []string{"live-post", "a-draft", "custom-slug", "3 created, 0 updated, 0 unchanged, 0 skipped, 0 failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	live := e.getPost("live-post")
	if live.Status != post.Published || live.Title != "Live post" || live.Description != "Hello" ||
		live.CoverURL != "https://example.com/c.png" || live.CoverAlt != "A cover" || live.Body != "Live body.\n" ||
		live.PublishedOn == nil || live.PublishedOn.Year() != 2026 {
		t.Errorf("live post = %+v", live)
	}
	if strings.Join(live.Tags, ",") != "go,blog" {
		t.Errorf("tags = %v, want go and blog", live.Tags)
	}
	if !strings.Contains(live.Extra, "seo:") || !strings.Contains(live.Extra, "noindex: true") || strings.Contains(live.Extra, "tags") {
		t.Errorf("extra frontmatter = %q, want seo.noindex kept and tags moved out", live.Extra)
	}
	if p := e.getPost("a-draft"); p.Status != post.Draft {
		t.Errorf("draft status = %s", p.Status)
	}
	if p := e.getPost("custom-slug"); p.Title != "Named in file" {
		t.Errorf("slug from frontmatter: %+v", p)
	}
	if e.hasPost("by-name") {
		t.Error("the file name was used although the frontmatter names a slug")
	}
	if e.hasPost("deep") {
		t.Error("a sub-directory was read without --recursive")
	}

	out = e.must("", "import", dir, "--recursive")
	if !strings.Contains(out, "deep") || !strings.Contains(out, "1 created, 0 updated, 0 unchanged, 3 skipped") {
		t.Errorf("recursive output:\n%s", out)
	}
}

func TestImportSkipsExisting(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	e.must("", "new", "My post", "--no-edit")
	before := e.getPost("my-post")
	dir := writeFiles(t, map[string]string{"my-post.md": "---\ntitle: Replaced\n---\n\nNew body.\n"})

	out := e.must("", "import", dir)
	if !strings.Contains(out, "skipped my-post: already exists") {
		t.Errorf("output = %q", out)
	}
	if after := e.getPost("my-post"); after.Title != "My post" || after.Version != before.Version {
		t.Errorf("an existing post was changed: %+v", after)
	}

	j := decode(t, e.must("", "import", dir, "--json"))
	if j["skipped"] != float64(1) || j["created"] != float64(0) || j["failed"] != float64(0) {
		t.Errorf("json = %v", j)
	}
}

func TestImportUpdate(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	e.must("", "new", "My post", "--no-edit")
	before := e.getPost("my-post")
	dir := writeFiles(t, map[string]string{"my-post.md": "---\ntitle: Replaced\nstatus: published\n---\n\nNew body.\n"})

	out := e.must("", "import", dir, "--update")
	if !strings.Contains(out, "my-post updated (published)") || !strings.Contains(out, "1 updated") {
		t.Errorf("output = %q", out)
	}
	after := e.getPost("my-post")
	if after.Title != "Replaced" || after.Status != post.Published || after.Version != before.Version+1 {
		t.Errorf("updated post = %+v", after)
	}
	revs, err := e.store.Revisions(context.Background(), "my-post")
	if err != nil || len(revs) == 0 {
		t.Errorf("previous content not kept as a revision: %v %v", revs, err)
	}

	j := decode(t, e.must("", "import", dir, "--update", "--json"))
	files := j["files"].([]any)
	if j["unchanged"] != float64(1) || files[0].(map[string]any)["action"] != "unchanged" {
		t.Errorf("repeat import = %v", j)
	}
}

func TestImportDraftFlag(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	dir := writeFiles(t, map[string]string{"live-post.md": importPublished})
	e.must("", "import", dir, "--draft")
	p := e.getPost("live-post")
	if p.Status != post.Draft || !strings.Contains(p.Extra, "noindex: true") || p.CoverURL == "" {
		t.Errorf("--draft post = %+v", p)
	}
}

func TestImportDryRun(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	e.must("", "new", "Existing", "--no-edit")
	dir := writeFiles(t, map[string]string{
		"live-post.md": importPublished,
		"existing.md":  "---\ntitle: Existing changed\n---\n",
		"bad.md":       "no frontmatter",
	})

	out, _, code := e.run("", "import", dir, "--dry-run")
	if code != ExitValidation {
		t.Errorf("exit = %d, want %d", code, ExitValidation)
	}
	for _, want := range []string{"live-post would be created", "skipped existing: already exists", "Dry run: 1 created, 0 updated, 0 unchanged, 1 skipped, 1 failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if e.hasPost("live-post") {
		t.Error("--dry-run created a post")
	}
	if p := e.getPost("existing"); p.Title != "Existing" {
		t.Errorf("--dry-run changed a post: %+v", p)
	}
	out = e.must("", "import", filepath.Join(dir, "live-post.md"), "--dry-run", "--update", "--json")
	if j := decode(t, out); j["dryRun"] != true || j["created"] != float64(1) {
		t.Errorf("json = %v", j)
	}
}

func TestImportFailures(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	dir := writeFiles(t, map[string]string{
		"good.md":         importDraft,
		"no-title.md":     "---\ndescription: x\n---\n",
		"Bad Name.md":     importDraft,
		"bad-yaml.md":     "---\ntitle: [unclosed\n---\n",
		"control-\x1b.md": importDraft,
	})

	out, errOut, code := e.run("", "import", dir)
	if code != ExitValidation {
		t.Errorf("exit = %d, want %d", code, ExitValidation)
	}
	if !strings.Contains(out, "1 created, 0 updated, 0 unchanged, 0 skipped, 4 failed") {
		t.Errorf("output = %q", out)
	}
	if strings.Contains(errOut, "\x1b") || !strings.Contains(errOut, "Bad Name.md") || !strings.Contains(errOut, "4 of 5 files") {
		t.Errorf("stderr = %q", errOut)
	}
	e.getPost("good")

	// A missing path is a different kind of failure, so a mix is a plain error.
	_, _, code = e.run("", "import", filepath.Join(dir, "good.md"), filepath.Join(dir, "missing.md"), filepath.Join(dir, "no-title.md"))
	if code != ExitError {
		t.Errorf("mixed failures exit = %d, want %d", code, ExitError)
	}
	if _, _, code = e.run("", "import", filepath.Join(dir, "missing.md")); code != ExitError {
		t.Errorf("missing file exit = %d, want %d", code, ExitError)
	}
	txt := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(txt, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := e.run("", "import", txt); code != ExitValidation || !strings.Contains(errOut, ".md") {
		t.Errorf("non-markdown file: %d %q", code, errOut)
	}

	out, errOut, code = e.run("", "import", dir, "--json")
	doc := decode(t, out)
	if code != ExitValidation || doc["failed"] != float64(4) || !strings.Contains(errOut, `"validation"`) {
		t.Errorf("json: exit %d, %v, stderr %q", code, doc, errOut)
	}
	var codes int
	for _, f := range doc["files"].([]any) {
		if m := f.(map[string]any); m["action"] == "failed" && m["error"].(map[string]any)["code"] == "validation" {
			codes++
		}
	}
	if codes != 4 {
		t.Errorf("failed files carry %d validation codes: %v", codes, doc["files"])
	}

	if _, _, code := e.run("", "import"); code != ExitUsage {
		t.Errorf("no arguments exit = %d, want %d", code, ExitUsage)
	}
}

func TestImportNeedsSignIn(t *testing.T) {
	e := newEnv(t)
	dir := writeFiles(t, map[string]string{"a.md": importDraft})
	if _, _, code := e.run("", "import", dir); code != ExitAuth {
		t.Errorf("exit = %d, want %d", code, ExitAuth)
	}
}

func TestImportRoundTripFromExport(t *testing.T) {
	src := newEnv(t)
	src.signedIn()
	docs := writeFiles(t, map[string]string{
		"live-post.md": importPublished,
		"second.md":    "---\ntitle: Second\nstatus: published\n---\n\n## Heading\n\nText.\n",
	})
	src.must("", "push", filepath.Join(docs, "live-post.md"))
	src.must("", "push", filepath.Join(docs, "second.md"))

	out := filepath.Join(t.TempDir(), "export")
	src.must("", "export", "--out", out)

	dst := newEnv(t)
	dst.signedIn()
	dst.must("", "import", out)

	for _, slug := range []string{"live-post", "second"} {
		a, b := src.getPost(slug), dst.getPost(slug)
		if post.Encode(a) != post.Encode(b) {
			t.Errorf("%s differs after the round trip:\n%s\n---\n%s", slug, post.Encode(a), post.Encode(b))
		}
	}
}

func TestImportSlugMismatch(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	e.must("", "new", "Custom", "--no-edit")
	before := e.getPost("custom")
	dir := writeFiles(t, map[string]string{"other-name.md": "---\ntitle: Replaced\nslug: custom\n---\n\nNew body.\n"})

	// Without --update the frontmatter slug still picks the post, with a warning.
	out, errOut, code := e.run("", "import", dir)
	if code != 0 || !strings.Contains(errOut, "warning") || !strings.Contains(errOut, `"custom" differs from the file name "other-name"`) {
		t.Errorf("exit %d, out %q, stderr %q", code, out, errOut)
	}
	if !strings.Contains(out, "skipped custom: already exists") {
		t.Errorf("output = %q", out)
	}

	out, _, _ = e.run("", "import", dir, "--json")
	files := decode(t, out)["files"].([]any)
	if w, _ := files[0].(map[string]any)["warning"].(string); !strings.Contains(w, "differs from the file name") {
		t.Errorf("json files = %v", files)
	}

	// With --update the file is refused and nothing is overwritten.
	_, errOut, code = e.run("", "import", dir, "--update")
	if code != ExitValidation || !strings.Contains(errOut, "rename the file or fix the slug") {
		t.Errorf("--update: exit %d, stderr %q", code, errOut)
	}
	if after := e.getPost("custom"); after.Title != before.Title || after.Version != before.Version {
		t.Errorf("post was overwritten: %+v", after)
	}
	out, _, code = e.run("", "import", dir, "--update", "--json", "--dry-run")
	if code != ExitValidation || decode(t, out)["failed"] != float64(1) {
		t.Errorf("--update --dry-run: exit %d, out %q", code, out)
	}

	// A matching slug raises no warning.
	dir = writeFiles(t, map[string]string{"custom-slug.md": importSlugged})
	if _, errOut, _ := e.run("", "import", dir); strings.Contains(errOut, "warning") {
		t.Errorf("unexpected warning: %q", errOut)
	}
}
