// Package snapshot writes and restores the complete state of one blog, its
// data directory: a consistent copy of the database (posts, every revision, accounts,
// two-factor secrets, token digests, tag labels, the audit log) and every
// uploaded file. It knows nothing about HTTP.
//
// A snapshot is a gzipped tar file holding burzblog.db, media/<name> for each
// upload and, last, manifest.json with the size and SHA-256 of every file. It
// can be encrypted with a passphrase (see crypt.go). Restoring checks every
// file and the database's integrity before anything in the data directory is
// touched.
package snapshot

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/weburz/burzblog/internal/media"
	"github.com/weburz/burzblog/internal/store"
)

// FormatVersion is the layout of a snapshot that carries its uploads.
// RemoteFormat is one stored in a bucket, whose uploads are stored once each
// beside it (see remote.go) and only listed in its manifest.
const (
	FormatVersion = 1
	RemoteFormat  = 2
)

const (
	manifestName = "manifest.json"
	mediaPrefix  = media.DirName + "/"
	// maxManifest bounds the manifest read into memory: a blog with a
	// hundred thousand uploads needs about 15 MB.
	maxManifest = 64 << 20
	// ReplacedPrefix starts the folder in <data>/backups that a forced
	// restore moves the replaced data into.
	ReplacedPrefix = "replaced-"
)

// Errors callers can branch on.
var (
	ErrNotSnapshot = errors.New("this is not a BurzBlog snapshot")
	// ErrNotEmpty means the data directory already holds a blog.
	ErrNotEmpty = errors.New("the data directory already holds a blog")
	// ErrServerRunning means a server is running on the data directory.
	ErrServerRunning = errors.New("BurzBlog is running with this data directory")
)

// File is one file in a snapshot.
type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest describes a snapshot. It is the last file in the archive.
type Manifest struct {
	Format    int       `json:"format"`
	CreatedAt time.Time `json:"createdAt"`
	// Version is the BurzBlog that wrote it, and Schema the database's.
	Version   string `json:"version"`
	Schema    int    `json:"schema"`
	Posts     int    `json:"posts"`
	Revisions int    `json:"revisions"`
	Accounts  int    `json:"accounts"`
	Database  File   `json:"database"`
	Media     []File `json:"media"`
	// Missing lists uploads the database records whose file was not in the
	// media folder when the snapshot was made.
	Missing []string `json:"missing,omitempty"`
	// Damaged lists uploads whose bytes no longer match the checksum taken
	// when they were uploaded. They are carried as they were found.
	Damaged []string `json:"damaged,omitempty"`
}

// MediaBytes is the size of every upload in the snapshot.
func (m *Manifest) MediaBytes() int64 {
	var n int64
	for _, f := range m.Media {
		n += f.Size
	}
	return n
}

