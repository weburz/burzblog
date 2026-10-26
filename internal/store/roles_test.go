package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationGivesExistingAccountsTheAdminRole(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), FileName)
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	// A database as the TOTP release left it (schema 7), with content in it.
	for _, q := range migrations[:7] {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"PRAGMA user_version = 7",
		"INSERT INTO users (id, username, password_hash, created_at) VALUES (1, 'ada', 'x', '2026-01-01T00:00:00Z'), (2, 'grace', 'y', '2026-01-02T00:00:00Z')",
		"INSERT INTO tokens (token_hash, user_id, name, scope, created_at) VALUES ('h', 2, 'laptop', 'write', '2026-01-03T00:00:00Z')",
		"INSERT INTO posts (slug, title, status, created_at, updated_at) VALUES ('hello', 'Hello', 'draft', '2026-01-04T00:00:00Z', '2026-01-04T00:00:00Z')",
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()

	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatalf("open a schema 7 database: %v", err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) {
		t.Errorf("schema version = %d, %v; want %d", version, err, len(migrations))
	}
	users, err := s.Users(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("users = %v, %v", users, err)
	}
	for _, u := range users {
		if u.Role != RoleAdmin {
			t.Errorf("%s has role %q after the migration; want admin", u.Username, u.Role)
		}
	}
	if tokens, _ := s.Tokens(ctx); len(tokens) != 1 || tokens[0].Name != "laptop" {
		t.Errorf("tokens after migration = %v", tokens)
	}
	if _, err := s.GetPost(ctx, "hello"); err != nil {
		t.Errorf("post after migration: %v", err)
	}
	// New accounts and the audit log work on the migrated database.
	if u, err := s.CreateUser(ctx, "newbie", "z", RoleAuthor); err != nil || u.Role != RoleAuthor {
		t.Errorf("create author = %v, %v", u, err)
	}
	if err := s.AddAudit(ctx, Audit{ActorName: "ada", Action: "login.success"}); err != nil {
		t.Errorf("audit on migrated database: %v", err)
	}
}

func TestCreateUserRoles(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if u, err := s.CreateUser(ctx, "ada", "x", RoleAuthor); err != nil || u.Role != RoleAuthor {
		t.Fatalf("create author = %v, %v", u, err)
	}
	if got, _ := s.UserByName(ctx, "ada"); got.Role != RoleAuthor {
		t.Errorf("stored role = %q", got.Role)
	}
	if _, err := s.CreateUser(ctx, "bob", "x", "owner"); err == nil || !strings.Contains(err.Error(), "role must be") {
		t.Errorf("unknown role error = %v", err)
	}
	first := open(t)
	if u, err := first.CreateFirstUser(ctx, "root", "x"); err != nil || u.Role != RoleAdmin {
		t.Errorf("first account = %v, %v; want admin", u, err)
	}
}

func TestSetRole(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ada, _ := s.CreateUser(ctx, "ada", "x", RoleAdmin)
	grace, _ := s.CreateUser(ctx, "grace", "x", RoleAuthor)

	if _, err := s.SetRole(ctx, ada.ID, RoleAuthor); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote the only admin = %v; want ErrLastAdmin", err)
	}
	if got, _ := s.UserByID(ctx, ada.ID); got.Role != RoleAdmin {
		t.Errorf("last admin demoted anyway: %q", got.Role)
	}
	prev, err := s.SetRole(ctx, grace.ID, RoleAdmin)
	if err != nil || prev != RoleAuthor {
		t.Fatalf("promote = %q, %v", prev, err)
	}
	if prev, err := s.SetRole(ctx, ada.ID, RoleAuthor); err != nil || prev != RoleAdmin {
		t.Errorf("demote with another admin present = %q, %v", prev, err)
	}
	if _, err := s.SetRole(ctx, grace.ID, RoleAuthor); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote the remaining admin = %v; want ErrLastAdmin", err)
	}
	if prev, err := s.SetRole(ctx, grace.ID, RoleAdmin); err != nil || prev != RoleAdmin {
		t.Errorf("set the same role = %q, %v", prev, err)
	}
	if _, err := s.SetRole(ctx, grace.ID, "owner"); err == nil {
		t.Error("an unknown role was accepted")
	}
	if _, err := s.SetRole(ctx, 999, RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user = %v; want ErrNotFound", err)
	}
}

