package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/store"
)

const goodPassword = "correct horse battery"

// emptyService is a Service over a store with no accounts.
func emptyService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := New(s)
	if err != nil {
		t.Fatal(err)
	}
	return a, s
}

func TestSetupTokenWorksOnce(t *testing.T) {
	ctx := context.Background()
	a, s := emptyService(t)
	token, err := a.IssueSetupToken(ctx, SetupTTL)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 || strings.ContainsAny(token, "+/=") {
		t.Errorf("token %q is not 256 bits of unpadded base64url", token)
	}
	user, err := a.Setup(ctx, token, " ada ", goodPassword)
	if err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	if user.Username != "ada" {
		t.Errorf("username = %q, want ada", user.Username)
	}
	if _, err := a.Authenticate(ctx, "ada", goodPassword); err != nil {
		t.Errorf("the new account cannot sign in: %v", err)
	}
	if _, err := a.Setup(ctx, token, "grace", goodPassword); !errors.Is(err, ErrSetupDone) {
		t.Errorf("second Setup() = %v, want ErrSetupDone", err)
	}
	if users, _ := s.Users(ctx); len(users) != 1 {
		t.Errorf("%d accounts, want 1", len(users))
	}
}

func TestSetupRejectsBadTokens(t *testing.T) {
	ctx := context.Background()
	a, s := emptyService(t)
	if _, err := a.Setup(ctx, "anything", "ada", goodPassword); !errors.Is(err, ErrSetupToken) {
		t.Errorf("Setup() with no token issued = %v, want ErrSetupToken", err)
	}
	token, _ := a.IssueSetupToken(ctx, SetupTTL)
	for _, bad := range []string{"", "wrong", token + "x", token[:len(token)-1]} {
		if _, err := a.Setup(ctx, bad, "ada", goodPassword); !errors.Is(err, ErrSetupToken) {
			t.Errorf("Setup(%q) = %v, want ErrSetupToken", bad, err)
		}
	}
	if has, _ := s.HasUsers(ctx); has {
		t.Error("a wrong token created an account")
	}
	if _, err := a.Setup(ctx, token, "ada", goodPassword); err != nil {
		t.Errorf("the right token stopped working after wrong guesses: %v", err)
	}
}

func TestSetupTokenExpires(t *testing.T) {
	ctx := context.Background()
	a, s := emptyService(t)
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	token, _ := a.IssueSetupToken(ctx, SetupTTL)
	now = now.Add(SetupTTL - time.Second)
	if !a.checkSetupToken(token) {
		t.Error("token expired early")
	}
	now = now.Add(time.Second)
	if _, err := a.Setup(ctx, token, "ada", goodPassword); !errors.Is(err, ErrSetupToken) {
		t.Errorf("Setup() at expiry = %v, want ErrSetupToken", err)
	}
	if has, _ := s.HasUsers(ctx); has {
		t.Error("an expired token created an account")
	}
}

func TestSetupTokenDiesWhenAnAccountExists(t *testing.T) {
	ctx := context.Background()
	a, s := emptyService(t)
	token, _ := a.IssueSetupToken(ctx, SetupTTL)
	hash, _ := HashPassword(goodPassword)
	if _, err := s.CreateUser(ctx, "root", hash); err != nil { // like `burzblog user add`
		t.Fatal(err)
	}
	if _, err := a.Setup(ctx, token, "ada", goodPassword); !errors.Is(err, ErrSetupDone) {
		t.Errorf("Setup() after user add = %v, want ErrSetupDone", err)
	}
	if a.checkSetupToken(token) {
		t.Error("the token is still alive after an account was created")
	}
	if _, err := a.IssueSetupToken(ctx, SetupTTL); !errors.Is(err, ErrSetupDone) {
		t.Errorf("IssueSetupToken() with accounts = %v, want ErrSetupDone", err)
	}
}

func TestSetupConcurrentCreatesOneAccount(t *testing.T) {
	ctx := context.Background()
	a, s := emptyService(t)
	token, _ := a.IssueSetupToken(ctx, SetupTTL)
	var wg sync.WaitGroup
	results := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = a.Setup(ctx, token, "admin"+string(rune('a'+i)), goodPassword)
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrSetupDone), errors.Is(err, ErrBusy):
		default:
			t.Errorf("unexpected error %v", err)
		}
	}
	if users, _ := s.Users(ctx); len(users) != 1 || ok != 1 {
		t.Errorf("%d accounts and %d successes, want exactly 1 of each", len(users), ok)
	}
}

func TestSetupChecksNameAndPassword(t *testing.T) {
	ctx := context.Background()
	a, s := emptyService(t)
	token, _ := a.IssueSetupToken(ctx, SetupTTL)
	for _, c := range []struct{ name, password string }{
		{"ada", "short"},
		{"ada", strings.Repeat("é", 11)},
		{"", goodPassword},
		{"a b", goodPassword},
		{"a\x1bb", goodPassword},
		{strings.Repeat("a", 65), goodPassword},
	} {
		_, err := a.Setup(ctx, token, c.name, c.password)
		if err == nil || errors.Is(err, ErrSetupToken) {
			t.Errorf("Setup(%q, %q) = %v, want a validation error", c.name, c.password, err)
		}
	}
	if has, _ := s.HasUsers(ctx); has {
		t.Error("an invalid account was created")
	}
	if _, err := a.Setup(ctx, token, "ada", goodPassword); err != nil {
		t.Errorf("a rejected form used up the token: %v", err)
	}
}

func TestSetupGuessesAreLimited(t *testing.T) {
	ctx := WithClient(context.Background(), "203.0.113.9")
	a, _ := emptyService(t)
	token, _ := a.IssueSetupToken(ctx, SetupTTL)
	for i := 0; i < userLimit; i++ {
		if _, err := a.Setup(ctx, "guess", "ada", goodPassword); !errors.Is(err, ErrSetupToken) {
			t.Fatalf("guess %d = %v, want ErrSetupToken", i, err)
		}
	}
	if _, err := a.Setup(ctx, token, "ada", goodPassword); !errors.Is(err, ErrRateLimited) {
		t.Errorf("Setup() after too many guesses = %v, want ErrRateLimited", err)
	}
	other := WithClient(context.Background(), "198.51.100.4")
	if _, err := a.Setup(other, token, "ada", goodPassword); err != nil {
		t.Errorf("another client is blocked too: %v", err)
	}
}
