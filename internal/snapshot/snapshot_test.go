package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/media"
	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/store"
)

// blog is a data directory with an open store, as a running server has.
type blog struct {
	dir   string
	st    *store.Store
	files map[string][]byte // upload name -> bytes
}

func newBlog(t *testing.T) *blog {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := &blog{dir: dir, st: st, files: map[string][]byte{}}

	u, err := st.CreateUser(ctx, "ada", "$2a$10$fakehashfakehashfakehashfakehashfakehashfakehashfake", store.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.StartTOTP(ctx, u.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnableTOTP(ctx, u.ID, 1, []string{strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateToken(ctx, u.ID, strings.Repeat("b", 64), "laptop", "write"); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		slug := fmt.Sprintf("post-%d", i)
		p, err := st.CreatePost(ctx, &post.Post{Slug: slug, Title: "Post", Body: "First draft."}, "ada")
		if err != nil {
			t.Fatal(err)
		}
		// Two more versions, so revisions exist.
		for v := range 2 {
			p, err = st.UpdatePost(ctx, slug, p.Version, "ada", func(p *post.Post) error {
				p.Body = fmt.Sprintf("Version %d of %s.", v+2, slug)
				p.Tags = []string{"go"}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := st.SetTagLabel(ctx, "go", "Go", "ada"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAudit(ctx, store.Audit{ActorName: "ada", Action: "post.publish", Target: "post-0"}); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		b.upload(t, []byte(fmt.Sprintf("%%PDF-1.4 upload %d", i)))
	}
	return b
}

// upload stores data as the server does: bytes first, then the row.
func (b *blog) upload(t *testing.T, data []byte) string {
	t.Helper()
	disk, err := media.NewDisk(filepath.Join(b.dir, media.DirName))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	kind, _ := media.KindOf(".pdf")
	name := media.Name(digest, kind)
	if err := disk.Put(name, data); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.st.AddMedia(context.Background(), store.Media{FileName: name, OriginalName: "a.pdf",
		MIME: "application/pdf", Size: int64(len(data)), SHA256: digest}); err != nil {
		t.Fatal(err)
	}
	b.files[name] = data
	return name
}

func (b *blog) snapshot(t *testing.T, passphrase string) ([]byte, *Manifest) {
	t.Helper()
	var buf bytes.Buffer
	m, err := Create(context.Background(), b.st, b.dir, &buf, Options{Passphrase: passphrase, Version: "v9.9.9"})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), m
}

// checkRestored compares a restored data directory with the blog it came from.
func checkRestored(t *testing.T, from *blog, dataDir string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := range 3 {
		slug := fmt.Sprintf("post-%d", i)
		want, _ := from.st.GetPost(ctx, slug)
		got, err := st.GetPost(ctx, slug)
		if err != nil || got.Body != want.Body || got.Version != want.Version {
			t.Errorf("%s = %+v, %v; want body %q version %d", slug, got, err, want.Body, want.Version)
		}
		if revs, err := st.Revisions(ctx, slug); err != nil || len(revs) != 2 {
			t.Errorf("%s revisions = %d, %v; want 2", slug, len(revs), err)
		}
	}
	wantUser, _ := from.st.UserByName(ctx, "ada")
	if u, err := st.UserByName(ctx, "ada"); err != nil || u.PasswordHash != wantUser.PasswordHash || u.Role != store.RoleAdmin {
		t.Errorf("account = %+v, %v", u, err)
	}
	if tf, err := st.TOTP(ctx, wantUser.ID); err != nil || !tf.Enabled {
		t.Errorf("two-factor sign-in after restore = %+v, %v", tf, err)
	}
	if entry, err := st.LastAudit(ctx, "post.publish"); err != nil || entry.Target != "post-0" {
		t.Errorf("audit log = %+v, %v", entry, err)
	}
	for name, data := range from.files {
		got, err := os.ReadFile(filepath.Join(dataDir, media.DirName, name))
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("upload %s = %q, %v", name, got, err)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(dataDir, store.FileName)); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("restored database mode = %v, %v", info.Mode(), err)
		}
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	for _, passphrase := range []string{"", testPassphrase} {
		t.Run(fmt.Sprintf("encrypted=%v", passphrase != ""), func(t *testing.T) {
			b := newBlog(t)
			data, m := b.snapshot(t, passphrase)
			if m.Posts != 3 || m.Revisions != 6 || m.Accounts != 1 || len(m.Media) != 3 || m.Version != "v9.9.9" ||
				len(m.Missing) != 0 || len(m.Damaged) != 0 {
				t.Errorf("manifest = %+v", m)
			}
			pass := func() (string, error) { return passphrase, nil }

			res, err := Verify(context.Background(), bytes.NewReader(data), pass, nil, t.TempDir())
			if err != nil || res.Encrypted != (passphrase != "") || res.Manifest.Database.SHA256 != m.Database.SHA256 {
				t.Fatalf("verify = %+v, %v", res, err)
			}

			target := filepath.Join(t.TempDir(), "data")
			res, err = Restore(context.Background(), bytes.NewReader(data), pass, target, RestoreOptions{})
			if err != nil || res.Replaced != "" {
				t.Fatalf("restore = %+v, %v", res, err)
			}
			checkRestored(t, b, target)
			leftovers, _ := filepath.Glob(filepath.Join(target, ".restore-*"))
			if len(leftovers) != 0 {
				t.Errorf("working folders left behind: %v", leftovers)
			}
		})
	}
}

func TestSnapshotLeavesNoWorkingFiles(t *testing.T) {
	b := newBlog(t)
	b.snapshot(t, "")
	if left, _ := filepath.Glob(filepath.Join(b.dir, ".snapshot-*")); len(left) != 0 {
		t.Errorf("working folders left behind: %v", left)
	}
}

func TestEncryptedSnapshotNeedsTheRightPassphrase(t *testing.T) {
	b := newBlog(t)
	data, _ := b.snapshot(t, testPassphrase)
	if bytes.Contains(data, []byte("Version 3 of post-0")) {
		t.Fatal("post text is readable in the encrypted snapshot")
	}
	target := filepath.Join(t.TempDir(), "data")
	if _, err := Restore(context.Background(), bytes.NewReader(data), nil, target, RestoreOptions{}); !errors.Is(err, ErrNeedPassphrase) {
		t.Errorf("no passphrase: %v", err)
	}
	wrong := func() (string, error) { return "not the passphrase", nil }
	if _, err := Restore(context.Background(), bytes.NewReader(data), wrong, target, RestoreOptions{}); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong passphrase: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, store.FileName)); !os.IsNotExist(err) {
		t.Errorf("a failed restore left a database: %v", err)
	}
}

func TestRestoreRefusesAnExistingBlogUnlessForced(t *testing.T) {
	from := newBlog(t)
	data, _ := from.snapshot(t, "")

	other := newBlog(t)
	otherUpload := other.upload(t, []byte("%PDF-1.4 only in the other blog"))
	other.st.Close()
	_, err := Restore(context.Background(), bytes.NewReader(data), nil, other.dir, RestoreOptions{})
	if !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("restore over a blog: %v", err)
	}

	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	res, err := Restore(context.Background(), bytes.NewReader(data), nil, other.dir, RestoreOptions{Force: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	wantReplaced := filepath.Join(other.dir, store.BackupDir, "replaced-20261027T100000.000Z")
	if res.Replaced != wantReplaced {
		t.Errorf("replaced = %q, want %q", res.Replaced, wantReplaced)
	}
	checkRestored(t, from, other.dir)
	if _, err := os.Stat(filepath.Join(other.dir, media.DirName, otherUpload)); !os.IsNotExist(err) {
		t.Errorf("the replaced blog's upload is still live: %v", err)
	}
	// The replaced blog is kept whole, and opens.
	if _, err := os.Stat(filepath.Join(wantReplaced, media.DirName, otherUpload)); err != nil {
		t.Errorf("replaced upload: %v", err)
	}
	if sum, err := store.InspectFile(context.Background(), filepath.Join(wantReplaced, store.FileName)); err != nil || sum.Posts != 3 {
		t.Errorf("replaced database = %+v, %v", sum, err)
	}
}

func TestRestoreRefusesWhileAServerRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no flock on Windows")
	}
	from := newBlog(t)
	data, _ := from.snapshot(t, "")
	target := t.TempDir()
	unlock, err := store.LockShared(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), bytes.NewReader(data), nil, target, RestoreOptions{Force: true}); !errors.Is(err, ErrServerRunning) {
		t.Errorf("restore while serving: %v", err)
	}
	unlock()
	if _, err := Restore(context.Background(), bytes.NewReader(data), nil, target, RestoreOptions{}); err != nil {
		t.Errorf("restore after the server stopped: %v", err)
	}
	// And a server cannot start during a restore.
	release, err := store.LockExclusive(target)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := store.LockShared(target); !errors.Is(err, store.ErrInUse) {
		t.Errorf("serve during a restore: %v", err)
	}
}

func TestBadSnapshotLeavesTheBlogAlone(t *testing.T) {
	from := newBlog(t)
	data, _ := from.snapshot(t, "")
	encrypted, _ := from.snapshot(t, testPassphrase)

	cases := map[string][]byte{
		"cut short":           data[:len(data)/2],
		"last bytes cut":      data[:len(data)-4],
		"flipped byte":        flip(data, len(data)/2),
		"encrypted, cut":      encrypted[:len(encrypted)-10],
		"encrypted, flipped":  flip(encrypted, len(encrypted)/2),
		"empty file":          {},
		"not a snapshot":      []byte("hello, I am a text file"),
		"export archive":      tarGz(t, map[string][]byte{"published/hello.md": []byte("# Hi")}, nil),
		"escaping path":       tarGz(t, map[string][]byte{store.FileName: []byte("x"), "../evil": []byte("x")}, nil),
		"media path escape":   tarGz(t, map[string][]byte{store.FileName: []byte("x"), "media/../../evil": []byte("x")}, nil),
		"no manifest":         tarGz(t, map[string][]byte{store.FileName: []byte("x")}, nil),
		"checksum mismatch":   swapDatabase(t, data, []byte("SQLite format 3\x00 but not really")),
		"changed upload":      changeUpload(t, data),
		"newer format":        withManifest(t, data, func(m *Manifest) { m.Format = 3 }),
		"schema mismatch":     withManifest(t, data, func(m *Manifest) { m.Schema-- }),
		"unlisted upload":     withManifest(t, data, func(m *Manifest) { m.Media = m.Media[1:] }),
		"newer schema":        newerSchema(t, from),
		"garbage as database": garbageDatabase(t),
	}
	for name, snap := range cases {
		t.Run(name, func(t *testing.T) {
			victim := newBlog(t)
			victim.st.Close()
			before, _ := os.ReadFile(filepath.Join(victim.dir, store.FileName))
			pass := func() (string, error) { return testPassphrase, nil }
			if _, err := Restore(context.Background(), bytes.NewReader(snap), pass, victim.dir, RestoreOptions{Force: true}); err == nil {
				t.Fatal("restored a bad snapshot")
			}
			if _, err := Verify(context.Background(), bytes.NewReader(snap), pass, nil, t.TempDir()); err == nil {
				t.Error("verified a bad snapshot")
			}
			after, err := os.ReadFile(filepath.Join(victim.dir, store.FileName))
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("the database changed: %v", err)
			}
			for name := range victim.files {
				if _, err := os.Stat(filepath.Join(victim.dir, media.DirName, name)); err != nil {
					t.Errorf("upload %s: %v", name, err)
				}
			}
			for _, dir := range []string{filepath.Dir(victim.dir), victim.dir} {
				if _, err := os.Stat(filepath.Join(dir, "evil")); !os.IsNotExist(err) {
					t.Errorf("a file was written outside the working folder, in %s", dir)
				}
			}
			if left, _ := filepath.Glob(filepath.Join(victim.dir, ".restore-*")); len(left) != 0 {
				t.Errorf("working folders left behind: %v", left)
			}
			if _, err := os.Stat(filepath.Join(victim.dir, store.BackupDir)); !os.IsNotExist(err) {
				t.Errorf("the blog was moved aside for a snapshot that failed: %v", err)
			}
		})
	}
}

