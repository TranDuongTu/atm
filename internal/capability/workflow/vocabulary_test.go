package workflow

import (
	"path/filepath"
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
