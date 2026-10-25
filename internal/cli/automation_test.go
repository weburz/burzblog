package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// signedIn creates an account and signs the CLI in.
func (e *env) signedIn() {
	e.t.Helper()
	e.must("correct horse battery\n", "user", "add", "ada")
	e.must("correct horse battery\n", "login", e.url, "-u", "ada")
}

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("not one JSON document: %v\n%q", err, raw)
	}
	return m
}

// failure runs a command expected to fail with --json and returns the
// decoded error document and the exit code; stdout must stay empty.
func (e *env) failure(stdin string, args ...string) (map[string]any, int) {
	e.t.Helper()
	out, errOut, code := e.run(stdin, args...)
	if out != "" {
		e.t.Errorf("burzblog %s printed to stdout on failure: %q", strings.Join(args, " "), out)
	}
	doc := decode(e.t, errOut)
	inner, ok := doc["error"].(map[string]any)
	if !ok {
		e.t.Fatalf("no error object in %q", errOut)
	}
	for _, key := range []string{"code", "message", "detail"} {
		if _, ok := inner[key].(string); !ok {
			e.t.Errorf("error.%s missing or not a string in %q", key, errOut)
		}
	}
	return inner, code
}

func TestJSONResults(t *testing.T) {
	e := newEnv(t)
	e.signedIn()

	list := decode(t, e.must("", "list", "--json"))
	if items, ok := list["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("empty list items = %#v", list["items"])
	}
	if _, ok := list["counts"].(map[string]any); !ok {
		t.Errorf("list counts = %#v", list["counts"])
	}

	who := decode(t, e.must("", "whoami", "--json"))
	if who["username"] != "ada" || who["server"] != e.url {
		t.Errorf("whoami = %v", who)
	}

	created := decode(t, e.must("", "new", "Plain draft", "--no-edit", "--json"))
	if created["slug"] != "plain-draft" || created["status"] != "draft" || created["version"] != float64(1) {
		t.Errorf("new = %v", created)
	}
	if _, has := created["body"]; has {
		t.Errorf("new printed the body: %v", created)
	}

	withBody := decode(t, e.must("# Heading\n\nSome words here.\n", "new", "From stdin", "--body-file", "-", "--json"))
	if withBody["slug"] != "from-stdin" || withBody["version"] != float64(2) {
		t.Errorf("new --body-file - = %v", withBody)
	}
	p, err := e.store.GetPost(context.Background(), "from-stdin")
	if err != nil || p.Title != "From stdin" || p.Body != "# Heading\n\nSome words here.\n" {
		t.Errorf("stored post = %+v %v", p, err)
	}

	list = decode(t, e.must("", "list", "--json", "--status", "draft"))
	items := list["items"].([]any)
	if len(items) != 2 || list["counts"].(map[string]any)["draft"] != float64(2) {
		t.Errorf("list = %v", list)
	}
	first := items[0].(map[string]any)
	for _, key := range []string{"slug", "title", "status", "version", "words", "updatedAt", "updatedBy"} {
		if _, ok := first[key]; !ok {
			t.Errorf("list item lacks %q: %v", key, first)
		}
	}

	pub := decode(t, e.must("", "publish", "plain-draft", "--json"))
	if pub["status"] != "published" || pub["version"] != float64(2) {
		t.Errorf("publish = %v", pub)
	}

	file := filepath.Join(t.TempDir(), "plain-draft.md")
	pull := decode(t, e.must("", "pull", "plain-draft", "-o", file, "--json"))
	if pull["slug"] != "plain-draft" || pull["version"] != float64(2) || pull["path"] != file {
		t.Errorf("pull = %v", pull)
	}
	pullStdout := decode(t, e.must("", "pull", "plain-draft", "-o", "-", "--json"))
	if md, _ := pullStdout["markdown"].(string); !strings.Contains(md, "title: Plain draft") || pullStdout["version"] != float64(2) {
		t.Errorf("pull -o - = %v", pullStdout)
	}

	pushed := decode(t, e.must("", "push", file, "--json"))
	if pushed["created"] != false || pushed["slug"] != "plain-draft" {
		t.Errorf("push = %v", pushed)
	}
	fresh := filepath.Join(t.TempDir(), "fresh.md")
	os.WriteFile(fresh, []byte("---\ntitle: Fresh\n---\n\nBody.\n"), 0o644)
	if pushed := decode(t, e.must("", "push", fresh, "--json")); pushed["created"] != true || pushed["version"] != float64(1) {
		t.Errorf("push new = %v", pushed)
	}

	backup := decode(t, e.must("", "backup", filepath.Join(t.TempDir(), "b"), "--json"))
	if backup["posts"] != float64(3) {
		t.Errorf("backup = %v", backup)
	}

	// Without --json the human output is unchanged.
	if out := e.must("", "whoami"); out != "ada on "+e.url+"\n" {
		t.Errorf("whoami = %q", out)
	}
}

