package workflowai

import (
	"strings"
	"testing"

	"atm/internal/capability"
)

// The full interface must be satisfied — the packaging design freezes it.
var _ capability.Capability = Cap{}

func TestNameIsTheMetadataKey(t *testing.T) {
	if (Cap{}.Name()) != CapabilityName || CapabilityName != "workflow_ai" {
		t.Fatalf("Name/CapabilityName mismatch: %q vs %q", Cap{}.Name(), CapabilityName)
	}
}

func TestSummaryIsOneLine(t *testing.T) {
	s := Cap{}.Summary()
	if s == "" || strings.Contains(s, "\n") {
		t.Fatalf("Summary must be one non-empty line, got %q", s)
	}
}

// The guide is the single source of the capability's semantics: every verb,
// the ladder, the invariant, the boards, and the operating doctrine.
func TestGuideCarriesSemantics(t *testing.T) {
	g := Cap{}.Guide()
	for _, want := range []string{
		"atm capability workflow_ai brainstorm",
		"atm capability workflow_ai clarify",
		"atm capability workflow_ai plan",
		"atm capability workflow_ai ready",
		"atm capability workflow_ai done",
		"atm capability workflow_ai demote",
		"atm capability workflow_ai link",
		"atm capability workflow_ai report",
		"atm capability workflow_ai links",
		"atm capability workflow_ai seed",
		"exactly-one-stage", "paved road, not a fence",
		"new-tasks", "brainstormed-tasks", "planned-tasks", "revisions", "done-tasks",
		"stage:implementable", "never implement", "ephemeral",
		"revision_of", "relates_to",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}

func TestGuideHasBriefAndAutopilotSections(t *testing.T) {
	g := Cap{}.Guide()
	for _, section := range []string{"\n## Brief\n", "\n## Autopilot\n"} {
		if !strings.Contains(g, section) {
			t.Errorf("guide missing %q section", strings.TrimSpace(section))
		}
	}
}
