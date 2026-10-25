package post

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	p := valid()
	p.Tags = []string{"  Go ", "Machine Learning", "go", "C++", "---", "", "web-dev", "Web  Dev!", "ÜBER"}
	Normalize(p)
	want := []string{"go", "machine-learning", "c", "web-dev", "über"}
	if !reflect.DeepEqual(p.Tags, want) {
		t.Errorf("Normalize tags = %q, want %q", p.Tags, want)
	}
	if err := Validate(p); err != nil {
		t.Errorf("Validate(normalized) = %v", err)
	}
}

func TestNormalizeTagsEmptyStaysNil(t *testing.T) {
	p := valid()
	p.Tags = []string{"", "  ", "!!"}
	Normalize(p)
	if p.Tags != nil {
		t.Errorf("Tags = %#v, want nil", p.Tags)
	}
}

func TestValidateMetadataRefusals(t *testing.T) {
	many := make([]string, MaxTags+1)
	for i := range many {
		many[i] = "t" + strings.Repeat("x", i)
	}
	cases := map[string]struct {
		mutate func(*Post)
		want   string
	}{
		"too many tags":      {func(p *Post) { p.Tags = many }, "at most 20"},
		"long tag":           {func(p *Post) { p.Tags = []string{strings.Repeat("a", MaxTag+1)} }, "at most 40"},
		"unnormalized tag":   {func(p *Post) { p.Tags = []string{"Go"} }, "fix"},
		"duplicate tag":      {func(p *Post) { p.Tags = []string{"go", "go"} }, "twice"},
		"author newline":     {func(p *Post) { p.Author = "a\nb" }, "single line"},
		"author escape":      {func(p *Post) { p.Author = "\x1b[2Jada" }, "control characters"},
		"long author":        {func(p *Post) { p.Author = strings.Repeat("a", MaxAuthor+1) }, "at most 100"},
		"seo title tab":      {func(p *Post) { p.SEOTitle = "a\tb" }, "control characters"},
		"long seo title":     {func(p *Post) { p.SEOTitle = strings.Repeat("a", MaxSEOTitle+1) }, "at most 120"},
		"seo desc newline":   {func(p *Post) { p.SEODescription = "a\r\nb" }, "single line"},
		"long seo desc":      {func(p *Post) { p.SEODescription = strings.Repeat("a", MaxSEODesc+1) }, "at most 320"},
		"http canonical":     {func(p *Post) { p.CanonicalURL = "http://example.com/a" }, "https"},
		"relative canonical": {func(p *Post) { p.CanonicalURL = "/a" }, "https"},
		"script canonical":   {func(p *Post) { p.CanonicalURL = "javascript:alert(1)" }, "https"},
		"hostless canonical": {func(p *Post) { p.CanonicalURL = "https:///a" }, "https"},
	}
	for name, c := range cases {
		p := valid()
		c.mutate(p)
		err := Validate(p)
		if err == nil || !IsFieldError(err) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Validate = %v, want a FieldError mentioning %q", name, err, c.want)
		}
	}
}

func TestValidateMetadataAccepts(t *testing.T) {
	p := valid()
	p.Tags = []string{"go", "web-dev", "2026"}
	p.Author = "Ada Lovelace"
	p.SEOTitle = "Hello | Blog"
	p.SEODescription = "A short summary."
	p.CanonicalURL = "https://example.com/blog/hello"
	if err := Validate(p); err != nil {
		t.Errorf("Validate = %v", err)
	}
}

func TestSameContentSeesMetadata(t *testing.T) {
	base := func() *Post {
		p := valid()
		p.Tags = []string{"a", "b"}
		return p
	}
	if !SameContent(base(), base()) {
		t.Fatal("equal posts differ")
	}
	for name, mutate := range map[string]func(*Post){
		"tag order":  func(p *Post) { p.Tags = []string{"b", "a"} },
		"tag added":  func(p *Post) { p.Tags = append(p.Tags, "c") },
		"author":     func(p *Post) { p.Author = "ada" },
		"seo title":  func(p *Post) { p.SEOTitle = "t" },
		"seo desc":   func(p *Post) { p.SEODescription = "d" },
		"canonical":  func(p *Post) { p.CanonicalURL = "https://example.com" },
		"tags empty": func(p *Post) { p.Tags = nil },
	} {
		b := base()
		mutate(b)
		if SameContent(base(), b) {
			t.Errorf("%s: SameContent = true, want false", name)
		}
	}
}

