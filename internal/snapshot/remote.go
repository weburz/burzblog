package snapshot

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/weburz/burzblog/internal/media"
	"github.com/weburz/burzblog/internal/s3"
	"github.com/weburz/burzblog/internal/store"
)

// A remote snapshot lives in a bucket under a prefix, one prefix per blog:
//
//	<prefix>snapshots/<Name>            the database and manifest (RemoteFormat)
//	<prefix>snapshots/<Name>.manifest   the manifest alone, for pruning
//	<prefix>media/<upload name>[.enc]   each upload, stored once
//
// Uploads are named by their content, so one already in the bucket is never
// sent again: after the first run a night's snapshot sends the database and
// only the new uploads. Everything is encrypted when the snapshot is, each
// file under its own key.

// Bucket is the storage a remote snapshot uses; *s3.Client is one.
type Bucket interface {
	Put(ctx context.Context, key string, body io.ReaderAt, size int64, sum string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]s3.Object, error)
	Delete(ctx context.Context, key string) error
}

// Remote is a place for snapshots in a bucket.
type Remote struct {
	Bucket Bucket
	// Prefix is "" or ends with "/".
	Prefix string
	// Label names it in messages, such as s3://bucket/blog/.
	Label string
}

func (r Remote) String() string { return r.Label }

func (r Remote) snapshotKey(name string) string { return r.Prefix + "snapshots/" + name }
func (r Remote) mediaKey(name string, encrypted bool) string {
	key := r.Prefix + "media/" + name
	if encrypted {
		key += ".enc"
	}
	return key
}

// MediaSource gives the stored uploads of the remote snapshot being read.
type MediaSource interface {
	// Fetch opens the stored, encrypted upload named name (as in
	// media/<name>).
	Fetch(ctx context.Context, name string) (io.ReadCloser, error)
	// Snapshot is the name the snapshot was stored under, which its sealed
	// manifest must agree with.
	Snapshot() string
}

// ErrNotSealed means something in object storage was not encrypted.
// BurzBlog only stores encrypted snapshots there, so a plain one was put
// there by someone else and is refused.
var ErrNotSealed = errors.New("it is not encrypted, so BurzBlog did not store it; refusing it")

// remoteMedia fetches the uploads of one stored snapshot.
type remoteMedia struct {
	r    Remote
	name string
}

func (m remoteMedia) Snapshot() string { return m.name }

func (m remoteMedia) Fetch(ctx context.Context, name string) (io.ReadCloser, error) {
	rc, err := m.r.Bucket.Get(ctx, m.r.mediaKey(name, true))
	if errors.Is(err, s3.ErrNotFound) {
		return nil, fmt.Errorf("%w: upload %s is missing from the storage", ErrDamaged, name)
	}
	return rc, err
}

