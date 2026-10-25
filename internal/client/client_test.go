package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// target records everything that reaches it, so tests can prove a
// redirect delivered nothing.
type target struct {
	*httptest.Server
	mu   sync.Mutex
	seen []string
}

func newTarget(t *testing.T) *target {
	t.Helper()
	tg := &target{}
	tg.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		tg.mu.Lock()
		tg.seen = append(tg.seen, r.Method+" "+r.URL.Path+" auth="+r.Header.Get("Authorization")+" body="+string(b))
		tg.mu.Unlock()
		w.Header().Set("ETag", `"1"`)
		w.Write([]byte(`{"secret":"x"}`))
	}))
	t.Cleanup(tg.Close)
	return tg
}

func (tg *target) requests() []string {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	return append([]string(nil), tg.seen...)
}

func TestRedirectsAreRefused(t *testing.T) {
	ctx := context.Background()
	dest := newTarget(t)
	// 307 and 308 keep the method and body, the worst case for a password POST.
	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, dest.URL+r.URL.Path, code)
		}))
		c := New(origin.URL, "bzb_SECRETTOKEN", "test")
		calls := map[string]func() error{
			"login":    func() error { _, err := c.IssueToken(ctx, "alice", "correct horse battery", "CLI"); return err },
			"whoami":   func() error { _, err := c.Whoami(ctx); return err },
			"list":     func() error { _, err := c.Posts(ctx, "", "", ""); return err },
			"get":      func() error { _, err := c.Post(ctx, "hello"); return err },
			"markdown": func() error { _, _, err := c.Markdown(ctx, "hello"); return err },
			"save":     func() error { _, err := c.SaveMarkdown(ctx, "hello", "# secret draft", 1); return err },
			"export":   func() error { _, err := c.Export(ctx); return err },
			"backup":   func() error { _, err := c.Backup(ctx); return err },
			"revoke":   func() error { return c.RevokeToken(ctx, 3) },
		}
		for name, call := range calls {
			err := call()
			if !IsCode(err, "redirect") {
				t.Fatalf("%d %s: err = %v, want a redirect error", code, name, err)
			}
			apiErr := err.(*Error)
			if apiErr.Status != code || !strings.Contains(apiErr.Message, "burzblog login "+dest.URL+"`") {
				t.Errorf("%d %s: message = %q", code, name, apiErr.Message)
			}
			if strings.Contains(apiErr.Message, "SECRETTOKEN") || strings.Contains(apiErr.Message, "correct horse") {
				t.Errorf("%d %s: message leaks a credential: %q", code, name, apiErr.Message)
			}
		}
		origin.Close()
	}
	if got := dest.requests(); len(got) != 0 {
		t.Errorf("redirect target received %q, want nothing", got)
	}
}

func TestRedirectWithoutLocation(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	defer origin.Close()
	_, err := New(origin.URL, "t", "").Whoami(context.Background())
	if !IsCode(err, "redirect") || !strings.Contains(err.Error(), "burzblog login") {
		t.Errorf("err = %v, want an actionable redirect error", err)
	}
}

func TestRedirectIsRefusedWithCallerClient(t *testing.T) {
	dest := newTarget(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	c := New(origin.URL, "bzb_SECRETTOKEN", "")
	c.HTTP = &http.Client{} // the default policy follows redirects
	if _, err := c.Whoami(context.Background()); !IsCode(err, "redirect") {
		t.Errorf("err = %v, want a redirect error", err)
	}
	if got := dest.requests(); len(got) != 0 {
		t.Errorf("redirect target received %q", got)
	}
}

func TestNormalFlowsUnchanged(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/session":
			w.Write([]byte(`{"authenticated":true,"username":"ada"}`))
		case r.URL.Path == "/api/posts/hello/markdown" && r.Method == http.MethodGet:
			w.Header().Set("ETag", `"7"`)
			w.Write([]byte("---\ntitle: Hello\n---\n"))
		case r.URL.Path == "/api/export.tar.gz":
			w.Write([]byte("tar"))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":"not_found","message":"No such post."}}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "t", "")
	if name, err := c.Whoami(ctx); err != nil || name != "ada" {
		t.Errorf("Whoami = %q, %v", name, err)
	}
	if doc, v, err := c.Markdown(ctx, "hello"); err != nil || v != 7 || !strings.Contains(doc, "Hello") {
		t.Errorf("Markdown = %q, %d, %v", doc, v, err)
	}
	body, err := c.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	body.Close()
	if _, err := c.Post(ctx, "missing"); !IsCode(err, "not_found") {
		t.Errorf("Post(missing) = %v, want not_found", err)
	}
}
