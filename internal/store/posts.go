package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/weburz/burzblog/internal/post"
)

const postColumns = `id, slug, title, description, status, body, cover_url, cover_alt, extra,
	published_on, updated_on, created_at, updated_at, updated_by, version, publish_at, publish_by`

type scanner interface {
	Scan(dest ...any) error
}

func scanPost(row scanner) (*post.Post, error) {
	var p post.Post
	var status, created, updated string
	var publishedOn, updatedOn, publishAt sql.NullString
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Description, &status, &p.Body, &p.CoverURL,
		&p.CoverAlt, &p.Extra, &publishedOn, &updatedOn, &created, &updated, &p.UpdatedBy, &p.Version, &publishAt, &p.PublishBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read post: %w", err)
	}
	p.Status = post.Status(status)
	p.PublishedOn = timePtr(publishedOn)
	p.UpdatedOn = timePtr(updatedOn)
	p.PublishAt = timePtr(publishAt)
	p.CreatedAt = parseStamp(created)
	p.UpdatedAt = parseStamp(updated)
	return &p, nil
}

// CreatePost stores a new post. Slug, title and status must be set.
func (s *Store) CreatePost(ctx context.Context, p *post.Post, by string) (*post.Post, error) {
	post.Normalize(p)
	if p.Status == "" {
		p.Status = post.Draft
	}
	if p.Status == post.Published && p.PublishedOn == nil {
		now := s.now().Truncate(time.Second)
		p.PublishedOn = &now
	}
	if err := post.Validate(p); err != nil {
		return nil, err
	}
	now := s.stamp()
	row := s.db.QueryRowContext(ctx, `INSERT INTO posts
		(slug, title, description, status, body, cover_url, cover_alt, extra, published_on, updated_on,
		 created_at, updated_at, updated_by, version, words)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
		ON CONFLICT (slug) DO NOTHING
		RETURNING `+postColumns,
		p.Slug, p.Title, p.Description, string(p.Status), p.Body, p.CoverURL, p.CoverAlt, p.Extra,
		nullTime(p.PublishedOn), nullTime(p.UpdatedOn), now, now, by, post.CountWords(p.Body))
	created, err := scanPost(row)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrTaken
	}
	return created, err
}

// GetPost loads one post by slug.
func (s *Store) GetPost(ctx context.Context, slug string) (*post.Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM posts WHERE slug = ?", slug))
}

// Filter narrows ListPosts.
type Filter struct {
	// Status limits results to one status; empty means drafts and published.
	Status post.Status
	// Scheduled limits results to drafts with a publish time.
	Scheduled bool
	// Query matches title, slug or description, case-insensitively.
	Query string
	// Sort orders the result; empty means SortUpdated.
	Sort Sort
	// Limit caps the number of posts returned and Offset skips that many
	// first, for paging; zero Limit means all of them.
	Limit, Offset int
}

// Sort names an ordering of ListPosts.
type Sort string

// The orderings ListPosts understands.
const (
	SortUpdated   Sort = "updated"
	SortPublished Sort = "published"
	SortTitle     Sort = "title"
	SortCreated   Sort = "created"
)

// ValidSort reports whether s is a known ordering.
func ValidSort(s Sort) bool {
	_, ok := orderBy[s]
	return ok
}

// orderBy holds fixed ORDER BY clauses; user input only selects one. Every
// clause ends in id so equal keys keep a stable order.
var orderBy = map[Sort]string{
	SortUpdated:   "updated_at DESC, id DESC",
	SortPublished: "published_on IS NULL, published_on DESC, updated_at DESC, id DESC",
	SortTitle:     "title COLLATE NOCASE ASC, id ASC",
	SortCreated:   "created_at DESC, id DESC",
}

// Summary is a post without its body, for lists.
type Summary struct {
	Slug        string
	Title       string
	Description string
	Status      post.Status
	Words       int
	PublishedOn *time.Time
	PublishAt   *time.Time
	UpdatedAt   time.Time
	UpdatedBy   string
	Version     int64
}

