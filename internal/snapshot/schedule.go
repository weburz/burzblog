package snapshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/weburz/burzblog/internal/store"
)

// WriteFile writes a snapshot to path through a temporary file in the same
// folder, so path only ever holds a complete snapshot, and returns its size.
func WriteFile(ctx context.Context, st *store.Store, dataDir, path string, opts Options) (*Manifest, int64, error) {
	tmp := path + ".partial"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, 0, fmt.Errorf("write the snapshot: %w", err)
	}
	fail := func(err error) (*Manifest, int64, error) {
		f.Close()
		os.Remove(tmp)
		return nil, 0, err
	}
	m, err := Create(ctx, st, dataDir, f, opts)
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
	syncDir(filepath.Dir(path))
	return m, info.Size(), nil
}

// Destination is where scheduled snapshots go: a Folder or a Remote.
type Destination interface {
	String() string
	put(ctx context.Context, st *store.Store, dataDir string, keep int, opt Options) (*Outcome, error)
}

// Outcome is one stored snapshot.
type Outcome struct {
	Manifest *Manifest
	// Name is the snapshot's file name, and Where its full path or key.
	Name, Where string
	// Sent counts the uploads a Remote sent; a Folder's snapshot holds all.
	Sent    int
	Pruned  []string
	Warning string
}

// Folder is a local folder of snapshot files.
type Folder string

func (f Folder) String() string { return string(f) }

func (f Folder) put(ctx context.Context, st *store.Store, dataDir string, keep int, opt Options) (*Outcome, error) {
	unlock, err := lockRuns(dataDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := os.MkdirAll(string(f), 0o700); err != nil {
		return nil, fmt.Errorf("make the snapshot folder: %w", err)
	}
	SweepPartial(string(f))
	now := time.Now().UTC()
	if opt.Now != nil {
		now = opt.Now().UTC()
	}
	opt.Now = func() time.Time { return now }
	name := Name(now, opt.Passphrase != "")
	m, _, err := WriteFile(ctx, st, dataDir, filepath.Join(string(f), name), opt)
	if err != nil {
		return nil, err
	}
	out := &Outcome{Manifest: m, Name: name, Where: filepath.Join(string(f), name)}
	if keep > 0 {
		if out.Pruned, err = Prune(string(f), keep); err != nil {
			return out, fmt.Errorf("the snapshot was written, but deleting older ones failed: %w", err)
		}
	}
	return out, nil
}

func (r Remote) put(ctx context.Context, st *store.Store, dataDir string, keep int, opt Options) (*Outcome, error) {
	res, err := CreateRemote(ctx, st, dataDir, r, keep, opt)
	if res == nil {
		return nil, err
	}
	name := filepath.Base(res.Key)
	return &Outcome{Manifest: res.Manifest, Name: name, Where: r.Label + "snapshots/" + name, Sent: res.Sent,
		Pruned: res.Pruned, Warning: res.PruneWarning}, err
}

// Schedule makes snapshots on its own while the server runs.
type Schedule struct {
	Every      time.Duration
	Keep       int
	To         Destination
	Passphrase string
	// Heartbeat is an address to GET after every snapshot that worked, for a
	// monitor that alerts when the pings stop. It is a secret: never shown.
	Heartbeat string
	Version   string
	Store     *store.Store
	DataDir   string
	Log       *slog.Logger

	// MinWait is the pause after start before the first snapshot that is due
	// (tests shorten it), and RetryAfter the wait after a failure.
	MinWait, RetryAfter time.Duration
	http                *http.Client

	mu     sync.Mutex
	status ScheduleStatus
}

// MinEvery is the shortest interval a schedule accepts.
const MinEvery = time.Hour

// ScheduleStatus is what admins see of a schedule.
type ScheduleStatus struct {
	EverySeconds int64      `json:"everySeconds"`
	To           string     `json:"to"`
	Keep         int        `json:"keep"`
	Encrypted    bool       `json:"encrypted"`
	Heartbeat    bool       `json:"heartbeat"`
	NextAt       *time.Time `json:"nextAt"`
	Running      bool       `json:"running"`
	LastError    string     `json:"lastError,omitempty"`
	LastErrorAt  *time.Time `json:"lastErrorAt,omitempty"`
	// Warning is a problem of the last snapshot that still worked, such as
	// old snapshots that could not be deleted.
	Warning string `json:"warning,omitempty"`
}

// Check validates the schedule before the server starts.
func (s *Schedule) Check() error {
	if s.Every < MinEvery {
		return fmt.Errorf("snapshots can run at most every %v, not every %v", MinEvery, s.Every)
	}
	if s.Keep < 0 {
		return errors.New("the number of snapshots to keep must be 0 (all) or more")
	}
	if _, remote := s.To.(Remote); remote && s.Passphrase == "" {
		return errors.New("snapshots stored off the server must be encrypted: set BURZBLOG_SNAPSHOT_PASSPHRASE or BURZBLOG_SNAPSHOT_PASSPHRASE_FILE")
	}
	if s.Passphrase != "" && len(s.Passphrase) < MinPassphrase {
		return fmt.Errorf("the snapshot passphrase must be at least %d characters", MinPassphrase)
	}
	if s.Heartbeat != "" {
		u, err := url.Parse(s.Heartbeat)
		local := err == nil && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())
		if err != nil || u.Host == "" || !(u.Scheme == "https" || u.Scheme == "http" && local) {
			return errors.New("the snapshot heartbeat must be an https address")
		}
	}
	return nil
}

