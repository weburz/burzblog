package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func TestDuplicatePostKeepsContentNotLifecycle(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	src, err := s.CreatePost(ctx, &post.Post{
		Slug: "src", Title: "Source", Description: "d", Body: "b", Extra: "x: 1\n", Tags: []string{"a", "b"},
		Author: "ada", CoverURL: "https://example.com/c.png", Featured: true, Status: post.Published,
	}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	copied, err := s.DuplicatePost(ctx, "src", "grace")
	if err != nil {
		t.Fatal(err)
	}
	if copied.Slug != "src-copy" || copied.Title != "Source (copy)" || copied.Status != post.Draft || copied.Version != 1 {
		t.Errorf("copy = %+v", copied)
	}
	if copied.Description != "d" || copied.Body != "b" || copied.Extra != "x: 1\n" || copied.Author != "ada" || copied.CoverURL != src.CoverURL {
		t.Errorf("copy lost content: %+v", copied)
	}
	if copied.Featured || copied.PublishedOn != nil || copied.UpdatedOn != nil || copied.PublishAt != nil || copied.UpdatedBy != "grace" {
		t.Errorf("copy kept lifecycle: %+v", copied)
	}
	got, err := s.GetPost(ctx, "src-copy")
	if err != nil || len(got.Tags) != 2 || got.Tags[0] != "a" {
		t.Errorf("stored copy = %+v, %v", got, err)
	}
}

func TestDuplicatePostFitsLimits(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	slug := strings.Repeat("a", post.MaxSlug)
	if _, err := s.CreatePost(ctx, &post.Post{Slug: slug, Title: strings.Repeat("é", post.MaxTitle)}, "u"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{strings.Repeat("a", post.MaxSlug-5) + "-copy", strings.Repeat("a", post.MaxSlug-7) + "-copy-2"} {
		copied, err := s.DuplicatePost(ctx, slug, "u")
		if err != nil || copied.Slug != want {
			t.Fatalf("copy of a maximum-length post = %v, %v; want slug %s", copied, err, want)
		}
		if n := len([]rune(copied.Title)); n != post.MaxTitle || !strings.HasSuffix(copied.Title, " (copy)") {
			t.Errorf("title has %d characters: %q", n, copied.Title)
		}
	}
}

func TestDuplicatePostUnknown(t *testing.T) {
	s := open(t)
	if _, err := s.DuplicatePost(context.Background(), "missing", "u"); !errors.Is(err, ErrNotFound) {
		t.Errorf("duplicate unknown = %v, want ErrNotFound", err)
	}
}
