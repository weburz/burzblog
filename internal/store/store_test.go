package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/post"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenRestrictsFileAndReopens(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A"}, "me"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("database mode = %o, want 600", mode)
	}
	s, err = Open(ctx, dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	if _, err := s.GetPost(ctx, "a"); err != nil {
		t.Errorf("post lost after reopen: %v", err)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(ctx, dir); err == nil {
		t.Fatal("Open accepted a schema from a newer BurzBlog")
	}
}

func TestCreateAndGetPost(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, err := s.CreatePost(ctx, &post.Post{Slug: "hello", Title: "  Hello  ", Body: "a\r\nb"}, "sagar")
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 1 || p.Status != post.Draft || p.Title != "Hello" || p.Body != "a\nb" || p.UpdatedBy != "sagar" {
		t.Errorf("created post = %+v", p)
	}
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "hello", Title: "Again"}, "x"); !errors.Is(err, ErrTaken) {
		t.Errorf("duplicate slug: err = %v, want ErrTaken", err)
	}
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "Bad Slug", Title: "x"}, "x"); !post.IsFieldError(err) {
		t.Errorf("bad slug: err = %v, want FieldError", err)
	}
	if _, err := s.GetPost(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing post: err = %v", err)
	}
	pub, err := s.CreatePost(ctx, &post.Post{Slug: "live", Title: "Live", Status: post.Published}, "x")
	if err != nil || pub.PublishedOn == nil {
		t.Errorf("published create: %v %v", err, pub)
	}
}

func TestUpdatePostVersionsAndRevisions(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, _ := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Body: "one"}, "u1")

	same, err := s.UpdatePost(ctx, "a", 1, "u2", func(p *post.Post) error { p.Title = " A "; return nil })
	if err != nil || same.Version != 1 {
		t.Fatalf("no-op save created a version: %v %+v", err, same)
	}

	p, err = s.UpdatePost(ctx, "a", 1, "u2", func(p *post.Post) error { p.Body = "two"; return nil })
	if err != nil || p.Version != 2 || p.UpdatedBy != "u2" {
		t.Fatalf("update: %v %+v", err, p)
	}

	current, err := s.UpdatePost(ctx, "a", 1, "u3", func(p *post.Post) error { p.Body = "stale"; return nil })
	if !errors.Is(err, ErrConflict) || current == nil || current.Body != "two" {
		t.Fatalf("stale update: err = %v, current = %+v", err, current)
	}

	if _, err := s.UpdatePost(ctx, "a", 2, "u2", func(p *post.Post) error { p.Title = ""; return nil }); !post.IsFieldError(err) {
		t.Fatalf("invalid update: %v", err)
	}

	revs, err := s.Revisions(ctx, "a")
	if err != nil || len(revs) != 1 || revs[0].Version != 1 || revs[0].SavedBy != "u1" {
		t.Fatalf("revisions = %+v, %v", revs, err)
	}
	if revs[0].Words != 1 || revs[0].Post.Slug != "a" || revs[0].Post.Body != "" {
		t.Errorf("revision summary = %+v, want one word, slug a, no body", revs[0])
	}
	old, err := s.Revision(ctx, "a", 1)
	if err != nil || old.Post.Body != "one" {
		t.Fatalf("revision 1 = %+v, %v", old, err)
	}
}

func TestUpdatePostRenameAndPublish(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A"}, "u")
	s.CreatePost(ctx, &post.Post{Slug: "b", Title: "B"}, "u")
	if _, err := s.UpdatePost(ctx, "a", 1, "u", func(p *post.Post) error { p.Slug = "b"; return nil }); !errors.Is(err, ErrTaken) {
		t.Errorf("rename onto existing: %v", err)
	}
	fixed := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return fixed })
	p, err := s.UpdatePost(ctx, "a", 1, "u", func(p *post.Post) error {
		p.Slug = "c"
		p.Status = post.Published
		return nil
	})
	if err != nil || p.Slug != "c" || p.PublishedOn == nil || !p.PublishedOn.Equal(fixed) {
		t.Fatalf("rename+publish: %v %+v", err, p)
	}
	if _, err := s.GetPost(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("old slug still resolves: %v", err)
	}
	published, err := s.PublishedPosts(ctx)
	if err != nil || len(published) != 1 || published[0].Slug != "c" {
		t.Errorf("published posts = %v, %v", published, err)
	}
}

