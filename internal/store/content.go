package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/weburz/burzblog/internal/post"
)

// Listing is a published post without its body, as the content API lists it.
// Words is the stored word count, which the post itself does not carry.
type Listing struct {
	Post  *post.Post
	Words int
}

// ListPublished returns published posts, newest published first, and how many
// match before limit and offset apply. A zero limit returns every match. query
// matches title, slug or description like ListPosts does, and a non-empty tag
// keeps only posts with that tag, normalized like ListPosts. Bodies are not read.
func (s *Store) ListPublished(ctx context.Context, query, tag string, limit, offset int) ([]Listing, int, error) {
	where, args := "status = 'published'", []any{}
	if tag != "" {
		where += " AND id IN (SELECT post_id FROM post_tags WHERE tag = ?)"
		args = append(args, post.NormalizeTag(tag))
	}
	if q := strings.TrimSpace(query); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
		where += ` AND (lower(title) LIKE ? ESCAPE '\' OR slug LIKE ? ESCAPE '\' OR lower(description) LIKE ? ESCAPE '\')`
		args = append(args, like, like, like)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM posts WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count published posts: %w", err)
	}
	stmt := `SELECT id, slug, title, description, cover_url, cover_alt, words, published_on, updated_on,
		updated_at, version, author, seo_title, seo_description, canonical_url FROM posts WHERE ` + where + " ORDER BY " + orderBy[SortPublished]
	if limit > 0 {
		stmt += " LIMIT ? OFFSET ?"
		args = append(args, limit, max(offset, 0))
	}
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list published posts: %w", err)
	}
	defer rows.Close()
	list := []Listing{}
	for rows.Next() {
		p := &post.Post{Status: post.Published}
		var l Listing
		var publishedOn, updatedOn sql.NullString
		var updated string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Description, &p.CoverURL, &p.CoverAlt, &l.Words,
			&publishedOn, &updatedOn, &updated, &p.Version, &p.Author, &p.SEOTitle, &p.SEODescription,
			&p.CanonicalURL); err != nil {
			return nil, 0, fmt.Errorf("list published posts: %w", err)
		}
		p.PublishedOn, p.UpdatedOn, p.UpdatedAt = timePtr(publishedOn), timePtr(updatedOn), parseStamp(updated)
		l.Post = p
		list = append(list, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list published posts: %w", err)
	}
	// One query for every published post's tags, rather than an IN list
	// that a large unpaged listing could push past SQLite's variable limit.
	tags, err := tagsByPost(ctx, s.db, `SELECT post_id, tag FROM post_tags WHERE post_id IN
		(SELECT id FROM posts WHERE status = 'published') ORDER BY post_id, position`)
	if err != nil {
		return nil, 0, err
	}
	for _, l := range list {
		l.Post.Tags = tags[l.Post.ID]
	}
	return list, total, nil
}

// PublishedTags lists the tags of published posts with how many published
// posts carry each, by tag. Tags used only by drafts or archived posts are
// left out, so a website never learns about unpublished work.
func (s *Store) PublishedTags(ctx context.Context) ([]TagCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.tag, count(*) FROM post_tags t
		JOIN posts p ON p.id = t.post_id WHERE p.status = 'published' GROUP BY t.tag ORDER BY t.tag`)
	if err != nil {
		return nil, fmt.Errorf("list published tags: %w", err)
	}
	defer rows.Close()
	list := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, fmt.Errorf("list published tags: %w", err)
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list published tags: %w", err)
	}
	return list, nil
}
