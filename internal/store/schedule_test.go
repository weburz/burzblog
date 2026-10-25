package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/post"
)

func TestMigrationAddsScheduleToExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), FileName)
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		migrations[0],
		"PRAGMA user_version = 1",
		`INSERT INTO posts (slug, title, status, created_at, updated_at) VALUES
			('old', 'Old', 'draft', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()

	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatalf("open an existing database: %v", err)
	}
	defer s.Close()
	p, err := s.GetPost(ctx, "old")
	if err != nil {
		t.Fatal(err)
	}
	if p.PublishAt != nil || p.PublishBy != "" {
		t.Errorf("existing post gained a schedule: %+v", p)
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) {
		t.Errorf("schema version = %d, %v; want %d", version, err, len(migrations))
	}
}

func schedule(at time.Time, by string) func(*post.Post) error {
	return func(p *post.Post) error {
		p.PublishAt, p.PublishBy = &at, by
		return nil
	}
}

func TestSchedulingIsAVersionedChange(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	p, _ := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Body: "text"}, "me")

	scheduled, err := s.UpdatePost(ctx, "a", p.Version, "sagar", schedule(at, "sagar"))
	if err != nil {
		t.Fatal(err)
	}
	if scheduled.Version != 2 || scheduled.PublishAt == nil || !scheduled.PublishAt.Equal(at) || scheduled.PublishBy != "sagar" {
		t.Fatalf("scheduled post = %+v", scheduled)
	}
	if _, err := s.UpdatePost(ctx, "a", 1, "sagar", schedule(at, "sagar")); !errors.Is(err, ErrConflict) {
		t.Errorf("stale schedule = %v, want ErrConflict", err)
	}
	revs, _ := s.Revisions(ctx, "a")
	if len(revs) != 1 || revs[0].Version != 1 {
		t.Errorf("revisions = %+v, want the version before scheduling", revs)
	}

	cleared, err := s.UpdatePost(ctx, "a", 2, "sagar", func(p *post.Post) error { p.PublishAt = nil; return nil })
	if err != nil || cleared.PublishAt != nil || cleared.PublishBy != "" {
		t.Errorf("unschedule = %+v, %v", cleared, err)
	}
}

func TestPublishingOrArchivingClearsSchedule(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	for _, status := range []post.Status{post.Published, post.Archived} {
		slug := string(status)
		p, _ := s.CreatePost(ctx, &post.Post{Slug: slug, Title: slug}, "me")
		p, err := s.UpdatePost(ctx, slug, p.Version, "me", schedule(at, "me"))
		if err != nil {
			t.Fatal(err)
		}
		p, err = s.UpdatePost(ctx, slug, p.Version, "me", func(p *post.Post) error { p.Status = status; return nil })
		if err != nil {
			t.Fatal(err)
		}
		if p.PublishAt != nil || p.PublishBy != "" {
			t.Errorf("%s post kept its schedule: %+v", status, p)
		}
	}
	if list, err := s.ScheduledPosts(ctx); err != nil || len(list) != 0 {
		t.Errorf("ScheduledPosts = %+v, %v; want none", list, err)
	}
}

func TestScheduledPostsAndFilter(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	times := map[string]time.Time{"later": base.Add(time.Hour), "sooner": base, "earliest": base.Add(-time.Hour)}
	for slug, at := range times {
		p, _ := s.CreatePost(ctx, &post.Post{Slug: slug, Title: slug}, "me")
		if _, err := s.UpdatePost(ctx, slug, p.Version, "me", schedule(at, "me")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "unscheduled", Title: "u"}, "me"); err != nil {
		t.Fatal(err)
	}

	list, err := s.ScheduledPosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, sc := range list {
		order = append(order, sc.Slug)
	}
	if strings.Join(order, ",") != "earliest,sooner,later" {
		t.Errorf("order = %v", order)
	}

	items, counts, err := s.ListPosts(ctx, Filter{Scheduled: true})
	if err != nil || len(items) != 3 {
		t.Fatalf("scheduled filter = %d items, %v", len(items), err)
	}
	if items[0].PublishAt == nil {
		t.Error("summary lost its publish time")
	}
	if counts.Scheduled != 3 || counts.Draft != 4 {
		t.Errorf("counts = %+v, want 3 scheduled within 4 drafts", counts)
	}
}
