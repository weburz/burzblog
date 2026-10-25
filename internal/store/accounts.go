package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User is an account that can sign in. Every user can do everything.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// CreateUser adds an account.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (*User, error) {
	u := &User{Username: username, PasswordHash: passwordHash, CreatedAt: s.now()}
	err := s.db.QueryRowContext(ctx, `INSERT INTO users (username, password_hash, created_at)
		VALUES (?, ?, ?) ON CONFLICT (username) DO NOTHING RETURNING id`,
		username, passwordHash, s.stamp()).Scan(&u.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// UserByName finds an account, ignoring case.
func (s *Store) UserByName(ctx context.Context, username string) (*User, error) {
	return s.user(ctx, "username = ?", username)
}

// UserByID finds an account by id.
func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return s.user(ctx, "id = ?", id)
}

func (s *Store) user(ctx context.Context, where string, arg any) (*User, error) {
	var u User
	var created string
	err := s.db.QueryRowContext(ctx, "SELECT id, username, password_hash, created_at FROM users WHERE "+where, arg).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read user: %w", err)
	}
	u.CreatedAt = parseStamp(created)
	return &u, nil
}

// Users lists every account.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, username, created_at FROM users ORDER BY username")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var list []User
	for rows.Next() {
		var u User
		var created string
		if err := rows.Scan(&u.ID, &u.Username, &created); err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		u.CreatedAt = parseStamp(created)
		list = append(list, u)
	}
	return list, rows.Err()
}

// SetPassword replaces a user's password hash, signs them out everywhere
// except, optionally, the session keepSession, and revokes their write
// tokens (CLI logins). Export tokens stay: they cannot change anything, and
// revoking them would silently break the website build.
func (s *Store) SetPassword(ctx context.Context, userID int64, passwordHash, keepSession string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password change: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, userID)
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND token_hash != ?", userID, keepSession); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM tokens WHERE user_id = ? AND scope = ?", userID, ScopeWrite); err != nil {
		return fmt.Errorf("revoke write tokens: %w", err)
	}
	return tx.Commit()
}

// RehashPassword stores a stronger hash for the same password.
func (s *Store) RehashPassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, userID)
	return err
}

// Session is a signed-in browser.
type Session struct {
	TokenHash string
	UserID    int64
	CSRF      string
	ExpiresAt time.Time
}

// maxSessions bounds how many browsers one account keeps signed in.
const maxSessions = 20

// CreateSession records a session and drops the user's oldest beyond the cap.
func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session: %w", err)
	}
	defer tx.Rollback()
	now := s.now().Unix()
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now); err != nil {
		return fmt.Errorf("drop expired sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, csrf, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`, sess.TokenHash, sess.UserID, sess.CSRF, now, sess.ExpiresAt.Unix()); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash NOT IN
		(SELECT token_hash FROM sessions WHERE user_id = ? ORDER BY expires_at DESC LIMIT ?)`,
		sess.UserID, sess.UserID, maxSessions); err != nil {
		return fmt.Errorf("cap sessions: %w", err)
	}
	return tx.Commit()
}

// SessionByHash finds a live session.
func (s *Store) SessionByHash(ctx context.Context, tokenHash string) (*Session, error) {
	var sess Session
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT token_hash, user_id, csrf, expires_at FROM sessions
		WHERE token_hash = ? AND expires_at > ?`, tokenHash, s.now().Unix()).
		Scan(&sess.TokenHash, &sess.UserID, &sess.CSRF, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}
	sess.ExpiresAt = time.Unix(expires, 0).UTC()
	return &sess, nil
}

// DeleteSession signs a browser out.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

// Token scopes. Write tokens can do everything a signed-in user can; export
// tokens can only read published posts, for reader builds.
const (
	ScopeWrite  = "write"
	ScopeExport = "export"
)

