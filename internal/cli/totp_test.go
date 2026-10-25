package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/auth"
)

// withTwoFactor creates ada with two-factor sign-in on and returns the
// secret and her recovery codes.
func withTwoFactor(t *testing.T, e *env) (string, []string) {
	t.Helper()
	e.must("correct horse battery\n", "user", "add", "ada")
	ctx := context.Background()
	svc, _ := auth.New(e.store)
	u, err := e.store.UserByName(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.StartTwoFactor(ctx, u, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := auth.TOTPCode(secret, time.Now())
	codes, err := svc.ConfirmTwoFactor(ctx, u, code)
	if err != nil {
		t.Fatal(err)
	}
	return secret, codes
}

func nextCode(secret string) string {
	code, _ := auth.TOTPCode(secret, time.Now().Add(30*time.Second))
	return code
}

func TestLoginTwoFactor(t *testing.T) {
	e := newEnv(t)
	secret, codes := withTwoFactor(t, e)

	// No code and no way to ask for one: a clear error, no config written.
	_, errOut, code := e.run("correct horse battery\n", "login", e.url, "-u", "ada")
	if code == 0 || !strings.Contains(errOut, "--code") || !strings.Contains(errOut, "BURZBLOG_TOTP") {
		t.Errorf("login without a code: %d %q", code, errOut)
	}
	if _, errOut, code := e.run("", "whoami"); code == 0 || !strings.Contains(errOut, "burzblog login") {
		t.Errorf("signed in without a code: %d %q", code, errOut)
	}

	// A wrong code is refused.
	if _, errOut, code := e.run("correct horse battery\n", "login", e.url, "-u", "ada", "--code", "000000"); code == 0 || !strings.Contains(errOut, "code is not right") {
		t.Errorf("wrong code: %d %q", code, errOut)
	}

	// --code
	e.must("correct horse battery\n", "login", e.url, "-u", "ada", "--code", nextCode(secret))
	if out := e.must("", "whoami"); !strings.Contains(out, "ada") {
		t.Errorf("whoami = %q", out)
	}

	// BURZBLOG_TOTP (a recovery code works too, once)
	e.vars["BURZBLOG_TOTP"] = codes[0]
	e.must("correct horse battery\n", "login", e.url, "-u", "ada")
	if _, _, code := e.run("correct horse battery\n", "login", e.url, "-u", "ada"); code == 0 {
		t.Error("a recovery code signed in twice")
	}
	delete(e.vars, "BURZBLOG_TOTP")

	// The code on the line after the password, as a prompt would read it.
	e.must("correct horse battery\n"+codes[1]+"\n", "login", e.url, "-u", "ada")
}

func TestTokensSurviveEnablingTwoFactor(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ada")
	e.must("correct horse battery\n", "login", e.url, "-u", "ada")
	ctx := context.Background()
	svc, _ := auth.New(e.store)
	u, _ := e.store.UserByName(ctx, "ada")
	secret, _ := svc.StartTwoFactor(ctx, u, "correct horse battery")
	code, _ := auth.TOTPCode(secret, time.Now())
	if _, err := svc.ConfirmTwoFactor(ctx, u, code); err != nil {
		t.Fatal(err)
	}
	if out := e.must("", "whoami"); !strings.Contains(out, "ada") {
		t.Errorf("existing token after enabling two-factor: %q", out)
	}
}

func TestUserTwoFactorReset(t *testing.T) {
	e := newEnv(t)
	secret, _ := withTwoFactor(t, e)
	e.must("correct horse battery\n", "login", e.url, "-u", "ada", "--code", nextCode(secret))

	if _, errOut, code := e.run("", "user", "2fa-reset", "nobody"); code == 0 || !strings.Contains(errOut, "no account called nobody") {
		t.Errorf("reset unknown user: %d %q", code, errOut)
	}
	if out := e.must("", "user", "2fa-reset", "ada"); !strings.Contains(out, "Turned off two-factor") {
		t.Errorf("reset = %q", out)
	}
	if _, errOut, code := e.run("", "user", "2fa-reset", "ada"); code == 0 || !strings.Contains(errOut, "does not use two-factor") {
		t.Errorf("second reset: %d %q", code, errOut)
	}
	// The password alone signs in again.
	e.must("correct horse battery\n", "login", e.url, "-u", "ada")
}

func TestLoginWithoutTwoFactorIgnoresCode(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "ada")
	e.vars["BURZBLOG_TOTP"] = "123456"
	e.must("correct horse battery\n", "login", e.url, "-u", "ada")
}
