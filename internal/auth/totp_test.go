package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/store"
)

// RFC 6238 Appendix B, SHA-1, 8 digits, secret "12345678901234567890".
func TestHOTPRFC6238Vectors(t *testing.T) {
	key := []byte("12345678901234567890")
	for _, v := range []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	} {
		if got := hotp(key, uint64(v.unix/totpPeriod), 8); got != v.want {
			t.Errorf("T=%d: got %s, want %s", v.unix, got, v.want)
		}
	}
}

func TestTOTPCodeSixDigits(t *testing.T) {
	secret := base32Secret.EncodeToString([]byte("12345678901234567890"))
	got, err := TOTPCode(secret, time.Unix(59, 0))
	if err != nil || got != "287082" {
		t.Errorf("TOTPCode(59) = %q, %v; want 287082", got, err)
	}
	if _, err := TOTPCode("not base32!", time.Unix(0, 0)); err == nil {
		t.Error("invalid secret accepted")
	}
}

func TestMatchTOTPWindow(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 32 {
		t.Errorf("secret %q is %d characters, want 32 (160 bits)", secret, len(secret))
	}
	now := time.Unix(1_700_000_000, 0)
	for _, c := range []struct {
		offset time.Duration
		ok     bool
	}{
		{0, true}, {-30 * time.Second, true}, {30 * time.Second, true},
		{-60 * time.Second, false}, {60 * time.Second, false},
	} {
		code, _ := TOTPCode(secret, now.Add(c.offset))
		if _, ok := matchTOTP(secret, code, now); ok != c.ok {
			t.Errorf("code from %v away: accepted = %v, want %v", c.offset, ok, c.ok)
		}
	}
	if _, ok := matchTOTP(secret, "12345", now); ok {
		t.Error("short code accepted")
	}
}