func TestMetadataFrontmatterRoundTrip(t *testing.T) {
	p := &Post{
		Slug: "m", Title: "Meta", Status: Draft,
		Tags: []string{"go", "web-dev"}, Author: "Ada",
		SEOTitle: "SEO: Meta", SEODescription: "yes", CanonicalURL: "https://example.com/m",
		Extra: "sitemap:\n  priority: 1\n",
	}
	doc := Encode(p)
	for _, want := range []string{"tags:\n  - go\n  - web-dev", "author: Ada", "seo:\n  title: 'SEO: Meta'",
		"description: \"yes\"", "canonical: https://example.com/m", "sitemap:"} {
		if !strings.Contains(doc, want) && !strings.Contains(doc, strings.ReplaceAll(want, "'", "\"")) {
			t.Errorf("Encode output missing %q:\n%s", want, doc)
		}
	}
	d, err := Decode(doc)
	if err != nil {
		t.Fatalf("Decode = %v\n%s", err, doc)
	}
	back := &Post{Slug: "m"}
	d.Apply(back)
	if !SameContent(p, back) {
		t.Errorf("round trip changed the post:\n got %+v\nwant %+v", back, p)
	}
}

func TestMetadataOmittedWhenEmpty(t *testing.T) {
	doc := Encode(&Post{Slug: "m", Title: "Meta", Status: Draft})
	for _, key := range []string{"tags", "author", "seo", "canonical"} {
		if strings.Contains(doc, key+":") {
			t.Errorf("Encode wrote empty %s:\n%s", key, doc)
		}
	}
}

func TestDecodeMetadataKeys(t *testing.T) {
	doc := "---\ntitle: x\ntags: [Go, 2026, ~]\nauthor: Ada\ncanonical: https://example.com/x\nseo:\n  title: T\n  description: D\n---\n"
	d, err := Decode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Tags, []string{"Go", "2026"}) || d.Author != "Ada" ||
		d.SEOTitle != "T" || d.SEODescription != "D" || d.CanonicalURL != "https://example.com/x" {
		t.Errorf("document = %+v", d)
	}
	if d.Extra != "" {
		t.Errorf("modelled keys leaked into extra: %q", d.Extra)
	}
}

