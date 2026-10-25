package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func TestRedirectsCommand(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ada")

	// Not signed in is refused like every other command.
	if _, errOut, code := e.run("", "redirects"); code != ExitAuth || !strings.Contains(errOut, "burzblog login") {
		t.Errorf("redirects before login: %d %q", code, errOut)
	}
	e.must("correct horse battery\n", "login", e.url, "-u", "ada")

	if out := e.must("", "redirects"); !strings.Contains(out, "No redirects") {
		t.Errorf("empty redirects = %q", out)
	}
	if out := e.must("", "redirects", "--json"); strings.TrimSpace(out) != `{"items":[]}` {
		t.Errorf("empty redirects --json = %q", out)
	}

	ctx := context.Background()
	p, err := e.store.CreatePost(ctx, &post.Post{Slug: "old-name", Title: "T", Status: post.Published}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.UpdatePost(ctx, "old-name", p.Version, "ada", func(p *post.Post) error { p.Slug = "new-name"; return nil }); err != nil {
		t.Fatal(err)
	}
	out := e.must("", "redirects")
	if !strings.Contains(out, "FROM") || !strings.Contains(out, "old-name") || !strings.Contains(out, "new-name") {
		t.Errorf("redirects = %q", out)
	}
	if out := e.must("", "redirects", "--json"); !strings.Contains(out, `{"items":[{"from":"old-name","to":"new-name"}]}`) {
		t.Errorf("redirects --json = %q", out)
	}

	// The export carries redirects.json beside the posts and does not count it.
	dir := t.TempDir()
	if out := e.must("", "export", "--out", dir); !strings.Contains(out, "Exported 1 published post to") {
		t.Errorf("export = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "redirects.json"))
	if err != nil || !strings.Contains(string(raw), `"from": "old-name"`) {
		t.Errorf("redirects.json = %q, %v", raw, err)
	}
	if _, _, code := e.run("", "redirects", "extra"); code != ExitUsage {
		t.Errorf("redirects with an argument: %d, want usage error", code)
	}
}
