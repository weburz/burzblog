package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/weburz/burzblog/internal/post"
)

const postColumns = `id, slug, title, description, status, body, cover_url, cover_alt, extra,
	published_on, updated_on, created_at, updated_at, updated_by, version, publish_at, publish_by,
	author, seo_title, seo_description, canonical_url, featured`

type scanner interface {
	Scan(dest ...any) error
}

func scanPost(row scanner) (*post.Post, error) {
	var p post.Post
	var status, created, updated string
	var publishedOn, updatedOn, publishAt sql.NullString
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Description, &status, &p.Body, &p.CoverURL,
		&p.CoverAlt, &p.Extra, &publishedOn, &updatedOn, &created, &updated, &p.UpdatedBy, &p.Version, &publishAt, &p.PublishBy,
		&p.Author, &p.SEOTitle, &p.SEODescription, &p.CanonicalURL, &p.Featured)
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

// queryer is what *sql.DB and *sql.Tx share.
type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// tagsByPost reads (post_id, tag) rows in tag order and groups them by post.
func tagsByPost(ctx context.Context, q queryer, query string, args ...any) (map[int64][]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	defer rows.Close()
	tags := map[int64][]string{}
	for rows.Next() {
		var id int64
		var tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, fmt.Errorf("read tags: %w", err)
		}
		tags[id] = append(tags[id], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	return tags, nil
}

// loadTags fills in the tags of posts that were just read.
func loadTags(ctx context.Context, q queryer, posts ...*post.Post) error {
	if len(posts) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(posts)), ",")
	ids := make([]any, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	tags, err := tagsByPost(ctx, q, "SELECT post_id, tag FROM post_tags WHERE post_id IN ("+marks+") ORDER BY post_id, position", ids...)
	if err != nil {
		return err
	}
	for _, p := range posts {
		p.Tags = tags[p.ID]
	}
	return nil
}

// insertTags stores a post's tags in order.
func insertTags(ctx context.Context, tx *sql.Tx, id int64, tags []string) error {
	for i, tag := range tags {
		if _, err := tx.ExecContext(ctx, "INSERT INTO post_tags (post_id, position, tag) VALUES (?, ?, ?)", id, i, tag); err != nil {
			return fmt.Errorf("keep tag: %w", err)
		}
	}
	return nil
}

// CreatePost stores a new post. Slug, title and status must be set. A post
// without an author is credited to the account creating it.
func (s *Store) CreatePost(ctx context.Context, p *post.Post, by string) (*post.Post, error) {
	post.Normalize(p)
	if p.Author == "" {
		p.Author = by
	}
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create: %w", err)
	}
	defer tx.Rollback()
	now := s.stamp()
	row := tx.QueryRowContext(ctx, `INSERT INTO posts
		(slug, title, description, status, body, cover_url, cover_alt, extra, published_on, updated_on,
		 created_at, updated_at, updated_by, version, words, author, seo_title, seo_description, canonical_url, featured)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (slug) DO NOTHING
		RETURNING `+postColumns,
		p.Slug, p.Title, p.Description, string(p.Status), p.Body, p.CoverURL, p.CoverAlt, p.Extra,
		nullTime(p.PublishedOn), nullTime(p.UpdatedOn), now, now, by, post.CountWords(p.Body),
		p.Author, p.SEOTitle, p.SEODescription, p.CanonicalURL, p.Featured)
	created, err := scanPost(row)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrTaken
	}
	if err != nil {
		return nil, err
	}
	if err := insertTags(ctx, tx, created.ID, p.Tags); err != nil {
		return nil, err
	}
	created.Tags = p.Tags
	if err := indexPost(ctx, tx, created); err != nil {
		return nil, err
	}
	// A live slug wins over a redirect that used to claim it.
	if err := dropRedirect(ctx, tx, created.Slug); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create: %w", err)
	}
	return created, nil
}

// GetPost loads one post by slug.
func (s *Store) GetPost(ctx context.Context, slug string) (*post.Post, error) {
	p, err := scanPost(s.db.QueryRowContext(ctx, "SELECT "+postColumns+" FROM posts WHERE slug = ?", slug))
	if err != nil {
		return nil, err
	}
	return p, loadTags(ctx, s.db, p)
}

