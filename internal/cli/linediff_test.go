package cli

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// apply rebuilds both texts from an edit script; it must give back the inputs.
func apply(script []diffLine) (old, new string) {
	var o, n strings.Builder
	for _, l := range script {
		if l.Op != '+' {
			o.WriteString(l.Text)
		}
		if l.Op != '-' {
			n.WriteString(l.Text)
		}
	}
	return o.String(), n.String()
}

func unified(old, new string) string {
	return renderUnified("a", "b", diffText(old, new, 3), nil, nil)
}

func TestDiffTextCases(t *testing.T) {
	cases := []struct {
		name, old, new, want string
	}{
		{"identical", "a\nb\n", "a\nb\n", ""},
		{"both empty", "", "", ""},
		{"empty to content", "", "a\nb\n", "--- a\n+++ b\n@@ -0,0 +1,2 @@\n+a\n+b\n"},
		{"content to empty", "a\n", "", "--- a\n+++ b\n@@ -1 +0,0 @@\n-a\n"},
		{"change in the middle", "a\nb\nc\n", "a\nX\nc\n", "--- a\n+++ b\n@@ -1,3 +1,3 @@\n a\n-b\n+X\n c\n"},
		{"final newline added", "a\nb", "a\nb\n", "--- a\n+++ b\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+b\n"},
		{"final newline removed", "a\nb\n", "a\nb", "--- a\n+++ b\n@@ -1,2 +1,2 @@\n a\n-b\n+b\n\\ No newline at end of file\n"},
		{"crlf against lf", "a\r\nb\r\n", "a\nb\n", "--- a\n+++ b\n@@ -1,2 +1,2 @@\n-a\r\n-b\r\n+a\n+b\n"},
		{"only a blank line added", "a\n", "a\n\n", "--- a\n+++ b\n@@ -1 +1,2 @@\n a\n+\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := unified(c.old, c.new); got != c.want {
				t.Errorf("diff =\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}

func numbered(n int) string {
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	return sb.String()
}

func TestHunkContextAndMerging(t *testing.T) {
	base := numbered(30)
	// Two edits far apart give two hunks, each with 3 lines of context.
	far := strings.Replace(strings.Replace(base, "line 5\n", "five\n", 1), "line 25\n", "twenty-five\n", 1)
	hs := diffText(base, far, 3)
	if len(hs) != 2 {
		t.Fatalf("far edits gave %d hunks, want 2", len(hs))
	}
	if h := hs[0]; h.OldStart != 1 || h.OldLines != 7 || h.NewStart != 1 || h.NewLines != 7 {
		t.Errorf("first hunk = %+v", h)
	}
	// Edits 6 lines apart (twice the context) share one hunk; 7 apart do not.
	near := strings.Replace(strings.Replace(base, "line 10\n", "x\n", 1), "line 17\n", "y\n", 1)
	if got := len(diffText(base, near, 3)); got != 1 {
		t.Errorf("edits 6 lines apart gave %d hunks, want 1", got)
	}
	apart := strings.Replace(strings.Replace(base, "line 10\n", "x\n", 1), "line 18\n", "y\n", 1)
	if got := len(diffText(base, apart, 3)); got != 2 {
		t.Errorf("edits 7 lines apart gave %d hunks, want 2", got)
	}
	// Context 0 keeps only the changes.
	hs = diffText(base, far, 0)
	if len(hs) != 2 || len(hs[0].Lines) != 2 {
		t.Errorf("context 0 = %+v", hs)
	}
}

func TestDiffRoundTripsRandomTexts(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	words := []string{"a\n", "b\n", "c\n", "d\n", "\n", "e", "a\r\n"}
	for i := 0; i < 500; i++ {
		var o, n strings.Builder
		for j := rng.Intn(12); j > 0; j-- {
			o.WriteString(words[rng.Intn(len(words))])
		}
		for j := rng.Intn(12); j > 0; j-- {
			n.WriteString(words[rng.Intn(len(words))])
		}
		script := diffLines(splitLines(o.String()), splitLines(n.String()))
		if gotO, gotN := apply(script); gotO != o.String() || gotN != n.String() {
			t.Fatalf("script does not rebuild %q -> %q: got %q -> %q", o.String(), n.String(), gotO, gotN)
		}
		if o.String() == n.String() && len(diffText(o.String(), n.String(), 3)) != 0 {
			t.Fatalf("identical text %q has hunks", o.String())
		}
	}
}

func TestDiffIsMinimal(t *testing.T) {
	// One line changed in 500 must be one deletion and one insertion.
	base := numbered(500)
	edited := strings.Replace(base, "line 250\n", "changed\n", 1)
	changes := 0
	for _, l := range diffLines(splitLines(base), splitLines(edited)) {
		if l.Op != ' ' {
			changes++
		}
	}
	if changes != 2 {
		t.Errorf("changes = %d, want 2", changes)
	}
}

func TestLargeDiffsAreBounded(t *testing.T) {
	cases := map[string][2]string{
		// Nothing in common: far beyond the edit-script limit.
		"disjoint": {strings.Repeat("a\n", 100000), strings.Repeat("b\n", 100000)},
		// Many small scattered edits.
		"scattered": {numbered(200000), strings.ReplaceAll(numbered(200000), "5 ", "five ")},
		// One huge line against many.
		"one long line": {strings.Repeat("x", 1<<20), numbered(50000)},
		"shuffled":      {numbered(20000), reversed(20000)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			script := diffLines(splitLines(c[0]), splitLines(c[1]))
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("took %v", elapsed)
			}
			if o, n := apply(script); o != c[0] || n != c[1] {
				t.Error("script does not rebuild its inputs")
			}
		})
	}
}

func reversed(n int) string {
	var sb strings.Builder
	for i := n; i >= 1; i-- {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	return sb.String()
}

func TestRenderUnifiedDisplayAndPaint(t *testing.T) {
	hs := diffText("a\n", "b\n", 3)
	got := renderUnified("old", "new", hs, strings.ToUpper, func(kind byte, s string) string { return "<" + string(kind) + ">" + s })
	want := "<h>--- old\n<h>+++ new\n<@>@@ -1 +1 @@\n<->-A\n<+>+B\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
