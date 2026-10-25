package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/store"
)

// schedulerTick is the longest the scheduler sleeps, so a missed wake-up or a
// failed attempt is retried within a minute.
var schedulerTick = time.Minute

// schedulerUser stands in for the account when a schedule has none recorded.
const schedulerUser = "scheduler"

// maxPublishAttempts bounds retries when a post keeps changing underneath us.
const maxPublishAttempts = 3

// errNotDue means the post is no longer a draft waiting for its time.
var errNotDue = errors.New("not due")

// scheduler publishes drafts whose publish time has come.
type scheduler struct {
	poke chan struct{}
	wg   sync.WaitGroup
}

func newScheduler() *scheduler {
	return &scheduler{poke: make(chan struct{}, 1)}
}

// wake asks the loop to look at the schedule again now.
func (sc *scheduler) wake() {
	select {
	case sc.poke <- struct{}{}:
	default:
	}
}

// StartScheduler runs the scheduler in the background until ctx ends. Close
// waits for it, so cancel ctx before calling Close.
func (s *Server) StartScheduler(ctx context.Context) {
	s.scheduler.wg.Add(1)
	go func() {
		defer s.scheduler.wg.Done()
		s.runScheduler(ctx)
	}()
}

// runScheduler publishes whatever is due, including posts that came due while
// the server was down, then sleeps until the next one.
func (s *Server) runScheduler(ctx context.Context) {
	for {
		wait := schedulerTick
		published, next, err := s.publishDue(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.log.Error("scheduled publishing failed", "err", err)
		} else if next != nil {
			if d := next.Sub(s.store.Now()); d < wait {
				wait = max(d, time.Second)
			}
		}
		if published > 0 {
			s.log.Info("published scheduled posts", "count", published)
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-s.scheduler.poke:
			t.Stop()
		}
	}
}

// publishDue publishes every due post and returns how many it published and
// when the next scheduled post is due, if any. It publishes the rest even
// when one fails, and returns the first error.
func (s *Server) publishDue(ctx context.Context) (int, *time.Time, error) {
	list, err := s.store.ScheduledPosts(ctx)
	if err != nil {
		return 0, nil, err
	}
	now := s.store.Now()
	published := 0
	var first error
	for _, sc := range list {
		if sc.At.After(now) {
			return published, &sc.At, first
		}
		ok, err := s.publishScheduled(ctx, sc.Slug, sc.Version, sc.By)
		if err != nil && first == nil {
			first = fmt.Errorf("publish %s: %w", sc.Slug, err)
		}
		if ok {
			published++
		}
	}
	return published, nil, first
}

// publishScheduled publishes the post as it is now through the versioned
// update path, whatever was edited since it was scheduled. On a version
// conflict it re-reads and tries again; it never overwrites newer content.
// It reports whether this call published the post.
func (s *Server) publishScheduled(ctx context.Context, slug string, version int64, by string) (bool, error) {
	if by == "" {
		by = schedulerUser
	}
	for range maxPublishAttempts {
		updated, err := s.store.UpdatePost(ctx, slug, version, by, func(p *post.Post) error {
			if p.Status != post.Draft || p.PublishAt == nil || p.PublishAt.After(s.store.Now()) {
				return errNotDue
			}
			if p.PublishedOn == nil {
				at := *p.PublishAt
				p.PublishedOn = &at
			}
			p.Status = post.Published
			return nil
		})
		switch {
		case errors.Is(err, errNotDue), errors.Is(err, store.ErrNotFound):
			return false, nil
		case errors.Is(err, store.ErrConflict):
			version = updated.Version
			continue
		case err != nil:
			return false, err
		}
		s.deploy.schedule()
		return true, nil
	}
	return false, errors.New("the post kept changing; will retry")
}
