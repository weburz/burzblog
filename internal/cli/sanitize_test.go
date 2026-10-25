package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"plain":                    "plain",
		"Shipping 🚀 مرحبا Café":   "Shipping 🚀 مرحبا Café",
		"a\x1b]52;c;ZXZpbA==\x07b": "a�]52;c;ZXZpbA==�b",
		"a\x1b[2Jb":                "a�[2Jb",
		"tab\there":                "tab�here",
		"line\nbreak\r":            "line�break�",
		"c1\u009b31m":              "c1�31m",
		"del\x7f":                  "del�",
	}
	for in, want := range cases {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanLinesKeepsLineBreaks(t *testing.T) {
	if got, want := cleanLines("a\x1b[2J\n       b\u009b\r"), "a�[2J\n       b��"; got != want {
		t.Errorf("cleanLines = %q, want %q", got, want)
	}
}

// hostile serves posts and errors full of terminal control sequences.
func hostile(t *testing.T) *env {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/posts":
			w.Write([]byte(`{"items":[{"slug":"x","title":"\u001b]52;c;ZXZpbA==\u0007Evil","status":"draft\u001b[2J",
				"updatedBy":"mallory\u009b","words":1,"updatedAt":"2026-01-01T00:00:00Z"}],"counts":{}}`))
		case "/api/session":
			w.Write([]byte(`{"authenticated":true,"username":"eve\u001b[31m"}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"validation","message":"bad \u001b[2Jthing","detail":"\u009bdetail"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	e.vars["BURZBLOG_SERVER"] = srv.URL
	e.vars["BURZBLOG_TOKEN"] = "bzb_test"
	return e
}

func TestServerTextIsSanitizedBeforePrinting(t *testing.T) {
	e := hostile(t)
	check := func(name, s string) {
		t.Helper()
		for _, r := range s {
			if r < 0x20 && r != '\n' || r == 0x7f || r >= 0x80 && r <= 0x9f {
				t.Errorf("%s printed control character %U in %q", name, r, s)
				return
			}
		}
	}
	out, errOut, code := e.run("", "list")
	check("list", out+errOut)
	if code != 0 || !strings.Contains(out, "Evil") {
		t.Errorf("list: %d %q %q", code, out, errOut)
	}
	out, errOut, _ = e.run("", "whoami")
	check("whoami", out+errOut)
	if !strings.Contains(out, "eve") {
		t.Errorf("whoami = %q", out)
	}
	out, errOut, code = e.run("", "publish", "x")
	check("error", out+errOut)
	if code == 0 || !strings.Contains(errOut, "bad") || !strings.Contains(errOut, "detail") {
		t.Errorf("error output: %d %q", code, errOut)
	}
}

func TestRedirectErrorIsShown(t *testing.T) {
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("redirect target was contacted: %s %s", r.Method, r.URL.Path)
	}))
	defer dest.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	e := newEnv(t)
	_, errOut, code := e.run("correct horse battery\n", "login", origin.URL, "-u", "jarmos")
	if code == 0 || !strings.Contains(errOut, "redirected to "+dest.URL) || !strings.Contains(errOut, "burzblog login") {
		t.Errorf("login through a redirect: %d %q", code, errOut)
	}
	if strings.Contains(errOut, "correct horse") {
		t.Errorf("error leaks the password: %q", errOut)
	}
}

func TestUserAddRejectsControlCharacters(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"bad\x1b[2J", "tab\tname", "a b", "c1\u009bx"} {
		if _, errOut, code := e.run("correct horse battery\n", "user", "add", name); code == 0 || !strings.Contains(errOut, "username") {
			t.Errorf("user add %q: %d %q", name, code, errOut)
		}
	}
	e.must("correct horse battery\n", "user", "add", "josé-🚀")
}