func TestSnapshotErrorsAreClear(t *testing.T) {
	ctx := context.Background()
	target := func() string { return filepath.Join(t.TempDir(), "data") }
	if _, err := Restore(ctx, strings.NewReader("hello"), nil, target(), RestoreOptions{}); !errors.Is(err, ErrNotSnapshot) {
		t.Errorf("text file: %v", err)
	}
	export := tarGz(t, map[string][]byte{"published/hello.md": []byte("# Hi")}, nil)
	if _, err := Restore(ctx, bytes.NewReader(export), nil, target(), RestoreOptions{}); !errors.Is(err, ErrNotSnapshot) {
		t.Errorf("export archive: %v", err)
	}
	from := newBlog(t)
	data, _ := from.snapshot(t, "")
	if _, err := Restore(ctx, bytes.NewReader(data[:len(data)/2]), nil, target(), RestoreOptions{}); !errors.Is(err, ErrDamaged) {
		t.Errorf("cut short: %v", err)
	}
	newer := withManifest(t, data, func(m *Manifest) { m.Format = 3 })
	if _, err := Restore(ctx, bytes.NewReader(newer), nil, target(), RestoreOptions{}); err == nil || !strings.Contains(err.Error(), "format 3") {
		t.Errorf("newer format: %v", err)
	}
	if _, err := Restore(ctx, bytes.NewReader(newerSchema(t, from)), nil, target(), RestoreOptions{}); err == nil || !strings.Contains(err.Error(), "upgrade BurzBlog") {
		t.Errorf("newer schema: %v", err)
	}
}

