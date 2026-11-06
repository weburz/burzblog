package post

import (
	"strings"
	"testing"
	"time"
)

func TestValidateDates(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	at := func(year int) *time.Time {
		v := time.Date(year, time.June, 1, 0, 0, 0, 0, time.UTC)
		return &v
	}
	edge := now.Add(MaxScheduleAhead)
	tooLate := edge.Add(time.Second)
	first := time.Date(MinPublishYear, time.January, 1, 0, 0, 0, 0, time.UTC)
	before := first.Add(-time.Second)
	cases := []struct {
		name      string
		published *time.Time
		updated   *time.Time
		wantField string
	}{
		{"no dates", nil, nil, ""},
		{"ordinary", at(2025), at(2026), ""},
		{"same instant", at(2025), at(2025), ""},
		{"oldest allowed", &first, nil, ""},
		{"furthest allowed", &edge, &edge, ""},
		{"updated only", nil, at(2026), ""},
		{"year 3000", at(3000), nil, "published date"},
		{"year 1", at(1), nil, "published date"},
		{"just before 1990", &before, nil, "published date"},
		{"just past the limit", &tooLate, nil, "published date"},
		{"updated in year 3000", at(2025), at(3000), "updated date"},
		{"updated before 1990", nil, at(1980), "updated date"},
		{"updated before published", at(2026), at(2025), "updated date"},
	}
	for _, c := range cases {
		err := ValidateDates(&Post{PublishedOn: c.published, UpdatedOn: c.updated}, now)
		switch {
		case c.wantField == "" && err != nil:
			t.Errorf("%s: %v, want nil", c.name, err)
		case c.wantField != "" && !(IsFieldError(err) && strings.HasPrefix(err.Error(), c.wantField)):
			t.Errorf("%s: %v, want a %q field error", c.name, err, c.wantField)
		}
	}
}

func TestValidateRejectsWhitespaceInCanonicalURL(t *testing.T) {
	for _, url := range []string{"https://x.test/a b", "https://x.test/a b"} {
		p := &Post{Slug: "a", Title: "A", Status: Draft, CanonicalURL: url}
		if err := Validate(p); !IsFieldError(err) {
			t.Errorf("Validate(%q) = %v, want a field error", url, err)
		}
	}
	p := &Post{Slug: "a", Title: "A", Status: Draft, CanonicalURL: "https://x.test/a%20b"}
	if err := Validate(p); err != nil {
		t.Errorf("encoded space: %v", err)
	}
}