// Counts is the number of posts in each status. Scheduled counts the drafts
// that have a publish time, so it is part of Draft, not on top of it. Matched
// is the number of posts the filter matches, before Limit and Offset.
type Counts struct {
	Draft, Published, Archived, Scheduled, Matched int
}

// StatusCounts returns the number of posts in each status.
func (s *Store) StatusCounts(ctx context.Context) (Counts, error) {
	var counts Counts
	rows, err := s.db.QueryContext(ctx, "SELECT status, count(*) FROM posts GROUP BY status")
	if err != nil {
		return counts, fmt.Errorf("count posts: %w", err)
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return counts, fmt.Errorf("count posts: %w", err)
		}
		switch post.Status(status) {
		case post.Draft:
			counts.Draft = n
		case post.Published:
			counts.Published = n
		case post.Archived:
			counts.Archived = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return counts, fmt.Errorf("count posts: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM posts WHERE status = 'draft' AND publish_at IS NOT NULL").
		Scan(&counts.Scheduled); err != nil {
		return counts, fmt.Errorf("count scheduled posts: %w", err)
	}
	return counts, nil
}

// ListPosts returns matching posts, most recently changed first unless
// f.Sort says otherwise, and the count per status regardless of the filter.
// An unknown sort is an error.
func (s *Store) ListPosts(ctx context.Context, f Filter) ([]Summary, Counts, error) {
	if f.Sort == "" {
		f.Sort = SortUpdated
	}
	order, ok := orderBy[f.Sort]
	if !ok {
		return nil, Counts{}, fmt.Errorf("list posts: unknown sort %q", f.Sort)
	}
	counts, err := s.StatusCounts(ctx)
	if err != nil {
		return nil, counts, err
	}

	where, args := []string{}, []any{}
	if f.Scheduled {
		where = append(where, "status = 'draft' AND publish_at IS NOT NULL")
	} else if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(f.Status))
	} else {
		where = append(where, "status != 'archived'")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
		where = append(where, `(lower(title) LIKE ? ESCAPE '\' OR slug LIKE ? ESCAPE '\' OR lower(description) LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	cond := strings.Join(where, " AND ")
	query := `SELECT slug, title, description, status, words, published_on,
		publish_at, updated_at, updated_by, version FROM posts WHERE ` + cond + " ORDER BY " + order
	listArgs := args
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		listArgs = append(append([]any{}, args...), f.Limit, max(f.Offset, 0))
	}
	rows, err := s.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, counts, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()
	list := []Summary{}
	for rows.Next() {
		var sm Summary
		var status, updated string
		var publishedOn, publishAt sql.NullString
		if err := rows.Scan(&sm.Slug, &sm.Title, &sm.Description, &status, &sm.Words, &publishedOn, &publishAt,
			&updated, &sm.UpdatedBy, &sm.Version); err != nil {
			return nil, counts, fmt.Errorf("list posts: %w", err)
		}
		sm.Status = post.Status(status)
		sm.PublishedOn = timePtr(publishedOn)
		sm.PublishAt = timePtr(publishAt)
		sm.UpdatedAt = parseStamp(updated)
		list = append(list, sm)
	}
	if err := rows.Err(); err != nil {
		return nil, counts, fmt.Errorf("list posts: %w", err)
	}
	counts.Matched = len(list)
	if f.Limit > 0 && (f.Offset > 0 || len(list) == f.Limit) {
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM posts WHERE "+cond, args...).Scan(&counts.Matched); err != nil {
			return nil, counts, fmt.Errorf("count matching posts: %w", err)
		}
	}
	return list, counts, nil
}

// UpdatePost applies change to the post with slug, if its version still
// equals version. The previous content is kept as a revision. A change that
// leaves the content as it was returns the post unchanged, without a new
// version. On ErrConflict the current post is returned too.
func (s *Store) UpdatePost(ctx context.Context, slug string, version int64, by string,
	change func(*post.Post) error) (*post.Post, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update: %w", err)
	}
	defer tx.Rollback()
	current, err := scanPost(tx.QueryRowContext(ctx, "SELECT "+postColumns+" FROM posts WHERE slug = ?", slug))
	if err != nil {
		return nil, err
	}
	if current.Version != version {
		return current, ErrConflict
	}
	next := *current
	if err := change(&next); err != nil {
		return nil, err
	}
	post.Normalize(&next)
	// Only drafts wait for a publish time: publishing or archiving ends it.
	if next.Status != post.Draft || next.PublishAt == nil {
		next.PublishAt, next.PublishBy = nil, ""
	}
	// A published post keeps its first publish date when a save leaves the
	// date out, so re-pushing a file without one does not move the post.
	if next.Status == post.Published && next.PublishedOn == nil {
		if current.PublishedOn != nil {
			next.PublishedOn = current.PublishedOn
		} else {
			now := s.now().Truncate(time.Second)
			next.PublishedOn = &now
		}
	}
	if err := post.Validate(&next); err != nil {
		return nil, err
	}
	if post.SameContent(current, &next) {
		return current, nil
	}
	if next.Slug != current.Slug {
		var taken int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM posts WHERE slug = ?", next.Slug).Scan(&taken); err != nil {
			return nil, fmt.Errorf("check slug: %w", err)
		}
		if taken > 0 {
			return nil, ErrTaken
		}
	}
	now := s.stamp()
	if _, err := tx.ExecContext(ctx, `INSERT INTO revisions
		(post_id, version, slug, title, description, status, body, cover_url, cover_alt, extra,
		 published_on, updated_on, saved_at, saved_by, words)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		current.ID, current.Version, current.Slug, current.Title, current.Description, string(current.Status),
		current.Body, current.CoverURL, current.CoverAlt, current.Extra, nullTime(current.PublishedOn),
		nullTime(current.UpdatedOn), current.UpdatedAt.Format(time.RFC3339Nano), current.UpdatedBy, post.CountWords(current.Body)); err != nil {
		return nil, fmt.Errorf("keep revision: %w", err)
	}
	updated, err := scanPost(tx.QueryRowContext(ctx, `UPDATE posts SET
		slug = ?, title = ?, description = ?, status = ?, body = ?, cover_url = ?, cover_alt = ?, extra = ?,
		published_on = ?, updated_on = ?, publish_at = ?, publish_by = ?, updated_at = ?, updated_by = ?,
		words = ?, version = version + 1
		WHERE id = ? RETURNING `+postColumns,
		next.Slug, next.Title, next.Description, string(next.Status), next.Body, next.CoverURL, next.CoverAlt,
		next.Extra, nullTime(next.PublishedOn), nullTime(next.UpdatedOn), nullTime(next.PublishAt), next.PublishBy,
		now, by, post.CountWords(next.Body), current.ID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update: %w", err)
	}
	return updated, nil
}

// Scheduled is a draft waiting for its publish time.
type Scheduled struct {
	Slug    string
	Version int64
	At      time.Time
	By      string
}

// ScheduledPosts lists drafts with a publish time, soonest first.
func (s *Store) ScheduledPosts(ctx context.Context) ([]Scheduled, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT slug, version, publish_at, publish_by FROM posts
		WHERE status = 'draft' AND publish_at IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("list scheduled posts: %w", err)
	}
	defer rows.Close()
	var list []Scheduled
	for rows.Next() {
		var sc Scheduled
		var at string
		if err := rows.Scan(&sc.Slug, &sc.Version, &at, &sc.By); err != nil {
			return nil, fmt.Errorf("list scheduled posts: %w", err)
		}
		sc.At = parseStamp(at)
		list = append(list, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list scheduled posts: %w", err)
	}
	// Sorted here rather than in SQL: stored times are text.
	sort.Slice(list, func(i, j int) bool {
		if !list[i].At.Equal(list[j].At) {
			return list[i].At.Before(list[j].At)
		}
		return list[i].Slug < list[j].Slug
	})
	return list, nil
}

// DeletePost removes an archived post and its revisions for good.
func (s *Store) DeletePost(ctx context.Context, slug string, version int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete: %w", err)
	}
	defer tx.Rollback()
	current, err := scanPost(tx.QueryRowContext(ctx, "SELECT "+postColumns+" FROM posts WHERE slug = ?", slug))
	if err != nil {
		return err
	}
	if current.Version != version {
		return ErrConflict
	}
	if current.Status != post.Archived {
		return ErrState
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM posts WHERE id = ?", current.ID); err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete: %w", err)
	}
	return nil
}