func TestSnapshotListsMissingAndDamagedUploads(t *testing.T) {
	b := newBlog(t)
	var names []string
	for name := range b.files {
		names = append(names, name)
	}
	if err := os.Remove(filepath.Join(b.dir, media.DirName, names[0])); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.dir, media.DirName, names[1]), []byte("bit rot"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, m := b.snapshot(t, "")
	if len(m.Missing) != 1 || m.Missing[0] != names[0] || len(m.Damaged) != 1 || m.Damaged[0] != names[1] || len(m.Media) != 2 {
		t.Fatalf("manifest = %+v", m)
	}
	// It still restores: one lost file must not stop every later backup.
	target := filepath.Join(t.TempDir(), "data")
	if _, err := Restore(context.Background(), bytes.NewReader(data), nil, target, RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(target, media.DirName, names[1])); string(got) != "bit rot" {
		t.Errorf("damaged upload = %q, want it as found", got)
	}
}

func TestSnapshotWhileWriting(t *testing.T) {
	b := newBlog(t)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var saves atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ctx.Err() == nil; i++ {
			p, err := b.st.GetPost(context.Background(), "post-0")
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := b.st.UpdatePost(context.Background(), "post-0", p.Version, "ada", func(p *post.Post) error {
				p.Body = strings.Repeat(fmt.Sprintf("save %d ", i), 500)
				return nil
			}); err != nil {
				t.Error(err)
				return
			}
			if _, err := b.st.CreatePost(context.Background(), &post.Post{Slug: fmt.Sprintf("busy-%d", i), Title: "Busy"}, "ada"); err != nil {
				t.Error(err)
				return
			}
			saves.Add(1)
		}
	}()
	var snaps [][]byte
	for range 5 {
		data, _ := b.snapshot(t, "")
		snaps = append(snaps, data)
	}
	cancel()
	wg.Wait()
	if saves.Load() == 0 {
		t.Fatal("no saves ran during the snapshots")
	}
	for i, data := range snaps {
		target := filepath.Join(t.TempDir(), "data")
		res, err := Restore(context.Background(), bytes.NewReader(data), nil, target, RestoreOptions{})
		if err != nil {
			t.Fatalf("snapshot %d: %v", i, err)
		}
		st, err := store.Open(context.Background(), target)
		if err != nil {
			t.Fatal(err)
		}
		// Each snapshot is one moment: post-0 has as many revisions as saves.
		p, err := st.GetPost(context.Background(), "post-0")
		revs, _ := st.Revisions(context.Background(), "post-0")
		st.Close()
		if err != nil || int64(len(revs)) != p.Version-1 {
			t.Errorf("snapshot %d: post-0 at version %d with %d revisions, %v", i, p.Version, len(revs), err)
		}
		if res.Manifest.Posts < 3 {
			t.Errorf("snapshot %d: %d posts", i, res.Manifest.Posts)
		}
	}
}

