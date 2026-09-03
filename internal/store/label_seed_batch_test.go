package store

import (
	"errors"
	"testing"

	"atm/internal/core"
)

func TestLabelSeedBatch(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme Task Manager", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	task, err := s.CreateTask("ATM", "real title", "", nil, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Dirty batch: creates both labels with LabelSeed's exact semantics.
	batch := []core.Label{
		{Name: "ATM:status:open", Description: "open work"},
		{Name: "ATM:open-tasks", Description: "open board", Expr: "status:open"},
	}
	if err := s.LabelSeedBatch(batch, testActor); err != nil {
		t.Fatalf("dirty batch: %v", err)
	}
	board, err := s.LabelShow("ATM:open-tasks")
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if board.Description != "open board" || board.Expr != "status:open" {
		t.Errorf("board = %+v, want seeded description and expr", board)
	}

	// No-op batch: the transaction stays clean, so the cache is not
	// rewritten — a planted canary row survives (ATM-d402aa gate).
	db, err := s.cacheDB()
	if err != nil {
		t.Fatalf("cacheDB: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET title = 'CANARY' WHERE id = ?`, task.ID); err != nil {
		t.Fatalf("plant canary: %v", err)
	}
	if err := s.LabelSeedBatch(batch, testActor); err != nil {
		t.Fatalf("no-op batch: %v", err)
	}
	got, ok, err := cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after no-op batch: ok=%v err=%v", ok, err)
	}
	if got.Title != "CANARY" {
		t.Errorf("no-op batch rewrote the cache (title %q, want CANARY)", got.Title)
	}

	// A dirty batch (changed description) reprojects: the canary is healed.
	dirty := []core.Label{{Name: "ATM:status:open", Description: "updated open work"}}
	if err := s.LabelSeedBatch(dirty, testActor); err != nil {
		t.Fatalf("re-dirty batch: %v", err)
	}
	got, ok, err = cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after dirty batch: ok=%v err=%v", ok, err)
	}
	if got.Title != "real title" {
		t.Errorf("dirty batch did not reproject (title %q, want %q)", got.Title, "real title")
	}

	// Mixed-project batches are rejected before any I/O.
	mixed := []core.Label{
		{Name: "ATM:status:open", Description: "x"},
		{Name: "OTHER:status:open", Description: "x"},
	}
	if err := s.LabelSeedBatch(mixed, testActor); !errors.Is(err, core.ErrUsage) {
		t.Errorf("mixed-project batch err = %v, want ErrUsage", err)
	}

	// Empty batch is a no-op success.
	if err := s.LabelSeedBatch(nil, testActor); err != nil {
		t.Errorf("empty batch err = %v, want nil", err)
	}
}
