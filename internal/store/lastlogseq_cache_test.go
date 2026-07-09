package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLastLogSeqCachedAfterAppend proves Fix A: after AppendLog writes the
// meta row, LastLogSeq returns the cached value WITHOUT re-reading log.jsonl.
// We verify this by deleting the log file after the append — if LastLogSeq
// still returns the right number, it read the cache, not the file.
func TestLastLogSeqCachedAfterAppend(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t"}))

	// Delete the log file. If LastLogSeq reads the file, it would return 0
	// or error. The cached meta row must still return 2.
	if err := os.Remove(s.logPath("ATM")); err != nil {
		t.Fatalf("remove log: %v", err)
	}
	last, err := s.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq error after log removed: %v", err)
	}
	if last != 2 {
		t.Fatalf("LastLogSeq = %d want 2 (from cache, log file deleted)", last)
	}
}

// TestLastLogSeqFallsBackToFileOnCacheMiss proves the cache-miss path: when
// the meta row is absent (fresh cache.db, log.jsonl present), LastLogSeq
// scans the file, returns the right number, and populates the cache so a
// second call does not re-scan.
func TestLastLogSeqFallsBackToFileOnCacheMiss(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t"}))

	// Wipe the meta row to simulate a fresh cache.db against an existing log.
	db, _ := s.cacheDB()
	if _, err := db.Exec(`DELETE FROM meta WHERE key = ?`, "last_log_seq:ATM"); err != nil {
		t.Fatalf("delete meta: %v", err)
	}

	// First call scans the file.
	last, err := s.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq (cache miss) error: %v", err)
	}
	if last != 2 {
		t.Fatalf("LastLogSeq (cache miss) = %d want 2", last)
	}

	// Now delete the log file. A second call must still return 2 from the
	// now-populated cache — proving the fallback populated it.
	if err := os.Remove(s.logPath("ATM")); err != nil {
		t.Fatalf("remove log: %v", err)
	}
	last2, err := s.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq after fallback+delete: %v", err)
	}
	if last2 != 2 {
		t.Fatalf("LastLogSeq after fallback+delete = %d want 2 (cache populated by fallback)", last2)
	}
}

// TestLastLogSeqReplayPopulatesMeta proves that after a full Replay (the
// cache.db-wiped recovery path), the meta row is set so subsequent
// LastLogSeq calls are O(1).
func TestLastLogSeqReplayPopulatesMeta(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskTitleChanged, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t2"}))

	// Wipe the meta row.
	db, _ := s.cacheDB()
	if _, err := db.Exec(`DELETE FROM meta WHERE key = ?`, "last_log_seq:ATM"); err != nil {
		t.Fatalf("delete meta: %v", err)
	}
	// Replay rebuilds the cache from the log.
	if _, err := s.Replay("ATM"); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	// Delete the log; LastLogSeq must now come from the meta row Replay set.
	if err := os.Remove(s.logPath("ATM")); err != nil {
		t.Fatalf("remove log: %v", err)
	}
	last, err := s.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq after Replay: %v", err)
	}
	if last != 3 {
		t.Fatalf("LastLogSeq after Replay = %d want 3", last)
	}
}

// TestLastLogSeqFreshProjectReturnsZero proves a project with no log and no
// meta row returns 0, not an error.
func TestLastLogSeqFreshProjectReturnsZero(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	last, err := s.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq fresh project: %v", err)
	}
	if last != 0 {
		t.Fatalf("LastLogSeq fresh project = %d want 0", last)
	}
	// And the meta row is now populated with 0 so a second call is cached.
	last2, _ := s.LastLogSeq("ATM")
	if last2 != 0 {
		t.Fatalf("LastLogSeq second call = %d want 0", last2)
	}
}

// TestLastLogSeqExternalAppendBumpsMeta proves that an append through the
// store API on a project that already had a meta row bumps it (not just
// inserts a duplicate).
func TestLastLogSeqExternalAppendBumpsMeta(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskTitleChanged, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t2"}))
	if last, _ := s.LastLogSeq("ATM"); last != 3 {
		t.Fatalf("before 4th append LastLogSeq = %d want 3", last)
	}
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskTitleChanged, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t3"}))
	if last, _ := s.LastLogSeq("ATM"); last != 4 {
		t.Fatalf("after 4th append LastLogSeq = %d want 4", last)
	}
	// Delete log to prove the value is cached, not scanned.
	_ = os.Remove(s.logPath("ATM"))
	if last, _ := s.LastLogSeq("ATM"); last != 4 {
		t.Fatalf("after delete LastLogSeq = %d want 4 (cached)", last)
	}
}

// Ensure the meta table path helper is exercised once with a non-default
// store root, to catch any path-derivation bug.
func TestLastLogSeqNonDefaultStoreRoot(t *testing.T) {
	root := t.TempDir()
	s, err := Open(filepath.Join(root, "deep", "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x"}))
	if last, _ := s.LastLogSeq("ATM"); last != 1 {
		t.Fatalf("nested root LastLogSeq = %d want 1", last)
	}
}