package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/store"
)

// First-run setup errors callers can branch on.
var (
	// ErrSetupToken covers a wrong, expired, used or never-issued token; the
	// caller must not say which.
	ErrSetupToken = errors.New("the setup link is not valid")
	// ErrSetupDone means an account already exists, so setup is closed.
	ErrSetupDone = errors.New("setup is already done")
)

// SetupTTL is how long a first-run setup token stays valid.
const SetupTTL = time.Hour

// setupToken is the one live first-run token. Only its digest is kept, in
// memory: a restart forgets it and prints a new one.
type setupToken struct {
	digest  [sha256.Size]byte
	expires time.Time
}

// CheckUsername returns an error people can act on if name cannot be an
// account name. name must already be normalized.
func CheckUsername(name string) error {
	if name == "" || len(name) > 64 || strings.Contains(name, " ") || post.HasControl(name) {
		return errors.New("a username is 1 to 64 characters without spaces or control characters")
	}
	return nil
}

// SetupNeeded reports whether the server has no accounts yet.
func (a *Service) SetupNeeded(ctx context.Context) (bool, error) {
	has, err := a.store.HasUsers(ctx)
	return !has, err
}

// IssueSetupToken makes a one-time token for creating the first account,
// valid for ttl, replacing any earlier one. It returns the secret once.
func (a *Service) IssueSetupToken(ctx context.Context, ttl time.Duration) (string, error) {
	needed, err := a.SetupNeeded(ctx)
	if err != nil {
		return "", err
	}
	if !needed {
		return "", ErrSetupDone
	}
	secret, err := randomToken("")
	if err != nil {
		return "", err
	}
	a.setupMu.Lock()
	a.setup = &setupToken{digest: sha256.Sum256([]byte(secret)), expires: a.now().Add(ttl)}
	a.setupMu.Unlock()
	return secret, nil
}

// checkSetupToken compares in constant time and does not consume the token.
func (a *Service) checkSetupToken(secret string) bool {
	got := sha256.Sum256([]byte(secret))
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	if a.setup == nil {
		return false
	}
	match := subtle.ConstantTimeCompare(got[:], a.setup.digest[:]) == 1
	return match && a.now().Before(a.setup.expires)
}

func (a *Service) dropSetupToken() {
	a.setupMu.Lock()
	a.setup = nil
	a.setupMu.Unlock()
}

// Setup creates the first account when secret is the live setup token. It
// shares sign-in's hashing slots and attempt limit. The token dies with the
// first account, however that account was made.
func (a *Service) Setup(ctx context.Context, secret, username, password string) (*store.User, error) {
	username = store.NormalizeUsername(username)
	if err := CheckUsername(username); err != nil {
		return nil, err
	}
	if err := CheckPassword(password); err != nil {
		return nil, err
	}
	needed, err := a.SetupNeeded(ctx)
	if err != nil {
		return nil, err
	}
	if !needed {
		a.dropSetupToken()
		return nil, ErrSetupDone
	}
	release, err := a.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	client, _ := ctx.Value(clientKey{}).(string)
	key := "setup:" + client
	// Counted before the token is checked, like a sign-in, so parallel
	// guesses cannot slip past the limit.
	ok, err := a.store.ReserveLoginAttempt(ctx, key, userLimit, window)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrRateLimited
	}
	if !a.checkSetupToken(secret) {
		// A request that lost a race with the first account finds the token
		// gone; tell it setup is done, not that its link was wrong.
		if needed, err := a.SetupNeeded(ctx); err == nil && !needed {
			return nil, ErrSetupDone
		}
		return nil, ErrSetupToken
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	user, err := a.store.CreateFirstUser(ctx, username, hash)
	if errors.Is(err, store.ErrHasUsers) {
		a.dropSetupToken()
		return nil, ErrSetupDone
	}
	if err != nil {
		return nil, fmt.Errorf("create first account: %w", err)
	}
	a.dropSetupToken()
	_ = a.store.ClearLoginFailures(ctx, key)
	return user, nil
}
