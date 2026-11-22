package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/weburz/burzblog/internal/post"
)

func usedIn(t *testing.T, s *Store, m Media) int {
	t.Helper()
	list, _, err := s.FindMedia(context.Background(), MediaFilter{}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range list {
		if got.FileName == m.FileName {
			return got.UsedIn
		}
	}
	t.Fatalf("%s not listed", m.FileName)
	return 0
}

func TestMediaUsesFollowPostWrites(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	m := upload(digest("a"))
	if _, _, err := s.AddMedia(ctx, m); err != nil {
		t.Fatal(err)
	}
	if n := usedIn(t, s, m); n != 0 {
		t.Fatalf("unused file counted %d", n)
	}
	p, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "A", Body: "![](/media/" + m.FileName + ")"}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	cover, err := s.CreatePost(ctx, &post.Post{Slug: "b", Title: "B", CoverURL: "https://x.test/media/" + m.FileName}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if n := usedIn(t, s, m); n != 2 {
		t.Errorf("body and cover = %d, want 2", n)
	}
	if _, err := s.UpdatePost(ctx, "a", p.Version, "ada", func(p *post.Post) error { p.Body = "gone"; return nil }); err != nil {
		t.Fatal(err)
	}
	if n := usedIn(t, s, m); n != 1 {
		t.Errorf("after removing from a body = %d, want 1", n)
	}
	archived, err := s.UpdatePost(ctx, "b", cover.Version, "ada", func(p *post.Post) error { p.Status = post.Archived; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePost(ctx, "b", archived.Version); err != nil {
		t.Fatal(err)
	}
	if n := usedIn(t, s, m); n != 0 {
		t.Errorf("after deleting the post = %d, want 0", n)
	}
	if n, err := s.MediaUseCount(ctx, m.FileName); err != nil || n != 0 {
		t.Errorf("MediaUseCount = %d, %v", n, err)
	}
}

func TestMediaUsesCoverUploadsAfterThePost(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	m := upload(digest("b"))
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "early", Title: "Early", Body: m.FileName}, "ada"); err != nil {
		t.Fatal(err)
	}
	added, _, err := s.AddMedia(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if n := usedIn(t, s, m); n != 1 {
		t.Errorf("post written before the upload = %d, want 1", n)
	}
	if _, err := s.DeleteMedia(ctx, added.ID); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.db.QueryRow("SELECT count(*) FROM media_uses").Scan(&rows); err != nil || rows != 0 {
		t.Errorf("media_uses rows after deleting the upload = %d, %v", rows, err)
	}
}

// Concurrent saves leave the table equal to what a full scan finds.
func TestMediaUsesStayExactUnderConcurrentSaves(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	m := upload(digest("c"))
	if _, _, err := s.AddMedia(ctx, m); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slug := "p" + string(rune('a'+i))
			p, err := s.CreatePost(ctx, &post.Post{Slug: slug, Title: slug, Body: m.FileName}, "ada")
			if err != nil {
				t.Error(err)
				return
			}
			for j := range 5 {
				body := "x"
				if (i+j)%2 == 0 {
					body = m.FileName
				}
				if p, err = s.UpdatePost(ctx, slug, p.Version, "ada", func(p *post.Post) error { p.Body = body; return nil }); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	var scanned int
	if err := s.db.QueryRow(`SELECT count(*) FROM posts WHERE instr(body, ?1) > 0 OR instr(cover_url, ?1) > 0`, m.FileName).Scan(&scanned); err != nil {
		t.Fatal(err)
	}
	if got := usedIn(t, s, m); got != scanned {
		t.Errorf("usedIn = %d, full scan = %d", got, scanned)
	}
}

// Keep the content size constant while varying the size of the upload catalog.
func BenchmarkAutosaveWithUploadCatalog(b *testing.B) {
	for _, uploads := range []int{20, 2000} {
		b.Run(fmt.Sprintf("%d-uploads", uploads), func(b *testing.B) {
			ctx := b.Context()
			s, err := Open(ctx, b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			for i := range uploads {
				if _, _, err := s.AddMedia(ctx, upload(fmt.Sprintf("%032x%032x", i, i))); err != nil {
					b.Fatal(err)
				}
			}
			body := strings.Repeat("word ", 20000)
			p, err := s.CreatePost(ctx, &post.Post{Slug: "draft", Title: "Draft", Body: body}, "ada")
			if err != nil {
				b.Fatal(err)
			}
			edit := 0
			for b.Loop() {
				edit++
				p, err = s.AutosavePost(ctx, p.Slug, p.Version, "ada", func(next *post.Post) error { next.Body = body + fmt.Sprint(edit); return nil })
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
