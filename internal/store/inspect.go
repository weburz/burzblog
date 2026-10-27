package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrDamaged means a database file failed SQLite's integrity check or is not
// a BurzBlog database.
var ErrDamaged = errors.New("the database file is damaged or not a BurzBlog database")

// FileSummary describes a database file that is not in use, such as the copy
// inside a snapshot.
type FileSummary struct {
	// Schema is the migration the file is at; Current is this BurzBlog's.
	Schema, Current int
	Posts           int
	Revisions       int
	Accounts        int
	// Media lists every upload the file records, which the snapshot holding
	// it must carry.
	Media []Media
}

// InspectFile checks the database file at path with SQLite's integrity check,
// refuses one with a newer schema than this BurzBlog knows, and summarises
// it. It does not migrate or change the content. The file must not be in use
// by a server.
func InspectFile(ctx context.Context, path string) (*FileSummary, error) {
	q := url.Values{}
	q.Add("_pragma", "query_only(1)")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	if len(problems) > 0 {
		if len(problems) > 3 {
			problems = append(problems[:3], "...")
		}
		return nil, fmt.Errorf("%w: %s", ErrDamaged, strings.Join(problems, "; "))
	}

	sum := &FileSummary{Current: len(migrations)}
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&sum.Schema); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	if sum.Schema < 1 {
		return nil, fmt.Errorf("%w: it has no BurzBlog schema", ErrDamaged)
	}
	if sum.Schema > sum.Current {
		return nil, fmt.Errorf("the database is at schema %d, newer than this BurzBlog (%d); upgrade BurzBlog first", sum.Schema, sum.Current)
	}
	for _, c := range []struct {
		n     *int
		table string
	}{{&sum.Posts, "posts"}, {&sum.Revisions, "revisions"}, {&sum.Accounts, "users"}} {
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+c.table).Scan(c.n); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
		}
	}
	// Uploads arrived with migration 9; an older file has none.
	var hasMedia int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'media'").Scan(&hasMedia); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	if hasMedia == 0 {
		return sum, nil
	}
	mrows, err := db.QueryContext(ctx, "SELECT "+mediaColumns+" FROM media ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	defer mrows.Close()
	for mrows.Next() {
		m, err := scanMedia(mrows)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
		}
		sum.Media = append(sum.Media, *m)
	}
	if err := mrows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	return sum, nil
}
