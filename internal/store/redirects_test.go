package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

// renamed creates a published post and renames it through the given slugs,
// returning the final post.
func renamed(t *testing.T, s *Store, slugs ...string) *post.Post {
	t.Helper()
	ctx := context.Background()
	p, err := s.CreatePost(ctx, &post.Post{Slug: slugs[0], Title: "T", Status: post.Published}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range slugs[1:] {
		p, err = s.UpdatePost(ctx, p.Slug, p.Version, "ada", func(p *post.Post) error { p.Slug = slug; return nil })
		if err != nil {
			t.Fatalf("rename to %s: %v", slug, err)
		}
	}
	return p
}

func pairs(t *testing.T, s *Store) []Redirect {
	t.Helper()
	list, err := s.PublishedRedirects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestRenameKeepsRedirect(t *testing.T) {
	s := open(t)
	renamed(t, s, "old", "new")
	want := []Redirect{{"old", "new"}}
	if got := pairs(t, s); !reflect.DeepEqual(got, want) {
		t.Errorf("redirects = %v, want %v", got, want)
	}
	if to, err := s.RedirectTarget(context.Background(), "old"); err != nil || to != "new" {
		t.Errorf("RedirectTarget = %q, %v", to, err)
	}
}

func TestRenameWithoutSlugChangeAddsNoRedirect(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p := renamed(t, s, "same")
	if _, err := s.UpdatePost(ctx, "same", p.Version, "ada", func(p *post.Post) error { p.Title = "Other"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := pairs(t, s); len(got) != 0 {
		t.Errorf("redirects = %v, want none", got)
	}
}

func TestRedirectChainsCollapse(t *testing.T) {
	s := open(t)
	renamed(t, s, "a", "b", "c")
	want := []Redirect{{"a", "c"}, {"b", "c"}}
	if got := pairs(t, s); !reflect.DeepEqual(got, want) {
		t.Errorf("redirects = %v, want %v", got, want)
	}
}

func TestRenameBackDropsOwnRedirect(t *testing.T) {
	s := open(t)
	renamed(t, s, "a", "b", "a")
	want := []Redirect{{"b", "a"}}
	if got := pairs(t, s); !reflect.DeepEqual(got, want) {
		t.Errorf("redirects = %v, want %v", got, want)
	}
}

func TestLiveSlugWinsOverRedirect(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	renamed(t, s, "taken", "moved")

	// A new post takes the redirect's slug.
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "taken", Title: "New", Status: post.Published}, "ada"); err != nil {
		t.Fatal(err)
	}
	if got := pairs(t, s); len(got) != 0 {
		t.Errorf("redirects after create = %v, want none", got)
	}

	// A rename takes one.
	renamed(t, s, "x", "y")
	p, err := s.CreatePost(ctx, &post.Post{Slug: "z", Title: "Z", Status: post.Published}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePost(ctx, "z", p.Version, "ada", func(p *post.Post) error { p.Slug = "x"; return nil }); err != nil {
		t.Fatal(err)
	}
	want := []Redirect{{"z", "x"}}
	if got := pairs(t, s); !reflect.DeepEqual(got, want) {
		t.Errorf("redirects after rename = %v, want %v", got, want)
	}
}

func TestRenameOntoLiveSlugIsRefused(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	renamed(t, s, "a", "b")
	p, err := s.CreatePost(ctx, &post.Post{Slug: "c", Title: "C"}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.UpdatePost(ctx, "c", p.Version, "ada", func(p *post.Post) error { p.Slug = "b"; return nil })
	if !errors.Is(err, ErrTaken) {
		t.Fatalf("rename onto a live slug = %v, want ErrTaken", err)
	}
	// The refused rename left no redirect behind.
	if _, err := s.RedirectTarget(ctx, "c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("redirect from refused rename = %v, want ErrNotFound", err)
	}
}

func TestRedirectsToUnpublishedPostsAreHidden(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p := renamed(t, s, "old", "new")
	for _, status := range []post.Status{post.Draft, post.Archived} {
		var err error
		p, err = s.UpdatePost(ctx, "new", p.Version, "ada", func(p *post.Post) error { p.Status = status; return nil })
		if err != nil {
			t.Fatal(err)
		}
		if got := pairs(t, s); len(got) != 0 {
			t.Errorf("%s target listed: %v", status, got)
		}
		if _, err := s.RedirectTarget(ctx, "old"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s target resolved: %v", status, err)
		}
		// The panel still sees the redirect.
		if from, err := s.PostRedirects(ctx, "new"); err != nil || !reflect.DeepEqual(from, []string{"old"}) {
			t.Errorf("PostRedirects of %s = %v, %v", status, from, err)
		}
	}
}

func TestDeletePostDeletesRedirects(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p := renamed(t, s, "old", "new")
	p, err := s.UpdatePost(ctx, "new", p.Version, "ada", func(p *post.Post) error { p.Status = post.Archived; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePost(ctx, "new", p.Version); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM slug_redirects").Scan(&n); err != nil || n != 0 {
		t.Errorf("redirect rows after delete = %d, %v", n, err)
	}
	// The slug is free to use again.
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "old", Title: "Again"}, "ada"); err != nil {
		t.Errorf("reuse after delete: %v", err)
	}
}

func TestDeleteRedirect(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	renamed(t, s, "a", "b")
	other := renamed(t, s, "c")
	if err := s.DeleteRedirect(ctx, other.Slug, "a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete through another post = %v, want ErrNotFound", err)
	}
	if err := s.DeleteRedirect(ctx, "b", "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRedirect(ctx, "b", "a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete twice = %v, want ErrNotFound", err)
	}
	if err := s.DeleteRedirect(ctx, "nope", "a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete on a missing post = %v, want ErrNotFound", err)
	}
	if _, err := s.PostRedirects(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("PostRedirects of a missing post = %v, want ErrNotFound", err)
	}
	if got := pairs(t, s); len(got) != 0 {
		t.Errorf("redirects = %v, want none", got)
	}
}
