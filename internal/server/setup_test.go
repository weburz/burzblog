package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/store"
)

// setupHarness is a server over a store with no accounts.
type setupHarness struct {
	t      *testing.T
	ts     *httptest.Server
	store  *store.Store
	auth   *auth.Service
	logs   *bytes.Buffer
	client *http.Client
}

func newSetupHarness(t *testing.T) *setupHarness {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := auth.New(s)
	if err != nil {
		t.Fatal(err)
	}
	h := &setupHarness{t: t, store: s, auth: a, logs: &bytes.Buffer{}}
	h.ts = httptest.NewUnstartedServer(nil)
	h.ts.Start()
	srv, err := New(Config{
		Origin:  h.ts.URL,
		Logger:  slog.New(slog.NewTextHandler(h.logs, nil)),
		Panel:   fstest.MapFS{"index.html": {Data: []byte("<html><script>window.__NUXT__={}</script></html>")}},
		Version: "test",
	}, s, a)
	if err != nil {
		t.Fatal(err)
	}
	h.ts.Config.Handler = srv.Handler()
	t.Cleanup(func() { srv.Close(context.Background()); h.ts.Close() })
	jar, _ := cookiejar.New(nil)
	h.client = &http.Client{Jar: jar}
	return h
}

func (h *setupHarness) token() string {
	h.t.Helper()
	token, err := h.auth.IssueSetupToken(context.Background(), auth.SetupTTL)
	if err != nil {
		h.t.Fatal(err)
	}
	return token
}

// post sends the setup form as the panel would; origin "" omits the header.
func (h *setupHarness) post(origin string, body any) (*http.Response, map[string]any) {
	h.t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/setup", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	data, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(data, &out)
	return resp, out
}

func (h *setupHarness) form(token, name, pw string) map[string]string {
	return map[string]string{"token": token, "username": name, "password": pw}
}

func (h *setupHarness) needed() bool {
	h.t.Helper()
	resp, err := http.Get(h.ts.URL + "/api/setup")
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != 200 {
		h.t.Fatalf("GET /api/setup = %d, %v", resp.StatusCode, err)
	}
	return out["needed"]
}

func TestSetupCreatesFirstAccountAndSignsIn(t *testing.T) {
	h := newSetupHarness(t)
	if !h.needed() {
		t.Fatal("GET /api/setup needed = false on an empty server")
	}
	token := h.token()
	resp, out := h.post(h.ts.URL, h.form(token, "ada", password))
	if resp.StatusCode != 201 || out["authenticated"] != true || out["username"] != "ada" || out["csrf"] == "" {
		t.Fatalf("setup = %d %v", resp.StatusCode, out)
	}
	cookie := false
	for _, c := range resp.Cookies() {
		cookie = cookie || (c.HttpOnly && c.Value != "" && c.MaxAge > 0)
	}
	if !cookie {
		t.Error("setup did not set a session cookie")
	}
	// The cookie signs the browser in.
	sess, err := h.client.Get(h.ts.URL + "/api/session")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Body.Close()
	var got map[string]any
	_ = json.NewDecoder(sess.Body).Decode(&got)
	if got["authenticated"] != true || got["username"] != "ada" {
		t.Errorf("session after setup = %v", got)
	}
	if h.needed() {
		t.Error("GET /api/setup still says needed after the first account")
	}
	// The token is single use, and setup is closed with a generic answer.
	resp, out = h.post(h.ts.URL, h.form(token, "grace", password))
	if resp.StatusCode != http.StatusConflict || errCode(out) != "setup_done" {
		t.Errorf("second setup = %d %v, want 409 setup_done", resp.StatusCode, out)
	}
	if users, _ := h.store.Users(context.Background()); len(users) != 1 {
		t.Errorf("%d accounts, want 1", len(users))
	}
}

