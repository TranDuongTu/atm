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
// every verb and the invariant the capability maintains, and carry the
// Semantics, Actions, and Converge sections (the composed manager prompt
// points here).
func TestGuideCarriesSemantics(t *testing.T) {
	g := Cap{}.Guide()
	for _, want := range []string{
		"atm capability workflow start",
		"atm capability workflow status",
		"atm capability workflow seed",
		"exactly-one-status", "backlog", "open-tasks", "in-progress-tasks",
		"all-tasks", "paved road, not a fence",
		"atm capability workflow complete",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}

func TestGuideHasSemanticsActionsConvergeSections(t *testing.T) {
	g := Cap{}.Guide()
	for _, section := range []string{"\n## Semantics\n", "\n## Actions\n", "\n## Converge\n"} {
		if !strings.Contains(g, section) {
			t.Errorf("guide missing %q section", strings.TrimSpace(section))
		}
	}
	if strings.Contains(g, "Manager duty") {
		t.Error("guide still has the old Manager duty section")
	}
	if strings.Contains(g, "`atm workflow") || strings.Contains(g, "`atm context ") {
		t.Error("guide references pre-namespace command paths")
	}
}
