package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/snapshot"
	"github.com/weburz/burzblog/internal/store"
)

// snapshotDir is the folder in the data directory that snapshot create writes
// to when given no target.
const snapshotDir = "snapshots"

// snapshotLine is a snapshot as `burzblog snapshot ... --json` prints it.
type snapshotLine struct {
	Path        string    `json:"path,omitempty"`
	Size        int64     `json:"size,omitempty"`
	Encrypted   bool      `json:"encrypted"`
	CreatedAt   time.Time `json:"createdAt"`
	Version     string    `json:"version"`
	Schema      int       `json:"schema"`
	Posts       int       `json:"posts"`
	Revisions   int       `json:"revisions"`
	Accounts    int       `json:"accounts"`
	Uploads     int       `json:"uploads"`
	UploadBytes int64     `json:"uploadBytes"`
	Missing     []string  `json:"missing"`
	Damaged     []string  `json:"damaged"`
	Pruned      []string  `json:"pruned,omitempty"`
	Replaced    string    `json:"replaced,omitempty"`
}

func newSnapshotLine(m *snapshot.Manifest, encrypted bool) snapshotLine {
	l := snapshotLine{Encrypted: encrypted, CreatedAt: m.CreatedAt, Version: m.Version, Schema: m.Schema,
		Posts: m.Posts, Revisions: m.Revisions, Accounts: m.Accounts, Uploads: len(m.Media),
		UploadBytes: m.MediaBytes(), Missing: m.Missing, Damaged: m.Damaged}
	if l.Missing == nil {
		l.Missing = []string{}
	}
	if l.Damaged == nil {
		l.Damaged = []string{}
	}
	return l
}

