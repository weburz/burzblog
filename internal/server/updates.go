package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/weburz/burzblog/internal/release"
)

// updateDelay is how long after start the first check waits, and updateEvery
// the time between checks. updateTimeout bounds each one.
var (
	updateDelay   = 30 * time.Second
	updateEvery   = 24 * time.Hour
	updateTimeout = 10 * time.Second
)

// updateChecker asks GitHub for the newest release, only when the operator
// turned checks on, and keeps the last answer in memory.
type updateChecker struct {
	enabled bool
	url     string
	current string
	client  *http.Client

	mu        sync.Mutex
	latest    release.Release
	checkedAt time.Time
	failed    bool
}

// updateInfo is the admin-facing answer; Update is nil until a check has
// succeeded.
type updateInfo struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest"`
	Available bool      `json:"available"`
	URL       string    `json:"url"`
	CheckedAt time.Time `json:"checkedAt"`
}

func newUpdateChecker(cfg Config) *updateChecker {
	url := cfg.updateURL
	if url == "" {
		url = release.LatestURL
	}
	return &updateChecker{
		enabled: cfg.CheckUpdates,
		url:     url,
		current: cfg.Version,
		client: &http.Client{
			Timeout: updateTimeout,
			// GitHub answers directly; follow nothing else.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// check performs one lookup and stores the result. A failure is logged once
// until a check works again, and leaves the previous answer in place.
func (u *updateChecker) check(ctx context.Context, warn func(msg string, args ...any)) {
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()
	rel, err := release.Fetch(ctx, u.client, u.url, "burzblog/"+release.Trim(u.current))
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		if !u.failed {
			warn("update check failed; it will be tried again later", "err", err)
		}
		u.failed = true
		return
	}
	u.failed = false
	u.latest, u.checkedAt = rel, time.Now().UTC()
}

// info returns the cached answer, or nil when checks are off or none has
// succeeded yet.
func (u *updateChecker) info() *updateInfo {
	if !u.enabled {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.checkedAt.IsZero() {
		return nil
	}
	return &updateInfo{
		Current:   release.Trim(u.current),
		Latest:    release.Trim(u.latest.Tag),
		Available: release.Newer(u.current, u.latest.Tag),
		URL:       u.latest.URL,
		CheckedAt: u.checkedAt,
	}
}

// StartUpdateChecks looks for a newer release shortly after start and then
// every 24 hours until ctx ends. It does nothing, and contacts nobody, unless
// Config.CheckUpdates is set. Close waits for it, so cancel ctx first.
func (s *Server) StartUpdateChecks(ctx context.Context) {
	u := s.updates
	if !u.enabled {
		return
	}
	s.scheduler.wg.Add(1)
	go func() {
		defer s.scheduler.wg.Done()
		wait := updateDelay
		for {
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			u.check(ctx, s.log.Warn)
			wait = updateEvery
		}
	}()
}

// listUpdates answers GET /api/updates for admins: whether checks are on and, once
// one has worked, what it found.
func (s *Server) listUpdates(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, map[string]any{
		"enabled": s.updates.enabled,
		"update":  s.updates.info(),
	})
}
