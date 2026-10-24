package post

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Frontmatter keys BurzBlog models. Everything else is kept in Post.Extra.
var reserved = map[string]bool{
	"title": true, "description": true, "status": true,
	"timestamps": true, "coverImage": true, "slug": true,
}

const fence = "---"

// Encode renders p as a Markdown document with YAML frontmatter, in the shape
// Nuxt Content sites such as jarmos.dev read. The slug is not included: it is
// the file name.
func Encode(p *Post) string {
	root := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key string, value *yaml.Node) {
		root.Content = append(root.Content, scalar(key), value)
	}
	add("title", scalar(p.Title))
	if p.Description != "" {
		add("description", scalar(p.Description))
	}
	status := p.Status
	if status == Archived {
		status = Draft
	}
	add("status", scalar(string(status)))
	if p.PublishedOn != nil || p.UpdatedOn != nil {
		ts := &yaml.Node{Kind: yaml.MappingNode}
		if p.PublishedOn != nil {
			ts.Content = append(ts.Content, scalar("publishedOn"), timestamp(*p.PublishedOn))
		}
		if p.UpdatedOn != nil {
			ts.Content = append(ts.Content, scalar("updatedOn"), timestamp(*p.UpdatedOn))
		}
		add("timestamps", ts)
	}
	if p.CoverURL != "" {
		cover := &yaml.Node{Kind: yaml.MappingNode}
		cover.Content = append(cover.Content, scalar("url"), scalar(p.CoverURL))
		if p.CoverAlt != "" {
			cover.Content = append(cover.Content, scalar("alt"), scalar(p.CoverAlt))
		}
		add("coverImage", cover)
	}
	if extra, err := parseExtra(p.Extra); err == nil && extra != nil {
		root.Content = append(root.Content, extra.Content...)
	}

	var out bytes.Buffer
	out.WriteString(fence + "\n")
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	// Encoding a node tree we built ourselves cannot fail.
	_ = enc.Encode(root)
	_ = enc.Close()
	out.WriteString(fence + "\n")
	// Always leave a blank line after the frontmatter, so an empty post
	// opens in an editor with a place to start writing.
	out.WriteString("\n")
	if p.Body != "" {
		out.WriteString(p.Body)
		if !strings.HasSuffix(p.Body, "\n") {
			out.WriteString("\n")
		}
	}
	return out.String()
}

func scalar(value string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	// Quote values a YAML reader would otherwise turn into another type.
	var probe any
	if yaml.Unmarshal([]byte(value), &probe) != nil {
		n.Style = yaml.DoubleQuotedStyle
	} else if _, ok := probe.(string); !ok || probe.(string) != value {
		n.Style = yaml.DoubleQuotedStyle
	}
	if isYAML11Literal(value) {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// Older YAML 1.1 readers turn these plain words into booleans.
func isYAML11Literal(value string) bool {
	switch strings.ToLower(value) {
	case "y", "n", "yes", "no", "on", "off", "true", "false", "null", "~":
		return true
	}
	return false
}

func timestamp(t time.Time) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: t.Format(time.RFC3339)}
}

// Document is what Decode reads from a frontmatter document.
type Document struct {
	Title       string
	Description string
	Status      Status
	CoverURL    string
	CoverAlt    string
	PublishedOn *time.Time
	UpdatedOn   *time.Time
	Extra       string
	Body        string
}

// Apply copies the document's fields onto p.
func (d *Document) Apply(p *Post) {
	p.Title = d.Title
	p.Description = d.Description
	p.Status = d.Status
	p.CoverURL = d.CoverURL
	p.CoverAlt = d.CoverAlt
	p.PublishedOn = d.PublishedOn
	p.UpdatedOn = d.UpdatedOn
	p.Extra = d.Extra
	p.Body = d.Body
}

