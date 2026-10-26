package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.10.0", "v1.9.0", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1},
		{"v1.0.0-1", "v1.0.0-alpha", -1},
		{"v1.0.0+build1", "v1.0.0+build2", 0},
	} {
		a, err := ParseVersion(c.a)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ParseVersion(c.b)
		if err != nil {
			t.Fatal(err)
		}
		if got := Compare(a, b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d; want %d", c.a, c.b, got, c.want)
		}
		if got := Compare(b, a); got != -c.want {
			t.Errorf("Compare(%s, %s) = %d; want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestParseVersionRejects(t *testing.T) {
	for _, s := range []string{"", "dev", "1.0.0", "v1.0", "v1.0.0.0", "v01.0.0", "v1.0.0-", "v1.0.0/../x", "v1.0.0 "} {
		if _, err := ParseVersion(s); err == nil {
			t.Errorf("ParseVersion(%q) succeeded; want an error", s)
		}
	}
}

func TestCheckURL(t *testing.T) {
	for _, s := range []string{"https://example.com/x", "http://127.0.0.1:8080/x", "http://localhost/x", "http://[::1]:80/x"} {
		if err := CheckURL(s); err != nil {
			t.Errorf("CheckURL(%q) = %v; want ok", s, err)
		}
	}
	for _, s := range []string{"http://example.com/x", "ftp://example.com", "https://user:pw@example.com/x", "file:///etc/passwd", "example.com"} {
		if err := CheckURL(s); err == nil {
			t.Errorf("CheckURL(%q) succeeded; want an error", s)
		}
	}
}

func TestLatestAt(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name, body string
		status     int
		want       string
		wantErr    bool
	}{
		{"ok", `{"tag_name":"v1.4.0"}`, 200, "v1.4.0", false},
		{"not found", `{}`, 404, "", true},
		{"bad json", `<html>`, 200, "", true},
		{"bad tag", `{"tag_name":"nightly"}`, 200, "", true},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			fmt.Fprint(w, c.body)
		}))
		got, err := LatestAt(ctx, nil, srv.URL)
		srv.Close()
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%s: LatestAt = %q, %v; want %q, error %v", c.name, got, err, c.want, c.wantErr)
		}
	}
	if _, err := LatestAt(ctx, nil, "http://example.com/latest"); err == nil {
		t.Error("LatestAt accepted a plain http URL")
	}
}

func TestChecksum(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	sums := []byte(sum + "  burzblog-v1.0.0-linux-amd64\n" + strings.Repeat("cd", 32) + " *burzblog-v1.0.0-linux-arm64\n")
	if got, err := Checksum(sums, "burzblog-v1.0.0-linux-amd64"); err != nil || got != sum {
		t.Errorf("Checksum = %q, %v; want %q", got, err, sum)
	}
	if got, err := Checksum(sums, "burzblog-v1.0.0-linux-arm64"); err != nil || got != strings.Repeat("cd", 32) {
		t.Errorf("Checksum (binary mode) = %q, %v", got, err)
	}
	if _, err := Checksum(sums, "burzblog-v1.0.0-darwin-arm64"); err == nil {
		t.Error("Checksum found an asset that is not listed")
	}
	if _, err := Checksum([]byte("zz  burzblog-v1.0.0-linux-amd64\n"), "burzblog-v1.0.0-linux-amd64"); err == nil {
		t.Error("Checksum accepted a malformed sum")
	}
}

func TestAssetNames(t *testing.T) {
	if got := AssetName("v1.2.3", "linux", "arm"); got != "burzblog-v1.2.3-linux-arm" {
		t.Errorf("AssetName = %q", got)
	}
	if got := AssetName("v1.2.3", "windows", "amd64"); got != "burzblog-v1.2.3-windows-amd64.exe" {
		t.Errorf("AssetName = %q", got)
	}
	if got := ChecksumName("v1.2.3"); got != "SHA256SUMS-v1.2.3" {
		t.Errorf("ChecksumName = %q", got)
	}
	if got := AssetURL("https://example.com/dl/", "v1.2.3", "x"); got != "https://example.com/dl/v1.2.3/x" {
		t.Errorf("AssetURL = %q", got)
	}
}

func TestFetch(t *testing.T) {
	ctx := context.Background()
	payload := bytes.Repeat([]byte("x"), 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write(payload)
	}))
	defer srv.Close()
	client := NewClient(5 * time.Second)

	var buf bytes.Buffer
	sum, err := Fetch(ctx, client, srv.URL+"/file", 1000, &buf)
	want := sha256.Sum256(payload)
	if err != nil || sum != hex.EncodeToString(want[:]) || buf.Len() != 1000 {
		t.Errorf("Fetch = %q, %v (%d bytes); want the payload's sum", sum, err, buf.Len())
	}
	if _, err := Fetch(ctx, client, srv.URL+"/file", 999, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("Fetch over the limit = %v; want a limit error", err)
	}
	if _, err := Fetch(ctx, client, srv.URL+"/missing", 1000, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("Fetch of a missing file = %v; want HTTP 404", err)
	}
	if _, err := Fetch(ctx, client, "http://example.com/file", 1000, &bytes.Buffer{}); err == nil {
		t.Error("Fetch accepted a plain http URL")
	}
}

func TestRedirectsMustStayOnHTTPS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/evil", http.StatusFound)
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), NewClient(5*time.Second), srv.URL, 10, &bytes.Buffer{}); err == nil {
		t.Error("Fetch followed a redirect to plain http")
	}
}
