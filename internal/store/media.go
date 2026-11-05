package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Media is one uploaded file. The bytes are not in the database: FileName
// names them in the media storage.
type Media struct {
	ID           int64
	FileName     string
	OriginalName string
	MIME         string
	Size         int64
	SHA256       string
	// Width and Height are 0 when the format is not decoded.
	Width, Height int
	Alt           string
	UploadedBy    string
	CreatedAt     time.Time
	// UsedIn is how many posts mention the file. Only FindMedia fills it.
	UsedIn int
}

const mediaColumns = "id, file_name, original_name, mime, size, sha256, width, height, alt, uploaded_by, created_at"

type rowScanner interface{ Scan(dest ...any) error }

func scanMedia(row rowScanner) (*Media, error) {
	var m Media
	var created string
	if err := row.Scan(&m.ID, &m.FileName, &m.OriginalName, &m.MIME, &m.Size, &m.SHA256,
		&m.Width, &m.Height, &m.Alt, &m.UploadedBy, &created); err != nil {
		return nil, err
	}
	m.CreatedAt = parseStamp(created)
	return &m, nil
}

// AddMedia records an upload. When a file with the same SHA256 is already
// recorded, that row is returned unchanged and created is false.
func (s *Store) AddMedia(ctx context.Context, m Media) (media *Media, created bool, err error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO media (`+mediaColumns[len("id, "):]+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		m.FileName, m.OriginalName, m.MIME, m.Size, m.SHA256, m.Width, m.Height, m.Alt, m.UploadedBy, s.stamp())
	if err != nil {
		return nil, false, fmt.Errorf("record upload: %w", err)
	}
	n, _ := res.RowsAffected()
	media, err = scanMedia(s.db.QueryRowContext(ctx, "SELECT "+mediaColumns+" FROM media WHERE sha256 = ?", m.SHA256))
	if err != nil {
		return nil, false, fmt.Errorf("read upload: %w", err)
	}
	return media, n == 1, nil
}

// GetMedia returns one upload. It returns ErrNotFound when there is none.
func (s *Store) GetMedia(ctx context.Context, id int64) (*Media, error) {
	m, err := scanMedia(s.db.QueryRowContext(ctx, "SELECT "+mediaColumns+" FROM media WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	return m, nil
}

// ListMedia lists uploads, newest first, and how many there are in all. A
// zero limit lists every upload.
func (s *Store) ListMedia(ctx context.Context, limit, offset int) ([]Media, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM media").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count uploads: %w", err)
	}
	if limit == 0 {
		limit = -1
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+mediaColumns+` FROM media
		ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list uploads: %w", err)
	}
	defer rows.Close()
	list := []Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("list uploads: %w", err)
		}
		list = append(list, *m)
	}
	return list, total, rows.Err()
}

// SetMediaAlt replaces an upload's alt text and returns the updated record. It
// returns ErrNotFound when there is none.
func (s *Store) SetMediaAlt(ctx context.Context, id int64, alt string) (*Media, error) {
	m, err := scanMedia(s.db.QueryRowContext(ctx, "UPDATE media SET alt = ? WHERE id = ? RETURNING "+mediaColumns, alt, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update upload: %w", err)
	}
	return m, nil
}

// MediaFilter narrows FindMedia. A zero field matches everything.
type MediaFilter struct {
	// Query matches, ignoring ASCII case, the original file name, stored file
	// name or alt text.
	Query string
	// Type is "image" or "pdf".
	Type string
}

// mediaUsedIn counts the posts whose body or cover mention the media row m.
const mediaUsedIn = `(SELECT count(*) FROM posts p
	WHERE instr(p.body, m.file_name) > 0 OR instr(p.cover_url, m.file_name) > 0)`

// FindMedia lists the uploads that match f, newest first, with how many posts
// use each (UsedIn) and how many match in all. A zero limit lists every match.
func (s *Store) FindMedia(ctx context.Context, f MediaFilter, limit, offset int) ([]Media, int, error) {
	where, args := "1 = 1", []any{}
	if f.Query != "" {
		where += ` AND (instr(lower(m.original_name), lower(?1)) > 0 OR instr(lower(m.file_name), lower(?1)) > 0
			OR instr(lower(m.alt), lower(?1)) > 0)`
		args = append(args, f.Query)
	}
	switch f.Type {
	case "image":
		where += " AND m.mime LIKE 'image/%'"
	case "pdf":
		where += " AND m.mime = 'application/pdf'"
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM media m WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count uploads: %w", err)
	}
	if limit == 0 {
		limit = -1
	}
	cols := "m." + strings.ReplaceAll(mediaColumns, ", ", ", m.")
	rows, err := s.db.QueryContext(ctx, "SELECT "+cols+", "+mediaUsedIn+" FROM media m WHERE "+where+
		" ORDER BY m.created_at DESC, m.id DESC LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list uploads: %w", err)
	}
	defer rows.Close()
	list := []Media{}
	for rows.Next() {
		var m Media
		var created string
		if err := rows.Scan(&m.ID, &m.FileName, &m.OriginalName, &m.MIME, &m.Size, &m.SHA256,
			&m.Width, &m.Height, &m.Alt, &m.UploadedBy, &created, &m.UsedIn); err != nil {
			return nil, 0, fmt.Errorf("list uploads: %w", err)
		}
		m.CreatedAt = parseStamp(created)
		list = append(list, m)
	}
	return list, total, rows.Err()
}