// Filter narrows ListPosts.
type Filter struct {
	// Status limits results to one status; empty means drafts and published.
	Status post.Status
	// Scheduled limits results to drafts with a publish time.
	Scheduled bool
	// Query is searched for in title, description, body, tags, author and
	// slug as plain words, all of which must appear, the last one as a
	// prefix, ignoring case and accents. Without Sort, better matches
	// come first. A Query without a word in it filters nothing.
	Query string
	// Tag limits results to posts with this tag. It is normalized like a
	// post's tags, so "Machine Learning" finds "machine-learning".
	Tag string
	// Featured limits results to featured (true) or not featured (false)
	// posts; nil means both.
	Featured *bool
	// Sort orders the result; empty means SortUpdated, or best match first
	// when there is a Query.
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
	Tags        []string
	Author      string
	Featured    bool
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

// TagCount is a tag and the number of posts that carry it.
type TagCount struct {
	Tag   string
	Count int
}

// Tags lists every tag in use with its number of posts, archived ones
// included, by tag.
func (s *Store) Tags(ctx context.Context) ([]TagCount, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT tag, count(*) FROM post_tags GROUP BY tag ORDER BY tag")
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	list := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, fmt.Errorf("list tags: %w", err)
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// ListPosts returns matching posts, most recently changed first unless
// f.Sort says otherwise, and the count per status regardless of the filter.
// An unknown sort is an error.
func (s *Store) ListPosts(ctx context.Context, f Filter) ([]Summary, Counts, error) {
	match := matchExpr(f.Query)
	order, ok := orderBy[f.Sort]
	switch {
	case f.Sort == "" && match != "":
		order, ok = rankedOrder, true
	case f.Sort == "":
		order, ok = orderBy[SortUpdated], true
	}
	if !ok {
		return nil, Counts{}, fmt.Errorf("list posts: unknown sort %q", f.Sort)
	}
	counts, err := s.StatusCounts(ctx)
	if err != nil {
		return nil, counts, err
	}

	from, args := "posts", []any{}
	if match != "" {
		from, args = searchSource(match)
	}
	where := []string{}
	if f.Scheduled {
		where = append(where, "status = 'draft' AND publish_at IS NOT NULL")
	} else if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(f.Status))
	} else {
		where = append(where, "status != 'archived'")
	}
	if f.Tag != "" {
		// A tag that normalizes to nothing matches no post.
		where = append(where, "id IN (SELECT post_id FROM post_tags WHERE tag = ?)")
		args = append(args, post.NormalizeTag(f.Tag))
	}
	if f.Featured != nil {
		where = append(where, "featured = ?")
		args = append(args, *f.Featured)
	}
	cond := strings.Join(where, " AND ")
	page := " FROM " + from + " WHERE " + cond + " ORDER BY " + order
	listArgs := args
	if f.Limit > 0 {
		page += " LIMIT ? OFFSET ?"
		listArgs = append(append([]any{}, args...), f.Limit, max(f.Offset, 0))
	}
	query := `SELECT id, slug, title, description, status, words, published_on,
		publish_at, updated_at, updated_by, version, author, featured` + page
	rows, err := s.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, counts, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()
	list := []Summary{}
	ids := []int64{}
	for rows.Next() {
		var sm Summary
		var id int64
		var status, updated string
		var publishedOn, publishAt sql.NullString
		if err := rows.Scan(&id, &sm.Slug, &sm.Title, &sm.Description, &status, &sm.Words, &publishedOn, &publishAt,
			&updated, &sm.UpdatedBy, &sm.Version, &sm.Author, &sm.Featured); err != nil {
			return nil, counts, fmt.Errorf("list posts: %w", err)
		}
		ids = append(ids, id)
		sm.Status = post.Status(status)
		sm.PublishedOn = timePtr(publishedOn)
		sm.PublishAt = timePtr(publishAt)
		sm.UpdatedAt = parseStamp(updated)
		list = append(list, sm)
	}
	if err := rows.Err(); err != nil {
		return nil, counts, fmt.Errorf("list posts: %w", err)
	}
	rows.Close()
	if len(list) > 0 {
		// The same page again, as a subquery, so the number of posts never
		// becomes a number of query parameters.
		tags, err := tagsByPost(ctx, s.db, `SELECT post_id, tag FROM post_tags WHERE post_id IN (SELECT id`+page+
			`) ORDER BY post_id, position`, listArgs...)
		if err != nil {
			return nil, counts, err
		}
		for i := range list {
			list[i].Tags = tags[ids[i]]
		}
	}
	counts.Matched = len(list)
	if f.Limit > 0 && (f.Offset > 0 || len(list) == f.Limit) {
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+from+" WHERE "+cond, args...).Scan(&counts.Matched); err != nil {
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
	if err := loadTags(ctx, tx, current); err != nil {
		return nil, err
	}
	if current.Version != version {
		return current, ErrConflict
	}
	next := *current
	next.Tags = slices.Clone(current.Tags)
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
	oldTags, err := json.Marshal(current.Tags)
	if err != nil {
		return nil, fmt.Errorf("keep revision: %w", err)
	}
	if current.Tags == nil {
		oldTags = []byte("[]")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO revisions
		(post_id, version, slug, title, description, status, body, cover_url, cover_alt, extra,
		 published_on, updated_on, saved_at, saved_by, words, tags, author, seo_title, seo_description, canonical_url, featured)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		current.ID, current.Version, current.Slug, current.Title, current.Description, string(current.Status),
		current.Body, current.CoverURL, current.CoverAlt, current.Extra, nullTime(current.PublishedOn),
		nullTime(current.UpdatedOn), current.UpdatedAt.Format(time.RFC3339Nano), current.UpdatedBy, post.CountWords(current.Body),
		string(oldTags), current.Author, current.SEOTitle, current.SEODescription, current.CanonicalURL,
		current.Featured); err != nil {
		return nil, fmt.Errorf("keep revision: %w", err)
	}
	updated, err := scanPost(tx.QueryRowContext(ctx, `UPDATE posts SET
		slug = ?, title = ?, description = ?, status = ?, body = ?, cover_url = ?, cover_alt = ?, extra = ?,
		published_on = ?, updated_on = ?, publish_at = ?, publish_by = ?, updated_at = ?, updated_by = ?,
		words = ?, author = ?, seo_title = ?, seo_description = ?, canonical_url = ?, featured = ?,
		version = version + 1
		WHERE id = ? RETURNING `+postColumns,
		next.Slug, next.Title, next.Description, string(next.Status), next.Body, next.CoverURL, next.CoverAlt,
		next.Extra, nullTime(next.PublishedOn), nullTime(next.UpdatedOn), nullTime(next.PublishAt), next.PublishBy,
		now, by, post.CountWords(next.Body), next.Author, next.SEOTitle, next.SEODescription, next.CanonicalURL,
		next.Featured, current.ID))
	if err != nil {
		return nil, err
	}
	if !slices.Equal(current.Tags, next.Tags) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM post_tags WHERE post_id = ?", current.ID); err != nil {
			return nil, fmt.Errorf("replace tags: %w", err)
		}
		if err := insertTags(ctx, tx, current.ID, next.Tags); err != nil {
			return nil, err
		}
	}
	if next.Slug != current.Slug {
		// The new slug is live, so it stops being a redirect; the old one
		// starts being one. Earlier redirects of this post point at the
		// post, so they follow the rename without any work.
		if err := dropRedirect(ctx, tx, next.Slug); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO slug_redirects (old_slug, post_id, created_at)
			VALUES (?, ?, ?) ON CONFLICT (old_slug) DO UPDATE SET post_id = excluded.post_id`,
			current.Slug, current.ID, now); err != nil {
			return nil, fmt.Errorf("keep redirect: %w", err)
		}
	}
	updated.Tags = next.Tags
	if err := indexPost(ctx, tx, updated); err != nil {
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
	if err := unindexPost(ctx, tx, current.ID); err != nil {
		return err
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
		extra, published_on, updated_on, saved_at, saved_by, tags, author, seo_title, seo_description, canonical_url,
		featured FROM revisions WHERE post_id = ? ORDER BY version DESC LIMIT 200`, p.ID)
	if err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	defer rows.Close()
	list := []Revision{}
	for rows.Next() {
		var r Revision
		var status, saved, tags string
		var publishedOn, updatedOn sql.NullString
		if err := rows.Scan(&r.Version, &r.Post.Slug, &r.Post.Title, &r.Post.Description, &status, &r.Words,
			&r.Post.CoverURL, &r.Post.CoverAlt, &r.Post.Extra, &publishedOn, &updatedOn, &saved, &r.SavedBy,
			&tags, &r.Post.Author, &r.Post.SEOTitle, &r.Post.SEODescription, &r.Post.CanonicalURL,
			&r.Post.Featured); err != nil {
			return nil, fmt.Errorf("list revisions: %w", err)
		}
		if r.Post.Tags, err = decodeTags(tags); err != nil {
			return nil, err
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
	var status, saved, tags string
	var publishedOn, updatedOn sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT version, slug, title, description, status, body, cover_url,
		cover_alt, extra, published_on, updated_on, saved_at, saved_by, tags, author, seo_title,
		seo_description, canonical_url, featured
		FROM revisions WHERE post_id = ? AND version = ?`, p.ID, version).Scan(
		&r.Version, &r.Post.Slug, &r.Post.Title, &r.Post.Description, &status, &r.Post.Body, &r.Post.CoverURL,
		&r.Post.CoverAlt, &r.Post.Extra, &publishedOn, &updatedOn, &saved, &r.SavedBy,
		&tags, &r.Post.Author, &r.Post.SEOTitle, &r.Post.SEODescription, &r.Post.CanonicalURL, &r.Post.Featured)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read revision: %w", err)
	}
	if r.Post.Tags, err = decodeTags(tags); err != nil {
		return nil, err
	}
	r.Post.Status = post.Status(status)
	r.Post.PublishedOn = timePtr(publishedOn)
	r.Post.UpdatedOn = timePtr(updatedOn)
	r.SavedAt = parseStamp(saved)
	return &r, nil
}

// decodeTags reads the JSON array a revision keeps its tags in.
func decodeTags(raw string) ([]string, error) {
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil, fmt.Errorf("read revision tags: %w", err)
	}
	return tags, nil
}

// ForEachPost calls fn with every post in full, whatever its status, by
// status and slug, one at a time so a large blog is never held in memory.
func (s *Store) ForEachPost(ctx context.Context, fn func(*post.Post) error) error {
	return s.forEach(ctx, "", fn)
}

// ForEachPublished calls fn with every published post in full, by slug, one
// at a time.
func (s *Store) ForEachPublished(ctx context.Context, fn func(*post.Post) error) error {
	return s.forEach(ctx, "status = 'published' AND ", fn)
}

// forEachBatch is how many posts forEach reads per query.
const forEachBatch = 100

// forEach reads posts in (status, slug) order in small batches and calls fn
// between queries, so a slow reader of an export never holds one of the few
// database connections while it downloads.
func (s *Store) forEach(ctx context.Context, where string, fn func(*post.Post) error) error {
	var lastStatus, lastSlug string
	for {
		rows, err := s.db.QueryContext(ctx, "SELECT "+postColumns+" FROM posts WHERE "+where+
			"(status, slug) > (?, ?) ORDER BY status, slug LIMIT ?", lastStatus, lastSlug, forEachBatch)
		if err != nil {
			return fmt.Errorf("list posts: %w", err)
		}
		var batch []*post.Post
		for rows.Next() {
			p, err := scanPost(rows)
			if err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("list posts: %w", err)
		}
		if err := loadTags(ctx, s.db, batch...); err != nil {
			return err
		}
		for _, p := range batch {
			if err := fn(p); err != nil {
				return err
			}
		}
		if len(batch) < forEachBatch {
			return nil
		}
		last := batch[len(batch)-1]
		lastStatus, lastSlug = string(last.Status), last.Slug
	}
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
