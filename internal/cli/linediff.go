package cli

import (
	"fmt"
	"strings"
)

// Limits that keep a diff of two large, very different documents cheap. When
// either is exceeded the changed middle section is shown as one deletion
// followed by one insertion: a correct diff, only not a minimal one.
const (
	maxEditScript = 1000     // most differing lines Myers' algorithm searches for
	maxDiffWork   = 20 << 20 // most line comparisons it may make
)

// diffLine is one line of a diff. Op is ' ' (in both), '-' (only in the old
// text) or '+' (only in the new text). Text keeps its line terminator, so a
// missing final newline or CRLF endings count as differences, as in diff(1).
type diffLine struct {
	Op   byte
	Text string
}

// hunk is a run of changes with its surrounding context, in the shape of a
// unified diff. Starts are zero-based line indexes.
type hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Lines              []diffLine
}

// splitLines splits s after each "\n". The last element has no terminator
// when s does not end in a newline; s == "" has no lines.
func splitLines(s string) []string {
	var lines []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			lines = append(lines, s)
			break
		}
		lines = append(lines, s[:i+1])
		s = s[i+1:]
	}
	return lines
}

// diffText returns the unified-diff hunks that turn old into new, with
// context unchanged lines around each change. Identical texts have no hunks.
func diffText(old, new string, context int) []hunk {
	return groupHunks(diffLines(splitLines(old), splitLines(new)), context)
}

// diffLines returns the full edit script from a to b, unchanged lines included.
func diffLines(a, b []string) []diffLine {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	midA, midB := a[prefix:len(a)-suffix], b[prefix:len(b)-suffix]

	out := make([]diffLine, 0, len(a)+len(b)-prefix-suffix)
	for _, l := range a[:prefix] {
		out = append(out, diffLine{' ', l})
	}
	if ops, ok := myers(midA, midB); ok {
		x, y := 0, 0
		for _, op := range ops {
			switch op {
			case ' ':
				out = append(out, diffLine{' ', midA[x]})
				x++
				y++
			case '-':
				out = append(out, diffLine{'-', midA[x]})
				x++
			case '+':
				out = append(out, diffLine{'+', midB[y]})
				y++
			}
		}
	} else {
		for _, l := range midA {
			out = append(out, diffLine{'-', l})
		}
		for _, l := range midB {
			out = append(out, diffLine{'+', l})
		}
	}
	for _, l := range a[len(a)-suffix:] {
		out = append(out, diffLine{' ', l})
	}
	return out
}

// myers finds a shortest edit script from a to b with Myers' O(ND) algorithm.
// It reports false when the script is longer than maxEditScript or the search
// takes more than maxDiffWork comparisons. Memory is O(D^2) in the script
// length D, so maxEditScript bounds it too.
func myers(a, b []string) (ops []byte, ok bool) {
	n, m := len(a), len(b)
	switch {
	case n == 0 && m == 0:
		return nil, true
	case n == 0:
		return []byte(strings.Repeat("+", m)), true
	case m == 0:
		return []byte(strings.Repeat("-", n)), true
	}
	maxD := min(n+m, maxEditScript)
	off := maxD + 1
	v := make([]int, 2*maxD+3)
	// trace[d] holds the furthest-reaching x per diagonal before round d,
	// for diagonals -d-1 to d+1.
	trace := make([][]int, 0, 64)
	work := 0
	for d := 0; d <= maxD; d++ {
		trace = append(trace, append([]int(nil), v[off-d-1:off+d+2]...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
				work++
			}
			v[off+k] = x
			if x >= n && y >= m {
				return backtrack(trace, n, m), true
			}
		}
		work += d + 1
		if work > maxDiffWork {
			return nil, false
		}
	}
	return nil, false
}

// backtrack walks the trace from (n, m) to the origin and returns the edit
// operations in order: ' ' keep, '-' delete from a, '+' insert from b.
func backtrack(trace [][]int, n, m int) []byte {
	var rev []byte
	x, y := n, m
	for d := len(trace) - 1; d > 0; d-- {
		prev := func(k int) int { return trace[d][k+d+1] }
		k := x - y
		var pk int
		if k == -d || (k != d && prev(k-1) < prev(k+1)) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := prev(pk)
		py := px - pk
		for x > px && y > py {
			rev = append(rev, ' ')
			x--
			y--
		}
		if x == px {
			rev = append(rev, '+')
		} else {
			rev = append(rev, '-')
		}
		x, y = px, py
	}
	for x > 0 && y > 0 {
		rev = append(rev, ' ')
		x--
		y--
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// groupHunks groups an edit script into unified-diff hunks. Changes that are
// no more than twice the context apart share one hunk.
func groupHunks(script []diffLine, context int) []hunk {
	// oldPos[i] and newPos[i] count the lines of each text before script[i].
	oldPos, newPos := make([]int, len(script)+1), make([]int, len(script)+1)
	for i, l := range script {
		oldPos[i+1], newPos[i+1] = oldPos[i], newPos[i]
		if l.Op != '+' {
			oldPos[i+1]++
		}
		if l.Op != '-' {
			newPos[i+1]++
		}
	}
	var out []hunk
	for i := 0; i < len(script); {
		if script[i].Op == ' ' {
			i++
			continue
		}
		start := max(i-context, 0)
		end := i + 1 // just past the last change in this hunk
		for j := end; j < len(script) && j-end <= 2*context; j++ {
			if script[j].Op != ' ' {
				end = j + 1
			}
		}
		stop := min(end+context, len(script))
		out = append(out, hunk{
			OldStart: oldPos[start], OldLines: oldPos[stop] - oldPos[start],
			NewStart: newPos[start], NewLines: newPos[stop] - newPos[start],
			Lines: script[start:stop],
		})
		i = stop
	}
	return out
}

// rangeText is a unified diff range: "start,count", with the count left out
// when it is 1. An empty range is given as the line before it.
func rangeText(start, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d,%d", start+1, count)
}

// renderUnified writes hunks as a unified diff between the named files, or ""
// when there are none. display turns a line's text, without its terminator,
// into what is printed; paint colours a finished output line by kind: '-',
// '+' and ' ' for lines, '@' for hunk headers and 'h' for file headers and
// the no-newline marker. Either may be nil.
func renderUnified(oldName, newName string, hs []hunk, display func(string) string, paint func(kind byte, s string) string) string {
	if len(hs) == 0 {
		return ""
	}
	if display == nil {
		display = func(s string) string { return s }
	}
	if paint == nil {
		paint = func(_ byte, s string) string { return s }
	}
	var sb strings.Builder
	sb.WriteString(paint('h', "--- "+oldName) + "\n")
	sb.WriteString(paint('h', "+++ "+newName) + "\n")
	for _, h := range hs {
		sb.WriteString(paint('@', fmt.Sprintf("@@ -%s +%s @@", rangeText(h.OldStart, h.OldLines), rangeText(h.NewStart, h.NewLines))) + "\n")
		for _, l := range h.Lines {
			text, terminated := strings.CutSuffix(l.Text, "\n")
			sb.WriteString(paint(l.Op, string(l.Op)+display(text)) + "\n")
			if !terminated {
				sb.WriteString(paint('h', `\ No newline at end of file`) + "\n")
			}
		}
	}
	return sb.String()
}
