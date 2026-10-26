package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func upload(sha string) Media {
	return Media{
		FileName: sha[:32] + ".png", OriginalName: "a.png", MIME: "image/png", Size: 10,
		SHA256: sha, Width: 2, Height: 3, Alt: "alt", UploadedBy: "ada",
	}
}

func digest(c string) string { return strings.Repeat(c, 64) }

func TestAddMediaDedupesBySHA(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	first, created, err := s.AddMedia(ctx, upload(digest("a")))
	if err != nil || !created || first.ID == 0 || first.Width != 2 || first.CreatedAt.IsZero() {
		t.Fatalf("first = %+v %v %v", first, created, err)
	}
	again := upload(digest("a"))
	again.Alt, again.OriginalName = "other", "b.png"
	second, created, err := s.AddMedia(ctx, again)
	if err != nil || created || second.ID != first.ID || second.Alt != "alt" || second.OriginalName != "a.png" {
		t.Errorf("second = %+v %v %v", second, created, err)
	}
	if _, total, _ := s.ListMedia(ctx, 0, 0); total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
}

func TestMediaRejectsBadRows(t *testing.T) {
	s := open(t)
	bad := upload(digest("a"))
	bad.SHA256 = "short"
	if _, _, err := s.AddMedia(context.Background(), bad); err == nil {
		t.Error("accepted a short digest")
	}
}

func TestListAndDeleteMedia(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	for _, c := range []string{"a", "b", "c"} {
		if _, _, err := s.AddMedia(ctx, upload(digest(c))); err != nil {
			t.Fatal(err)
		}
	}
	list, total, err := s.ListMedia(ctx, 2, 1)
	if err != nil || total != 3 || len(list) != 2 || list[0].SHA256 != digest("b") || list[1].SHA256 != digest("a") {
		t.Errorf("page = %v %d %v", list, total, err)
	}
	m, err := s.DeleteMedia(ctx, list[0].ID)
	if err != nil || m.FileName != digest("b")[:32]+".png" {
		t.Errorf("delete = %+v %v", m, err)
	}
	if _, err := s.DeleteMedia(ctx, list[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
	if _, err := s.GetMedia(ctx, list[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get deleted = %v, want ErrNotFound", err)
	}
}

func TestMediaUsers(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	name := digest("a")[:32] + ".png"
	for slug, p := range map[string]post.Post{
		"in-body":  {Title: "T", Status: post.Draft, Body: "see ![x](https://b.example/media/" + name + ")"},
		"in-cover": {Title: "T", Status: post.Published, CoverURL: "https://b.example/media/" + name},
		"neither":  {Title: "T", Status: post.Draft, Body: "nothing"},
	} {
		p.Slug = slug
		if _, err := s.CreatePost(ctx, &p, "ada"); err != nil {
			t.Fatal(err)
		}
	}
	slugs, err := s.MediaUsers(ctx, name, 10)
	if err != nil || strings.Join(slugs, ",") != "in-body,in-cover" {
		t.Errorf("users = %v %v", slugs, err)
	}
	if slugs, _ := s.MediaUsers(ctx, name, 1); len(slugs) != 1 {
		t.Errorf("limited users = %v", slugs)
	}
}
