package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataThroughFiles(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ada")
	e.must("correct horse battery\n", "login", e.url, "-u", "ada")
	dir := t.TempDir()

	// New posts are credited to the signed-in account and the author stays
	// when --body-file saves the first body.
	e.must("", "new", "Tagged post", "--no-edit", "--body-file", writeFiles(t, map[string]string{"b.md": "Body.\n"})+"/b.md")
	if p := e.getPost("tagged-post"); p.Author != "ada" {
		t.Errorf("author = %q, want ada", p.Author)
	}

	file := filepath.Join(dir, "tagged-post.md")
	e.must("", "pull", "tagged-post", "-o", file)
	raw, _ := os.ReadFile(file)
	edited := strings.Replace(string(raw), "author: ada\n", "author: Ada L.\ntags: [Go, Web Dev]\ncanonical: https://example.com/t\nseo:\n  title: T\n  robots: noindex\n", 1)
	if edited == string(raw) {
		t.Fatalf("pulled file has no author line:\n%s", raw)
	}
	os.WriteFile(file, []byte(edited), 0o644)

	if out, errOut, code := e.run("", "diff", file); code == 0 || !strings.Contains(out+errOut, "+tags: [Go, Web Dev]") {
		t.Errorf("diff does not show the tags: %d %q %q", code, out, errOut)
	}
	e.must("", "push", file)
	p := e.getPost("tagged-post")
	if strings.Join(p.Tags, ",") != "go,web-dev" || p.Author != "Ada L." || p.SEOTitle != "T" || p.CanonicalURL != "https://example.com/t" ||
		!strings.Contains(p.Extra, "robots: noindex") {
		t.Errorf("pushed post = %+v", p)
	}
	// A bad value is refused with a message that names the field.
	bad := strings.Replace(edited, "https://example.com/t", "http://example.com/t", 1)
	os.WriteFile(file, []byte(bad), 0o644)
	if _, errOut, code := e.run("", "push", file); code == 0 || !strings.Contains(errOut, "canonical URL") {
		t.Errorf("push with http canonical: %d %q", code, errOut)
	}

	e.must("", "new", "Untagged", "--no-edit")
	if out := e.must("", "list", "--tag", "Web Dev"); !strings.Contains(out, "tagged-post") || strings.Contains(out, "untagged") ||
		!strings.Contains(out, "go,web-dev") {
		t.Errorf("list --tag = %q", out)
	}
	if out := e.must("", "list", "--tag", "nothing"); !strings.Contains(out, "No posts match.") {
		t.Errorf("list --tag nothing = %q", out)
	}
	if out := e.must("", "--json", "list", "--tag", "go"); !strings.Contains(out, `"tags":["go","web-dev"]`) {
		t.Errorf("list --json lacks tags: %q", out)
	}

	// Import carries the fields and keeps unknown seo keys.
	src := writeFiles(t, map[string]string{"imported.md": "---\ntitle: Imported\ntags: [Rust]\nauthor: Grace\nseo:\n  description: D\n  noindex: true\n---\n\nB.\n"})
	e.must("", "import", src)
	ip, err := e.store.GetPost(context.Background(), "imported")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ip.Tags, ",") != "rust" || ip.Author != "Grace" || ip.SEODescription != "D" || !strings.Contains(ip.Extra, "noindex: true") {
		t.Errorf("imported post = %+v", ip)
	}
}
