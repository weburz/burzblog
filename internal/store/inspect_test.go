package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func userVersion(t *testing.T, s *Store) int {
	t.Helper()
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenExistingNeverMigrates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	// As if a newer binary opened the database of an older, running server.
	older := len(migrations) - 1
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", older)); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = OpenExisting(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if v := userVersion(t, s); v != older {
		t.Errorf("schema after OpenExisting = %d, want %d unchanged", v, older)
	}
	if backups, _ := os.ReadDir(filepath.Join(dir, BackupDir)); len(backups) != 0 {
		t.Errorf("OpenExisting made an upgrade backup: %v", backups)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := OpenExisting(ctx, dir); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("newer schema: %v", err)
	}

	empty := t.TempDir()
	if _, err := OpenExisting(ctx, empty); err == nil {
		t.Error("OpenExisting accepted a directory without a database")
	}
	if _, err := os.Stat(filepath.Join(empty, FileName)); !os.IsNotExist(err) {
		t.Errorf("OpenExisting created a database: %v", err)
	}
}

func TestInspectFile(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A"}, "ada"); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	if err := s.CopyTo(ctx, copyPath); err != nil {
		t.Fatal(err)
	}
	sum, err := InspectFile(ctx, copyPath)
	if err != nil || sum.Posts != 1 || sum.Schema != len(migrations) || sum.Current != len(migrations) {
		t.Errorf("summary = %+v, %v", sum, err)
	}
	if err := s.CopyTo(ctx, copyPath); err == nil {
		t.Error("CopyTo overwrote an existing file")
	}

	garbage := filepath.Join(t.TempDir(), "garbage.db")
	os.WriteFile(garbage, []byte(strings.Repeat("not a database ", 100)), 0o600)
	if _, err := InspectFile(ctx, garbage); !errors.Is(err, ErrDamaged) {
		t.Errorf("garbage: %v", err)
	}
	blank := filepath.Join(t.TempDir(), "blank.db")
	os.WriteFile(blank, nil, 0o600)
	if _, err := InspectFile(ctx, blank); !errors.Is(err, ErrDamaged) {
		t.Errorf("empty file: %v", err)
	}
}

func TestOpenSharedHoldsTheLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no flock on Windows")
	}
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenShared(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockExclusive(dir); !errors.Is(err, ErrInUse) {
		t.Errorf("restore while a store is open: %v", err)
	}
	s.Close()
	release, err := LockExclusive(dir)
	if err != nil {
		t.Fatalf("restore after Close: %v", err)
	}
	if _, err := OpenShared(ctx, dir); !errors.Is(err, ErrRestoring) {
		t.Errorf("open during a restore: %v", err)
	}
	if _, err := OpenExisting(ctx, dir); !errors.Is(err, ErrRestoring) {
		t.Errorf("open existing during a restore: %v", err)
	}
	release()

	// A lock file another user made (a root restore) only needs to be readable.
	if err := os.Chmod(filepath.Join(dir, LockFile), 0o444); err != nil {
		t.Fatal(err)
	}
	unlock, err := LockShared(dir)
	if err != nil {
		t.Fatalf("read-only lock file: %v", err)
	}
	unlock()
	if info, _ := os.Stat(filepath.Join(dir, LockFile)); info.Mode().Perm()&0o044 == 0 {
		t.Errorf("lock file mode = %v", info.Mode())
	}
}
