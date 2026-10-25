package cli

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/server"
	"github.com/weburz/burzblog/internal/store"
)

func TestAnnounceSetup(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc, _ := auth.New(st)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))

	ts := httptest.NewUnstartedServer(nil)
	ts.Start()
	t.Cleanup(ts.Close)
	srv, err := server.New(server.Config{Origin: ts.URL, Logger: log}, st, svc)
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = srv.Handler()

	if err := announceSetup(ctx, log, svc, ts.URL+"/", "/var/lib/burzblog"); err != nil {
		t.Fatal(err)
	}
	line := logs.String()
	m := regexp.MustCompile(`open (\S+)/setup#token=([A-Za-z0-9_-]{43}) `).FindStringSubmatch(line)
	if m == nil || m[1] != ts.URL {
		t.Fatalf("log line has no setup link on the origin:\n%s", line)
	}
	for _, want := range []string{"60 minutes", "restart", "burzblog user add <name> --data /var/lib/burzblog"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line does not mention %q:\n%s", want, line)
		}
	}
	token := m[2]

	// Failed and successful setups never write the token to the log again.
	post := func(token, password string) int {
		body := `{"token":"` + token + `","username":"ada","password":"` + password + `"}`
		req, _ := http.NewRequest("POST", ts.URL+"/api/setup", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", ts.URL)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	post("wrong", "correct horse battery")
	post(token, "short")
	if code := post(token, "correct horse battery"); code != 201 {
		t.Fatalf("setup with the logged token = %d, want 201", code)
	}
	post(token, "correct horse battery")
	if n := strings.Count(logs.String(), token); n != 1 {
		t.Errorf("the token appears %d times in the log, want 1 (the setup link):\n%s", n, logs.String())
	}
}

func TestAnnounceSetupStaysQuietWithAccounts(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := auth.HashPassword("correct horse battery")
	if _, err := st.CreateUser(ctx, "ada", hash); err != nil {
		t.Fatal(err)
	}
	svc, _ := auth.New(st)
	var logs bytes.Buffer
	if err := announceSetup(ctx, slog.New(slog.NewTextHandler(&logs, nil)), svc, "http://x", "d"); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Errorf("announced setup although an account exists:\n%s", logs.String())
	}
}
