package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/post"
)

func TestCreateUntitledPicksNextFreeSlug(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	want := []string{"untitled", "untitled-2", "untitled-3"}
	for _, slug := range want {
		p, err := s.CreateUntitled(ctx, "ada")
		if err != nil || p.Slug != slug || p.Title != "" || p.Status != post.Draft || p.Author != "ada" {
			t.Fatalf("CreateUntitled = %+v, %v; want slug %s", p, err, slug)
		}
	}
	// The next slug follows the highest in use, so a freed middle one is not
	// reused; a titled post named like one blocks it.
	if err := s.DeleteEmptyDraft(ctx, "untitled-2", 1); err != nil {
		t.Fatal(err)
	}
	if p, err := s.CreateUntitled(ctx, "ada"); err != nil || p.Slug != "untitled-4" {
		t.Errorf("after delete = %+v, %v; want untitled-4", p, err)
	}
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "untitled-5", Title: "Mine"}, "ada"); err != nil {
		t.Fatal(err)
	}
	if p, err := s.CreateUntitled(ctx, "ada"); err != nil || p.Slug != "untitled-6" {
		t.Errorf("after untitled-5 taken = %+v, %v; want untitled-6", p, err)
	}
}

func TestNextUntitledSlugIgnoresLookalikes(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for _, slug := range []string{"untitled-9-copy", "untitled-notes", "untitled-x9"} {
		if _, err := s.CreatePost(ctx, &post.Post{Slug: slug, Title: "Mine"}, "ada"); err != nil {
			t.Fatal(err)
		}
	}
	if p, err := s.CreateUntitled(ctx, "ada"); err != nil || p.Slug != "untitled" {
		t.Errorf("with only lookalikes = %+v, %v; want untitled", p, err)
	}
	if p, err := s.CreateUntitled(ctx, "ada"); err != nil || p.Slug != "untitled-2" {
		t.Errorf("then = %+v, %v; want untitled-2", p, err)
	}
}

func TestCreateUntitledConcurrentWritersNeverCollide(t *testing.T) {
	s := open(t)
	var wg sync.WaitGroup
	slugs := make(chan string, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := s.CreateUntitled(context.Background(), "ada")
			if err != nil {
				t.Error(err)
				return
			}
			slugs <- p.Slug
		}()
	}
	wg.Wait()
	close(slugs)
	seen := map[string]bool{}
	for slug := range slugs {
		if seen[slug] {
			t.Errorf("slug %s handed out twice", slug)
		}
		seen[slug] = true
	}
	if len(seen) != 8 {
		t.Errorf("got %d slugs, want 8", len(seen))
	}
}

