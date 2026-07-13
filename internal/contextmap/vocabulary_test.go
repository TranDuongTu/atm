package contextmap

import (
	"testing"

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

func TestEnsureVocabularyCreatesLabelsAndBoard(t *testing.T) {
	s, actor := newTestStore(t)
	if err := EnsureVocabulary(s, "TST", actor); err != nil {
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
	if err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("second: %v", err)
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
	if err := EnsureVocabulary(s, "TST", actor); err != nil {
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
