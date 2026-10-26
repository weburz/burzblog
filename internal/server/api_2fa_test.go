package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/auth"
)

// enrol turns two-factor sign-in on for ada through the API and returns the
// secret and recovery codes. It leaves the harness signed out.
func (h *harness) enrol() (string, []string) {
	h.t.Helper()
	h.login()
	resp, out := h.do("POST", "/api/account/2fa/start", map[string]string{"password": password})
	if resp.StatusCode != 200 {
		h.t.Fatalf("start = %d %v", resp.StatusCode, out)
	}
	secret := out["secret"].(string)
	if uri := out["uri"].(string); !strings.HasPrefix(uri, "otpauth://totp/BurzBlog:ada@") || !strings.Contains(uri, "secret="+secret) {
		h.t.Errorf("uri = %q", uri)
	}
	code, _ := auth.TOTPCode(secret, time.Now())
	resp, out = h.do("POST", "/api/account/2fa/confirm", map[string]string{"code": code})
	if resp.StatusCode != 200 {
		h.t.Fatalf("confirm = %d %v", resp.StatusCode, out)
	}
	var codes []string
	for _, c := range out["recoveryCodes"].([]any) {
		codes = append(codes, c.(string))
	}
	h.do("POST", "/api/logout", nil)
	h.csrf = ""
	return secret, codes
}

// nextCode is a code newer than anything enrolment used, still inside the
// accepted window.
func nextCode(secret string) string {
	code, _ := auth.TOTPCode(secret, time.Now().Add(30*time.Second))
	return code
}

func (h *harness) passwordStep() map[string]any {
	h.t.Helper()
	resp, out := h.do("POST", "/api/login", map[string]string{"username": "ada", "password": password})
	if resp.StatusCode != 200 || out["second_factor"] != true || out["challenge"] == "" {
		h.t.Fatalf("password step = %d %v", resp.StatusCode, out)
	}
	if len(h.client.Jar.Cookies(mustURL(h.ts.URL))) != 0 {
		h.t.Fatal("a session cookie was set before the code")
	}
	return out
}

func TestTwoFactorSignIn(t *testing.T) {
	h := newHarness(t, nil)
	secret, codes := h.enrol()
	if len(codes) != 10 {
		t.Fatalf("got %d recovery codes", len(codes))
	}

	// The password alone gives no session.
	ch := h.passwordStep()["challenge"].(string)
	if resp, _ := h.do("GET", "/api/posts", nil); resp.StatusCode != 401 {
		t.Errorf("posts after the password step = %d, want 401", resp.StatusCode)
	}

	resp, out := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": "000000"})
	if resp.StatusCode != 401 || errCode(out) != "code" {
		t.Errorf("wrong code = %d %v", resp.StatusCode, out)
	}
	resp, out = h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": nextCode(secret)})
	if resp.StatusCode != 200 || out["authenticated"] != true || out["csrf"] == "" {
		t.Fatalf("right code = %d %v", resp.StatusCode, out)
	}
	h.csrf = out["csrf"].(string)
	if resp, _ := h.do("GET", "/api/posts", nil); resp.StatusCode != 200 {
		t.Errorf("posts after sign-in = %d", resp.StatusCode)
	}

	// The challenge is spent.
	h.do("POST", "/api/logout", nil)
	resp, out = h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": codes[0]})
	if resp.StatusCode != 401 || errCode(out) != "challenge" {
		t.Errorf("reused challenge = %d %v", resp.StatusCode, out)
	}
}

func TestTwoFactorRecoveryCodeSignsInOnce(t *testing.T) {
	h := newHarness(t, nil)
	_, codes := h.enrol()
	ch := h.passwordStep()["challenge"].(string)
	if resp, out := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": codes[3]}); resp.StatusCode != 200 {
		t.Fatalf("recovery code = %d %v", resp.StatusCode, out)
	}
	h.do("POST", "/api/logout", nil)
	ch = h.passwordStep()["challenge"].(string)
	if resp, out := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": codes[3]}); resp.StatusCode != 401 || errCode(out) != "code" {
		t.Errorf("recovery code reused = %d %v", resp.StatusCode, out)
	}
}

func TestTwoFactorCodesAreRateLimited(t *testing.T) {
	h := newHarness(t, nil)
	secret, _ := h.enrol()
	ch := h.passwordStep()["challenge"].(string)
	var last int
	for i := 0; i < 12; i++ {
		resp, _ := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": "000000"})
		last = resp.StatusCode
	}
	if last != 429 {
		t.Errorf("after many wrong codes = %d, want 429", last)
	}
	// The right code is refused while locked, and so is the password.
	if resp, _ := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": nextCode(secret)}); resp.StatusCode != 429 {
		t.Errorf("right code while locked = %d, want 429", resp.StatusCode)
	}
	if resp, _ := h.do("POST", "/api/login", map[string]string{"username": "ada", "password": password}); resp.StatusCode != 429 {
		t.Errorf("password while locked = %d, want 429", resp.StatusCode)
	}
}

