package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/weburz/burzblog/internal/post"
)

// Listing is a published post without its body, as the content API lists it.
// Words is the stored word count, which the post itself does not carry.
// Snippet is set by a search: a short plain-text excerpt of the Markdown body
// near the match, which a caller must escape before putting it in a page.
type Listing struct {
	Post    *post.Post
	Words   int
	Snippet string
}

// ListPublished returns published posts, newest published first, and how many
// match before limit and offset apply. A zero limit returns every match. A
// query is searched for like Filter.Query does, best match first, and a
// non-empty tag keeps only posts with that tag, normalized like ListPosts.
// Bodies are not returned.
func (s *Store) ListPublished(ctx context.Context, query, tag string, limit, offset int) ([]Listing, int, error) {
	return s.ListPublishedFeatured(ctx, query, tag, nil, limit, offset)
}

// ListPublishedFeatured is ListPublished limited to featured posts when
// featured points at true and to the others when it points at false. Nil keeps
// both. The order is the same: newest published first.
func (s *Store) ListPublishedFeatured(ctx context.Context, query, tag string, featured *bool, limit, offset int) ([]Listing, int, error) {
	match := matchExpr(query)
	from, args, order := "posts", []any{}, orderBy[SortPublished]
	if match != "" {
		from, args = searchSource(match)
		order = rankedOrder
	}
	where := "status = 'published'"
	if tag != "" {
		where += " AND id IN (SELECT post_id FROM post_tags WHERE tag = ?)"
		args = append(args, post.NormalizeTag(tag))
	}
	if featured != nil {
		where += " AND featured = ?"
		args = append(args, *featured)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+from+" WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count published posts: %w", err)
	}
	stmt := `SELECT id, slug, title, description, cover_url, cover_alt, words, published_on, updated_on,
		updated_at, version, author, seo_title, seo_description, canonical_url, featured FROM ` + from + ` WHERE ` + where + " ORDER BY " + order
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
			&p.CanonicalURL, &p.Featured); err != nil {
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
	if match != "" {
		ids := make([]int64, len(list))
		for i, l := range list {
			ids[i] = l.Post.ID
		}
		excerpts, err := snippets(ctx, s.db, match, ids)
		if err != nil {
			return nil, 0, err
		}
		for i := range list {
			list[i].Snippet = excerpts[ids[i]]
		}
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
