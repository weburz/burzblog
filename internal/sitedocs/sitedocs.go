// Package sitedocs renders the Markdown guides in docs/ into the project
// website (site/docs/), so the guides read the same on GitHub and on the site.
// The output is committed: the Pages workflow publishes site/ as it is.
package sitedocs

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Repo is where links to files outside docs/ point.
const Repo = "https://github.com/weburz/burzblog/blob/main/"

// Site is the published address of the project website.
const Site = "https://burzblog.weburz.com/"

// Group is a titled set of guides on the docs index and in the sidebar.
type Group struct {
	Title string
	Names []string // docs/<name>.md, in reading order
}

// Groups lists every guide the site publishes. A doc missing here is an error,
// so a new guide cannot silently stay off the site.
var Groups = []Group{
	{"Guides", []string{"self-hosting", "terminal", "publishing", "containers"}},
	{"Reference", []string{"api", "security", "architecture"}},
}

// Heading is one section of a guide, for the page's table of contents.
type Heading struct {
	ID, Text string
}

// Page is one rendered guide.
type Page struct {
	Name     string
	Title    string
	Summary  string
	Headings []Heading
	Body     template.HTML
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
		parser.WithASTTransformers(util.Prioritized(linkRewriter{}, 100)),
	),
	// The guides are ours and use inline anchors such as <a id="...">.
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// Build renders every guide in docsDir into outDir, replacing the .html files
// there. It fails when a guide is missing, unlisted in Groups, or links to an
// anchor that does not exist.
func Build(docsDir, outDir string) error {
	pages, err := Render(docsDir)
	if err != nil {
		return err
	}
	files, err := Files(pages)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", outDir, err)
	}
	old, err := filepath.Glob(filepath.Join(outDir, "*.html"))
	if err != nil {
		return err
	}
	for _, f := range old {
		if _, keep := files[filepath.Base(f)]; !keep {
			if err := os.Remove(f); err != nil {
				return fmt.Errorf("remove stale %s: %w", f, err)
			}
		}
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// Render parses every guide listed in Groups and checks that links between
// guides land on real headings.
func Render(docsDir string) ([]Page, error) {
	listed := map[string]bool{}
	var pages []Page
	for _, g := range Groups {
		for _, name := range g.Names {
			listed[name] = true
			src, err := os.ReadFile(filepath.Join(docsDir, name+".md"))
			if err != nil {
				return nil, fmt.Errorf("read guide %s: %w", name, err)
			}
			p, err := renderPage(name, src)
			if err != nil {
				return nil, fmt.Errorf("render %s.md: %w", name, err)
			}
			pages = append(pages, p)
		}
	}
	all, err := filepath.Glob(filepath.Join(docsDir, "*.md"))
	if err != nil {
		return nil, err
	}
	for _, f := range all {
		if name := strings.TrimSuffix(filepath.Base(f), ".md"); !listed[name] {
			return nil, fmt.Errorf("docs/%s.md is not on the site: add it to sitedocs.Groups", name)
		}
	}
	return pages, checkAnchors(pages)
}

func renderPage(name string, src []byte) (Page, error) {
	doc := md.Parser().Parse(text.NewReader(src))
	p := Page{Name: name}
	var title ast.Node
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			id, _ := n.AttributeString("id")
			idStr, _ := id.([]byte)
			switch {
			case n.Level == 1 && p.Title == "":
				p.Title = plain(n, src)
				title = n
			case n.Level == 2:
				p.Headings = append(p.Headings, Heading{ID: string(idStr), Text: plain(n, src)})
			}
			return ast.WalkSkipChildren, nil
		case *ast.Paragraph:
			if p.Summary == "" && p.Title != "" {
				p.Summary = plain(n, src)
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return p, err
	}
	if p.Title == "" {
		return p, errors.New("the guide has no top-level # title")
	}
	// The layout prints the title; drop it from the body.
	title.Parent().RemoveChild(title.Parent(), title)
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return p, err
	}
	p.Body = template.HTML(buf.String()) //nolint:gosec // rendered from the repository's own docs
	return p, nil
}

// plain is the visible text of a node: inline code keeps its text, markup goes.
func plain(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(src))
			if c.SoftLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(c.Value)
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}

// linkRewriter points links to other guides at their pages, and links to
// anything else in the repository at GitHub.
type linkRewriter struct{}

func (linkRewriter) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if l, ok := n.(*ast.Link); ok && entering {
			l.Destination = []byte(rewrite(string(l.Destination)))
		}
		return ast.WalkContinue, nil
	})
}

var external = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:`)

func rewrite(dest string) string {
	if dest == "" || strings.HasPrefix(dest, "#") || external.MatchString(dest) {
		return dest
	}
	file, frag, _ := strings.Cut(dest, "#")
	if frag != "" {
		frag = "#" + frag
	}
	if !strings.Contains(file, "/") && strings.HasSuffix(file, ".md") {
		return strings.TrimSuffix(file, ".md") + ".html" + frag
	}
	return Repo + path.Clean(path.Join("docs", file)) + frag
}

var anchorLink = regexp.MustCompile(`href="([a-z0-9-]*)(?:\.html)?#([^"]+)"`)
var idAttr = regexp.MustCompile(`id="([^"]+)"`)

