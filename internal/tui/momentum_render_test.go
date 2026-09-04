package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func plainLines(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = stripANSI(l)
	}
	return strings.Join(out, "\n")
}

// ansiFG extracts the 256-color foreground codes (`38;5;N`) of one
// rendered string. Empty means plain text. The strip and the legend must
// speak the same palette, so tests pin the mapping, not exact glyph runs.
var fgRe = regexp.MustCompile("38;5;([0-9]+)")

func ansiFG(s string) map[string]bool {
	codes := map[string]bool{}
	for _, m := range fgRe.FindAllStringSubmatch(s, -1) {
		codes[m[1]] = true
	}
	return codes
}

func TestRenderFlowStripOneColumnPerBucket(t *testing.T) {
	in := []int{0, 4, 8, 16}
	done := []int{0, 0, 8, 8}
	evict := []int{0, 4, 0, 8}
	got := plainLines(renderFlowStrip(in, done, evict, 2, 4, 2, buildStyles(themeGraphite)))
	want := strings.Join([]string{
		"     █", // top row: only the 16 (=2 full rows) reaches it
		"   ▄██", // bottom-above row: 4 -> half, 8 -> full, 16 -> full
		"   ▀██", // top-below row: 4 -> upper half, 8 -> full, 16 -> full
		"     █", // bottom-below row: only the 16
	}, "\n")
	if got != want {
		t.Fatalf("strip =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderFlowStripTwoColumnsPerBucketAndPadding(t *testing.T) {
	got := renderFlowStrip([]int{8, 0}, []int{0, 8}, []int{0, 0}, 1, 5, 1, buildStyles(themeGraphite))
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	if p := stripANSI(got[0]); p != " ██   " {
		t.Fatalf("above = %q", p)
	}
	if p := stripANSI(got[1]); p != "   ██ " {
		t.Fatalf("below = %q", p)
	}
}

func TestRenderFlowStripMergesWhenNarrow(t *testing.T) {
	got := renderFlowStrip([]int{1, 1, 1, 1}, []int{0, 0, 0, 0}, []int{0, 0, 0, 0}, 0, 2, 1, buildStyles(themeGraphite))
	if p := stripANSI(got[0]); p != "██" {
		t.Fatalf("merged above = %q, want two full columns (1+1 each at max 2)", p)
	}
}

func TestRenderFlowStripAllZero(t *testing.T) {
	got := renderFlowStrip([]int{0, 0}, []int{0, 0}, []int{0, 0}, 0, 2, 1, buildStyles(themeGraphite))
	if p := plainLines(got); p != "  \n  " {
		t.Fatalf("zero strip = %q", p)
	}
}

// The legend is the color key for the strip: in reads in the accent colour
// (the strip's above-baseline glyphs), done in success, evict in error —
// distinct, because graphite paints accent and warning with the SAME 256
// colour (214), which made in and evict indistinguishable.
func TestRenderFlowStripColorsAreDistinctPerSeries(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	st := buildStyles(themeGraphite)
	in := st.HeaderLabel.Render("██")
	done := st.Success.Render("██")
	evict := st.Error.Render("██")
	inC, doneC, evictC := ansiFG(in), ansiFG(done), ansiFG(evict)
	if len(inC) == 0 || len(doneC) == 0 || len(evictC) == 0 {
		t.Fatalf("styles carry no 256-color codes: in=%v done=%v evict=%v", inC, doneC, evictC)
	}
	if equalSets(inC, evictC) {
		t.Fatalf("in colour %v equals evict colour: the two rates are indistinguishable", inC)
	}
	if equalSets(inC, doneC) || equalSets(doneC, evictC) {
		t.Fatalf("series colours collide: in=%v done=%v evict=%v", inC, doneC, evictC)
	}
}

func TestRenderFlowStripUsesSeriesPalette(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	st := buildStyles(themeGraphite)
	// Column 0: only in (fills both above rows). Column 1: done+evict 4+4 —
	// the shared eighth split puts the first eighth on done, the rest on
	// evict, so row 2 wears done's colour and row 3 evict's.
	got := renderFlowStrip([]int{8, 0}, []int{0, 4}, []int{0, 4}, 0, 2, 2, st)
	wantIn := ansiFG(st.HeaderLabel.Render("x"))
	wantDone := ansiFG(st.Success.Render("x"))
	wantEvict := ansiFG(st.Error.Render("x"))
	if c := ansiFG(got[0]); len(c) == 0 || !equalSets(c, wantIn) {
		t.Fatalf("in row colour = %v, want %v", c, wantIn)
	}
	if c := ansiFG(got[2]); len(c) == 0 || !equalSets(c, wantDone) {
		t.Fatalf("done row colour = %v, want %v", c, wantDone)
	}
	if c := ansiFG(got[3]); len(c) == 0 || !equalSets(c, wantEvict) {
		t.Fatalf("evict row colour = %v, want %v", c, wantEvict)
	}
}

func equalSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// segmentAt returns the ANSI-coded substring of s spanning the plain-text
// runes from column idx to idx+plainLen, escapes included, so a test can
// read the colour one legend entry wears.
func segmentAt(s string, idx, plainLen int) string {
	var out strings.Builder
	plainIdx := 0
	i := 0
	for i < len(s) && plainIdx < idx+plainLen {
		if s[i] == '\x1b' {
			if end := strings.IndexByte(s[i:], 'm'); end >= 0 {
				if plainIdx >= idx {
					out.WriteString(s[i : i+end+1])
				}
				i += end + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if plainIdx >= idx {
			out.WriteString(string(r))
		}
		i += size
		plainIdx++
	}
	return out.String()
}

func TestRenderMomentumChartHeightAndLegend(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	st := buildStyles(themeGraphite)
	series := momentumSeries{
		In:    []int{1, 0, 2, 0, 1, 0, 0},
		Done:  []int{0, 1, 0, 0, 1, 0, 0},
		Evict: []int{0, 0, 0, 1, 0, 0, 0},
		Open:  []int{3, 2, 4, 3, 3, 3, 3},
	}
	end := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	out := renderMomentumChart(series, chartRanges[0], 60, end, st)
	lines := strings.Split(out, "\n")
	if len(lines) != momentumInnerHeight {
		t.Fatalf("lines = %d, want %d:\n%s", len(lines), momentumInnerHeight, stripANSI(out))
	}
	legend := stripANSI(lines[len(lines)-1])
	for _, want := range []string{"in +4", "done ✓2", "evict ✗1", "open 3"} {
		if !strings.Contains(legend, want) {
			t.Fatalf("legend %q lacks %q", legend, want)
		}
	}
	if strings.Contains(legend, "Range:") {
		t.Fatalf("legend repeats the range the box title already carries: %q", legend)
	}
	if !strings.Contains(legend, "Ctrl+") {
		t.Fatalf("legend lost the range key hint: %q", legend)
	}
	// Color-keyed: each entry wears its series' colour — in/open accent
	// (depth line), done success, evict error. Checked per entry so a
	// legend that renders one colour for all cannot pass.
	wantIn := ansiFG(st.HeaderLabel.Render("x"))
	wantDone := ansiFG(st.Success.Render("x"))
	wantEvict := ansiFG(st.Error.Render("x"))
	if equalSets(wantIn, wantDone) || equalSets(wantIn, wantEvict) {
		t.Fatalf("test palette collapsed: in=%v done=%v evict=%v", wantIn, wantDone, wantEvict)
	}
	for i, entry := range []struct {
		text string
		want map[string]bool
	}{
		{"in +4", wantIn},
		{"done ✓2", wantDone},
		{"evict ✗1", wantEvict},
		{"open 3", wantIn},
	} {
		plain := stripANSI(lines[len(lines)-1])
		byteIdx := strings.Index(plain, entry.text)
		if byteIdx < 0 {
			t.Fatalf("legend lacks %q", entry.text)
		}
		idx := utf8.RuneCountInString(plain[:byteIdx]) // Index is bytes; segmentAt counts runes
		seg := ansiFG(segmentAt(lines[len(lines)-1], idx, len([]rune(entry.text))))
		if !equalSets(seg, entry.want) {
			t.Fatalf("legend entry %d (%q) colour = %v, want %v", i, entry.text, seg, entry.want)
		}
	}
	// The depth tier keeps the shared relative x-axis labels: at four x
	// steps the leftmost is the window's first day. (relDayLabel's "Today"
	// only lands on a step at some widths, so it is not what we pin.)
	if !strings.Contains(stripANSI(out), "6d ago") {
		t.Fatalf("depth tier lost the relative x labels:\n%s", stripANSI(out))
	}
}

func TestRenderMomentumChartTooNarrow(t *testing.T) {
	if out := renderMomentumChart(momentumSeries{Open: []int{1}}, chartRanges[0], 10, time.Now(), buildStyles(themeGraphite)); out != "" {
		t.Fatalf("narrow chart = %q, want empty", out)
	}
}