// Status reports the schedule's state.
func (s *Schedule) Status() ScheduleStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.EverySeconds, st.To, st.Keep = int64(s.Every/time.Second), s.To.String(), s.Keep
	st.Encrypted, st.Heartbeat = s.Passphrase != "", s.Heartbeat != ""
	return st
}

// Run makes a snapshot whenever one is due until ctx ends: Every after the
// newest snapshot the audit log records, made by the schedule or by hand,
// and a while after a failure.
func (s *Schedule) Run(ctx context.Context) {
	if s.MinWait == 0 {
		s.MinWait = time.Minute
	}
	if s.RetryAfter == 0 {
		s.RetryAfter = min(s.Every, time.Hour)
	}
	if s.http == nil {
		s.http = &http.Client{Timeout: 10 * time.Second}
	}
	earliest := time.Now().Add(s.MinWait)
	for {
		next := s.next(ctx, earliest)
		s.mu.Lock()
		s.status.NextAt = &next
		s.mu.Unlock()
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.runOnce(ctx)
	}
}

func (s *Schedule) next(ctx context.Context, earliest time.Time) time.Time {
	next := earliest
	s.mu.Lock()
	failedAt := s.status.LastErrorAt
	s.mu.Unlock()
	if failedAt != nil {
		next = failedAt.Add(s.RetryAfter)
	} else if last, err := s.Store.LastAudit(ctx, "snapshot.create"); err == nil {
		next = last.At.Add(s.Every)
	}
	if next.Before(earliest) {
		next = earliest
	}
	// A clock that was wrong when the last one was recorded must not push
	// the next past one interval from now.
	if limit := time.Now().Add(s.Every); next.After(limit) {
		next = limit
	}
	return next
}

func (s *Schedule) runOnce(ctx context.Context) {
	s.mu.Lock()
	s.status.Running = true
	s.mu.Unlock()
	out, err := s.To.put(ctx, s.Store, s.DataDir, s.Keep, Options{Passphrase: s.Passphrase, Version: s.Version})
	if out != nil && err != nil {
		// Stored, but pruning failed (often a key that may not delete):
		// that is a snapshot, not a failure to retry every hour.
		out.Warning, err = err.Error(), nil
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.status.Running = false
	if out != nil {
		s.status.LastError, s.status.LastErrorAt, s.status.Warning = "", nil, out.Warning
	} else if ctx.Err() == nil {
		s.status.LastError, s.status.LastErrorAt = err.Error(), &now
	}
	s.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	if out == nil {
		s.Log.Error("scheduled snapshot failed", "to", s.To.String(), "err", err)
		s.audit(ctx, "snapshot.fail", err.Error())
		return
	}
	target := out.Where
	if s.Passphrase != "" {
		target += ", encrypted"
	}
	s.audit(ctx, "snapshot.create", target)
	s.Log.Info("scheduled snapshot stored", "to", s.To.String(), "name", out.Name, "posts", out.Manifest.Posts,
		"uploads", len(out.Manifest.Media), "uploads_sent", out.Sent, "pruned", len(out.Pruned))
	if len(out.Manifest.Missing) > 0 || len(out.Manifest.Damaged) > 0 {
		s.Log.Warn("scheduled snapshot left uploads out", "missing", out.Manifest.Missing, "changed", out.Manifest.Damaged)
	}
	if out.Warning != "" {
		s.Log.Warn("scheduled snapshot", "warning", out.Warning)
	}
	s.ping(ctx)
}

func (s *Schedule) audit(ctx context.Context, action, target string) {
	if err := s.Store.AddAudit(ctx, store.Audit{ActorName: "scheduler", Action: action, Target: target}); err != nil {
		s.Log.Warn("could not record the snapshot in the audit log", "err", err)
	}
}

// ping tells the heartbeat monitor a snapshot worked. The address is never
// logged: it is a secret.
func (s *Schedule) ping(ctx context.Context) {
	if s.Heartbeat == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Heartbeat, nil)
	if err != nil {
		return
	}
	resp, err := s.http.Do(req)
	if err != nil {
		s.Log.Warn("the snapshot heartbeat could not be sent")
		return
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		s.Log.Warn("the snapshot heartbeat was refused", "status", resp.StatusCode)
	}
}
