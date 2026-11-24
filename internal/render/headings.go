package render

import (
	"html"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// headingIDs uses visible inline text so a linked heading doesn't acquire
// its destination URL. Suffixes are unique within this Markdown document.
type headingIDs struct{}

func (headingIDs) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	used := make(map[string]bool)
	next := make(map[string]int)
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != ast.KindHeading {
			return ast.WalkContinue, nil
		}
		var letters []rune
		for _, r := range strings.TrimSpace(html.UnescapeString(string(n.Text(reader.Source())))) {
			switch {
			case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
				letters = append(letters, unicode.ToLower(r))
			case r == '-' || r == '_' || unicode.IsSpace(r):
				letters = append(letters, '-')
			}
			if len(letters) == 120 {
				break
			}
		}
		base := string(letters)
		if base == "" {
			base = "heading"
		}
		id := base
		for used[id] {
			next[base]++
			id = base + "-" + strconv.Itoa(next[base])
		}
		used[id] = true
		n.SetAttributeString("id", []byte(id))
		return ast.WalkSkipChildren, nil
	})
}