func TestDecodeTagsAsText(t *testing.T) {
	d, err := Decode("---\ntitle: x\ntags: go, Web Dev\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	p := &Post{}
	d.Apply(p)
	Normalize(p)
	if want := []string{"go", "web-dev"}; !reflect.DeepEqual(p.Tags, want) {
		t.Errorf("tags = %q, want %q", p.Tags, want)
	}
}

func TestDecodeMetadataErrors(t *testing.T) {
	cases := map[string]string{
		"nested tags":      "tags:\n  - [a, b]\n",
		"map tags":         "tags: {a: 1}\n",
		"list author":      "author: [a]\n",
		"seo scalar":       "seo: hello\n",
		"seo title list":   "seo:\n  title: [a]\n",
		"canonical in map": "canonical: {a: 1}\n",
	}
	for name, head := range cases {
		_, err := Decode("---\ntitle: x\n" + head + "---\n")
		if err == nil || !IsFieldError(err) {
			t.Errorf("%s: Decode = %v, want a FieldError", name, err)
		}
	}
}

func TestSEOUnmodelledKeysRoundTrip(t *testing.T) {
	doc := "---\ntitle: x\nseo:\n  title: T\n  robots: noindex\n  og:\n    image: https://example.com/i.png\n---\n"
	d, err := Decode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if d.SEOTitle != "T" {
		t.Errorf("SEOTitle = %q", d.SEOTitle)
	}
	if !strings.Contains(d.Extra, "seo:") || !strings.Contains(d.Extra, "robots: noindex") ||
		!strings.Contains(d.Extra, "image: https://example.com/i.png") || strings.Contains(d.Extra, "title") {
		t.Errorf("extra = %q, want seo with robots and og only", d.Extra)
	}
	p := &Post{Slug: "x", Status: Draft}
	d.Apply(p)
	if err := Validate(p); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	out := Encode(p)
	if strings.Count(out, "seo:") != 1 {
		t.Errorf("Encode wrote seo more than once:\n%s", out)
	}
	again, err := Decode(out)
	if err != nil {
		t.Fatalf("Decode(Encode) = %v\n%s", err, out)
	}
	if again.SEOTitle != "T" || again.Extra != d.Extra {
		t.Errorf("round trip changed seo: title %q extra %q, want %q", again.SEOTitle, again.Extra, d.Extra)
	}
}

func TestSEOOnlyUnmodelledKeysStayExtra(t *testing.T) {
	d, err := Decode("---\ntitle: x\nseo:\n  robots: noindex\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	if d.SEOTitle != "" || d.SEODescription != "" || d.Extra != "seo:\n  robots: noindex\n" {
		t.Errorf("document = %+v", d)
	}
	p := &Post{Slug: "x", Status: Draft}
	d.Apply(p)
	out := Encode(p)
	if !strings.Contains(out, "seo:\n  robots: noindex") {
		t.Errorf("Encode lost seo.robots:\n%s", out)
	}
}

func TestExtraRefusesModelledSEOKeys(t *testing.T) {
	for _, extra := range []string{
		"tags: [a]\n", "author: x\n", "canonical: https://x.test\n",
		"seo:\n  title: t\n", "seo:\n  description: d\n", "seo: hello\n",
	} {
		if _, err := parseExtra(extra); err == nil || !IsFieldError(err) {
			t.Errorf("parseExtra(%q) = %v, want a FieldError", extra, err)
		}
	}
	if _, err := parseExtra("seo:\n  robots: noindex\n"); err != nil {
		t.Errorf("parseExtra(seo.robots) = %v, want success", err)
	}
}

func TestMoveMetadataFromExtra(t *testing.T) {
	extra := "sitemap:\n  priority: 1\ntags:\n  - Go\n  - Web Dev\nauthor: Ada\ncanonical: https://example.com/x\n" +
		"seo:\n  title: T\n  description: D\n  robots: noindex\n"
	moved, rest, ok := MoveMetadataFromExtra(extra)
	if !ok {
		t.Fatal("MoveMetadataFromExtra not ok")
	}
	if !reflect.DeepEqual(moved.Tags, []string{"go", "web-dev"}) || moved.Author != "Ada" ||
		moved.SEOTitle != "T" || moved.SEODescription != "D" || moved.CanonicalURL != "https://example.com/x" {
		t.Errorf("moved = %+v", moved)
	}
	if _, err := parseExtra(rest); err != nil {
		t.Errorf("remaining extra %q is not valid: %v", rest, err)
	}
	for _, want := range []string{"sitemap:", "priority: 1", "robots: noindex"} {
		if !strings.Contains(rest, want) {
			t.Errorf("remaining extra %q lost %q", rest, want)
		}
	}
	for _, gone := range []string{"tags", "author", "canonical", "title", "description"} {
		if strings.Contains(rest, gone) {
			t.Errorf("remaining extra %q still has %q", rest, gone)
		}
	}
}

func TestMoveMetadataFromExtraLeavesOtherPostsAlone(t *testing.T) {
	many := "tags: ["
	for i := 0; i < MaxTags+1; i++ {
		many += string(rune('a'+i)) + ", "
	}
	many += "z]\n"
	cases := map[string]string{
		"no modelled keys": "sitemap:\n  priority: 1\n",
		"empty":            "",
		"too many tags":    many,
		"bad canonical":    "canonical: http://example.com\n",
		"newline author":   "author: \"a\\nb\"\n",
		"tags map":         "tags: {a: 1}\n",
		"seo scalar":       "seo: hello\n",
		"not yaml":         "a: [\n",
	}
	for name, extra := range cases {
		moved, rest, ok := MoveMetadataFromExtra(extra)
		if ok || moved != nil || rest != extra {
			t.Errorf("%s: got ok=%v moved=%v rest=%q, want the extra untouched", name, ok, moved, rest)
		}
	}
}
