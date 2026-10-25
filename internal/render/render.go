// Package render turns post Markdown into sanitized HTML for previews.
package render

import (
	"bytes"
	"fmt"
	"regexp"

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

// MaxInput bounds what one preview may render.
const MaxInput = 2 << 20

// HTML renders Markdown to sanitized HTML.
func HTML(source string) (string, error) {
	if len(source) > MaxInput {
		return "", fmt.Errorf("the post is too large to preview")
	}
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(source), &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return policy.Sanitize(buf.String()), nil
}
