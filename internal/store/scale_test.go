package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func TestListPostsPages(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for _, slug := range []string{"a", "b", "c", "d", "e"} {
		if _, err := s.CreatePost(ctx, &post.Post{Slug: slug, Title: slug}, "u"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "old", Title: "old", Status: post.Archived}, "u"); err != nil {
		t.Fatal(err)
	}
	page := func(f Filter) (string, int) {
		t.Helper()
		list, counts, err := s.ListPosts(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var slugs []string
		for _, sm := range list {
			slugs = append(slugs, sm.Slug)
		}
		return strings.Join(slugs, ","), counts.Matched
	}
	for _, tc := range []struct {
		name    string
		filter  Filter
		want    string
		matched int
	}{
		{"all", Filter{Sort: SortTitle}, "a,b,c,d,e", 5},
		{"first page", Filter{Sort: SortTitle, Limit: 2}, "a,b", 5},
		{"second page", Filter{Sort: SortTitle, Limit: 2, Offset: 2}, "c,d", 5},
		{"last page", Filter{Sort: SortTitle, Limit: 2, Offset: 4}, "e", 5},
		{"past the end", Filter{Sort: SortTitle, Limit: 2, Offset: 10}, "", 5},
		{"search", Filter{Sort: SortTitle, Limit: 1, Query: "c"}, "c", 1},
		{"archived", Filter{Status: post.Archived, Limit: 1}, "old", 1},
	} {
		if got, matched := page(tc.filter); got != tc.want || matched != tc.matched {
			t.Errorf("%s = %q (%d matched), want %q (%d)", tc.name, got, matched, tc.want, tc.matched)
		}
	}
}

func TestWordCountsFollowEdits(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, err := s.CreatePost(ctx, &post.Post{Slug: "w", Title: "W", Body: "one two three"}, "u")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePost(ctx, "w", p.Version, "u", func(p *post.Post) error { p.Body = "one two"; return nil }); err != nil {
		t.Fatal(err)
	}
	list, _, err := s.ListPosts(ctx, Filter{})
	if err != nil || len(list) != 1 || list[0].Words != 2 {
		t.Errorf("list words = %+v, %v; want 2", list, err)
	}
	revs, err := s.Revisions(ctx, "w")
	if err != nil || len(revs) != 1 || revs[0].Words != 3 {
		t.Errorf("revision words = %+v, %v; want 3, the count before the edit", revs, err)
	}
}

// Posts saved before the words columns existed get their counts when the
// database is migrated.
func TestMigrationBackfillsWordCounts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), FileName)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		migrations[0], migrations[1],
		`INSERT INTO posts (slug, title, status, body, created_at, updated_at) VALUES ('old', 'Old', 'draft', 'one two three four', 'x', 'x')`,
		`INSERT INTO revisions (post_id, version, slug, title, description, status, body, cover_url, cover_alt, extra, saved_at, saved_by)
			VALUES (1, 1, 'old', 'Old', '', 'draft', 'one two', '', '', '', 'x', 'u')`,
		"PRAGMA user_version = 2",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, _, err := s.ListPosts(ctx, Filter{})
	if err != nil || len(list) != 1 || list[0].Words != 4 {
		t.Errorf("post words = %+v, %v; want 4", list, err)
	}
	revs, err := s.Revisions(ctx, "old")
	if err != nil || len(revs) != 1 || revs[0].Words != 2 {
		t.Errorf("revision words = %+v, %v; want 2", revs, err)
	}
}

func TestForEachPostStreamsInOrder(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for _, p := range []*post.Post{
		{Slug: "b", Title: "B", Status: post.Published},
		{Slug: "a", Title: "A", Status: post.Published},
		{Slug: "c", Title: "C"},
	} {
		if _, err := s.CreatePost(ctx, p, "u"); err != nil {
			t.Fatal(err)
		}
	}
	collect := func(each func(context.Context, func(*post.Post) error) error) string {
		var slugs []string
		if err := each(ctx, func(p *post.Post) error { slugs = append(slugs, p.Slug); return nil }); err != nil {
			t.Fatal(err)
		}
		return strings.Join(slugs, ",")
	}
	if got := collect(s.ForEachPublished); got != "a,b" {
		t.Errorf("published = %s, want a,b", got)
	}
	if got := collect(s.ForEachPost); got != "c,a,b" {
		t.Errorf("all = %s, want c,a,b (by status, then slug)", got)
	}
	stop := fmt.Errorf("stop")
	if err := s.ForEachPost(ctx, func(*post.Post) error { return stop }); err != stop {
		t.Errorf("callback error = %v, want it returned", err)
	}
}

func TestForEachPostReadsInBatches(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	n := 2*forEachBatch + 7
	for i := range n {
		status := post.Draft
		if i%3 == 0 {
			status = post.Published
		}
		if _, err := s.CreatePost(ctx, &post.Post{Slug: fmt.Sprintf("p-%03d", i), Title: "T", Status: status}, "u"); err != nil {
			t.Fatal(err)
		}
	}
	// Every connection is taken while the callbacks run, so a batch that
	// kept its query open would deadlock here.
	s.db.SetMaxOpenConns(1)
	seen, last := 0, ""
	err := s.ForEachPost(ctx, func(p *post.Post) error {
		key := string(p.Status) + "/" + p.Slug
		if key <= last {
			t.Fatalf("out of order: %s after %s", key, last)
		}
		last = key
		seen++
		_, err := s.GetPost(ctx, p.Slug)
		return err
	})
	if err != nil || seen != n {
		t.Errorf("saw %d of %d posts: %v", seen, n, err)
	}
}

// queryPlan returns EXPLAIN QUERY PLAN for query as one string.
func queryPlan(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	return strings.Join(plan, "; ")
}

func TestListQueriesUseIndexes(t *testing.T) {
	s := open(t)
	// A status-filtered list reads its status range, not the whole table.
	plan := queryPlan(t, s, "SELECT slug FROM posts WHERE status = ? ORDER BY updated_at DESC, id DESC LIMIT 100", "published")
	if !strings.Contains(plan, "USING INDEX posts_status_updated") {
		t.Errorf("status list plan = %s", plan)
	}
	// The status counts come from the index alone.
	plan = queryPlan(t, s, "SELECT status, count(*) FROM posts GROUP BY status")
	if !strings.Contains(plan, "COVERING INDEX posts_status_updated") {
		t.Errorf("count plan = %s", plan)
	}
	// Revisions of a post are found by (post_id, version).
	plan = queryPlan(t, s, "SELECT version FROM revisions WHERE post_id = ? ORDER BY version DESC LIMIT 200", 1)
	if !strings.Contains(plan, "INDEX sqlite_autoindex_revisions_1") || strings.Contains(plan, "TEMP B-TREE") {
		t.Errorf("revisions plan = %s", plan)
	}
}

func BenchmarkListPosts(b *testing.B) {
	ctx := context.Background()
	s, err := Open(ctx, b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	body := strings.Repeat("lorem ipsum dolor sit amet consectetur ", 750)
	for i := 0; i < 1500; i++ {
		if _, err := s.CreatePost(ctx, &post.Post{Slug: fmt.Sprintf("post-%04d", i), Title: fmt.Sprintf("Post %d", i), Body: body}, "u"); err != nil {
			b.Fatal(err)
		}
	}
	for name, f := range map[string]Filter{"all": {}, "first page": {Limit: 100}, "search": {Query: "post 14", Limit: 100}} {
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, _, err := s.ListPosts(ctx, f); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
