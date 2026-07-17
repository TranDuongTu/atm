package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/store"
)

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
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
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
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestEnsureVocabularyDoesNotOverwriteHumanDescription(t *testing.T) {
	s := newTestStore(t)
	humanDesc := "curated by a human"
	if err := s.LabelAdd(BoardOpenTasks("ATM"), humanDesc, "status:open", "admin@cli:unset"); err != nil {
		t.Fatalf("seed human label: %v", err)
	}
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
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
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := s.LabelShow(BoardOpenTasks("ATM")); err != nil {
		t.Errorf("open-tasks missing after ensure without seed: %v", err)
	}
}

func TestEnsureVocabularySeedsBacklogBoard(t *testing.T) {
	s := newTestStore(t)
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
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
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
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
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
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
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
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
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
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
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
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
