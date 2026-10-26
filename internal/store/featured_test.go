package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func TestMigrationMovesFeaturedOutOfExtra(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), FileName)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	// A database at schema 10, as the previous release left it.
	for i := 0; i < 10; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			t.Fatal(err)
		}
	}
	insert := `INSERT INTO posts (slug, title, status, body, extra, created_at, updated_at, version)
		VALUES (?, ?, 'published', '', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 3)`
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{insert, []any{"lead", "Lead", "sitemap:\n  priority: 1\nfeatured: true\n"}},
		{insert, []any{"off", "Off", "featured: false\n"}},
		{insert, []any{"odd", "Odd", "featured: maybe\n"}},
		{insert, []any{"plain", "Plain", "sitemap:\n  priority: 2\n"}},
		{`INSERT INTO revisions (post_id, version, slug, title, description, status, body, cover_url, cover_alt, extra, saved_at, saved_by)
			VALUES (1, 2, 'lead', 'Lead', '', 'draft', '', '', '', 'featured: true', 'x', 'u')`, nil},
		{"PRAGMA user_version = 10", nil},
	} {
		if _, err := db.Exec(stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) || version != 12 {
		t.Fatalf("schema version = %d, %v; want 12", version, err)
	}

	lead, err := s.GetPost(ctx, "lead")
	if err != nil {
		t.Fatal(err)
	}
	if !lead.Featured || strings.Contains(lead.Extra, "featured") || !strings.Contains(lead.Extra, "priority: 1") || lead.Version != 3 {
		t.Errorf("lead = featured %v extra %q version %d", lead.Featured, lead.Extra, lead.Version)
	}
	if off, _ := s.GetPost(ctx, "off"); off.Featured || off.Extra != "" {
		t.Errorf("off = featured %v extra %q", off.Featured, off.Extra)
	}
	// A value that is not a boolean stays where it was.
	if odd, _ := s.GetPost(ctx, "odd"); odd.Featured || odd.Extra != "featured: maybe\n" {
		t.Errorf("odd = featured %v extra %q", odd.Featured, odd.Extra)
	}
	if plain, _ := s.GetPost(ctx, "plain"); plain.Featured || plain.Extra != "sitemap:\n  priority: 2\n" {
		t.Errorf("plain = featured %v extra %q", plain.Featured, plain.Extra)
	}
	rev, err := s.Revision(ctx, "lead", 2)
	if err != nil || !rev.Post.Featured || rev.Post.Extra != "" {
		t.Errorf("revision = %+v, %v", rev, err)
	}
	// The migrated post saves again, and the new tables work.
	if _, err := s.UpdatePost(ctx, "lead", 3, "me", func(p *post.Post) error { p.Body = "x"; return nil }); err != nil {
		t.Errorf("saving a migrated post: %v", err)
	}
	if _, err := s.db.Exec("INSERT INTO tag_labels (tag, label, updated_at) VALUES ('a', 'A', 'x')"); err != nil {
		t.Errorf("tag_labels after migration: %v", err)
	}
}

func TestFeaturedIsVersionedWithRevisions(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Featured: true}, "me")
	if err != nil || !p.Featured {
		t.Fatalf("create = %+v, %v", p, err)
	}
	p, err = s.UpdatePost(ctx, "a", p.Version, "me", func(p *post.Post) error { p.Featured = false; return nil })
	if err != nil || p.Featured || p.Version != 2 {
		t.Fatalf("unfeature = %+v, %v; want a new version", p, err)
	}
	// Saving the same flag again is not a change.
	if same, err := s.UpdatePost(ctx, "a", 2, "me", func(p *post.Post) error { p.Featured = false; return nil }); err != nil || same.Version != 2 {
		t.Errorf("unchanged save = %+v, %v", same, err)
	}
	rev, err := s.Revision(ctx, "a", 1)
	if err != nil || !rev.Post.Featured {
		t.Errorf("revision 1 = %+v, %v; want featured", rev, err)
	}
	revs, err := s.Revisions(ctx, "a")
	if err != nil || len(revs) != 1 || !revs[0].Post.Featured {
		t.Errorf("revisions = %+v, %v", revs, err)
	}
}

