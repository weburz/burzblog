package render

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPreviewLineMarkers(t *testing.T) {
	src := "# Title\n\nFirst paragraph\nsecond line.\n\n- a\n- b\n\n```go\nx := 1\n```\n\n> quote\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n    indented\n\nlast"
	got, err := Preview(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		`<h1 id="title" data-line="1">`,
		`<p data-line="3">`,
		`<ul data-line="6">`,
		`<pre data-line="9"><code class="language-go">x := 1`,
		`<blockquote data-line="13">`,
		`<table data-line="15">`,
		`<pre data-line="19"><code>indented`,
		`<p data-line="21">last`,
	} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in %s", w, got)
		}
	}
}

func TestPreviewKeepsSanitizing(t *testing.T) {
	got, err := Preview(context.Background(), "<script>alert(1)</script>\n\n<p data-line=\"7\" onclick=x>hi</p>\n\n[x](javascript:alert(1))")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"<script", "onclick", "javascript:", `data-line="7"`} {
		if strings.Contains(got, d) {
			t.Errorf("unexpected %q in %s", d, got)
		}
	}
}

func TestPublishedHTMLHasNoLineMarkers(t *testing.T) {
	src := "# Title\n\ntext\n\n```go\nx\n```\n\n- a\n\n<p data-line=\"3\">raw</p>"
	got, err := HTML(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "data-line") {
		t.Errorf("published HTML carries data-line: %s", got)
	}
}

func TestPreviewLimitsStillApply(t *testing.T) {
	_, err := Preview(context.Background(), strings.Repeat("x", MaxInput+1))
	var limit *LimitError
	if !errors.As(err, &limit) {
		t.Errorf("oversized input: err = %v, want *LimitError", err)
	}
}