func TestDeletePostOnlyWhenArchived(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A"}, "u")
	if err := s.DeletePost(ctx, "a", 1); !errors.Is(err, ErrState) {
		t.Errorf("delete draft: %v", err)
	}
	s.UpdatePost(ctx, "a", 1, "u", func(p *post.Post) error { p.Status = post.Archived; return nil })
	if err := s.DeletePost(ctx, "a", 1); !errors.Is(err, ErrConflict) {
		t.Errorf("delete with stale version: %v", err)
	}
	if err := s.DeletePost(ctx, "a", 2); err != nil {
		t.Errorf("delete archived: %v", err)
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM revisions").Scan(&n)
	if n != 0 {
		t.Errorf("revisions left behind: %d", n)
	}
}

func TestListPosts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	s.CreatePost(ctx, &post.Post{Slug: "go-tips", Title: "Go Tips", Body: "one two three"}, "u")
	s.CreatePost(ctx, &post.Post{Slug: "rust", Title: "Rust 100%", Status: post.Published}, "u")
	s.CreatePost(ctx, &post.Post{Slug: "old", Title: "Old", Status: post.Archived}, "u")

	list, counts, err := s.ListPosts(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || counts != (Counts{Draft: 1, Published: 1, Archived: 1}) {
		t.Errorf("list = %+v counts = %+v", list, counts)
	}
	list, _, _ = s.ListPosts(ctx, Filter{Query: "GO"})
	if len(list) != 1 || list[0].Words != 3 {
		t.Errorf("query GO = %+v", list)
	}
	list, _, _ = s.ListPosts(ctx, Filter{Query: "%"})
	if len(list) != 1 || list[0].Slug != "rust" {
		t.Errorf("query %% should match the literal percent only: %+v", list)
	}
	list, _, _ = s.ListPosts(ctx, Filter{Status: post.Archived})
	if len(list) != 1 || list[0].Slug != "old" {
		t.Errorf("archived = %+v", list)
	}
}

func TestListPostsSort(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	at := func(d int) *time.Time { v := time.Date(2026, 1, d, 9, 0, 0, 0, time.UTC); return &v }
	// Created in this order: b-post, A-post, c-post, d-post.
	s.CreatePost(ctx, &post.Post{Slug: "b-post", Title: "banana", Status: post.Published, PublishedOn: at(5)}, "u")
	s.CreatePost(ctx, &post.Post{Slug: "a-post", Title: "Apple"}, "u")
	s.CreatePost(ctx, &post.Post{Slug: "c-post", Title: "cherry", Status: post.Published, PublishedOn: at(9)}, "u")
	s.CreatePost(ctx, &post.Post{Slug: "d-post", Title: "Date"}, "u")
	// Touch b-post so updated order differs from created order.
	if _, err := s.UpdatePost(ctx, "b-post", 1, "u", func(p *post.Post) error { p.Body = "changed"; return nil }); err != nil {
		t.Fatal(err)
	}

	slugs := func(sort Sort) string {
		t.Helper()
		list, _, err := s.ListPosts(ctx, Filter{Sort: sort})
		if err != nil {
			t.Fatalf("sort %q: %v", sort, err)
		}
		var out []string
		for _, p := range list {
			out = append(out, p.Slug)
		}
		return strings.Join(out, ",")
	}
	for sort, want := range map[Sort]string{
		"":            "b-post,d-post,c-post,a-post",
		SortUpdated:   "b-post,d-post,c-post,a-post",
		SortCreated:   "d-post,c-post,a-post,b-post",
		SortTitle:     "a-post,b-post,c-post,d-post",
		SortPublished: "c-post,b-post,d-post,a-post",
	} {
		if got := slugs(sort); got != want {
			t.Errorf("sort %q = %s, want %s", sort, got, want)
		}
	}
	if _, _, err := s.ListPosts(ctx, Filter{Sort: "title; DROP TABLE posts"}); err == nil {
		t.Error("unknown sort was accepted")
	}
	if ValidSort("") || !ValidSort(SortTitle) || ValidSort("size") {
		t.Error("ValidSort mismatch")
	}
}

func TestAccounts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "Jarmos", "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "jarmos", "x"); !errors.Is(err, ErrTaken) {
		t.Errorf("case-insensitive duplicate: %v", err)
	}
	if got, err := s.UserByName(ctx, "JARMOS"); err != nil || got.ID != u.ID {
		t.Errorf("lookup ignoring case: %v %v", got, err)
	}

	exp := time.Now().Add(time.Hour)
	s.CreateSession(ctx, Session{TokenHash: "keep", UserID: u.ID, CSRF: "c", ExpiresAt: exp})
	s.CreateSession(ctx, Session{TokenHash: "drop", UserID: u.ID, CSRF: "c", ExpiresAt: exp})
	if err := s.SetPassword(ctx, u.ID, "hash2", "keep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByHash(ctx, "keep"); err != nil {
		t.Errorf("kept session gone: %v", err)
	}
	if _, err := s.SessionByHash(ctx, "drop"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other session survived password change: %v", err)
	}

	s.CreateSession(ctx, Session{TokenHash: "old", UserID: u.ID, CSRF: "c", ExpiresAt: time.Now().Add(-time.Second)})
	if _, err := s.SessionByHash(ctx, "old"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session accepted: %v", err)
	}

	tok, err := s.CreateToken(ctx, u.ID, "th", "laptop", ScopeWrite)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.TokenByHash(ctx, "th")
	if err != nil || got.Username != "Jarmos" || got.Scope != ScopeWrite {
		t.Errorf("token lookup = %+v, %v", got, err)
	}
	if err := s.DeleteToken(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TokenByHash(ctx, "th"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked token accepted: %v", err)
	}
}

func TestLoginAttempts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	s.SetClock(func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if ok, err := s.ReserveLoginAttempt(ctx, "k", 3, time.Minute); !ok || err != nil {
			t.Fatalf("attempt %d refused: %v", i+1, err)
		}
	}
	if ok, _ := s.ReserveLoginAttempt(ctx, "k", 3, time.Minute); ok {
		t.Error("allowed after reaching the limit")
	}
	now = now.Add(time.Minute)
	if ok, _ := s.ReserveLoginAttempt(ctx, "k", 3, time.Minute); !ok {
		t.Error("still blocked after the window passed")
	}
	s.ClearLoginFailures(ctx, "k")
	for i := 0; i < 3; i++ {
		if ok, _ := s.ReserveLoginAttempt(ctx, "k", 3, time.Minute); !ok {
			t.Fatalf("attempt %d after clearing refused", i+1)
		}
	}
}
