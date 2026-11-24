package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func TestRewriteOrigin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	old, next := "https://old.example", "https://new.example"
	path := "/media/" + strings.Repeat("1", 32) + ".jpg"
	before, after := old+path, next+path
	unchanged := " https://another.example/?next=" + before + " " + before + ".other " + old + "/article"
	body := "![one](" + before + "?size=2#here) " + before + "." + unchanged
	p, err := e.store.CreatePost(ctx, &post.Post{Slug: "hello", Title: "Hello", Body: body, CoverURL: before}, "user")
	if err != nil {
		t.Fatal(err)
	}
	if out := e.must("", "rewrite-origin", old, next, "--dry-run"); !strings.Contains(out, "Would update 1 post") {
		t.Fatal(out)
	}
	if got, _ := e.store.GetPost(ctx, p.Slug); got.Version != p.Version {
		t.Fatal("dry run changed the post")
	}
	e.must("", "rewrite-origin", old, next)
	got, err := e.store.GetPost(ctx, p.Slug)
	if err != nil || got.Version != p.Version+1 || got.CoverURL != after || got.Body != "![one]("+after+"?size=2#here) "+after+"."+unchanged {
		t.Fatalf("updated post: %+v, %v", got, err)
	}
	if rev, err := e.store.Revision(ctx, p.Slug, p.Version); err != nil || rev.Post.Body != body {
		t.Fatalf("original revision: %+v, %v", rev, err)
	}
	if out := e.must("", "rewrite-origin", old, next); !strings.Contains(out, "Updated 0 posts") {
		t.Fatal(out)
	}
}