func TestJSONFailures(t *testing.T) {
	e := newEnv(t)

	// Not signed in.
	if inner, code := e.failure("", "list", "--json"); code != ExitAuth || inner["code"] != "authentication" {
		t.Errorf("unauthenticated: %d %v", code, inner)
	}
	if _, errOut, code := e.run("", "list"); code != ExitAuth || !strings.Contains(errOut, "burzblog login") {
		t.Errorf("unauthenticated human: %d %q", code, errOut)
	}

	e.signedIn()

	// A token the server does not know.
	bad := &env{t: t, url: e.url, store: e.store, config: t.TempDir(), cache: t.TempDir(),
		vars: map[string]string{"BURZBLOG_SERVER": e.url, "BURZBLOG_TOKEN": "bzb_not-a-token"}}
	if inner, code := bad.failure("", "whoami", "--json"); code != ExitAuth || inner["code"] != "authentication" {
		t.Errorf("bad token: %d %v", code, inner)
	}

	// Not found.
	if inner, code := e.failure("", "publish", "no-such-post", "--json"); code != ExitNotFound || inner["code"] != "not_found" {
		t.Errorf("not found: %d %v", code, inner)
	}
	if _, _, code := e.run("", "pull", "no-such-post", "-o", "-"); code != ExitNotFound {
		t.Errorf("not found human exit = %d", code)
	}

	// Conflict: someone saves after the file was pulled.
	e.must("", "new", "Contested", "--no-edit")
	file := filepath.Join(t.TempDir(), "contested.md")
	e.must("", "pull", "contested", "-o", file)
	cur, _ := e.store.GetPost(context.Background(), "contested")
	if _, err := e.store.UpdatePost(context.Background(), cur.Slug, cur.Version, "grace", touch); err != nil {
		t.Fatal(err)
	}
	inner, code := e.failure("", "push", file, "--json")
	if code != ExitConflict || inner["code"] != "conflict" || !strings.Contains(inner["message"].(string), "nothing was overwritten") {
		t.Errorf("conflict: %d %v", code, inner)
	}
	if inner["detail"] == "" {
		t.Errorf("conflict has no detail: %v", inner)
	}
}

func TestPushFromStdin(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	doc := "---\ntitle: Agent post\n---\n\nFirst.\n"

	// A new post needs no base version.
	out := decode(t, e.must(doc, "push", "-", "--slug", "agent-post", "--json"))
	if out["created"] != true || out["version"] != float64(1) {
		t.Fatalf("create = %v", out)
	}

	// An existing post is not overwritten blindly.
	revised := "---\ntitle: Agent post\n---\n\nSecond.\n"
	inner, code := e.failure(revised, "push", "-", "--slug", "agent-post", "--json")
	if code != ExitConflict || inner["code"] != "conflict" {
		t.Errorf("push without base: %d %v", code, inner)
	}

	// With the version it started from, it saves.
	out = decode(t, e.must(revised, "push", "-", "--slug", "agent-post", "--base-version", "1", "--json"))
	if out["created"] != false || out["version"] != float64(2) {
		t.Fatalf("save = %v", out)
	}
	if p, _ := e.store.GetPost(context.Background(), "agent-post"); !strings.Contains(p.Body, "Second.") {
		t.Errorf("stored = %+v", p)
	}

	// The version is stale now: refused, nothing overwritten, exit 3.
	inner, code = e.failure("---\ntitle: Agent post\n---\n\nThird.\n", "push", "-", "--slug", "agent-post", "--base-version", "1", "--json")
	if code != ExitConflict || inner["code"] != "conflict" {
		t.Errorf("stale base: %d %v", code, inner)
	}
	if p, _ := e.store.GetPost(context.Background(), "agent-post"); strings.Contains(p.Body, "Third.") || p.Version != 2 {
		t.Errorf("stale push overwrote: %+v", p)
	}

	// Content the server rejects: exit 6.
	inner, code = e.failure("---\ntitle: \"\"\n---\n\nBody.\n", "push", "-", "--slug", "agent-post", "--base-version", "2", "--json")
	if code != ExitValidation || inner["code"] != "validation" || !strings.Contains(inner["detail"].(string), "title") {
		t.Errorf("invalid: %d %v", code, inner)
	}

	// Usage: stdin needs --slug; --slug needs stdin.
	if inner, code := e.failure(doc, "push", "-", "--json"); code != ExitUsage || inner["code"] != "usage" {
		t.Errorf("no slug: %d %v", code, inner)
	}
	file := filepath.Join(t.TempDir(), "agent-post.md")
	os.WriteFile(file, []byte(doc), 0o644)
	if _, _, code := e.run("", "push", file, "--slug", "other"); code != ExitUsage {
		t.Errorf("--slug with a file: %d", code)
	}
}

