package server

import (
	"context"
	"testing"

	"github.com/weburz/burzblog/internal/store"
)

func TestSettingsReportTheLastSnapshot(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	if _, out := h.do("GET", "/api/settings", nil); out["snapshot"] != nil {
		t.Errorf("snapshot before any = %v", out["snapshot"])
	}
	for _, file := range []string{"burzblog-snapshot-1.tar.gz", "burzblog-snapshot-2.tar.gz, encrypted"} {
		if err := h.store.AddAudit(context.Background(), store.Audit{ActorName: "host", Action: "snapshot.create", Target: file}); err != nil {
			t.Fatal(err)
		}
	}
	_, out := h.do("GET", "/api/settings", nil)
	snap, _ := out["snapshot"].(map[string]any)
	if snap["file"] != "burzblog-snapshot-2.tar.gz, encrypted" || snap["at"] == "" {
		t.Errorf("snapshot = %v", out["snapshot"])
	}
}

func TestSettingsReportTheSchedule(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	if _, out := h.do("GET", "/api/settings", nil); out["schedule"] != nil {
		t.Errorf("schedule while off = %v", out["schedule"])
	}
	h2 := newHarness(t, func(c *Config) {
		c.SnapshotSchedule = func() any { return map[string]any{"everySeconds": 86400, "to": "s3://blogs/ada/"} }
	})
	h2.login()
	_, out := h2.do("GET", "/api/settings", nil)
	if sched, _ := out["schedule"].(map[string]any); sched["to"] != "s3://blogs/ada/" {
		t.Errorf("schedule = %v", out["schedule"])
	}
}
