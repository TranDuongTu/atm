package store

import "testing"

// TestRebuildThenVerifyIsFullySynced is a regression test for a bug where
// Replay() stored entities with LogSeq=0 (the value baked into the log
// payload at marshal time, before the log entry's real seq was assigned).
// Rebuild() sourced its cache.db rows straight from Replay(), so every row
// it wrote got LogSeq=0 — even though the log had many entries — causing
// `atm store verify` to report a freshly rebuilt cache as fully diverged.
func TestRebuildThenVerifyIsFullySynced(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "t", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	// Update the task a couple of times so its LogSeq must reflect the LAST
	// event, not the first.
	if err := s.SetTitle(tk.ID, "t2", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(tk.ID, "hello", nil, "", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	report, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if report.Diverged {
		t.Fatalf("freshly rebuilt cache reported as diverged: %+v", report)
	}
	for _, c := range report.Caches {
		if c.Status != "ok" {
			t.Fatalf("cache check %+v want status ok", c)
		}
	}
}

// TestRebuildReportCountsTombstonedTasks pins the pre-carve output parity of
// the `atm store rebuild` report: rep.Tasks counts every task ever created,
// tombstoned included (the fold map's len), not just the live set. The count
// is printed in both text and JSON, so a store that ever removed a task must
// still report the tombstone-inclusive number byte-for-byte.
func TestRebuildReportCountsTombstonedTasks(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "keep-one", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "keep-two", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	drop, err := s.CreateTask("ATM", "drop", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	// Remove one of the three tasks; the fold keeps its tombstoned state in
	// the task map, so the pre-carve report counted it.
	if err := s.RemoveTask(drop.ID, testActor); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tasks != 3 {
		t.Fatalf("rep.Tasks = %d, want 3 (tombstone-inclusive: 3 created, 1 removed)", rep.Tasks)
	}
}

func TestRebuildWritesCommentCachesAndSweepsOrphans(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := s.CreateComment(tk.ID, "hello", nil, "", testActor)
	db, _ := s.cacheDB()
	// Hand-insert an orphan comment row (no log entry for it).
	_, _ = db.Exec(`INSERT INTO comments (id, task_id, reply_to, body, log_seq, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, '', 'orphan', 0, '2026-01-01T00:00:00Z', 'x', '2026-01-01T00:00:00Z', 'x')`,
		"ATM-0001-c0099", tk.ID)
	// Hand-delete the live comment row.
	_, _ = db.Exec(`DELETE FROM comments WHERE id = ?`, c.ID)
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cacheGetComment(db, c.ID); !ok {
		t.Fatal("live comment cache not rebuilt")
	}
	if _, ok, _ := cacheGetComment(db, "ATM-0001-c0099"); ok {
		t.Fatal("orphan comment cache not swept")
	}
}
