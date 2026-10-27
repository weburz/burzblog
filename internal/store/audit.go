package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// Limits on the free text of an audit entry, so one cannot grow without bound.
const (
	maxAuditName   = 64
	maxAuditTarget = 200
	maxAuditRemote = 64
)

// Audit is one thing somebody did, to be recorded with AddAudit. Never put a
// secret (password, token, deploy hook URL, TOTP secret, recovery code) in it.
type Audit struct {
	// ActorUserID is 0 when no account did it (the host's command line, the
	// scheduler, a sign-in with an unknown name).
	ActorUserID int64
	// ActorName is the account's username, or a label such as "host".
	ActorName string
	// TokenID is the API token the request came with, or 0 for a browser.
	TokenID int64
	// Action is a dotted name such as "post.publish".
	Action string
	// Target is what it was done to, such as a post slug.
	Target string
	// Remote is the address the request came from.
	Remote string
}

// AuditEntry is a stored audit row.
type AuditEntry struct {
	ID int64
	At time.Time
	Audit
}

// AddAudit appends an entry to the audit log. There is deliberately no way to
// change or remove one: the table refuses it.
func (s *Store) AddAudit(ctx context.Context, a Audit) error {
	if a.Action == "" || a.ActorName == "" {
		return errors.New("audit entry needs an action and an actor")
	}
	var user, token sql.NullInt64
	if a.ActorUserID != 0 {
		user = sql.NullInt64{Int64: a.ActorUserID, Valid: true}
	}
	if a.TokenID != 0 {
		token = sql.NullInt64{Int64: a.TokenID, Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log (at, actor_user_id, actor_name, token_id, action, target, remote)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, s.stamp(), user, clip(a.ActorName, maxAuditName), token, a.Action,
		clip(a.Target, maxAuditTarget), clip(a.Remote, maxAuditRemote))
	if err != nil {
		return fmt.Errorf("record audit entry: %w", err)
	}
	return nil
}

// clip shortens s to at most n bytes without splitting a character.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// AuditLog returns one page of the audit log, newest first, and the total
// number of entries.
func (s *Store) AuditLog(ctx context.Context, limit, offset int) ([]AuditEntry, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM audit_log").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit entries: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, at, actor_user_id, actor_name, token_id, action, target, remote
		FROM audit_log ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()
	list := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var at string
		var user, token sql.NullInt64
		if err := rows.Scan(&e.ID, &at, &user, &e.ActorName, &token, &e.Action, &e.Target, &e.Remote); err != nil {
			return nil, 0, fmt.Errorf("list audit entries: %w", err)
		}
		e.At, e.ActorUserID, e.TokenID = parseStamp(at), user.Int64, token.Int64
		list = append(list, e)
	}
	return list, total, rows.Err()
}

// LastAudit returns the newest entry recording action. It returns ErrNotFound
// when there is none.
func (s *Store) LastAudit(ctx context.Context, action string) (*AuditEntry, error) {
	var e AuditEntry
	var at string
	var user, token sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, at, actor_user_id, actor_name, token_id, action, target, remote
		FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT 1`, action).
		Scan(&e.ID, &at, &user, &e.ActorName, &token, &e.Action, &e.Target, &e.Remote)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read audit log: %w", err)
	}
	e.At, e.ActorUserID, e.TokenID = parseStamp(at), user.Int64, token.Int64
	return &e, nil
}
