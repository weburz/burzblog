package post

import (
	"testing"
	"time"
)

func TestValidateSchedule(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	good := []time.Time{now.Add(time.Second), now.Add(48 * time.Hour), now.Add(MaxScheduleAhead)}
	for _, at := range good {
		if err := ValidateSchedule(at, now); err != nil {
			t.Errorf("ValidateSchedule(%v) = %v, want nil", at, err)
		}
	}
	bad := []time.Time{now, now.Add(-time.Hour), now.Add(MaxScheduleAhead + time.Second)}
	for _, at := range bad {
		if err := ValidateSchedule(at, now); !IsFieldError(err) {
			t.Errorf("ValidateSchedule(%v) = %v, want a field error", at, err)
		}
	}
}

func TestValidateRejectsScheduleOnNonDraft(t *testing.T) {
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	p := &Post{Slug: "a", Title: "A", Status: Draft, PublishAt: &at}
	if err := Validate(p); err != nil {
		t.Errorf("scheduled draft: %v", err)
	}
	for _, status := range []Status{Published, Archived} {
		p.Status = status
		if err := Validate(p); !IsFieldError(err) {
			t.Errorf("%s with a publish time = %v, want a field error", status, err)
		}
	}
}

func TestSameContentSeesSchedule(t *testing.T) {
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	a := &Post{Slug: "a", Title: "A", Status: Draft}
	b := *a
	if !SameContent(a, &b) {
		t.Fatal("equal posts differ")
	}
	b.PublishAt = &at
	if SameContent(a, &b) {
		t.Error("scheduling did not count as a change")
	}
}
