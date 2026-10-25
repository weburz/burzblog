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
// matches title, slug or description like ListPosts does. Bodies are not read.
func (s *Store) ListPublished(ctx context.Context, query string, limit, offset int) ([]Listing, int, error) {
	where, args := "status = 'published'", []any{}
	if q := strings.TrimSpace(query); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
		where += ` AND (lower(title) LIKE ? ESCAPE '\' OR slug LIKE ? ESCAPE '\' OR lower(description) LIKE ? ESCAPE '\')`
		args = append(args, like, like, like)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM posts WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count published posts: %w", err)
	}
	stmt := `SELECT slug, title, description, cover_url, cover_alt, words, published_on, updated_on,
		updated_at, version FROM posts WHERE ` + where + " ORDER BY " + orderBy[SortPublished]
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
		if err := rows.Scan(&p.Slug, &p.Title, &p.Description, &p.CoverURL, &p.CoverAlt, &l.Words,
			&publishedOn, &updatedOn, &updated, &p.Version); err != nil {
			return nil, 0, fmt.Errorf("list published posts: %w", err)
		}
		p.PublishedOn, p.UpdatedOn, p.UpdatedAt = timePtr(publishedOn), timePtr(updatedOn), parseStamp(updated)
		l.Post = p
		list = append(list, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list published posts: %w", err)
	}
	return list, total, nil
}
