package post

import (
	"strings"
	"testing"
)

func TestFeaturedRoundTripsThroughFrontmatter(t *testing.T) {
	p := valid()
	p.Featured = true
	doc := Encode(p)
	if !strings.Contains(doc, "\nfeatured: true\n") {
		t.Fatalf("Encode lacks featured: true:\n%s", doc)
	}
	d, err := Decode(doc)
	if err != nil || !d.Featured {
		t.Fatalf("Decode featured = %v, %v", d, err)
	}
	var back Post
	d.Apply(&back)
	if !back.Featured || back.Extra != "" {
		t.Errorf("applied = featured %v extra %q", back.Featured, back.Extra)
	}
	// Not featured writes no key, and an explicit false reads as false.
	if plain := Encode(valid()); strings.Contains(plain, "featured") {
		t.Errorf("unfeatured post writes a featured key:\n%s", plain)
	}
	d, err = Decode("---\ntitle: T\nfeatured: false\n---\n")
	if err != nil || d.Featured || d.Extra != "" {
		t.Errorf("featured: false = %+v, %v", d, err)
	}
}

func TestDecodeRejectsFeaturedThatIsNotABoolean(t *testing.T) {
	for _, value := range []string{"yes", "\"true\"", "1", "[a]", "{a: b}"} {
		_, err := Decode("---\ntitle: T\nfeatured: " + value + "\n---\n")
		if err == nil || !IsFieldError(err) || !strings.Contains(err.Error(), "must be true or false") {
			t.Errorf("featured: %s = %v, want a true-or-false error", value, err)
		}
	}
}

func TestValidateRefusesFeaturedInExtra(t *testing.T) {
	p := valid()
	p.Extra = "featured: true\n"
	if err := Validate(p); err == nil || !strings.Contains(err.Error(), `must not set "featured"`) {
		t.Errorf("Validate = %v, want featured refused in extra", err)
	}
}

func TestMoveFeaturedFromExtra(t *testing.T) {
	featured, rest, ok := MoveFeaturedFromExtra("sitemap:\n  priority: 1\nfeatured: true\nseo:\n  robots: noindex\n")
	if !ok || !featured || strings.Contains(rest, "featured") ||
		!strings.Contains(rest, "priority: 1") || !strings.Contains(rest, "robots: noindex") {
		t.Errorf("moved = %v %q %v", featured, rest, ok)
	}
	if featured, rest, ok = MoveFeaturedFromExtra("featured: false\n"); !ok || featured || rest != "" {
		t.Errorf("only featured: false = %v %q %v", featured, rest, ok)
	}
	for _, extra := range []string{"", "sitemap: 1\n", "featured: maybe\n", "featured: [1]\n", "not: [valid"} {
		if _, rest, ok := MoveFeaturedFromExtra(extra); ok || rest != extra {
			t.Errorf("MoveFeaturedFromExtra(%q) = %q, %v; want unchanged", extra, rest, ok)
		}
	}
}

func TestMoveMetadataFromExtraKeepsFeatured(t *testing.T) {
	// Migration 4 predates the featured column, so it must leave the key.
	moved, rest, ok := MoveMetadataFromExtra("author: Ada\nfeatured: true\n")
	if !ok || moved.Author != "Ada" || !strings.Contains(rest, "featured: true") {
		t.Errorf("moved = %+v rest %q ok %v", moved, rest, ok)
	}
}

func TestDefaultTagLabel(t *testing.T) {
	for tag, want := range map[string]string{
		"constitutional-law": "Constitutional Law",
		"go":                 "Go",
		"über-cool":          "Über Cool",
		"2026-budget":        "2026 Budget",
		"c":                  "C",
		"":                   "",
	} {
		if got := DefaultTagLabel(tag); got != want {
			t.Errorf("DefaultTagLabel(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestNormalizeTagLabel(t *testing.T) {
	if got, err := NormalizeTagLabel("  Verfassungsrecht & Märchen  "); err != nil || got != "Verfassungsrecht & Märchen" {
		t.Errorf("NormalizeTagLabel = %q, %v", got, err)
	}
	if _, err := NormalizeTagLabel(strings.Repeat("é", MaxTagLabel)); err != nil {
		t.Errorf("a label of %d characters = %v, want ok", MaxTagLabel, err)
	}
	for name, raw := range map[string]string{
		"empty":     "",
		"blank":     "   ",
		"too long":  strings.Repeat("x", MaxTagLabel+1),
		"newline":   "a\nb",
		"tab":       "a\tb",
		"escape":    "a\x1b[31m",
		"bad UTF-8": "a\xffb",
	} {
		if _, err := NormalizeTagLabel(raw); err == nil || !IsFieldError(err) {
			t.Errorf("%s = %v, want a field error", name, err)
		}
	}
}
