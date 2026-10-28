package cli

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/weburz/burzblog/internal/store"
)

// fakeS3 is enough of an S3 API, path-style, for the snapshot commands:
// put, get, head, delete and list-type=2 of one bucket. It checks that every
// request is signed, not the signature itself (internal/s3 tests that).
func fakeS3(t *testing.T) (*httptest.Server, map[string][]byte) {
	t.Helper()
	var mu sync.Mutex
	objects := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-key/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if bucket != "blogs" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, "<Error><Code>NoSuchBucket</Code></Error>")
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch {
		case key == "" && r.Method == http.MethodGet:
			type entry struct {
				Key  string `xml:"Key"`
				Size int    `xml:"Size"`
			}
			var out struct {
				XMLName  xml.Name `xml:"ListBucketResult"`
				Contents []entry  `xml:"Contents"`
			}
			for k, v := range objects {
				if strings.HasPrefix(k, r.URL.Query().Get("prefix")) {
					out.Contents = append(out.Contents, entry{k, len(v)})
				}
			}
			sort.Slice(out.Contents, func(i, j int) bool { return out.Contents[i].Key < out.Contents[j].Key })
			xml.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPut:
			objects[key], _ = io.ReadAll(r.Body)
		case r.Method == http.MethodGet || r.Method == http.MethodHead:
			data, ok := objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Method == http.MethodGet {
				w.Write(data)
			}
		case r.Method == http.MethodDelete:
			delete(objects, key)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, objects
}

func s3Env(t *testing.T) (*env, map[string][]byte) {
	t.Helper()
	e := snapshotEnv(t)
	srv, objects := fakeS3(t)
	e.vars["BURZBLOG_S3_ENDPOINT"] = srv.URL
	e.vars["BURZBLOG_S3_ACCESS_KEY_ID"] = "test-key"
	e.vars["BURZBLOG_S3_SECRET_ACCESS_KEY"] = "test-secret"
	return e, objects
}

func TestSnapshotToObjectStorage(t *testing.T) {
	e, objects := s3Env(t)
	if _, errOut, code := e.run("", "snapshot", "create", "s3://blogs/ada/"); code != ExitUsage || !strings.Contains(errOut, "must be encrypted") {
		t.Fatalf("plain remote: %d %q", code, errOut)
	}
	e.vars["BURZBLOG_SNAPSHOT_PASSPHRASE"] = "a long passphrase here"
	var first, second snapshotLine
	json.Unmarshal([]byte(e.must("", "snapshot", "create", "s3://blogs/ada/", "--json")), &first)
	json.Unmarshal([]byte(e.must("", "snapshot", "create", "s3://blogs/ada", "--json", "--keep", "5")), &second)
	if !first.Encrypted || first.Sent == nil || !strings.HasPrefix(first.Path, "s3://blogs/ada/snapshots/burzblog-snapshot-") {
		t.Fatalf("first = %+v", first)
	}
	if second.Sent == nil || *second.Sent != 0 {
		t.Errorf("second run sent uploads again: %+v", second)
	}
	for key := range objects {
		if !strings.HasPrefix(key, "ada/") {
			t.Errorf("object outside the prefix: %s", key)
		}
	}
	if entry, err := e.store.LastAudit(t.Context(), "snapshot.create"); err != nil || entry.Target != second.Path+", encrypted" {
		t.Errorf("audit = %+v, %v", entry, err)
	}

	var listed struct {
		Items []struct{ Name, Path string } `json:"items"`
	}
	json.Unmarshal([]byte(e.must("", "snapshot", "list", "s3://blogs/ada/", "--json")), &listed)
	if len(listed.Items) != 2 || listed.Items[0].Path != first.Path {
		t.Fatalf("list = %+v", listed)
	}
	if out := e.must("", "snapshot", "verify", "s3://blogs/ada/"); !strings.Contains(out, "complete and restores") {
		t.Errorf("verify = %q", out)
	}

	// Restore the older one onto a new server, by its full key.
	e.vars["BURZBLOG_DATA"] = filepath.Join(t.TempDir(), "new")
	if out := e.must("", "snapshot", "restore", first.Path); !strings.Contains(out, "Restored") {
		t.Errorf("restore = %q", out)
	}
	st, err := store.Open(t.Context(), e.vars["BURZBLOG_DATA"])
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if p, err := st.GetPost(t.Context(), "hello"); err != nil || p.Body != "Kept safe." {
		t.Errorf("restored post = %+v, %v", p, err)
	}

	delete(e.vars, "BURZBLOG_S3_ENDPOINT")
	if _, errOut, code := e.run("", "snapshot", "list", "s3://blogs/ada/"); code != ExitUsage || !strings.Contains(errOut, "BURZBLOG_S3_ENDPOINT") {
		t.Errorf("no endpoint: %d %q", code, errOut)
	}
}

