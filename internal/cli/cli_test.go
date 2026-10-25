package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/server"
	"github.com/weburz/burzblog/internal/store"
)

type env struct {
	t      *testing.T
	url    string
	store  *store.Store
	vars   map[string]string
	config string
	cache  string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	data := t.TempDir()
	st, err := store.Open(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc, _ := auth.New(st)
	ts := httptest.NewUnstartedServer(nil)
	ts.Start()
	srv, err := server.New(server.Config{Origin: ts.URL, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, st, svc)
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = srv.Handler()
	t.Cleanup(ts.Close)
	return &env{t: t, url: ts.URL, store: st, vars: map[string]string{"BURZBLOG_DATA": data},
		config: t.TempDir(), cache: t.TempDir()}
}

// run executes the CLI with stdin and returns stdout, stderr and exit code.
func (e *env) run(stdin string, args ...string) (string, string, int) {
	e.t.Helper()
	var out, errOut bytes.Buffer
	a := &App{
		In: strings.NewReader(stdin), Out: &out, Err: &errOut,
		ConfigDir: e.config, CacheDir: e.cache, Version: "test",
		Getenv: func(k string) string { return e.vars[k] },
	}
	code := a.Execute(args)
	return out.String(), errOut.String(), code
}

func (e *env) must(stdin string, args ...string) string {
	e.t.Helper()
	out, errOut, code := e.run(stdin, args...)
	if code != 0 {
		e.t.Fatalf("burzblog %s failed:\n%s%s", strings.Join(args, " "), out, errOut)
	}
	return out
}

// editor installs a fake $EDITOR that runs a sed script on the file.
func (e *env) editor(script string) {
	e.t.Helper()
	path := filepath.Join(e.t.TempDir(), "editor.sh")
	body := "#!/bin/sh\nset -e\nsed -i '" + script + "' \"$1\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		e.t.Fatal(err)
	}
	e.vars["EDITOR"] = path
}

func TestTerminalWorkflow(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ada")
	if out := e.must("", "user", "list"); !strings.Contains(out, "ada") {
		t.Errorf("user list = %q", out)
	}

	if _, errOut, code := e.run("", "list"); code == 0 || !strings.Contains(errOut, "burzblog login") {
		t.Errorf("list before login: %d %q", code, errOut)
	}
	if _, errOut, code := e.run("wrong password\n", "login", e.url, "-u", "ada"); code == 0 || !strings.Contains(errOut, "do not match") {
		t.Errorf("bad login: %d %q", code, errOut)
	}
	out := e.must("correct horse battery\n", "login", e.url, "-u", "ada")
	if !strings.Contains(out, "Signed in") {
		t.Errorf("login = %q", out)
	}
	info, err := os.Stat(filepath.Join(e.config, "config.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("config file: %v %v", info, err)
	}
	if out := e.must("", "whoami"); !strings.Contains(out, "ada") {
		t.Errorf("whoami = %q", out)
	}
	if out := e.must("", "list"); !strings.Contains(out, "No posts yet") {
		t.Errorf("empty list = %q", out)
	}

	e.editor("s/^$/Hello from vim./")
	// Without a terminal, new never opens the editor, even if $EDITOR is set.
	out, errOut, code := e.run("", "new", "Go", "interfaces", "explained")
	if code != 0 || !strings.Contains(out, "Created draft go-interfaces-explained") || !strings.Contains(errOut, "burzblog edit go-interfaces-explained") {
		t.Fatalf("new = %d %q %q", code, out, errOut)
	}
	if p, _ := e.store.GetPost(context.Background(), "go-interfaces-explained"); strings.Contains(p.Body, "Hello from vim.") {
		t.Fatalf("new opened the editor without a terminal: %+v", p)
	}
	out = e.must("", "edit", "go-interfaces-explained")
	if !strings.Contains(out, "version 2") {
		t.Fatalf("edit = %q", out)
	}
	p, _ := e.store.GetPost(context.Background(), "go-interfaces-explained")
	if !strings.Contains(p.Body, "Hello from vim.") || p.UpdatedBy != "ada" {
		t.Errorf("post after new+edit = %+v", p)
	}

	e.editor("s/nothing-matches-this//")
	if out := e.must("", "edit", "go-interfaces-explained"); !strings.Contains(out, "No changes") {
		t.Errorf("unchanged edit = %q", out)
	}

	// An invalid edit without a terminal is reported and the text kept.
	e.editor("s/^title: .*/title: \"\"/")
	_, errOut, code = e.run("", "edit", "go-interfaces-explained")
	if code != ExitValidation || !strings.Contains(errOut, "title must not be empty") || !strings.Contains(errOut, "kept at") {
		t.Errorf("invalid edit: %d %q", code, errOut)
	}
	e.editor("s/^title: .*/title: Fixed title/")
	if out := e.must("", "edit", "go-interfaces-explained"); !strings.Contains(out, "Saved") {
		t.Errorf("resumed edit = %q", out)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "go-interfaces-explained.md")
	e.must("", "pull", "go-interfaces-explained", "-o", file)
	if _, errOut, code := e.run("", "pull", "go-interfaces-explained", "-o", file); code == 0 || !strings.Contains(errOut, "already exists") {
		t.Errorf("pull over existing file: %d %q", code, errOut)
	}
	raw, _ := os.ReadFile(file)
	os.WriteFile(file, bytes.Replace(raw, []byte("Hello from vim."), []byte("Edited in VS Code."), 1), 0o644)

	// Someone saves in the browser meanwhile.
	cur, _ := e.store.GetPost(context.Background(), "go-interfaces-explained")
	if _, err := e.store.UpdatePost(context.Background(), cur.Slug, cur.Version, "grace", touch); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := e.run("", "push", file); code == 0 || !strings.Contains(errOut, "changed on the server") {
		t.Errorf("stale push: %d %q", code, errOut)
	}
	if out := e.must("", "push", file, "--force"); !strings.Contains(out, "Saved") {
		t.Errorf("forced push = %q", out)
	}

	// A file pulled earlier stays stale even after the post is edited
	// through another route; pushing it must not overwrite that work.
	stale := filepath.Join(dir, "stale", "go-interfaces-explained.md")
	os.MkdirAll(filepath.Dir(stale), 0o755)
	e.must("", "pull", "go-interfaces-explained", "-o", stale)
	e.editor("s/Edited in VS Code./Edited again via edit./")
	e.must("", "edit", "go-interfaces-explained")
	if _, errOut, code := e.run("", "push", stale); code == 0 || !strings.Contains(errOut, "changed on the server") {
		t.Errorf("stale file pushed over newer edit: %d %q", code, errOut)
	}

	newFile := filepath.Join(dir, "fresh-idea.md")
	os.WriteFile(newFile, []byte("---\ntitle: Fresh idea\n---\n\nBody.\n"), 0o644)
	if out := e.must("", "push", newFile); !strings.Contains(out, "Created fresh-idea") {
		t.Errorf("push new = %q", out)
	}
	if _, errOut, code := e.run("", "push", filepath.Join(dir, "Bad Name.md")); code == 0 || !strings.Contains(errOut, "name the file") {
		t.Errorf("bad file name: %d %q", code, errOut)
	}

	if out := e.must("", "publish", "fresh-idea"); !strings.Contains(out, "now published") {
		t.Errorf("publish = %q", out)
	}
	if _, errOut, code := e.run("", "publish", "fresh-idea"); code == 0 || !strings.Contains(errOut, "cannot be published again") {
		t.Errorf("publish twice: %d %q", code, errOut)
	}
	if out := e.must("", "list", "--status", "published"); !strings.Contains(out, "fresh-idea") || strings.Contains(out, "go-interfaces") {
		t.Errorf("published list = %q", out)
	}

	exportDir := t.TempDir()
	os.WriteFile(filepath.Join(exportDir, "stale.md"), []byte("old"), 0o644)
	out = e.must("", "export", "--out", exportDir, "--clean")
	if !strings.Contains(out, "Exported 1") || !strings.Contains(out, "removed 1") {
		t.Errorf("export = %q", out)
	}
	if b, err := os.ReadFile(filepath.Join(exportDir, "fresh-idea.md")); err != nil || !strings.Contains(string(b), "status: published") {
		t.Errorf("exported file: %q %v", b, err)
	}

	if out := e.must("", "logout"); !strings.Contains(out, "Signed out") {
		t.Errorf("logout = %q", out)
	}
	tokens, _ := e.store.Tokens(context.Background())
	if len(tokens) != 0 {
		t.Errorf("logout left tokens behind: %+v", tokens)
	}
}

func TestBackup(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ada")
	e.must("correct horse battery\n", "login", e.url, "-u", "ada")
	e.must("", "new", "Only a draft", "--no-edit")
	e.must("", "new", "Live one", "--no-edit")
	e.must("", "publish", "live-one")
	e.must("", "new", "Gone", "--no-edit")
	e.must("", "archive", "gone")

	dir := filepath.Join(t.TempDir(), "backup")
	if out := e.must("", "backup", dir); !strings.Contains(out, "Backed up 3 posts") {
		t.Errorf("backup = %q", out)
	}
	for name, status := range map[string]string{
		"draft/only-a-draft.md": "status: draft", "published/live-one.md": "status: published", "archived/gone.md": "status: draft",
	} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || !strings.Contains(string(b), status) {
			t.Errorf("%s = %q %v", name, b, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "README.txt")); err != nil || !strings.Contains(string(b), "burzblog push") {
		t.Errorf("README.txt = %q %v", b, err)
	}
	if _, errOut, code := e.run("", "backup", "a", "b"); code == 0 || errOut == "" {
		t.Errorf("two directories: %d %q", code, errOut)
	}
}

func TestReadBackup(t *testing.T) {
	archive := func(entries ...tar.Header) *bytes.Buffer {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, h := range entries {
			if h.Typeflag == 0 {
				h.Typeflag = tar.TypeReg
			}
			h.Size = 2
			if h.Typeflag != tar.TypeReg {
				h.Size = 0
			}
			tw.WriteHeader(&h)
			if h.Size > 0 {
				tw.Write([]byte("x\n"))
			}
		}
		tw.Close()
		gz.Close()
		return &buf
	}
	readme := tar.Header{Name: "README.txt"}

	files, err := readBackup(archive(readme, tar.Header{Name: "draft/a.md"}, tar.Header{Name: "archived/b-2.md"}))
	if err != nil || len(files) != 3 {
		t.Fatalf("valid backup = %v %v", files, err)
	}

	bad := map[string][]tar.Header{
		"traversal":      {readme, {Name: "../evil.md"}},
		"nested":         {readme, {Name: "draft/../../evil.md"}},
		"absolute":       {readme, {Name: "/etc/evil.md"}},
		"unknown status": {readme, {Name: "trash/a.md"}},
		"flat file":      {readme, {Name: "a.md"}},
		"deep":           {readme, {Name: "draft/x/a.md"}},
		"not markdown":   {readme, {Name: "draft/a.sh"}},
		"bad slug":       {readme, {Name: "draft/Bad Name.md"}},
		"other root":     {readme, {Name: "notes.txt"}},
		"symlink":        {readme, {Name: "draft/a.md", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		"directory":      {readme, {Name: "draft/", Typeflag: tar.TypeDir}},
		"no readme":      {{Name: "draft/a.md"}},
	}
	for name, entries := range bad {
		if _, err := readBackup(archive(entries...)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := readBackup(strings.NewReader("not gzip")); err == nil {
		t.Error("accepted a file that is not gzip")
	}
}

func TestExportWithEnvironmentToken(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ci")
	e.must("correct horse battery\n", "login", e.url, "-u", "ci")
	e.must("", "new", "Published thing", "--no-edit")
	e.must("", "publish", "published-thing")
	os.Remove(filepath.Join(e.config, "config.json"))

	u, _ := e.store.UserByName(context.Background(), "ci")
	svc, _ := auth.New(e.store)
	secret, _, err := svc.NewToken(context.Background(), u, "vercel", store.ScopeExport)
	if err != nil {
		t.Fatal(err)
	}
	e.vars["BURZBLOG_SERVER"] = e.url
	e.vars["BURZBLOG_TOKEN"] = secret
	out := e.must("", "export", "--out", t.TempDir())
	if !strings.Contains(out, "Exported 1") {
		t.Errorf("export with env token = %q", out)
	}
	if _, errOut, code := e.run("", "list"); code == 0 || !strings.Contains(errOut, "only download published posts") {
		t.Errorf("export token listing: %d %q", code, errOut)
	}
}

func TestNormalizeServer(t *testing.T) {
	cases := map[string]string{
		"blog.example.com":           "https://blog.example.com",
		"https://blog.example.com/":  "https://blog.example.com",
		"http://localhost:8080":      "http://localhost:8080",
		"http://127.0.0.1:8080/blog": "http://127.0.0.1:8080/blog",
	}
	for in, want := range cases {
		if got, err := normalizeServer(in); err != nil || got != want {
			t.Errorf("normalizeServer(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := normalizeServer("http://blog.example.com"); err == nil {
		t.Error("plain http to a remote host accepted")
	}
}

func touch(p *post.Post) error {
	p.Body += "\nBrowser edit.\n"
	return nil
}

func TestPasswdRevokesTerminalLogins(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ada")
	e.must("correct horse battery\n", "login", e.url, "-u", "ada")
	u, _ := e.store.UserByName(context.Background(), "ada")
	svc, _ := auth.New(e.store)
	exportSecret, _, err := svc.NewToken(context.Background(), u, "vercel", store.ScopeExport)
	if err != nil {
		t.Fatal(err)
	}
	e.must("", "whoami")

	e.must("a brand new password\n", "user", "passwd", "ada")

	if _, errOut, code := e.run("", "whoami"); code == 0 || !strings.Contains(errOut, "burzblog login") {
		t.Errorf("whoami after passwd: %d %q, want a prompt to log in again", code, errOut)
	}
	if _, errOut, code := e.run("", "list"); code == 0 || !strings.Contains(errOut, "burzblog login") {
		t.Errorf("list after passwd: %d %q, want a prompt to log in again", code, errOut)
	}
	e.must("a brand new password\n", "login", e.url, "-u", "ada")
	e.must("", "whoami")

	os.Remove(filepath.Join(e.config, "config.json"))
	e.vars["BURZBLOG_SERVER"] = e.url
	e.vars["BURZBLOG_TOKEN"] = exportSecret
	e.must("", "export", "--out", t.TempDir())
}
