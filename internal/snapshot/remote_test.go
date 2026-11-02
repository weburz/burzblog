package snapshot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/media"
	"github.com/weburz/burzblog/internal/s3"
	"github.com/weburz/burzblog/internal/store"
)

// memBucket is an in-memory Bucket.
type memBucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	stored  map[string]time.Time
	puts    int
}

func newMemBucket() *memBucket {
	return &memBucket{objects: map[string][]byte{}, stored: map[string]time.Time{}}
}

// age makes every object look stored d ago.
func (b *memBucket) age(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k := range b.stored {
		b.stored[k] = b.stored[k].Add(-d)
	}
}

func (b *memBucket) Put(_ context.Context, key string, body io.ReaderAt, size int64, _ string) error {
	data, err := io.ReadAll(io.NewSectionReader(body, 0, size))
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return errors.New("size mismatch")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = data
	b.stored[key] = time.Now()
	b.puts++
	return nil
}

func (b *memBucket) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.objects[key]
	if !ok {
		return nil, s3.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (b *memBucket) List(_ context.Context, prefix string) ([]s3.Object, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []s3.Object
	for k, v := range b.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, s3.Object{Key: k, Size: int64(len(v)), LastModified: b.stored[k]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (b *memBucket) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects, key)
	return nil
}

func (b *memBucket) keys(prefix string) []string {
	list, _ := b.List(context.Background(), prefix)
	var keys []string
	for _, o := range list {
		keys = append(keys, o.Key)
	}
	return keys
}

// clock gives each snapshot its own minute, so names sort in run order.
type clock struct{ t time.Time }

func (c *clock) now() time.Time {
	c.t = c.t.Add(time.Minute)
	return c.t
}

func remoteOpts(c *clock) Options {
	return Options{Passphrase: testPassphrase, Version: "v9.9.9", Now: c.now}
}

func restoreRemote(t *testing.T, r Remote, name string) (string, error) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "data")
	rc, src, err := r.Open(context.Background(), name)
	if err != nil {
		return target, err
	}
	defer rc.Close()
	_, err = Restore(context.Background(), rc, func() (string, error) { return testPassphrase, nil }, target,
		RestoreOptions{Media: src})
	return target, err
}

func TestRemoteSendsEachUploadOnce(t *testing.T) {
	b := newBlog(t)
	bucket := newMemBucket()
	r := Remote{Bucket: bucket, Prefix: "blog/", Label: "mem://blog/"}
	c := &clock{t: time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)}
	ctx := context.Background()

	first, err := CreateRemote(ctx, b.st, b.dir, r, 0, remoteOpts(c))
	if err != nil {
		t.Fatal(err)
	}
	if first.Sent != 3 || len(first.Manifest.Media) != 3 || first.Manifest.Format != RemoteFormat {
		t.Fatalf("first run = %+v", first)
	}
	second, err := CreateRemote(ctx, b.st, b.dir, r, 0, remoteOpts(c))
	if err != nil || second.Sent != 0 || len(second.Manifest.Media) != 3 {
		t.Fatalf("second run sent %d uploads again: %+v, %v", second.Sent, second, err)
	}
	added := b.upload(t, []byte("%PDF-1.4 added later"))
	third, err := CreateRemote(ctx, b.st, b.dir, r, 0, remoteOpts(c))
	if err != nil || third.Sent != 1 || len(third.Manifest.Media) != 4 {
		t.Fatalf("third run = %+v, %v", third, err)
	}
	if got := len(bucket.keys("blog/media/")); got != 4 {
		t.Errorf("stored uploads = %d, want 4", got)
	}

	// Nothing readable is stored.
	for key, data := range bucket.objects {
		for _, secret := range [][]byte{[]byte("Version 3 of post-0"), []byte("%PDF-1.4"), []byte("JBSWY3DPEHPK3PXP")} {
			if bytes.Contains(data, secret) {
				t.Errorf("%s holds %q in the clear", key, secret)
			}
		}
	}

	// The newest restores with the added upload; the oldest without it.
	target, err := restoreRemote(t, r, "")
	if err != nil {
		t.Fatal(err)
	}
	checkRestored(t, b, target)
	oldest, err := restoreRemote(t, r, Name(first.Manifest.CreatedAt, true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(oldest, media.DirName, added)); !os.IsNotExist(err) {
		t.Errorf("the oldest snapshot restored an upload made after it: %v", err)
	}

	list, err := r.List(ctx)
	if err != nil || len(list) != 3 || list[2].Key != third.Key {
		t.Errorf("list = %+v, %v", list, err)
	}
	rc, src, err := r.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if res, err := Verify(ctx, rc, func() (string, error) { return testPassphrase, nil }, src, t.TempDir()); err != nil || len(res.Manifest.Media) != 4 {
		t.Errorf("verify = %+v, %v", res, err)
	}
}

