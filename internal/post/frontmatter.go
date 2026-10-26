package post

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Frontmatter keys BurzBlog models. Everything else is kept in Post.Extra.
// "seo" is modelled too, but only its title and description: other keys under
// it stay in Extra, as a seo mapping without those two.
var reserved = map[string]bool{
	"title": true, "description": true, "status": true,
	"timestamps": true, "coverImage": true, "slug": true,
	"tags": true, "author": true, "canonical": true,
}

const fence = "---"

// Encode renders p as a Markdown document with YAML frontmatter, in the shape
// Nuxt Content sites read. The slug is not included: it is the file name.
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
	if len(p.Tags) > 0 {
		tags := &yaml.Node{Kind: yaml.SequenceNode}
		for _, tag := range p.Tags {
			tags.Content = append(tags.Content, scalar(tag))
		}
		add("tags", tags)
	}
	if p.Author != "" {
		add("author", scalar(p.Author))
	}
	extra, err := parseExtra(p.Extra)
	if err != nil {
		extra = nil
	}
	// Keys under seo that BurzBlog does not model are written back inside it.
	seo := &yaml.Node{Kind: yaml.MappingNode}
	if p.SEOTitle != "" {
		seo.Content = append(seo.Content, scalar("title"), scalar(p.SEOTitle))
	}
	if p.SEODescription != "" {
		seo.Content = append(seo.Content, scalar("description"), scalar(p.SEODescription))
	}
	if extra != nil {
		for i := 0; i+1 < len(extra.Content); i += 2 {
			if extra.Content[i].Value == "seo" {
				seo.Content = append(seo.Content, extra.Content[i+1].Content...)
			}
		}
	}
	if len(seo.Content) > 0 {
		add("seo", seo)
	}
	if p.CanonicalURL != "" {
		add("canonical", scalar(p.CanonicalURL))
	}
	if extra != nil {
		for i := 0; i+1 < len(extra.Content); i += 2 {
			if extra.Content[i].Value != "seo" {
				root.Content = append(root.Content, extra.Content[i], extra.Content[i+1])
			}
		}
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
	// Slug is set only when the frontmatter names one; the file name or URL
	// decides the post, so callers check that the two agree.
	Slug        string
	Title       string
	Description string
	Status      Status
	// HasStatus is false when the frontmatter has no status key, so a save
	// can keep the post's current status instead of assuming draft.
	HasStatus   bool
	CoverURL    string
	CoverAlt    string
	PublishedOn *time.Time
	UpdatedOn   *time.Time
	Tags        []string
	Author      string
	// SEOTitle and SEODescription come from the seo mapping; its other keys
	// stay in Extra.
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	Extra          string
	Body           string
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
	p.Tags, p.Author = d.Tags, d.Author
	p.SEOTitle, p.SEODescription, p.CanonicalURL = d.SEOTitle, d.SEODescription, d.CanonicalURL
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
	if root == nil {
		// Only comments: there is no title.
		return nil, &FieldError{"title", "must not be empty."}
	}
	if err := d.readMapping(root); err != nil {
		return nil, err
	}
	return d, nil
}

// readMapping reads the modelled keys of a frontmatter mapping into d and
// collects the rest as d.Extra.
func (d *Document) readMapping(root *yaml.Node) error {
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
			d.HasStatus = true
			if s, err = text(key.Value, value); err == nil {
				d.Status = Status(s)
				if d.Status != Draft && d.Status != Published {
					err = &FieldError{"status", fmt.Sprintf("(line %d) must be draft or published.", value.Line)}
				}
			}
		case "slug":
			d.Slug, err = text(key.Value, value)
		case "timestamps":
			err = d.readTimestamps(value)
		case "coverImage":
			err = d.readCover(value)
		case "tags":
			d.Tags, err = readTags(value)
		case "author":
			d.Author, err = text(key.Value, value)
		case "canonical":
			d.CanonicalURL, err = text(key.Value, value)
		case "seo":
			var rest *yaml.Node
			if rest, err = d.readSEO(value); err == nil && len(rest.Content) > 0 {
				extra.Content = append(extra.Content, key, rest)
			}
		default:
			extra.Content = append(extra.Content, key, value)
		}
		if err != nil {
			return err
		}
	}
	if len(extra.Content) > 0 {
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(extra); err != nil {
			return &FieldError{"frontmatter", "has values BurzBlog cannot keep."}
		}
		_ = enc.Close()
		d.Extra = buf.String()
	}
	return nil
}

