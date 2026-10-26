package server

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/store"
)

// withRoles adds an author "grace" and a second author "linus" with a token
// of his own to a harness that already has the admin "ada". It returns the id
// of linus's token.
func withRoles(t *testing.T, h *harness) int64 {
	t.Helper()
	ctx := context.Background()
	hash, _ := auth.HashPassword(password)
	if _, err := h.store.CreateUser(ctx, "grace", hash, store.RoleAuthor); err != nil {
		t.Fatal(err)
	}
	linus, err := h.store.CreateUser(ctx, "linus", hash, store.RoleAuthor)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := h.store.CreateToken(ctx, linus.ID, auth.Digest(auth.TokenPrefix+"linus"), "linus laptop", store.ScopeWrite)
	if err != nil {
		t.Fatal(err)
	}
	return tok.ID
}

// seedPosts adds a draft "hello" (version 1), a published "pub" (version 2)
// and an archived "gone" (version 2), and a renamed "renamed" (version 2)
// that redirects from "old".
func seedPosts(t *testing.T, h *harness) {
	t.Helper()
	ctx := context.Background()
	for _, slug := range []string{"hello", "pub", "gone", "old"} {
		if _, err := h.store.CreatePost(ctx, &post.Post{Slug: slug, Title: slug, Status: post.Draft}, "ada"); err != nil {
			t.Fatal(err)
		}
	}
	change := func(slug string, f func(*post.Post)) {
		if _, err := h.store.UpdatePost(ctx, slug, 1, "ada", func(p *post.Post) error { f(p); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	change("pub", func(p *post.Post) { p.Status = post.Published })
	change("gone", func(p *post.Post) { p.Status = post.Archived })
	change("old", func(p *post.Post) { p.Slug = "renamed" })
}

// signInAs makes the harness act as user, with a browser session or a bearer
// token.
func (h *harness) signInAs(user string, viaToken bool) {
	h.t.Helper()
	jar, _ := cookiejar.New(nil)
	h.client = &http.Client{Jar: jar}
	h.csrf, h.token = "", ""
	if viaToken {
		u, err := h.store.UserByName(context.Background(), user)
		if err != nil {
			h.t.Fatal(err)
		}
		secret := auth.TokenPrefix + "test-" + user
		if _, err := h.store.CreateToken(context.Background(), u.ID, auth.Digest(secret), "test", store.ScopeWrite); err != nil {
			h.t.Fatal(err)
		}
		h.token = secret
		return
	}
	resp, out := h.do("POST", "/api/login", map[string]string{"username": user, "password": password})
	if resp.StatusCode != 200 {
		h.t.Fatalf("sign in %s = %d %v", user, resp.StatusCode, out)
	}
	h.csrf = out["csrf"].(string)
}

// markdownDoc is a frontmatter document for the markdown endpoints.
const markdownDoc = "---\ntitle: Hello\n---\n\nWords.\n"

// send is do with extra request headers, and the raw response body.
func (h *harness) send(method, path, body string, headers map[string]string) (int, string) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.ts.URL+path, strings.NewReader(body))
	if body != "" && !strings.HasPrefix(body, "---") {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", h.ts.URL)
	if h.csrf != "" {
		req.Header.Set("X-CSRF-Token", h.csrf)
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

type routeCase struct {
	name, method, path, body string
	headers                  map[string]string
	// adminOnly routes answer an author 403 with the code "role".
	adminOnly bool
	// want is the status an allowed caller gets.
	want int
}

func routeCases(otherToken int64) []routeCase {
	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	return []routeCase{
		// Admin only.
		{name: "settings", method: "GET", path: "/api/settings", adminOnly: true, want: 200},
		{name: "deploy", method: "POST", path: "/api/deploy", body: "{}", adminOnly: true, want: 409},
		{name: "audit log", method: "GET", path: "/api/audit", adminOnly: true, want: 200},
		{name: "delete post", method: "DELETE", path: "/api/posts/gone?version=2", adminOnly: true, want: 200},
		{name: "backup", method: "GET", path: "/api/backup.tar.gz", adminOnly: true, want: 200},
		{name: "export", method: "GET", path: "/api/export.tar.gz", adminOnly: true, want: 200},
		{name: "export token", method: "POST", path: "/api/tokens", body: `{"name":"build","scope":"export"}`, adminOnly: true, want: 201},
		{name: "revoke someone else's token", method: "DELETE", path: "/api/tokens/{other}", adminOnly: true, want: 200},
		{name: "delete upload", method: "DELETE", path: "/api/media/999", adminOnly: true, want: 404},
		// Every role.
		{name: "write token", method: "POST", path: "/api/tokens", body: `{"name":"laptop","scope":"write"}`, want: 201},
		{name: "list tokens", method: "GET", path: "/api/tokens", want: 200},
		{name: "change password", method: "POST", path: "/api/account/password", body: `{"current":"` + password + `","next":"another long passphrase"}`, want: 200},
		{name: "2fa status", method: "GET", path: "/api/account/2fa", want: 200},
		{name: "2fa start", method: "POST", path: "/api/account/2fa/start", body: `{"password":"` + password + `"}`, want: 200},
		{name: "session", method: "GET", path: "/api/session", want: 200},
		{name: "list posts", method: "GET", path: "/api/posts", want: 200},
		{name: "list tags", method: "GET", path: "/api/tags", want: 200},
		{name: "list uploads", method: "GET", path: "/api/media", want: 200},
		{name: "create post", method: "POST", path: "/api/posts", body: `{"title":"Fresh"}`, want: 201},
		{name: "get post", method: "GET", path: "/api/posts/hello", want: 200},
		{name: "save post", method: "PUT", path: "/api/posts/hello", body: `{"version":1,"title":"Changed"}`, want: 200},
		{name: "publish", method: "POST", path: "/api/posts/hello/publish", body: `{"version":1}`, want: 200},
		{name: "unpublish", method: "POST", path: "/api/posts/pub/unpublish", body: `{"version":2}`, want: 200},
		{name: "archive", method: "POST", path: "/api/posts/pub/archive", body: `{"version":2}`, want: 200},
		{name: "restore", method: "POST", path: "/api/posts/gone/restore", body: `{"version":2}`, want: 200},
		{name: "schedule", method: "POST", path: "/api/posts/hello/schedule", body: `{"version":1,"at":"` + future + `"}`, want: 200},
		{name: "get markdown", method: "GET", path: "/api/posts/hello/markdown", want: 200},
		{name: "put markdown", method: "PUT", path: "/api/posts/hello/markdown", body: markdownDoc, headers: map[string]string{"If-Match": "1"}, want: 200},
		{name: "revisions", method: "GET", path: "/api/posts/pub/revisions", want: 200},
		{name: "restore revision", method: "POST", path: "/api/posts/pub/revisions/1/restore", body: `{"version":2}`, want: 200},
		{name: "list redirects", method: "GET", path: "/api/posts/renamed/redirects", want: 200},
		{name: "delete redirect", method: "DELETE", path: "/api/posts/renamed/redirects/old", want: 200},
		{name: "preview", method: "POST", path: "/api/preview", body: `{"markdown":"hi"}`, want: 200},
		{name: "content list", method: "GET", path: "/api/content/posts", want: 200},
	}
}

func TestRoleMatrix(t *testing.T) {
	for _, viaToken := range []bool{false, true} {
		for _, role := range []string{store.RoleAuthor, store.RoleAdmin} {
			user := map[string]string{store.RoleAuthor: "grace", store.RoleAdmin: "ada"}[role]
			mode := "session"
			if viaToken {
				mode = "token"
			}
			for _, tc := range routeCases(0) {
				t.Run(mode+"/"+role+"/"+tc.name, func(t *testing.T) {
					h := newHarness(t, nil)
					other := withRoles(t, h)
					seedPosts(t, h)
					h.signInAs(user, viaToken)
					path := strings.ReplaceAll(tc.path, "{other}", itoa(other))
					status, raw := h.send(tc.method, path, tc.body, tc.headers)
					if role == store.RoleAuthor && tc.adminOnly {
						if status != 403 || !strings.Contains(raw, `"code":"role"`) {
							t.Errorf("author %s %s = %d %s; want 403 role", tc.method, tc.path, status, raw)
						}
						return
					}
					if status != tc.want {
						t.Errorf("%s %s as %s = %d %.200s; want %d", tc.method, tc.path, role, status, raw, tc.want)
					}
				})
			}
		}
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// An admin-only route must refuse an author before it looks at the request,
// so a nonsense body cannot tell an author anything.
func TestAuthorIsRefusedBeforeTheRequestIsRead(t *testing.T) {
	h := newHarness(t, nil)
	withRoles(t, h)
	h.signInAs("grace", false)
	status, raw := h.send("DELETE", "/api/posts/nothing-here", "", nil)
	if status != 403 || !strings.Contains(raw, `"code":"role"`) {
		t.Errorf("delete as author = %d %s", status, raw)
	}
	status, _ = h.send("GET", "/api/audit?limit=nonsense", "", nil)
	if status != 403 {
		t.Errorf("audit as author with a bad limit = %d, want 403", status)
	}
	h.signInAs("ada", false)
	if status, _ := h.send("GET", "/api/audit?limit=nonsense", "", nil); status != 422 {
		t.Errorf("audit as admin with a bad limit = %d, want 422", status)
	}
}

func TestAnonymousCannotReachRoleRoutes(t *testing.T) {
	h := newHarness(t, nil)
	for _, route := range []string{"GET /api/audit", "GET /api/settings", "POST /api/deploy", "GET /api/backup.tar.gz", "GET /api/export.tar.gz"} {
		parts := strings.SplitN(route, " ", 2)
		if status, raw := h.send(parts[0], parts[1], "", nil); status != 401 {
			t.Errorf("%s anonymous = %d %s; want 401", route, status, raw)
		}
	}
}

func TestAuthorSeesOnlyOwnTokens(t *testing.T) {
	h := newHarness(t, nil)
	other := withRoles(t, h)
	h.signInAs("grace", false)
	_, out := h.do("POST", "/api/tokens", map[string]string{"name": "mine", "scope": "write"})
	mine := int64(out["id"].(float64))

	_, out = h.do("GET", "/api/tokens", nil)
	items := out["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["name"] != "mine" {
		t.Errorf("author's token list = %v", items)
	}
	if resp, out := h.do("DELETE", "/api/tokens/"+itoa(other), nil); resp.StatusCode != 403 || errCode(out) != "role" {
		t.Errorf("revoke another author's token = %d %v", resp.StatusCode, out)
	}
	if resp, _ := h.do("DELETE", "/api/tokens/"+itoa(mine), nil); resp.StatusCode != 200 {
		t.Errorf("revoke own token = %d", resp.StatusCode)
	}
	if resp, out := h.do("DELETE", "/api/tokens/"+itoa(mine), nil); resp.StatusCode != 404 {
		t.Errorf("revoke twice = %d %v", resp.StatusCode, out)
	}

	h.signInAs("ada", false)
	_, out = h.do("GET", "/api/tokens", nil)
	if n := len(out["items"].([]any)); n != 1 { // linus's; grace's was revoked
		t.Errorf("admin's token list has %d items, want 1 (all accounts')", n)
	}
	if resp, _ := h.do("DELETE", "/api/tokens/"+itoa(other), nil); resp.StatusCode != 200 {
		t.Errorf("admin revoking another's token = %d", resp.StatusCode)
	}
}

func TestSessionReportsRole(t *testing.T) {
	h := newHarness(t, nil)
	withRoles(t, h)
	for user, want := range map[string]string{"ada": "admin", "grace": "author"} {
		h.signInAs(user, false)
		if _, out := h.do("GET", "/api/session", nil); out["role"] != want {
			t.Errorf("%s session role = %v, want %s", user, out["role"], want)
		}
	}
}

func TestTokensInheritTheirOwnersRole(t *testing.T) {
	h := newHarness(t, nil)
	withRoles(t, h)
	ctx := context.Background()
	grace, _ := h.store.UserByName(ctx, "grace")
	h.store.CreateToken(ctx, grace.ID, auth.Digest(auth.TokenPrefix+"grace-export"), "build", store.ScopeExport)
	ada, _ := h.store.UserByName(ctx, "ada")
	h.store.CreateToken(ctx, ada.ID, auth.Digest(auth.TokenPrefix+"ada-export"), "build", store.ScopeExport)

	h.signInAs("ada", false)
	h.client = &http.Client{}
	h.csrf = ""
	for secret, want := range map[string]int{"grace-export": 403, "ada-export": 200} {
		h.token = auth.TokenPrefix + secret
		if status, raw := h.send("GET", "/api/export.tar.gz", "", nil); status != want {
			t.Errorf("export with %s = %d %.100s; want %d", secret, status, raw, want)
		}
		// The content API is public content: any role's token reads it.
		if status, _ := h.send("GET", "/api/content/posts", "", nil); status != 200 {
			t.Errorf("content with %s = %d", secret, status)
		}
	}

	// Demoting takes effect on the next request, for sessions and tokens alike.
	h.signInAs("ada", true)
	if status, _ := h.send("GET", "/api/audit", "", nil); status != 200 {
		t.Fatalf("audit as admin token = %d", status)
	}
	h.store.SetRole(ctx, grace.ID, store.RoleAdmin)
	if _, err := h.store.SetRole(ctx, ada.ID, store.RoleAuthor); err != nil {
		t.Fatal(err)
	}
	if status, _ := h.send("GET", "/api/audit", "", nil); status != 403 {
		t.Errorf("audit after demotion = %d, want 403", status)
	}
}

func TestAuditEndpointPages(t *testing.T) {
	h := newHarness(t, nil)
	h.signInAs("ada", false)
	for i := 0; i < 5; i++ {
		h.do("POST", "/api/tokens", map[string]string{"name": "t", "scope": "write"})
	}
	_, out := h.do("GET", "/api/audit?limit=2&offset=1", nil)
	items := out["items"].([]any)
	// 1 sign-in + 5 tokens.
	if len(items) != 2 || out["total"] != 6.0 {
		t.Fatalf("page = %d items of %v", len(items), out["total"])
	}
	first := items[0].(map[string]any)
	if first["action"] != "token.create" || first["actor"] != "ada" || first["remote"] != "127.0.0.1" || first["at"] == "" {
		t.Errorf("entry = %v", first)
	}
	if first["id"].(float64) <= items[1].(map[string]any)["id"].(float64) {
		t.Error("entries are not newest first")
	}
	if resp, out := h.do("GET", "/api/audit?limit=0", nil); resp.StatusCode != 422 {
		t.Errorf("limit=0 = %d %v", resp.StatusCode, out)
	}
	_, out = h.do("GET", "/api/audit", nil)
	if len(out["items"].([]any)) != 6 {
		t.Errorf("default page = %d items", len(out["items"].([]any)))
	}
}

// actions returns the audit actions newest last, with their actor and target.
func (h *harness) audit() []store.AuditEntry {
	h.t.Helper()
	entries, _, err := h.store.AuditLog(context.Background(), 1000, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}

func TestAuditRecordsEveryAction(t *testing.T) {
	var hits int
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer hook.Close()
	deployDelay = time.Millisecond
	h := newHarness(t, func(c *Config) { c.DeployHook = hook.URL })
	withRoles(t, h)
	seedPosts(t, h)

	// A wrong password for a real account is recorded, one for a made-up
	// account is not.
	h.do("POST", "/api/login", map[string]string{"username": "grace", "password": "not the password"})
	h.do("POST", "/api/login", map[string]string{"username": "nobody", "password": "not the password"})
	h.signInAs("grace", false)
	_, out := h.do("POST", "/api/tokens", map[string]string{"name": "laptop", "scope": "write"})
	token := int64(out["id"].(float64))
	h.do("POST", "/api/posts/hello/publish", map[string]any{"version": 1})
	h.do("POST", "/api/posts/hello/unpublish", map[string]any{"version": 2})
	h.do("POST", "/api/posts/hello/archive", map[string]any{"version": 3})
	h.do("POST", "/api/posts/hello/restore", map[string]any{"version": 4})
	h.do("POST", "/api/posts/hello/schedule", map[string]any{"version": 5, "at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)})
	h.do("POST", "/api/posts/hello/unschedule", map[string]any{"version": 6})
	h.do("DELETE", "/api/posts/renamed/redirects/old", nil)
	h.do("DELETE", "/api/posts/gone?version=2", nil) // refused: not an admin
	h.do("DELETE", "/api/tokens/"+itoa(token), nil)
	h.do("POST", "/api/account/password", map[string]string{"current": password, "next": "a brand new passphrase"})

	h.signInAs("ada", false)
	h.do("DELETE", "/api/posts/gone?version=2", nil)
	h.do("POST", "/api/deploy", map[string]any{})
	h.do("GET", "/api/backup.tar.gz", nil)

	want := []struct{ actor, action, target string }{
		{"grace", "login.failure", "password"},
		{"grace", "login.success", ""},
		{"grace", "token.create", "laptop (token %d, write)"},
		{"grace", "post.publish", "hello"},
		{"grace", "post.unpublish", "hello"},
		{"grace", "post.archive", "hello"},
		{"grace", "post.restore", "hello"},
		{"grace", "post.schedule", "hello"},
		{"grace", "post.unschedule", "hello"},
		{"grace", "redirect.delete", "old to renamed"},
		{"grace", "token.revoke", "laptop (token %d, write) of grace"},
		{"grace", "password.change", ""},
		{"ada", "login.success", ""},
		{"ada", "post.delete", "gone"},
		{"ada", "deploy", ""},
		{"ada", "backup.download", ""},
	}
	got := h.audit()
	if len(got) != len(want) {
		for _, e := range got {
			t.Logf("%s %s %q", e.ActorName, e.Action, e.Target)
		}
		t.Fatalf("%d audit entries, want %d", len(got), len(want))
	}
	for i, w := range want {
		target := strings.ReplaceAll(w.target, "%d", itoa(token))
		e := got[i]
		if e.ActorName != w.actor || e.Action != w.action || e.Target != target || e.Remote != "127.0.0.1" || e.At.IsZero() {
			t.Errorf("entry %d = %s %s %q from %q; want %s %s %q", i, e.ActorName, e.Action, e.Target, e.Remote, w.actor, w.action, target)
		}
		if e.ActorUserID == 0 {
			t.Errorf("entry %d has no actor id", i)
		}
	}
}

func TestAuditRecordsTokenActors(t *testing.T) {
	h := newHarness(t, nil)
	withRoles(t, h)
	seedPosts(t, h)
	h.signInAs("grace", true)
	h.do("POST", "/api/posts/hello/publish", map[string]any{"version": 1})
	grace, _ := h.store.UserByName(context.Background(), "grace")
	tokens, _ := h.store.UserTokens(context.Background(), grace.ID)

	got := h.audit()
	last := got[len(got)-1]
	if last.Action != "post.publish" || last.TokenID != tokens[0].ID || last.ActorName != "grace" {
		t.Errorf("entry = %+v; want publish by grace with token %d", last, tokens[0].ID)
	}
}

func TestAuditRecordsTerminalSignInAndTwoFactor(t *testing.T) {
	h := newHarness(t, nil)
	secret, codes := h.enrol()

	// Terminal login: a wrong code, then a recovery code.
	body := map[string]string{"username": "ada", "password": password, "name": "CLI", "code": "000000"}
	if resp, _ := h.do("POST", "/api/auth/token", body); resp.StatusCode != 401 {
		t.Fatalf("wrong code = %d", resp.StatusCode)
	}
	body["code"] = codes[0]
	if resp, out := h.do("POST", "/api/auth/token", body); resp.StatusCode != 201 {
		t.Fatalf("recovery code login = %d %v", resp.StatusCode, out)
	}
	// Browser login with a TOTP code, then turning it off with a recovery code.
	ch := h.passwordStep()["challenge"].(string)
	if resp, _ := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": "000000"}); resp.StatusCode != 401 {
		t.Fatal("wrong browser code accepted")
	}
	_, out := h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": nextCode(secret)})
	h.csrf = out["csrf"].(string)
	if resp, out := h.do("POST", "/api/account/2fa/disable", map[string]string{"password": password, "code": codes[1]}); resp.StatusCode != 200 {
		t.Fatalf("disable = %d %v", resp.StatusCode, out)
	}

	var actions []string
	for _, e := range h.audit() {
		actions = append(actions, e.Action+"|"+e.Target)
	}
	want := []string{
		"login.success|", // enrolment signed in without a code
		"2fa.enable|",
		"login.failure|second factor",
		"2fa.recovery|",
		"login.success|terminal",
		"token.create|CLI (token 1, write)",
		"login.failure|second factor",
		"login.success|",
		"2fa.recovery|",
		"2fa.disable|",
	}
	if strings.Join(actions, "\n") != strings.Join(want, "\n") {
		t.Errorf("actions:\n%s\nwant:\n%s", strings.Join(actions, "\n"), strings.Join(want, "\n"))
	}
}

func TestSchedulerPublishIsAudited(t *testing.T) {
	h, c := scheduleHarness(t, nil)
	slug := h.draft("Timed")
	h.schedule(slug, 1, c.Now().Add(time.Hour))
	c.advance(2 * time.Hour)
	if n, _, err := h.srv.publishDue(context.Background()); n != 1 || err != nil {
		t.Fatalf("publishDue = %d, %v", n, err)
	}
	got := h.audit()
	last := got[len(got)-1]
	if last.ActorName != "scheduler" || last.Action != "post.publish" || last.Target != slug {
		t.Errorf("last entry = %+v", last)
	}
}

// Nothing secret may reach the audit log: passwords, token secrets, the
// deploy hook URL, two-factor secrets and recovery codes.
func TestAuditNeverHoldsSecrets(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer hook.Close()
	hookURL := hook.URL + "/deploy/very-secret-hook-key"
	deployDelay = time.Millisecond
	h := newHarness(t, func(c *Config) { c.DeployHook = hookURL })
	secret, codes := h.enrol()
	next := "the next long passphrase"

	h.do("POST", "/api/login", map[string]string{"username": "ada", "password": "wrong wrong wrong"})
	body := map[string]string{"username": "ada", "password": password, "name": "CLI", "code": codes[2]}
	_, out := h.do("POST", "/api/auth/token", body)
	secrets := []string{password, next, "wrong wrong wrong", secret, hookURL, "very-secret-hook-key", out["secret"].(string)}
	secrets = append(secrets, codes...)

	ch := h.passwordStep()["challenge"].(string)
	_, out = h.do("POST", "/api/login/totp", map[string]string{"challenge": ch, "code": nextCode(secret)})
	h.csrf = out["csrf"].(string)
	_, out = h.do("POST", "/api/tokens", map[string]string{"name": "build", "scope": "export"})
	secrets = append(secrets, out["secret"].(string))
	h.do("POST", "/api/deploy", map[string]any{})
	h.do("POST", "/api/account/password", map[string]string{"current": password, "next": next})
	h.do("POST", "/api/account/2fa/start", map[string]string{"password": next})
	_, settings := h.do("GET", "/api/settings", nil)
	if strings.Contains(settings["_raw"].(string), "very-secret-hook-key") {
		t.Fatal("the settings reply leaks the deploy hook")
	}

	_, audit := h.do("GET", "/api/audit", nil)
	raw := audit["_raw"].(string)
	if len(h.audit()) < 6 {
		t.Fatalf("only %d audit entries; the flow did not run: %s", len(h.audit()), raw)
	}
	for _, s := range secrets {
		if s != "" && strings.Contains(raw, s) {
			t.Errorf("the audit log holds a secret: %q", s)
		}
	}
	// And the rows themselves, not just their JSON.
	for _, e := range h.audit() {
		row := e.ActorName + " " + e.Action + " " + e.Target + " " + e.Remote
		for _, s := range secrets {
			if s != "" && strings.Contains(row, s) {
				t.Errorf("audit row %q holds a secret: %q", row, s)
			}
		}
	}
}
