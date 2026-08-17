package setup

import "testing"

// Fact is a tri-state on purpose. A probe that could not answer is neither
// present nor absent, and collapsing it to a bool is how a surface starts
// lying about setup it never checked.
func TestFactTriState(t *testing.T) {
	if FactUnknown == FactPresent || FactUnknown == FactAbsent {
		t.Fatal("unknown must be distinct from present and absent")
	}
	if FactUnknown.String() != "unknown" {
		t.Fatalf("String() = %q", FactUnknown.String())
	}
}

func TestAgentRowGlyphGradesByWhoCanFixIt(t *testing.T) {
	cases := []struct {
		name   string
		row    AgentRow
		glyph  string
	}{
		{"ready", AgentRow{Binary: FactPresent, Plugin: FactPresent}, "●"},
		{"plugin missing is fixable here", AgentRow{Binary: FactPresent, Plugin: FactAbsent}, "◐"},
		{"binary missing is fixed outside atm", AgentRow{Binary: FactAbsent, Plugin: FactAbsent}, "○"},
		{"binary missing outranks a present plugin", AgentRow{Binary: FactAbsent, Plugin: FactPresent}, "○"},
	}
	for _, c := range cases {
		if got := c.row.Glyph(); got != c.glyph {
			t.Fatalf("%s: glyph = %q, want %q", c.name, got, c.glyph)
		}
	}
}
