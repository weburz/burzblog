package store

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// BackupDir is the folder inside the data directory that holds the copies
// made before a schema upgrade.
const BackupDir = "backups"

// keepBackups is how many pre-upgrade copies are kept.
const keepBackups = 5

// backupName matches only the files backupBeforeMigrating writes, so
// retention never touches anything else in the folder. The timestamp sorts
// in time order as text.
var backupName = regexp.MustCompile(`^burzblog-schema\d+-(\d{8}T\d{6}\.\d{3}Z)\.db$`)

// backupBeforeMigrating writes a consistent copy of the database, still at
// schema version, to <data>/backups/burzblog-schema<version>-<UTC time>.db
// with VACUUM INTO, which is safe on a database in WAL mode where a plain
// file copy is not. It refuses to let the migration go on when the copy
// cannot be made.
func (s *Store) backupBeforeMigrating(ctx context.Context, version int) error {
	dir := filepath.Join(filepath.Dir(s.path), BackupDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return backupError(dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return backupError(dir, err)
	}
	name := fmt.Sprintf("burzblog-schema%d-%s.db", version, s.now().UTC().Format("20060102T150405.000Z"))
	dest := filepath.Join(dir, name)
	quoted := "'" + strings.ReplaceAll(dest, "'", "''") + "'"
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO "+quoted); err != nil {
		os.Remove(dest)
		return backupError(dest, err)
	}
	if err := os.Chmod(dest, 0o600); err != nil {
		os.Remove(dest)
		return backupError(dest, err)
	}
	slog.Info("database backed up before upgrading its schema",
		"file", dest, "from_schema", version, "to_schema", len(migrations))
	pruneBackups(dir)
	return nil
}

func backupError(path string, err error) error {
	return fmt.Errorf("back up the database before upgrading it: %s: %w\n"+
		"The database was not changed. Free disk space or fix the permissions of the data directory, then start BurzBlog again", path, err)
}

// pruneBackups deletes all but the newest keepBackups pre-upgrade copies.
// A copy that cannot be deleted is logged and left alone.
func pruneBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("could not list old database backups", "dir", dir, "error", err)
		return
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && backupName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return backupName.FindStringSubmatch(names[i])[1] < backupName.FindStringSubmatch(names[j])[1]
	})
	for len(names) > keepBackups {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			slog.Warn("could not delete an old database backup", "file", names[0], "error", err)
		}
		names = names[1:]
	}
}