func TestUsageErrors(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{{"list", "--no-such-flag"}, {"no-such-command"}, {"publish"}, {"export"}} {
		if _, errOut, code := e.run("", args...); code != ExitUsage || errOut == "" {
			t.Errorf("burzblog %s: %d %q", strings.Join(args, " "), code, errOut)
		}
	}
	// Usage errors are JSON too when asked, even if the flag was not parsed.
	inner, code := e.failure("", "list", "--no-such-flag", "--json")
	if code != ExitUsage || inner["code"] != "usage" {
		t.Errorf("usage json: %d %v", code, inner)
	}
	if out, _, code := e.run("", "--help"); code != ExitOK || !strings.Contains(out, "Exit codes:") || !strings.Contains(out, "BURZBLOG_TOKEN") {
		t.Errorf("root help: %d %q", code, out)
	}
}

func TestNeverPromptsWithoutTerminal(t *testing.T) {
	e := newEnv(t)
	e.signedIn()

	// confirm falls back to its default and leaves stdin alone.
	a := &App{In: strings.NewReader("n\n"), Out: &strings.Builder{}, Err: &strings.Builder{}}
	if !a.confirm("Continue?", true) || a.confirm("Continue?", false) {
		t.Error("confirm asked without a terminal")
	}

	// edit with no terminal and no editor refuses instead of starting vi.
	e.must("", "new", "Needs editor", "--no-edit")
	if _, errOut, code := e.run("", "edit", "needs-editor"); code != ExitUsage || !strings.Contains(errOut, "burzblog pull") {
		t.Errorf("edit without terminal: %d %q", code, errOut)
	}

	// new never opens an editor without a terminal, even with $EDITOR set.
	marker := filepath.Join(t.TempDir(), "editor-ran")
	script := filepath.Join(t.TempDir(), "editor.sh")
	os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	e.vars["EDITOR"], e.vars["VISUAL"] = script, script
	e.must("", "new", "Quiet one")
	e.must("body\n", "new", "Quiet two", "--body-file", "-")
	e.must("", "new", "Quiet three", "--json")
	if _, err := os.Stat(marker); err == nil {
		t.Error("an editor was started without a terminal")
	}

	// An unreadable body file fails before anything is created.
	if _, _, code := e.run("", "new", "Ghost", "--body-file", filepath.Join(t.TempDir(), "missing.md")); code != ExitError {
		t.Errorf("missing body file: %d", code)
	}
	if _, err := e.store.GetPost(context.Background(), "ghost"); err == nil {
		t.Error("a draft was created although the body could not be read")
	}
}

func TestHelpTexts(t *testing.T) {
	e := newEnv(t)
	// The global --json help names every command that emits JSON.
	out, _, code := e.run("", "list", "--help")
	if code != ExitOK {
		t.Fatalf("list --help: %d", code)
	}
	for _, name := range []string{"schedule", "unschedule", "import", "backup"} {
		if !strings.Contains(out, name+",") && !strings.Contains(out, name+")") {
			t.Errorf("--json help does not name %s: %q", name, out)
		}
	}
	// Commands people type every day carry an example.
	for _, args := range [][]string{
		{"login"}, {"whoami"}, {"list"}, {"new"}, {"edit"}, {"pull"}, {"push"}, {"publish"}, {"schedule"},
		{"open"}, {"import"}, {"export"}, {"backup"}, {"serve"}, {"user", "add"}, {"user", "passwd"},
	} {
		help, _, code := e.run("", append(args, "--help")...)
		if code != ExitOK || !strings.Contains(help, "Examples:") {
			t.Errorf("burzblog %s --help has no examples: %d %q", strings.Join(args, " "), code, help)
		}
	}
	// pull -o must end in <slug>.md, and the help says so.
	if help, _, _ := e.run("", "pull", "--help"); !strings.Contains(help, "named <slug>.md") {
		t.Errorf("pull --help does not explain -o: %q", help)
	}
}