func (a *App) snapshotCmd() *cobra.Command {
	var data, passFile string
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Back up or restore the whole server: posts, history, accounts, uploads (run on the host)",
		Long: `A snapshot is one file with everything the server holds: every post and every
earlier version of it, accounts with their passwords, two-factor and roles,
tokens, tag labels, the audit log and every uploaded file. It is made while
BurzBlog runs and restores onto a new server with the same addresses.

A snapshot on the server's own disk is lost with that disk: copy it to another
machine or to storage elsewhere. Snapshots hold password hashes and two-factor
secrets, so encrypt them when they leave the server: set
BURZBLOG_SNAPSHOT_PASSPHRASE or pass --passphrase-file, or use --encrypt to
type a passphrase. Without the passphrase an encrypted snapshot cannot be
restored, by anyone.`,
		Example: `  burzblog snapshot create --data /var/lib/burzblog
  burzblog snapshot create /mnt/backup --keep 14 --passphrase-file /etc/burzblog/passphrase
  docker compose exec -T burzblog /burzblog snapshot create - > blog.tar.gz
  burzblog snapshot verify blog.tar.gz
  burzblog snapshot restore blog.tar.gz --data /var/lib/burzblog`,
	}
	cmd.PersistentFlags().StringVar(&data, "data", "", "data directory (default $BURZBLOG_DATA, $STATE_DIRECTORY or ./burzblog-data)")
	cmd.PersistentFlags().StringVar(&passFile, "passphrase-file", "", "read the passphrase from this file (default $BURZBLOG_SNAPSHOT_PASSPHRASE)")

	// givenPassphrase is the passphrase from --passphrase-file or the
	// environment, or "" when neither is set.
	givenPassphrase := func() (string, error) {
		if passFile != "" {
			b, err := os.ReadFile(passFile)
			if err != nil {
				return "", fmt.Errorf("read the passphrase file: %w", err)
			}
			p := strings.TrimRight(string(b), "\r\n")
			if p == "" {
				return "", fmt.Errorf("the passphrase file %s is empty", passFile)
			}
			return p, nil
		}
		return a.Getenv("BURZBLOG_SNAPSHOT_PASSPHRASE"), nil
	}
	// askPassphrase returns a PassphraseFunc for reading a snapshot: the given
	// passphrase, else one typed at the terminal unless the snapshot itself
	// comes from standard input.
	askPassphrase := func(fromStdin bool) snapshot.PassphraseFunc {
		return func() (string, error) {
			p, err := givenPassphrase()
			if err != nil || p != "" {
				return p, err
			}
			if fromStdin || !a.terminal() {
				return "", errors.New("the snapshot is encrypted; set BURZBLOG_SNAPSHOT_PASSPHRASE or pass --passphrase-file")
			}
			return a.secret("Passphrase: ")
		}
	}
	openSnapshot := func(name string) (io.ReadCloser, error) {
		if name == "-" {
			return io.NopCloser(a.In), nil
		}
		f, err := os.Open(name)
		if err != nil {
			return nil, fmt.Errorf("open the snapshot: %w", err)
		}
		return f, nil
	}

	var encrypt bool
	var keep int
	create := &cobra.Command{
		Use:   "create [file | folder | -]",
		Short: "Write a snapshot of the whole server, safely while it runs",
		Long: `Writes a snapshot of the server to a file. A target ending in .gz or .enc is
the file to write. Anything else is a folder, made when missing; given one, or
nothing (the snapshots folder in the data directory), it names the file
burzblog-snapshot-<UTC time>.tar.gz, with .enc added when encrypted; - writes
to standard output. --keep N then deletes all but the newest N snapshots in
that folder, touching no other file.

The database is copied consistently while BurzBlog keeps running and writing.
An upload the database records but the media folder lacks is listed and
skipped, so one lost file does not stop backups. Run it from cron or a
systemd timer, then copy the file off the server.`,
		Example: `  burzblog snapshot create
  burzblog snapshot create /mnt/backup --keep 14
  BURZBLOG_SNAPSHOT_PASSPHRASE=... burzblog snapshot create - | ssh backup 'cat > blog.tar.gz.enc'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			toStdout := target == "-"
			if toStdout && a.asJSON {
				return usagef("--json prints to standard output, which - uses for the snapshot; write to a file instead")
			}
			if toStdout && a.outTerminal() {
				return usagef("a snapshot is binary; send it to a file (> blog.tar.gz) or another program")
			}
			if keep < 0 {
				return usagef("--keep must be 0 (keep every snapshot) or more")
			}
			pass, err := givenPassphrase()
			if err != nil {
				return err
			}
			if encrypt && pass == "" {
				if !a.terminal() {
					return usagef("--encrypt asks for a passphrase at a terminal; without one, set BURZBLOG_SNAPSHOT_PASSPHRASE or pass --passphrase-file")
				}
				if pass, err = a.secret("Passphrase for the snapshot: "); err != nil {
					return errors.New("enter a passphrase")
				}
				if again, err := a.secret("Repeat it: "); err != nil || again != pass {
					return errors.New("the passphrases do not match")
				}
			}
			if pass != "" && len(pass) < snapshot.MinPassphrase {
				return usagef("the passphrase must be at least %d characters", snapshot.MinPassphrase)
			}
			encrypted := pass != ""

			dir := a.dataDir(data)
			if _, err := os.Stat(filepath.Join(dir, store.FileName)); errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("there is no BurzBlog database in %s; pass the data directory with --data", dir)
			}
			now := time.Now().UTC()
			path, inFolder := "", false
			if !toStdout {
				if target == "" {
					target = filepath.Join(dir, snapshotDir)
				}
				if info, err := os.Stat(target); (err == nil && info.IsDir()) || !snapshotFileName(target) {
					if err := os.MkdirAll(target, 0o700); err != nil {
						return fmt.Errorf("make the snapshot folder: %w", err)
					}
					path, inFolder = filepath.Join(target, snapshot.Name(now, encrypted)), true
				} else {
					path = target
				}
				if _, err := os.Stat(path); err == nil {
					return conflictf("%s already exists; choose another name", path)
				}
			}
			if keep > 0 && !inFolder {
				return usagef("--keep works on a folder of snapshots; give a folder, not a file")
			}

			// Never migrate here: a newer binary must not change the schema
			// under the server that is running.
			ctx, stop := interruptible()
			defer stop()
			st, err := store.OpenExisting(ctx, dir)
			if err != nil {
				return err
			}
			defer st.Close()
			opts := snapshot.Options{Passphrase: pass, Version: a.Version, Now: func() time.Time { return now }}
			var m *snapshot.Manifest
			var size int64
			if toStdout {
				if m, err = snapshot.Create(ctx, st, dir, a.Out, opts); err != nil {
					return err
				}
			} else {
				if inFolder {
					snapshot.SweepPartial(target)
				}
				if m, size, err = writeSnapshot(ctx, st, dir, path, opts); err != nil {
					return err
				}
			}

			what := "to standard output"
			if !toStdout {
				what = filepath.Base(path)
			}
			if encrypted {
				what += ", encrypted"
			}
			// The snapshot exists either way; a database too old for the
			// audit log (or a full disk) only costs the entry.
			auditErr := hostAudit(st, "snapshot.create", what)
			var pruned []string
			if keep > 0 {
				if pruned, err = snapshot.Prune(target, keep); err != nil {
					return fmt.Errorf("the snapshot was written, but deleting older ones failed: %w", err)
				}
			}

			if auditErr != nil {
				fmt.Fprintf(a.Err, "%s %v\n", a.paint(yellow, "warning:"), auditErr)
			}
			if a.asJSON {
				l := newSnapshotLine(m, encrypted)
				l.Path, l.Size, l.Pruned = path, size, pruned
				return a.emit(l)
			}
			out := a.Out
			if toStdout {
				out = a.Err
				fmt.Fprintf(out, "%s Wrote the snapshot to standard output.\n", a.paint(green, "✓"))
			} else {
				fmt.Fprintf(out, "%s Wrote %s (%s).\n", a.paint(green, "✓"), path, sizeText(size))
			}
			a.printSnapshotSummary(out, m, encrypted)
			if len(pruned) > 0 {
				fmt.Fprintf(out, "  Deleted %s.\n", count(len(pruned), "older snapshot", "older snapshots"))
			}
			if !toStdout {
				fmt.Fprintln(out, a.paint(dim, "  It is on this server's disk: copy it somewhere else, or it is lost with the server."))
			}
			return nil
		},
	}
	create.Flags().BoolVar(&encrypt, "encrypt", false, "ask for a passphrase and encrypt the snapshot")
	create.Flags().IntVar(&keep, "keep", 0, "keep only the newest N snapshots in the folder (default: keep all)")

	var work string
	verify := &cobra.Command{
		Use:   "verify <file | ->",
		Short: "Check that a snapshot is complete and would restore",
		Long: `Reads the whole snapshot, checks every file against its checksum and the
database with SQLite's integrity check, and reports what it holds. Nothing is
restored. It unpacks into the system's temporary folder, or the folder --work
names, which needs room for the whole snapshot; the copy is deleted
afterwards. In a container with a read-only root, pass --work /data.`,
		Example: `  burzblog snapshot verify /mnt/backup/burzblog-snapshot-20261027T031700.000Z.tar.gz`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openSnapshot(args[0])
			if err != nil {
				return err
			}
			defer r.Close()
			if work == "" {
				work = os.TempDir()
			}
			ctx, stop := interruptible()
			defer stop()
			res, err := snapshot.Verify(ctx, r, askPassphrase(args[0] == "-"), work)
			if err != nil {
				return snapshotError(err)
			}
			if a.asJSON {
				return a.emit(newSnapshotLine(res.Manifest, res.Encrypted))
			}
			fmt.Fprintf(a.Out, "%s The snapshot is complete and restores.\n", a.paint(green, "✓"))
			a.printSnapshotSummary(a.Out, res.Manifest, res.Encrypted)
			return nil
		},
	}

	verify.Flags().StringVar(&work, "work", "", "folder to unpack into while checking (default: the system's temporary folder)")

	var force bool
	restore := &cobra.Command{
		Use:   "restore <file | ->",
		Short: "Restore a snapshot into the data directory (stop BurzBlog first)",
		Long: `Restores a snapshot into the data directory, for example on a new server after
the old one was lost. Stop BurzBlog first: restore refuses while a server runs
with the directory.

The whole snapshot is checked before anything changes, so a damaged one leaves
the directory as it was. A directory that already holds a blog is refused;
with --force its database and uploads are moved to
<data>/backups/replaced-<time>/ instead of being deleted. Run it as the user
the server runs as; when run as root, the restored files are given to the
owner of the data directory.

An older snapshot's database is upgraded when BurzBlog next starts, with a
backup first. A snapshot from a newer BurzBlog needs that version or newer.`,
		Example: `  sudo systemctl stop burzblog
  sudo -u burzblog burzblog snapshot restore blog.tar.gz --data /var/lib/burzblog
  sudo systemctl start burzblog

  docker compose stop burzblog
  docker compose run --rm -T burzblog snapshot restore - < blog.tar.gz
  docker compose start burzblog`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := a.dataDir(data)
			r, err := openSnapshot(args[0])
			if err != nil {
				return err
			}
			defer r.Close()
			ctx, stop := interruptible()
			defer stop()
			// Upgrade an older database and record the restore while the
			// lock still keeps a server or another command out.
			finish := func(dir string, m *snapshot.Manifest) error {
				st, err := store.Open(ctx, dir)
				if err != nil {
					return fmt.Errorf("the snapshot was restored, but the database does not open: %w", err)
				}
				defer st.Close()
				return hostAudit(st, "snapshot.restore", fmt.Sprintf("made %s by %s", m.CreatedAt.Format(time.RFC3339), m.Version))
			}
			res, err := snapshot.Restore(ctx, r, askPassphrase(args[0] == "-"), dir, snapshot.RestoreOptions{Force: force, Finish: finish})
			if errors.Is(err, snapshot.ErrServerRunning) {
				return conflictf("BurzBlog is running with %s; stop it first (systemctl stop burzblog, docker compose stop burzblog), then restore", dir)
			}
			if errors.Is(err, snapshot.ErrNotEmpty) {
				return conflictf("%s already holds a blog; add --force to replace it (its database and uploads are moved to %s, not deleted)",
					dir, filepath.Join(dir, store.BackupDir, snapshot.ReplacedPrefix+"<time>"))
			}
			if err != nil {
				return snapshotError(err)
			}
			m := res.Manifest
			if a.asJSON {
				l := newSnapshotLine(m, res.Encrypted)
				l.Replaced = res.Replaced
				return a.emit(l)
			}
			fmt.Fprintf(a.Out, "%s Restored the snapshot into %s.\n", a.paint(green, "✓"), dir)
			a.printSnapshotSummary(a.Out, m, res.Encrypted)
			if res.Replaced != "" {
				fmt.Fprintf(a.Out, "  The blog that was here is kept in %s.\n", res.Replaced)
			}
			fmt.Fprintln(a.Out, "  Start BurzBlog again.")
			return nil
		},
	}
	restore.Flags().BoolVar(&force, "force", false, "replace a blog already in the data directory (it is moved aside, not deleted)")

	cmd.AddCommand(create, verify, restore)
	return cmd
}

// interruptible is a context that Ctrl-C or a service stop cancels, so
// snapshot work stops cleanly and removes its working files instead of the
// process ending in the middle.
func interruptible() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// snapshotFileName reports whether create treats target as a file to write:
// it ends in .gz or .enc. Anything else is a folder, made when missing, so a
// mistyped folder never becomes a file named like it.
func snapshotFileName(target string) bool {
	return strings.HasSuffix(target, ".gz") || strings.HasSuffix(target, ".enc")
}

// writeSnapshot writes a snapshot to path through a temporary file in the
// same folder, so path only ever holds a complete snapshot.
func writeSnapshot(ctx context.Context, st *store.Store, dataDir, path string, opts snapshot.Options) (*snapshot.Manifest, int64, error) {
	tmp := path + ".partial"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, 0, fmt.Errorf("write the snapshot: %w", err)
	}
	fail := func(err error) (*snapshot.Manifest, int64, error) {
		f.Close()
		os.Remove(tmp)
		return nil, 0, err
	}
	m, err := snapshot.Create(ctx, st, dataDir, f, opts)
	if err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(fmt.Errorf("write the snapshot: %w", err))
	}
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return nil, 0, fmt.Errorf("write the snapshot: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, 0, fmt.Errorf("write the snapshot: %w", err)
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync() // keep the rename across a power cut; not possible on Windows
		d.Close()
	}
	return m, info.Size(), nil
}

func (a *App) printSnapshotSummary(w io.Writer, m *snapshot.Manifest, encrypted bool) {
	lock := "not encrypted"
	if encrypted {
		lock = "encrypted"
	}
	fmt.Fprintf(w, "  Made %s by BurzBlog %s, %s.\n", m.CreatedAt.Local().Format("2006-01-02 15:04 MST"), clean(m.Version), lock)
	fmt.Fprintf(w, "  %s, %s, %s, %s (%s).\n", count(m.Posts, "post", "posts"),
		count(m.Revisions, "earlier version", "earlier versions"), count(m.Accounts, "account", "accounts"),
		count(len(m.Media), "upload", "uploads"), sizeText(m.MediaBytes()))
	if len(m.Missing) > 0 {
		fmt.Fprintf(w, "  %s %s not in the media folder, so not in the snapshot: %s\n", a.paint(yellow, "!"),
			count(len(m.Missing), "upload was", "uploads were"), clean(strings.Join(m.Missing, ", ")))
	}
	if len(m.Damaged) > 0 {
		fmt.Fprintf(w, "  %s %s changed on disk since upload and %s as found: %s\n", a.paint(yellow, "!"),
			count(len(m.Damaged), "upload", "uploads"), plural(len(m.Damaged), "is kept", "are kept"), clean(strings.Join(m.Damaged, ", ")))
	}
}

// snapshotError rewords the errors of reading a snapshot for the terminal.
func snapshotError(err error) error {
	switch {
	case errors.Is(err, snapshot.ErrPassphrase):
		return errors.New("the passphrase is wrong")
	case errors.Is(err, snapshot.ErrNotSnapshot):
		return errors.New("this is not a BurzBlog snapshot (`burzblog snapshot create` makes one; `burzblog backup` and `export` archives are restored with `burzblog push`)")
	}
	return err
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// sizeText prints a byte count the way people read it.
func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