func TestUserTokensListsOnlyTheOwners(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ada, _ := s.CreateUser(ctx, "ada", "x", RoleAdmin)
	grace, _ := s.CreateUser(ctx, "grace", "x", RoleAuthor)
	own, _ := s.CreateToken(ctx, grace.ID, "h1", "graces", ScopeWrite)
	s.CreateToken(ctx, ada.ID, "h2", "adas", ScopeWrite)

	mine, err := s.UserTokens(ctx, grace.ID)
	if err != nil || len(mine) != 1 || mine[0].ID != own.ID {
		t.Errorf("UserTokens = %v, %v", mine, err)
	}
	if all, _ := s.Tokens(ctx); len(all) != 2 {
		t.Errorf("Tokens = %d, want 2", len(all))
	}
	if got, err := s.TokenByID(ctx, own.ID); err != nil || got.Username != "grace" || got.LastUsedAt != nil {
		t.Errorf("TokenByID = %v, %v", got, err)
	}
	if _, err := s.TokenByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing token = %v", err)
	}
}

func TestAuditLogIsAppendOnly(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	for _, a := range []Audit{
		{ActorUserID: 1, ActorName: "ada", Action: "login.success", Remote: "203.0.113.5"},
		{ActorUserID: 1, ActorName: "ada", TokenID: 4, Action: "post.publish", Target: "hello", Remote: "203.0.113.5"},
		{ActorName: "host", Action: "role.change", Target: "grace: author to admin", Remote: "local"},
	} {
		if err := s.AddAudit(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	entries, total, err := s.AuditLog(ctx, 2, 0)
	if err != nil || total != 3 || len(entries) != 2 {
		t.Fatalf("AuditLog = %d entries of %d, %v", len(entries), total, err)
	}
	if entries[0].Action != "role.change" || entries[0].ActorUserID != 0 || entries[1].TokenID != 4 || entries[1].Target != "hello" {
		t.Errorf("entries = %+v", entries)
	}
	if older, _, _ := s.AuditLog(ctx, 2, 2); len(older) != 1 || older[0].Action != "login.success" {
		t.Errorf("second page = %+v", older)
	}

	if _, err := s.db.ExecContext(ctx, "UPDATE audit_log SET action = 'nothing'"); err == nil {
		t.Error("an audit row was updated")
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM audit_log"); err == nil {
		t.Error("audit rows were deleted")
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM audit_log WHERE id = 1"); err == nil {
		t.Error("an audit row was deleted")
	}
	if _, total, _ := s.AuditLog(ctx, 10, 0); total != 3 {
		t.Errorf("total after tampering attempts = %d", total)
	}
}

func TestAddAuditValidatesAndClips(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.AddAudit(ctx, Audit{ActorName: "ada"}); err == nil {
		t.Error("an entry without an action was accepted")
	}
	if err := s.AddAudit(ctx, Audit{Action: "x"}); err == nil {
		t.Error("an entry without an actor was accepted")
	}
	long := strings.Repeat("é", 300)
	if err := s.AddAudit(ctx, Audit{ActorName: "ada", Action: "post.delete", Target: long}); err != nil {
		t.Fatal(err)
	}
	entries, _, _ := s.AuditLog(ctx, 1, 0)
	if got := entries[0].Target; len(got) > maxAuditTarget || strings.ContainsRune(got, '�') {
		t.Errorf("target = %d bytes %q", len(got), got)
	}
}