// fetchMedia writes the upload name (media/<file>) from src to path,
// decrypting it when it was stored encrypted, and returns its size and
// checksum.
func fetchMedia(ctx context.Context, src MediaSource, keys *openers, name, path string) (File, error) {
	rc, err := src.Fetch(ctx, strings.TrimPrefix(name, mediaPrefix))
	if err != nil {
		return File{}, err
	}
	defer rc.Close()
	plain, sealed, err := keys.open(rc)
	if err != nil {
		return File{}, err
	}
	if !sealed {
		return File{}, fmt.Errorf("upload %s in the storage: %w", name, ErrNotSealed)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return File{}, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), plain)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return File{}, damaged(err)
	}
	return File{Name: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// RemoteResult is what CreateRemote did.
type RemoteResult struct {
	Manifest *Manifest
	// Key is the snapshot's key in the bucket.
	Key string
	// Size is the stored snapshot's size, without the uploads.
	Size int64
	// Sent counts the uploads sent this time and their stored bytes; the
	// others were in the bucket already.
	Sent      int
	SentBytes int64
	// Pruned lists the snapshots deleted to keep the newest Keep.
	Pruned []string
	// PruneWarning says why old uploads were not cleaned up, if they were not.
	PruneWarning string
}

// CreateRemote stores a snapshot in r: the uploads the bucket lacks first,
// then the database and manifest, so a stored snapshot never names an
// upload that is not there. Remote snapshots must be encrypted. With keep
// above 0 it then deletes all but the newest keep snapshots and the uploads
// none of those still names.
func CreateRemote(ctx context.Context, st *store.Store, dataDir string, r Remote, keep int, opt Options) (*RemoteResult, error) {
	if opt.Passphrase == "" {
		return nil, errors.New("snapshots stored off the server must be encrypted: set BURZBLOG_SNAPSHOT_PASSPHRASE")
	}
	unlock, err := lockRuns(dataDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	key, err := newSealKey(opt.Passphrase)
	if err != nil {
		return nil, err
	}
	snap, cleanup, err := prepare(ctx, st, dataDir, opt)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	stored, err := r.Bucket.List(ctx, r.Prefix+"media/")
	if err != nil {
		return nil, fmt.Errorf("list the uploads in %s: %w", r, err)
	}
	have := map[string]bool{}
	for _, o := range stored {
		have[o.Key] = true
	}

	res := &RemoteResult{}
	m := snap.manifest(RemoteFormat)
	dir := filepath.Join(dataDir, media.DirName)
	for _, rec := range snap.sum.Media {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := File{Name: mediaPrefix + rec.FileName, Size: rec.Size, SHA256: rec.SHA256}
		if !media.ValidName(rec.FileName) {
			m.Missing = append(m.Missing, rec.FileName)
			continue
		}
		if have[r.mediaKey(rec.FileName, true)] {
			m.Media = append(m.Media, entry)
			continue
		}
		sent, err := sendUpload(ctx, r, key, filepath.Join(dir, rec.FileName), snap.work, rec)
		switch {
		case errors.Is(err, os.ErrNotExist):
			m.Missing = append(m.Missing, rec.FileName)
			continue
		case errors.Is(err, errChanged):
			// Stored names promise their content; bytes that changed on
			// disk are left out instead of stored under a false name.
			m.Damaged = append(m.Damaged, rec.FileName)
			continue
		case err != nil:
			return nil, fmt.Errorf("store upload %s: %w", rec.FileName, err)
		}
		res.Sent++
		res.SentBytes += sent
		m.Media = append(m.Media, entry)
	}

	name := Name(snap.at, true)
	archive := filepath.Join(snap.work, name)
	f, err := os.OpenFile(archive, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := writeArchive(f, key, m, snap.db, func(*tar.Writer) error { return nil }); err != nil {
		return nil, err
	}
	if res.Size, err = putFile(ctx, r.Bucket, r.snapshotKey(name), f); err != nil {
		return nil, fmt.Errorf("store the snapshot in %s: %w", r, err)
	}
	var sealed bytes.Buffer
	if err := sealJSON(&sealed, key, m); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(sealed.Bytes())
	if err := r.Bucket.Put(ctx, r.snapshotKey(name)+".manifest", bytes.NewReader(sealed.Bytes()), int64(sealed.Len()), hex.EncodeToString(sum[:])); err != nil {
		return nil, fmt.Errorf("store the snapshot's manifest in %s: %w", r, err)
	}
	res.Manifest, res.Key = m, r.snapshotKey(name)

	if keep > 0 {
		pruned, warning, err := pruneRemote(ctx, r, keep, opt.Passphrase)
		res.Pruned, res.PruneWarning = pruned, warning
		if err != nil {
			return res, fmt.Errorf("the snapshot was stored, but deleting older ones failed: %w", err)
		}
	}
	return res, nil
}

var errChanged = errors.New("the upload changed on disk since it was uploaded")

// uploadGrace is how long a stored upload no kept snapshot names survives.
const uploadGrace = 24 * time.Hour

// lockRuns takes the snapshot lock of dataDir, so two snapshots made with it
// never prune under each other.
func lockRuns(dataDir string) (func(), error) {
	unlock, err := store.LockSnapshots(dataDir)
	if errors.Is(err, store.ErrInUse) {
		return nil, ErrBusy
	}
	return unlock, err
}

// ErrBusy means another snapshot is being made with the data directory.
var ErrBusy = errors.New("another snapshot is being made; try again when it has finished")

// sendUpload seals the upload at path into a file in work, checks the bytes
// against rec's checksum and stores them. It returns the stored size.
func sendUpload(ctx context.Context, r Remote, key *sealKey, path, work string, rec store.Media) (int64, error) {
	in, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(work, "upload-*")
	if err != nil {
		return 0, err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()
	w, err := key.sealTo(tmp)
	if err != nil {
		return 0, err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), in); err != nil {
		return 0, err
	}
	if err := w.Close(); err != nil {
		return 0, err
	}
	if hex.EncodeToString(h.Sum(nil)) != rec.SHA256 {
		return 0, errChanged
	}
	return putFile(ctx, r.Bucket, r.mediaKey(rec.FileName, true), tmp)
}

// putFile stores the whole of f as key and returns its size.
func putFile(ctx context.Context, b Bucket, key string, f *os.File) (int64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return 0, err
	}
	return size, b.Put(ctx, key, f, size, hex.EncodeToString(h.Sum(nil)))
}

func sealJSON(w io.Writer, key *sealKey, v any) error {
	sw, err := key.sealTo(w)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(sw).Encode(v); err != nil {
		return err
	}
	return sw.Close()
}

// StoredSnapshot is a snapshot in a bucket.
type StoredSnapshot struct {
	Name string
	Key  string
	Size int64
}

// futureSlack is how far ahead of this clock a snapshot's time may be
// before its name is treated as planted: one far in the future would stay
// "the newest" for ever and make pruning delete every real one.
const futureSlack = time.Hour

// nameTime is the time in a snapshot name.
func nameTime(name string) (time.Time, bool) {
	m := nameRE.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102T150405.000Z", m[1])
	return t, err == nil
}

// List returns the encrypted snapshots in r, oldest first. Names that are
// not snapshots, not encrypted or dated in the future are left out.
func (r Remote) List(ctx context.Context) ([]StoredSnapshot, error) {
	objects, err := r.Bucket.List(ctx, r.Prefix+"snapshots/")
	if err != nil {
		return nil, fmt.Errorf("list the snapshots in %s: %w", r, err)
	}
	var out []StoredSnapshot
	limit := time.Now().Add(futureSlack)
	for _, o := range objects {
		name := strings.TrimPrefix(o.Key, r.Prefix+"snapshots/")
		if t, ok := nameTime(name); ok && strings.HasSuffix(name, ".enc") && t.Before(limit) {
			out = append(out, StoredSnapshot{Name: name, Key: o.Key, Size: o.Size})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return nameRE.FindStringSubmatch(out[i].Name)[1] < nameRE.FindStringSubmatch(out[j].Name)[1]
	})
	return out, nil
}

// Open opens the stored snapshot name, or the newest when name is "", and
// returns it with the source of its uploads for Verify or Restore.
func (r Remote) Open(ctx context.Context, name string) (io.ReadCloser, MediaSource, error) {
	if name == "" {
		list, err := r.List(ctx)
		if err != nil {
			return nil, nil, err
		}
		if len(list) == 0 {
			return nil, nil, fmt.Errorf("there are no snapshots in %s", r)
		}
		name = list[len(list)-1].Name
	}
	if t, ok := nameTime(name); !ok || !strings.HasSuffix(name, ".enc") {
		return nil, nil, fmt.Errorf("%q is not the name of a snapshot BurzBlog stores (burzblog-snapshot-<time>.tar.gz.enc)", name)
	} else if t.After(time.Now().Add(futureSlack)) {
		return nil, nil, fmt.Errorf("%s is dated in the future; refusing it", name)
	}
	rc, err := r.Bucket.Get(ctx, r.snapshotKey(name))
	if errors.Is(err, s3.ErrNotFound) {
		return nil, nil, fmt.Errorf("there is no snapshot %s in %s", name, r)
	}
	if err != nil {
		return nil, nil, err
	}
	return rc, remoteMedia{r: r, name: name}, nil
}

// pruneRemote deletes all but the newest keep snapshots in r, then every
// stored upload that no kept snapshot names. When a kept manifest cannot be
// read, no upload is deleted and the warning says why: deleting on a partial
// list could break a snapshot.
func pruneRemote(ctx context.Context, r Remote, keep int, passphrase string) ([]string, string, error) {
	list, err := r.List(ctx)
	if err != nil {
		return nil, "", err
	}
	var pruned []string
	for len(list) > keep {
		old := list[0]
		if err := r.Bucket.Delete(ctx, old.Key); err != nil {
			return pruned, "", err
		}
		if err := r.Bucket.Delete(ctx, old.Key+".manifest"); err != nil {
			return pruned, "", err
		}
		pruned = append(pruned, old.Name)
		list = list[1:]
	}

	ring := newKeyring(passphrase)
	needed := map[string]bool{}
	for _, s := range list {
		m, err := readManifest(ctx, r, s.Key+".manifest", ring)
		if err != nil {
			return pruned, fmt.Sprintf("old uploads were not cleaned up: the manifest of %s cannot be read (%v)", s.Name, err), nil
		}
		for _, f := range m.Media {
			needed[strings.TrimPrefix(f.Name, mediaPrefix)] = true
		}
	}
	stored, err := r.Bucket.List(ctx, r.Prefix+"media/")
	if err != nil {
		return pruned, "", err
	}
	for _, o := range stored {
		name := strings.TrimSuffix(strings.TrimPrefix(o.Key, r.Prefix+"media/"), ".enc")
		// A day's grace: an upload a run on another machine just sent is not
		// named by a stored manifest yet.
		if !needed[name] && media.ValidName(name) && time.Since(o.LastModified) > uploadGrace {
			if err := r.Bucket.Delete(ctx, o.Key); err != nil {
				return pruned, "", err
			}
		}
	}
	return pruned, "", nil
}

func readManifest(ctx context.Context, r Remote, key string, ring *keyring) (*Manifest, error) {
	rc, err := r.Bucket.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	br := bufio.NewReader(io.LimitReader(rc, maxManifest))
	var in io.Reader = br
	if isEncrypted(br) {
		if in, err = ring.open(br); err != nil {
			return nil, err
		}
	}
	var m Manifest
	if err := json.NewDecoder(in).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}
