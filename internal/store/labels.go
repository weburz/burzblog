package store

import (
	"context"
	"fmt"

	"github.com/weburz/burzblog/internal/post"
)

// TagLabels returns the display labels people set, by tag. A tag without an
// entry shows post.DefaultTagLabel.
func (s *Store) TagLabels(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT tag, label FROM tag_labels")
	if err != nil {
		return nil, fmt.Errorf("list tag labels: %w", err)
	}
	defer rows.Close()
	labels := map[string]string{}
	for rows.Next() {
		var tag, label string
		if err := rows.Scan(&tag, &label); err != nil {
			return nil, fmt.Errorf("list tag labels: %w", err)
		}
		labels[tag] = label
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tag labels: %w", err)
	}
	return labels, nil
}

// SetTagLabel gives a tag in use a display label and returns the label as
// stored, trimmed. The tag must be normalized and carried by some post, so
// labels cannot pile up for tags that were never used; otherwise it returns
// ErrNotFound. An invalid label is a *post.FieldError.
func (s *Store) SetTagLabel(ctx context.Context, tag, label, by string) (string, error) {
	label, err := post.NormalizeTagLabel(label)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tag label: %w", err)
	}
	defer tx.Rollback()
	var used int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM post_tags WHERE tag = ?", tag).Scan(&used); err != nil {
		return "", fmt.Errorf("check tag: %w", err)
	}
	if used == 0 {
		return "", ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tag_labels (tag, label, updated_at, updated_by) VALUES (?, ?, ?, ?)
		ON CONFLICT (tag) DO UPDATE SET label = excluded.label, updated_at = excluded.updated_at,
		updated_by = excluded.updated_by`, tag, label, s.stamp(), by); err != nil {
		return "", fmt.Errorf("keep tag label: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit tag label: %w", err)
	}
	return label, nil
}

// ClearTagLabel removes a tag's label, so it shows the derived one again. It
// is not an error when the tag had none.
func (s *Store) ClearTagLabel(ctx context.Context, tag string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM tag_labels WHERE tag = ?", tag); err != nil {
		return fmt.Errorf("clear tag label: %w", err)
	}
	return nil
}
