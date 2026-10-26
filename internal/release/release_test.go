package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestFetch(t *testing.T) {
	var gotUA, gotMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotMethod = r.Header.Get("User-Agent"), r.Method
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","html_url":"https://github.com/weburz/burzblog/releases/tag/v1.2.0","published_at":"2026-10-01T10:00:00Z","body":"notes"}`))
	}))
	defer ts.Close()
	rel, err := Fetch(context.Background(), ts.Client(), ts.URL, "burzblog/1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v1.2.0" || !strings.HasSuffix(rel.URL, "/v1.2.0") || rel.PublishedAt.Year() != 2026 {
		t.Errorf("release = %+v", rel)
	}
	if gotUA != "burzblog/1.1.0" || gotMethod != "GET" {
		t.Errorf("sent %s with User-Agent %q", gotMethod, gotUA)
	}
}

func TestFetchFailures(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"not found", serve(t, 404, `{"message":"Not Found"}`).URL, "HTTP 404"},
		{"rate limited", serve(t, 403, `{}`).URL, "HTTP 403"},
		{"malformed JSON", serve(t, 200, `{"tag_name":`).URL, "decode"},
		{"no tag", serve(t, 200, `{"html_url":"x"}`).URL, "no tag"},
		{"oversize", serve(t, 200, `{"tag_name":"v1.0.0","body":"`+strings.Repeat("a", maxBody)+`"}`).URL, "larger than"},
		{"timeout", slow.URL, "fetch the latest release"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_, err := Fetch(ctx, http.DefaultClient, tc.url, "burzblog")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"1.0.0", "v1.0.1", true},
		{"v1.9.0", "v1.10.0", true},
		{"1.2.3", "2.0.0", true},
		{"1.0.0", "v1.0.0", false},
		{"1.1.0", "v1.0.9", false},
		{"2.0.0", "v1.9.9", false},
		{"1.0.0-rc.1", "v1.0.0", true},
		{"1.0.0", "v1.0.0-rc.1", false},
		{"1.0.0+build5", "v1.0.0", false},
		{"dev", "v1.0.0", false},
		{"test", "v1.0.0", false},
		{"", "v1.0.0", false},
		{"1.0.0", "latest", false},
		{"1.0", "1.1.0", false},
		{"1.0.0", "01.0.1", false},
	}
	for _, tc := range cases {
		if got := Newer(tc.current, tc.latest); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestLatestUsesTheGitHubAddress(t *testing.T) {
	if !strings.HasPrefix(LatestURL, "https://api.github.com/repos/weburz/burzblog/") {
		t.Errorf("LatestURL = %s", LatestURL)
	}
}