func TestFeaturedFilters(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for _, c := range []struct {
		slug     string
		featured bool
		status   post.Status
	}{
		{"lead", true, post.Published}, {"second", false, post.Published},
		{"hidden-lead", true, post.Draft}, {"old-lead", true, post.Archived},
	} {
		if _, err := s.CreatePost(ctx, &post.Post{Slug: c.slug, Title: c.slug, Featured: c.featured, Status: c.status}, "me"); err != nil {
			t.Fatal(err)
		}
	}
	yes, no := true, false
	slugs := func(list []Listing) []string {
		out := []string{}
		for _, l := range list {
			out = append(out, l.Post.Slug)
		}
		return out
	}
	list, total, err := s.ListPublishedFeatured(ctx, "", "", &yes, 0, 0)
	if err != nil || total != 1 || !reflect.DeepEqual(slugs(list), []string{"lead"}) || !list[0].Post.Featured {
		t.Errorf("featured = %v %d, %v; want only the published lead", slugs(list), total, err)
	}
	list, total, err = s.ListPublishedFeatured(ctx, "", "", &no, 0, 0)
	if err != nil || total != 1 || !reflect.DeepEqual(slugs(list), []string{"second"}) {
		t.Errorf("not featured = %v %d, %v", slugs(list), total, err)
	}
	if _, total, _ = s.ListPublishedFeatured(ctx, "", "", nil, 0, 0); total != 2 {
		t.Errorf("no filter total = %d, want 2", total)
	}
	if _, total, _ = s.ListPublished(ctx, "", "", 0, 0); total != 2 {
		t.Errorf("ListPublished total = %d, want 2", total)
	}
	// Combined with a search.
	if list, total, _ = s.ListPublishedFeatured(ctx, "lead", "", &yes, 0, 0); total != 1 || list[0].Post.Slug != "lead" {
		t.Errorf("search + featured = %v %d", slugs(list), total)
	}
	if _, total, _ = s.ListPublishedFeatured(ctx, "second", "", &yes, 0, 0); total != 0 {
		t.Errorf("search for a non-featured post with featured = %d, want 0", total)
	}

	all, _, err := s.ListPosts(ctx, Filter{Featured: &yes})
	if err != nil || len(all) != 2 {
		t.Errorf("ListPosts featured = %+v, %v; want lead and hidden-lead (archived is hidden)", all, err)
	}
	for _, sm := range all {
		if !sm.Featured {
			t.Errorf("summary %s is not featured", sm.Slug)
		}
	}
	if all, _, _ = s.ListPosts(ctx, Filter{Featured: &yes, Status: post.Archived}); len(all) != 1 || all[0].Slug != "old-lead" {
		t.Errorf("archived featured = %+v", all)
	}
}

func TestTagLabels(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Tags: []string{"constitutional-law"}}, "me"); err != nil {
		t.Fatal(err)
	}
	if labels, err := s.TagLabels(ctx); err != nil || len(labels) != 0 {
		t.Errorf("fresh labels = %v, %v", labels, err)
	}
	got, err := s.SetTagLabel(ctx, "constitutional-law", "  Verfassungsrecht  ", "ada")
	if err != nil || got != "Verfassungsrecht" {
		t.Fatalf("SetTagLabel = %q, %v", got, err)
	}
	if _, err := s.SetTagLabel(ctx, "constitutional-law", "Constitutional Law, India", "ada"); err != nil {
		t.Fatal(err)
	}
	if labels, _ := s.TagLabels(ctx); !reflect.DeepEqual(labels, map[string]string{"constitutional-law": "Constitutional Law, India"}) {
		t.Errorf("labels = %v, want the second label to replace the first", labels)
	}
	if _, err := s.SetTagLabel(ctx, "never-used", "Nope", "ada"); !errors.Is(err, ErrNotFound) {
		t.Errorf("label for an unused tag = %v, want ErrNotFound", err)
	}
	for _, bad := range []string{"", "  ", strings.Repeat("x", post.MaxTagLabel+1), "a\nb"} {
		if _, err := s.SetTagLabel(ctx, "constitutional-law", bad, "ada"); !post.IsFieldError(err) {
			t.Errorf("SetTagLabel(%q) = %v, want a field error", bad, err)
		}
	}
	if labels, _ := s.TagLabels(ctx); labels["constitutional-law"] != "Constitutional Law, India" {
		t.Errorf("a refused label changed the stored one: %v", labels)
	}
	if err := s.ClearTagLabel(ctx, "constitutional-law"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearTagLabel(ctx, "constitutional-law"); err != nil {
		t.Errorf("clearing twice = %v, want nil", err)
	}
	if labels, _ := s.TagLabels(ctx); len(labels) != 0 {
		t.Errorf("labels after clear = %v", labels)
	}
}
