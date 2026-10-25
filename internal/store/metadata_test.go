package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func TestCreatePostKeepsMetadata(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, err := s.CreatePost(ctx, &post.Post{
		Slug: "m", Title: "M", Tags: []string{"Go", "Web Dev", "go"}, Author: "  Ada  ",
		SEOTitle: "SEO", SEODescription: "Desc", CanonicalURL: "https://example.com/m",
	}, "grace")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go", "web-dev"}
	if !reflect.DeepEqual(p.Tags, want) || p.Author != "Ada" || p.SEOTitle != "SEO" ||
		p.SEODescription != "Desc" || p.CanonicalURL != "https://example.com/m" {
		t.Errorf("created = %+v", p)
	}
	got, err := s.GetPost(ctx, "m")
	if err != nil || !reflect.DeepEqual(got.Tags, want) || got.Author != "Ada" || got.CanonicalURL != p.CanonicalURL {
		t.Errorf("GetPost = %+v, %v", got, err)
	}
}

func TestCreatePostDefaultsAuthorToCreator(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A"}, "grace")
	if err != nil || p.Author != "grace" {
		t.Errorf("author = %q, %v; want grace", p.Author, err)
	}
	if len(p.Tags) != 0 {
		t.Errorf("tags = %v, want none", p.Tags)
	}
	// Clearing the author on a later save is respected.
	p, err = s.UpdatePost(ctx, "a", p.Version, "grace", func(p *post.Post) error { p.Author = ""; return nil })
	if err != nil || p.Author != "" {
		t.Errorf("author after clearing = %q, %v", p.Author, err)
	}
}

func TestCreatePostRejectsBadMetadata(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	var many []string
	for i := 0; i <= post.MaxTags; i++ {
		many = append(many, fmt.Sprintf("tag%d", i))
	}
	for name, p := range map[string]*post.Post{
		"many tags":      {Slug: "a", Title: "A", Tags: many},
		"long tag":       {Slug: "b", Title: "B", Tags: []string{strings.Repeat("x", post.MaxTag+1)}},
		"http canonical": {Slug: "c", Title: "C", CanonicalURL: "http://example.com"},
		"seo newline":    {Slug: "d", Title: "D", SEOTitle: "a\nb"},
	} {
		if _, err := s.CreatePost(ctx, p, "me"); !post.IsFieldError(err) {
			t.Errorf("%s: CreatePost = %v, want a FieldError", name, err)
		}
	}
	if list, _, _ := s.ListPosts(ctx, Filter{}); len(list) != 0 {
		t.Errorf("rejected posts were stored: %v", list)
	}
}

func TestUpdatePostTagsAndRevisions(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Tags: []string{"one", "two"}, Author: "ada", SEOTitle: "old"}, "me")
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.UpdatePost(ctx, "a", p.Version, "me", func(p *post.Post) error {
		p.Tags = []string{"three", "one"}
		p.SEOTitle = "new"
		p.Author = "bob"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Tags, []string{"three", "one"}) || p.Version != 2 {
		t.Errorf("updated = %+v", p)
	}
	got, _ := s.GetPost(ctx, "a")
	if !reflect.DeepEqual(got.Tags, []string{"three", "one"}) || got.Author != "bob" || got.SEOTitle != "new" {
		t.Errorf("stored = %+v", got)
	}
	rev, err := s.Revision(ctx, "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rev.Post.Tags, []string{"one", "two"}) || rev.Post.Author != "ada" || rev.Post.SEOTitle != "old" {
		t.Errorf("revision lost the earlier metadata: %+v", rev.Post)
	}
	revs, err := s.Revisions(ctx, "a")
	if err != nil || len(revs) != 1 || !reflect.DeepEqual(revs[0].Post.Tags, []string{"one", "two"}) {
		t.Errorf("Revisions = %+v, %v", revs, err)
	}
	// A tag change alone is a change; the same tags again are not.
	again, err := s.UpdatePost(ctx, "a", p.Version, "me", func(p *post.Post) error { p.Tags = []string{"Three", "one"}; return nil })
	if err != nil || again.Version != p.Version {
		t.Errorf("same tags saved a new version: %+v, %v", again, err)
	}
	reordered, err := s.UpdatePost(ctx, "a", p.Version, "me", func(p *post.Post) error { p.Tags = []string{"one", "three"}; return nil })
	if err != nil || reordered.Version != p.Version+1 || !reflect.DeepEqual(reordered.Tags, []string{"one", "three"}) {
		t.Errorf("reordering tags = %+v, %v", reordered, err)
	}
}