func TestPrune(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var names []string
	for i := range 5 {
		name := Name(base.Add(time.Duration(i)*time.Hour), i%2 == 0)
		names = append(names, name)
		os.WriteFile(filepath.Join(dir, name), nil, 0o600)
	}
	for _, other := range []string{"notes.txt", "burzblog-snapshot-latest.tar.gz"} {
		os.WriteFile(filepath.Join(dir, other), nil, 0o600)
	}
	deleted, err := Prune(dir, 2)
	if err != nil || len(deleted) != 3 || deleted[0] != names[0] || deleted[2] != names[2] {
		t.Fatalf("pruned %v, %v", deleted, err)
	}
	left, _ := os.ReadDir(dir)
	var kept []string
	for _, e := range left {
		kept = append(kept, e.Name())
	}
	if strings.Join(kept, ",") != strings.Join([]string{names[3], names[4], "burzblog-snapshot-latest.tar.gz", "notes.txt"}, ",") {
		t.Errorf("kept %v", kept)
	}
}

func flip(data []byte, at int) []byte {
	out := append([]byte{}, data...)
	out[at] ^= 0xff
	return out
}

// tarGz builds a gzipped tar of files in sorted order, then manifest if set.
func tarGz(t *testing.T, files map[string][]byte, manifest *Manifest) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(data)
	}
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		add(name, files[name])
	}
	if manifest != nil {
		data, _ := json.Marshal(manifest)
		add(manifestName, data)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// readSnapshot unpacks a plain snapshot into its files and manifest.
func readSnapshot(t *testing.T, data []byte) (map[string][]byte, *Manifest) {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	var m Manifest
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		if hdr.Name == manifestName {
			json.Unmarshal(b, &m)
			continue
		}
		files[hdr.Name] = b
	}
	return files, &m
}

