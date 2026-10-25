package render

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHTML(t *testing.T) {
	cases := []struct {
		name, in   string
		want, deny []string
	}{
		{"heading ids", "## Hello World", []string{`<h2 id="hello-world">Hello World</h2>`}, nil},
		{"table", "| a | b |\n|:-|-:|\n| 1 | 2 |", []string{"<table>", "<td"}, nil},
		{"task list", "- [x] done", []string{`type="checkbox"`, "checked"}, nil},
		{"code class", "```go\nx := 1\n```", []string{`class="language-go"`}, nil},
		{"raw html dropped", "<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>", nil, []string{"<script", "onerror"}},
		{"javascript link", "[x](javascript:alert(1))", nil, []string{"javascript:"}},
		{"external link", "[x](https://example.com)", []string{`nofollow`, `noopener`, `target="_blank"`}, nil},
		{"image", "![cat](https://example.com/c.png)", []string{`<img src="https://example.com/c.png" alt="cat"`}, nil},
		{"footnote", "a[^1]\n\n[^1]: note", []string{`class="footnote-ref"`, `id="fn:1"`}, nil},
	}
	for _, c := range cases {
		got, err := HTML(context.Background(), c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in %s", c.name, w, got)
			}
		}
		for _, d := range c.deny {
			if strings.Contains(got, d) {
				t.Errorf("%s: unexpected %q in %s", c.name, d, got)
			}
		}
	}
	_, err := HTML(context.Background(), strings.Repeat("x", MaxInput+1))
	var limit *LimitError
	if !errors.As(err, &limit) {
		t.Errorf("oversized input: err = %v, want *LimitError", err)
	}
}