func TestRenamingNeverPublishedDraftLeavesNoRedirect(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, err := s.CreateUntitled(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.AutosavePost(ctx, p.Slug, p.Version, "ada", func(p *post.Post) error {
		p.Title, p.Slug = "My idea", "my-idea"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.PostRedirects(ctx, p.Slug); err != nil || len(got) != 0 {
		t.Errorf("redirects after draft rename = %v, %v; want none", got, err)
	}
	if _, err := s.RedirectTarget(ctx, "untitled"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RedirectTarget(untitled) = %v, want ErrNotFound", err)
	}
	// Once published, a rename keeps the old address working.
	p, err = s.UpdatePost(ctx, p.Slug, p.Version, "ada", func(p *post.Post) error { p.Status = post.Published; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePost(ctx, p.Slug, p.Version, "ada", func(p *post.Post) error { p.Slug = "my-idea-2"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.PostRedirects(ctx, "my-idea-2"); len(got) != 1 || got[0] != "my-idea" {
		t.Errorf("redirects after published rename = %v, want [my-idea]", got)
	}
}

func TestDeleteEmptyDraft(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, _ := s.CreateUntitled(ctx, "ada")
	if err := s.DeleteEmptyDraft(ctx, p.Slug, p.Version+1); !errors.Is(err, ErrConflict) {
		t.Errorf("stale version = %v, want ErrConflict", err)
	}
	if err := s.DeleteEmptyDraft(ctx, p.Slug, p.Version); err != nil {
		t.Fatalf("delete empty draft = %v", err)
	}
	if _, err := s.GetPost(ctx, p.Slug); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPost after delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteEmptyDraft(ctx, p.Slug, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete twice = %v, want ErrNotFound", err)
	}
}

func TestDeleteEmptyDraftRefusesWritingAndPublishedPosts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	titled, _ := s.CreatePost(ctx, &post.Post{Slug: "titled", Title: "T"}, "ada")
	body, _ := s.CreatePost(ctx, &post.Post{Slug: "body", Body: "words"}, "ada")
	for _, p := range []*post.Post{titled, body} {
		if err := s.DeleteEmptyDraft(ctx, p.Slug, p.Version); !errors.Is(err, ErrState) {
			t.Errorf("delete %s = %v, want ErrState", p.Slug, err)
		}
	}

	// Emptied again, but an earlier revision held writing.
	typed, _ := s.CreateUntitled(ctx, "ada")
	typed, _ = s.UpdatePost(ctx, typed.Slug, typed.Version, "ada", func(p *post.Post) error { p.Body = "precious"; return nil })
	typed, _ = s.UpdatePost(ctx, typed.Slug, typed.Version, "ada", func(p *post.Post) error { p.Body = ""; return nil })
	if err := s.DeleteEmptyDraft(ctx, typed.Slug, typed.Version); !errors.Is(err, ErrState) {
		t.Errorf("delete emptied draft = %v, want ErrState", err)
	}

	// Published once, then unpublished and emptied.
	was, _ := s.CreateUntitled(ctx, "ada")
	was, _ = s.UpdatePost(ctx, was.Slug, was.Version, "ada", func(p *post.Post) error { p.Title, p.Status = "T", post.Published; return nil })
	was, _ = s.UpdatePost(ctx, was.Slug, was.Version, "ada", func(p *post.Post) error { p.Title, p.Status = "", post.Draft; return nil })
	if err := s.DeleteEmptyDraft(ctx, was.Slug, was.Version); !errors.Is(err, ErrState) {
		t.Errorf("delete once-published draft = %v, want ErrState", err)
	}
}

func TestIsEmptyDraft(t *testing.T) {
	blank := func() *post.Post {
		return &post.Post{Status: post.Draft, Author: "ada", UpdatedBy: "ada"}
	}
	if !IsEmptyDraft(blank()) {
		t.Fatal("a fresh untitled draft is not empty")
	}
	whitespace := blank()
	whitespace.Title, whitespace.Body, whitespace.Description, whitespace.Extra = " \t", "\n\n", "  ", " \n"
	if !IsEmptyDraft(whitespace) {
		t.Error("whitespace-only fields count as writing")
	}
	published := time.Now()
	for name, change := range map[string]func(*post.Post){
		"title":         func(p *post.Post) { p.Title = "T" },
		"body":          func(p *post.Post) { p.Body = "words" },
		"description":   func(p *post.Post) { p.Description = "d" },
		"tags":          func(p *post.Post) { p.Tags = []string{"go"} },
		"cover":         func(p *post.Post) { p.CoverURL = "/media/a.png" },
		"cover alt":     func(p *post.Post) { p.CoverAlt = "alt" },
		"seo title":     func(p *post.Post) { p.SEOTitle = "s" },
		"seo desc":      func(p *post.Post) { p.SEODescription = "s" },
		"canonical":     func(p *post.Post) { p.CanonicalURL = "https://example.com/a" },
		"extra":         func(p *post.Post) { p.Extra = "layout: wide" },
		"author":        func(p *post.Post) { p.Author = "Grace" },
		"featured":      func(p *post.Post) { p.Featured = true },
		"published":     func(p *post.Post) { p.PublishedOn = &published },
		"archived":      func(p *post.Post) { p.Status = post.Archived },
		"published now": func(p *post.Post) { p.Status = post.Published },
	} {
		p := blank()
		change(p)
		if IsEmptyDraft(p) {
			t.Errorf("draft with %s counts as empty", name)
		}
	}
}

func TestDeleteEmptyDraftRefusesMetadataOnlyDrafts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for name, change := range map[string]func(*post.Post){
		"description": func(p *post.Post) { p.Description = "A summary" },
		"tags":        func(p *post.Post) { p.Tags = []string{"notes"} },
		"seo":         func(p *post.Post) { p.SEOTitle = "Search title" },
		"extra":       func(p *post.Post) { p.Extra = "layout: wide" },
	} {
		p, err := s.CreateUntitled(ctx, "ada")
		if err != nil {
			t.Fatal(err)
		}
		p, err = s.UpdatePost(ctx, p.Slug, p.Version, "ada", func(p *post.Post) error { change(p); return nil })
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := s.DeleteEmptyDraft(ctx, p.Slug, p.Version); !errors.Is(err, ErrState) {
			t.Errorf("delete draft with only %s = %v, want ErrState", name, err)
		}
	}

	// Metadata that was cleared again is still in an earlier revision.
	p, _ := s.CreateUntitled(ctx, "ada")
	p, _ = s.UpdatePost(ctx, p.Slug, p.Version, "ada", func(p *post.Post) error { p.Description = "kept"; return nil })
	p, _ = s.UpdatePost(ctx, p.Slug, p.Version, "ada", func(p *post.Post) error { p.Description = ""; return nil })
	if err := s.DeleteEmptyDraft(ctx, p.Slug, p.Version); !errors.Is(err, ErrState) {
		t.Errorf("delete draft whose description was cleared = %v, want ErrState", err)
	}

	// Whitespace-only writing, saved and revised, is still empty.
	blank, _ := s.CreateUntitled(ctx, "ada")
	blank, _ = s.UpdatePost(ctx, blank.Slug, blank.Version, "ada", func(p *post.Post) error { p.Body = "  \n"; return nil })
	if err := s.DeleteEmptyDraft(ctx, blank.Slug, blank.Version); err != nil {
		t.Errorf("delete whitespace-only draft = %v, want nil", err)
	}
}

func TestSweepEmptyDrafts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	fresh, _ := s.CreateUntitled(ctx, "ada")
	old, _ := s.CreateUntitled(ctx, "ada")
	typed, _ := s.CreateUntitled(ctx, "ada")
	if _, err := s.UpdatePost(ctx, typed.Slug, typed.Version, "ada", func(p *post.Post) error { p.Tags = []string{"x"}; return nil }); err != nil {
		t.Fatal(err)
	}
	titled, _ := s.CreatePost(ctx, &post.Post{Slug: "untitled-notes"}, "ada")
	now = now.Add(48 * time.Hour)
	later, _ := s.CreateUntitled(ctx, "ada")

	removed, err := s.SweepEmptyDrafts(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 || removed[0] != fresh.Slug || removed[1] != old.Slug {
		t.Errorf("removed = %v, want [%s %s]", removed, fresh.Slug, old.Slug)
	}
	for _, slug := range []string{typed.Slug, titled.Slug, later.Slug} {
		if _, err := s.GetPost(ctx, slug); err != nil {
			t.Errorf("%s was swept: %v", slug, err)
		}
	}
	if removed, err := s.SweepEmptyDrafts(ctx, now.Add(-24*time.Hour)); err != nil || len(removed) != 0 {
		t.Errorf("second sweep = %v, %v; want nothing", removed, err)
	}
}

func TestRenamedDraft(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	p, _ := s.CreateUntitled(ctx, "ada")
	rename := func(slug string, version int64, to string) *post.Post {
		t.Helper()
		got, err := s.UpdatePost(ctx, slug, version, "ada", func(p *post.Post) error { p.Slug = to; return nil })
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	p = rename(p.Slug, p.Version, "first")
	rename(p.Slug, p.Version, "second")
	if to, err := s.RenamedDraft(ctx, "untitled"); err != nil || to != "second" {
		t.Errorf("RenamedDraft(untitled) = %q, %v; want second", to, err)
	}
	if _, err := s.RenamedDraft(ctx, "second"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RenamedDraft(live slug) = %v, want ErrNotFound", err)
	}
	if _, err := s.RenamedDraft(ctx, "nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RenamedDraft(unknown) = %v, want ErrNotFound", err)
	}
	now = now.Add(25 * time.Hour)
	if _, err := s.RenamedDraft(ctx, "untitled"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RenamedDraft after a day = %v, want ErrNotFound", err)
	}
}