func withManifest(t *testing.T, data []byte, change func(*Manifest)) []byte {
	files, m := readSnapshot(t, data)
	change(m)
	return tarGz(t, files, m)
}

func swapDatabase(t *testing.T, data []byte, db []byte) []byte {
	files, m := readSnapshot(t, data)
	files[store.FileName] = db
	return tarGz(t, files, m)
}

// newerSchema is a valid snapshot whose database claims a future schema.
func newerSchema(t *testing.T, b *blog) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "copy.db")
	if err := b.st.CopyTo(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	setUserVersion(t, path, 999)
	db, _ := os.ReadFile(path)
	data, _ := b.snapshot(t, "")
	files, m := readSnapshot(t, data)
	files[store.FileName] = db
	sum := sha256.Sum256(db)
	m.Database = File{Name: store.FileName, Size: int64(len(db)), SHA256: hex.EncodeToString(sum[:])}
	return tarGz(t, files, m)
}

// garbageDatabase is a snapshot whose checksums are right but whose database
// is not one.
func garbageDatabase(t *testing.T) []byte {
	db := []byte("this is not an SQLite database at all, just text")
	sum := sha256.Sum256(db)
	m := &Manifest{Format: FormatVersion, Schema: 1, Media: []File{},
		Database: File{Name: store.FileName, Size: int64(len(db)), SHA256: hex.EncodeToString(sum[:])}}
	return tarGz(t, map[string][]byte{store.FileName: db}, m)
}

func setUserVersion(t *testing.T, path string, v int) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
		t.Fatal(err)
	}
}

// changeUpload alters one upload's bytes and leaves the manifest as it was.
func changeUpload(t *testing.T, data []byte) []byte {
	files, m := readSnapshot(t, data)
	name := m.Media[0].Name
	files[name] = append([]byte("tampered "), files[name]...)
	return tarGz(t, files, m)
}

