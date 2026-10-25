package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/post"
)

// clock is a settable time source shared by the store and the scheduler.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// scheduleHarness is a logged-in harness whose clock the test controls.
func scheduleHarness(t *testing.T, mutate func(*Config)) (*harness, *clock) {
	t.Helper()
	h := newHarness(t, mutate)
	c := &clock{now: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}
	h.store.SetClock(c.Now)
	h.login()
	return h, c
}

func (h *harness) draft(title string) string {
	h.t.Helper()
	resp, out := h.do("POST", "/api/posts", map[string]string{"title": title})
	if resp.StatusCode != 201 {
		h.t.Fatalf("create %q = %d %v", title, resp.StatusCode, out)
	}
	return out["slug"].(string)
}

func (h *harness) schedule(slug string, version int, at time.Time) (*http.Response, map[string]any) {
	return h.do("POST", "/api/posts/"+slug+"/schedule", map[string]any{"version": version, "at": at.Format(time.RFC3339)})
}

func TestScheduleValidation(t *testing.T) {
	h, c := scheduleHarness(t, nil)
	slug := h.draft("Soon")
	now := c.Now()

	for name, at := range map[string]time.Time{
		"past":     now.Add(-time.Minute),
		"now":      now,
		"too far":  now.Add(post.MaxScheduleAhead + time.Hour),
		"way past": now.AddDate(-1, 0, 0),
	} {
		if resp, out := h.schedule(slug, 1, at); resp.StatusCode != 422 || errCode(out) != "validation" {
			t.Errorf("%s: %d %v, want 422 validation", name, resp.StatusCode, out)
		}
	}
	if resp, out := h.do("POST", "/api/posts/"+slug+"/schedule", map[string]any{"version": 1}); resp.StatusCode != 422 {
		t.Errorf("missing at = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts/"+slug+"/schedule", map[string]any{"version": 1, "at": "tomorrow"}); resp.StatusCode != 400 {
		t.Errorf("unreadable at = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.schedule(slug, 7, now.Add(time.Hour)); resp.StatusCode != 409 || errCode(out) != "conflict" {
		t.Errorf("wrong version = %d %v", resp.StatusCode, out)
	}
	if resp, _ := h.schedule("missing", 1, now.Add(time.Hour)); resp.StatusCode != 404 {
		t.Errorf("unknown post = %d", resp.StatusCode)
	}
	if _, out := h.do("GET", "/api/posts/"+slug, nil); out["publishAt"] != nil || out["version"] != 1.0 {
		t.Errorf("a refused schedule changed the post: %v", out)
	}

	at := now.Add(36 * time.Hour)
	resp, out := h.schedule(slug, 1, at)
	if resp.StatusCode != 200 || out["status"] != "draft" || out["publishAt"] != at.Format(time.RFC3339) || out["version"] != 2.0 {
		t.Fatalf("schedule = %d %v", resp.StatusCode, out)
	}

	// Only drafts can be scheduled.
	pub := h.draft("Live")
	h.do("POST", "/api/posts/"+pub+"/publish", map[string]any{"version": 1})
	if resp, out := h.schedule(pub, 2, now.Add(time.Hour)); resp.StatusCode != 422 {
		t.Errorf("schedule published = %d %v", resp.StatusCode, out)
	}
	gone := h.draft("Gone")
	h.do("POST", "/api/posts/"+gone+"/archive", map[string]any{"version": 1})
	if resp, out := h.schedule(gone, 2, now.Add(time.Hour)); resp.StatusCode != 422 {
		t.Errorf("schedule archived = %d %v", resp.StatusCode, out)
	}
}

func TestListAndGetShowPublishAt(t *testing.T) {
	h, c := scheduleHarness(t, nil)
	soon := h.draft("Soon")
	h.draft("Plain")
	at := c.Now().Add(time.Hour)
	h.schedule(soon, 1, at)

	_, list := h.do("GET", "/api/posts", nil)
	counts := list["counts"].(map[string]any)
	if counts["scheduled"] != 1.0 || counts["draft"] != 2.0 {
		t.Errorf("counts = %v", counts)
	}
	for _, it := range list["items"].([]any) {
		item := it.(map[string]any)
		want := any(nil)
		if item["slug"] == soon {
			want = at.Format(time.RFC3339)
		}
		if item["publishAt"] != want {
			t.Errorf("%v publishAt = %v, want %v", item["slug"], item["publishAt"], want)
		}
	}
	_, only := h.do("GET", "/api/posts?status=scheduled", nil)
	if items := only["items"].([]any); len(items) != 1 || items[0].(map[string]any)["slug"] != soon {
		t.Errorf("status=scheduled = %v", only["items"])
	}
	if resp, _ := h.do("GET", "/api/posts?status=bogus", nil); resp.StatusCode != 400 {
		t.Errorf("status=bogus = %d", resp.StatusCode)
	}

	resp, out := h.do("POST", "/api/posts/"+soon+"/unschedule", map[string]any{"version": 2})
	if resp.StatusCode != 200 || out["publishAt"] != nil || out["status"] != "draft" || out["version"] != 3.0 {
		t.Errorf("unschedule = %d %v", resp.StatusCode, out)
	}
	if resp, out := h.do("POST", "/api/posts/"+soon+"/unschedule", map[string]any{"version": 3}); resp.StatusCode != 200 || out["version"] != 3.0 {
		t.Errorf("unschedule again = %d %v, want a no-op", resp.StatusCode, out)
	}
	if _, out := h.do("GET", "/api/posts/"+soon+"/revisions", nil); len(out["items"].([]any)) != 2 {
		t.Errorf("revisions = %v, want one per schedule change", out["items"])
	}
}

func TestManualPublishAndArchiveClearSchedule(t *testing.T) {
	h, c := scheduleHarness(t, nil)
	at := c.Now().Add(time.Hour)
	for _, action := range []string{"publish", "archive"} {
		slug := h.draft("Post " + action)
		h.schedule(slug, 1, at)
		resp, out := h.do("POST", "/api/posts/"+slug+"/"+action, map[string]any{"version": 2})
		if resp.StatusCode != 200 || out["publishAt"] != nil {
			t.Errorf("%s = %d %v, want the schedule cleared", action, resp.StatusCode, out)
		}
	}
	// The scheduler has nothing left to do.
	c.advance(2 * time.Hour)
	if n, next, err := h.srv.publishDue(context.Background()); n != 0 || next != nil || err != nil {
		t.Errorf("publishDue = %d %v %v, want nothing", n, next, err)
	}
	_, out := h.do("GET", "/api/posts/post-archive", nil)
	if out["status"] != "archived" {
		t.Errorf("archived post = %v", out["status"])
	}
}

func TestSchedulerPublishesDuePostsOnce(t *testing.T) {
	h, c := scheduleHarness(t, nil)
	ctx := context.Background()
	soon, later := h.draft("Soon"), h.draft("Later")
	t1, t2 := c.Now().Add(time.Hour), c.Now().Add(48*time.Hour)
	h.schedule(soon, 1, t1)
	h.schedule(later, 1, t2)

	n, next, err := h.srv.publishDue(ctx)
	if n != 0 || err != nil || next == nil || !next.Equal(t1) {
		t.Fatalf("before the time: %d %v %v", n, next, err)
	}

	c.advance(90 * time.Minute)
	n, next, err = h.srv.publishDue(ctx)
	if n != 1 || err != nil || next == nil || !next.Equal(t2) {
		t.Fatalf("at the time: %d %v %v", n, next, err)
	}
	_, out := h.do("GET", "/api/posts/"+soon, nil)
	if out["status"] != "published" || out["publishAt"] != nil || out["updatedBy"] != "jarmos" ||
		out["publishedOn"] != t1.Format(time.RFC3339) || out["version"] != 3.0 {
		t.Errorf("published post = %v", out)
	}
	if _, out := h.do("GET", "/api/posts/"+later, nil); out["status"] != "draft" || out["publishAt"] == nil {
		t.Errorf("a post that is not due changed: %v", out)
	}

	// Running again changes nothing: no new version, no second publish.
	if n, _, err := h.srv.publishDue(ctx); n != 0 || err != nil {
		t.Errorf("second run published %d, %v", n, err)
	}
	if _, out := h.do("GET", "/api/posts/"+soon, nil); out["version"] != 3.0 {
		t.Errorf("second run changed the post: %v", out)
	}
	revs := h.do2("GET", "/api/posts/"+soon+"/revisions")
	if len(revs["items"].([]any)) != 2 {
		t.Errorf("revisions = %v, want the drafts before scheduling and publishing", revs["items"])
	}
}

func (h *harness) do2(method, path string) map[string]any {
	h.t.Helper()
	_, out := h.do(method, path, nil)
	return out
}

func TestSchedulerPublishesPostsDueWhileDown(t *testing.T) {
	schedulerTickBefore := schedulerTick
	schedulerTick = 10 * time.Millisecond
	t.Cleanup(func() { schedulerTick = schedulerTickBefore })

	h, c := scheduleHarness(t, nil)
	slug := h.draft("Overdue")
	h.schedule(slug, 1, c.Now().Add(time.Hour))
	// The server was down at the due time; it starts a day late.
	c.advance(24 * time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	// Cleanups run last-in first-out, so this ends the scheduler before the
	// harness's Close waits for it, even when the test fails early.
	t.Cleanup(cancel)
	h.srv.StartScheduler(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if p, err := h.store.GetPost(context.Background(), slug); err == nil && p.Status == post.Published {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("overdue post was not published on start")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// It keeps running: a post scheduled later and then due is picked up.
	// Sessions are not tied to the test clock, so schedule through the store.
	bg := context.Background()
	created, err := h.store.CreatePost(bg, &post.Post{Slug: "later", Title: "Later"}, "jarmos")
	if err != nil {
		t.Fatal(err)
	}
	at := c.Now().Add(time.Hour)
	if _, err := h.store.UpdatePost(bg, "later", created.Version, "jarmos", func(p *post.Post) error {
		p.PublishAt, p.PublishBy = &at, "jarmos"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	later := "later"
	c.advance(2 * time.Hour)
	for {
		if p, err := h.store.GetPost(context.Background(), later); err == nil && p.Status == post.Published {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("later post was not published by the running scheduler")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	done := make(chan struct{})
	go func() { h.srv.Close(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after the scheduler's context ended")
	}
}

func TestSchedulerTriggersDeployHook(t *testing.T) {
	var hits atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer hook.Close()
	deployDelay = 10 * time.Millisecond
	h, c := scheduleHarness(t, func(cfg *Config) { cfg.DeployHook = hook.URL })

	slug := h.draft("Hooked")
	h.schedule(slug, 1, c.Now().Add(time.Hour))
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatalf("scheduling alone rebuilt the reader site (%d hits)", hits.Load())
	}
	c.advance(2 * time.Hour)
	if n, _, err := h.srv.publishDue(context.Background()); n != 1 || err != nil {
		t.Fatalf("publishDue = %d, %v", n, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("publishing on schedule did not call the deploy hook")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSchedulerPublishesCurrentContentAfterConcurrentEdit(t *testing.T) {
	h, c := scheduleHarness(t, nil)
	ctx := context.Background()
	slug := h.draft("Edited")
	h.schedule(slug, 1, c.Now().Add(time.Hour))
	c.advance(2 * time.Hour)

	// The scheduler read version 2, then a writer saves version 3.
	h.do("PUT", "/api/posts/"+slug, map[string]any{"version": 2, "title": "Edited", "body": "newer words"})

	ok, err := h.srv.publishScheduled(ctx, slug, 2, "jarmos")
	if !ok || err != nil {
		t.Fatalf("publishScheduled = %v, %v", ok, err)
	}
	_, out := h.do("GET", "/api/posts/"+slug, nil)
	if out["status"] != "published" || out["body"] != "newer words" || out["version"] != 4.0 {
		t.Errorf("post after a conflicting edit = %v", out)
	}

	// Unscheduled in the meantime: the stale attempt must leave it alone.
	other := h.draft("Cancelled")
	h.schedule(other, 1, c.Now().Add(time.Hour))
	c.advance(2 * time.Hour)
	h.do("POST", "/api/posts/"+other+"/unschedule", map[string]any{"version": 2})
	if ok, err := h.srv.publishScheduled(ctx, other, 2, "jarmos"); ok || err != nil {
		t.Errorf("publishScheduled on an unscheduled post = %v, %v", ok, err)
	}
	if _, out := h.do("GET", "/api/posts/"+other, nil); out["status"] != "draft" {
		t.Errorf("cancelled schedule published anyway: %v", out)
	}
}

func TestScheduledPostStaysOutOfExportUntilPublished(t *testing.T) {
	h, c := scheduleHarness(t, nil)
	slug := h.draft("Hidden")
	h.schedule(slug, 1, c.Now().Add(time.Hour))
	posts, err := h.store.PublishedPosts(context.Background())
	if err != nil || len(posts) != 0 {
		t.Fatalf("PublishedPosts = %d, %v; a scheduled draft must not be exported", len(posts), err)
	}
	c.advance(2 * time.Hour)
	h.srv.publishDue(context.Background())
	if posts, _ := h.store.PublishedPosts(context.Background()); len(posts) != 1 {
		t.Errorf("PublishedPosts = %d after the time, want 1", len(posts))
	}
}
