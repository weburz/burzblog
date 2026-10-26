package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// releaseServer answers like GitHub's latest-release endpoint and counts hits.
func releaseServer(t *testing.T, tag string) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	t.Helper()
	var hits atomic.Int32
	var ua atomic.Value
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		ua.Store(r.Header.Get("User-Agent"))
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","html_url":"https://github.com/weburz/burzblog/releases/tag/` + tag + `","published_at":"2026-10-01T10:00:00Z"}`))
	}))
	t.Cleanup(ts.Close)
	return ts, &hits, &ua
}

func withFastUpdates(t *testing.T) {
	t.Helper()
	oldDelay, oldEvery := updateDelay, updateEvery
	updateDelay, updateEvery = time.Millisecond, time.Hour
	t.Cleanup(func() { updateDelay, updateEvery = oldDelay, oldEvery })
}

// updatesHarness starts a harness with checks on and waits for the first one.
func updatesHarness(t *testing.T, version, tag string) (*harness, *atomic.Int32, *atomic.Value) {
	t.Helper()
	withFastUpdates(t)
	ts, hits, ua := releaseServer(t, tag)
	h := newHarness(t, func(c *Config) {
		c.CheckUpdates, c.Version, c.updateURL = true, version, ts.URL
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.srv.StartUpdateChecks(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for h.srv.updates.info() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return h, hits, ua
}

func TestUpdatesReportANewerRelease(t *testing.T) {
	h, hits, ua := updatesHarness(t, "1.1.0", "v1.2.0")
	h.login()
	resp, out := h.do("GET", "/api/updates", nil)
	if resp.StatusCode != 200 || out["enabled"] != true {
		t.Fatalf("GET /api/updates = %d %v", resp.StatusCode, out)
	}
	u, _ := out["update"].(map[string]any)
	if u["current"] != "1.1.0" || u["latest"] != "1.2.0" || u["available"] != true ||
		u["url"] != "https://github.com/weburz/burzblog/releases/tag/v1.2.0" || u["checkedAt"] == nil {
		t.Errorf("update = %v", u)
	}
	if got, _ := ua.Load().(string); !strings.Contains(got, "1.1.0") {
		t.Errorf("User-Agent = %q, want the running version in it", got)
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", hits.Load())
	}
}

func TestUpdatesNotAvailable(t *testing.T) {
	for name, tc := range map[string]struct{ version, tag string }{
		"same":  {"1.2.0", "v1.2.0"},
		"older": {"1.3.0", "v1.2.0"},
		"dev":   {"dev", "v9.9.9"},
	} {
		t.Run(name, func(t *testing.T) {
			h, _, _ := updatesHarness(t, tc.version, tc.tag)
			h.login()
			_, out := h.do("GET", "/api/updates", nil)
			u, _ := out["update"].(map[string]any)
			if u == nil || u["available"] != false {
				t.Errorf("update = %v, want available false", out["update"])
			}
		})
	}
}

func TestUpdatesAreForAdminsOnly(t *testing.T) {
	h, _, _ := updatesHarness(t, "1.1.0", "v1.2.0")
	withRoles(t, h)

	anon := h.srv.Handler()
	rec := httptest.NewRecorder()
	anon.ServeHTTP(rec, httptest.NewRequest("GET", "/api/updates", nil))
	if rec.Code != 401 {
		t.Errorf("anonymous = %d, want 401", rec.Code)
	}

	h.signInAs("grace", false)
	if status, raw := h.send("GET", "/api/updates", "", nil); status != 403 || !strings.Contains(raw, `"code":"role"`) {
		t.Errorf("author = %d %s, want 403 role", status, raw)
	}
	h.signInAs("ada", false)
	if status, _ := h.send("GET", "/api/updates", "", nil); status != 200 {
		t.Errorf("admin = %d, want 200", status)
	}
}

func TestUpdatesStayOutOfTheContentAPI(t *testing.T) {
	h, _, _ := updatesHarness(t, "1.1.0", "v1.2.0")
	h.login()
	for _, path := range []string{"/api/settings", "/api/session", "/healthz"} {
		_, out := h.do("GET", path, nil)
		if strings.Contains(out["_raw"].(string), "1.2.0") {
			t.Errorf("%s leaks the update: %s", path, out["_raw"])
		}
	}
}

func TestUpdatesOffMakeNoRequest(t *testing.T) {
	withFastUpdates(t)
	ts, hits, _ := releaseServer(t, "v9.9.9")
	h := newHarness(t, func(c *Config) { c.Version, c.updateURL = "1.1.0", ts.URL })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.srv.StartUpdateChecks(ctx)
	time.Sleep(100 * time.Millisecond)
	h.login()
	_, out := h.do("GET", "/api/updates", nil)
	if out["enabled"] != false || out["update"] != nil {
		t.Errorf("GET /api/updates = %v, want disabled and null", out)
	}
	if hits.Load() != 0 {
		t.Errorf("the release server got %d requests with checks off", hits.Load())
	}
}

func TestUpdatesFailureLeavesNoAnswer(t *testing.T) {
	withFastUpdates(t)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer ts.Close()
	h := newHarness(t, func(c *Config) { c.CheckUpdates, c.Version, c.updateURL = true, "1.1.0", ts.URL })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.srv.StartUpdateChecks(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	h.login()
	resp, out := h.do("GET", "/api/updates", nil)
	if resp.StatusCode != 200 || out["enabled"] != true || out["update"] != nil {
		t.Errorf("GET /api/updates = %d %v, want enabled and null", resp.StatusCode, out)
	}
	// Everything else keeps working.
	if resp, _ := h.do("GET", "/api/settings", nil); resp.StatusCode != 200 {
		t.Errorf("settings = %d", resp.StatusCode)
	}
}
