package store

import (
	"context"
	"errors"
	"sync"
	"testing"

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
	// A freed slug is reused; a titled post named like one blocks it.
	if err := s.DeleteEmptyDraft(ctx, "untitled-2", 1); err != nil {
		t.Fatal(err)
	}
	if p, err := s.CreateUntitled(ctx, "ada"); err != nil || p.Slug != "untitled-2" {
		t.Errorf("after delete = %+v, %v; want untitled-2", p, err)
	}
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "untitled-4", Title: "Mine"}, "ada"); err != nil {
		t.Fatal(err)
	}
	if p, err := s.CreateUntitled(ctx, "ada"); err != nil || p.Slug != "untitled-5" {
		t.Errorf("after untitled-4 taken = %+v, %v; want untitled-5", p, err)
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
