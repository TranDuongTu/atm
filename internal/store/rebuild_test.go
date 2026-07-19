package store

import (
	"database/sql"
	"testing"
)

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

// TestRebuildWipesV2FreshnessInSameTx reproduces ATM-5b1db5: Rebuild() wiped
// the six row tables in one transaction but left the per-project
// last_v2_event_count:<code> meta freshness rows in place. If the process
// crashed between that wipe-commit and reprojectAllV2's projection, a later
// reader saw freshness rows asserting "fresh over empty tables":
// ensureV2CacheFresh's probe (cacheGetV2Freshness + eng.ChangeCount) matched
// the still-present freshness count against the unchanged event file, returned
// fresh, and skipped self-heal — so the cache stayed empty despite intact v2
// logs on disk. ListTasksErr, which goes through the freshness gate, returned
// an empty slice with a nil error.
//
// The fix is to delete the freshness keys in the SAME transaction as the row
// wipe, so the post-wipe state never coexists with stale freshness. This test
// drives that wipe helper directly — the exact code path Rebuild's first tx
// runs — and asserts both row tables AND freshness rows are gone, so a crash
// any time after the wipe-commit leaves no stale freshness row to fool
// ensureV2CacheFresh. It then confirms ensureV2CacheFresh self-heals the
// empty cache rather than falsely reporting fresh.
func TestRebuildWipesV2FreshnessInSameTx(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "t", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	db, err := s.cacheDB()
	if err != nil {
		t.Fatal(err)
	}

	// Precondition: the projected cache has the task and a freshness row.
	tasks, err := s.ListTasksErr(QueryFilters{Project: "ATM"})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("precondition: ListTasksErr = %d tasks, err=%v", len(tasks), err)
	}
	gotCount, ok, err := cacheGetV2Freshness(db, "ATM")
	if err != nil || !ok || gotCount == 0 {
		t.Fatalf("precondition: freshness = (%d, %v), err=%v", gotCount, ok, err)
	}

	// Run ONLY the wipe half of Rebuild — the exact state a crash between
	// the wipe-commit and reprojectAllV2 would leave behind. This is the
	// code path the fix touches; running it in isolation lets us observe
	// the post-wipe state directly, which Rebuild's completed call hides
	// by re-projecting over it.
	if err := s.wipeCacheForRebuild(db); err != nil {
		t.Fatalf("wipeCacheForRebuild: %v", err)
	}

	// Row tables must be empty.
	if n := countRows(t, db, "tasks"); n != 0 {
		t.Fatalf("tasks table not wiped: %d rows", n)
	}
	if n := countRows(t, db, "projects"); n != 0 {
		t.Fatalf("projects table not wiped: %d rows", n)
	}

	// The freshness row MUST be gone — the crux of the fix. Before the
	// fix, the wipe tx touched only the six row tables and left meta
	// untouched, so this row survived and fooled ensureV2CacheFresh.
	if _, ok, err := cacheGetV2Freshness(db, "ATM"); err != nil || ok {
		t.Fatalf("freshness row survived wipe: ok=%v, err=%v (must be deleted in same tx as row wipe)", ok, err)
	}

	// And the user-visible payoff: with freshness gone, a list read's
	// ensureV2CacheFresh probe sees "never projected from a v2 file",
	// rebuilds the project, and returns the live task — instead of the
	// pre-fix behavior of returning 0 tasks with nil error.
	tasks, err = s.ListTasksErr(QueryFilters{Project: "ATM"})
	if err != nil {
		t.Fatalf("post-wipe ListTasksErr: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("post-wipe: ensureV2CacheFresh did not self-heal empty cache; got %d tasks, want 1", len(tasks))
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
