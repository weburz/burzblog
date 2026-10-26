package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/release"
)

// fakeReleases serves GitHub's latest-release API and release downloads for
// linux/amd64 builds of the given versions.
type fakeReleases struct {
	srv    *httptest.Server
	latest string
	// bad lists versions whose binary does not match the checksum file.
	bad map[string]bool
}

func newFakeReleases(t *testing.T, latest string, versions ...string) *fakeReleases {
	t.Helper()
	f := &fakeReleases{latest: latest, bad: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, f.latest)
	})
	for _, v := range versions {
		asset := release.AssetName(v, "linux", "amd64")
		body := []byte("binary " + v)
		sum := sha256.Sum256(body)
		mux.HandleFunc("/dl/"+v+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
			w.Write(body)
		})
		mux.HandleFunc("/dl/"+v+"/"+release.ChecksumName(v), func(w http.ResponseWriter, r *http.Request) {
			l := hex.EncodeToString(sum[:])
			if f.bad[v] {
				l = strings.Repeat("0", 64)
			}
			fmt.Fprintf(w, "%s  %s\n%s  other-file\n", l, asset, strings.Repeat("1", 64))
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type upgradeRun struct {
	out, errOut string
	code        int
	exe         string
}

// runUpgrade runs `burzblog upgrade args...` as version current, with a fake
// binary on disk to replace.
func runUpgrade(t *testing.T, f *fakeReleases, current string, container bool, args ...string) upgradeRun {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "burzblog")
	if err := os.WriteFile(exe, []byte("binary "+current), 0o755); err != nil {
		t.Fatal(err)
	}
	return runUpgradeAt(t, f, current, container, exe, args...)
}

func runUpgradeAt(t *testing.T, f *fakeReleases, current string, container bool, exe string, args ...string) upgradeRun {
	t.Helper()
	var out, errOut bytes.Buffer
	a := &App{
		In: strings.NewReader(""), Out: &out, Err: &errOut, Version: current,
		Getenv: func(string) string { return "" },
		release: &releaseSource{
			LatestURL: f.srv.URL + "/api/latest", DownloadBase: f.srv.URL + "/dl",
			Client: release.NewClient(10 * time.Second), GOOS: "linux", GOARCH: "amd64",
			Executable:  func() (string, error) { return exe, nil },
			InContainer: func() bool { return container },
		},
	}
	code := a.Execute(append([]string{"upgrade"}, args...))
	return upgradeRun{out.String(), errOut.String(), code, exe}
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpgradeReplacesBinary(t *testing.T) {
	f := newFakeReleases(t, "v1.4.0", "v1.3.0", "v1.4.0")
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("binary v1.3.0"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "burzblog")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	r := runUpgradeAt(t, f, "v1.3.0", false, link)
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	if got := fileText(t, real); got != "binary v1.4.0" {
		t.Errorf("binary = %q; want the v1.4.0 download", got)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the symlink was replaced instead of its target: %v, %v", fi, err)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v; want 0755", fi.Mode().Perm())
	}
	for _, want := range []string{"v1.4.0", "sudo systemctl restart burzblog", "backs itself up"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output lacks %q:\n%s", want, r.out)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("directory holds %d entries; want the binary and its link only", len(entries))
	}
}

func TestUpgradeJSON(t *testing.T) {
	f := newFakeReleases(t, "v1.4.0", "v1.4.0")
	r := runUpgrade(t, f, "v1.3.0", false, "--json")
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s%s", r.code, r.out, r.errOut)
	}
	var got struct{ From, To, Path, Restart string }
	if err := json.Unmarshal([]byte(r.out), &got); err != nil || got.From != "v1.3.0" || got.To != "v1.4.0" || got.Restart != "sudo systemctl restart burzblog" {
		t.Errorf("output = %q (%v); want from, to, path and restart", r.out, err)
	}
}

func TestUpgradeAlreadyUpToDate(t *testing.T) {
	f := newFakeReleases(t, "v1.4.0", "v1.4.0")
	for _, current := range []string{"v1.4.0", "v1.5.0"} {
		r := runUpgrade(t, f, current, false)
		if r.code != ExitOK || !strings.Contains(r.out, "Already up to date") {
			t.Errorf("%s: exit %d, output %q; want already up to date", current, r.code, r.out)
		}
		if got := fileText(t, r.exe); got != "binary "+current {
			t.Errorf("%s: binary changed to %q", current, got)
		}
	}
}

func TestUpgradeCheck(t *testing.T) {
	f := newFakeReleases(t, "v1.4.0")
	r := runUpgrade(t, f, "v1.3.0", false, "--check")
	if r.code != ExitUpdate || !strings.Contains(r.out, "Update available: v1.4.0") {
		t.Errorf("exit %d, output %q; want %d and an update notice", r.code, r.out, ExitUpdate)
	}
	if r.errOut != "" {
		t.Errorf("--check printed an error: %q", r.errOut)
	}
	if got := fileText(t, r.exe); got != "binary v1.3.0" {
		t.Errorf("--check changed the binary to %q", got)
	}

	r = runUpgrade(t, f, "v1.3.0", false, "--check", "--json")
	var doc struct {
		Current, Latest string
		UpdateAvailable bool
	}
	if err := json.Unmarshal([]byte(r.out), &doc); err != nil || r.code != ExitUpdate ||
		doc.Current != "v1.3.0" || doc.Latest != "v1.4.0" || !doc.UpdateAvailable {
		t.Errorf("exit %d, output %q (%v); want an update-available document", r.code, r.out, err)
	}

	r = runUpgrade(t, f, "v1.4.0", false, "--check", "--json")
	if err := json.Unmarshal([]byte(r.out), &doc); err != nil || r.code != ExitOK || doc.UpdateAvailable {
		t.Errorf("exit %d, output %q (%v); want up to date", r.code, r.out, err)
	}
}