// Decode parses a Markdown document with YAML frontmatter. Errors are
// *FieldError values that point at the problem.
func Decode(doc string) (*Document, error) {
	doc = strings.TrimPrefix(strings.ReplaceAll(doc, "\r\n", "\n"), "\ufeff")
	if len(doc) > MaxBody+MaxExtra {
		return nil, &FieldError{"document", "is too large."}
	}
	if !strings.HasPrefix(doc, fence+"\n") {
		return nil, &FieldError{"frontmatter", "is missing: the file must start with a line containing only ---."}
	}
	rest := doc[len(fence)+1:]
	var head, body string
	switch {
	case strings.HasPrefix(rest, fence+"\n") || rest == fence:
		head, body = "", strings.TrimPrefix(strings.TrimPrefix(rest, fence), "\n")
	default:
		end := strings.Index(rest, "\n"+fence+"\n")
		if end < 0 {
			if strings.HasSuffix(rest, "\n"+fence) {
				end = len(rest) - len(fence) - 1
			} else {
				return nil, &FieldError{"frontmatter", "is not closed: add a line containing only --- after it."}
			}
		}
		head = rest[:end]
		body = strings.TrimPrefix(rest[end+1:], fence)
		body = strings.TrimPrefix(body, "\n")
	}
	if len(head) > MaxExtra {
		return nil, &FieldError{"frontmatter", fmt.Sprintf("must be at most %d KiB.", MaxExtra>>10)}
	}
	// One blank line separates frontmatter from the body; it is not content.
	body = strings.TrimPrefix(body, "\n")

	d := &Document{Status: Draft, Body: body}
	if strings.TrimSpace(head) == "" {
		return nil, &FieldError{"title", "must not be empty."}
	}
	root, err := parseMapping(head, "frontmatter")
	if err != nil {
		return nil, err
	}
	extra := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		var err error
		switch key.Value {
		case "title":
			d.Title, err = text(key.Value, value)
		case "description":
			d.Description, err = text(key.Value, value)
		case "status":
			var s string
			if s, err = text(key.Value, value); err == nil {
				d.Status = Status(s)
				if d.Status != Draft && d.Status != Published {
					err = &FieldError{"status", fmt.Sprintf("(line %d) must be draft or published.", value.Line)}
				}
			}
		case "slug":
			err = &FieldError{"slug", fmt.Sprintf("(line %d) comes from the file name; remove it from the frontmatter.", key.Line)}
		case "timestamps":
			err = d.readTimestamps(value)
		case "coverImage":
			err = d.readCover(value)
		default:
			extra.Content = append(extra.Content, key, value)
		}
		if err != nil {
			return nil, err
		}
	}
	if len(extra.Content) > 0 {
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(extra); err != nil {
			return nil, &FieldError{"frontmatter", "has values BurzBlog cannot keep."}
		}
		_ = enc.Close()
		d.Extra = buf.String()
	}
	return d, nil
}

func (d *Document) readTimestamps(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return &FieldError{"timestamps", fmt.Sprintf("(line %d) must be a mapping with publishedOn and updatedOn.", n.Line)}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i], n.Content[i+1]
		t, err := parseTime(key.Value, value)
		if err != nil {
			return err
		}
		switch key.Value {
		case "publishedOn":
			d.PublishedOn = t
		case "updatedOn":
			d.UpdatedOn = t
		default:
			return &FieldError{"timestamps", fmt.Sprintf("(line %d) only supports publishedOn and updatedOn.", key.Line)}
		}
	}
	return nil
}

func (d *Document) readCover(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return &FieldError{"coverImage", fmt.Sprintf("(line %d) must be a mapping with url and alt.", n.Line)}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i], n.Content[i+1]
		v, err := text("coverImage."+key.Value, value)
		if err != nil {
			return err
		}
		switch key.Value {
		case "url":
			d.CoverURL = v
		case "alt":
			d.CoverAlt = v
		default:
			return &FieldError{"coverImage", fmt.Sprintf("(line %d) only supports url and alt.", key.Line)}
		}
	}
	return nil
}

func text(field string, n *yaml.Node) (string, error) {
	if n.Kind != yaml.ScalarNode {
		return "", &FieldError{field, fmt.Sprintf("(line %d) must be text.", n.Line)}
	}
	if n.Tag == "!!null" {
		return "", nil
	}
	return n.Value, nil
}

func parseTime(field string, n *yaml.Node) (*time.Time, error) {
	v, err := text(field, n)
	if err != nil || v == "" {
		return nil, err
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", time.DateOnly} {
		if t, err := time.Parse(layout, v); err == nil {
			return &t, nil
		}
	}
	return nil, &FieldError{field, fmt.Sprintf("(line %d) must be a date like 2026-10-24 or 2026-10-24T09:30:00+00:00.", n.Line)}
}

// parseMapping reads YAML that must be one mapping without anchors, aliases
// or custom tags, which keeps hostile input from expanding.
func parseMapping(src, field string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return nil, &FieldError{field, "is not valid YAML: " + strings.TrimPrefix(err.Error(), "yaml: ")}
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, &FieldError{field, "must be key: value pairs."}
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k.Kind != yaml.ScalarNode {
			return nil, &FieldError{field, fmt.Sprintf("(line %d) keys must be plain text.", k.Line)}
		}
		if seen[k.Value] {
			return nil, &FieldError{field, fmt.Sprintf("(line %d) repeats the key %q.", k.Line, k.Value)}
		}
		seen[k.Value] = true
	}
	if err := plain(root, field); err != nil {
		return nil, err
	}
	return root, nil
}

func plain(n *yaml.Node, field string) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return &FieldError{field, fmt.Sprintf("(line %d) uses YAML anchors or aliases, which BurzBlog does not accept.", n.Line)}
	}
	if n.Tag != "" && !strings.HasPrefix(n.Tag, "!!") {
		return &FieldError{field, fmt.Sprintf("(line %d) uses a custom YAML tag, which BurzBlog does not accept.", n.Line)}
	}
	for _, c := range n.Content {
		if err := plain(c, field); err != nil {
			return err
		}
	}
	return nil
}

// parseExtra validates the extra frontmatter: a mapping without keys
// BurzBlog models itself.
func parseExtra(src string) (*yaml.Node, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	root, err := parseMapping(src, "extra frontmatter")
	if err != nil || root == nil {
		return root, err
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if k := root.Content[i]; reserved[k.Value] {
			return nil, &FieldError{"extra frontmatter", fmt.Sprintf("(line %d) must not set %q; use the field for it.", k.Line, k.Value)}
		}
	}
	return root, nil
}
