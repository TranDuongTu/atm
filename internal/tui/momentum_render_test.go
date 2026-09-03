package tui

import (
	"strings"
	"testing"
)

func plainLines(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = stripANSI(l)
	}
	return strings.Join(out, "\n")
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