func mustOpen(t *testing.T, r Remote, name string) io.Reader {
	t.Helper()
	rc, _, err := r.Open(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rc.Close() })
	return rc
}

func TestRemoteNeedsEncryption(t *testing.T) {
	b := newBlog(t)
	r := Remote{Bucket: newMemBucket(), Label: "mem://"}
	if _, err := CreateRemote(context.Background(), b.st, b.dir, r, 0, Options{}); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Errorf("plain remote snapshot: %v", err)
	}
}

func TestRemotePruneKeepsWhatKeptSnapshotsNeed(t *testing.T) {
	b := newBlog(t)
	bucket := newMemBucket()
	r := Remote{Bucket: bucket, Prefix: "p/", Label: "mem://p/"}
	c := &clock{t: time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)}
	ctx := context.Background()

	if _, err := CreateRemote(ctx, b.st, b.dir, r, 2, remoteOpts(c)); err != nil {
		t.Fatal(err)
	}
	// Delete one upload from the blog: snapshots made from now on lack it.
	var gone string
	for name := range b.files {
		gone = name
		break
	}
	list, _, _ := b.st.ListMedia(ctx, 0, 0)
	for _, m := range list {
		if m.FileName == gone {
			if _, err := b.st.DeleteMedia(ctx, m.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	os.Remove(filepath.Join(b.dir, media.DirName, gone))
	delete(b.files, gone)

	second, err := CreateRemote(ctx, b.st, b.dir, r, 2, remoteOpts(c))
	if err != nil || len(second.Pruned) != 0 {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if _, err := bucket.Get(ctx, r.mediaKey(gone, true)); err != nil {
		t.Errorf("the first snapshot still needs %s, but it was deleted: %v", gone, err)
	}
	third, err := CreateRemote(ctx, b.st, b.dir, r, 2, remoteOpts(c))
	if err != nil || len(third.Pruned) != 1 || third.PruneWarning != "" {
		t.Fatalf("third = %+v, %v", third, err)
	}
	// Unnamed now, but stored within the day: another machine's run might
	// be about to name it.
	if _, err := bucket.Get(ctx, r.mediaKey(gone, true)); err != nil {
		t.Errorf("an upload was deleted inside its grace period: %v", err)
	}
	bucket.age(25 * time.Hour)
	if _, err := CreateRemote(ctx, b.st, b.dir, r, 2, remoteOpts(c)); err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Get(ctx, r.mediaKey(gone, true)); !errors.Is(err, s3.ErrNotFound) {
		t.Errorf("an upload no kept snapshot names was kept past its grace: %v", err)
	}
	if n := len(bucket.keys("p/snapshots/")); n != 4 {
		t.Errorf("snapshot objects = %d, want 2 snapshots and their 2 manifests", n)
	}
	if _, err := restoreRemote(t, r, ""); err != nil {
		t.Errorf("newest after pruning: %v", err)
	}

	// A kept manifest that cannot be read stops the clean-up of uploads.
	list2, _ := r.List(ctx)
	bucket.objects[list2[0].Key+".manifest"] = []byte("garbage")
	b.upload(t, []byte("%PDF-1.4 one more"))
	fourth, err := CreateRemote(ctx, b.st, b.dir, r, 5, remoteOpts(c))
	if err != nil || fourth.PruneWarning == "" {
		t.Errorf("unreadable manifest: %+v, %v", fourth, err)
	}
}

func TestRemoteRestoreChecksEveryUpload(t *testing.T) {
	b := newBlog(t)
	ctx := context.Background()
	c := &clock{t: time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)}
	for name, spoil := range map[string]func(*memBucket, Remote, string){
		"changed upload": func(bk *memBucket, r Remote, key string) { bk.objects[key][len(bk.objects[key])-1] ^= 1 },
		"missing upload": func(bk *memBucket, r Remote, key string) { delete(bk.objects, key) },
		"swapped uploads": func(bk *memBucket, r Remote, key string) {
			for k, v := range bk.objects {
				if strings.HasPrefix(k, r.Prefix+"media/") && k != key {
					bk.objects[key] = v
					return
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			bucket := newMemBucket()
			r := Remote{Bucket: bucket, Label: "mem://"}
			if _, err := CreateRemote(ctx, b.st, b.dir, r, 0, remoteOpts(c)); err != nil {
				t.Fatal(err)
			}
			spoil(bucket, r, bucket.keys("media/")[0])
			target, err := restoreRemote(t, r, "")
			if !errors.Is(err, ErrDamaged) {
				t.Errorf("err = %v, want ErrDamaged", err)
			}
			if _, err := os.Stat(filepath.Join(target, store.FileName)); !os.IsNotExist(err) {
				t.Error("a database was put in place")
			}
		})
	}
}

func TestRemoteSnapshotNeedsItsStorage(t *testing.T) {
	b := newBlog(t)
	r := Remote{Bucket: newMemBucket(), Label: "mem://"}
	c := &clock{t: time.Now()}
	if _, err := CreateRemote(context.Background(), b.st, b.dir, r, 0, remoteOpts(c)); err != nil {
		t.Fatal(err)
	}
	_, err := Restore(context.Background(), mustOpen(t, r, ""), func() (string, error) { return testPassphrase, nil },
		filepath.Join(t.TempDir(), "d"), RestoreOptions{})
	if err == nil || !strings.Contains(err.Error(), "restore it from there") {
		t.Errorf("remote snapshot without its storage: %v", err)
	}
	rc, src, err := r.Open(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	_, err = Restore(context.Background(), rc, func() (string, error) { return "wrong passphrase!!", nil },
		filepath.Join(t.TempDir(), "d"), RestoreOptions{Media: src})
	if !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong passphrase: %v", err)
	}
}

func TestRemoteLeavesChangedUploadsOut(t *testing.T) {
	b := newBlog(t)
	var name string
	for n := range b.files {
		name = n
		break
	}
	os.WriteFile(filepath.Join(b.dir, media.DirName, name), []byte("bit rot"), 0o600)
	bucket := newMemBucket()
	r := Remote{Bucket: bucket, Label: "mem://"}
	res, err := CreateRemote(context.Background(), b.st, b.dir, r, 0, remoteOpts(&clock{t: time.Now()}))
	if err != nil || len(res.Manifest.Damaged) != 1 || res.Manifest.Damaged[0] != name || res.Sent != 2 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if _, err := bucket.Get(context.Background(), r.mediaKey(name, true)); !errors.Is(err, s3.ErrNotFound) {
		t.Error("changed bytes were stored under the upload's content name")
	}
	if _, err := restoreRemote(t, r, ""); err != nil {
		t.Errorf("restore without the changed upload: %v", err)
	}
}

func TestRemoteSnapshotWithFilesInsideIsRefused(t *testing.T) {
	b := newBlog(t)
	data, _ := b.snapshot(t, "")
	// A carrying snapshot relabelled as a stored one: its uploads are both
	// inside and listed for fetching.
	odd := withManifest(t, data, func(m *Manifest) { m.Format = RemoteFormat })
	r := Remote{Bucket: newMemBucket(), Label: "mem://"}
	_, err := Restore(context.Background(), bytes.NewReader(odd), nil, filepath.Join(t.TempDir(), "d"),
		RestoreOptions{Media: remoteMedia{r: r, name: Name(time.Now(), true)}})
	if err == nil {
		t.Errorf("err = %v", err)
	}
}

// plantedSnapshot stores a plain snapshot of another blog in r under name, as
// someone with write access to the bucket could.
func plantedSnapshot(t *testing.T, bucket *memBucket, r Remote, name string, format int) {
	t.Helper()
	other := newBlog(t)
	data, _ := other.snapshot(t, "")
	data = withManifest(t, data, func(m *Manifest) { m.Format = format })
	bucket.objects[r.snapshotKey(name)] = data
	bucket.stored[r.snapshotKey(name)] = time.Now()
}

func TestRemoteRefusesPlantedSnapshots(t *testing.T) {
	b := newBlog(t)
	ctx := context.Background()
	bucket := newMemBucket()
	r := Remote{Bucket: bucket, Prefix: "blog/", Label: "mem://blog/"}
	c := &clock{t: time.Now().Add(-time.Hour)}
	real, err := CreateRemote(ctx, b.st, b.dir, r, 3, remoteOpts(c))
	if err != nil {
		t.Fatal(err)
	}

	// A plain one with a newer name is not listed, so not "the newest".
	plain := Name(time.Now(), false)
	plantedSnapshot(t, bucket, r, plain, RemoteFormat)
	if _, err := restoreRemote(t, r, plain); err == nil {
		t.Error("restored a plain snapshot from the bucket by name")
	}
	// One named as encrypted but plain inside is refused when read.
	fake := Name(time.Now(), true)
	plantedSnapshot(t, bucket, r, fake, RemoteFormat)
	if _, err := restoreRemote(t, r, ""); !errors.Is(err, ErrNotSealed) {
		t.Errorf("planted plain snapshot as the newest: %v", err)
	}
	delete(bucket.objects, r.snapshotKey(fake))

	// One dated far ahead is ignored, so it can neither be restored as the
	// newest nor make pruning delete the real ones.
	future := Name(time.Now().Add(365*24*time.Hour), true)
	bucket.objects[r.snapshotKey(future)] = bucket.objects[real.Key]
	list, _ := r.List(ctx)
	for _, s := range list {
		if s.Name == future {
			t.Error("a snapshot dated next year is listed")
		}
	}
	if _, err := restoreRemote(t, r, future); err == nil || !strings.Contains(err.Error(), "future") {
		t.Errorf("future snapshot by name: %v", err)
	}
	for range 3 {
		if _, err := CreateRemote(ctx, b.st, b.dir, r, 3, remoteOpts(c)); err != nil {
			t.Fatal(err)
		}
	}
	if list, _ := r.List(ctx); len(list) != 3 {
		t.Errorf("real snapshots after pruning with a planted future one = %d, want 3", len(list))
	}
	delete(bucket.objects, r.snapshotKey(future))

	// An older snapshot copied over a newer name: its sealed time says so.
	list, _ = r.List(ctx)
	bucket.objects[list[2].Key] = bucket.objects[list[0].Key]
	if _, err := restoreRemote(t, r, ""); !errors.Is(err, ErrDamaged) || !strings.Contains(err.Error(), "copied over") {
		t.Errorf("rolled-back snapshot: %v", err)
	}
}

func TestRemoteRefusesPlainUploads(t *testing.T) {
	b := newBlog(t)
	bucket := newMemBucket()
	r := Remote{Bucket: bucket, Label: "mem://"}
	if _, err := CreateRemote(context.Background(), b.st, b.dir, r, 0, remoteOpts(&clock{t: time.Now()})); err != nil {
		t.Fatal(err)
	}
	// Swap one stored upload for its plain bytes.
	for name, data := range b.files {
		bucket.objects[r.mediaKey(name, true)] = data
		break
	}
	if _, err := restoreRemote(t, r, ""); !errors.Is(err, ErrNotSealed) {
		t.Errorf("plain upload: %v", err)
	}
}

func TestOneSnapshotAtATime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no flock on Windows")
	}
	b := newBlog(t)
	unlock, err := store.LockSnapshots(b.dir)
	if err != nil {
		t.Fatal(err)
	}
	r := Remote{Bucket: newMemBucket(), Label: "mem://"}
	if _, err := CreateRemote(context.Background(), b.st, b.dir, r, 2, remoteOpts(&clock{t: time.Now()})); !errors.Is(err, ErrBusy) {
		t.Errorf("remote while another runs: %v", err)
	}
	if _, err := Folder(t.TempDir()).put(context.Background(), b.st, b.dir, 2, Options{}); !errors.Is(err, ErrBusy) {
		t.Errorf("folder while another runs: %v", err)
	}
	unlock()
	if _, err := CreateRemote(context.Background(), b.st, b.dir, r, 2, remoteOpts(&clock{t: time.Now()})); err != nil {
		t.Errorf("after the other finished: %v", err)
	}
}