// readTags reads a YAML list of tags, or one text of comma-separated tags.
func readTags(n *yaml.Node) ([]string, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return nil, nil
		}
		return strings.Split(n.Value, ","), nil
	case yaml.SequenceNode:
		var tags []string
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, &FieldError{"tags", fmt.Sprintf("(line %d) must be a list of words, such as [go, web].", item.Line)}
			}
			if item.Tag != "!!null" {
				tags = append(tags, item.Value)
			}
		}
		return tags, nil
	}
	return nil, &FieldError{"tags", fmt.Sprintf("(line %d) must be a list of words, such as [go, web].", n.Line)}
}

// readSEO reads seo.title and seo.description and returns a mapping of the
// other keys, which stay in the extra frontmatter.
func (d *Document) readSEO(n *yaml.Node) (*yaml.Node, error) {
	rest := &yaml.Node{Kind: yaml.MappingNode}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return rest, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, &FieldError{"seo", fmt.Sprintf("(line %d) must be a mapping with title and description.", n.Line)}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i], n.Content[i+1]
		switch key.Value {
		case "title", "description":
			v, err := text("seo."+key.Value, value)
			if err != nil {
				return nil, err
			}
			if key.Value == "title" {
				d.SEOTitle = v
			} else {
				d.SEODescription = v
			}
		default:
			rest.Content = append(rest.Content, key, value)
		}
	}
	return rest, nil
}

// MoveMetadataFromExtra parses the modelled metadata keys (tags, author,
// seo.title, seo.description, canonical) out of extra, for posts saved before
// BurzBlog modelled them. It returns the fields in a Post and the extra
// without them. ok is false when extra holds none of those keys or they
// cannot be used as they are (a tag list that is too long, say); the post is
// then left as it is.
func MoveMetadataFromExtra(extra string) (moved *Post, rest string, ok bool) {
	root, err := parseMapping(extra, "extra frontmatter")
	if err != nil || root == nil {
		return nil, extra, false
	}
	var d Document
	if d.readMapping(root) != nil {
		return nil, extra, false
	}
	found := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "tags", "author", "canonical", "seo":
			found = true
		}
	}
	if !found {
		return nil, extra, false
	}
	p := &Post{}
	d.Apply(p)
	Normalize(p)
	if validateMetadata(p) != nil {
		return nil, extra, false
	}
	return p, d.Extra, true
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

// parseMapping reads YAML that must be one mapping without anchors, aliases,
// merge keys or tags outside the core schema, which keeps hostile input from
// expanding and keeps exports readable by strict YAML readers.
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

// coreTags are the YAML core-schema tags every reader resolves the same way.
// yaml.v3 reports implicit scalars with their resolved tag, so plain values
// pass without being written out.
var coreTags = map[string]bool{
	"!!str": true, "!!int": true, "!!float": true, "!!bool": true,
	"!!null": true, "!!timestamp": true, "!!map": true, "!!seq": true,
	"!!binary": true,
}

func plain(n *yaml.Node, field string) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return &FieldError{field, fmt.Sprintf("(line %d) uses YAML anchors or aliases, which BurzBlog does not accept.", n.Line)}
	}
	if n.Tag == "!!merge" {
		return &FieldError{field, fmt.Sprintf("(line %d) uses a YAML merge key (<<), which BurzBlog does not accept; write the keys out.", n.Line)}
	}
	if n.Tag != "" && !coreTags[n.Tag] {
		return &FieldError{field, fmt.Sprintf("(line %d) uses the YAML tag %s, which BurzBlog does not accept; remove it or use a core tag such as !!str.", n.Line, n.Tag)}
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if k := n.Content[i]; k.Kind == yaml.ScalarNode && k.Value == "<<" {
				return &FieldError{field, fmt.Sprintf("(line %d) uses a YAML merge key (<<), which BurzBlog does not accept; write the keys out.", k.Line)}
			}
		}
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
		k, v := root.Content[i], root.Content[i+1]
		if reserved[k.Value] {
			return nil, &FieldError{"extra frontmatter", fmt.Sprintf("(line %d) must not set %q; use the field for it.", k.Line, k.Value)}
		}
		if k.Value != "seo" {
			continue
		}
		if v.Kind != yaml.MappingNode {
			return nil, &FieldError{"extra frontmatter", fmt.Sprintf("(line %d) seo must be a mapping.", k.Line)}
		}
		for j := 0; j+1 < len(v.Content); j += 2 {
			if sub := v.Content[j]; sub.Value == "title" || sub.Value == "description" {
				return nil, &FieldError{"extra frontmatter", fmt.Sprintf("(line %d) must not set seo.%s; use the SEO field for it.", sub.Line, sub.Value)}
			}
		}
	}
	return root, nil
}
