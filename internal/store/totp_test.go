package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationAddsTwoFactorToExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), FileName)
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range migrations[:4] {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"PRAGMA user_version = 4",
		"INSERT INTO users (username, password_hash, created_at) VALUES ('ada', 'x', '2026-01-01T00:00:00Z')",
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	// Backfill steps are keyed by migration number; running 1-4 by hand skips
	// them, which is fine for a database with no posts.
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatalf("open an existing database: %v", err)
	}
	defer s.Close()
	u, err := s.UserByName(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TOTP(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("existing user has two-factor state: %v", err)
	}
	if err := s.StartTOTP(ctx, u.ID, "SECRET"); err != nil {
		t.Errorf("start on a migrated database: %v", err)
	}
}

func TestTOTPState(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	u, _ := s.CreateUser(ctx, "ada", "x")
	if err := s.StartTOTP(ctx, u.ID, "ONE"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartTOTP(ctx, u.ID, "TWO"); err != nil { // a pending secret is replaced
		t.Fatal(err)
	}
	if t0, _ := s.TOTP(ctx, u.ID); t0.Secret != "TWO" || t0.Enabled {
		t.Errorf("pending = %+v", t0)
	}
	if ok, _ := s.AdvanceTOTPStep(ctx, u.ID, 5); ok {
		t.Error("a pending secret accepted a step")
	}
	if err := s.EnableTOTP(ctx, u.ID, 10, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartTOTP(ctx, u.ID, "THREE"); !errors.Is(err, ErrTaken) {
		t.Errorf("start while enabled = %v, want ErrTaken", err)
	}
	if err := s.EnableTOTP(ctx, u.ID, 11, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("enable twice = %v, want ErrNotFound", err)
	}
	if ok, _ := s.AdvanceTOTPStep(ctx, u.ID, 10); ok {
		t.Error("the enrolment step was accepted again")
	}
	if ok, _ := s.AdvanceTOTPStep(ctx, u.ID, 11); !ok {
		t.Error("a newer step was refused")
	}
	if ok, _ := s.AdvanceTOTPStep(ctx, u.ID, 11); ok {
		t.Error("a step was accepted twice")
	}
	if ok, _ := s.UseRecoveryCode(ctx, u.ID, "a"); !ok {
		t.Error("recovery code refused")
	}
	if ok, _ := s.UseRecoveryCode(ctx, u.ID, "a"); ok {
		t.Error("recovery code accepted twice")
	}
	if n, _ := s.RecoveryCodesLeft(ctx, u.ID); n != 1 {
		t.Errorf("recovery codes left = %d, want 1", n)
	}
	if err := s.DisableTOTP(ctx, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableTOTP(ctx, u.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("disable twice = %v, want ErrNotFound", err)
	}
	if n, _ := s.RecoveryCodesLeft(ctx, u.ID); n != 0 {
		t.Errorf("recovery codes after disable = %d", n)
	}
}

func TestChallengeLifetime(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Unix(1_700_000_000, 0)
	s.SetClock(func() time.Time { return now })
	u, _ := s.CreateUser(ctx, "ada", "x")
	if err := s.CreateChallenge(ctx, u.ID, "h1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ChallengeUser(ctx, "h1"); err != nil || got.ID != u.ID {
		t.Fatalf("live challenge = %v, %v", got, err)
	}
	// A newer challenge replaces the older one.
	if err := s.CreateChallenge(ctx, u.ID, "h2", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChallengeUser(ctx, "h1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("replaced challenge = %v", err)
	}
	if ok, _ := s.ConsumeChallenge(ctx, "h2"); !ok {
		t.Error("live challenge not consumed")
	}
	if ok, _ := s.ConsumeChallenge(ctx, "h2"); ok {
		t.Error("challenge consumed twice")
	}
	s.CreateChallenge(ctx, u.ID, "h3", now.Add(time.Minute))
	now = now.Add(2 * time.Minute)
	if _, err := s.ChallengeUser(ctx, "h3"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired challenge = %v", err)
	}
	if ok, _ := s.ConsumeChallenge(ctx, "h3"); ok {
		t.Error("expired challenge consumed")
	}
}
