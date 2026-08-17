package tui

import "testing"

func TestSummaryChartsBoxedThreshold(t *testing.T) {
	if summaryChartsBoxed(6) {
		t.Fatal("summary height 6 must use compact charts")
	}
	if !summaryChartsBoxed(7) {
		t.Fatal("summary height 7 must box the combined chart")
	}
}