// Revision is an earlier version of a post.
type Revision struct {
	Version int64
	SavedAt time.Time
	SavedBy string
	// Words is set by Revisions, which leaves the body out.
	Words int
	Post  post.Post
}

// Revisions lists a post's earlier versions, newest first, without bodies but
// with word counts.
func (s *Store) Revisions(ctx context.Context, slug string) ([]Revision, error) {
	p, err := s.GetPost(ctx, slug)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT version, slug, title, description, status, words, cover_url, cover_alt,
		extra, published_on, updated_on, saved_at, saved_by
		FROM revisions WHERE post_id = ? ORDER BY version DESC LIMIT 200`, p.ID)
	if err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	defer rows.Close()
	list := []Revision{}
	for rows.Next() {
		var r Revision
		var status, saved string
		var publishedOn, updatedOn sql.NullString
		if err := rows.Scan(&r.Version, &r.Post.Slug, &r.Post.Title, &r.Post.Description, &status, &r.Words,
			&r.Post.CoverURL, &r.Post.CoverAlt, &r.Post.Extra, &publishedOn, &updatedOn, &saved, &r.SavedBy); err != nil {
			return nil, fmt.Errorf("list revisions: %w", err)
		}
		r.Post.Status = post.Status(status)
		r.Post.PublishedOn = timePtr(publishedOn)
		r.Post.UpdatedOn = timePtr(updatedOn)
		r.SavedAt = parseStamp(saved)
		list = append(list, r)
	}
	return list, rows.Err()
}

// Revision loads one earlier version of a post in full.
func (s *Store) Revision(ctx context.Context, slug string, version int64) (*Revision, error) {
	p, err := s.GetPost(ctx, slug)
	if err != nil {
		return nil, err
	}
	var r Revision
	var status, saved string
	var publishedOn, updatedOn sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT version, slug, title, description, status, body, cover_url,
		cover_alt, extra, published_on, updated_on, saved_at, saved_by
		FROM revisions WHERE post_id = ? AND version = ?`, p.ID, version).Scan(
		&r.Version, &r.Post.Slug, &r.Post.Title, &r.Post.Description, &status, &r.Post.Body, &r.Post.CoverURL,
		&r.Post.CoverAlt, &r.Post.Extra, &publishedOn, &updatedOn, &saved, &r.SavedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read revision: %w", err)
	}
	r.Post.Status = post.Status(status)
	r.Post.PublishedOn = timePtr(publishedOn)
	r.Post.UpdatedOn = timePtr(updatedOn)
	r.SavedAt = parseStamp(saved)
	return &r, nil
}

// ForEachPost calls fn with every post in full, whatever its status, by
// status and slug, one at a time so a large blog is never held in memory.
func (s *Store) ForEachPost(ctx context.Context, fn func(*post.Post) error) error {
	return s.forEach(ctx, "SELECT "+postColumns+" FROM posts ORDER BY status, slug", fn)
}

// ForEachPublished calls fn with every published post in full, by slug, one
// at a time.
func (s *Store) ForEachPublished(ctx context.Context, fn func(*post.Post) error) error {
	return s.forEach(ctx, "SELECT "+postColumns+" FROM posts WHERE status = 'published' ORDER BY slug", fn)
}

func (s *Store) forEach(ctx context.Context, query string, fn func(*post.Post) error) error {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return err
		}
		if err := fn(p); err != nil {
			return err
		}
	}
	return rows.Err()
}

// PublishedPosts returns every published post in full, by slug.
func (s *Store) PublishedPosts(ctx context.Context) ([]*post.Post, error) {
	var list []*post.Post
	err := s.ForEachPublished(ctx, func(p *post.Post) error {
		list = append(list, p)
		return nil
	})
	return list, err
}
