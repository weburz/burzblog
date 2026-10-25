// Package auth signs people in: Argon2id passwords, browser sessions with
// CSRF tokens, API tokens for the CLI and reader builds, and bounded login
// attempts.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/weburz/burzblog/internal/store"
)

// Errors callers can branch on.
var (
	ErrCredentials = errors.New("wrong username or password")
	ErrRateLimited = errors.New("too many failed sign-in attempts")
	ErrNoSession   = errors.New("not signed in")
	ErrBusy        = errors.New("too many sign-ins are being checked")
)

// Argon2id parameters: OWASP's 19 MiB, 2 passes, 1 lane. Comfortable on a
// Raspberry Pi; the concurrency gate below bounds memory under load.
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
)

// Password rules.
const (
	MinPassword = 12
	MaxPassword = 1024
)

// HashPassword returns a PHC-format Argon2id hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read random salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword checks password against a PHC hash and reports whether the
// hash uses weaker parameters than the current ones.
func verifyPassword(hash, password string) (ok, stale bool) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false
	}
	var version int
	var memory uint32
	var passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false, false
	}
	// Refuse absurd stored parameters instead of allocating them.
	if memory == 0 || memory > 1<<20 || passes == 0 || passes > 16 || threads == 0 {
		return false, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 64 {
		return false, false
	}
	got := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(want)))
	ok = subtle.ConstantTimeCompare(got, want) == 1
	return ok, memory < argonMemory || passes < argonTime
}

// CheckPassword returns an error people can act on if password is too weak.
func CheckPassword(password string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case !utf8.ValidString(password):
		return errors.New("password must be valid text")
	case n < MinPassword:
		return fmt.Errorf("password must be at least %d characters", MinPassword)
	case len(password) > MaxPassword:
		return fmt.Errorf("password must be at most %d bytes", MaxPassword)
	}
	return nil
}

// Digest is how secrets are stored: SHA-256, hex.
func Digest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func randomToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// TokenPrefix marks BurzBlog API tokens so secret scanners can find them.
const TokenPrefix = "bzb_"

// Sign-in limits per username. There is deliberately no instance-wide
// failure limit: anyone could exhaust it and lock every author out. The gate
// below bounds how much password hashing runs at once instead, and sheds
// requests (ErrBusy, never a lockout) that cannot get a slot soon.
const (
	window     = 15 * time.Minute
	userLimit  = 10
	SessionTTL = 12 * time.Hour

	// gateWait is how long a sign-in queues for a hashing slot before it is
	// told to retry, so a flood cannot build an unbounded queue.
	gateWait = 2 * time.Second
	// clientLimit caps the sign-ins one client address may have queued or
	// hashing at once, so one flooding machine cannot fill the queue. It
	// only separates clients when requests arrive from their own
	// addresses: behind a reverse proxy (X-Forwarded-For is ignored on
	// purpose) everyone shares the proxy's address, so the limit leaves room
	// for a short queue behind the two hashing slots.
	clientLimit = 4
)

type clientKey struct{}

// WithClient tags ctx with the address a sign-in came from.
func WithClient(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, clientKey{}, addr)
}

// Service signs people in against the store.
type Service struct {
	store *store.Store
	gate  chan struct{}
	wait  time.Duration
	// inflight counts queued and hashing sign-ins per client address.
	mu        sync.Mutex
	inflight  map[string]int
	perClient int
	// dummy is verified when a username does not exist, so timing does not
	// reveal which accounts exist.
	dummy string
}

// New returns a Service. Only two password checks run at once.
func New(s *store.Store) (*Service, error) {
	dummy, err := HashPassword("burzblog-timing-equalizer")
	if err != nil {
		return nil, err
	}
	return &Service{store: s, gate: make(chan struct{}, 2), wait: gateWait,
		inflight: map[string]int{}, perClient: clientLimit, dummy: dummy}, nil
}

// Authenticate checks a username and password, with rate limiting.
func (a *Service) Authenticate(ctx context.Context, username, password string) (*store.User, error) {
	username = store.NormalizeUsername(username)
	if username == "" || password == "" || len(password) > MaxPassword || len(username) > 64 {
		return nil, ErrCredentials
	}
	release, err := a.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	userKey := "user:" + strings.ToLower(username)
	// Reserve the attempt before hashing so parallel guesses cannot all pass.
	ok, err := a.store.ReserveLoginAttempt(ctx, userKey, userLimit, window)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrRateLimited
	}
	user, err := a.store.UserByName(ctx, username)
	hash := a.dummy
	if err == nil {
		hash = user.PasswordHash
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	verified, stale := verifyPassword(hash, password)
	if err != nil || !verified {
		return nil, ErrCredentials
	}
	if err := a.store.ClearLoginFailures(ctx, userKey); err != nil {
		return nil, err
	}
	if stale {
		if fresh, err := HashPassword(password); err == nil {
			_ = a.store.RehashPassword(ctx, user.ID, fresh)
		}
	}
	return user, nil
}

