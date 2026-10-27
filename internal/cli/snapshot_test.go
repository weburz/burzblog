package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/store"
)

func snapshotEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ada")
	if _, err := e.store.CreatePost(context.Background(), &post.Post{Slug: "hello", Title: "Hello", Body: "Kept safe."}, "ada"); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSnapshotCreateVerifyRestore(t *testing.T) {
	e := snapshotEnv(t)
	data := e.vars["BURZBLOG_DATA"]

	var made snapshotLine
	if err := json.Unmarshal([]byte(e.must("", "snapshot", "create", "--json")), &made); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(made.Path) != filepath.Join(data, snapshotDir) || made.Posts != 1 || made.Accounts != 1 || made.Encrypted || made.Size == 0 {
		t.Fatalf("create = %+v", made)
	}
	if info, err := os.Stat(made.Path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Errorf("snapshot file: %v %v", info, err)
	}
	if entry, err := e.store.LastAudit(context.Background(), "snapshot.create"); err != nil || entry.Target != filepath.Base(made.Path) || entry.ActorName != "host" {
		t.Errorf("audit = %+v, %v", entry, err)
	}

	if out := e.must("", "snapshot", "verify", made.Path); !strings.Contains(out, "complete and restores") || !strings.Contains(out, "1 post") {
		t.Errorf("verify = %q", out)
	}
	work := t.TempDir()
	e.must("", "snapshot", "verify", made.Path, "--work", work)
	if left, _ := os.ReadDir(work); len(left) != 0 {
		t.Errorf("verify left %v in its work folder", left)
	}
	if _, errOut, code := e.run("", "snapshot", "verify", made.Path, "--work", filepath.Join(work, "missing")); code == 0 || !strings.Contains(errOut, "working folder") {
		t.Errorf("missing work folder: %d %q", code, errOut)
	}

	// Restore onto a "new server".
	e.vars["BURZBLOG_DATA"] = filepath.Join(t.TempDir(), "new")
	out := e.must("", "snapshot", "restore", made.Path)
	if !strings.Contains(out, "Restored the snapshot") || !strings.Contains(out, "Start BurzBlog again") {
		t.Errorf("restore = %q", out)
	}
	st, err := store.Open(context.Background(), e.vars["BURZBLOG_DATA"])
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if p, err := st.GetPost(context.Background(), "hello"); err != nil || p.Body != "Kept safe." {
		t.Errorf("restored post = %+v, %v", p, err)
	}
	if entry, err := st.LastAudit(context.Background(), "snapshot.restore"); err != nil || !strings.Contains(entry.Target, "by test") {
		t.Errorf("restore audit = %+v, %v", entry, err)
	}

	// Again: the directory holds a blog now.
	if _, errOut, code := e.run("", "snapshot", "restore", made.Path); code != ExitConflict || !strings.Contains(errOut, "--force") {
		t.Errorf("restore over a blog: %d %q", code, errOut)
	}
	if out := e.must("", "snapshot", "restore", made.Path, "--force"); !strings.Contains(out, "is kept in") {
		t.Errorf("forced restore = %q", out)
	}
}

func TestSnapshotEncrypted(t *testing.T) {
	e := snapshotEnv(t)
	e.vars["BURZBLOG_SNAPSHOT_PASSPHRASE"] = "a long passphrase here"
	var made snapshotLine
	if err := json.Unmarshal([]byte(e.must("", "snapshot", "create", t.TempDir(), "--json")), &made); err != nil {
		t.Fatal(err)
	}
	if !made.Encrypted || !strings.HasSuffix(made.Path, ".tar.gz.enc") {
		t.Fatalf("create = %+v", made)
	}

	delete(e.vars, "BURZBLOG_SNAPSHOT_PASSPHRASE")
	if _, errOut, code := e.run("", "snapshot", "verify", made.Path); code == 0 || !strings.Contains(errOut, "BURZBLOG_SNAPSHOT_PASSPHRASE") {
		t.Errorf("no passphrase: %d %q", code, errOut)
	}
	wrong := filepath.Join(t.TempDir(), "wrong")
	os.WriteFile(wrong, []byte("not the passphrase at all\n"), 0o600)
	if _, errOut, code := e.run("", "snapshot", "verify", made.Path, "--passphrase-file", wrong); code == 0 || !strings.Contains(errOut, "passphrase is wrong") {
		t.Errorf("wrong passphrase: %d %q", code, errOut)
	}
	right := filepath.Join(t.TempDir(), "right")
	os.WriteFile(right, []byte("a long passphrase here\n"), 0o600)
	if out := e.must("", "snapshot", "verify", made.Path, "--passphrase-file", right); !strings.Contains(out, ", encrypted.") {
		t.Errorf("verify = %q", out)
	}

	e.vars["BURZBLOG_SNAPSHOT_PASSPHRASE"] = "short"
	if _, errOut, code := e.run("", "snapshot", "create"); code != ExitUsage || !strings.Contains(errOut, "at least 12") {
		t.Errorf("short passphrase: %d %q", code, errOut)
	}
	delete(e.vars, "BURZBLOG_SNAPSHOT_PASSPHRASE")
	if _, errOut, code := e.run("", "snapshot", "create", "--encrypt"); code != ExitUsage || !strings.Contains(errOut, "terminal") {
		t.Errorf("--encrypt without a terminal: %d %q", code, errOut)
	}
}