func TestRemoteTargetParsing(t *testing.T) {
	e, _ := s3Env(t)
	a := &App{Getenv: func(k string) string { return e.vars[k] }}
	for target, want := range map[string][2]string{
		"s3://blogs":      {"", ""},
		"s3://blogs/":     {"", ""},
		"s3://blogs/ada":  {"ada/", ""},
		"s3://blogs/a/b/": {"a/b/", ""},
		"s3://blogs/ada/snapshots/burzblog-snapshot-20261028T031700.000Z.tar.gz.enc": {"ada/", "burzblog-snapshot-20261028T031700.000Z.tar.gz.enc"},
		"s3://blogs/snapshots/burzblog-snapshot-20261028T031700.000Z.tar.gz.enc":     {"", "burzblog-snapshot-20261028T031700.000Z.tar.gz.enc"},
	} {
		r, name, err := a.remote(target)
		if err != nil || r.Prefix != want[0] || name != want[1] {
			t.Errorf("%s = prefix %q name %q, %v; want %q %q", target, r.Prefix, name, err, want[0], want[1])
		}
	}
	if _, _, err := a.remote("s3://Bad_Bucket/x"); err == nil {
		t.Error("accepted a bad bucket name")
	}
	if _, _, err := a.remote("s3://blogs/a//b/"); err == nil || !strings.Contains(err.Error(), "empty part") {
		t.Errorf("double slash: %v", err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte("from-a-file\n"), 0o600)
	e.vars["BURZBLOG_S3_SECRET_ACCESS_KEY_FILE"] = secret
	if _, _, err := a.remote("s3://blogs/ada/"); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("secret twice: %v", err)
	}
	delete(e.vars, "BURZBLOG_S3_SECRET_ACCESS_KEY")
	if got, err := a.secretSetting("BURZBLOG_S3_SECRET_ACCESS_KEY"); err != nil || got != "from-a-file" {
		t.Errorf("secret from a file = %q, %v", got, err)
	}
}

func TestServeScheduleSettings(t *testing.T) {
	e, _ := s3Env(t)
	a := &App{Getenv: func(k string) string { return e.vars[k] }}
	st := e.store
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	data := e.vars["BURZBLOG_DATA"]

	if s, err := a.snapshotSchedule("", "", 14, false, st, data, log); s != nil || err != nil {
		t.Errorf("off by default: %v %v", s, err)
	}
	s, err := a.snapshotSchedule("24h", "", 14, false, st, data, log)
	if err != nil || s.To.String() != filepath.Join(data, "snapshots") || s.Keep != 14 {
		t.Fatalf("local default = %+v, %v", s, err)
	}
	e.vars["BURZBLOG_SNAPSHOT_KEEP"] = "3"
	if s, err := a.snapshotSchedule("24h", "", 14, false, st, data, log); err != nil || s.Keep != 3 {
		t.Errorf("keep from the environment = %+v, %v", s, err)
	}
	for name, c := range map[string]struct{ every, to, want string }{
		"bad duration": {"daily", "", "duration"},
		"too often":    {"10m", "", "at most every"},
		"plain remote": {"24h", "s3://blogs/ada/", "must be encrypted"},
		"one snapshot": {"24h", "s3://blogs/ada/snapshots/burzblog-snapshot-20261028T031700.000Z.tar.gz.enc", "not one snapshot"},
	} {
		if _, err := a.snapshotSchedule(c.every, c.to, 14, false, st, data, log); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	e.vars["BURZBLOG_SNAPSHOT_PASSPHRASE"] = "a long passphrase here"
	e.vars["BURZBLOG_SNAPSHOT_HEARTBEAT"] = "http://hc-ping.com/abc"
	if _, err := a.snapshotSchedule("24h", "s3://blogs/ada/", 14, false, st, data, log); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("http heartbeat: %v", err)
	}
	e.vars["BURZBLOG_SNAPSHOT_HEARTBEAT"] = "https://hc-ping.com/abc"
	if s, err := a.snapshotSchedule("12h", "s3://blogs/ada/", 14, false, st, data, log); err != nil || s.To.String() != "s3://blogs/ada/" {
		t.Errorf("remote = %+v, %v", s, err)
	}
	e.vars["BURZBLOG_SNAPSHOT_PASSPHRASE_FILE"] = "/somewhere"
	if _, err := a.snapshotSchedule("12h", "", 14, false, st, data, log); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("both passphrase settings: %v", err)
	}
}
