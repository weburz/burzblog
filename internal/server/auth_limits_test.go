package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/store"
)

// guess sends one wrong password for user from h's browser and returns the
// status.
func (h *harness) guess(user string) int {
	h.t.Helper()
	resp, _ := h.do("POST", "/api/login", map[string]string{"username": user, "password": "wrong password!"})
	return resp.StatusCode
}

// lockOut uses up user's shared attempt budget from a browser that never
// signed in, and checks that it is then refused.
func (h *harness) lockOut(user string) {
	h.t.Helper()
	stranger := h.secondBrowser()
	for i := range 10 {
		if got := stranger.guess(user); got != 401 {
			h.t.Fatalf("guess %d for %s = %d, want 401", i+1, user, got)
		}
	}
	if got := stranger.guess(user); got != 429 {
		h.t.Fatalf("guess 11 for %s = %d, want 429", user, got)
	}
}

func TestKnownBrowserSignsInWhileOthersGuess(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/logout", nil)
	h.csrf = ""
	h.lockOut("ada")

	// A browser that never signed in shares the locked counter, even with
	// the right password.
	other := h.secondBrowser()
	if resp, out := other.do("POST", "/api/login", map[string]string{"username": "ada", "password": password}); resp.StatusCode != 429 || errCode(out) != "rate_limited" {
		t.Errorf("new browser, right password = %d %v, want 429", resp.StatusCode, out)
	}
	// The browser that signed in before still can.
	h.login()

	// Its own counter is limited as well.
	h.do("POST", "/api/logout", nil)
	h.csrf = ""
	for i := range 10 {
		if got := h.guess("ada"); got != 401 {
			t.Fatalf("known browser guess %d = %d, want 401", i+1, got)
		}
	}
	if got := h.guess("ada"); got != 429 {
		t.Errorf("known browser guess 11 = %d, want 429", got)
	}
}

func TestKnownBrowserCookieOnlyHelpsItsOwnAccount(t *testing.T) {
	h := newHarness(t, nil)
	hash, _ := auth.HashPassword(password)
	if _, err := h.store.CreateUser(context.Background(), "bob", hash, store.RoleAuthor); err != nil {
		t.Fatal(err)
	}
	h.login() // a known browser for ada, not bob
	h.do("POST", "/api/logout", nil)
	h.csrf = ""
	h.lockOut("bob")
	if resp, _ := h.do("POST", "/api/login", map[string]string{"username": "bob", "password": password}); resp.StatusCode != 429 {
		t.Errorf("bob from ada's browser = %d, want 429", resp.StatusCode)
	}
}

func TestMadeUpKnownBrowserCookieIsIgnored(t *testing.T) {
	h := newHarness(t, nil)
	h.lockOut("ada")
	forged := h.secondBrowser()
	jarURL := mustURL(h.ts.URL)
	forged.client.Jar.SetCookies(jarURL, []*http.Cookie{{Name: "burzblog_device", Value: "made-up"}})
	if resp, _ := forged.do("POST", "/api/login", map[string]string{"username": "ada", "password": password}); resp.StatusCode != 429 {
		t.Errorf("made-up cookie = %d, want 429", resp.StatusCode)
	}
}

func TestKnownBrowserCodeStepUsesItsOwnCounter(t *testing.T) {
	h := newHarness(t, nil)
	secret, _ := h.enrol()
	h.lockOut("ada")
	ch := h.passwordStep()["challenge"].(string)
	resp, out := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": nextCode(secret)})
	if resp.StatusCode != 200 || out["authenticated"] != true {
		t.Errorf("code step from the known browser = %d %v", resp.StatusCode, out)
	}
}

