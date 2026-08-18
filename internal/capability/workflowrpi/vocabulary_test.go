package workflowrpi

import (
	"path/filepath"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
)

const testActor = "admin@cli:unset"

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
	if _, err := s.CreateProject("ATM", "Atm", testActor); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", testActor); err != nil {
		t.Fatalf("ensure vocabulary: %v", err)
	}
	return s
}

func TestEnsureVocabularySeedsLabelsAndBoards(t *testing.T) {
	s := newTestStore(t)
	got := map[string]bool{}
	for _, l := range s.LabelList("ATM", "") {
		got[l.Name] = true
	}
	for _, want := range []string{
		"ATM:rpi:*", "ATM:rpi:product", "ATM:rpi:pipeline", "ATM:rpi:reject",
		"ATM:rpi-product:*", "ATM:rpi-product:unclarified", "ATM:rpi-product:clarified",
		"ATM:rpi-dev:*", "ATM:rpi-dev:clarified", "ATM:rpi-dev:brainstormed",
		"ATM:rpi-dev:planned", "ATM:rpi-dev:implementing", "ATM:rpi-dev:review", "ATM:rpi-dev:done",
		"ATM:rpi-reject:*", "ATM:rpi-reject:duplicate", "ATM:rpi-reject:out-of-scope",
		"ATM:rpi-reject:not-worth-it", "ATM:rpi-reject:covered-by",
		"ATM:rpi-backlog", "ATM:rpi-product", "ATM:rpi-pipeline", "ATM:rpi-reject",
	} {
		if !got[want] {
			t.Errorf("missing label %s", want)
		}
	}
}

func TestEnsureVocabularyReturnsFourBoards(t *testing.T) {
	s := newTestStore(t)
	boards, err := EnsureVocabulary(s, "ATM", testActor)
	if err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	if len(boards) != 4 {
		t.Fatalf("boards = %d, want 4", len(boards))
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
	if len(Exposed("ATM")) != 8 { // 4 boards + 4 namespace descriptors
		t.Errorf("Exposed = %d entries, want 8", len(Exposed("ATM")))
	}
}

func TestBoardsSelectByRPIState(t *testing.T) {
	s := newTestStore(t)
	product, _ := s.CreateTask("ATM", "product", "", []string{"ATM:rpi:product"}, testActor)
	pipeline, _ := s.CreateTask("ATM", "pipeline", "", []string{"ATM:rpi:pipeline"}, testActor)
	reject, _ := s.CreateTask("ATM", "reject", "", []string{"ATM:rpi:reject"}, testActor)
	backlog, _ := s.CreateTask("ATM", "backlog", "", nil, testActor)

	find := func(board string) map[string]bool {
		out := map[string]bool{}
		for _, tk := range s.ListTasks(store.QueryFilters{Project: "ATM", Labels: []string{board}}) {
			out[tk.ID] = true
		}
		return out
	}
	if got := find(BoardBacklog("ATM")); !got[backlog.ID] || got[product.ID] || got[pipeline.ID] || got[reject.ID] {
		t.Errorf("rpi-backlog = %v", got)
	}
	if got := find(BoardProduct("ATM")); !got[product.ID] || got[backlog.ID] {
		t.Errorf("rpi-product = %v", got)
	}
	if got := find(BoardPipeline("ATM")); !got[pipeline.ID] || got[backlog.ID] {
		t.Errorf("rpi-pipeline = %v", got)
	}
	if got := find(BoardReject("ATM")); !got[reject.ID] || got[backlog.ID] {
		t.Errorf("rpi-reject = %v", got)
	}
}