// acquire takes a hashing slot, waiting at most a.wait, and returns the
// function that frees it. The slot is taken before the attempt is counted so
// a shed request does not use up anyone's budget.
func (a *Service) acquire(ctx context.Context) (func(), error) {
	client, _ := ctx.Value(clientKey{}).(string)
	a.mu.Lock()
	if client != "" && a.inflight[client] >= a.perClient {
		a.mu.Unlock()
		return nil, ErrBusy
	}
	a.inflight[client]++
	a.mu.Unlock()
	free := func() {
		a.mu.Lock()
		if a.inflight[client]--; a.inflight[client] <= 0 {
			delete(a.inflight, client)
		}
		a.mu.Unlock()
	}
	timer := time.NewTimer(a.wait)
	defer timer.Stop()
	select {
	case a.gate <- struct{}{}:
		return func() { <-a.gate; free() }, nil
	case <-timer.C:
		free()
		return nil, ErrBusy
	case <-ctx.Done():
		free()
		return nil, ctx.Err()
	}
}

// NewSession starts a browser session and returns the cookie value.
func (a *Service) NewSession(ctx context.Context, user *store.User) (token string, sess *store.Session, err error) {
	token, err = randomToken("")
	if err != nil {
		return "", nil, err
	}
	csrf, err := randomToken("")
	if err != nil {
		return "", nil, err
	}
	sess = &store.Session{TokenHash: Digest(token), UserID: user.ID, CSRF: csrf, ExpiresAt: time.Now().Add(SessionTTL)}
	if err := a.store.CreateSession(ctx, *sess); err != nil {
		return "", nil, err
	}
	return token, sess, nil
}

// Session resolves a session cookie value.
func (a *Service) Session(ctx context.Context, token string) (*store.Session, *store.User, error) {
	if token == "" || len(token) > 128 {
		return nil, nil, ErrNoSession
	}
	sess, err := a.store.SessionByHash(ctx, Digest(token))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, ErrNoSession
	}
	if err != nil {
		return nil, nil, err
	}
	user, err := a.store.UserByID(ctx, sess.UserID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, ErrNoSession
	}
	return sess, user, err
}

// CheckCSRF compares a request's CSRF header with the session's token.
func CheckCSRF(sess *store.Session, header string) bool {
	return header != "" && subtle.ConstantTimeCompare([]byte(sess.CSRF), []byte(header)) == 1
}

// EndSession signs a browser out.
func (a *Service) EndSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return a.store.DeleteSession(ctx, Digest(token))
}

// NewToken issues an API token and returns the secret once.
func (a *Service) NewToken(ctx context.Context, user *store.User, name, scope string) (string, *store.Token, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 || strings.ContainsAny(name, "\r\n\x00") {
		return "", nil, errors.New("token name must be one line of 1 to 100 characters")
	}
	if scope != store.ScopeWrite && scope != store.ScopeExport {
		return "", nil, errors.New("token scope must be write or export")
	}
	secret, err := randomToken(TokenPrefix)
	if err != nil {
		return "", nil, err
	}
	t, err := a.store.CreateToken(ctx, user.ID, Digest(secret), name, scope)
	if err != nil {
		return "", nil, err
	}
	t.Username = user.Username
	return secret, t, nil
}

// Token resolves a bearer token.
func (a *Service) Token(ctx context.Context, secret string) (*store.Token, error) {
	if !strings.HasPrefix(secret, TokenPrefix) || len(secret) > 128 {
		return nil, ErrNoSession
	}
	t, err := a.store.TokenByHash(ctx, Digest(secret))
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNoSession
	}
	return t, err
}

// ChangePassword verifies the current password and stores a new one,
// signing out every other browser.
func (a *Service) ChangePassword(ctx context.Context, user *store.User, current, next, keepSessionHash string) error {
	if _, err := a.Authenticate(ctx, user.Username, current); err != nil {
		return err
	}
	if err := CheckPassword(next); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return a.store.SetPassword(ctx, user.ID, hash, keepSessionHash)
}
