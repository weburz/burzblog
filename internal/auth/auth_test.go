package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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

// fillGate occupies every hashing slot until the test ends.
func fillGate(t *testing.T, a *Service) {
	t.Helper()
	for i := 0; i < cap(a.gate); i++ {
		a.gate <- struct{}{}
	}
	t.Cleanup(func() { emptyGate(a) })
}

// emptyGate frees the slots fillGate took, once.
func emptyGate(a *Service) {
	for i := 0; i < cap(a.gate); i++ {
		select {
		case <-a.gate:
		default:
		}
	}
}

func TestAuthenticateBusyGate(t *testing.T) {
	a, _, _ := setup(t)
	a.wait = 50 * time.Millisecond
	ctx := context.Background()
	if _, err := a.Authenticate(ctx, "jarmos", "correct horse battery"); err != nil {
		t.Fatalf("free gate: %v", err)
	}
	fillGate(t, a)
	start := time.Now()
	_, err := a.Authenticate(ctx, "jarmos", "correct horse battery")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("full gate: %v, want ErrBusy", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("busy answer took %v, want about the %v wait", took, a.wait)
	}
}

func TestBusyDoesNotSpendAttempts(t *testing.T) {
	a, _, _ := setup(t)
	a.wait = time.Millisecond
	ctx := context.Background()
	fillGate(t, a)
	for i := 0; i < userLimit+5; i++ {
		if _, err := a.Authenticate(ctx, "jarmos", "wrong"); !errors.Is(err, ErrBusy) {
			t.Fatalf("attempt %d: %v, want ErrBusy", i, err)
		}
	}
	emptyGate(a)
	if _, err := a.Authenticate(ctx, "jarmos", "correct horse battery"); err != nil {
		t.Errorf("shed requests used up the budget: %v", err)
	}
}

func TestClientLimit(t *testing.T) {
	a, _, _ := setup(t)
	a.wait = time.Second
	fillGate(t, a)
	flooder := WithClient(context.Background(), "203.0.113.9")
	var wg sync.WaitGroup
	for i := 0; i < a.perClient; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Authenticate(flooder, "nobody", "x")
		}()
	}
	for {
		a.mu.Lock()
		n := a.inflight["203.0.113.9"]
		a.mu.Unlock()
		if n == a.perClient {
			break
		}
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	if _, err := a.Authenticate(flooder, "nobody", "x"); !errors.Is(err, ErrBusy) || time.Since(start) > 500*time.Millisecond {
		t.Errorf("over-limit client: %v after %v, want an immediate ErrBusy", err, time.Since(start))
	}
	// The queued requests free their slot when the gate empties.
	emptyGate(a)
	wg.Wait()
	a.mu.Lock()
	left := len(a.inflight)
	a.mu.Unlock()
	if left != 0 {
		t.Errorf("%d client entries leaked", left)
	}
}

func TestSignInSurvivesFlood(t *testing.T) {
	a, _, _ := setup(t)
	a.wait = 5 * time.Second
	stop := make(chan struct{})
	var wg sync.WaitGroup
	// Eight flooding addresses, each over its per-client cap. Kept modest so
	// the test measures queueing, not how slow Argon2 gets on a loaded machine.
	for i := 0; i < 8*(clientLimit+2); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := WithClient(context.Background(), fmt.Sprintf("198.51.100.%d", i%8))
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				a.Authenticate(ctx, fmt.Sprintf("nobody-%d-%d", i, n), "x")
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	_, err := a.Authenticate(WithClient(context.Background(), "192.0.2.1"), "jarmos", "correct horse battery")
	took := time.Since(start)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("real sign-in during flood: %v", err)
	}
	// Flooders over their cap are shed at once, so the real client only
	// queues for a hashing slot: it must get one within the bounded wait
	// (succeeding above proves that) plus its own hash.
	if limit := a.wait + 3*time.Second; took > limit {
		t.Errorf("real sign-in took %v during the flood, want under %v", took, limit)
	}
}
