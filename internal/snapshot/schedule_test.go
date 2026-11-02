package snapshot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/s3"
	"github.com/weburz/burzblog/internal/store"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// waitFor polls cond until it holds or a few seconds pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestScheduleMakesSnapshotsAndPings(t *testing.T) {
	b := newBlog(t)
	var pings atomic.Int32
	hb := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { pings.Add(1) }))
	defer hb.Close()
	folder := filepath.Join(t.TempDir(), "snaps")
	s := &Schedule{Every: time.Hour, Keep: 3, To: Folder(folder), Passphrase: testPassphrase, Heartbeat: hb.URL,
		Version: "v9", Store: b.st, DataDir: b.dir, Log: quietLog(), MinWait: 10 * time.Millisecond}
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "the first snapshot", func() bool { return pings.Load() == 1 })

	entry, err := b.st.LastAudit(context.Background(), "snapshot.create")
	if err != nil || entry.ActorName != "scheduler" || !strings.HasPrefix(entry.Target, filepath.Join(folder, "burzblog-snapshot-")) ||
		!strings.HasSuffix(entry.Target, ".tar.gz.enc, encrypted") {
		t.Fatalf("audit = %+v, %v", entry, err)
	}
	files, _ := filepath.Glob(filepath.Join(folder, "*.enc"))
	if len(files) != 1 {
		t.Fatalf("snapshot files = %v", files)
	}
	// The next one is due an hour after the newest.
	waitFor(t, "the next run time", func() bool {
		st := s.Status()
		return st.NextAt != nil && st.NextAt.After(entry.At.Add(59*time.Minute))
	})
	st := s.Status()
	if st.EverySeconds != 3600 || st.To != folder || st.Keep != 3 || !st.Encrypted || !st.Heartbeat || st.LastError != "" {
		t.Errorf("status = %+v", st)
	}
	cancel()
	<-done
}

func TestScheduleWaitsForTheNewestSnapshot(t *testing.T) {
	b := newBlog(t)
	s := &Schedule{Every: 24 * time.Hour, To: Folder(t.TempDir()), Store: b.st, DataDir: b.dir, Log: quietLog(),
		RetryAfter: time.Hour}
	start := time.Now()
	if next := s.next(context.Background(), start); !next.Equal(start) {
		t.Errorf("first ever snapshot at %v, want at once (%v)", next, start)
	}
	// One made by hand counts too.
	if err := b.st.AddAudit(context.Background(), store.Audit{ActorName: "host", Action: "snapshot.create", Target: "x"}); err != nil {
		t.Fatal(err)
	}
	last, _ := b.st.LastAudit(context.Background(), "snapshot.create")
	if next := s.next(context.Background(), start); !next.Equal(last.At.Add(24 * time.Hour)) {
		t.Errorf("next = %v, want a day after %v", next, last.At)
	}
}

type failingBucket struct{ memBucket }

func (f *failingBucket) List(context.Context, string) ([]s3.Object, error) {
	return nil, &s3.Error{Status: 403, Code: "AccessDenied"}
}

func TestScheduleRecordsFailuresAndRetries(t *testing.T) {
	b := newBlog(t)
	var pings atomic.Int32
	hb := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { pings.Add(1) }))
	defer hb.Close()
	r := Remote{Bucket: &failingBucket{}, Prefix: "blog/", Label: "s3://bucket/blog/"}
	s := &Schedule{Every: time.Hour, To: r, Passphrase: testPassphrase, Heartbeat: hb.URL, Store: b.st, DataDir: b.dir,
		Log: quietLog(), MinWait: 10 * time.Millisecond, RetryAfter: 30 * time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "the failure", func() bool { return s.Status().LastErrorAt != nil })
	waitFor(t, "the retry time", func() bool {
		st := s.Status()
		return st.NextAt != nil && st.NextAt.After(st.LastErrorAt.Add(29*time.Minute))
	})
	cancel()
	<-done
	st := s.Status()
	if !strings.Contains(st.LastError, "AccessDenied") {
		t.Errorf("status = %+v", st)
	}
	if pings.Load() != 0 {
		t.Error("the heartbeat was pinged for a failed snapshot")
	}
	entry, err := b.st.LastAudit(context.Background(), "snapshot.fail")
	if err != nil || entry.ActorName != "scheduler" || !strings.Contains(entry.Target, "AccessDenied") {
		t.Errorf("failure audit = %+v, %v", entry, err)
	}
	if _, err := b.st.LastAudit(context.Background(), "snapshot.create"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a failed run was recorded as a snapshot: %v", err)
	}
}

