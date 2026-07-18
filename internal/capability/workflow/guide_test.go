package workflow

import (
	"strings"
	"testing"
)

func TestSummaryIsOneLine(t *testing.T) {
	s := Cap{}.Summary()
	if s == "" || strings.Contains(s, "\n") {
		t.Fatalf("Summary must be one non-empty line, got %q", s)
	}
}

// The guide is the single source of the capability's semantics: it must name
// every verb and the invariant the capability maintains, and carry a manager
// section (the composed manager prompt points here).
func TestGuideCarriesSemantics(t *testing.T) {
	g := Cap{}.Guide()
	for _, want := range []string{
		"atm workflow start", "atm workflow open", "atm workflow block",
		"atm workflow complete", "atm workflow status", "atm workflow seed",
		"exactly-one-status", "backlog", "open-tasks", "in-progress-tasks",
		"all-tasks", "## Manager duty", "paved road, not a fence",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}