func checkAnchors(pages []Page) error {
	ids := map[string]map[string]bool{}
	for _, p := range pages {
		ids[p.Name] = map[string]bool{}
		for _, m := range idAttr.FindAllStringSubmatch(string(p.Body), -1) {
			ids[p.Name][m[1]] = true
		}
	}
	var errs []error
	for _, p := range pages {
		for _, m := range anchorLink.FindAllStringSubmatch(string(p.Body), -1) {
			target := m[1]
			if target == "" {
				target = p.Name
			}
			if have, ok := ids[target]; ok && !have[m[2]] {
				errs = append(errs, fmt.Errorf("docs/%s.md links to %s.md#%s, which has no such heading", p.Name, target, m[2]))
			}
		}
	}
	return errors.Join(errs...)
}

// Files is every page of the docs section, keyed by file name.
func Files(pages []Page) (map[string][]byte, error) {
	byName := map[string]Page{}
	for _, p := range pages {
		byName[p.Name] = p
	}
	type navGroup struct {
		Title string
		Pages []Page
	}
	var nav []navGroup
	for _, g := range Groups {
		ng := navGroup{Title: g.Title}
		for _, n := range g.Names {
			ng.Pages = append(ng.Pages, byName[n])
		}
		nav = append(nav, ng)
	}
	out := map[string][]byte{}
	render := func(file string, data any) error {
		var buf bytes.Buffer
		if err := layout.Execute(&buf, data); err != nil {
			return fmt.Errorf("render %s: %w", file, err)
		}
		out[file] = buf.Bytes()
		return nil
	}
	if err := render("index.html", map[string]any{
		"Nav": nav, "Index": true, "Title": "Documentation", "File": "", "Current": "",
		"Summary": "Guides for installing, writing with and connecting BurzBlog, and the reference for its API and security model.",
	}); err != nil {
		return nil, err
	}
	for _, p := range pages {
		if err := render(p.Name+".html", map[string]any{
			"Nav": nav, "Page": p, "Current": p.Name, "Title": p.Title, "File": p.Name + ".html", "Summary": p.Summary,
			"Source": Repo + "docs/" + p.Name + ".md",
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

var layout = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>{{.Title}} · BurzBlog docs</title>
    <meta name="description" content="{{.Summary}}" />
    <meta
      http-equiv="Content-Security-Policy"
      content="default-src 'none'; img-src 'self'; style-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'"
    />
    <meta name="color-scheme" content="light dark" />
    <meta name="theme-color" content="#fafaf8" media="(prefers-color-scheme: light)" />
    <meta name="theme-color" content="#12171d" media="(prefers-color-scheme: dark)" />
    <link rel="canonical" href="` + Site + `docs/{{.File}}" />
    <link rel="icon" href="../assets/favicon.svg" type="image/svg+xml" />
    <link rel="stylesheet" href="../style.css" />
  </head>
  <body>
    <a class="skip" href="#main">Skip to content</a>
    <header class="top">
      <div class="wrap top-inner">
        <a class="brand" href="../" aria-label="BurzBlog home"><span class="mark" aria-hidden="true"></span>BurzBlog</a>
        <nav aria-label="Main">
          <a href="./"{{if .Index}} aria-current="page"{{end}}>Docs</a>
          <a href="https://github.com/weburz/burzblog">GitHub</a>
          <a class="nav-cta" href="../#install">Download</a>
        </nav>
      </div>
    </header>

    <div class="wrap docs">
      <nav class="docs-nav" aria-label="Documentation">
        {{- range .Nav}}
        <div>
          <p class="docs-nav-title">{{.Title}}</p>
          <ul>
            {{- range .Pages}}
            <li><a href="{{.Name}}.html"{{if eq .Name $.Current}} aria-current="page"{{end}}>{{.Title}}</a></li>
            {{- end}}
          </ul>
        </div>
        {{- end}}
      </nav>

      <main id="main" class="docs-main">
        {{- if .Index}}
        <h1>Documentation</h1>
        <p class="lede">{{.Summary}}</p>
        {{- range .Nav}}
        <h2>{{.Title}}</h2>
        <ul class="docs-cards">
          {{- range .Pages}}
          <li>
            <a href="{{.Name}}.html">{{.Title}}</a>
            <p>{{.Summary}}</p>
          </li>
          {{- end}}
        </ul>
        {{- end}}
        {{- else}}
        <article class="doc">
          <h1>{{.Page.Title}}</h1>
          {{- if gt (len .Page.Headings) 2}}
          <nav class="doc-toc" aria-label="On this page">
            <p>On this page</p>
            <ul>
              {{- range .Page.Headings}}
              <li><a href="#{{.ID}}">{{.Text}}</a></li>
              {{- end}}
            </ul>
          </nav>
          {{- end}}
{{.Page.Body}}
          <p class="doc-source"><a href="{{.Source}}">Edit this page on GitHub</a></p>
        </article>
        {{- end}}
      </main>
    </div>

    <footer class="foot">
      <div class="wrap foot-inner">
        <p><span class="mark" aria-hidden="true"></span>BurzBlog is made by <a href="https://weburz.com">Weburz</a> and released under the Apache-2.0 license.</p>
        <nav aria-label="Footer">
          <a href="https://github.com/weburz/burzblog">Source</a>
          <a href="https://github.com/weburz/burzblog/blob/main/CHANGELOG.md">Changelog</a>
          <a href="security.html">Security</a>
          <a href="https://github.com/weburz/burzblog/issues">Issues</a>
        </nav>
      </div>
    </footer>
  </body>
</html>
`))
