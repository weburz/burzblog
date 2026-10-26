package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// TOTP is a user's authenticator-app secret. It is only a second factor once
// Enabled; before that it is a pending enrolment.
type TOTP struct {
	UserID   int64
	Secret   string
	Enabled  bool
	LastStep int64
}

// TOTP reads a user's second-factor state, or ErrNotFound if they never
// started enrolling.
func (s *Store) TOTP(ctx context.Context, userID int64) (*TOTP, error) {
	var t TOTP
	err := s.db.QueryRowContext(ctx, "SELECT user_id, secret, enabled, last_step FROM user_totp WHERE user_id = ?", userID).
		Scan(&t.UserID, &t.Secret, &t.Enabled, &t.LastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read two-factor state: %w", err)
	}
	return &t, nil
}

// StartTOTP stores a pending secret, replacing an earlier pending one. It
// returns ErrTaken if two-factor sign-in is already on.
func (s *Store) StartTOTP(ctx context.Context, userID int64, secret string) error {
	res, err := s.db.ExecContext(ctx, `INSERT INTO user_totp (user_id, secret, created_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET secret = excluded.secret, created_at = excluded.created_at, last_step = 0
		WHERE enabled = 0`, userID, secret, s.stamp())
	if err != nil {
		return fmt.Errorf("start two-factor enrolment: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTaken
	}
	return nil
}

// EnableTOTP turns a pending enrolment on after its first code (matching
// step) was confirmed, and stores the digests of the recovery codes.
func (s *Store) EnableTOTP(ctx context.Context, userID, step int64, recoveryHashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin two-factor enable: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE user_totp SET enabled = 1, last_step = ? WHERE user_id = ? AND enabled = 0", step, userID)
	if err != nil {
		return fmt.Errorf("enable two-factor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM recovery_codes WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("clear recovery codes: %w", err)
	}
	for _, h := range recoveryHashes {
		if _, err := tx.ExecContext(ctx, "INSERT INTO recovery_codes (code_hash, user_id) VALUES (?, ?)", h, userID); err != nil {
			return fmt.Errorf("store recovery code: %w", err)
		}
	}
	return tx.Commit()
}

// AdvanceTOTPStep records step as used and reports whether it was newer than
// every step used before, so each code works once.
func (s *Store) AdvanceTOTPStep(ctx context.Context, userID, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE user_totp SET last_step = ? WHERE user_id = ? AND enabled = 1 AND last_step < ?", step, userID, step)
	if err != nil {
		return false, fmt.Errorf("record two-factor code: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// UseRecoveryCode deletes a recovery code and reports whether it existed.
func (s *Store) UseRecoveryCode(ctx context.Context, userID int64, hash string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM recovery_codes WHERE user_id = ? AND code_hash = ?", userID, hash)
	if err != nil {
		return false, fmt.Errorf("use recovery code: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// RecoveryCodesLeft counts a user's unused recovery codes.
func (s *Store) RecoveryCodesLeft(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM recovery_codes WHERE user_id = ?", userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count recovery codes: %w", err)
	}
	return n, nil
}

// DisableTOTP removes a user's second factor, recovery codes and pending
// sign-ins, and signs them out everywhere except, optionally, the session
// keepSession. Write tokens stay: they are a credential of their own.
func (s *Store) DisableTOTP(ctx context.Context, userID int64, keepSession string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin two-factor disable: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "DELETE FROM user_totp WHERE user_id = ?", userID)
	if err != nil {
		return fmt.Errorf("disable two-factor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM recovery_codes WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("clear recovery codes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM login_challenges WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("clear sign-in challenges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND token_hash != ?", userID, keepSession); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	return tx.Commit()
}

// CreateChallenge stores the digest of a sign-in challenge, replacing the
// user's earlier ones and dropping expired ones.
func (s *Store) CreateChallenge(ctx context.Context, userID int64, tokenHash string, expires time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin challenge: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM login_challenges WHERE user_id = ? OR expires_at <= ?", userID, s.now().Unix()); err != nil {
		return fmt.Errorf("drop old challenges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO login_challenges (token_hash, user_id, expires_at) VALUES (?, ?, ?)",
		tokenHash, userID, expires.Unix()); err != nil {
		return fmt.Errorf("create challenge: %w", err)
	}
	return tx.Commit()
}

// ChallengeUser finds the user a live challenge was issued to.
func (s *Store) ChallengeUser(ctx context.Context, tokenHash string) (*User, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT user_id FROM login_challenges WHERE token_hash = ? AND expires_at > ?",
		tokenHash, s.now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read challenge: %w", err)
	}
	return s.UserByID(ctx, id)
}

// ConsumeChallenge deletes a live challenge and reports whether it existed,
// so it can complete one sign-in only.
func (s *Store) ConsumeChallenge(ctx context.Context, tokenHash string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM login_challenges WHERE token_hash = ? AND expires_at > ?", tokenHash, s.now().Unix())
	if err != nil {
		return false, fmt.Errorf("use challenge: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
