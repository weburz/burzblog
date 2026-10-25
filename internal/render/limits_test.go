package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weburz/burzblog/internal/post"
)

// Generous: the point is to catch seconds and gigabytes, not to benchmark.
const (
	timeBudget  = 2 * time.Second * raceSlowdown
	allocBudget = 256 << 20
)

// measure renders source and reports the time and bytes allocated.
func measure(source string) (elapsed time.Duration, alloc uint64, err error) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	_, err = HTML(context.Background(), source)
	elapsed = time.Since(start)
	runtime.ReadMemStats(&after)
	return elapsed, after.TotalAlloc - before.TotalAlloc, err
}

// fill repeats unit until the result is size bytes.
func fill(unit string, size int) string {
	return strings.Repeat(unit, size/len(unit))
}

func table(cols, rows int) string {
	row := "|" + strings.Repeat(" x |", cols) + "\n"
	return row + "|" + strings.Repeat("---|", cols) + "\n" + strings.Repeat(row, rows)
}

func TestMaxInputMatchesPostBody(t *testing.T) {
	if MaxInput != post.MaxBody {
		t.Errorf("MaxInput = %d, post.MaxBody = %d: an author must be able to preview what they can save", MaxInput, post.MaxBody)
	}
}

func TestTableLimits(t *testing.T) {
	cases := []struct {
		name, in string
		reject   string // substring of the error; empty means accepted
	}{
		{"widest allowed", table(MaxTableColumns, 10), ""},
		{"one column too wide", table(MaxTableColumns+1, 2), "65 columns"},
		{"issue reproduction", table(2000, 3), "2000 columns"},
		{"most cells allowed", table(10, MaxTableCells/10-2), ""},
		{"too many cells", table(10, MaxTableCells/10+1), "passes 50000 table cells"},
		{"single column", "| a |\n|---|\n| 1 |\n", ""},
		{"aligned", "| a | b |\n|:-:|--:|\n| 1 | 2 |\n", ""},
		{"several tables within the total", table(10, 1000) + "\n" + table(10, 1000), ""},
		{"several tables past the total", table(10, 3000) + "\n" + table(10, 3000), "passes 50000 table cells"},
		{"wide table in code", "```\n" + table(500, 2) + "```\n", ""},
	}
	for _, c := range cases {
		elapsed, alloc, err := measure(c.in)
		var limit *LimitError
		switch {
		case c.reject == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.reject != "" && !errors.As(err, &limit):
			t.Errorf("%s: err = %v, want *LimitError", c.name, err)
		case c.reject != "" && !strings.Contains(err.Error(), c.reject):
			t.Errorf("%s: err = %q, want %q", c.name, err, c.reject)
		}
		if elapsed > timeBudget || alloc > allocBudget {
			t.Errorf("%s: took %v and %d MiB", c.name, elapsed, alloc>>20)
		}
	}
}