func TestScheduleCheck(t *testing.T) {
	valid := func() *Schedule { return &Schedule{Every: 24 * time.Hour, To: Folder("/tmp/x"), Keep: 14} }
	if err := valid().Check(); err != nil {
		t.Errorf("valid: %v", err)
	}
	for name, change := range map[string]func(*Schedule){
		"too often":         func(s *Schedule) { s.Every = 10 * time.Minute },
		"negative keep":     func(s *Schedule) { s.Keep = -1 },
		"plain remote":      func(s *Schedule) { s.To = Remote{Label: "s3://b/"} },
		"short passphrase":  func(s *Schedule) { s.Passphrase = "short" },
		"http heartbeat":    func(s *Schedule) { s.Heartbeat = "http://hc-ping.com/abc" },
		"garbage heartbeat": func(s *Schedule) { s.Heartbeat = "not a url" },
	} {
		s := valid()
		change(s)
		if err := s.Check(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s := valid()
	s.To, s.Passphrase, s.Heartbeat = Remote{Label: "s3://b/"}, testPassphrase, "https://hc-ping.com/abc"
	if err := s.Check(); err != nil {
		t.Errorf("encrypted remote with heartbeat: %v", err)
	}
}

// noDeleteBucket stores but may not delete, like a key kept from deleting.
type noDeleteBucket struct{ *memBucket }

func (noDeleteBucket) Delete(context.Context, string) error {
	return &s3.Error{Status: 403, Code: "AccessDenied"}
}

func TestSchedulePruneFailureIsAWarning(t *testing.T) {
	b := newBlog(t)
	var pings atomic.Int32
	hb := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { pings.Add(1) }))
	defer hb.Close()
	bucket := noDeleteBucket{newMemBucket()}
	// Two old snapshots already there, so keeping one has to delete.
	for _, at := range []time.Time{time.Now().Add(-72 * time.Hour), time.Now().Add(-48 * time.Hour)} {
		if _, err := CreateRemote(context.Background(), b.st, b.dir, Remote{Bucket: bucket.memBucket, Label: "m"}, 0,
			Options{Passphrase: testPassphrase, Now: func() time.Time { return at }}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Schedule{Every: 24 * time.Hour, Keep: 1, To: Remote{Bucket: bucket, Label: "mem://"}, Passphrase: testPassphrase,
		Heartbeat: hb.URL, Store: b.st, DataDir: b.dir, Log: quietLog(), MinWait: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "the snapshot", func() bool { return pings.Load() == 1 })
	waitFor(t, "the next run time", func() bool { st := s.Status(); return st.NextAt != nil && !st.Running })
	cancel()
	<-done
	st := s.Status()
	if st.LastError != "" || !strings.Contains(st.Warning, "AccessDenied") {
		t.Errorf("status = %+v", st)
	}
	if st.NextAt.Before(time.Now().Add(23 * time.Hour)) {
		t.Errorf("next run %v: a stored snapshot whose pruning failed is retried like a failure", st.NextAt)
	}
	if e, err := b.st.LastAudit(context.Background(), "snapshot.create"); err != nil || e.ActorName != "scheduler" {
		t.Errorf("audit = %+v, %v", e, err)
	}
}
