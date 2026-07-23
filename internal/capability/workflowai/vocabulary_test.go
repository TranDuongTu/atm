package workflowai

import (
	"path/filepath"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
)

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
		"ATM:stage:*", "ATM:stage:queued", "ATM:stage:brainstormed",
		"ATM:stage:clarified", "ATM:stage:planned", "ATM:stage:done",
		"ATM:wfai:*", "ATM:wfai:revision", "ATM:wfai:framework",
		"ATM:to-brainstorm", "ATM:to-clarify", "ATM:to-plan",
		"ATM:to-implement", "ATM:revisions", "ATM:done-tasks",
	} {
		if !got[want] {
			t.Errorf("missing label %s", want)
		}
	}
	for _, gone := range []string{
		"ATM:stage:implementable", "ATM:new-tasks",
		"ATM:brainstormed-tasks", "ATM:planned-tasks",
	} {
		if got[gone] {
			t.Errorf("old label %s should not be seeded", gone)
		}
	}
}

func TestEnsureVocabularyReturnsTheSixBoards(t *testing.T) {
	s := newTestStore(t)
	boards, err := EnsureVocabulary(s, "ATM", "admin@cli:unset")
	if err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	if len(boards) != 6 {
		t.Fatalf("boards = %d, want 6", len(boards))
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
	if len(Exposed("ATM")) != 8 { // 6 boards + 2 namespace descriptors
		t.Errorf("Exposed = %d entries, want 8", len(Exposed("ATM")))
	}
}

func TestBoardsSelectByStage(t *testing.T) {
	s := newTestStore(t)
	actor := "admin@cli:unset"
	queued, _ := s.CreateTask("ATM", "q", "", []string{"ATM:stage:queued"}, actor)
	br, _ := s.CreateTask("ATM", "br", "", []string{"ATM:stage:brainstormed"}, actor)
	cl, _ := s.CreateTask("ATM", "cl", "", []string{"ATM:stage:clarified"}, actor)
	pl, _ := s.CreateTask("ATM", "pl", "", []string{"ATM:stage:planned"}, actor)
	rev, _ := s.CreateTask("ATM", "rev", "", []string{"ATM:stage:clarified", "ATM:wfai:revision"}, actor)
	done, _ := s.CreateTask("ATM", "dn", "", []string{"ATM:stage:done"}, actor)
	noStage, _ := s.CreateTask("ATM", "naked", "", nil, actor)

	find := func(board string) map[string]bool {
		out := map[string]bool{}
		for _, tk := range s.ListTasks(store.QueryFilters{Project: "ATM", Labels: []string{board}}) {
			out[tk.ID] = true
		}
		return out
	}
	if got := find(BoardToBrainstorm("ATM")); !got[queued.ID] || got[br.ID] || got[noStage.ID] {
		t.Errorf("to-brainstorm = %v", got)
	}
	if got := find(BoardToClarify("ATM")); !got[br.ID] || got[cl.ID] {
		t.Errorf("to-clarify = %v (want brainstormed only)", got)
	}
	if got := find(BoardToPlan("ATM")); !got[cl.ID] || !got[rev.ID] || got[pl.ID] {
		t.Errorf("to-plan = %v (want clarified, including revision-marked)", got)
	}
	if got := find(BoardToImplement("ATM")); !got[pl.ID] || got[done.ID] {
		t.Errorf("to-implement = %v", got)
	}
	if got := find(BoardRevisions("ATM")); !got[rev.ID] || got[br.ID] || got[done.ID] {
		t.Errorf("revisions = %v", got)
	}
	if got := find(BoardDoneTasks("ATM")); !got[done.ID] || got[pl.ID] {
		t.Errorf("done-tasks = %v", got)
	}
}