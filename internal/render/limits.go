package render

import (
	"fmt"
	"strings"
)

// Limits on constructs whose rendering cost grows much faster than their
// size. They are far above anything a real post uses; the checks run before
// the parser sees the text and cost one pass over it.
const (
	// MaxTableColumns is the widest table a preview renders. Rows are padded
	// to the header's width, so output grows as rows times columns.
	MaxTableColumns = 64
	// MaxTableCells is the most table cells (rows times columns, padding
	// included) in one post.
	MaxTableCells = 50000
	// MaxNesting is how many block quotes and list items one line may open.
	MaxNesting = 32
	// MaxDeepLevels is how many levels beyond shallowNesting all lines of a
	// post may add up to. Every level repeats its opening and closing tags
	// for each line, so many deep lines make output grow several times
	// faster than input.
	MaxDeepLevels  = 50000
	shallowNesting = 4
	// MaxLines is how many non-blank lines outside code blocks a post may
	// have. Each one becomes several parser nodes, whatever its length.
	MaxLines = 50000
	// MaxHeadings is how many headings a post may have. Each heading id is
	// made unique by trying suffixes until one is free, so repeated titles
	// cost quadratic time.
	MaxHeadings = 1000
	// MaxMarkers is how many emphasis, strikethrough, code span and link openers one
	// paragraph may hold; unmatched ones are re-scanned by the inline parser.
	MaxMarkers = 4096
	// MaxMarkerWork bounds the sum over a post's paragraphs of the squared
	// marker count, which is how the inline parser's cost grows. One
	// paragraph at MaxMarkers uses about half of it.
	MaxMarkerWork = 1 << 25
)

// LimitError says why a post cannot be previewed. Message is written for the
// author and names the line to fix.
type LimitError struct{ Message string }

func (e *LimitError) Error() string { return e.Message }

// checkConstructs rejects text built to make the Markdown parser do
// quadratic work or emit far more output than input.
func checkConstructs(source string) error {
	var (
		fence       string // fence marker while inside a fenced code block
		markers     int    // openers in the current paragraph
		markersFrom int    // line the current paragraph started on
		tableCols   int    // columns of the table being read
		cells       int    // table cells so far, padding included
		deep        int    // levels past shallowNesting so far
		work        int    // squared marker counts so far
		lines       int    // non-blank lines outside code blocks so far
		headings    int    // headings so far
		prevBlank   = true
	)
	for n, rest := 1, source; rest != ""; n++ {
		line := rest
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line, rest = rest[:i], rest[i+1:]
		} else {
			rest = ""
		}
		body, depth, item := stripContainers(line)
		if depth > MaxNesting {
			return &LimitError{fmt.Sprintf("Line %d nests block quotes or list items %d levels deep; previews support at most %d. Flatten that line.", n, depth, MaxNesting)}
		}
		if depth > shallowNesting {
			deep += depth - shallowNesting
			if deep > MaxDeepLevels {
				return &LimitError{fmt.Sprintf("Line %d is where the post passes %d levels of deeply nested block quotes and list items in total; previews support no more. Flatten the nesting.", n, MaxDeepLevels)}
			}
		}
		trimmed := strings.TrimSpace(body)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if f := fenceOpener(trimmed); f != "" {
			fence, markers = f, 0
			continue
		}
		if trimmed == "" {
			markers, tableCols, prevBlank = 0, 0, true
			continue
		}
		if isHeading(trimmed, prevBlank) {
			if headings++; headings > MaxHeadings {
				return &LimitError{fmt.Sprintf("Line %d is where the post passes %d headings; previews support no more.", n, MaxHeadings)}
			}
		}
		prevBlank = false
		if lines++; lines > MaxLines {
			return &LimitError{fmt.Sprintf("Line %d is where the post passes %d lines outside code blocks; previews support no more.", n, MaxLines)}
		}
		if tableCols > 0 {
			cells += tableCols
			if cells > MaxTableCells {
				return &LimitError{fmt.Sprintf("Line %d is where the post passes %d table cells in total; previews support no more. Shorten or remove a table.", n, MaxTableCells)}
			}
		} else if cols := delimiterColumns(trimmed); cols > 0 {
			if cols > MaxTableColumns {
				return &LimitError{fmt.Sprintf("The table on line %d has %d columns; previews support at most %d. Split the table or turn it into a list.", n, cols, MaxTableColumns)}
			}
			tableCols = cols
			cells += 2 * cols
		}
		// Table cells and list items are parsed one at a time; only the
		// lines of one paragraph add up.
		if item || tableCols > 0 || strings.HasPrefix(trimmed, "#") {
			markers = 0
		}
		if markers == 0 {
			markersFrom = n
		}
		added := strings.Count(body, "*") + strings.Count(body, "_") + strings.Count(body, "~") + strings.Count(body, "[") + strings.Count(body, "`")
		work += added * (2*markers + added)
		markers += added
		if markers > MaxMarkers {
			return &LimitError{fmt.Sprintf("The paragraph starting on line %d has more than %d emphasis, code or link markers; previews support at most %d. Split it into shorter paragraphs.", markersFrom, MaxMarkers, MaxMarkers)}
		}
		if work > MaxMarkerWork {
			return &LimitError{fmt.Sprintf("The paragraph starting on line %d is where the post's long paragraphs, with their many emphasis and link markers, become too much to preview. Split long paragraphs.", markersFrom)}
		}
	}
	return nil
}