func TestSnapshotThroughStandardStreams(t *testing.T) {
	e := snapshotEnv(t)
	snap, errOut, code := e.run("", "snapshot", "create", "-")
	if code != 0 || !strings.Contains(errOut, "standard output") || len(snap) < 100 {
		t.Fatalf("create - = %d bytes, %d %q", len(snap), code, errOut)
	}
	if _, errOut, code := e.run("", "snapshot", "create", "-", "--json"); code != ExitUsage {
		t.Errorf("- with --json: %d %q", code, errOut)
	}
	e.vars["BURZBLOG_DATA"] = filepath.Join(t.TempDir(), "new")
	if out := e.must(snap, "snapshot", "restore", "-"); !strings.Contains(out, "Restored") {
		t.Errorf("restore - = %q", out)
	}
}

func TestSnapshotKeep(t *testing.T) {
	e := snapshotEnv(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o600)
	for range 4 {
		e.must("", "snapshot", "create", dir, "--keep", "2")
	}
	left, _ := filepath.Glob(filepath.Join(dir, "burzblog-snapshot-*"))
	if len(left) != 2 {
		t.Errorf("kept %v", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Errorf("an unrelated file was deleted: %v", err)
	}
	// A missing folder is made, not written as a file named like it.
	fresh := filepath.Join(t.TempDir(), "backups", "blog")
	e.must("", "snapshot", "create", fresh)
	if made, _ := filepath.Glob(filepath.Join(fresh, "burzblog-snapshot-*.tar.gz")); len(made) != 1 {
		t.Errorf("snapshot in a new folder: %v", made)
	}
	if _, errOut, code := e.run("", "snapshot", "create", filepath.Join(dir, "one.tar.gz"), "--keep", "2"); code != ExitUsage {
		t.Errorf("--keep with a file: %d %q", code, errOut)
	}
	file := filepath.Join(dir, "one.tar.gz")
	e.must("", "snapshot", "create", file)
	if _, errOut, code := e.run("", "snapshot", "create", file); code != ExitConflict || !strings.Contains(errOut, "already exists") {
		t.Errorf("overwrite: %d %q", code, errOut)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.partial")); len(left) != 0 {
		t.Errorf("partial files left: %v", left)
	}
}

func TestSnapshotErrors(t *testing.T) {
	e := snapshotEnv(t)
	e.vars["BURZBLOG_DATA"] = filepath.Join(t.TempDir(), "nothing-here")
	if _, errOut, code := e.run("", "snapshot", "create"); code == 0 || !strings.Contains(errOut, "no BurzBlog database") {
		t.Errorf("no database: %d %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(e.vars["BURZBLOG_DATA"], store.FileName)); !os.IsNotExist(err) {
		t.Error("create made an empty database")
	}
	text := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(text, []byte("hello"), 0o600)
	if _, errOut, code := e.run("", "snapshot", "restore", text); code == 0 || !strings.Contains(errOut, "not a BurzBlog snapshot") {
		t.Errorf("not a snapshot: %d %q", code, errOut)
	}
	if _, errOut, code := e.run("", "snapshot", "verify", filepath.Join(t.TempDir(), "missing.tar.gz")); code == 0 || !strings.Contains(errOut, "open the snapshot") {
		t.Errorf("missing file: %d %q", code, errOut)
	}
}

func TestSnapshotRestoreRefusesWhileServing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no flock on Windows")
	}
	e := snapshotEnv(t)
	file := filepath.Join(t.TempDir(), "s.tar.gz")
	e.must("", "snapshot", "create", file)
	unlock, err := store.LockShared(e.vars["BURZBLOG_DATA"]) // what serve holds
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, errOut, code := e.run("", "snapshot", "restore", file, "--force"); code != ExitConflict || !strings.Contains(errOut, "stop it first") {
		t.Errorf("restore while serving: %d %q", code, errOut)
	}
}

func TestSnapshotCreateNeverMigrates(t *testing.T) {
	e := snapshotEnv(t)
	data := e.vars["BURZBLOG_DATA"]
	db, err := sql.Open("sqlite", "file:"+filepath.Join(data, store.FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var current int
	db.QueryRow("PRAGMA user_version").Scan(&current)
	// A newer binary snapshots the database of a server not yet restarted.
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", current-1)); err != nil {
		t.Fatal(err)
	}
	var made snapshotLine
	if err := json.Unmarshal([]byte(e.must("", "snapshot", "create", "--json")), &made); err != nil {
		t.Fatal(err)
	}
	var after int
	db.QueryRow("PRAGMA user_version").Scan(&after)
	if after != current-1 || made.Schema != current-1 {
		t.Errorf("schema after snapshot = %d, snapshot says %d; want %d untouched", after, made.Schema, current-1)
	}
}
