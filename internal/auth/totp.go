package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 authenticator apps use HMAC-SHA1.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/weburz/burzblog/internal/store"
)

// Errors for the second sign-in step.
var (
	ErrCode      = errors.New("wrong two-factor code")
	ErrChallenge = errors.New("the sign-in expired")
	// ErrSecondFactor means the password was right but the account also needs a code.
	ErrSecondFactor = errors.New("a two-factor code is required")
	// ErrTwoFactorOn and ErrTwoFactorOff report enrolment in the wrong state.
	ErrTwoFactorOn  = errors.New("two-factor sign-in is already on")
	ErrTwoFactorOff = errors.New("two-factor sign-in is not on")
)

// TOTP parameters, RFC 6238 defaults that every authenticator app supports.
const (
	totpPeriod = 30
	totpDigits = 6
	// totpSkew is how many steps either side of now are accepted, to forgive
	// a phone clock that is a little off.
	totpSkew = 1
	// ChallengeTTL is how long a correct password waits for its code.
	ChallengeTTL = 5 * time.Minute

	recoveryCount = 10
	// recoveryChars is the length of a recovery code: 16 base32 characters,
	// 80 random bits.
	recoveryChars = 16
)

var base32Secret = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret in base32, the form
// authenticator apps accept.
func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random secret: %w", err)
	}
	return base32Secret.EncodeToString(b), nil
}

// TOTPURI is the otpauth:// address an authenticator app imports.
func TOTPURI(issuer, account, secret string) string {
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + url.PathEscape(issuer) + ":" + url.PathEscape(account) + "?" + q.Encode()
}

// hotp is RFC 4226: HMAC-SHA1 over the counter, dynamically truncated.
func hotp(key []byte, counter uint64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, n%mod)
}

func totpKey(secret string) ([]byte, error) {
	key, err := base32Secret.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 {
		return nil, errors.New("two-factor secret is not valid base32")
	}
	return key, nil
}

func totpStep(t time.Time) int64 {
	return t.Unix() / totpPeriod
}

// TOTPCode is the code an authenticator app shows at t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := totpKey(secret)
	if err != nil {
		return "", err
	}
	return hotp(key, uint64(totpStep(t)), totpDigits), nil
}

// matchTOTP checks code against the steps around now and returns the step it
// belongs to.
func matchTOTP(secret, code string, now time.Time) (int64, bool) {
	key, err := totpKey(secret)
	if err != nil || len(code) != totpDigits {
		return 0, false
	}
	var matched int64
	found := false
	// Every step is compared, whatever the first result, so timing does not
	// reveal which one was close.
	for d := -totpSkew; d <= totpSkew; d++ {
		step := totpStep(now) + int64(d)
		if step < 0 {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(hotp(key, uint64(step), totpDigits)), []byte(code)) == 1 {
			matched, found = step, true
		}
	}
	return matched, found
}

// normalizeCode lets people type a code the way it was shown: with spaces or
// dashes, in any case.
func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(code)))
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// newRecoveryCodes returns recoveryCount codes shaped xxxx-xxxx-xxxx-xxxx.
// They have 80 random bits each, so a plain SHA-256 digest is enough to store
// them: there is nothing to guess offline, unlike a password.
func newRecoveryCodes() ([]string, error) {
	codes := make([]string, recoveryCount)
	for i := range codes {
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("read random recovery code: %w", err)
		}
		raw := strings.ToLower(base32Secret.EncodeToString(b))[:recoveryChars]
		codes[i] = raw[0:4] + "-" + raw[4:8] + "-" + raw[8:12] + "-" + raw[12:16]
	}
	return codes, nil
}

// TwoFactorStatus is what Settings shows about a user's second factor.
type TwoFactorStatus struct {
	Enabled       bool
	Pending       bool
	RecoveryCodes int
}

// TwoFactor reports a user's second-factor state.
func (a *Service) TwoFactor(ctx context.Context, user *store.User) (TwoFactorStatus, error) {
	t, err := a.store.TOTP(ctx, user.ID)
	if errors.Is(err, store.ErrNotFound) {
		return TwoFactorStatus{}, nil
	}
	if err != nil {
		return TwoFactorStatus{}, err
	}
	if !t.Enabled {
		return TwoFactorStatus{Pending: true}, nil
	}
	n, err := a.store.RecoveryCodesLeft(ctx, user.ID)
	return TwoFactorStatus{Enabled: true, RecoveryCodes: n}, err
}

