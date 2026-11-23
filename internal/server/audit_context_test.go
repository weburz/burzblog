package server

import (
	"context"
	"testing"

	"github.com/weburz/burzblog/internal/store"
)

func TestCommittedActionAuditHasIndependentLifetime(t *testing.T) {
	h := newHarness(t, nil)
	ctx, done := context.WithCancel(context.Background())
	done()
	h.srv.addAudit(ctx, store.Audit{ActorName: "ada", Action: "post.publish", Target: "example"})
	entry, err := h.store.LastAudit(t.Context(), "post.publish")
	if err != nil || entry.Target != "example" {
		t.Fatalf("entry = %+v, %v", entry, err)
	}
}
