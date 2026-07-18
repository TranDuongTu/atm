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
// prompt template: verbs, the check report vocabulary, and the three-step
// manager duty must all be present here, because nothing else states them.
func TestGuideCarriesSemanticsAndManagerDuty(t *testing.T) {
	g := Cap{}.Guide()
	for _, want := range []string{
		"atm context add", "atm context stamp", "atm context retarget",
		"atm context supersede", "atm context check",
		"DRIFT", "AGE", "UNVERIFIED", "NEW",
		"context-current",
		"## Manager duty",
		"1. **Verify.**", "2. **Discover.**", "3. **Close.**",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}

func TestManagerActionIsMapping(t *testing.T) {
	acts := Cap{}.ManagerActions()
	if len(acts) != 1 || acts[0].Name != "mapping" || acts[0].Summary == "" {
		t.Fatalf("contextmap must contribute exactly the mapping action, got %+v", acts)
	}
}
