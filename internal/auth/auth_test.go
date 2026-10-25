package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/weburz/burzblog/internal/store"
)

func setup(t *testing.T) (*Service, *store.Store, *store.User) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser(ctx, "jarmos", hash)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(s)
	if err != nil {
		t.Fatal(err)
	}
	return a, s, u
}

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("pässwörd long enough")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash format = %q", h)
	}
	if ok, stale := verifyPassword(h, "pässwörd long enough"); !ok || stale {
		t.Errorf("verify(correct) = %v, %v", ok, stale)
	}
	if ok, _ := verifyPassword(h, "wrong"); ok {
		t.Error("wrong password verified")
	}
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$a$b", "$argon2id$v=19$m=99999999,t=1,p=1$YQ$YQ"} {
		if ok, _ := verifyPassword(bad, "x"); ok {
			t.Errorf("malformed hash %q verified", bad)
		}
	}
}

func TestCheckPassword(t *testing.T) {
	if err := CheckPassword("short"); err == nil {
		t.Error("short password accepted")
	}
	if err := CheckPassword(strings.Repeat("x", MaxPassword+1)); err == nil {
		t.Error("huge password accepted")
	}
	if err := CheckPassword("twelve chars"); err != nil {
		t.Errorf("valid password rejected: %v", err)
	}
}

func TestAuthenticate(t *testing.T) {
	a, _, _ := setup(t)
	ctx := context.Background()
	if u, err := a.Authenticate(ctx, " JARMOS ", "correct horse battery"); err != nil || u.Username != "jarmos" {
		t.Fatalf("valid sign-in: %v %v", u, err)
	}
	if _, err := a.Authenticate(ctx, "jarmos", "nope"); !errors.Is(err, ErrCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := a.Authenticate(ctx, "ghost", "whatever"); !errors.Is(err, ErrCredentials) {
		t.Errorf("unknown user: %v", err)
	}
}

func TestAuthenticateRateLimit(t *testing.T) {
	a, _, _ := setup(t)
	ctx := context.Background()
	for i := 0; i < userLimit; i++ {
		a.Authenticate(ctx, "jarmos", "wrong")
	}
	if _, err := a.Authenticate(ctx, "jarmos", "correct horse battery"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("after %d failures: %v, want ErrRateLimited", userLimit, err)
	}
}

func TestSessions(t *testing.T) {
	a, _, u := setup(t)
	ctx := context.Background()
	token, sess, err := a.NewSession(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	got, user, err := a.Session(ctx, token)
	if err != nil || user.ID != u.ID || got.CSRF != sess.CSRF {
		t.Fatalf("session lookup: %v", err)
	}
	if !CheckCSRF(got, sess.CSRF) || CheckCSRF(got, "") || CheckCSRF(got, "x") {
		t.Error("CSRF check wrong")
	}
	if err := a.EndSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Session(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Errorf("ended session accepted: %v", err)
	}
}

func TestTokens(t *testing.T) {
	a, _, u := setup(t)
	ctx := context.Background()
	secret, tok, err := a.NewToken(ctx, u, "laptop", store.ScopeWrite)
	if err != nil || !strings.HasPrefix(secret, TokenPrefix) || tok.Username != "jarmos" {
		t.Fatalf("new token: %v %v", tok, err)
	}
	if got, err := a.Token(ctx, secret); err != nil || got.ID != tok.ID {
		t.Errorf("token lookup: %v %v", got, err)
	}
	if _, err := a.Token(ctx, "bzb_forged"); !errors.Is(err, ErrNoSession) {
		t.Errorf("forged token: %v", err)
	}
	if _, _, err := a.NewToken(ctx, u, "", store.ScopeWrite); err == nil {
		t.Error("empty name accepted")
	}
	for _, name := range []string{"a\x1b]0;evil\x07", "a\tb", "a\nb", "a\x00b", "a\u009bb"} {
		if _, _, err := a.NewToken(ctx, u, name, store.ScopeWrite); err == nil {
			t.Errorf("token name %q accepted", name)
		}
	}
	if _, _, err := a.NewToken(ctx, u, "Laptop 💻", store.ScopeWrite); err != nil {
		t.Errorf("unicode token name rejected: %v", err)
	}
	if _, _, err := a.NewToken(ctx, u, "x", "admin"); err == nil {
		t.Error("unknown scope accepted")
	}
}

func TestChangePassword(t *testing.T) {
	a, _, u := setup(t)
	ctx := context.Background()
	if err := a.ChangePassword(ctx, u, "wrong", "a brand new password", ""); !errors.Is(err, ErrCredentials) {
		t.Errorf("wrong current password: %v", err)
	}
	if err := a.ChangePassword(ctx, u, "correct horse battery", "short", ""); err == nil {
		t.Error("weak new password accepted")
	}
	if err := a.ChangePassword(ctx, u, "correct horse battery", "a brand new password", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, "jarmos", "a brand new password"); err != nil {
		t.Errorf("new password rejected: %v", err)
	}
}

func TestAuthenticateRateLimitHoldsUnderConcurrency(t *testing.T) {
	a, _, _ := setup(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	checked := 0
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.Authenticate(ctx, "jarmos", "wrong"); errors.Is(err, ErrCredentials) {
				mu.Lock()
				checked++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if checked > userLimit {
		t.Errorf("%d parallel guesses were checked, want at most %d", checked, userLimit)
	}
}

func TestOtherAccountsUnaffectedByOneLockedAccount(t *testing.T) {
	a, s, _ := setup(t)
	ctx := context.Background()
	hash, _ := HashPassword("another long password")
	s.CreateUser(ctx, "sagar", hash)
	for i := 0; i < userLimit+5; i++ {
		a.Authenticate(ctx, "jarmos", "wrong")
	}
	if _, err := a.Authenticate(ctx, "sagar", "another long password"); err != nil {
		t.Errorf("another account was locked out: %v", err)
	}
}