// Token is an API credential for the CLI or a reader build.
type Token struct {
	ID         int64
	UserID     int64
	Username   string
	Name       string
	Scope      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreateToken stores a token digest.
func (s *Store) CreateToken(ctx context.Context, userID int64, tokenHash, name, scope string) (*Token, error) {
	t := &Token{UserID: userID, Name: name, Scope: scope, CreatedAt: s.now()}
	err := s.db.QueryRowContext(ctx, `INSERT INTO tokens (token_hash, user_id, name, scope, created_at)
		VALUES (?, ?, ?, ?, ?) RETURNING id`, tokenHash, userID, name, scope, s.stamp()).Scan(&t.ID)
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	return t, nil
}

// TokenByHash finds a token and records that it was used.
func (s *Store) TokenByHash(ctx context.Context, tokenHash string) (*Token, error) {
	var t Token
	var created string
	var lastUsed sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT t.id, t.user_id, u.username, t.name, t.scope, t.created_at, t.last_used_at
		FROM tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ?`, tokenHash).
		Scan(&t.ID, &t.UserID, &t.Username, &t.Name, &t.Scope, &created, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read token: %w", err)
	}
	t.CreatedAt = parseStamp(created)
	t.LastUsedAt = timePtr(lastUsed)
	// Coarse last-used tracking: at most one write per token per minute.
	if t.LastUsedAt == nil || s.now().Sub(*t.LastUsedAt) > time.Minute {
		if _, err := s.db.ExecContext(ctx, "UPDATE tokens SET last_used_at = ? WHERE id = ?", s.stamp(), t.ID); err != nil {
			return nil, fmt.Errorf("record token use: %w", err)
		}
	}
	return &t, nil
}

// Tokens lists every API token with its owner.
func (s *Store) Tokens(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.user_id, u.username, t.name, t.scope, t.created_at, t.last_used_at
		FROM tokens t JOIN users u ON u.id = t.user_id ORDER BY t.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()
	list := []Token{}
	for rows.Next() {
		var t Token
		var created string
		var lastUsed sql.NullString
		if err := rows.Scan(&t.ID, &t.UserID, &t.Username, &t.Name, &t.Scope, &created, &lastUsed); err != nil {
			return nil, fmt.Errorf("list tokens: %w", err)
		}
		t.CreatedAt = parseStamp(created)
		t.LastUsedAt = timePtr(lastUsed)
		list = append(list, t)
	}
	return list, rows.Err()
}

// DeleteToken revokes a token.
func (s *Store) DeleteToken(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM tokens WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// pruneBatch is how many expired login_attempts rows one reservation removes.
const pruneBatch = 20

// ReserveLoginAttempt counts an attempt for key before the password is
// checked, atomically, and reports whether the key had attempts left in the
// current window. Concurrent requests cannot all slip under the limit.
// A successful sign-in clears the count with ClearLoginFailures.
func (s *Store) ReserveLoginAttempt(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin login attempt: %w", err)
	}
	defer tx.Rollback()
	now := s.now().Unix()
	// Expired rows count as absent, so drop a few on every attempt; the table
	// then stays about as big as the sign-ins of one window.
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_attempts WHERE key IN
		(SELECT key FROM login_attempts WHERE window_start <= ? LIMIT ?)`,
		now-int64(window.Seconds()), pruneBatch); err != nil {
		return false, fmt.Errorf("prune login attempts: %w", err)
	}
	var start int64
	var failures int
	err = tx.QueryRowContext(ctx, "SELECT window_start, failures FROM login_attempts WHERE key = ?", key).
		Scan(&start, &failures)
	switch {
	case errors.Is(err, sql.ErrNoRows), err == nil && now-start >= int64(window.Seconds()):
		start, failures = now, 0
	case err != nil:
		return false, fmt.Errorf("read login attempts: %w", err)
	}
	if failures >= limit {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO login_attempts (key, window_start, failures) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET window_start = excluded.window_start, failures = excluded.failures`,
		key, start, failures+1); err != nil {
		return false, fmt.Errorf("record login attempt: %w", err)
	}
	return true, tx.Commit()
}

// ClearLoginFailures forgets failures for key after a successful sign-in.
func (s *Store) ClearLoginFailures(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM login_attempts WHERE key = ?", key)
	return err
}

// NormalizeUsername is how usernames are compared and stored.
func NormalizeUsername(username string) string {
	return strings.TrimSpace(username)
}
