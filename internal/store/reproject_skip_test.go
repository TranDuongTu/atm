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

	// Unchanged seed: the label already has this description, so the txn is
	// clean and the canary must survive (no cache rewrite).
	if err := s.LabelSeed("ATM:open-tasks", "open work", "", testActor); err != nil {
		t.Fatalf("unchanged LabelSeed: %v", err)
	}
	got, ok, err := cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after unchanged seed: ok=%v err=%v", ok, err)
	}
	if got.Title != "CANARY" {
		t.Fatalf("unchanged LabelSeed rewrote the cache (title %q, want CANARY)", got.Title)
	}

	// Changed-description seed: the label is live, but the vocabulary
	// description changed, so reprojection must run and restore the canary row.
	if err := s.LabelSeed("ATM:open-tasks", "updated open work", "", testActor); err != nil {
		t.Fatalf("changed-description LabelSeed: %v", err)
	}
	got, ok, err = cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after changed-description seed: ok=%v err=%v", ok, err)
	}
	if got.Title != "real title" {
		t.Fatalf("changed-description LabelSeed did not reproject (title %q, want %q)", got.Title, "real title")
	}
}
