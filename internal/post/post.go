// Package post holds the BurzBlog post model, its validation rules and the
// frontmatter document format shared by the CLI and reader exports. It does no
// IO and knows nothing about storage or HTTP.
package post

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Status is the editorial state of a post.
type Status string

// The statuses a post can be in. Only published posts reach readers.
const (
	Draft     Status = "draft"
	Published Status = "published"
	Archived  Status = "archived"
)

// Limits keep one post small enough to edit comfortably and to render on a
// Raspberry Pi without surprises.
const (
	MaxSlug        = 120
	MaxTitle       = 300
	MaxDescription = 2000
	MaxURL         = 2048
	MaxAlt         = 500
	MaxBody        = 2 << 20
	MaxExtra       = 64 << 10
)

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	wordPattern = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}'’-]*`)
)

// CountWords counts words the way the editor does: runs of letters and
// digits, so Markdown symbols are not words.
func CountWords(text string) int {
	return len(wordPattern.FindAllStringIndex(text, -1))
}

// Post is one article. Body is Markdown. Extra holds frontmatter keys that
// BurzBlog does not model, as a YAML mapping, so they survive round trips.
type Post struct {
	ID          int64
	Slug        string
	Title       string
	Description string
	Status      Status
	Body        string
	CoverURL    string
	CoverAlt    string
	Extra       string
	PublishedOn *time.Time
	UpdatedOn   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	UpdatedBy   string
	Version     int64
}

// FieldError is a validation failure people can act on.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string {
	return e.Field + " " + e.Message
}

// IsFieldError reports whether err is a validation failure.
func IsFieldError(err error) bool {
	var fe *FieldError
	return errors.As(err, &fe)
}

// ValidStatus reports whether s is a known status.
func ValidStatus(s Status) bool {
	return s == Draft || s == Published || s == Archived
}

// ValidateSlug checks a URL-safe post identifier.
func ValidateSlug(slug string) error {
	switch {
	case slug == "":
		return &FieldError{"slug", "must not be empty."}
	case len(slug) > MaxSlug:
		return &FieldError{"slug", fmt.Sprintf("must be at most %d characters.", MaxSlug)}
	case !slugPattern.MatchString(slug):
		return &FieldError{"slug", "may only use lowercase letters, digits and single hyphens between words."}
	}
	return nil
}

// Slugify turns a title into a slug suggestion. The result may be empty.
func Slugify(title string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			hyphen = false
		case b.Len() > 0 && !hyphen:
			b.WriteByte('-')
			hyphen = true
		}
		if b.Len() >= MaxSlug {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// Validate checks every field of p.
func Validate(p *Post) error {
	if err := ValidateSlug(p.Slug); err != nil {
		return err
	}
	if !ValidStatus(p.Status) {
		return &FieldError{"status", "must be draft, published or archived."}
	}
	checks := []struct {
		field, value string
		max          int
	}{
		{"title", p.Title, MaxTitle},
		{"description", p.Description, MaxDescription},
		{"cover URL", p.CoverURL, MaxURL},
		{"cover alt text", p.CoverAlt, MaxAlt},
	}
	for _, c := range checks {
		if !utf8.ValidString(c.value) {
			return &FieldError{c.field, "is not valid UTF-8."}
		}
		if strings.ContainsAny(c.value, "\x00\r\n") {
			return &FieldError{c.field, "must be a single line."}
		}
		if utf8.RuneCountInString(c.value) > c.max {
			return &FieldError{c.field, fmt.Sprintf("must be at most %d characters.", c.max)}
		}
	}
	if strings.TrimSpace(p.Title) == "" {
		return &FieldError{"title", "must not be empty."}
	}
	if p.CoverURL != "" {
		if err := validateURL(p.CoverURL); err != nil {
			return err
		}
	} else if p.CoverAlt != "" {
		return &FieldError{"cover alt text", "needs a cover URL."}
	}
	if !utf8.ValidString(p.Body) || strings.ContainsRune(p.Body, 0) {
		return &FieldError{"body", "contains bytes that are not text."}
	}
	if len(p.Body) > MaxBody {
		return &FieldError{"body", fmt.Sprintf("must be at most %d MiB.", MaxBody>>20)}
	}
	if len(p.Extra) > MaxExtra {
		return &FieldError{"extra frontmatter", fmt.Sprintf("must be at most %d KiB.", MaxExtra>>10)}
	}
	if _, err := parseExtra(p.Extra); err != nil {
		return err
	}
	return nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return &FieldError{"cover URL", "must be a full http(s) address."}
	}
	return nil
}

// Normalize trims incidental whitespace from single-line fields and uses LF
// line endings in the body.
func Normalize(p *Post) {
	p.Title = strings.TrimSpace(p.Title)
	p.Description = strings.TrimSpace(p.Description)
	p.CoverURL = strings.TrimSpace(p.CoverURL)
	p.CoverAlt = strings.TrimSpace(p.CoverAlt)
	p.Body = strings.ReplaceAll(p.Body, "\r\n", "\n")
	p.Extra = strings.ReplaceAll(p.Extra, "\r\n", "\n")
	if strings.TrimSpace(p.Extra) == "" {
		p.Extra = ""
	}
}

// SameContent reports whether a and b would read the same to an author, so a
// save can skip creating a new version.
func SameContent(a, b *Post) bool {
	return a.Slug == b.Slug && a.Title == b.Title && a.Description == b.Description &&
		a.Status == b.Status && sameBody(a.Body, b.Body) && a.CoverURL == b.CoverURL &&
		a.CoverAlt == b.CoverAlt && a.Extra == b.Extra &&
		sameTime(a.PublishedOn, b.PublishedOn) && sameTime(a.UpdatedOn, b.UpdatedOn)
}

// sameBody ignores one trailing newline: files always end with one, the
// browser editor usually does not, and neither is a change worth a version.
func sameBody(a, b string) bool {
	return strings.TrimSuffix(a, "\n") == strings.TrimSuffix(b, "\n")
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