func TestTwoFactorConfirmIsRateLimited(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	resp, out := h.do("POST", "/api/account/2fa/start", map[string]string{"password": password})
	if resp.StatusCode != 200 {
		t.Fatalf("start = %d %v", resp.StatusCode, out)
	}
	secret := out["secret"].(string)
	right, _ := auth.TOTPCode(secret, time.Now())
	wrong := "000000"
	if wrong == right {
		wrong = "111111"
	}
	// Starting checked the right password, which clears the counter.
	for i := range 10 {
		if resp, out := h.do("POST", "/api/account/2fa/confirm", map[string]string{"code": wrong}); resp.StatusCode != 400 || errCode(out) != "code" {
			t.Fatalf("wrong code %d = %d %v, want 400 code", i+1, resp.StatusCode, out)
		}
	}
	resp, out = h.do("POST", "/api/account/2fa/confirm", map[string]string{"code": right})
	if resp.StatusCode != 429 || errCode(out) != "rate_limited" {
		t.Errorf("confirm after the limit = %d %v, want 429 rate_limited", resp.StatusCode, out)
	}
	if st, _ := h.store.TOTP(context.Background(), 1); st != nil && st.Enabled {
		t.Error("two-factor sign-in was turned on past the limit")
	}
}

func TestTwoFactorConfirmClearsTheCounter(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	_, out := h.do("POST", "/api/account/2fa/start", map[string]string{"password": password})
	secret := out["secret"].(string)
	for range 5 {
		h.do("POST", "/api/account/2fa/confirm", map[string]string{"code": "abcdef"})
	}
	code, _ := auth.TOTPCode(secret, time.Now())
	if resp, out := h.do("POST", "/api/account/2fa/confirm", map[string]string{"code": code}); resp.StatusCode != 200 {
		t.Fatalf("confirm = %d %v", resp.StatusCode, out)
	}
	h.do("POST", "/api/logout", nil)
	h.csrf = ""
	for i := range 10 {
		if got := h.guess("ada"); got != 401 {
			t.Fatalf("guess %d after enrolment = %d, want 401: the confirm did not clear the counter", i+1, got)
		}
	}
}

func TestCredentialRoutesRefuseLargeBodies(t *testing.T) {
	h := newHarness(t, nil)
	big := strings.Repeat("a", 20<<10)
	for _, path := range []string{"/api/login", "/api/login/totp", "/api/auth/token", "/api/setup"} {
		body := `{"username":"ada","password":"` + big + `"}`
		if path == "/api/login/totp" {
			body = `{"challenge":"` + big + `","code":"123456"}`
		}
		req, _ := http.NewRequest("POST", h.ts.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", h.ts.URL)
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 413 {
			t.Errorf("%s with a 20 KiB body = %d, want 413", path, resp.StatusCode)
		}
	}
	// A password of the longest allowed length still fits.
	long := strings.Repeat("a", auth.MaxPassword)
	if resp, out := h.do("POST", "/api/login", map[string]string{"username": "ada", "password": long}); resp.StatusCode != 401 || errCode(out) != "credentials" {
		t.Errorf("1 KiB password = %d %v, want 401 credentials", resp.StatusCode, out)
	}
}

func TestRefusedSignInsAreNotAudited(t *testing.T) {
	h := newHarness(t, nil)
	for _, pw := range []string{"", strings.Repeat("x", auth.MaxPassword+1)} {
		for range 20 {
			if resp, out := h.do("POST", "/api/login", map[string]string{"username": "ada", "password": pw}); resp.StatusCode != 401 || errCode(out) != "credentials" {
				t.Fatalf("password of %d bytes = %d %v, want 401", len(pw), resp.StatusCode, out)
			}
		}
	}
	if n := len(h.audit()); n != 0 {
		t.Errorf("refused sign-ins wrote %d audit entries, want 0", n)
	}
	// They did not use the budget either.
	h.login()
	if got := h.guess("ada"); got != 401 {
		t.Fatalf("wrong password = %d", got)
	}
	var failures int
	for _, e := range h.audit() {
		if e.Action == "login.failure" {
			failures++
		}
	}
	if failures != 1 {
		t.Errorf("a checked wrong password wrote %d failure entries, want 1", failures)
	}
}