func TestUpdatePostRejectsBadMetadataAndKeepsOldTags(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, _ := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Tags: []string{"keep"}}, "me")
	_, err := s.UpdatePost(ctx, "a", p.Version, "me", func(p *post.Post) error {
		p.Tags = []string{"new"}
		p.CanonicalURL = "ftp://example.com"
		return nil
	})
	if !post.IsFieldError(err) {
		t.Fatalf("UpdatePost = %v, want a FieldError", err)
	}
	got, _ := s.GetPost(ctx, "a")
	if !reflect.DeepEqual(got.Tags, []string{"keep"}) || got.Version != p.Version {
		t.Errorf("failed save changed the post: %+v", got)
	}
	// A stale version is still refused, with the tags as they are now.
	_, err = s.UpdatePost(ctx, "a", p.Version+5, "me", func(p *post.Post) error { p.Tags = nil; return nil })
	if !errors.Is(err, ErrConflict) {
		t.Errorf("stale save = %v, want ErrConflict", err)
	}
}

func TestListPostsByTag(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		tags := []string{"all"}
		if i%2 == 0 {
			tags = append(tags, "even")
		}
		p := &post.Post{Slug: fmt.Sprintf("p%d", i), Title: fmt.Sprintf("P%d", i), Tags: tags}
		if i == 4 {
			p.Status = post.Published
		}
		if _, err := s.CreatePost(ctx, p, "me"); err != nil {
			t.Fatal(err)
		}
	}
	list, counts, err := s.ListPosts(ctx, Filter{Tag: "even", Sort: SortTitle})
	if err != nil || len(list) != 3 || counts.Matched != 3 || list[0].Slug != "p0" {
		t.Fatalf("even = %+v %+v %v", list, counts, err)
	}
	if !reflect.DeepEqual(list[0].Tags, []string{"all", "even"}) || list[0].Author != "me" {
		t.Errorf("summary tags/author = %v %q", list[0].Tags, list[0].Author)
	}
	// Paging reports the total of the filtered list.
	list, counts, err = s.ListPosts(ctx, Filter{Tag: "even", Sort: SortTitle, Limit: 2, Offset: 1})
	if err != nil || len(list) != 2 || counts.Matched != 3 || list[0].Slug != "p2" || !reflect.DeepEqual(list[1].Tags, []string{"all", "even"}) {
		t.Errorf("paged = %+v %+v %v", list, counts, err)
	}
	// Tag filtering combines with status and ignores case and spacing.
	list, _, err = s.ListPosts(ctx, Filter{Tag: " EVEN ", Status: post.Published})
	if err != nil || len(list) != 1 || list[0].Slug != "p4" {
		t.Errorf("published even = %+v %v", list, err)
	}
	// Counts ignore the tag filter, like they ignore the other filters.
	if _, counts, _ = s.ListPosts(ctx, Filter{Tag: "even"}); counts.Draft != 4 || counts.Published != 1 {
		t.Errorf("counts = %+v", counts)
	}
	for _, tag := range []string{"missing", "eve", "!!!"} {
		list, counts, err = s.ListPosts(ctx, Filter{Tag: tag})
		if err != nil || len(list) != 0 || counts.Matched != 0 {
			t.Errorf("tag %q = %+v %+v %v, want nothing", tag, list, counts, err)
		}
	}
	// Untagged posts come back with no tags, not someone else's.
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "bare", Title: "Bare"}, "me"); err != nil {
		t.Fatal(err)
	}
	list, _, _ = s.ListPosts(ctx, Filter{Query: "bare"})
	if len(list) != 1 || len(list[0].Tags) != 0 {
		t.Errorf("bare post = %+v", list)
	}
}

func TestTagFilterUsesIndex(t *testing.T) {
	s := open(t)
	plan := queryPlan(t, s, "SELECT slug FROM posts WHERE status != 'archived' AND id IN (SELECT post_id FROM post_tags WHERE tag = ?)", "go")
	if !strings.Contains(plan, "post_tags_tag") {
		t.Errorf("tag filter plan = %s", plan)
	}
}

func TestDeletePostRemovesTags(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, _ := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Tags: []string{"x"}}, "me")
	p, _ = s.UpdatePost(ctx, "a", p.Version, "me", func(p *post.Post) error { p.Status = post.Archived; return nil })
	if err := s.DeletePost(ctx, "a", p.Version); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM post_tags").Scan(&n); err != nil || n != 0 {
		t.Errorf("post_tags rows after delete = %d, %v", n, err)
	}
	// The slug can be reused with other tags.
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Tags: []string{"y"}}, "me"); err != nil {
		t.Fatal(err)
	}
}