func TestTwoFactorChallengeExpires(t *testing.T) {
	h := newHarness(t, nil)
	secret, _ := h.enrol()
	ch := h.passwordStep()["challenge"].(string)
	h.store.SetClock(func() time.Time { return time.Now().Add(auth.ChallengeTTL + time.Minute) })
	resp, out := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": nextCode(secret)})
	if resp.StatusCode != 401 || errCode(out) != "challenge" {
		t.Errorf("expired challenge = %d %v", resp.StatusCode, out)
	}
}

func TestTwoFactorLoginIsOriginChecked(t *testing.T) {
	h := newHarness(t, nil)
	h.enrol()
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/login/totp", strings.NewReader(`{"challenge":"x","code":"000000"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := h.client.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("foreign origin = %d, want 403", resp.StatusCode)
	}
}

func TestWrongPasswordLooksTheSameWith2FA(t *testing.T) {
	h := newHarness(t, nil)
	h.enrol()
	resp, out := h.do("POST", "/api/login", map[string]string{"username": "ada", "password": "nope nope nope"})
	resp2, out2 := h.do("POST", "/api/login", map[string]string{"username": "ghost", "password": "nope nope nope"})
	if resp.StatusCode != 401 || resp2.StatusCode != 401 || out["_raw"] != out2["_raw"] || errCode(out) != "credentials" {
		t.Errorf("wrong password = %d %v; unknown user = %d %v", resp.StatusCode, out["_raw"], resp2.StatusCode, out2["_raw"])
	}
}

func TestTwoFactorSettingsFlow(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	if _, out := h.do("GET", "/api/account/2fa", nil); out["enabled"] != false || out["pending"] != false {
		t.Errorf("initial status = %v", out)
	}
	if resp, out := h.do("POST", "/api/account/2fa/start", map[string]string{"password": "wrong wrong wrong"}); resp.StatusCode != 400 || errCode(out) != "credentials" {
		t.Errorf("start with wrong password = %d %v", resp.StatusCode, out)
	}
	_, start := h.do("POST", "/api/account/2fa/start", map[string]string{"password": password})
	secret := start["secret"].(string)
	if _, out := h.do("GET", "/api/account/2fa", nil); out["pending"] != true || out["enabled"] != false {
		t.Errorf("status after start = %v", out)
	}
	if resp, out := h.do("POST", "/api/account/2fa/confirm", map[string]string{"code": "000000"}); resp.StatusCode != 400 || errCode(out) != "code" {
		t.Errorf("confirm with wrong code = %d %v", resp.StatusCode, out)
	}
	code, _ := auth.TOTPCode(secret, time.Now())
	resp, confirmed := h.do("POST", "/api/account/2fa/confirm", map[string]string{"code": code})
	if resp.StatusCode != 200 || confirmed["enabled"] != true {
		t.Fatalf("confirm = %d %v", resp.StatusCode, confirmed)
	}
	recovery := confirmed["recoveryCodes"].([]any)[0].(string)
	if _, out := h.do("GET", "/api/account/2fa", nil); out["enabled"] != true || out["recoveryCodes"] != float64(10) {
		t.Errorf("status after confirm = %v", out)
	}
	if resp, out := h.do("POST", "/api/account/2fa/start", map[string]string{"password": password}); resp.StatusCode != 409 || errCode(out) != "two_factor_on" {
		t.Errorf("start while on = %d %v", resp.StatusCode, out)
	}

	// Disabling needs the password and a code, and ends the other sessions.
	other := newHarnessSharing(t, h)
	other.login2FA(recovery)
	if resp, _ := other.do("GET", "/api/posts", nil); resp.StatusCode != 200 {
		t.Fatalf("second browser = %d", resp.StatusCode)
	}
	dis := map[string]string{"password": password, "code": "000000"}
	if resp, out := h.do("POST", "/api/account/2fa/disable", dis); resp.StatusCode != 401 || errCode(out) != "code" {
		t.Errorf("disable with wrong code = %d %v", resp.StatusCode, out)
	}
	dis = map[string]string{"password": "wrong wrong wrong", "code": nextCode(secret)}
	if resp, out := h.do("POST", "/api/account/2fa/disable", dis); resp.StatusCode != 400 || errCode(out) != "credentials" {
		t.Errorf("disable with wrong password = %d %v", resp.StatusCode, out)
	}
	dis = map[string]string{"password": password, "code": nextCode(secret)}
	if resp, out := h.do("POST", "/api/account/2fa/disable", dis); resp.StatusCode != 200 {
		t.Fatalf("disable = %d %v", resp.StatusCode, out)
	}
	if resp, _ := h.do("GET", "/api/posts", nil); resp.StatusCode != 200 {
		t.Errorf("current session after disable = %d, want 200", resp.StatusCode)
	}
	if resp, _ := other.do("GET", "/api/posts", nil); resp.StatusCode != 401 {
		t.Errorf("other session after disable = %d, want 401", resp.StatusCode)
	}
	if resp, out := h.do("POST", "/api/account/2fa/disable", dis); resp.StatusCode != 409 {
		t.Errorf("disable while off = %d %v", resp.StatusCode, out)
	}
}