func TestNormalLargeDocuments(t *testing.T) {
	cases := map[string]string{
		"plain text at the cap":   fill("Some ordinary prose, with a comma or two.\n\n", MaxInput),
		"long paragraph":          fill("word _emphasis_ **strong** [link](https://example.com) ", 20<<10) + "\n",
		"thousands of list links": fill("- [A link](https://example.com/a) and *more*\n", 512<<10),
		"big table":               table(5, 3000),
		"code with markers":       "```\n" + fill("a[0] * b[1] ~ c_d\n", 512<<10) + "```\n",
		"nested lists":            "- a\n  - b\n    - c\n      - d\n        - e\n          - f\n",
		"quoted list":             "> - a\n>   > - b\n",
	}
	for name, in := range cases {
		elapsed, alloc, err := measure(in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
		t.Logf("%-24s %8v %4d MiB", name, elapsed.Round(time.Millisecond), alloc>>20)
		if elapsed > timeBudget || alloc > allocBudget {
			t.Errorf("%s: took %v and %d MiB", name, elapsed, alloc>>20)
		}
	}
}

// The inputs behind issue 75, each at the largest size a request may carry.
func TestQuadraticConstructsAtTheCap(t *testing.T) {
	cases := map[string]string{
		"unclosed emphasis":      fill("*a", MaxInput),
		"unclosed code spans":    fill("`a", MaxInput),
		"repeated headings":      fill("# a\n", MaxInput),
		"repeated setext":        fill("a\n=\n", MaxInput),
		"unclosed strikethrough": fill("~~a", MaxInput),
		"nested list markers":    fill("- ", MaxInput) + "x",
		"nested numbered":        fill("1. ", MaxInput) + "x",
		"runs of >":              fill(">", MaxInput) + "x",
		"spaced >":               fill("> ", MaxInput) + "x",
		"link openers":           fill("[a](b", MaxInput),
		"bare brackets":          fill("[", MaxInput),
		"image openers":          fill("![", MaxInput),
		"reference openers":      fill("[a][", MaxInput),
		"wide table":             table(4000, 100),
		"many columns, one row":  table(100000, 0),
		"max-depth quotes":       fill(strings.Repeat("> ", MaxNesting)+"x\n", MaxInput),
		"max-depth lists":        fill(strings.Repeat("- ", MaxNesting)+"x\n", MaxInput),
		"max-width tables":       fill(table(MaxTableColumns, 300)+"\n", MaxInput),
		"shallow lists":          fill(strings.Repeat("- ", shallowNesting)+"x\n", MaxInput),
		"shallow quotes":         fill(strings.Repeat("> ", shallowNesting)+"x\n", MaxInput),
		"one long list":          fill("- x\n", MaxInput),
		"short lines":            fill("a\n", MaxInput),
		"tiny table rows":        table(1, MaxInput/4),
		"max markers repeated":   fill(strings.Repeat("*a", MaxMarkers/2)+"\n\n", MaxInput),
		"max link markers":       fill(strings.Repeat("[a](b", MaxMarkers)+"\n\n", MaxInput),
	}
	for name, in := range cases {
		elapsed, alloc, err := measure(in)
		var limit *LimitError
		if err != nil && !errors.As(err, &limit) {
			t.Errorf("%s: unexpected error %v", name, err)
		}
		t.Logf("%-24s %8v %4d MiB rejected=%v", name, elapsed.Round(time.Millisecond), alloc>>20, err != nil)
		if elapsed > timeBudget || alloc > allocBudget {
			t.Errorf("%s: took %v and %d MiB; budget %v and %d MiB", name, elapsed, alloc>>20, timeBudget, allocBudget>>20)
		}
	}
}

// Inputs just inside every limit: the most work a preview can be asked for.
func TestWorstAcceptedInputs(t *testing.T) {
	paragraph := strings.Repeat("[a](b", MaxMarkers) + "\n\n"
	cases := map[string]string{
		"repeated headings":   strings.Repeat("# a\n", MaxHeadings),
		"repeated setext":     strings.Repeat("a\n---\n", MaxHeadings),
		"code span work":      strings.Repeat(strings.Repeat("`a", MaxMarkers)+"\n\n", 2),
		"lines":               fill("a\n", 2*MaxLines),
		"list items":          fill("- x\n", 4*MaxLines),
		"nested items":        fill(strings.Repeat("- ", shallowNesting)+"x\n", 10*MaxLines),
		"deep nesting":        strings.Repeat(strings.Repeat("> ", shallowNesting+4)+"x\n", MaxDeepLevels/4),
		"quotes":              fill(strings.Repeat("> ", shallowNesting)+"x\n", 10*MaxLines),
		"table cells":         table(MaxTableColumns, MaxTableCells/MaxTableColumns-4),
		"marker work":         paragraph + paragraph,
		"emphasis work":       strings.Repeat(strings.Repeat("*a", MaxMarkers)+"\n\n", 2),
		"every limit at once": table(MaxTableColumns, 300) + "\n" + paragraph + fill("- x\n", 2*MaxLines),
	}
	for name, in := range cases {
		elapsed, alloc, err := measure(in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
		t.Logf("%-20s %8v %4d MiB", name, elapsed.Round(time.Millisecond), alloc>>20)
		if elapsed > timeBudget || alloc > allocBudget {
			t.Errorf("%s: took %v and %d MiB; budget %v and %d MiB", name, elapsed, alloc>>20, timeBudget, allocBudget>>20)
		}
	}
}

func TestConstructLimitMessagesNameTheLine(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"nesting", "a\n\nb\n" + strings.Repeat("> ", MaxNesting+1) + "x", "Line 4 nests"},
		{"headings", fill("# a\n", 8*MaxHeadings), "passes 1000 headings"},
		{"too many lines", fill("a\n", 4*MaxLines), "passes 50000 lines"},
		{"nesting in total", fill(strings.Repeat("- ", 10)+"x\n", MaxInput), "levels of deeply nested"},
		{"markers", "a\n\n" + strings.Repeat("*a", MaxMarkers+1), "paragraph starting on line 3"},
		{"markers across lines", "a\n\n" + strings.Repeat(strings.Repeat("*a", 100)+"\n", MaxMarkers/100+1), "paragraph starting on line 3"},
		{"table", "intro\n\n" + table(MaxTableColumns+1, 1), "table on line 4"},
	}
	for _, c := range cases {
		_, err := HTML(context.Background(), c.in)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestMarkersResetPerBlock(t *testing.T) {
	// Each of these has more markers than the limit in total but never in
	// one paragraph, list item or table row.
	for name, in := range map[string]string{
		"paragraphs": fill(strings.Repeat("*a*", 10)+"\n\n", 1<<20),
		"list items": fill("- [a](b) *c*\n", 600<<10),
		"headings":   fill("# [a](b) *c*\n", 900*13),
	} {
		if _, err := HTML(context.Background(), in); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestOutputLimit(t *testing.T) {
	b := &limitedBuffer{limit: 10}
	if n, err := b.Write([]byte("0123456789")); err != nil || n != 10 {
		t.Fatalf("write up to the limit: %d, %v", n, err)
	}
	if _, err := b.Write([]byte("x")); !errors.Is(err, errOutputTooLarge) {
		t.Errorf("write past the limit: err = %v", err)
	}
	if got := b.String(); got != "0123456789" {
		t.Errorf("buffer = %q", got)
	}
	var _ = bytes.Buffer{}
}

func TestConcurrencyLimit(t *testing.T) {
	var (
		mu           sync.Mutex
		running, max int
	)
	hold := make(chan struct{})
	// Take every slot but count how many goroutines get in at once.
	var wg sync.WaitGroup
	for i := 0; i < MaxConcurrent*3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-time.After(time.Second):
				return
			}
			mu.Lock()
			running++
			if running > max {
				max = running
			}
			mu.Unlock()
			<-hold
			mu.Lock()
			running--
			mu.Unlock()
			<-slots
		}()
	}
	time.Sleep(100 * time.Millisecond)
	// With every slot taken, a render must wait and then give up.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := HTML(ctx, "hi"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("render with no free slot: err = %v, want context deadline", err)
	}
	close(hold)
	wg.Wait()
	if max != MaxConcurrent {
		t.Errorf("%d slots held at once, want %d", max, MaxConcurrent)
	}
	if _, err := HTML(context.Background(), "hi"); err != nil {
		t.Errorf("render after slots freed: %v", err)
	}
}

func TestBusyAfterMaxWait(t *testing.T) {
	old := maxWait
	maxWait = 20 * time.Millisecond
	defer func() { maxWait = old }()
	for i := 0; i < MaxConcurrent; i++ {
		slots <- struct{}{}
	}
	defer func() {
		for i := 0; i < MaxConcurrent; i++ {
			<-slots
		}
	}()
	if _, err := HTML(context.Background(), "hi"); !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v, want ErrBusy", err)
	}
}

func TestConcurrentRendersAreBounded(t *testing.T) {
	// Many parallel previews of a heavy but allowed document all finish and
	// agree with each other. The slot wait is lengthened so a loaded machine
	// running -race cannot turn queueing into ErrBusy; that path has its own test.
	defer func(old time.Duration) { maxWait = old }(maxWait)
	maxWait = time.Minute
	in := table(MaxTableColumns, MaxTableCells/MaxTableColumns-2)
	want, err := HTML(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := HTML(context.Background(), in)
			if err == nil && got != want {
				err = fmt.Errorf("output differs")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}
