// Package store keeps BurzBlog's state in one SQLite file: posts, their
// revisions, accounts, sessions and API tokens.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	// Pure-Go SQLite driver, so one cross-compiled binary runs on a Pi.
	_ "modernc.org/sqlite"
)

// Errors callers can branch on.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("changed since it was loaded")
	ErrTaken    = errors.New("already exists")
	ErrState    = errors.New("not allowed in the current state")
)

// Store is a handle on the database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// FileName is the database file inside the data directory.
const FileName = "burzblog.db"

// Open creates the data directory if needed and opens or creates the
// database, applying migrations.
func Open(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	path := filepath.Join(dir, FileName)
	return OpenPath(ctx, path)
}

// OpenPath opens the database at an explicit path. ":memory:" is not
// supported because each pooled connection would see a different database.
func OpenPath(ctx context.Context, path string) (*Store, error) {
	q := url.Values{}
	for _, p := range []string{"busy_timeout(10000)", "journal_mode(WAL)", "foreign_keys(1)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	// The file holds password hashes; keep it private to the service user.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			db.Close()
			return nil, fmt.Errorf("restrict database permissions: %w", err)
		}
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Now returns the store's current time.
func (s *Store) Now() time.Time {
	return s.now()
}

// SetClock replaces the time source; tests use it.
func (s *Store) SetClock(now func() time.Time) {
	s.now = now
}

// Ping checks the database answers.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

var migrations = []string{
	`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		username TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (length(username) BETWEEN 1 AND 64),
		password_hash TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE posts (
		id INTEGER PRIMARY KEY,
		slug TEXT NOT NULL UNIQUE CHECK (length(slug) BETWEEN 1 AND 120),
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL CHECK (status IN ('draft', 'published', 'archived')),
		body TEXT NOT NULL DEFAULT '',
		cover_url TEXT NOT NULL DEFAULT '',
		cover_alt TEXT NOT NULL DEFAULT '',
		extra TEXT NOT NULL DEFAULT '',
		published_on TEXT,
		updated_on TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		updated_by TEXT NOT NULL DEFAULT '',
		version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1)
	);
	CREATE INDEX posts_status_updated ON posts (status, updated_at DESC);
	CREATE TABLE revisions (
		id INTEGER PRIMARY KEY,
		post_id INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
		version INTEGER NOT NULL,
		slug TEXT NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		status TEXT NOT NULL,
		body TEXT NOT NULL,
		cover_url TEXT NOT NULL,
		cover_alt TEXT NOT NULL,
		extra TEXT NOT NULL,
		published_on TEXT,
		updated_on TEXT,
		saved_at TEXT NOT NULL,
		saved_by TEXT NOT NULL,
		UNIQUE (post_id, version)
	);
	CREATE TABLE sessions (
		token_hash TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
		csrf TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL
	);
	CREATE INDEX sessions_user ON sessions (user_id, expires_at);
	CREATE TABLE tokens (
		id INTEGER PRIMARY KEY,
		token_hash TEXT NOT NULL UNIQUE,
		user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
		name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
		scope TEXT NOT NULL CHECK (scope IN ('write', 'export')),
		created_at TEXT NOT NULL,
		last_used_at TEXT
	);
	CREATE TABLE login_attempts (
		key TEXT PRIMARY KEY,
		window_start INTEGER NOT NULL,
		failures INTEGER NOT NULL
	);`,
	// Scheduled publishing: a draft with publish_at set publishes itself then.
	// Revisions keep content history only, so they do not mirror these.
	`ALTER TABLE posts ADD COLUMN publish_at TEXT;
	ALTER TABLE posts ADD COLUMN publish_by TEXT NOT NULL DEFAULT '';`,
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema %d is newer than this BurzBlog (%d); upgrade BurzBlog", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", i+1, err)
		}
	}
	return nil
}

func (s *Store) stamp() string {
	return s.now().Format(time.RFC3339Nano)
}

func parseStamp(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

func nullTime(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.Format(time.RFC3339Nano), Valid: true}
}

func timePtr(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, v.String)
	if err != nil {
		return nil
	}
	return &t
}
