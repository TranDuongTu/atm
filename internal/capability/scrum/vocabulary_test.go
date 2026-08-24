package scrum

import (
	"testing"

	"atm/internal/capability"
)

// scrum is a flow capability; the compiler is the first assertion of that.
var _ capability.Flow = Cap{}

func TestVocabularySeedsLaneBoardsWithSelfExclusionInbox(t *testing.T) {
	byName := map[string]string{}
	for _, l := range Vocabulary("ATM") {
		byName[l.Name] = l.Expr
	}
	if got := byName["ATM:scrum-inbox"]; got != "NOT scrum:* AND NOT scrum-out:*" {
		t.Fatalf("inbox expr = %q", got)
	}
	if got := byName["ATM:scrum-pipeline"]; got != "scrum:* AND NOT scrum-out:*" {
		t.Fatalf("pipeline expr = %q", got)
	}
	if got := byName["ATM:scrum-out-board"]; got != "scrum-out:*" {
		t.Fatalf("out expr = %q", got)
	}
}

func TestFlowContract(t *testing.T) {
	c := New()
	if c.Name() != CapabilityName {
		t.Fatalf("name = %q", c.Name())
	}
	if c.FinishLabel("ATM").Name != "ATM:scrum-stage:done" {
		t.Fatalf("finish = %q", c.FinishLabel("ATM").Name)
	}
	if c.EvictLabel("ATM").Name != "ATM:scrum-out:*" {
		t.Fatalf("evict = %q", c.EvictLabel("ATM").Name)
	}
	lanes := c.Lanes("ATM")
	if lanes.Inbox != "ATM:scrum-inbox" || lanes.Pipeline != "ATM:scrum-pipeline" || lanes.Out != "ATM:scrum-out-board" {
		t.Fatalf("lanes = %+v", lanes)
	}
	want := []string{"scrum:*", "scrum-out:*"}
	got := c.ClaimExprs()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("claim exprs = %v, want %v", got, want)
	}
}

// The declared sockets must be labels this capability actually seeds:
// a wirer reads the seeded description to learn what the socket certifies.
func TestSocketsAreSeededVocabulary(t *testing.T) {
	byName := map[string]string{}
	for _, l := range Vocabulary("ATM") {
		byName[l.Name] = l.Description
	}
	c := New()
	for _, n := range []string{c.FinishLabel("ATM").Name, c.EvictLabel("ATM").Name} {
		if byName[n] == "" {
			t.Fatalf("socket %q is not seeded with a description", n)
		}
	}
}

// Exposed ⊆ Vocabulary is a Capability-contract invariant.
func TestExposedIsSubsetOfVocabularyAndIsTheThreeLanes(t *testing.T) {
	vocab := map[string]bool{}
	for _, l := range Vocabulary("ATM") {
		vocab[l.Name] = true
	}
	exposed := Exposed("ATM")
	if len(exposed) != 3 {
		t.Fatalf("exposed = %d labels, want the 3 lanes", len(exposed))
	}
	for _, l := range exposed {
		if !vocab[l.Name] {
			t.Fatalf("exposed %q not in vocabulary", l.Name)
		}
		if l.Expr == "" {
			t.Fatalf("exposed %q has no expression", l.Name)
		}
	}
}