// newHarnessSharing returns a second browser against the same server.
func newHarnessSharing(t *testing.T, h *harness) *harness {
	t.Helper()
	other := *h
	jar, _ := cookiejar.New(nil)
	other.client = &http.Client{Jar: jar}
	other.csrf = ""
	return &other
}

// login2FA signs in through both steps with code.
func (h *harness) login2FA(code string) {
	h.t.Helper()
	ch := h.passwordStep()["challenge"].(string)
	resp, out := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": code})
	if resp.StatusCode != 200 {
		h.t.Fatalf("second step = %d %v", resp.StatusCode, out)
	}
	h.csrf = out["csrf"].(string)
}

func TestAccountsWithoutTwoFactorSignInAsBefore(t *testing.T) {
	h := newHarness(t, nil)
	resp, out := h.do("POST", "/api/login", map[string]string{"username": "ada", "password": password})
	if resp.StatusCode != 200 || out["authenticated"] != true || out["second_factor"] != nil {
		t.Errorf("login = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/login/totp", map[string]string{"challenge": "x", "code": "000000"}); resp.StatusCode != 401 || errCode(out) != "challenge" {
		t.Errorf("totp step without a challenge = %d %v", resp.StatusCode, out)
	}
}

func TestCLITokenNeedsCodeWhenTwoFactorIsOn(t *testing.T) {
	h := newHarness(t, nil)
	secret, codes := h.enrol()
	body := func(code string) map[string]string {
		return map[string]string{"username": "ada", "password": password, "name": "CLI", "code": code}
	}
	resp, out := h.do("POST", "/api/auth/token", body(""))
	if resp.StatusCode != 401 || errCode(out) != "second_factor_required" || out["secret"] != nil {
		t.Errorf("without a code = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/auth/token", body("000000")); resp.StatusCode != 401 || errCode(out) != "code" {
		t.Errorf("wrong code = %d %v", resp.StatusCode, out)
	}
	resp, out = h.do("POST", "/api/auth/token", body(nextCode(secret)))
	if resp.StatusCode != 201 || out["secret"] == nil {
		t.Fatalf("right code = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/auth/token", body(codes[0])); resp.StatusCode != 201 {
		t.Errorf("recovery code = %d %v", resp.StatusCode, out)
	}

	// A token issued earlier keeps working with 2FA on: it is the factor.
	h.token = out["secret"].(string)
	if resp, _ := h.do("GET", "/api/posts", nil); resp.StatusCode != 200 {
		t.Errorf("token after enrolment = %d", resp.StatusCode)
	}
}

func TestEnrolmentKeepsExistingTokens(t *testing.T) {
	h := newHarness(t, nil)
	_, out := h.do("POST", "/api/auth/token", map[string]string{"username": "ada", "password": password, "name": "CLI"})
	token := out["secret"].(string)
	h.enrol()
	h.token = token
	if resp, _ := h.do("GET", "/api/posts", nil); resp.StatusCode != 200 {
		t.Errorf("token issued before enrolment = %d, want 200", resp.StatusCode)
	}
}

func TestResetFromTheHostEndsSessions(t *testing.T) {
	h := newHarness(t, nil)
	secret, _ := h.enrol()
	h.login2FA(nextCode(secret))
	ctx := context.Background()
	u, err := h.store.UserByName(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.DisableTOTP(ctx, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if resp, _ := h.do("GET", "/api/posts", nil); resp.StatusCode != 401 {
		t.Errorf("session after host reset = %d, want 401", resp.StatusCode)
	}
	h.csrf = ""
	h.login() // the password alone signs in again
}

func TestWrongOriginNamesTheConfiguredOne(t *testing.T) {
	h := newHarness(t, nil)
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/login", strings.NewReader(`{"username":"a","password":"b"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:1")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	msg, _ := out["error"].(map[string]any)["message"].(string)
	if resp.StatusCode != 403 || errCode(out) != "origin" || !strings.Contains(msg, "Open BurzBlog at "+h.ts.URL) {
		t.Errorf("wrong origin = %d %q, want 403 naming %s", resp.StatusCode, msg, h.ts.URL)
	}
}