// MediaUseCount is how many posts, whatever their status, mention fileName in
// their body or cover.
func (s *Store) MediaUseCount(ctx context.Context, fileName string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM posts
		WHERE instr(body, ?1) > 0 OR instr(cover_url, ?1) > 0`, fileName).Scan(&n); err != nil {
		return 0, fmt.Errorf("count uploads in use: %w", err)
	}
	return n, nil
}

// DeleteMedia removes an upload's row and returns it, so the caller can
// remove the bytes. It returns ErrNotFound when there is none.
func (s *Store) DeleteMedia(ctx context.Context, id int64) (*Media, error) {
	m, err := scanMedia(s.db.QueryRowContext(ctx, "DELETE FROM media WHERE id = ? RETURNING "+mediaColumns, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("delete upload: %w", err)
	}
	return m, nil
}

// MediaUsers lists the slugs of the posts, whatever their status, whose body
// or cover mentions fileName, up to limit. Revisions are not searched.
func (s *Store) MediaUsers(ctx context.Context, fileName string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT slug FROM posts
		WHERE instr(body, ?1) > 0 OR instr(cover_url, ?1) > 0 ORDER BY slug LIMIT ?2`, fileName, limit)
	if err != nil {
		return nil, fmt.Errorf("find uploads in use: %w", err)
	}
	defer rows.Close()
	slugs := []string{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("find uploads in use: %w", err)
		}
		slugs = append(slugs, slug)
	}
	return slugs, rows.Err()
}

// MediaSize is the pixel size of an uploaded image.
type MediaSize struct{ Width, Height int }

// maxSizeNames is how many file names one MediaSizes query asks for, well
// under SQLite's limit on bound variables.
const maxSizeNames = 500

// MediaSizes returns the pixel size of the uploads named in fileNames, by file
// name. Names that are not uploads, and uploads whose format is not decoded
// (width 0), are left out.
func (s *Store) MediaSizes(ctx context.Context, fileNames []string) (map[string]MediaSize, error) {
	sizes := map[string]MediaSize{}
	for start := 0; start < len(fileNames); start += maxSizeNames {
		chunk := fileNames[start:min(start+maxSizeNames, len(fileNames))]
		args := make([]any, len(chunk))
		for i, name := range chunk {
			args[i] = name
		}
		rows, err := s.db.QueryContext(ctx, `SELECT file_name, width, height FROM media
			WHERE width > 0 AND height > 0 AND file_name IN (?`+strings.Repeat(",?", len(chunk)-1)+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("read upload sizes: %w", err)
		}
		for rows.Next() {
			var name string
			var size MediaSize
			if err := rows.Scan(&name, &size.Width, &size.Height); err != nil {
				rows.Close()
				return nil, fmt.Errorf("read upload sizes: %w", err)
			}
			sizes[name] = size
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("read upload sizes: %w", err)
		}
	}
	return sizes, nil
}