// isHeading reports whether trimmed, a non-blank line with its containers
// removed, is an ATX heading or a setext underline.
func isHeading(trimmed string, prevBlank bool) bool {
	if trimmed[0] == '#' {
		hashes := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
		return hashes <= 6 && (hashes == len(trimmed) || trimmed[hashes] == ' ' || trimmed[hashes] == '\t')
	}
	return !prevBlank && (strings.Trim(trimmed, "=") == "" || strings.Trim(trimmed, "-") == "")
}

// stripContainers removes the block quote and list item markers a line
// starts with. It reports how many there were and whether a list item
// started.
func stripContainers(line string) (body string, depth int, item bool) {
	for {
		line = strings.TrimLeft(line, " \t")
		switch {
		case strings.HasPrefix(line, ">"):
			line = line[1:]
		case isBullet(line):
			line, item = line[1:], true
		case orderedMarkerLen(line) > 0:
			line, item = line[orderedMarkerLen(line):], true
		default:
			return line, depth, item
		}
		depth++
	}
}

// isBullet reports whether line starts with "- ", "* " or "+ " (or is only
// the marker).
func isBullet(line string) bool {
	return line != "" && strings.IndexByte("-*+", line[0]) >= 0 && (len(line) == 1 || line[1] == ' ' || line[1] == '\t')
}

// orderedMarkerLen is the length of a leading "12." or "12)" marker that is
// followed by a space, or 0.
func orderedMarkerLen(line string) int {
	i := 0
	for i < len(line) && i < 10 && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(line) || (line[i] != '.' && line[i] != ')') {
		return 0
	}
	if i+1 < len(line) && line[i+1] != ' ' && line[i+1] != '\t' {
		return 0
	}
	return i + 1
}

// fenceOpener returns the backtick or tilde run that opens a fenced code
// block, or "".
func fenceOpener(line string) string {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return ""
	}
	run := len(line) - len(strings.TrimLeft(line, line[:1]))
	if run < 3 {
		return ""
	}
	return line[:run]
}

// delimiterColumns returns the column count if line is a GFM table
// delimiter row such as "|:--|--:|", or 0.
func delimiterColumns(line string) int {
	if !strings.Contains(line, "|") || !strings.Contains(line, "-") || strings.Trim(line, "|:- \t") != "" {
		return 0
	}
	return strings.Count(strings.Trim(line, "|"), "|") + 1
}
