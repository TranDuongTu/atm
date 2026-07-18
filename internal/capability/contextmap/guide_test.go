package contextmap

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

// The guide absorbs the mapping procedure that used to live in the manager
// prompt template: verbs, the check report vocabulary, and the Brief/Autopilot
// sections must all be present here, because nothing else states them.
func TestGuideCarriesSemanticsAndBriefAutopilot(t *testing.T) {
	g := Cap{}.Guide()
	for _, want := range []string{
		"atm capability contextmap add", "atm capability contextmap stamp",
		"atm capability contextmap retarget",
		"atm capability contextmap supersede", "atm capability contextmap check",
		"DRIFT", "AGE", "UNVERIFIED", "NEW",
		"context-current",
		"1. **Verify.**", "2. **Discover.**", "3. **Close.**",
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
	if strings.Contains(g, "Manager duty") {
		t.Error("guide still has the old Manager duty section")
	}
	if strings.Contains(g, "`atm workflow") || strings.Contains(g, "`atm context ") {
		t.Error("guide references pre-namespace command paths")
	}
}

func TestManagerActionIsMapping(t *testing.T) {
	acts := Cap{}.ManagerActions()
	if len(acts) != 1 || acts[0].Name != "mapping" || acts[0].Summary == "" {
		t.Fatalf("contextmap must contribute exactly the mapping action, got %+v", acts)
	}
}
