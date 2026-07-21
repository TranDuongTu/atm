package workflowai

import (
	"path/filepath"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
)

// newTestStore opens a fresh store with project ATM and this capability's
// vocabulary seeded. Every test in this package builds on it; nothing ever
// touches a real store.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "atm"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Atm", "admin@cli:unset"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure vocabulary: %v", err)
	}
	return s
}

func TestEnsureVocabularySeedsStageAndMarkerLabels(t *testing.T) {
	s := newTestStore(t)
	got := map[string]bool{}
	for _, l := range s.LabelList("ATM", "") {
		got[l.Name] = true
	}
	for _, want := range []string{
		"ATM:stage:*", "ATM:stage:brainstormed", "ATM:stage:clarified",
		"ATM:stage:planned", "ATM:stage:implementable", "ATM:stage:done",
		"ATM:wfai:*", "ATM:wfai:revision",
		"ATM:new-tasks", "ATM:brainstormed-tasks", "ATM:planned-tasks",
		"ATM:revisions", "ATM:done-tasks",
	} {
		if !got[want] {
			t.Errorf("missing label %s", want)
		}
	}
}

func TestEnsureVocabularyReturnsTheFiveBoards(t *testing.T) {
	s := newTestStore(t)
	boards, err := EnsureVocabulary(s, "ATM", "admin@cli:unset") // idempotent second run
	if err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	if len(boards) != 5 {
		t.Fatalf("boards = %d, want 5", len(boards))
	}
	for _, b := range boards {
		if b.Expr == "" {
			t.Errorf("board %s has empty expr", b.Name)
		}
		if _, err := core.ParseExpr(b.Expr); err != nil {
			t.Errorf("board %s expr %q does not parse: %v", b.Name, b.Expr, err)
		}
	}
}

func TestExposedIsSubsetOfVocabulary(t *testing.T) {
	vocab := map[string]bool{}
	for _, l := range Vocabulary("ATM") {
		vocab[l.Name] = true
	}
	for _, l := range Exposed("ATM") {
		if !vocab[l.Name] {
			t.Errorf("Exposed label %s not in Vocabulary", l.Name)
		}
	}
	if len(Exposed("ATM")) != 7 { // 5 boards + 2 namespace descriptors
		t.Errorf("Exposed = %d entries, want 7", len(Exposed("ATM")))
	}
}

func TestBoardsSelectByStage(t *testing.T) {
	s := newTestStore(t)
	actor := "admin@cli:unset"
	newTask, _ := s.CreateTask("ATM", "fresh", "", nil, actor)
	br, _ := s.CreateTask("ATM", "br", "", []string{"ATM:stage:brainstormed"}, actor)
	pl, _ := s.CreateTask("ATM", "pl", "", []string{"ATM:stage:planned"}, actor)
	rev, _ := s.CreateTask("ATM", "rev", "", []string{"ATM:stage:clarified", "ATM:wfai:revision"}, actor)
	done, _ := s.CreateTask("ATM", "dn", "", []string{"ATM:stage:done"}, actor)

	find := func(board string) map[string]bool {
		out := map[string]bool{}
		for _, tk := range s.ListTasks(store.QueryFilters{Project: "ATM", Labels: []string{board}}) {
			out[tk.ID] = true
		}
		return out
	}
	if got := find(BoardNewTasks("ATM")); !got[newTask.ID] || got[br.ID] {
		t.Errorf("new-tasks = %v", got)
	}
	if got := find(BoardBrainstormedTasks("ATM")); !got[br.ID] || !got[rev.ID] || got[pl.ID] {
		t.Errorf("brainstormed-tasks = %v (want brainstormed OR clarified)", got)
	}
	if got := find(BoardPlannedTasks("ATM")); !got[pl.ID] || got[done.ID] {
		t.Errorf("planned-tasks = %v", got)
	}
	if got := find(BoardRevisions("ATM")); !got[rev.ID] || got[br.ID] || got[done.ID] {
		t.Errorf("revisions = %v", got)
	}
	if got := find(BoardDoneTasks("ATM")); !got[done.ID] || got[pl.ID] {
		t.Errorf("done-tasks = %v", got)
	}
}
