package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/post"
)

func TestFTS5IsAvailable(t *testing.T) {
	s := open(t)
	if _, err := s.db.Exec(`CREATE VIRTUAL TABLE probe USING fts5 (a, tokenize = 'unicode61 remove_diacritics 2')`); err != nil {
		t.Fatalf("SQLite lacks FTS5: %v", err)
	}
}

func TestMatchExpr(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"golang", `"golang"*`},
		{"  go   interfaces ", `"go" "interfaces"*`},
		{`say "hi" OR NEAR(a b)`, `"say" "hi" "OR" "NEAR" "a" "b"*`},
		{"title:secret", `"title" "secret"*`},
		{"-draft *", `"draft"*`},
		{"café", `"café"*`},
		{"é", "\"é\"*"},
		{"", ""},
		{`"`, ""},
		{"  - * ( ) :: ", ""},
	} {
		if got := matchExpr(tc.in); got != tc.want {
			t.Errorf("matchExpr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := matchExpr(strings.Repeat("w ", 40)); strings.Count(got, `"w"`) != searchTerms {
		t.Errorf("long query = %q, want %d terms", got, searchTerms)
	}
}

// searchSlugs lists the slugs ListPosts returns for a query, in order.
func searchSlugs(t *testing.T, s *Store, f Filter) string {
	t.Helper()
	list, _, err := s.ListPosts(context.Background(), f)
	if err != nil {
		t.Fatalf("ListPosts(%+v): %v", f, err)
	}
	var slugs []string
	for _, sm := range list {
		slugs = append(slugs, sm.Slug)
	}
	return strings.Join(slugs, ",")
}

func seedSearch(t *testing.T, s *Store) {
	t.Helper()
	for _, p := range []*post.Post{
		{Slug: "go-intro", Title: "Learning Go", Description: "First steps", Body: "Goroutines and channels explained.", Tags: []string{"golang", "tutorial"}, Status: post.Published, Author: "Asha"},
		{Slug: "cafe", Title: "Café notes", Body: "Espresso and croissants.", Status: post.Published},
		{Slug: "ops", Title: "Operations", Body: "Deploy with systemd. title:secret OR NEAR", Status: post.Published, Tags: []string{"devops"}},
		{Slug: "wip", Title: "Unfinished", Body: "Secret goroutines plans.", Status: post.Draft},
	} {
		if _, err := s.CreatePost(context.Background(), p, "u"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearchFindsEveryField(t *testing.T) {
	s := open(t)
	seedSearch(t, s)
	for _, tc := range []struct{ name, q, want string }{
		{"body", "channels", "go-intro"},
		{"title", "learning", "go-intro"},
		{"description", "steps", "go-intro"},
		{"tag", "tutorial", "go-intro"},
		{"tag part", "devops", "ops"},
		{"author", "asha", "go-intro"},
		{"slug words", "go intro", "go-intro"},
		{"prefix of the last word", "gorout", "wip,go-intro"},
		{"accent in the data", "cafe", "cafe"},
		{"accent in the query", "CAFÉ", "cafe"},
		{"every word must match", "channels espresso", ""},
		{"no match", "kubernetes", ""},
		{"not a prefix of a middle word", "gorout explained", ""},
	} {
		if got := searchSlugs(t, s, Filter{Query: tc.q, Sort: SortTitle}); got != tc.want && !sameSet(got, tc.want) {
			t.Errorf("%s: %q found %q, want %q", tc.name, tc.q, got, tc.want)
		}
	}
}

func sameSet(a, b string) bool {
	x, y := strings.Split(a, ","), strings.Split(b, ",")
	if len(x) != len(y) {
		return false
	}
	seen := map[string]bool{}
	for _, v := range x {
		seen[v] = true
	}
	for _, v := range y {
		if !seen[v] {
			return false
		}
	}
	return true
}

// Search syntax typed by a person is only words.
func TestSearchTreatsSyntaxAsText(t *testing.T) {
	s := open(t)
	seedSearch(t, s)
	for _, tc := range []struct{ q, want string }{
		{`"`, "go-intro,cafe,ops,wip"}, // no word: no filter
		{`"unterminated`, ""},          // quote dropped; the word matches nothing
		{"*", "go-intro,cafe,ops,wip"}, // no word: no filter
		{"-", "go-intro,cafe,ops,wip"}, // no word: no filter
		{"learning OR espresso", ""},   // OR is a word, not a union
		{"title:secret", "ops"},        // a column filter would hit only titles
		{"NEAR", "ops"},                // only a body has the word
		{"(learning", "go-intro"},      // parentheses are punctuation
		{"learning -go", "go-intro"},   // minus is not NOT
		{"body:channels", ""},          // "body" is a word nothing has
		{"learn*ing", ""},              // star splits, no wildcard inside
		{"posts_fts:learning", ""},     // no table or column names
		{"{title description}: learning", ""},
	} {
		got := searchSlugs(t, s, Filter{Query: tc.q, Sort: SortTitle})
		if !sameSet2(got, tc.want) {
			t.Errorf("%q found %q, want %q", tc.q, got, tc.want)
		}
	}
}

func sameSet2(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return sameSet(a, b)
}

func TestSearchRanksTitleFirstUnlessSorted(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { clock = clock.Add(time.Minute); return clock })
	for _, p := range []*post.Post{
		{Slug: "body-hit", Title: "Misc", Body: "all about rust today, rust everywhere", Status: post.Published},
		{Slug: "title-hit", Title: "Rust", Body: "short", Status: post.Published},
		{Slug: "newest", Title: "Other", Body: "mentions rust once", Status: post.Published},
	} {
		if _, err := s.CreatePost(ctx, p, "u"); err != nil {
			t.Fatal(err)
		}
	}
	if got := searchSlugs(t, s, Filter{Query: "rust"}); !strings.HasPrefix(got, "title-hit,") {
		t.Errorf("ranked = %q, want title-hit first", got)
	}
	if got := searchSlugs(t, s, Filter{Query: "rust", Sort: SortCreated}); got != "newest,title-hit,body-hit" {
		t.Errorf("sorted by created = %q", got)
	}
	if got := searchSlugs(t, s, Filter{Query: "!!!"}); got != "newest,title-hit,body-hit" {
		t.Errorf("no-word query = %q, want the plain updated order", got)
	}
	if _, _, err := s.ListPosts(ctx, Filter{Query: "rust", Sort: "bogus"}); err == nil {
		t.Error("an unknown sort was accepted with a query")
	}
}

func TestSearchPagesAndCombinesFilters(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for i := range 7 {
		p := &post.Post{Slug: fmt.Sprintf("p%d", i), Title: fmt.Sprintf("Needle %d", i), Tags: []string{"odd"}}
		if i%2 == 0 {
			p.Tags = []string{"even"}
		}
		if _, err := s.CreatePost(ctx, p, "u"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "gone", Title: "Needle gone", Status: post.Archived}, "u"); err != nil {
		t.Fatal(err)
	}
	list, counts, err := s.ListPosts(ctx, Filter{Query: "needle", Sort: SortTitle, Limit: 3, Offset: 3})
	if err != nil || len(list) != 3 || counts.Matched != 7 || list[0].Slug != "p3" {
		t.Errorf("page = %v, matched %d, %v; want p3.. of 7", list, counts.Matched, err)
	}
	if len(list[0].Tags) != 1 {
		t.Errorf("tags on a searched page = %v", list[0].Tags)
	}
	list, counts, err = s.ListPosts(ctx, Filter{Query: "needle", Tag: "even", Sort: SortTitle})
	if err != nil || len(list) != 4 || counts.Matched != 4 {
		t.Errorf("with tag = %d posts, matched %d, %v; want 4", len(list), counts.Matched, err)
	}
	list, _, err = s.ListPosts(ctx, Filter{Query: "needle", Status: post.Archived})
	if err != nil || len(list) != 1 || list[0].Slug != "gone" {
		t.Errorf("archived = %v, %v", list, err)
	}
	if counts, err := s.StatusCounts(ctx); err != nil || counts.Draft != 7 {
		t.Errorf("counts ignore the search: %+v, %v", counts, err)
	}
}

func TestContentSearchIsPublishedOnlyWithSnippets(t *testing.T) {
	s := open(t)
	seedSearch(t, s)
	ctx := context.Background()
	list, total, err := s.ListPublished(ctx, "goroutines", "", 0, 0)
	if err != nil || total != 1 || len(list) != 1 || list[0].Post.Slug != "go-intro" {
		t.Fatalf("published search = %v, %d, %v; the draft must stay out", list, total, err)
	}
	if !strings.Contains(list[0].Snippet, "Goroutines") || strings.ContainsAny(list[0].Snippet, "<>") {
		t.Errorf("snippet = %q", list[0].Snippet)
	}
	if list[0].Post.Tags[0] != "golang" || list[0].Words == 0 {
		t.Errorf("listing lost tags or words: %+v", list[0])
	}
	if _, total, _ := s.ListPublished(ctx, "secret", "", 0, 0); total != 1 {
		t.Errorf(`"secret" found %d, want only the published body (not the draft)`, total)
	}
	list, total, err = s.ListPublished(ctx, "tutorial", "golang", 0, 0)
	if err != nil || total != 1 {
		t.Errorf("query with tag = %d, %v", total, err)
	}
	if _, total, _ = s.ListPublished(ctx, "tutorial", "devops", 0, 0); total != 0 {
		t.Errorf("query with another tag found %d", total)
	}
	list, total, err = s.ListPublished(ctx, "", "", 0, 0)
	if err != nil || total != 3 || list[0].Snippet != "" {
		t.Errorf("no query = %d posts, snippet %q, %v", total, list[0].Snippet, err)
	}
	list, total, err = s.ListPublished(ctx, "go", "", 1, 5)
	if err != nil || total != 1 || len(list) != 0 {
		t.Errorf("past the end = %v, %d, %v", list, total, err)
	}
}

func TestSearchFollowsEveryChange(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "Alpha", Body: "first draft text", Status: post.Published}, "u")
	if err != nil {
		t.Fatal(err)
	}
	found := func(q string) bool {
		t.Helper()
		_, total, err := s.ListPublished(ctx, q, "", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		return total > 0
	}
	if !found("alpha") || !found("first") {
		t.Fatal("a new post is not searchable")
	}

	p, err = s.UpdatePost(ctx, "a", p.Version, "u", func(p *post.Post) error {
		p.Title, p.Body, p.Tags = "Beta", "second version", []string{"fresh"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found("alpha") || found("first") || !found("beta") || !found("second") || !found("fresh") {
		t.Error("an edit did not reindex the post")
	}

	p, err = s.UpdatePost(ctx, "a", p.Version, "u", func(p *post.Post) error { p.Slug = "renamed-post"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !found("renamed") {
		t.Error("a rename did not reindex the slug")
	}

	// Restoring an earlier version is an update with that version's content.
	p, err = s.UpdatePost(ctx, "renamed-post", p.Version, "u", func(p *post.Post) error {
		p.Title, p.Body, p.Tags, p.Slug = "Alpha", "first draft text", nil, "a"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found("alpha") || !found("first") || found("fresh") || found("second") {
		t.Error("a restore did not reindex the post")
	}

	// Leaving published hides it from content search, though it stays indexed.
	p, err = s.UpdatePost(ctx, "a", p.Version, "u", func(p *post.Post) error { p.Status = post.Archived; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if found("alpha") {
		t.Error("an archived post is in content search")
	}
	if got := searchSlugs(t, s, Filter{Query: "alpha", Status: post.Archived}); got != "a" {
		t.Errorf("archived search = %q, want a", got)
	}

	if err := s.DeletePost(ctx, "a", p.Version); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM posts_fts").Scan(&n); err != nil || n != 0 {
		t.Errorf("index rows after delete = %d, %v; want 0", n, err)
	}
}

func TestFailedChangeLeavesIndexAlone(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	p, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "Alpha", Status: post.Published}, "u")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(ctx, &post.Post{Slug: "a", Title: "Duplicate"}, "u"); err != ErrTaken {
		t.Fatalf("duplicate = %v", err)
	}
	if _, err := s.UpdatePost(ctx, "a", p.Version+1, "u", func(p *post.Post) error { p.Title = "Stale"; return nil }); err != ErrConflict {
		t.Fatalf("stale update = %v", err)
	}
	if _, total, _ := s.ListPublished(ctx, "alpha", "", 0, 0); total != 1 {
		t.Errorf("alpha found %d, want 1", total)
	}
	for _, q := range []string{"duplicate", "stale"} {
		if _, total, _ := s.ListPublished(ctx, q, "", 0, 0); total != 0 {
			t.Errorf("%q found %d from a failed change", q, total)
		}
	}
}

// A database from before migration 5 gets its posts indexed, tags included,
// when it is opened.
func TestMigrationBackfillsSearch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), FileName)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := append([]string{}, migrations[:4]...)
	stmts = append(stmts,
		`INSERT INTO posts (slug, title, description, status, body, author, created_at, updated_at)
			VALUES ('old', 'Old post', 'Ancient', 'published', 'Dusty résumé text', 'Ravi', 'x', 'x'),
			       ('bare', 'Bare', '', 'draft', '', '', 'x', 'x')`,
		`INSERT INTO post_tags (post_id, position, tag) VALUES (1, 0, 'history'), (1, 1, 'long-ago')`,
		"PRAGMA user_version = 4")
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for q, want := range map[string]string{
		"dusty": "old", "RESUME": "old", "ancient": "old", "ravi": "old", "history": "old", "ago": "old",
		"bare": "bare", "absent": "",
	} {
		if got := searchSlugs(t, s, Filter{Query: q}); got != want {
			t.Errorf("after migrating, %q found %q, want %q", q, got, want)
		}
	}
	// And the migrated index keeps working.
	p, err := s.GetPost(ctx, "old")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePost(ctx, "old", p.Version, "u", func(p *post.Post) error { p.Body = "Fresh words"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := searchSlugs(t, s, Filter{Query: "fresh"}); got != "old" {
		t.Errorf("edit after migrating found %q", got)
	}
	if got := searchSlugs(t, s, Filter{Query: "dusty"}); got != "" {
		t.Errorf("old text still found: %q", got)
	}
}

// searchBudget bounds one search of the panel list plus one of the content
// list. It is not checked under the race detector (see raceEnabled).
const searchBudget = 200 * time.Millisecond

func TestSearchStaysFastWithManyPosts(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// About 200 words each from a vocabulary of 500, so a word is in a
	// fair share of the posts; "topic3" is in every tenth title.
	for i := range 2000 {
		var body strings.Builder
		for w := range 200 {
			fmt.Fprintf(&body, "word%d ", (i*31+w*17)%500)
		}
		p := &post.Post{
			Slug: fmt.Sprintf("post-%04d", i), Title: fmt.Sprintf("Post number %d topic%d", i, i%10), Status: post.Published,
			Body: body.String() + fmt.Sprintf("unique%04d", i), Tags: []string{"bulk"},
		}
		if _, err := s.CreatePost(ctx, p, "u"); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"unique1234", "topic3", "word17 word34", "wor", "number 19"} {
		start := time.Now()
		list, total, err := s.ListPublished(ctx, q, "", 50, 0)
		panel, counts, perr := s.ListPosts(ctx, Filter{Query: q, Limit: 50})
		elapsed := time.Since(start)
		if err != nil || perr != nil || total == 0 || len(list) == 0 || len(panel) == 0 || counts.Matched != total {
			t.Fatalf("%q: %d/%d found, %v %v", q, total, counts.Matched, err, perr)
		}
		t.Logf("%q: %d matches in %v", q, total, elapsed)
		if elapsed > searchBudget && !raceEnabled {
			t.Errorf("searching %q took %v with 2000 posts, want under %v for both lists", q, elapsed, searchBudget)
		}
	}
}
