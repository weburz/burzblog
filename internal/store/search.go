package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	"github.com/weburz/burzblog/internal/post"
)

// Search runs on posts_fts, an FTS5 table whose rowid is the post's id. It
// holds its own copy of title, description, body, tags (space-joined), author
// and slug, because tags live in another table and an external-content table
// could not index them. Go keeps it current in the transaction of every
// change to a post (indexPost, unindexPost) rather than triggers, which could
// not see a post's tags: they are written after the post row.

// rankedOrder is the ORDER BY of a search with no sort asked for: best match
// first (rank comes from searchSource), then the most recently changed.
const rankedOrder = "rank, updated_at DESC, id DESC"

// searchTerms caps the words of a search, so a pasted paragraph stays cheap.
const searchTerms = 16

// searchWeights are the bm25 column weights, in posts_fts column order:
// title, description, body, tags, author, slug. A title match counts most.
const searchWeights = "10, 4, 1, 5, 2, 3"

// matchExpr turns what a person typed into an FTS5 MATCH expression, or ""
// when it holds no searchable word. The input is split into runs of letters,
// digits and combining marks, so no quote, operator or column filter can
// reach FTS5: every run becomes a quoted phrase (so OR, AND, NOT and NEAR are
// ordinary words), all must match, and the last matches as a prefix, so a
// search finds posts while it is typed.
func matchExpr(input string) string {
	words := strings.FieldsFunc(input, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	})
	if len(words) == 0 {
		return ""
	}
	words = words[:min(len(words), searchTerms)]
	for i, w := range words {
		words[i] = `"` + w + `"`
	}
	return strings.Join(words, " ") + "*"
}

// searchSource is a FROM term and its arguments: posts joined to the posts
// that match expr, with their rank (lower is better).
func searchSource(expr string) (string, []any) {
	return "posts JOIN (SELECT rowid AS id, bm25(posts_fts, " + searchWeights +
		") AS rank FROM posts_fts WHERE posts_fts MATCH ?) hits USING (id)", []any{expr}
}

// maxSnippetIDs is how many post ids snippets passes as query parameters. A
// longer page, which only an unpaged listing can be, reads the excerpt of
// every match instead, rather than reaching SQLite's variable limit.
const maxSnippetIDs = 500

// snippets returns a short plain-text excerpt of the body of the posts with
// ids that expr matches, by id. It is a separate query so only the posts of
// one page pay for an excerpt, not every match.
func snippets(ctx context.Context, q queryer, expr string, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	query, args := `SELECT rowid, snippet(posts_fts, 2, '', '', '…', 24) FROM posts_fts WHERE posts_fts MATCH ?`, []any{expr}
	if len(ids) <= maxSnippetIDs {
		query += " AND rowid IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("excerpt posts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var text string
		if err := rows.Scan(&id, &text); err != nil {
			return nil, fmt.Errorf("excerpt posts: %w", err)
		}
		out[id] = text
	}
	return out, rows.Err()
}

// indexPost makes the search index agree with p, which must carry its tags.
func indexPost(ctx context.Context, tx *sql.Tx, p *post.Post) error {
	if err := unindexPost(ctx, tx, p.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO posts_fts (rowid, title, description, body, tags, author, slug)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Title, p.Description, p.Body, strings.Join(p.Tags, " "), p.Author, p.Slug); err != nil {
		return fmt.Errorf("index post: %w", err)
	}
	return nil
}

// unindexPost removes a post from the search index.
func unindexPost(ctx context.Context, tx *sql.Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM posts_fts WHERE rowid = ?", id); err != nil {
		return fmt.Errorf("unindex post: %w", err)
	}
	return nil
}

// backfillSearch indexes the posts that existed before the search index did.
func backfillSearch(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO posts_fts (rowid, title, description, body, tags, author, slug)
		SELECT id, title, description, body,
			coalesce((SELECT group_concat(tag, ' ') FROM
				(SELECT tag FROM post_tags WHERE post_id = posts.id ORDER BY position)), ''),
			author, slug FROM posts`)
	return err
}