func TestTOTPURI(t *testing.T) {
	uri := TOTPURI("BurzBlog", "ada@blog.example.com", "ABC234")
	for _, want := range []string{"otpauth://totp/BurzBlog:ada@blog.example.com?", "secret=ABC234", "issuer=BurzBlog", "digits=6", "period=30", "algorithm=SHA1"} {
		if !strings.Contains(uri, want) {
			t.Errorf("uri %q lacks %q", uri, want)
		}
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// twoFactor returns a service whose clock is fake, and ada with two-factor
// sign-in on.
func twoFactor(t *testing.T) (*Service, *store.Store, *store.User, *fakeClock, string, []string) {
	t.Helper()
	a, s, u := setup(t)
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	a.now = clock.now
	s.SetClock(clock.now)
	ctx := context.Background()
	secret, err := a.StartTwoFactor(ctx, u, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := a.TwoFactor(ctx, u); !st.Pending || st.Enabled {
		t.Fatalf("after start: %+v", st)
	}
	code, _ := TOTPCode(secret, clock.t)
	codes, err := a.ConfirmTwoFactor(ctx, u, code)
	if err != nil {
		t.Fatal(err)
	}
	clock.t = clock.t.Add(time.Minute) // the confirming code is spent
	return a, s, u, clock, secret, codes
}

func TestEnrolment(t *testing.T) {
	a, s, u, _, _, codes := twoFactor(t)
	ctx := context.Background()
	if len(codes) != 10 {
		t.Fatalf("got %d recovery codes, want 10", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(normalizeCode(c)) != recoveryChars || seen[c] {
			t.Errorf("bad or repeated recovery code %q", c)
		}
		seen[c] = true
	}
	if st, _ := a.TwoFactor(ctx, u); !st.Enabled || st.RecoveryCodes != 10 {
		t.Errorf("status = %+v", st)
	}
	// Only digests are stored.
	var plain int
	for _, c := range codes {
		if ok, _ := s.UseRecoveryCode(ctx, u.ID, c); ok {
			plain++
		}
	}
	if plain != 0 {
		t.Errorf("%d recovery codes are stored in the clear", plain)
	}
	if _, err := a.StartTwoFactor(ctx, u, "correct horse battery"); !errors.Is(err, ErrTwoFactorOn) {
		t.Errorf("starting twice = %v, want ErrTwoFactorOn", err)
	}
}

func TestEnrolmentNeedsPasswordAndCode(t *testing.T) {
	a, _, u := setup(t)
	ctx := context.Background()
	if _, err := a.StartTwoFactor(ctx, u, "wrong password here"); !errors.Is(err, ErrCredentials) {
		t.Errorf("start with wrong password = %v", err)
	}
	if _, err := a.ConfirmTwoFactor(ctx, u, "123456"); !errors.Is(err, ErrTwoFactorOff) {
		t.Errorf("confirm without start = %v", err)
	}
	if _, err := a.StartTwoFactor(ctx, u, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmTwoFactor(ctx, u, "000000"); !errors.Is(err, ErrCode) {
		t.Errorf("confirm with wrong code = %v", err)
	}
	if st, _ := a.TwoFactor(ctx, u); st.Enabled {
		t.Error("two-factor turned on without a valid code")
	}
	if ok, _ := a.NeedsSecondFactor(ctx, u); ok {
		t.Error("a pending enrolment already demands a code")
	}
}

func TestPasswordAloneDoesNotSignIn2FAAccount(t *testing.T) {
	a, _, _, _, _, _ := twoFactor(t)
	user, second, err := a.Login(context.Background(), "ada", "correct horse battery")
	if err != nil || !second || user == nil {
		t.Fatalf("Login = %v, %v, %v; want the second factor requested", user, second, err)
	}
}

func TestLoginWithoutTwoFactorUnchanged(t *testing.T) {
	a, _, _ := setup(t)
	ctx := context.Background()
	user, second, err := a.Login(ctx, "ada", "correct horse battery")
	if err != nil || second || user.Username != "ada" {
		t.Fatalf("Login = %v, %v, %v", user, second, err)
	}
	if _, _, err := a.Login(ctx, "ada", "wrong"); !errors.Is(err, ErrCredentials) {
		t.Errorf("wrong password = %v", err)
	}
	// An unknown name looks exactly like a wrong password.
	if _, _, err := a.Login(ctx, "ghost", "whatever"); !errors.Is(err, ErrCredentials) {
		t.Errorf("unknown user = %v", err)
	}
}

func TestChallengeCompletesOnce(t *testing.T) {
	a, _, u, clock, secret, _ := twoFactor(t)
	ctx := context.Background()
	ch, err := a.NewChallenge(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := TOTPCode(secret, clock.t)
	got, err := a.CompleteChallenge(ctx, ch, code)
	if err != nil || got.ID != u.ID {
		t.Fatalf("complete = %v, %v", got, err)
	}
	// Single use, even with a fresh valid code.
	clock.t = clock.t.Add(time.Minute)
	next, _ := TOTPCode(secret, clock.t)
	if _, err := a.CompleteChallenge(ctx, ch, next); !errors.Is(err, ErrChallenge) {
		t.Errorf("second use = %v, want ErrChallenge", err)
	}
}

func TestChallengeExpires(t *testing.T) {
	a, _, u, clock, secret, _ := twoFactor(t)
	ctx := context.Background()
	ch, _ := a.NewChallenge(ctx, u)
	clock.t = clock.t.Add(ChallengeTTL + time.Second)
	code, _ := TOTPCode(secret, clock.t)
	if _, err := a.CompleteChallenge(ctx, ch, code); !errors.Is(err, ErrChallenge) {
		t.Errorf("expired challenge = %v, want ErrChallenge", err)
	}
}

func TestChallengeBoundToItsUser(t *testing.T) {
	a, s, u, clock, _, _ := twoFactor(t)
	ctx := context.Background()
	hash, _ := HashPassword("another long password")
	grace, err := s.CreateUser(ctx, "grace", hash)
	if err != nil {
		t.Fatal(err)
	}
	graceSecret, err := a.StartTwoFactor(ctx, grace, "another long password")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := TOTPCode(graceSecret, clock.t)
	if _, err := a.ConfirmTwoFactor(ctx, grace, code); err != nil {
		t.Fatal(err)
	}
	clock.t = clock.t.Add(time.Minute)
	// Ada's challenge with Grace's valid code does not sign anyone in.
	ch, _ := a.NewChallenge(ctx, u)
	graceCode, _ := TOTPCode(graceSecret, clock.t)
	if _, err := a.CompleteChallenge(ctx, ch, graceCode); !errors.Is(err, ErrCode) {
		t.Errorf("another user's code = %v, want ErrCode", err)
	}
	if _, err := a.CompleteChallenge(ctx, "made-up", graceCode); !errors.Is(err, ErrChallenge) {
		t.Errorf("unknown challenge = %v, want ErrChallenge", err)
	}
}

func TestChallengeStoredHashed(t *testing.T) {
	a, s, u, _, _, _ := twoFactor(t)
	ctx := context.Background()
	ch, _ := a.NewChallenge(ctx, u)
	if _, err := s.ChallengeUser(ctx, ch); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("raw challenge found in the database: %v", err)
	}
	if got, err := s.ChallengeUser(ctx, Digest(ch)); err != nil || got.ID != u.ID {
		t.Errorf("digest lookup = %v, %v", got, err)
	}
}

func TestTOTPReplayRefused(t *testing.T) {
	a, _, u, clock, secret, _ := twoFactor(t)
	ctx := context.Background()
	code, _ := TOTPCode(secret, clock.t)
	if err := a.VerifySecondFactor(ctx, u, code); err != nil {
		t.Fatal(err)
	}
	if err := a.VerifySecondFactor(ctx, u, code); !errors.Is(err, ErrCode) {
		t.Errorf("replayed code = %v, want ErrCode", err)
	}
	// The previous step is inside the window but older than the one used.
	old, _ := TOTPCode(secret, clock.t.Add(-30*time.Second))
	if err := a.VerifySecondFactor(ctx, u, old); !errors.Is(err, ErrCode) {
		t.Errorf("older step after a newer one = %v, want ErrCode", err)
	}
	// A code one step ahead (a fast phone clock) is still accepted.
	ahead, _ := TOTPCode(secret, clock.t.Add(30*time.Second))
	if err := a.VerifySecondFactor(ctx, u, ahead); err != nil {
		t.Errorf("code one step ahead = %v", err)
	}
}

func TestCodeFormatTolerated(t *testing.T) {
	a, _, u, clock, secret, _ := twoFactor(t)
	code, _ := TOTPCode(secret, clock.t)
	if err := a.VerifySecondFactor(context.Background(), u, " "+code[:3]+" "+code[3:]+" "); err != nil {
		t.Errorf("code with spaces = %v", err)
	}
}

func TestRecoveryCodeSingleUse(t *testing.T) {
	a, _, u, _, _, codes := twoFactor(t)
	ctx := context.Background()
	if err := a.VerifySecondFactor(ctx, u, strings.ToUpper(codes[0])); err != nil {
		t.Fatalf("recovery code = %v", err)
	}
	if err := a.VerifySecondFactor(ctx, u, codes[0]); !errors.Is(err, ErrCode) {
		t.Errorf("recovery code reused = %v, want ErrCode", err)
	}
	if st, _ := a.TwoFactor(ctx, u); st.RecoveryCodes != 9 {
		t.Errorf("recovery codes left = %d, want 9", st.RecoveryCodes)
	}
	if err := a.VerifySecondFactor(ctx, u, "aaaa-aaaa-aaaa-aaaa"); !errors.Is(err, ErrCode) {
		t.Errorf("made-up recovery code = %v", err)
	}
}

func TestWrongCodesCountTowardTheSameLimit(t *testing.T) {
	a, _, u, clock, secret, _ := twoFactor(t)
	ctx := context.Background()
	// The password step already used one attempt of the ten.
	if _, _, err := a.Login(ctx, "ada", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < userLimit-1; i++ {
		if err := a.VerifySecondFactor(ctx, u, "000000"); !errors.Is(err, ErrCode) {
			t.Fatalf("wrong code %d = %v, want ErrCode", i, err)
		}
	}
	// Locked out: even the right code and the right password are refused.
	code, _ := TOTPCode(secret, clock.t)
	if err := a.VerifySecondFactor(ctx, u, code); !errors.Is(err, ErrRateLimited) {
		t.Errorf("right code while locked = %v, want ErrRateLimited", err)
	}
	if _, _, err := a.Login(ctx, "ada", "correct horse battery"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("password while locked = %v, want ErrRateLimited", err)
	}
}

func TestCorrectPasswordDoesNotResetCodeGuessing(t *testing.T) {
	a, _, u, _, _, _ := twoFactor(t)
	ctx := context.Background()
	// Someone who knows the password cannot get fresh code guesses by
	// signing in again: the failure count survives the password step.
	guesses := 0
	for i := 0; i < 50; i++ {
		_, _, err := a.Login(ctx, "ada", "correct horse battery")
		if errors.Is(err, ErrRateLimited) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := a.VerifySecondFactor(ctx, u, "000000"); errors.Is(err, ErrCode) {
			guesses++
		}
	}
	if guesses >= userLimit {
		t.Errorf("%d code guesses fit in one window, want fewer than %d", guesses, userLimit)
	}
}

func TestCompletingTheSecondStepClearsFailures(t *testing.T) {
	a, _, u, clock, secret, _ := twoFactor(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		a.Login(ctx, "ada", "wrong")
	}
	ch, _ := a.NewChallenge(ctx, u)
	code, _ := TOTPCode(secret, clock.t)
	if _, err := a.CompleteChallenge(ctx, ch, code); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < userLimit-1; i++ {
		if _, _, err := a.Login(ctx, "ada", "wrong"); !errors.Is(err, ErrCredentials) {
			t.Fatalf("attempt %d = %v: failures were not cleared", i, err)
		}
	}
}

func TestDisableTwoFactor(t *testing.T) {
	a, s, u, clock, secret, _ := twoFactor(t)
	ctx := context.Background()
	_, keep, err := a.NewSession(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	token, _, _ := a.NewSession(ctx, u)
	code, _ := TOTPCode(secret, clock.t)
	if err := a.DisableTwoFactor(ctx, u, "wrong password here", code, keep.TokenHash); !errors.Is(err, ErrCredentials) {
		t.Errorf("disable with wrong password = %v", err)
	}
	if err := a.DisableTwoFactor(ctx, u, "correct horse battery", "000000", keep.TokenHash); !errors.Is(err, ErrCode) {
		t.Errorf("disable with wrong code = %v", err)
	}
	if st, _ := a.TwoFactor(ctx, u); !st.Enabled {
		t.Fatal("two-factor turned off by a refused request")
	}
	if err := a.DisableTwoFactor(ctx, u, "correct horse battery", code, keep.TokenHash); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.TwoFactor(ctx, u); st.Enabled || st.Pending || st.RecoveryCodes != 0 {
		t.Errorf("status after disable = %+v", st)
	}
	if _, err := s.SessionByHash(ctx, keep.TokenHash); err != nil {
		t.Errorf("current session ended: %v", err)
	}
	if _, _, err := a.Session(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Errorf("other session still valid: %v", err)
	}
	if _, second, err := a.Login(ctx, "ada", "correct horse battery"); err != nil || second {
		t.Errorf("sign-in after disable = %v, %v", second, err)
	}
}
