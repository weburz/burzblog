package render

import (
	"bytes"
	"strconv"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const lineAttr = "data-line"

// lineMarker tags each top-level block with the 1-based line it starts on.
// Blocks with no position in the source (thematic breaks) stay untagged.
type lineMarker struct{}

func (lineMarker) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	line, at := 1, 0
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		offset, ok := startOffset(n)
		if !ok {
			continue
		}
		if _, fenced := n.(*ast.FencedCodeBlock); fenced {
			offset = previousLineStart(source, offset)
		}
		if offset < at {
			continue
		}
		line += bytes.Count(source[at:offset], []byte{'\n'})
		at = offset
		n.SetAttribute([]byte(lineAttr), []byte(strconv.Itoa(line)))
	}
}

// previousLineStart is the offset where the line before the one containing
// offset begins: a fenced block's content starts a line after its fence.
func previousLineStart(source []byte, offset int) int {
	end := bytes.LastIndexByte(source[:offset], '\n')
	if end < 0 {
		return 0
	}
	return bytes.LastIndexByte(source[:end], '\n') + 1
}

// startOffset is where the first text of n sits in the source.
func startOffset(n ast.Node) (int, bool) {
	if t, ok := n.(*ast.Text); ok {
		return t.Segment.Start, true
	}
	if n.Type() == ast.TypeBlock && n.Lines().Len() > 0 {
		return n.Lines().At(0).Start, true
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if offset, ok := startOffset(c); ok {
			return offset, true
		}
	}
	return 0, false
}

// codeBlockRenderer renders code blocks like goldmark does, plus data-line on
// the <pre>, which goldmark's own renderer has no room for.
type codeBlockRenderer struct{}

func (codeBlockRenderer) RegisterFuncs(r renderer.NodeRendererFuncRegisterer) {
	r.Register(ast.KindCodeBlock, renderCode)
	r.Register(ast.KindFencedCodeBlock, renderCode)
}

func renderCode(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</code></pre>\n")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<pre")
	if v, ok := n.Attribute([]byte(lineAttr)); ok {
		if b, ok := v.([]byte); ok {
			_, _ = w.WriteString(` data-line="`)
			_, _ = w.Write(b)
			_ = w.WriteByte('"')
		}
	}
	_, _ = w.WriteString("><code")
	if f, ok := n.(*ast.FencedCodeBlock); ok {
		if lang := f.Language(source); lang != nil {
			_, _ = w.WriteString(` class="language-`)
			html.DefaultWriter.Write(w, lang)
			_, _ = w.WriteString(`"`)
		}
	}
	_ = w.WriteByte('>')
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		html.DefaultWriter.RawWrite(w, seg.Value(source))
	}
	return ast.WalkContinue, nil
}
