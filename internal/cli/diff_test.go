package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postDoc(title, body string) string {
	return "---\ntitle: " + title + "\n---\n\n" + body
}

// create saves a new post from stdin and returns nothing; version 1.
func (e *env) create(slug, body string) {
	e.t.Helper()
	e.must(postDoc("Post "+slug, body), "push", "-", "--slug", slug)
}

// save pushes a change from another computer: it names the version it started from.
func (e *env) save(slug, body, base string) {
	e.t.Helper()
	e.must(postDoc("Post "+slug, body), "push", "-", "--slug", slug, "--base-version", base)
}

func (e *env) pull(dir, slug string) string {
	e.t.Helper()
	path := filepath.Join(dir, slug+".md")
	e.must("", "pull", slug, "-o", path)
	return path
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiffCommand(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	e.create("notes", "First.\n")
	dir := t.TempDir()
	file := e.pull(dir, "notes")

	// Identical to a pull: exit 0, nothing on stdout, based on the current version.
	out, errOut, code := e.run("", "diff", file)
	if code != ExitOK || out != "" || !strings.Contains(errOut, "current version (1)") {
		t.Errorf("identical: %d %q %q", code, out, errOut)
	}

	// An edited file: exit 1, a unified diff from the server to the file.
	write(t, file, postDoc("Post notes", "Second.\n"))
	out, errOut, code = e.run("", "diff", file)
	if code != ExitError {
		t.Errorf("different: exit %d, want 1", code)
	}
	for _, want := range []string{"--- server/notes.md (version 1)\n", "+++ " + file + "\n", "-First.\n", "+Second.\n", "@@ "} {
		if !strings.Contains(out, want) {
			t.Errorf("diff lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(errOut, "error:") || !strings.Contains(errOut, "current version") {
		t.Errorf("stderr = %q", errOut)
	}

	// The slug alone finds ./<slug>.md; slug plus file works from anywhere.
	if _, _, code := e.run("", "diff", "notes", file); code != ExitError {
		t.Errorf("slug and file: exit %d", code)
	}
	t.Chdir(dir)
	if _, _, code := e.run("", "diff", "notes"); code != ExitError {
		t.Errorf("slug only: exit %d", code)
	}

	// Someone saved since the pull: the file is stale.
	e.save("notes", "Elsewhere.\n", "1")
	_, errOut, code = e.run("", "diff", file)
	if code != ExitError || !strings.Contains(errOut, "based on version 1, but the server is at version 2") {
		t.Errorf("stale: %d %q", code, errOut)
	}
	doc := decode(t, jsonOut(t, e, "diff", file, "--json"))
	if doc["slug"] != "notes" || doc["baseVersion"] != float64(1) || doc["serverVersion"] != float64(2) ||
		doc["stale"] != true || doc["identical"] != false || !strings.Contains(doc["diff"].(string), "-Elsewhere.\n+Second.\n") {
		t.Errorf("json = %v", doc)
	}
}

// jsonOut runs a command that is expected to exit 0 or 1 and returns stdout.
func jsonOut(t *testing.T, e *env, args ...string) string {
	t.Helper()
	out, errOut, code := e.run("", args...)
	if code != ExitOK && code != ExitError {
		t.Fatalf("burzblog %s: exit %d %s", strings.Join(args, " "), code, errOut)
	}
	if errOut != "" {
		t.Errorf("--json wrote to stderr: %q", errOut)
	}
	return out
}

func TestDiffJSONIdenticalAndUnpulled(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	e.create("notes", "First.\n")
	dir := t.TempDir()
	file := e.pull(dir, "notes")

	out, _, code := e.run("", "diff", file, "--json")
	doc := decode(t, out)
	if code != ExitOK || doc["identical"] != true || doc["stale"] != false || doc["diff"] != "" || doc["baseVersion"] != float64(1) {
		t.Errorf("identical json: %d %v", code, doc)
	}

	// A copy this computer never pulled has no base version.
	copied := filepath.Join(dir, "copy", "notes.md")
	if err := os.Mkdir(filepath.Dir(copied), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	write(t, copied, string(raw)+"More.\n")
	out, errOut, code := e.run("", "diff", copied)
	if code != ExitError || !strings.Contains(errOut, "base version is unknown") || !strings.Contains(out, "+More.\n") {
		t.Errorf("unpulled: %d %q %q", code, out, errOut)
	}
	doc = decode(t, jsonOut(t, e, "diff", copied, "--json"))
	if doc["baseVersion"] != nil || doc["stale"] != false || doc["identical"] != false {
		t.Errorf("unpulled json = %v", doc)
	}
}

func TestDiffTrailingNewlineAndCRLF(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	e.create("notes", "First.\n")
	file := e.pull(t.TempDir(), "notes")
	raw, _ := os.ReadFile(file)

	write(t, file, strings.TrimSuffix(string(raw), "\n"))
	out, _, code := e.run("", "diff", file)
	if code != ExitError || !strings.Contains(out, "\\ No newline at end of file") {
		t.Errorf("missing final newline: %d %q", code, out)
	}

	write(t, file, strings.ReplaceAll(string(raw), "\n", "\r\n"))
	out, _, code = e.run("", "diff", file)
	if code != ExitError || !strings.Contains(out, "-First.\n") || !strings.Contains(out, "+First.␍\n") {
		t.Errorf("CRLF: %d %q", code, out)
	}
}

func noControlCharacters(t *testing.T, out string) {
	t.Helper()
	for _, r := range strings.TrimSuffix(out, "\n") {
		if r < 0x20 && r != '\n' || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			t.Fatalf("control character %q in output %q", r, out)
		}
	}
}

func TestDiffNeverPrintsControlCharacters(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	hostile := "Click \x1b]8;;https://evil.example\x07here\x1b]8;;\x07 \u009b31m tab\there\n"
	e.create("notes", hostile)
	file := e.pull(t.TempDir(), "notes")
	// The local file differs elsewhere, so the hostile line is context.
	raw, _ := os.ReadFile(file)
	write(t, file, string(raw)+"Added.\n")
	out, _, code := e.run("", "diff", file)
	if code != ExitError {
		t.Fatalf("exit %d", code)
	}
	noControlCharacters(t, out)
	if !strings.Contains(out, "\uFFFD") || !strings.Contains(out, "tab    here") {
		t.Errorf("output = %q", out)
	}
	// Server content as the *removed* side must be cleaned too.
	write(t, file, postDoc("Post notes", "Gone.\n"))
	out, _, _ = e.run("", "diff", file)
	noControlCharacters(t, out)
	if !strings.Contains(out, "-Click \uFFFD]8;;") {
		t.Errorf("removed side = %q", out)
	}
}

func TestDiffErrors(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "notes.md"), postDoc("Post notes", "x\n"))
	file := filepath.Join(dir, "notes.md")

	if _, errOut, code := e.run("", "diff", file); code != ExitAuth || !strings.Contains(errOut, "burzblog login") {
		t.Errorf("not signed in: %d %q", code, errOut)
	}
	e.signedIn()

	if _, _, code := e.run("", "diff", file); code != ExitNotFound {
		t.Errorf("post not on the server: exit %d, want %d", code, ExitNotFound)
	}
	if inner, code := e.failure("", "diff", file, "--json"); code != ExitNotFound || inner["code"] != "not_found" {
		t.Errorf("json not found: %d %v", code, inner)
	}
	if _, _, code := e.run("", "diff", filepath.Join(dir, "missing.md")); code != ExitUsage {
		t.Errorf("unreadable file: exit %d, want %d", code, ExitUsage)
	}
	write(t, filepath.Join(dir, "Not A Slug.md"), "x")
	if _, _, code := e.run("", "diff", filepath.Join(dir, "Not A Slug.md")); code != ExitUsage {
		t.Errorf("bad file name: exit %d, want %d", code, ExitUsage)
	}
	if _, _, code := e.run("", "diff", "Bad Slug", file); code != ExitUsage {
		t.Errorf("bad slug: exit %d, want %d", code, ExitUsage)
	}
	if _, _, code := e.run("", "diff"); code != ExitUsage {
		t.Errorf("no arguments: exit %d, want %d", code, ExitUsage)
	}
	if _, _, code := e.run("", "diff", "a", "b", "c"); code != ExitUsage {
		t.Errorf("three arguments: exit %d, want %d", code, ExitUsage)
	}
}

func TestPushConflictMentionsDiff(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	e.create("notes", "First.\n")
	file := e.pull(t.TempDir(), "notes")
	e.save("notes", "Elsewhere.\n", "1")
	write(t, file, postDoc("Post notes", "Mine.\n"))

	_, errOut, code := e.run("", "push", file)
	if code != ExitConflict || !strings.Contains(errOut, "burzblog diff "+file) {
		t.Errorf("stale push: %d %q", code, errOut)
	}
	// A file never pulled gets the same pointer.
	fresh := filepath.Join(t.TempDir(), "notes.md")
	write(t, fresh, postDoc("Post notes", "Other.\n"))
	if _, errOut, code := e.run("", "push", fresh); code != ExitConflict || !strings.Contains(errOut, "burzblog diff "+fresh) {
		t.Errorf("unpulled push: %d %q", code, errOut)
	}
	// Standard input has no file to diff, so it keeps the pull hint.
	_, errOut, code = e.run(postDoc("Post notes", "Stdin.\n"), "push", "-", "--slug", "notes", "--base-version", "1")
	if code != ExitConflict || strings.Contains(errOut, "burzblog diff") || !strings.Contains(errOut, "burzblog pull notes -o -") {
		t.Errorf("stdin push: %d %q", code, errOut)
	}
}

func TestStatusCommand(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	for _, slug := range []string{"clean", "edited", "behind", "both"} {
		e.create(slug, "First.\n")
	}
	dir := t.TempDir()
	for _, slug := range []string{"clean", "edited", "behind", "both"} {
		e.pull(dir, slug)
	}
	write(t, filepath.Join(dir, "edited.md"), postDoc("Post edited", "Mine.\n"))
	write(t, filepath.Join(dir, "both.md"), postDoc("Post both", "Mine.\n"))
	e.save("behind", "Theirs.\n", "1")
	e.save("both", "Theirs.\n", "1")
	write(t, filepath.Join(dir, "draft-idea.md"), postDoc("Idea", "Not saved yet.\n"))
	// Copied in, never pulled here, and different.
	e.create("copied", "Server text.\n")
	write(t, filepath.Join(dir, "copied.md"), postDoc("Post copied", "Other text.\n"))
	// Not posts: wrong extension, not a slug, a folder.
	write(t, filepath.Join(dir, "README.txt"), "x")
	write(t, filepath.Join(dir, "Not A Slug.md"), "x")
	if err := os.Mkdir(filepath.Join(dir, "sub.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, errOut, code := e.run("", "status", dir)
	if code != ExitOK || errOut != "" {
		t.Fatalf("status: %d %q", code, errOut)
	}
	want := map[string]string{
		"behind.md":     "changed on server (base 1, server 2)",
		"both.md":       "both changed (base 1, server 2)",
		"clean.md":      "up to date",
		"copied.md":     "differs, not pulled by this computer",
		"draft-idea.md": "not on server",
		"edited.md":     "modified locally (base 1, server 1)",
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(want), out)
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		name := filepath.Base(fields[0])
		if got := strings.TrimSpace(strings.TrimPrefix(line, fields[0])); got != want[name] {
			t.Errorf("%s: %q, want %q", name, got, want[name])
		}
	}

	// JSON states are stable identifiers.
	doc := decode(t, e.must("", "status", dir, "--json"))
	got := map[string]string{}
	for _, f := range doc["files"].([]any) {
		m := f.(map[string]any)
		got[m["slug"].(string)] = m["state"].(string)
	}
	wantJSON := map[string]string{"behind": "changed_on_server", "both": "both_changed", "clean": "up_to_date",
		"copied": "not_pulled", "draft-idea": "not_on_server", "edited": "modified"}
	for slug, state := range wantJSON {
		if got[slug] != state {
			t.Errorf("json %s = %q, want %q", slug, got[slug], state)
		}
	}
	if len(got) != len(wantJSON) {
		t.Errorf("json files = %v", got)
	}
}

func TestStatusAfterPushIsUpToDate(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	dir := t.TempDir()
	file := filepath.Join(dir, "fresh.md")
	write(t, file, postDoc("Fresh", "Body.\n"))
	if out := e.must("", "status", dir); !strings.Contains(out, "not on server") {
		t.Errorf("before push: %q", out)
	}
	e.must("", "push", file)
	if out := e.must("", "status", dir); !strings.Contains(out, "up to date") {
		t.Errorf("after push: %q", out)
	}
	// Edited again after the push.
	write(t, file, postDoc("Fresh", "Changed.\n"))
	if out := e.must("", "status", dir); !strings.Contains(out, "modified locally") {
		t.Errorf("after edit: %q", out)
	}
}

func TestStatusDefaultsToCurrentFolderAndEmpty(t *testing.T) {
	e := newEnv(t)
	e.signedIn()
	dir := t.TempDir()
	t.Chdir(dir)
	if out := e.must("", "status"); !strings.Contains(out, "No <slug>.md files") {
		t.Errorf("empty folder: %q", out)
	}
	doc := decode(t, e.must("", "status", "--json"))
	if files, ok := doc["files"].([]any); !ok || len(files) != 0 {
		t.Errorf("empty json = %v", doc)
	}
	write(t, filepath.Join(dir, "idea.md"), postDoc("Idea", "x\n"))
	if out := e.must("", "status"); !strings.Contains(out, "idea.md") || !strings.Contains(out, "not on server") {
		t.Errorf("current folder: %q", out)
	}
}

func TestStatusErrors(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "idea.md"), postDoc("Idea", "x\n"))
	if _, _, code := e.run("", "status", dir); code != ExitAuth {
		t.Errorf("not signed in: exit %d, want %d", code, ExitAuth)
	}
	e.signedIn()
	if _, errOut, code := e.run("", "status", filepath.Join(dir, "nope")); code != ExitError || !strings.Contains(errOut, "cannot read") {
		t.Errorf("missing folder: %d %q", code, errOut)
	}
	if _, _, code := e.run("", "status", dir, "extra"); code != ExitUsage {
		t.Errorf("two folders: exit %d, want %d", code, ExitUsage)
	}
}
