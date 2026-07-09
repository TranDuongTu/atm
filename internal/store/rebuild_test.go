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
	if _, err := s.CreateProject("ATM", "x", "claude"); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "t", "", nil, "claude")
	if err != nil {
		t.Fatal(err)
	}
	// Update the task a couple of times so its LogSeq must reflect the LAST
	// event, not the first.
	if err := s.SetTitle(tk.ID, "t2", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(tk.ID, "hello", nil, "", "claude"); err != nil {
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

// TestRebuildReconstructsNextTaskNPastRemovedTask is a regression test for a
// bug where Replay() (the bulk reconstruction used by Rebuild()) derived
// NextTaskN solely from the last project.* event's payload -- which never
// reflects tasks created afterward, since CreateTask never appends a
// project.* log event. It must instead be reconstructed as one past the
// highest task-ID N ever seen among the project's task.* log entries,
// INCLUDING removed tasks' tombstones (a removed task's number must never be
// reused).
func TestRebuildReconstructsNextTaskNPastRemovedTask(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", "claude"); err != nil {
		t.Fatal(err)
	}
	var last *Task
	for i := 0; i < 3; i++ {
		tk, err := s.CreateTask("ATM", "t", "", nil, "claude")
		if err != nil {
			t.Fatal(err)
		}
		last = tk
	}
	// Remove the last (highest-numbered) task, leaving a tombstone at N=3.
	if err := s.RemoveTask(last.ID, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if got.NextTaskN != 4 {
		t.Fatalf("NextTaskN after Rebuild = %d want 4 (highest task N seen was 3, including the removed tombstone; must not reset to 1 or to live-task-count+1=3)", got.NextTaskN)
	}
	// End-to-end: the next task created after rebuild must not reuse the
	// removed task's ID (ATM-0003).
	next, err := s.CreateTask("ATM", "u", "", nil, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == last.ID {
		t.Fatalf("new task %q collided with removed task's ID %q", next.ID, last.ID)
	}
	if next.ID != "ATM-0004" {
		t.Fatalf("new task ID = %q, want ATM-0004", next.ID)
	}
}

func TestRebuildWritesCommentCachesAndSweepsOrphans(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "hello", nil, "", "claude")
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

func TestRebuildDropsVectorsKeepsInquiry(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendInquiry("ATM", "q", []string{"ATM-0001"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if models, _ := s.ListVectorModels("ATM"); len(models) != 0 {
		t.Errorf("vectors not dropped: %v", models)
	}
	if got, _ := s.ReadInquiries("ATM"); len(got) != 1 {
		t.Errorf("inquiry-log dropped: got %d, want 1 (inquiry-log is ground truth, not derived)", len(got))
	}
}
