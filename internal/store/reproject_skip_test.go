package store

import "testing"

// TestNoopLabelSeedSkipsReprojection pins the fix for ATM-d402aa: a LabelSeed
// of an already-live label (the TUI's per-select EnsureVocabulary path) must
// not rewrite the project's cache rows. The canary is a row mutated directly
// in cache.db: a reprojection would restore it from the fold, a skipped
// reprojection leaves it alone.
func TestNoopLabelSeedSkipsReprojection(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme Task Manager", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	task, err := s.CreateTask("ATM", "real title", "", nil, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.LabelSeed("ATM:open-tasks", "open work", "status:open", testActor); err != nil {
		t.Fatalf("first LabelSeed: %v", err)
	}

	db, err := s.cacheDB()
	if err != nil {
		t.Fatalf("cacheDB: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET title = 'CANARY' WHERE id = ?`, task.ID); err != nil {
		t.Fatalf("plant canary: %v", err)
	}

	// No-op seed: the label is live, so the txn is clean and the canary
	// must survive (no cache rewrite).
	if err := s.LabelSeed("ATM:open-tasks", "different desc", "", testActor); err != nil {
		t.Fatalf("no-op LabelSeed: %v", err)
	}
	got, ok, err := cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after no-op seed: ok=%v err=%v", ok, err)
	}
	if got.Title != "CANARY" {
		t.Fatalf("no-op LabelSeed rewrote the cache (title %q, want CANARY)", got.Title)
	}

	// Dirty seed: a NEW label appends, so reprojection must run and restore
	// the canary row from the fold.
	if err := s.LabelSeed("ATM:in-progress-tasks", "wip", "status:in-progress", testActor); err != nil {
		t.Fatalf("dirty LabelSeed: %v", err)
	}
	got, ok, err = cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after dirty seed: ok=%v err=%v", ok, err)
	}
	if got.Title != "real title" {
		t.Fatalf("dirty LabelSeed did not reproject (title %q, want %q)", got.Title, "real title")
	}
}