// StartTwoFactor verifies the password and stores a fresh pending secret,
// returning it for the authenticator app.
func (a *Service) StartTwoFactor(ctx context.Context, user *store.User, password string) (string, error) {
	if _, err := a.Authenticate(ctx, user.Username, password); err != nil {
		return "", err
	}
	secret, err := NewTOTPSecret()
	if err != nil {
		return "", err
	}
	if err := a.store.StartTOTP(ctx, user.ID, secret); errors.Is(err, store.ErrTaken) {
		return "", ErrTwoFactorOn
	} else if err != nil {
		return "", err
	}
	return secret, nil
}

// ConfirmTwoFactor turns two-factor sign-in on once the pending secret
// produced a valid code, and returns the recovery codes. This is the only
// time they exist in the clear.
func (a *Service) ConfirmTwoFactor(ctx context.Context, user *store.User, code string) ([]string, error) {
	t, err := a.store.TOTP(ctx, user.ID)
	if errors.Is(err, store.ErrNotFound) || err == nil && t.Enabled {
		return nil, ErrTwoFactorOff
	}
	if err != nil {
		return nil, err
	}
	step, ok := matchTOTP(t.Secret, normalizeCode(code), a.now())
	if !ok {
		return nil, ErrCode
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = Digest(normalizeCode(c))
	}
	if err := a.store.EnableTOTP(ctx, user.ID, step, hashes); errors.Is(err, store.ErrNotFound) {
		return nil, ErrTwoFactorOff
	} else if err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTwoFactor needs the password and a current code (or recovery code),
// then ends every other session.
func (a *Service) DisableTwoFactor(ctx context.Context, user *store.User, password, code, keepSessionHash string) error {
	if _, err := a.Authenticate(ctx, user.Username, password); err != nil {
		return err
	}
	if err := a.VerifySecondFactor(ctx, user, code); err != nil {
		return err
	}
	return a.store.DisableTOTP(ctx, user.ID, keepSessionHash)
}

// NeedsSecondFactor reports whether signing in as user takes a code.
func (a *Service) NeedsSecondFactor(ctx context.Context, user *store.User) (bool, error) {
	t, err := a.store.TOTP(ctx, user.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return err == nil && t.Enabled, err
}

// NewChallenge starts the second step of a sign-in whose password was right.
// The challenge is single-use and short-lived; only its digest is stored.
func (a *Service) NewChallenge(ctx context.Context, user *store.User) (string, error) {
	challenge, err := randomToken("")
	if err != nil {
		return "", err
	}
	if err := a.store.CreateChallenge(ctx, user.ID, Digest(challenge), a.now().Add(ChallengeTTL)); err != nil {
		return "", err
	}
	return challenge, nil
}

// CompleteChallenge finishes a sign-in: the code must be right for the user
// the challenge was issued to, and the challenge is spent.
func (a *Service) CompleteChallenge(ctx context.Context, challenge, code string) (*store.User, error) {
	if challenge == "" || len(challenge) > 128 {
		return nil, ErrChallenge
	}
	hash := Digest(challenge)
	user, err := a.store.ChallengeUser(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrChallenge
	}
	if err != nil {
		return nil, err
	}
	if err := a.VerifySecondFactor(ctx, user, code); err != nil {
		return nil, err
	}
	if spent, err := a.store.ConsumeChallenge(ctx, hash); err != nil {
		return nil, err
	} else if !spent {
		return nil, ErrChallenge
	}
	return user, nil
}

// VerifySecondFactor checks a TOTP code or a recovery code. Attempts count
// against the same per-username limit as passwords, so codes are no separate
// guessing oracle. A TOTP step works once, and a recovery code is consumed.
// A right code also clears the username's failures.
func (a *Service) VerifySecondFactor(ctx context.Context, user *store.User, code string) error {
	userKey := "user:" + strings.ToLower(user.Username)
	ok, err := a.store.ReserveLoginAttempt(ctx, userKey, userLimit, window)
	if err != nil {
		return err
	}
	if !ok {
		return ErrRateLimited
	}
	t, err := a.store.TOTP(ctx, user.ID)
	if errors.Is(err, store.ErrNotFound) || err == nil && !t.Enabled {
		return ErrTwoFactorOff
	}
	if err != nil {
		return err
	}
	code = normalizeCode(code)
	var accepted bool
	switch {
	case len(code) == totpDigits && isDigits(code):
		step, match := matchTOTP(t.Secret, code, a.now())
		if match {
			accepted, err = a.store.AdvanceTOTPStep(ctx, user.ID, step)
		}
	case len(code) == recoveryChars:
		accepted, err = a.store.UseRecoveryCode(ctx, user.ID, Digest(code))
	}
	if err != nil {
		return err
	}
	if !accepted {
		return ErrCode
	}
	return a.store.ClearLoginFailures(ctx, userKey)
}
