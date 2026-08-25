package qa

import (
	"atm/internal/core"
	"testing"

	"atm/internal/capability"
)

var _ capability.Flow = Cap{}

func TestVocabularySeedsLaneBoardsWithSelfExclusionInbox(t *testing.T) {
	byName := map[string]string{}
	for _, l := range Vocabulary("ATM") {
		byName[l.Name] = l.Expr
	}
	if got := byName["ATM:qa-inbox"]; got != "NOT qa:* AND NOT qa-out:*" {
		t.Fatalf("inbox expr = %q", got)
	}
	if got := byName["ATM:qa-pipeline"]; got != "qa:* AND NOT qa-out:*" {
		t.Fatalf("pipeline expr = %q", got)
	}
	if got := byName["ATM:qa-out-board"]; got != "qa-out:*" {
		t.Fatalf("out expr = %q", got)
	}
}

func TestFlowContract(t *testing.T) {
	c := New()
	if c.Name() != CapabilityName {
		t.Fatalf("name = %q", c.Name())
	}
	if c.FinishLabel("ATM").Name != "ATM:qa:done" {
		t.Fatalf("finish = %q", c.FinishLabel("ATM").Name)
	}
	if c.EvictLabel("ATM").Name != "ATM:qa-out:*" {
		t.Fatalf("evict = %q", c.EvictLabel("ATM").Name)
	}
	lanes := c.Lanes("ATM")
	if lanes.Inbox != "ATM:qa-inbox" || lanes.Pipeline != "ATM:qa-pipeline" || lanes.Out != "ATM:qa-out-board" {
		t.Fatalf("lanes = %+v", lanes)
	}
	want := []string{"qa:*", "qa-out:*"}
	if got := c.ClaimExprs(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("claim exprs = %v, want %v", got, want)
	}
}

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

// The three lanes a flow declares must be seeded boards in its own
// vocabulary — the pane selects lanes by name and renders them by expression.
func TestLanesAreSeededBoardsInVocabulary(t *testing.T) {
	byName := map[string]core.Label{}
	for _, l := range Vocabulary("ATM") {
		byName[l.Name] = l
	}
	lanes := New().Lanes("ATM")
	for _, n := range []string{lanes.Inbox, lanes.Pipeline, lanes.Out} {
		l, ok := byName[n]
		if !ok {
			t.Fatalf("lane %q is not in the vocabulary", n)
		}
		if l.Expr == "" {
			t.Fatalf("lane %q has no expression", n)
		}
	}
}
