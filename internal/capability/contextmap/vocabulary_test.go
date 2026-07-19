package contextmap

import (
	"reflect"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
)

// newTestStore opens a store in a temp dir with one project. Mirrors the
// existing helpers in internal/store/*_test.go.
func newTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	const actor = "manager@claude:opus-4.8"
	if _, err := s.CreateProject("TST", "Test", actor); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return s, actor
}

// TestEnsureVocabularyReturnsBoards asserts EnsureVocabulary returns exactly
// the one board this capability owns (context-current), and nothing else.
func TestEnsureVocabularyReturnsBoards(t *testing.T) {
	s, actor := newTestStore(t)
	boards, err := EnsureVocabulary(s, "TST", actor)
	if err != nil {
		t.Fatal(err)
	}
	want := []core.Label{
		{Name: BoardCurrent("TST"), Description: "every context pointer that has not been superseded: the project's current knowledge. Agents read this board rather than the raw context:* namespace, so a query always returns the latest.", Expr: currentExpr()},
	}
	if !reflect.DeepEqual(boards, want) {
		t.Errorf("boards = %+v, want %+v", boards, want)
	}
}

func TestEnsureVocabularyCreatesLabelsAndBoard(t *testing.T) {
	s, actor := newTestStore(t)
	if _, err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}

	// Every label this capability owns must exist AND carry a description --
	// a label without one is a defect that warns in the Boards pane.
	for _, name := range []string{
		LabelSuperseded("TST"),
		LabelProvenance("TST"),
		LabelContextKind("TST", "documentation"),
	} {
		l, err := s.LabelShow(name)
		if err != nil {
			t.Fatalf("LabelShow(%q): %v", name, err)
		}
		if l.Description == "" {
			t.Errorf("label %q has no description", name)
		}
	}

	board, err := s.LabelShow(BoardCurrent("TST"))
	if err != nil {
		t.Fatalf("LabelShow(board): %v", err)
	}
	if board.Expr == "" {
		t.Error("context-current must be a board (Expr set), got a stored label")
	}
	if board.Description == "" {
		t.Error("board has no description")
	}
}

func TestEnsureVocabularyIsIdempotent(t *testing.T) {
	s, actor := newTestStore(t)
	if _, err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("second: %v", err)
	}
}

func TestEnsureVocabularySeedsContextNamespaceDescriptor(t *testing.T) {
	s, actor := newTestStore(t)
	if _, err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := s.LabelShow("TST:context:*"); err != nil {
		t.Fatalf("EnsureVocabulary did not seed the context:* namespace descriptor: %v", err)
	}
	l, err := s.LabelShow("TST:context:agent")
	if err != nil {
		t.Fatalf("context:agent not seeded: %v", err)
	}
	if !strings.Contains(l.Description, "agent-direction") {
		t.Errorf("context:agent description not absorbed from seed.go: %q", l.Description)
	}
	// Every recognized kind must carry a non-thin description: not the old
	// "context pointer kind: X" placeholder form that the absorbed seed.go
	// default used to emit. Fold the agent assertion above into the loop's
	// "non-thin" check while keeping its specific agent-direction assertion.
	for _, kind := range ContextKinds {
		k, err := s.LabelShow(LabelContextKind("TST", kind))
		if err != nil {
			t.Fatalf("EnsureVocabulary did not seed context kind %q: %v", kind, err)
		}
		if k.Description == "" {
			t.Errorf("context:%s seeded without a description", kind)
		}
		if strings.HasPrefix(k.Description, "context pointer kind:") {
			t.Errorf("context:%s description is the thin placeholder %q", kind, k.Description)
		}
	}
	sup, err := s.LabelShow("TST:knowledge:superseded")
	if err != nil {
		t.Fatalf("superseded not seeded: %v", err)
	}
	if !strings.Contains(sup.Description, "atm capability contextmap supersede") {
		t.Errorf("superseded description still references the old command path: %q", sup.Description)
	}
}

func TestVocabularyAndExposedSets(t *testing.T) {
	vocab := Vocabulary("ATM")
	if len(vocab) != 9 {
		t.Fatalf("Vocabulary = %d labels, want 9", len(vocab))
	}
	exp := Exposed("ATM")
	if len(exp) != 1 || exp[0].Name != "ATM:context-current" || exp[0].Expr == "" {
		t.Fatalf("Exposed = %+v, want exactly the context-current board", exp)
	}
	found := false
	for _, l := range vocab {
		if l == exp[0] {
			found = true
		}
	}
	if !found {
		t.Error("Exposed[0] must be identical to its Vocabulary entry")
	}
}

func TestEnsureVocabularyPreservesHumanDescription(t *testing.T) {
	// A human curated the description. The capability must not clobber it:
	// paved road, not fence.
	s, actor := newTestStore(t)
	name := LabelSuperseded("TST")
	if err := s.LabelAdd(name, "my own wording", "", actor); err != nil {
		t.Fatalf("LabelAdd: %v", err)
	}
	if _, err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	l, err := s.LabelShow(name)
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if l.Description != "my own wording" {
		t.Errorf("description clobbered: got %q", l.Description)
	}
}