func TestSetupRejectsWrongExpiredAndMissingTokens(t *testing.T) {
	h := newSetupHarness(t)
	if resp, out := h.post(h.ts.URL, h.form("never-issued", "ada", password)); resp.StatusCode != 403 || errCode(out) != "setup_token" {
		t.Errorf("setup with no token issued = %d %v", resp.StatusCode, out)
	}
	token := h.token()
	resp, wrong := h.post(h.ts.URL, h.form("wrong", "ada", password))
	if resp.StatusCode != 403 || errCode(wrong) != "setup_token" {
		t.Errorf("wrong token = %d %v", resp.StatusCode, wrong)
	}
	// An expired token answers exactly like a wrong one.
	expired, err := h.auth.IssueSetupToken(context.Background(), -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resp, out := h.post(h.ts.URL, h.form(expired, "ada", password))
	if resp.StatusCode != 403 || out["error"].(map[string]any)["message"] != wrong["error"].(map[string]any)["message"] {
		t.Errorf("expired token = %d %v, want the same 403 as a wrong token", resp.StatusCode, out)
	}
	// Issuing a new token replaces the old one.
	if resp, _ := h.post(h.ts.URL, h.form(token, "ada", password)); resp.StatusCode != 403 {
		t.Errorf("a replaced token = %d, want 403", resp.StatusCode)
	}
	if !h.needed() {
		t.Error("a refused token created an account")
	}
}

func TestSetupNeedsOrigin(t *testing.T) {
	h := newSetupHarness(t)
	token := h.token()
	for _, origin := range []string{"", "https://evil.example"} {
		if resp, out := h.post(origin, h.form(token, "ada", password)); resp.StatusCode != 403 || errCode(out) != "origin" {
			t.Errorf("setup with Origin %q = %d %v, want 403 origin", origin, resp.StatusCode, out)
		}
	}
	if !h.needed() {
		t.Fatal("a foreign request created an account")
	}
	if resp, _ := h.post(h.ts.URL, h.form(token, "ada", password)); resp.StatusCode != 201 {
		t.Errorf("the token was spent by refused requests: %d", resp.StatusCode)
	}
}

func TestSetupPasswordAndNamePolicy(t *testing.T) {
	h := newSetupHarness(t)
	token := h.token()
	for _, c := range []struct{ name, pw string }{
		{"ada", "elevenchars"},
		{"ada", ""},
		{"", password},
		{"has space", password},
		{"ada\x1b", password},
	} {
		resp, out := h.post(h.ts.URL, h.form(token, c.name, c.pw))
		if resp.StatusCode != 400 || errCode(out) != "validation" {
			t.Errorf("setup(%q, %q) = %d %v, want 400 validation", c.name, c.pw, resp.StatusCode, out)
		}
	}
	if !h.needed() {
		t.Fatal("an invalid form created an account")
	}
	if resp, _ := h.post(h.ts.URL, h.form(token, "ada", "twelve chars")); resp.StatusCode != 201 {
		t.Errorf("a 12 character password = %d, want 201", resp.StatusCode)
	}
}

func TestSetupClosedAfterUserAdd(t *testing.T) {
	h := newSetupHarness(t)
	token := h.token()
	hash, _ := auth.HashPassword(password)
	if _, err := h.store.CreateUser(context.Background(), "root", hash); err != nil { // `burzblog user add`
		t.Fatal(err)
	}
	if h.needed() {
		t.Error("GET /api/setup says needed although an account exists")
	}
	resp, out := h.post(h.ts.URL, h.form(token, "ada", password))
	if resp.StatusCode != http.StatusConflict || errCode(out) != "setup_done" {
		t.Errorf("setup after user add = %d %v, want 409 setup_done", resp.StatusCode, out)
	}
	if _, err := h.store.UserByName(context.Background(), "ada"); err == nil {
		t.Error("setup created a second account")
	}
}

func TestSetupConcurrentRequestsCreateOneAccount(t *testing.T) {
	h := newSetupHarness(t)
	token := h.token()
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for _, name := range []string{"ada", "grace", "linus", "ken"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, _ := h.post(h.ts.URL, h.form(token, name, password))
			mu.Lock()
			codes[resp.StatusCode]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	users, _ := h.store.Users(context.Background())
	if codes[201] != 1 || len(users) != 1 {
		t.Errorf("responses %v and %d accounts, want exactly one 201 and one account", codes, len(users))
	}
}

func TestSetupSheddingAndLimit(t *testing.T) {
	h := newSetupHarness(t)
	token := h.token()
	for i := 0; i < 10; i++ {
		h.post(h.ts.URL, h.form("guess", "ada", password))
	}
	resp, out := h.post(h.ts.URL, h.form(token, "ada", password))
	if resp.StatusCode != http.StatusTooManyRequests || errCode(out) != "rate_limited" || resp.Header.Get("Retry-After") != "900" {
		t.Errorf("setup after ten wrong tokens = %d %v, want 429 rate_limited", resp.StatusCode, out)
	}
}

func TestSetupTokenIsNotLogged(t *testing.T) {
	h := newSetupHarness(t)
	token := h.token()
	h.post(h.ts.URL, h.form(token, "x", "short"))
	h.post(h.ts.URL, h.form("wrong"+token, "ada", password))
	h.post("", h.form(token, "ada", password))
	h.post(h.ts.URL, h.form(token, "ada", password))
	h.post(h.ts.URL, h.form(token, "ada", password))
	if strings.Contains(h.logs.String(), token) {
		t.Errorf("the setup token reached the server log:\n%s", h.logs)
	}
	if strings.Contains(h.logs.String(), password) {
		t.Errorf("the password reached the server log:\n%s", h.logs)
	}
}

func TestSetupPageGetsTheCSP(t *testing.T) {
	h := newSetupHarness(t)
	for _, path := range []string{"/setup", "/setup/", "/setup/index.html"} {
		resp, err := http.Get(h.ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), "__NUXT__") || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sha256-") {
			t.Errorf("GET %s = %d, csp %q", path, resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
		}
		if resp.Header.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("GET %s Referrer-Policy = %q", path, resp.Header.Get("Referrer-Policy"))
		}
	}
}
