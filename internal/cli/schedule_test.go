package cli

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	good := map[string]time.Time{
		"2026-11-03T09:00:00Z":      time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC),
		"2026-11-03T09:00:00+05:30": time.Date(2026, 11, 3, 3, 30, 0, 0, time.UTC),
		"2026-11-03 09:00":          time.Date(2026, 11, 3, 3, 30, 0, 0, time.UTC),
		"2026-11-03T09:00":          time.Date(2026, 11, 3, 3, 30, 0, 0, time.UTC),
		"  2026-11-03 09:00  ":      time.Date(2026, 11, 3, 3, 30, 0, 0, time.UTC),
	}
	for in, want := range good {
		got, err := parseWhen(in, ist)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseWhen(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "tomorrow", "2026-11-03", "09:00", "2026-13-03 09:00"} {
		if _, err := parseWhen(in, ist); err == nil || !strings.Contains(err.Error(), "cannot read") {
			t.Errorf("parseWhen(%q) = %v, want an error that says what to write", in, err)
		}
	}
}

func TestScheduleAndUnscheduleCommands(t *testing.T) {
	e := newEnv(t)
	e.must("correct horse battery\n", "user", "add", "jarmos")
	e.must("correct horse battery\n", "login", e.url, "-u", "jarmos")
	e.must("", "new", "Coming soon", "--no-edit")

	when := time.Now().AddDate(0, 0, 3).Format("2006-01-02 15:04")
	out := e.must("", "schedule", "coming-soon", when)
	if !strings.Contains(out, "coming-soon is scheduled for") {
		t.Errorf("schedule = %q", out)
	}
	p, _ := e.store.GetPost(context.Background(), "coming-soon")
	if p.PublishAt == nil || p.Status != "draft" || p.PublishBy != "jarmos" {
		t.Fatalf("post after schedule = %+v", p)
	}

	list := e.must("", "list")
	if !strings.Contains(list, "scheduled") || !strings.Contains(list, "1 scheduled") {
		t.Errorf("list = %q", list)
	}
	if only := e.must("", "list", "--status", "scheduled"); !strings.Contains(only, "coming-soon") {
		t.Errorf("list --status scheduled = %q", only)
	}

	past := time.Now().AddDate(0, 0, -1).Format("2006-01-02 15:04")
	if _, errOut, code := e.run("", "schedule", "coming-soon", past); code == 0 || !strings.Contains(errOut, "in the future") {
		t.Errorf("schedule in the past: %d %q", code, errOut)
	}
	if _, errOut, code := e.run("", "schedule", "coming-soon", "someday"); code == 0 || !strings.Contains(errOut, "cannot read") {
		t.Errorf("schedule unreadable time: %d %q", code, errOut)
	}
	if _, errOut, code := e.run("", "schedule", "missing", when); code == 0 || !strings.Contains(errOut, "does not exist") {
		t.Errorf("schedule unknown post: %d %q", code, errOut)
	}

	if out := e.must("", "unschedule", "coming-soon"); !strings.Contains(out, "no longer scheduled") {
		t.Errorf("unschedule = %q", out)
	}
	p, _ = e.store.GetPost(context.Background(), "coming-soon")
	if p.PublishAt != nil {
		t.Errorf("post kept its schedule: %+v", p)
	}
}
