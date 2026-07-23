package tui

import "testing"

// TestProjectPaneSplitHeights4Way pins the four-way split projectPaneSplitHeights
// allocates the Projects pane into: project list (fixed page of 5 rows),
// background art (absorbs the spare height, folding into summary below its
// 3-line minimum), recent-events feed (~35%, collapsing under 4), and summary
// (~35%, keeping the bottom). See ATM-4eae82.
func TestProjectPaneSplitHeights4Way(t *testing.T) {
	cases := []struct {
		name                           string
		total                          int
		listH, artH, eventsH, summaryH int
	}{
		// Tall pane: list capped at 9 (5 rows + 4 overhead), events 35%,
		// summary 35%, art absorbs the rest.
		{"tall", 40, 9, 3, 14, 14},
		// Art below 3 lines folds into summary.
		{"art-folds", 30, 9, 0, 10, 11},
		// Scarce: list first, then summary, events collapse under 4.
		{"scarce", 10, 9, 0, 0, 1},
		// Degenerate.
		{"tiny", 5, 5, 0, 0, 0},
		{"one", 1, 1, 0, 0, 0},
		{"zero", 0, 0, 0, 0, 0},
	}
	for _, c := range cases {
		l, a, e, s := projectPaneSplitHeights(c.total)
		if l != c.listH || a != c.artH || e != c.eventsH || s != c.summaryH {
			t.Errorf("%s(%d): got (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				c.name, c.total, l, a, e, s, c.listH, c.artH, c.eventsH, c.summaryH)
		}
	}
}

// TestProjectPaneSplitAlwaysSumsToTotal is the invariant every caller relies
// on: the four sections must always exactly fill the pane, and any non-zero
// art region must be at least 3 lines (below that it isn't worth drawing and
// must fold into summary instead).
func TestProjectPaneSplitAlwaysSumsToTotal(t *testing.T) {
	for total := 0; total <= 120; total++ {
		l, a, e, s := projectPaneSplitHeights(total)
		if l+a+e+s != total {
			t.Fatalf("total %d: sections sum to %d", total, l+a+e+s)
		}
		if a != 0 && a < 3 {
			t.Fatalf("total %d: art region %d below minimum 3", total, a)
		}
	}
}

// TestListPageSizeCappedAtFive pins listPageSize to a fixed page of 5 rows
// now that the list section is sized for exactly 5 by projectPaneSplitHeights,
// degrading only when the whole pane is shorter than the fixed list section.
func TestListPageSizeCappedAtFive(t *testing.T) {
	p := &projectsModel{}
	if got := p.listPageSize(9); got != 5 {
		t.Fatalf("listPageSize(9) = %d, want 5", got)
	}
	if got := p.listPageSize(100); got != 5 {
		t.Fatalf("listPageSize(100) = %d, want 5 (fixed page)", got)
	}
	if got := p.listPageSize(7); got != 3 {
		t.Fatalf("listPageSize(7) = %d, want 3 (short pane degrades)", got)
	}
	if got := p.listPageSize(2); got != 1 {
		t.Fatalf("listPageSize(2) = %d, want 1 (floor)", got)
	}
}