// Options control Create.
type Options struct {
	// Passphrase encrypts the snapshot; empty leaves it plain.
	Passphrase string
	// Version is the BurzBlog version recorded in the manifest.
	Version string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Create writes a snapshot of the blog whose database is st and whose data
// directory is dataDir to w. It is safe while a server runs: the database is
// copied with VACUUM INTO first, and the uploads the copy records are added
// after it. Uploads are never changed once stored, so the two agree. A file
// the database records but storage lacks is listed in Missing, not fatal, so
// one lost file cannot stop every later backup.
func Create(ctx context.Context, st *store.Store, dataDir string, w io.Writer, opt Options) (*Manifest, error) {
	snap, cleanup, err := prepare(ctx, st, dataDir, opt)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	var key *sealKey
	if opt.Passphrase != "" {
		if key, err = newSealKey(opt.Passphrase); err != nil {
			return nil, err
		}
	}
	m := snap.manifest(FormatVersion)
	dir := filepath.Join(dataDir, media.DirName)
	err = writeArchive(w, key, m, snap.db, func(tw *tar.Writer) error {
		for _, rec := range snap.sum.Media {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !media.ValidName(rec.FileName) {
				m.Missing = append(m.Missing, rec.FileName)
				continue
			}
			f, err := addFile(tw, mediaPrefix+rec.FileName, filepath.Join(dir, rec.FileName), rec.CreatedAt)
			if errors.Is(err, fs.ErrNotExist) {
				m.Missing = append(m.Missing, rec.FileName)
				continue
			}
			if err != nil {
				return fmt.Errorf("add upload %s: %w", rec.FileName, err)
			}
			if f.SHA256 != rec.SHA256 {
				m.Damaged = append(m.Damaged, rec.FileName)
			}
			m.Media = append(m.Media, f)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// prepared is a consistent copy of the database, about to be archived.
type prepared struct {
	db      string // the copy
	work    string // its working folder in the data directory
	sum     *store.FileSummary
	at      time.Time
	version string
}

// prepare copies the database into a working folder in dataDir and checks
// the copy. cleanup removes the folder.
func prepare(ctx context.Context, st *store.Store, dataDir string, opt Options) (*prepared, func(), error) {
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	sweep(dataDir, ".snapshot-")
	work, err := os.MkdirTemp(dataDir, ".snapshot-")
	if err != nil {
		return nil, nil, fmt.Errorf("make a working folder in the data directory: %w", err)
	}
	cleanup := func() { os.RemoveAll(work) }
	dbCopy := filepath.Join(work, store.FileName)
	if err := st.CopyTo(ctx, dbCopy); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("copy the database (is the disk full?): %w", err)
	}
	sum, err := store.InspectFile(ctx, dbCopy)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("check the database copy: %w", err)
	}
	return &prepared{db: dbCopy, work: work, sum: sum, at: now().UTC(), version: opt.Version}, cleanup, nil
}

func (p *prepared) manifest(format int) *Manifest {
	return &Manifest{
		Format: format, CreatedAt: p.at, Version: p.version, Schema: p.sum.Schema,
		Posts: p.sum.Posts, Revisions: p.sum.Revisions, Accounts: p.sum.Accounts, Media: []File{},
	}
}

// writeArchive writes the gzipped tar, sealed when key is set: the database
// at dbPath, what addMedia adds, then m as the manifest, which addMedia may
// still fill in.
func writeArchive(w io.Writer, key *sealKey, m *Manifest, dbPath string, addMedia func(*tar.Writer) error) error {
	out := w
	var sealer io.WriteCloser
	if key != nil {
		var err error
		if sealer, err = key.sealTo(w); err != nil {
			return err
		}
		out = sealer
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	var err error
	if m.Database, err = addFile(tw, store.FileName, dbPath, m.CreatedAt); err != nil {
		return fmt.Errorf("add the database: %w", err)
	}
	if err := addMedia(tw); err != nil {
		return err
	}
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(manifest)), ModTime: m.CreatedAt,
		Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(manifest); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if sealer != nil {
		return sealer.Close()
	}
	return nil
}

// addFile adds the file at path to tw as name and returns its size and
// checksum. Nothing is written when the file cannot be opened.
func addFile(tw *tar.Writer, name, path string, modTime time.Time) (File, error) {
	f, err := os.Open(path)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s is not a regular file", path)
	}
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: modTime,
		Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := tw.WriteHeader(hdr); err != nil {
		return File{}, err
	}
	h := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(tw, h), f, info.Size()); err != nil {
		return File{}, fmt.Errorf("read %s: %w", path, err)
	}
	return File{Name: name, Size: info.Size(), SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// PassphraseFunc returns the passphrase of an encrypted snapshot. It is only
// called for an encrypted one.
type PassphraseFunc func() (string, error)

// Result is what Verify or Restore found.
type Result struct {
	Manifest  *Manifest
	Encrypted bool
	// Replaced is the folder a forced Restore moved the previous data into,
	// or "" when there was none.
	Replaced string
}

// Verify reads a whole snapshot into a temporary folder inside workDir,
// checks every file against the manifest and the database with SQLite's
// integrity check, and removes the folder again. A snapshot that verifies
// restores. src gives the uploads of a remote snapshot; nil for a file.
func Verify(ctx context.Context, r io.Reader, pass PassphraseFunc, src MediaSource, workDir string) (*Result, error) {
	sweep(workDir, ".verify-")
	stage, err := os.MkdirTemp(workDir, ".verify-")
	if err != nil {
		return nil, fmt.Errorf("make a working folder in %s: %w", workDir, err)
	}
	defer os.RemoveAll(stage)
	return unpack(ctx, r, pass, src, stage)
}

// RestoreOptions control Restore.
type RestoreOptions struct {
	// Force replaces a data directory that already holds a blog. The previous
	// database and uploads are moved to <data>/backups/replaced-<time>/, not
	// deleted.
	Force bool
	Now   func() time.Time
	// Finish runs on the restored directory while Restore still holds the
	// lock, so nothing else opens the database before it, for example to
	// upgrade it and record the restore in the audit log.
	Finish func(dataDir string, m *Manifest) error
	// Media gives the uploads of a remote snapshot; nil for a file.
	Media MediaSource
}

// Restore puts a snapshot into dataDir. It refuses while a server runs on the
// directory, unpacks and checks the whole snapshot first, and only then
// moves the database and uploads into place, so a bad snapshot leaves the
// directory as it was.
func Restore(ctx context.Context, r io.Reader, pass PassphraseFunc, dataDir string, opt RestoreOptions) (*Result, error) {
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("make the data directory: %w", err)
	}
	unlock, err := store.LockExclusive(dataDir)
	if errors.Is(err, store.ErrInUse) {
		return nil, ErrServerRunning
	}
	if err != nil {
		return nil, err
	}
	defer unlock()

	dbPath := filepath.Join(dataDir, store.FileName)
	mediaDir := filepath.Join(dataDir, media.DirName)
	existing, err := holdsBlog(dataDir)
	if err != nil {
		return nil, err
	}
	// Refuse before reading a possibly large snapshot for nothing.
	if existing && !opt.Force {
		return nil, ErrNotEmpty
	}

	sweep(dataDir, ".restore-")
	stage, err := os.MkdirTemp(dataDir, ".restore-")
	if err != nil {
		return nil, fmt.Errorf("make a working folder in the data directory: %w", err)
	}
	defer os.RemoveAll(stage)
	res, err := unpack(ctx, r, pass, opt.Media, stage)
	if err != nil {
		return nil, err
	}

	// Move the current data aside, every part that exists, keeping the WAL
	// files with their database.
	var moved [][2]string
	putBack := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			os.Rename(moved[i][1], moved[i][0])
		}
	}
	if existing {
		res.Replaced = filepath.Join(dataDir, store.BackupDir,
			ReplacedPrefix+now().UTC().Format("20060102T150405.000Z"))
		if err := os.MkdirAll(res.Replaced, 0o700); err != nil {
			return nil, fmt.Errorf("make %s: %w", res.Replaced, err)
		}
		for _, name := range []string{store.FileName, store.FileName + "-wal", store.FileName + "-shm", media.DirName} {
			from, to := filepath.Join(dataDir, name), filepath.Join(res.Replaced, name)
			if err := os.Rename(from, to); errors.Is(err, fs.ErrNotExist) {
				continue
			} else if err != nil {
				putBack()
				return nil, fmt.Errorf("move %s aside: %w", from, err)
			}
			moved = append(moved, [2]string{from, to})
		}
	}
	syncDir(filepath.Join(stage, media.DirName))
	// An empty media folder left by a server that never got an upload is
	// not a blog; remove it so the rename works on Windows too.
	os.Remove(mediaDir)
	if err := os.Rename(filepath.Join(stage, store.FileName), dbPath); err != nil {
		putBack()
		return nil, fmt.Errorf("put the database in place: %w", err)
	}
	if err := os.Rename(filepath.Join(stage, media.DirName), mediaDir); err != nil {
		os.Remove(dbPath)
		putBack()
		return nil, fmt.Errorf("put the uploads in place: %w", err)
	}
	syncDir(dataDir)
	var finishErr error
	if opt.Finish != nil {
		finishErr = opt.Finish(dataDir, res.Manifest)
	}
	// Last, so files Finish made (an upgrade backup) are covered too.
	if err := matchOwner(dataDir, dbPath, mediaDir, filepath.Join(dataDir, store.LockFile),
		filepath.Join(dataDir, store.BackupDir)); err != nil {
		return nil, err
	}
	return res, finishErr
}