func TestUpgradeRefusals(t *testing.T) {
	f := newFakeReleases(t, "v1.4.0", "v1.2.0", "v1.4.0")
	f.bad["v1.2.0"] = true
	for _, c := range []struct {
		name, current string
		container     bool
		args          []string
		code          int
		want          string
	}{
		{"downgrade", "v1.3.0", false, []string{"--version", "v1.2.0"}, ExitUsage, "--force"},
		{"dev build", "dev", false, nil, ExitUsage, "development build"},
		{"dev build check", "dev", false, []string{"--check"}, ExitUsage, "development build"},
		{"container", "v1.3.0", true, nil, ExitError, "pull the new image"},
		{"bad version", "v1.3.0", false, []string{"--version", "latest"}, ExitUsage, "not a version"},
		{"checksum mismatch", "v1.1.0", false, []string{"--version", "v1.2.0"}, ExitError, "does not match its checksum"},
		{"not published", "v1.3.0", false, []string{"--version", "v1.9.0"}, ExitError, "HTTP 404"},
	} {
		r := runUpgrade(t, f, c.current, c.container, c.args...)
		if r.code != c.code || !strings.Contains(r.errOut, c.want) {
			t.Errorf("%s: exit %d, stderr %q; want exit %d mentioning %q", c.name, r.code, r.errOut, c.code, c.want)
		}
		if got := fileText(t, r.exe); got != "binary "+c.current {
			t.Errorf("%s: binary changed to %q", c.name, got)
		}
	}
}

func TestUpgradeForceAndDevVersion(t *testing.T) {
	f := newFakeReleases(t, "v1.4.0", "v1.2.0", "v1.4.0")
	r := runUpgrade(t, f, "v1.3.0", false, "--version", "v1.2.0", "--force")
	if r.code != ExitOK || fileText(t, r.exe) != "binary v1.2.0" {
		t.Errorf("forced downgrade: exit %d, binary %q, stderr %q", r.code, fileText(t, r.exe), r.errOut)
	}
	r = runUpgrade(t, f, "dev", false, "--version", "v1.4.0")
	if r.code != ExitOK || fileText(t, r.exe) != "binary v1.4.0" {
		t.Errorf("dev build with --version: exit %d, binary %q, stderr %q", r.code, fileText(t, r.exe), r.errOut)
	}
	r = runUpgrade(t, f, "v1.4.0", false, "--force")
	if r.code != ExitOK || !strings.Contains(r.out, "Installed") {
		t.Errorf("forced reinstall: exit %d, output %q", r.code, r.out)
	}
}

func TestUpgradeUnlistedPlatform(t *testing.T) {
	f := newFakeReleases(t, "v1.4.0", "v1.4.0")
	exe := filepath.Join(t.TempDir(), "burzblog")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	a := &App{Out: &bytes.Buffer{}, Err: &errOut, Version: "v1.3.0", Getenv: func(string) string { return "" },
		release: &releaseSource{
			LatestURL: f.srv.URL + "/api/latest", DownloadBase: f.srv.URL + "/dl",
			Client: release.NewClient(5 * time.Second), GOOS: "linux", GOARCH: "riscv64",
			Executable: func() (string, error) { return exe, nil }, InContainer: func() bool { return false },
		}}
	if code := a.Execute([]string{"upgrade"}); code != ExitError || !strings.Contains(errOut.String(), "no build for your platform") {
		t.Errorf("exit %d, stderr %q; want a missing-platform error", code, errOut.String())
	}
}

func TestUpgradeNeedsHTTPS(t *testing.T) {
	var errOut bytes.Buffer
	a := &App{Out: &bytes.Buffer{}, Err: &errOut, Version: "v1.3.0", Getenv: func(string) string { return "" },
		release: &releaseSource{
			LatestURL: "http://example.com/latest", DownloadBase: "http://example.com/dl",
			Client: release.NewClient(5 * time.Second), GOOS: "linux", GOARCH: "amd64",
			Executable: func() (string, error) { return "unused", nil }, InContainer: func() bool { return false },
		}}
	if code := a.Execute([]string{"upgrade"}); code != ExitError || !strings.Contains(errOut.String(), "only https") {
		t.Errorf("exit %d, stderr %q; want an https-only error", code, errOut.String())
	}
}

func TestUpgradeUnwritableDirectory(t *testing.T) {
	f := newFakeReleases(t, "v1.4.0", "v1.4.0")
	dir := t.TempDir()
	exe := filepath.Join(dir, "burzblog")
	if err := os.WriteFile(exe, []byte("binary v1.3.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if f, err := os.CreateTemp(dir, "probe"); err == nil {
		f.Close()
		t.Skip("directory permissions are not enforced (running as root)")
	}
	r := runUpgradeAt(t, f, "v1.3.0", false, exe)
	if r.code != ExitError || !strings.Contains(r.errOut, "sudo") {
		t.Errorf("exit %d, stderr %q; want an error suggesting sudo", r.code, r.errOut)
	}
	if got := fileText(t, exe); got != "binary v1.3.0" {
		t.Errorf("binary changed to %q", got)
	}
}