func TestForEachPostCarriesTags(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for i := 0; i < forEachBatch+5; i++ {
		tags := []string{fmt.Sprintf("t%d", i)}
		if _, err := s.CreatePost(ctx, &post.Post{Slug: fmt.Sprintf("p%03d", i), Title: "T", Tags: tags, Status: post.Published}, "me"); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	err := s.ForEachPublished(ctx, func(p *post.Post) error {
		i, _ := strconv.Atoi(strings.TrimPrefix(p.Slug, "p"))
		if want := []string{fmt.Sprintf("t%d", i)}; !reflect.DeepEqual(p.Tags, want) {
			t.Errorf("%s tags = %v, want %v", p.Slug, p.Tags, want)
		}
		n++
		return nil
	})
	if err != nil || n != forEachBatch+5 {
		t.Errorf("visited %d, %v", n, err)
	}
}

// Posts saved before blog metadata existed carry tags, author, seo and
// canonical inside extra; migration 4 moves them into the new fields.
func TestMigrationMovesMetadataOutOfExtra(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), FileName)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := 0; i <= post.MaxTags; i++ {
		names = append(names, fmt.Sprintf("t%d", i))
	}
	many := "tags: [" + strings.Join(names, ", ") + "]\n"
	insert := `INSERT INTO posts (slug, title, status, body, extra, created_at, updated_at) VALUES (?, ?, 'draft', '', ?, 'x', 'x')`
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{migrations[0], nil}, {migrations[1], nil}, {migrations[2], nil},
		{insert, []any{"movable", "Movable", "sitemap:\n  priority: 1\ntags:\n  - Go\n  - Web Dev\nauthor: Ada\ncanonical: https://example.com/m\nseo:\n  title: T\n  description: D\n  robots: noindex\n"}},
		{insert, []any{"plain", "Plain", "sitemap:\n  priority: 2\n"}},
		{insert, []any{"toomany", "Too many", many}},
		{insert, []any{"badseo", "Bad", "seo: just text\n"}},
		{`INSERT INTO revisions (post_id, version, slug, title, description, status, body, cover_url, cover_alt, extra, saved_at, saved_by)
			VALUES (1, 0, 'movable', 'Movable', '', 'draft', '', '', '', ?, 'x', 'u')`, []any{"tags: [Old]\nauthor: Bob\n"}},
		{"PRAGMA user_version = 3", nil},
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

	m, err := s.GetPost(ctx, "movable")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Tags, []string{"go", "web-dev"}) || m.Author != "Ada" || m.SEOTitle != "T" ||
		m.SEODescription != "D" || m.CanonicalURL != "https://example.com/m" {
		t.Errorf("moved post = %+v", m)
	}
	for _, want := range []string{"sitemap:", "priority: 1", "robots: noindex"} {
		if !strings.Contains(m.Extra, want) {
			t.Errorf("extra %q lost %q", m.Extra, want)
		}
	}
	for _, gone := range []string{"tags", "author", "canonical", "title", "description"} {
		if strings.Contains(m.Extra, gone) {
			t.Errorf("extra %q still has %q", m.Extra, gone)
		}
	}
	if m.Version != 1 {
		t.Errorf("migration changed the version to %d", m.Version)
	}
	// The moved post can be saved again and filtered by tag.
	if _, err := s.UpdatePost(ctx, "movable", m.Version, "me", func(p *post.Post) error { p.Body = "x"; return nil }); err != nil {
		t.Errorf("saving a migrated post: %v", err)
	}
	if list, _, _ := s.ListPosts(ctx, Filter{Tag: "web-dev"}); len(list) != 1 || list[0].Slug != "movable" {
		t.Errorf("tag filter after migration = %+v", list)
	}

	if plain, _ := s.GetPost(ctx, "plain"); plain.Extra != "sitemap:\n  priority: 2\n" || len(plain.Tags) != 0 || plain.Author != "" {
		t.Errorf("plain post changed: %+v", plain)
	}
	// Extras that cannot be used as they are stay where they were.
	if tm, _ := s.GetPost(ctx, "toomany"); tm.Extra != many || len(tm.Tags) != 0 {
		t.Errorf("too-many-tags post = %+v", tm)
	}
	if bad, _ := s.GetPost(ctx, "badseo"); bad.Extra != "seo: just text\n" {
		t.Errorf("badseo extra = %q", bad.Extra)
	}

	rev, err := s.Revision(ctx, "movable", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rev.Post.Tags, []string{"old"}) || rev.Post.Author != "Bob" || rev.Post.Extra != "" {
		t.Errorf("revision = %+v", rev.Post)
	}
}

func TestTagsCounts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if tags, err := s.Tags(ctx); err != nil || len(tags) != 0 {
		t.Errorf("empty Tags = %v, %v", tags, err)
	}
	s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Tags: []string{"go", "web"}}, "me")
	s.CreatePost(ctx, &post.Post{Slug: "b", Title: "B", Tags: []string{"go"}}, "me")
	want := []TagCount{{"go", 2}, {"web", 1}}
	if tags, err := s.Tags(ctx); err != nil || !reflect.DeepEqual(tags, want) {
		t.Errorf("Tags = %v, %v; want %v", tags, err, want)
	}
}