// staleAfter is how old a working folder or partial file must be before a
// later run removes it: one left by a run that was killed, never one that
// another run is still using.
const staleAfter = 24 * time.Hour

// sweep removes working folders named prefix* in dir that a killed run left
// behind. They can hold a copy of the database, password hashes included.
func sweep(dir, prefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleAfter {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

// SweepPartial removes snapshot files in dir that a killed create left
// half-written (Name plus ".partial") more than a day ago.
func SweepPartial(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".partial")
		if !ok || !nameRE.MatchString(name) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleAfter {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// syncDir flushes a directory's entries, so renames into it survive a power
// cut. Where directories cannot be synced (Windows) it does nothing.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}

// holdsBlog reports whether a data directory has a database (or the WAL files
// of one) or any upload.
func holdsBlog(dataDir string) (bool, error) {
	for _, name := range []string{store.FileName, store.FileName + "-wal", store.FileName + "-shm"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); err == nil {
			return true, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, media.DirName))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

// opener decrypts the snapshot and, for a remote one, its uploads, asking
// for the passphrase once and deriving each key once.
type openers struct {
	pass PassphraseFunc
	ring *keyring
}

func (o *openers) keyring() (*keyring, error) {
	if o.ring != nil {
		return o.ring, nil
	}
	if o.pass == nil {
		return nil, ErrNeedPassphrase
	}
	p, err := o.pass()
	if err != nil {
		return nil, err
	}
	if p == "" {
		return nil, ErrNeedPassphrase
	}
	o.ring = newKeyring(p)
	return o.ring, nil
}

// open returns the plain content of r, decrypting it when needed.
func (o *openers) open(r io.Reader) (io.Reader, bool, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	if !isEncrypted(br) {
		return br, false, nil
	}
	ring, err := o.keyring()
	if err != nil {
		return nil, true, err
	}
	plain, err := ring.open(br)
	return plain, true, err
}

// unpack reads a snapshot into stage, an empty folder, and checks it whole:
// only the names a snapshot holds, each once, every size and checksum as the
// manifest says, and a database that passes SQLite's integrity check at a
// schema this BurzBlog can run.
func unpack(ctx context.Context, r io.Reader, pass PassphraseFunc, src MediaSource, stage string) (*Result, error) {
	keys := &openers{pass: pass}
	plain, encrypted, err := keys.open(r)
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(plain)
	if err != nil {
		if encrypted {
			return nil, ErrDamaged
		}
		return nil, ErrNotSnapshot
	}
	if err := os.Mkdir(filepath.Join(stage, media.DirName), 0o700); err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	got := map[string]File{}
	var m *Manifest
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, damaged(err)
		}
		if m != nil {
			return nil, fmt.Errorf("%w: %q follows the manifest", ErrDamaged, hdr.Name)
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Size < 0 {
			return nil, fmt.Errorf("%w: %q is not a plain file", ErrDamaged, hdr.Name)
		}
		if _, dup := got[hdr.Name]; dup {
			return nil, fmt.Errorf("%w: %q appears twice", ErrDamaged, hdr.Name)
		}
		var path string
		switch name := hdr.Name; {
		case name == manifestName:
			if hdr.Size > maxManifest {
				return nil, fmt.Errorf("%w: the manifest is too large", ErrDamaged)
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, damaged(err)
			}
			m = &Manifest{}
			if err := json.Unmarshal(data, m); err != nil {
				return nil, fmt.Errorf("%w: the manifest cannot be read: %v", ErrDamaged, err)
			}
			continue
		case name == store.FileName:
			path = filepath.Join(stage, store.FileName)
		case strings.HasPrefix(name, mediaPrefix) && media.ValidName(strings.TrimPrefix(name, mediaPrefix)):
			path = filepath.Join(stage, media.DirName, strings.TrimPrefix(name, mediaPrefix))
		default:
			if len(got) == 0 {
				return nil, ErrNotSnapshot
			}
			return nil, fmt.Errorf("%w: unexpected file %q", ErrDamaged, name)
		}
		f, err := extract(tr, path, hdr)
		if err != nil {
			return nil, err
		}
		got[hdr.Name] = f
	}
	// Read to the very end, so gzip checks its checksum and an encrypted
	// snapshot its last chunk: a file cut short after the tar fails here.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return nil, damaged(err)
	}
	if _, err := io.Copy(io.Discard, plain); err != nil {
		return nil, damaged(err)
	}
	if m == nil {
		if len(got) == 0 {
			return nil, ErrNotSnapshot
		}
		return nil, fmt.Errorf("%w: the manifest is missing", ErrDamaged)
	}
	if m.Format != FormatVersion && m.Format != RemoteFormat {
		return nil, fmt.Errorf("the snapshot has format %d; this BurzBlog reads formats %d and %d, so restore it with the BurzBlog version that made it (%s) or newer",
			m.Format, FormatVersion, RemoteFormat, m.Version)
	}
	if src != nil {
		// Only ever stored by BurzBlog, encrypted, under its own time.
		if !encrypted {
			return nil, fmt.Errorf("the snapshot in the storage: %w", ErrNotSealed)
		}
		if m.Format != RemoteFormat {
			return nil, fmt.Errorf("%w: it is not a stored snapshot", ErrDamaged)
		}
		if want := Name(m.CreatedAt, true); want != src.Snapshot() {
			return nil, fmt.Errorf("%w: it is stored as %s but was made as %s; an older snapshot may have been copied over a newer one",
				ErrDamaged, src.Snapshot(), want)
		}
	}
	if m.Format == RemoteFormat {
		if src == nil {
			return nil, errors.New("this snapshot keeps its uploads in the storage it came from; restore it from there (burzblog snapshot restore s3://...)")
		}
		for _, f := range m.Media {
			if !strings.HasPrefix(f.Name, mediaPrefix) || !media.ValidName(strings.TrimPrefix(f.Name, mediaPrefix)) {
				return nil, fmt.Errorf("%w: the manifest lists %q wrongly", ErrDamaged, f.Name)
			}
			if _, dup := got[f.Name]; dup {
				return nil, fmt.Errorf("%w: the manifest lists %q twice", ErrDamaged, f.Name)
			}
			g, err := fetchMedia(ctx, src, keys, f.Name, filepath.Join(stage, media.DirName, strings.TrimPrefix(f.Name, mediaPrefix)))
			if err != nil {
				return nil, err
			}
			got[f.Name] = g
		}
	}

	want := map[string]File{store.FileName: {Name: store.FileName, Size: m.Database.Size, SHA256: m.Database.SHA256}}
	if m.Database.Name != store.FileName {
		return nil, fmt.Errorf("%w: the manifest names no database", ErrDamaged)
	}
	for _, f := range m.Media {
		if _, dup := want[f.Name]; dup || !strings.HasPrefix(f.Name, mediaPrefix) {
			return nil, fmt.Errorf("%w: the manifest lists %q wrongly", ErrDamaged, f.Name)
		}
		want[f.Name] = f
	}
	if len(got) != len(want) {
		return nil, fmt.Errorf("%w: it holds %d files, the manifest lists %d", ErrDamaged, len(got), len(want))
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s is missing", ErrDamaged, name)
		}
		if g.Size != w.Size || g.SHA256 != w.SHA256 {
			return nil, fmt.Errorf("%w: %s does not match its checksum", ErrDamaged, name)
		}
	}
	sum, err := store.InspectFile(ctx, filepath.Join(stage, store.FileName))
	if err != nil {
		return nil, err
	}
	if sum.Schema != m.Schema {
		return nil, fmt.Errorf("%w: the database is at schema %d, the manifest says %d", ErrDamaged, sum.Schema, m.Schema)
	}
	return &Result{Manifest: m, Encrypted: encrypted}, nil
}

