package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oldDatabase creates a database at schema k, as an older release left it,
// holding one post.
func oldDatabase(t *testing.T, k int) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, FileName)
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < k; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO posts (slug, title, status, body, created_at, updated_at)
		VALUES ('keep-me', 'Keep me', 'draft', 'body', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", k)); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func schemaOf(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func backupFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, BackupDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestOpenBacksUpBeforeMigrating(t *testing.T) {
	dir, path := oldDatabase(t, 5)
	s, err := OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	names := backupFiles(t, dir)
	if len(names) != 1 || !strings.HasPrefix(names[0], "burzblog-schema5-") || !backupName.MatchString(names[0]) {
		t.Fatalf("backups = %v; want one burzblog-schema5-<time>.db", names)
	}
	backup := filepath.Join(dir, BackupDir, names[0])
	if got := schemaOf(t, backup); got != 5 {
		t.Errorf("backup schema = %d; want 5", got)
	}
	if got := schemaOf(t, path); got != len(migrations) {
		t.Errorf("database schema = %d; want %d", got, len(migrations))
	}
	db, err := sql.Open("sqlite", "file:"+backup+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var title string
	if err := db.QueryRow("SELECT title FROM posts WHERE slug = 'keep-me'").Scan(&title); err != nil || title != "Keep me" {
		t.Errorf("backup post = %q, %v; want Keep me", title, err)
	}
	for _, p := range []struct {
		path string
		mode os.FileMode
	}{{filepath.Join(dir, BackupDir), 0o700}, {backup, 0o600}} {
		info, err := os.Stat(p.path)
		if err != nil || info.Mode().Perm() != p.mode {
			t.Errorf("%s mode = %v, %v; want %v", p.path, info.Mode().Perm(), err, p.mode)
		}
	}
}

func TestOpenDoesNotBackUpWhenNothingMigrates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	for i := 0; i < 2; i++ { // a new database, then one that is current
		s, err := Open(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
	if names := backupFiles(t, dir); len(names) != 0 {
		t.Errorf("backups = %v; want none", names)
	}
}

func TestOpenRefusesToMigrateWithoutBackup(t *testing.T) {
	dir, path := oldDatabase(t, 5)
	// A file where the folder should be makes the backup impossible, even
	// for a user who can write anywhere.
	if err := os.WriteFile(filepath.Join(dir, BackupDir), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenPath(context.Background(), path)
	if err == nil {
		s.Close()
		t.Fatal("OpenPath succeeded without a backup")
	}
	if !strings.Contains(err.Error(), "back up the database") || !strings.Contains(err.Error(), "was not changed") {
		t.Errorf("error = %q; want an actionable backup error", err)
	}
	if got := schemaOf(t, path); got != 5 {
		t.Errorf("database schema = %d; want it left at 5", got)
	}
}

func TestBackupsKeepTheNewestFive(t *testing.T) {
	dir, path := oldDatabase(t, 5)
	folder := filepath.Join(dir, BackupDir)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	var old []string
	for day := 1; day <= 6; day++ {
		name := fmt.Sprintf("burzblog-schema4-2026010%dT000000.000Z.db", day)
		old = append(old, name)
		if err := os.WriteFile(filepath.Join(folder, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Files that do not match the pattern are never deleted.
	others := []string{"notes.txt", "burzblog-schema4-latest.db", "burzblog-schema4-20260101T000000.000Z.db.bak"}
	for _, name := range others {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	got := map[string]bool{}
	for _, n := range backupFiles(t, dir) {
		got[n] = true
	}
	var kept int
	for _, n := range old {
		if got[n] {
			kept++
		}
	}
	if kept != 4 || got[old[0]] || got[old[1]] || !got[old[5]] {
		t.Errorf("kept %d old backups (%v); want the 4 newest of 6 plus the new one", kept, got)
	}
	for _, n := range others {
		if !got[n] {
			t.Errorf("%s was deleted; only pre-upgrade backups may be pruned", n)
		}
	}
	matching := 0
	for n := range got {
		if backupName.MatchString(n) {
			matching++
		}
	}
	if matching != keepBackups {
		t.Errorf("%d backups kept; want %d", matching, keepBackups)
	}
}
