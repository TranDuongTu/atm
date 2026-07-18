package workflow

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
)

// recordingLabelService wraps a core.LabelService and records every
// LabelSeed(name, desc, expr, actor) call. It exists so a test can assert
// that EnsureVocabulary itself issued the seed calls (independent of any
// seeding that happened earlier, e.g. via store.CreateProject), which a
// plain *store.Store cannot prove -- LabelSeed on an existing label is a
// silent no-op.
type recordingLabelService struct {
	core.LabelService
	seedCalls []labelSeedCall
}

type labelSeedCall struct {
	name, desc, expr, actor string
}

func (r *recordingLabelService) LabelSeed(name, description, expr, actor string) error {
	r.seedCalls = append(r.seedCalls, labelSeedCall{name, description, expr, actor})
	return r.LabelService.LabelSeed(name, description, expr, actor)
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "atm")
	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Atm", "admin@cli:unset"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return s
}

func TestEnsureVocabularyCreatesOpenTasksBoard(t *testing.T) {
	s := newTestStore(t)
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardOpenTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Expr == "" {
		t.Error("open-tasks board has no expression")
	}
	if l.Description == "" {
		t.Error("open-tasks board has no description")
	}
}

func TestEnsureVocabularyIdempotent(t *testing.T) {
	s := newTestStore(t)
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestEnsureVocabularyDoesNotOverwriteHumanDescription(t *testing.T) {
	s := newTestStore(t)
	humanDesc := "curated by a human"
	if err := s.LabelAdd(BoardOpenTasks("ATM"), humanDesc, "status:open", "admin@cli:unset"); err != nil {
		t.Fatalf("seed human label: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardOpenTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Description != humanDesc {
		t.Errorf("description = %q, want %q (human curation must survive ensure)", l.Description, humanDesc)
	}
}

func TestEnsureVocabularyWorksWithoutLabelSeed(t *testing.T) {
	s := newTestStore(t)
	// Intentionally do NOT call SeedLabels.
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := s.LabelShow(BoardOpenTasks("ATM")); err != nil {
		t.Errorf("open-tasks missing after ensure without seed: %v", err)
	}
}

func TestEnsureVocabularySeedsBacklogBoard(t *testing.T) {
	s := newTestStore(t)
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardBacklog("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Expr != "NOT status:*" {
		t.Errorf("backlog expr = %q, want %q", l.Expr, "NOT status:*")
	}
	if l.Description == "" {
		t.Error("backlog board has no description")
	}
}

func TestEnsureVocabularySeedsInProgressTasksBoard(t *testing.T) {
	s := newTestStore(t)
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardInProgressTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Expr != "status:in-progress" {
		t.Errorf("in-progress-tasks expr = %q, want %q", l.Expr, "status:in-progress")
	}
	if l.Description == "" {
		t.Error("in-progress-tasks board has no description")
	}
}

func TestEnsureVocabularyPreservesHumanBacklogDescription(t *testing.T) {
	s := newTestStore(t)
	humanDesc := "curated by a human"
	if err := s.LabelAdd(BoardBacklog("ATM"), humanDesc, "NOT status:*", "admin@cli:unset"); err != nil {
		t.Fatalf("seed human label: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardBacklog("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Description != humanDesc {
		t.Errorf("backlog description = %q, want %q (human curation must survive ensure)", l.Description, humanDesc)
	}
}

func TestEnsureVocabularySeedsAllTasksBoard(t *testing.T) {
	s := newTestStore(t)
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardAllTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Expr != "*" {
		t.Errorf("all-tasks expr = %q, want %q", l.Expr, "*")
	}
	if l.Description == "" {
		t.Error("all-tasks board has no description")
	}
}

func TestEnsureVocabularyFreshOpenTasksDescriptionDropsDefaultClause(t *testing.T) {
	// On a fresh project (open-tasks does not yet exist), LabelSeed writes
	// the new description that drops the "Default board in the TUI." clause
	// (all-tasks now holds that role). Existing projects keep their current
	// description (the never-overwrite contract); that path is covered by
	// TestEnsureVocabularyDoesNotOverwriteHumanDescription.
	s := newTestStore(t)
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardOpenTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Description == "" {
		t.Fatal("open-tasks board has no description")
	}
	if strings.Contains(l.Description, "Default board in the TUI") {
		t.Errorf("open-tasks description = %q, still references 'Default board'; all-tasks is now the default", l.Description)
	}
}

func TestEnsureVocabularySeedsStatusLabels(t *testing.T) {
	s := newTestStore(t)
	// Wrap the store so we can observe the LabelSeed calls EnsureVocabulary
	// itself issues. A plain *store.Store cannot prove EnsureVocabulary
	// issued the calls directly: LabelSeed on an existing label is a silent
	// no-op, so prior state would mask the capability's own work. The
	// recording wrapper makes EnsureVocabulary's own calls visible
	// independent of any prior state.
	rec := &recordingLabelService{LabelService: s}
	if _, err := EnsureVocabulary(rec, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// EnsureVocabulary must itself issue a LabelSeed for each status label,
	// with an empty expr (status:* is a namespace label, not a board).
	wantStatus := []string{
		"ATM:status:*", "ATM:status:open", "ATM:status:in-progress",
		"ATM:status:blocked", "ATM:status:done",
	}
	seen := map[string]labelSeedCall{}
	for _, c := range rec.seedCalls {
		seen[c.name] = c
	}
	for _, want := range wantStatus {
		c, ok := seen[want]
		if !ok {
			t.Errorf("EnsureVocabulary did not LabelSeed %s (calls: %v)", want, rec.seedCalls)
			continue
		}
		if c.expr != "" {
			t.Errorf("%s seeded with expr %q, want empty (status labels are not boards)", want, c.expr)
		}
	}
	// The labels must be present, with non-empty descriptions and empty expr.
	for _, want := range wantStatus {
		l, err := s.LabelShow(want)
		if err != nil {
			t.Fatalf("EnsureVocabulary did not seed %s: %v", want, err)
		}
		if l.Description == "" {
			t.Errorf("%s seeded without a description", want)
		}
		if l.Expr != "" {
			t.Errorf("%s is a stored/namespace label, seeded with expr %q", want, l.Expr)
		}
	}
}

// TestEnsureVocabularyReturnsBoards asserts EnsureVocabulary returns the
// board labels (Expr != "") this capability owns, in the documented order,
// and never returns a stored/namespace label.
func TestEnsureVocabularyReturnsBoards(t *testing.T) {
	s := newTestStore(t)
	boards, err := EnsureVocabulary(s, "ATM", "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range boards {
		if b.Expr == "" {
			t.Errorf("returned non-board label %s", b.Name)
		}
		names = append(names, b.Name)
	}
	want := []string{"ATM:backlog", "ATM:open-tasks", "ATM:in-progress-tasks", "ATM:all-tasks"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("boards = %v, want %v", names, want)
	}
}

func TestEnsureVocabularyPreservesHumanAllTasksDescription(t *testing.T) {
	// Extends the never-overwrite contract to all-tasks: a human-curated
	// all-tasks description survives a re-ensure, exactly as open-tasks and
	// backlog do (TestEnsureVocabularyDoesNotOverwriteHumanDescription /
	// TestEnsureVocabularyPreservesHumanBacklogDescription).
	s := newTestStore(t)
	humanDesc := "curated by a human"
	if err := s.LabelAdd(BoardAllTasks("ATM"), humanDesc, "*", "admin@cli:unset"); err != nil {
		t.Fatalf("seed human label: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardAllTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Description != humanDesc {
		t.Errorf("all-tasks description = %q, want %q (human curation must survive ensure)", l.Description, humanDesc)
	}
}
