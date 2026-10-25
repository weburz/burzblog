// Package render turns post Markdown into sanitized HTML for previews.
package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

var (
	markdown = goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		// Raw HTML in Markdown is dropped; posts are text, not pages.
	)
	policy = newPolicy()
)

func newPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "hr", "blockquote", "pre", "code", "em", "strong", "del", "sup", "sub",
		"ul", "ol", "li", "table", "thead", "tbody", "tr", "th", "td", "section")
	p.AllowAttrs("id").Matching(regexp.MustCompile(`^[A-Za-z0-9_:.-]{1,128}$`)).
		OnElements("h1", "h2", "h3", "h4", "h5", "h6", "li", "sup")
	p.AllowElements("h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^(language-[A-Za-z0-9_+-]{1,40}|footnotes|footnote-ref|footnote-backref)$`)).
		OnElements("code", "a", "section")
	p.AllowAttrs("align").Matching(regexp.MustCompile(`^(left|right|center)$`)).OnElements("th", "td")
	p.AllowAttrs("style").Matching(regexp.MustCompile(`^text-align:(left|right|center)$`)).OnElements("th", "td")
	p.AllowAttrs("start").Matching(bluemonday.Integer).OnElements("ol")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	p.AllowElements("input")
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("src", "alt", "title").OnElements("img")
	p.AllowAttrs("title").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}

const (
	// MaxInput bounds what one preview may render. It equals the largest
	// post body (post.MaxBody), so an author can preview anything they can
	// save; checkConstructs and MaxOutput bound the cost, not the size.
	MaxInput = 2 << 20
	// MaxOutput bounds the HTML one preview may produce before sanitizing.
	MaxOutput = 8 << 20
	// MaxConcurrent is how many previews render at once; the rest wait.
	MaxConcurrent = 2
)

// maxWait is how long a preview waits for a free slot.
var maxWait = 5 * time.Second

// ErrBusy means every render slot stayed taken for longer than a preview
// should wait.
var ErrBusy = errors.New("previews are busy; try again in a moment")

var slots = make(chan struct{}, MaxConcurrent)

// HTML renders Markdown to sanitized HTML. It returns a *LimitError when the
// post is too large or built from constructs previews do not support, and
// ErrBusy when no render slot frees up in time. ctx ends the wait for a slot.
func HTML(ctx context.Context, source string) (string, error) {
	if len(source) > MaxInput {
		return "", &LimitError{fmt.Sprintf("The post is larger than the %d MiB previews support.", MaxInput>>20)}
	}
	if err := checkConstructs(source); err != nil {
		return "", err
	}
	wait := time.NewTimer(maxWait)
	defer wait.Stop()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	case <-wait.C:
		return "", ErrBusy
	}
	out := &limitedBuffer{limit: MaxOutput}
	if err := markdown.Convert([]byte(source), out); err != nil {
		if errors.Is(err, errOutputTooLarge) {
			return "", &LimitError{fmt.Sprintf("The preview would be larger than %d MiB. Look for very wide tables or deeply nested lists.", MaxOutput>>20)}
		}
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return policy.Sanitize(out.String()), nil
}

var errOutputTooLarge = errors.New("output too large")

// limitedBuffer is a bytes.Buffer that fails once it would hold more than
// limit bytes.
type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errOutputTooLarge
	}
	return b.Buffer.Write(p)
}