func TestRestoreFinishRunsUnderTheLock(t *testing.T) {
	from := newBlog(t)
	data, made := from.snapshot(t, "")
	target := filepath.Join(t.TempDir(), "data")
	var got *Manifest
	_, err := Restore(context.Background(), bytes.NewReader(data), nil, target, RestoreOptions{
		Finish: func(dir string, m *Manifest) error {
			got = m
			if runtime.GOOS != "windows" {
				if _, err := store.LockShared(dir); !errors.Is(err, store.ErrInUse) {
					t.Errorf("a server could start during Finish: %v", err)
				}
			}
			st, err := store.Open(context.Background(), dir)
			if err != nil {
				return err
			}
			return st.Close()
		},
	})
	if err != nil || got == nil || got.Database.SHA256 != made.Database.SHA256 {
		t.Errorf("finish saw %+v, %v", got, err)
	}
	if err := func() error {
		_, err := Restore(context.Background(), bytes.NewReader(data), nil, filepath.Join(t.TempDir(), "d"),
			RestoreOptions{Finish: func(string, *Manifest) error { return errors.New("audit failed") }})
		return err
	}(); err == nil || err.Error() != "audit failed" {
		t.Errorf("Finish error = %v", err)
	}
}

func TestRestoreTreatsStrayWALAsABlog(t *testing.T) {
	from := newBlog(t)
	data, _ := from.snapshot(t, "")
	target := t.TempDir()
	os.WriteFile(filepath.Join(target, store.FileName+"-wal"), []byte("left over"), 0o600)
	if _, err := Restore(context.Background(), bytes.NewReader(data), nil, target, RestoreOptions{}); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("stray WAL: %v", err)
	}
	res, err := Restore(context.Background(), bytes.NewReader(data), nil, target, RestoreOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(res.Replaced, store.FileName+"-wal")); err != nil {
		t.Errorf("the stray WAL was not moved aside: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, store.FileName+"-wal")); err == nil {
		// Opening the restored database may make a fresh WAL; it must not be the stray one.
		if b, _ := os.ReadFile(filepath.Join(target, store.FileName+"-wal")); string(b) == "left over" {
			t.Error("the stray WAL sits next to the restored database")
		}
	}
}

func TestRestoreIntoAnEmptyMediaFolder(t *testing.T) {
	from := newBlog(t)
	data, _ := from.snapshot(t, "")
	target := t.TempDir()
	os.Mkdir(filepath.Join(target, media.DirName), 0o700) // a server that started once
	if _, err := Restore(context.Background(), bytes.NewReader(data), nil, target, RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	checkRestored(t, from, target)
}

func TestInterruptedCreateLeavesNoCopy(t *testing.T) {
	b := newBlog(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Create(ctx, b.st, b.dir, io.Discard, Options{}); err == nil {
		t.Fatal("a cancelled snapshot succeeded")
	}
	if left, _ := filepath.Glob(filepath.Join(b.dir, ".snapshot-*")); len(left) != 0 {
		t.Errorf("working copy left behind: %v", left)
	}
}

func TestStaleWorkIsSwept(t *testing.T) {
	b := newBlog(t)
	old := time.Now().Add(-2 * staleAfter)
	for _, name := range []string{".snapshot-killed", ".restore-killed", ".snapshot-running"} {
		os.Mkdir(filepath.Join(b.dir, name), 0o700)
		os.WriteFile(filepath.Join(b.dir, name, store.FileName), []byte("secrets"), 0o600)
	}
	os.Chtimes(filepath.Join(b.dir, ".snapshot-killed"), old, old)
	os.Chtimes(filepath.Join(b.dir, ".restore-killed"), old, old)
	b.snapshot(t, "")
	if _, err := os.Stat(filepath.Join(b.dir, ".snapshot-killed")); !os.IsNotExist(err) {
		t.Errorf("a day-old working copy survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.dir, ".snapshot-running")); err != nil {
		t.Errorf("a fresh working copy (another run) was removed: %v", err)
	}

	dir := t.TempDir()
	stale := Name(time.Now(), false) + ".partial"
	fresh := Name(time.Now().Add(time.Second), false) + ".partial"
	for _, n := range []string{stale, fresh, "notes.partial"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o600)
		os.Chtimes(filepath.Join(dir, n), old, old)
	}
	os.Chtimes(filepath.Join(dir, fresh), time.Now(), time.Now())
	SweepPartial(dir)
	left, _ := os.ReadDir(dir)
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	if len(names) != 2 || names[0] != fresh || names[1] != "notes.partial" {
		t.Errorf("after sweeping partial files: %v", names)
	}
}
