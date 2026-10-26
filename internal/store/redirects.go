package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/weburz/burzblog/internal/post"
)

// Redirect sends an old slug of a renamed post to the slug it has now.
type Redirect struct {
	From, To string
}

// dropRedirect removes the redirect that starts at slug, if any.
func dropRedirect(ctx context.Context, tx *sql.Tx, slug string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM slug_redirects WHERE old_slug = ?", slug); err != nil {
		return fmt.Errorf("drop redirect: %w", err)
	}
	return nil
}

// PostRedirects lists the old slugs that redirect to the post with slug, by
// slug. It returns ErrNotFound when there is no such post.
func (s *Store) PostRedirects(ctx context.Context, slug string) ([]string, error) {
	p, err := s.GetPost(ctx, slug)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT old_slug FROM slug_redirects WHERE post_id = ? ORDER BY old_slug", p.ID)
	if err != nil {
		return nil, fmt.Errorf("list redirects: %w", err)
	}
	defer rows.Close()
	list := []string{}
	for rows.Next() {
		var from string
		if err := rows.Scan(&from); err != nil {
			return nil, fmt.Errorf("list redirects: %w", err)
		}
		list = append(list, from)
	}
	return list, rows.Err()
}

// DeleteRedirect removes the redirect from one old slug of the post with
// slug. It returns ErrNotFound when the post or that redirect does not exist.
func (s *Store) DeleteRedirect(ctx context.Context, slug, from string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM slug_redirects WHERE old_slug = ?
		AND post_id = (SELECT id FROM posts WHERE slug = ?)`, from, slug)
	if err != nil {
		return fmt.Errorf("delete redirect: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// PublishedRedirects lists the redirects whose post is published, by old
// slug. Redirects to drafts and archived posts are left out, so a website
// never learns about unpublished work.
func (s *Store) PublishedRedirects(ctx context.Context) ([]Redirect, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.old_slug, p.slug FROM slug_redirects r
		JOIN posts p ON p.id = r.post_id WHERE p.status = 'published' ORDER BY r.old_slug`)
	if err != nil {
		return nil, fmt.Errorf("list redirects: %w", err)
	}
	defer rows.Close()
	list := []Redirect{}
	for rows.Next() {
		var r Redirect
		if err := rows.Scan(&r.From, &r.To); err != nil {
			return nil, fmt.Errorf("list redirects: %w", err)
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// RedirectTarget returns the current slug of the published post that from
// used to be. It returns ErrNotFound when from is not a redirect or its post
// is not published.
func (s *Store) RedirectTarget(ctx context.Context, from string) (string, error) {
	var to string
	err := s.db.QueryRowContext(ctx, `SELECT p.slug FROM slug_redirects r
		JOIN posts p ON p.id = r.post_id WHERE r.old_slug = ? AND p.status = ?`, from, string(post.Published)).Scan(&to)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read redirect: %w", err)
	}
	return to, nil
}