// extract writes the current tar entry to path, a new file, and returns its
// size and checksum.
func extract(tr *tar.Reader, path string, hdr *tar.Header) (File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return File{}, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), tr)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return File{}, damaged(err)
	}
	if n != hdr.Size {
		return File{}, fmt.Errorf("%w: %s is cut short", ErrDamaged, hdr.Name)
	}
	return File{Name: hdr.Name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// damaged keeps ErrDamaged and ErrPassphrase as they are and wraps any other
// read error in ErrDamaged, unless it is a local disk problem.
func damaged(err error) error {
	var pathErr *fs.PathError
	switch {
	case errors.Is(err, ErrDamaged), errors.Is(err, ErrPassphrase), errors.As(err, &pathErr):
		return err
	}
	return fmt.Errorf("%w: %v", ErrDamaged, err)
}

// nameRE matches the files Name produces, so Prune never touches others.
var nameRE = regexp.MustCompile(`^burzblog-snapshot-(\d{8}T\d{6}\.\d{3}Z)\.tar\.gz(\.enc)?$`)

// Name is the file name for a snapshot made at t.
func Name(t time.Time, encrypted bool) string {
	name := "burzblog-snapshot-" + t.UTC().Format("20060102T150405.000Z") + ".tar.gz"
	if encrypted {
		name += ".enc"
	}
	return name
}

// Prune deletes all but the newest keep snapshots in dir, judged by the time
// in their names, and returns the names it deleted. Files Name did not make
// are never touched.
func Prune(dir string, keep int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && nameRE.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return nameRE.FindStringSubmatch(names[i])[1] < nameRE.FindStringSubmatch(names[j])[1]
	})
	var deleted []string
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return deleted, err
		}
		deleted = append(deleted, names[0])
		names = names[1:]
	}
	return deleted, nil
}

// IsName reports whether s is a snapshot file name that Name makes.
func IsName(s string) bool { return nameRE.MatchString(s) }
