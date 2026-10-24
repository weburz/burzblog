package post

import (
	"strings"
	"testing"
	"time"
)

func TestValidateSlug(t *testing.T) {
	good := []string{"a", "hello-world", "go-1-27", strings.Repeat("a", MaxSlug)}
	for _, s := range good {
		if err := ValidateSlug(s); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, want nil", s, err)
		}
	}
	bad := []string{"", "Hello", "a--b", "-a", "a-", "a b", "a/b", "../x", strings.Repeat("a", MaxSlug+1)}
	for _, s := range bad {
		if err := ValidateSlug(s); err == nil {
			t.Errorf("ValidateSlug(%q) = nil, want error", s)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello, World!":           "hello-world",
		"  Go 1.27 -- what's new": "go-1-27-what-s-new",
		"!!!":                     "",
		"Ünïcode only":            "n-code-only",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Slugify(strings.Repeat("word ", 100)); len(got) > MaxSlug || strings.HasSuffix(got, "-") {
		t.Errorf("Slugify of a long title = %q", got)
	}
}

func valid() *Post {
	return &Post{Slug: "hello", Title: "Hello", Status: Draft, Body: "# Hi\n"}
}

func TestValidate(t *testing.T) {
	if err := Validate(valid()); err != nil {
		t.Fatalf("Validate(valid) = %v", err)
	}
	cases := map[string]func(*Post){
		"empty title":      func(p *Post) { p.Title = "  " },
		"multi-line title": func(p *Post) { p.Title = "a\nb" },
		"long title":       func(p *Post) { p.Title = strings.Repeat("x", MaxTitle+1) },
		"bad status":       func(p *Post) { p.Status = "live" },
		"relative cover":   func(p *Post) { p.CoverURL = "/img.png" },
		"script cover":     func(p *Post) { p.CoverURL = "javascript:alert(1)" },
		"alt without url":  func(p *Post) { p.CoverAlt = "a cat" },
		"nul in body":      func(p *Post) { p.Body = "a\x00b" },
		"huge body":        func(p *Post) { p.Body = strings.Repeat("x", MaxBody+1) },
		"reserved extra":   func(p *Post) { p.Extra = "title: sneaky\n" },
		"list extra":       func(p *Post) { p.Extra = "- a\n- b\n" },
		"alias extra":      func(p *Post) { p.Extra = "a: &x 1\nb: *x\n" },
	}
	for name, mutate := range cases {
		p := valid()
		mutate(p)
		err := Validate(p)
		if err == nil || !IsFieldError(err) {
			t.Errorf("%s: Validate = %v, want a FieldError", name, err)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	published := time.Date(2026, 4, 25, 1, 46, 48, 0, time.UTC)
	p := &Post{
		Slug:        "auth-guide",
		Title:       "Web Authentication: Pros, Cons",
		Description: "yes",
		Status:      Published,
		CoverURL:    "https://ik.imagekit.io/jarmos/a.svg",
		CoverAlt:    "A guide.",
		PublishedOn: &published,
		Extra:       "sitemap:\n  loc: /blogs/auth-guide\n  priority: 1\n",
		Body:        "Intro\n\n## Part\n\n---\n\nAfter a rule.\n",
	}
	doc := Encode(p)
	for _, want := range []string{
		"title: 'Web Authentication: Pros, Cons'", "description: \"yes\"", "status: published",
		"publishedOn: 2026-04-25T01:46:48Z", "url: https://ik.imagekit.io/jarmos/a.svg",
		"sitemap:\n  loc: /blogs/auth-guide",
	} {
		if !strings.Contains(doc, want) && !strings.Contains(doc, strings.ReplaceAll(want, "'", "\"")) {
			t.Errorf("Encode output missing %q:\n%s", want, doc)
		}
	}
	d, err := Decode(doc)
	if err != nil {
		t.Fatalf("Decode(Encode(p)) = %v\n%s", err, doc)
	}
	back := &Post{Slug: p.Slug}
	d.Apply(back)
	if !SameContent(p, back) {
		t.Errorf("round trip changed the post:\n got %+v\nwant %+v", back, p)
	}
}

func TestDecodeJarmosPost(t *testing.T) {
	doc := `---
title:
  "Web Authentication Methods Explained: Pros, Cons, and Best Practices (2026
  Guide)"
description:
  Comprehensive guide to authentication in web applications.
timestamps:
  publishedOn: 2026-04-25T01:46:48+00:00
status: published
coverImage:
  url: https://ik.imagekit.io/jarmos/x.svg
  alt: A comprehensive guide.
sitemap:
  loc: /blogs/a
  lastmod: 2026-05-01
---

Body text.
`
	d, err := Decode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Web Authentication Methods Explained: Pros, Cons, and Best Practices (2026 Guide)" {
		t.Errorf("title = %q", d.Title)
	}
	if d.Status != Published || d.PublishedOn == nil || d.PublishedOn.Year() != 2026 {
		t.Errorf("status/timestamp = %v %v", d.Status, d.PublishedOn)
	}
	if d.CoverAlt != "A comprehensive guide." || d.Body != "Body text.\n" {
		t.Errorf("cover alt %q body %q", d.CoverAlt, d.Body)
	}
	if !strings.Contains(d.Extra, "lastmod: 2026-05-01") {
		t.Errorf("extra lost sitemap: %q", d.Extra)
	}
}

func TestDecodeErrors(t *testing.T) {
	cases := map[string]string{
		"no frontmatter":  "# Just markdown\n",
		"unclosed":        "---\ntitle: x\nbody",
		"empty":           "---\n---\nbody",
		"bad yaml":        "---\ntitle: [x\n---\n",
		"list root":       "---\n- a\n---\n",
		"archived status": "---\ntitle: x\nstatus: archived\n---\n",
		"slug key":        "---\ntitle: x\nslug: y\n---\n",
		"bad date":        "---\ntitle: x\ntimestamps:\n  publishedOn: soon\n---\n",
		"unknown ts":      "---\ntitle: x\ntimestamps:\n  editedOn: 2026-01-01\n---\n",
		"duplicate":       "---\ntitle: x\ntitle: y\n---\n",
		"alias":           "---\ntitle: x\na: &k [1]\nb: *k\n---\n",
		"custom tag":      "---\ntitle: x\na: !evil 1\n---\n",
		"cover string":    "---\ntitle: x\ncoverImage: https://x\n---\n",
	}
	for name, doc := range cases {
		if _, err := Decode(doc); err == nil || !IsFieldError(err) {
			t.Errorf("%s: Decode = %v, want a FieldError", name, err)
		}
	}
}

func TestDecodeKeepsBodyExactly(t *testing.T) {
	body := "  indented first line\n\n\n---\nnot frontmatter\n"
	d, err := Decode("---\ntitle: x\n---\n\n" + body)
	if err != nil {
		t.Fatal(err)
	}
	if d.Body != body {
		t.Errorf("body = %q, want %q", d.Body, body)
	}
	d, err = Decode("---\ntitle: x\n---")
	if err != nil || d.Body != "" {
		t.Errorf("frontmatter only: %v %q", err, d.Body)
	}
}
