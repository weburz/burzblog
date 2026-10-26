// Package post holds the BurzBlog post model, its validation rules and the
// frontmatter document format shared by the CLI and reader exports. It does no
// IO and knows nothing about storage or HTTP.
package post

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
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
	MaxTags        = 20
	MaxTag         = 40
	MaxAuthor      = 100
	MaxSEOTitle    = 120
	MaxSEODesc     = 320
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
// Tags are the post's topics and categories in one list: lowercase words with
// hyphens, in the author's order. SEOTitle, SEODescription and CanonicalURL
// are for search engines and social cards; empty means the site's default.
type Post struct {
	ID             int64
	Slug           string
	Title          string
	Description    string
	Status         Status
	Body           string
	CoverURL       string
	CoverAlt       string
	Extra          string
	Tags           []string
	Author         string
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	PublishedOn    *time.Time
	UpdatedOn      *time.Time
	// PublishAt is when a draft publishes itself; nil when not scheduled.
	// Only drafts carry it. PublishBy is the account that scheduled it.
	PublishAt *time.Time
	PublishBy string
	CreatedAt time.Time
	UpdatedAt time.Time
	UpdatedBy string
	Version   int64
}

// MaxScheduleAhead is how far in the future a post can be scheduled.
const MaxScheduleAhead = 2 * 365 * 24 * time.Hour

// ValidateSchedule checks that at is after now and within MaxScheduleAhead.
func ValidateSchedule(at, now time.Time) error {
	switch {
	case !at.After(now):
		return &FieldError{"publish time", "must be in the future."}
	case at.After(now.Add(MaxScheduleAhead)):
		return &FieldError{"publish time", "must be within 2 years from now."}
	}
	return nil
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

// HasControl reports whether s contains a control character: C0 (U+0000 to
// U+001F, which includes tab, newline and escape), DEL, or C1 (U+0080 to
// U+009F). Terminals act on these, so they are not allowed in single-line
// text that the CLI prints.
func HasControl(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

// Validate checks every field of p. Single-line fields reject every control
// character, tabs included. The body keeps tabs and line breaks and is only
// checked for NUL, so existing Markdown (ANSI samples in code blocks, say)
// still saves; the CLI sanitizes anything it prints to a terminal.
func Validate(p *Post) error {
	if err := ValidateSlug(p.Slug); err != nil {
		return err
	}
	if !ValidStatus(p.Status) {
		return &FieldError{"status", "must be draft, published or archived."}
	}
	if p.PublishAt != nil && p.Status != Draft {
		return &FieldError{"publish time", "can only be set on a draft."}
	}
	if err := checkLines([]lineCheck{
		{"title", p.Title, MaxTitle},
		{"description", p.Description, MaxDescription},
		{"cover URL", p.CoverURL, MaxURL},
		{"cover alt text", p.CoverAlt, MaxAlt},
	}); err != nil {
		return err
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
	if err := validateMetadata(p); err != nil {
		return err
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

type lineCheck struct {
	field, value string
	max          int
}

// checkLines holds single-line text to its length cap and rejects control
// characters.
func checkLines(checks []lineCheck) error {
	for _, c := range checks {
		if !utf8.ValidString(c.value) {
			return &FieldError{c.field, "is not valid UTF-8."}
		}
		if strings.ContainsAny(c.value, "\x00\r\n") {
			return &FieldError{c.field, "must be a single line."}
		}
		if HasControl(c.value) {
			return &FieldError{c.field, "must not contain control characters such as tabs or escape sequences. Remove them and save again."}
		}
		if utf8.RuneCountInString(c.value) > c.max {
			return &FieldError{c.field, fmt.Sprintf("must be at most %d characters.", c.max)}
		}
	}
	return nil
}

// validateMetadata checks the blog metadata: author, SEO fields and tags.
func validateMetadata(p *Post) error {
	if err := checkLines([]lineCheck{
		{"author", p.Author, MaxAuthor},
		{"SEO title", p.SEOTitle, MaxSEOTitle},
		{"SEO description", p.SEODescription, MaxSEODesc},
		{"canonical URL", p.CanonicalURL, MaxURL},
	}); err != nil {
		return err
	}
	if p.CanonicalURL != "" {
		if u, err := url.Parse(p.CanonicalURL); err != nil || u.Scheme != "https" || u.Host == "" {
			return &FieldError{"canonical URL", "must be a full https address such as https://example.com/post."}
		}
	}
	return validateTags(p.Tags)
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return &FieldError{"cover URL", "must be a full http(s) address."}
	}
	return nil
}

// NormalizeTag turns text into a tag: lowercase letters, digits and marks,
// with single hyphens between words. "Machine Learning" becomes
// "machine-learning". The result is empty when nothing usable is left.
func NormalizeTag(raw string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(raw) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
			hyphen = false
		case b.Len() > 0 && !hyphen:
			b.WriteByte('-')
			hyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// normalizeTags normalizes each tag, drops empty ones and repeats, and keeps
// the author's order.
func normalizeTags(tags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range tags {
		tag := NormalizeTag(raw)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

func validateTags(tags []string) error {
	if len(tags) > MaxTags {
		return &FieldError{"tags", fmt.Sprintf("allow at most %d tags. Remove some and save again.", MaxTags)}
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		switch {
		case utf8.RuneCountInString(tag) > MaxTag:
			return &FieldError{"tags", fmt.Sprintf("must be at most %d characters each; shorten %q.", MaxTag, tag)}
		case tag == "" || NormalizeTag(tag) != tag:
			return &FieldError{"tags", fmt.Sprintf("may only use lowercase letters, digits and single hyphens between words; fix %q.", tag)}
		case seen[tag]:
			return &FieldError{"tags", fmt.Sprintf("lists %q twice.", tag)}
		}
		seen[tag] = true
	}
	return nil
}

// Normalize trims incidental whitespace from single-line fields, cleans the
// tags and uses LF line endings in the body.
func Normalize(p *Post) {
	p.Title = strings.TrimSpace(p.Title)
	p.Description = strings.TrimSpace(p.Description)
	p.CoverURL = strings.TrimSpace(p.CoverURL)
	p.CoverAlt = strings.TrimSpace(p.CoverAlt)
	p.Author = strings.TrimSpace(p.Author)
	p.SEOTitle = strings.TrimSpace(p.SEOTitle)
	p.SEODescription = strings.TrimSpace(p.SEODescription)
	p.CanonicalURL = strings.TrimSpace(p.CanonicalURL)
	p.Tags = normalizeTags(p.Tags)
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
		a.CoverAlt == b.CoverAlt && a.Extra == b.Extra && slices.Equal(a.Tags, b.Tags) &&
		a.Author == b.Author && a.SEOTitle == b.SEOTitle && a.SEODescription == b.SEODescription &&
		a.CanonicalURL == b.CanonicalURL &&
		sameTime(a.PublishedOn, b.PublishedOn) && sameTime(a.UpdatedOn, b.UpdatedOn) &&
		sameTime(a.PublishAt, b.PublishAt)
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
