package tui

import "testing"

func TestSummaryChartsBoxedThreshold(t *testing.T) {
	for _, height := range []int{6, 7, 8, 9, 10} {
		if summaryChartsBoxed(height) {
			t.Fatalf("summary height %d must use compact charts", height)
		}
	}
	if !summaryChartsBoxed(11) {
		t.Fatal("summary height 11 must box the combined chart")
	}
}
