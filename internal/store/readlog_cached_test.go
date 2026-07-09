package store

import (
	"os"
	"sync"
	"testing"
)

// TestReadLogCachedServesFromMemory proves Fix B: a second call to
// ReadLogCached returns the same entries WITHOUT re-reading the file. We
// verify this by deleting the log file after the first call — if the second
// call still returns the entries, it read the in-memory snapshot.
func TestReadLogCachedServesFromMemory(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t"}))

	first, err := s.ReadLogCached("ATM")
	if err != nil {
		t.Fatalf("ReadLogCached first: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("first call entries = %d want 2", len(first))
	}

	// Delete the log file. A file-reading implementation would return
	// (nil, nil) or error; the cached one must still return 2 entries.
	if err := os.Remove(s.logPath("ATM")); err != nil {
		t.Fatalf("remove log: %v", err)
	}
	second, err := s.ReadLogCached("ATM")
	if err != nil {
		t.Fatalf("ReadLogCached second (after delete): %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("second call entries = %d want 2 (from memory snapshot)", len(second))
	}
}

// TestReadLogCachedInvalidatesOnNewAppend proves the snapshot is invalidated
// when a new append bumps the last log seq: a second call after an append
// returns the new entry too, not the stale snapshot.
func TestReadLogCachedInvalidatesOnNewAppend(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x"}))
	first, _ := s.ReadLogCached("ATM")
	if len(first) != 1 {
		t.Fatalf("first call entries = %d want 1", len(first))
	}
	// New append bumps last_log_seq (cache.db row) — snapshot must invalidate.
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t"}))
	second, _ := s.ReadLogCached("ATM")
	if len(second) != 2 {
		t.Fatalf("after append entries = %d want 2 (snapshot invalidated)", len(second))
	}
}

// TestReadLogCachedConcurrentSafe proves two goroutines calling ReadLogCached
// on the same project don't race (the snapshot is mutex-guarded).
func TestReadLogCachedConcurrentSafe(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t"}))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			es, err := s.ReadLogCached("ATM")
			if err != nil {
				t.Errorf("ReadLogCached: %v", err)
				return
			}
			if len(es) != 2 {
				t.Errorf("entries = %d want 2", len(es))
			}
		}()
	}
	wg.Wait()
}

// TestReadLogCachedFreshProjectReturnsEmpty proves a project with no log
// returns (nil, nil) and caches that empty result.
func TestReadLogCachedFreshProjectReturnsEmpty(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	es, err := s.ReadLogCached("ATM")
	if err != nil {
		t.Fatalf("ReadLogCached fresh: %v", err)
	}
	if es != nil && len(es) != 0 {
		t.Fatalf("fresh project entries = %v want nil/empty", es)
	}
	// Delete the (nonexistent) log dir; second call must still be empty.
	_ = os.RemoveAll(s.projectDir("ATM"))
	es2, err := s.ReadLogCached("ATM")
	if err != nil {
		t.Fatalf("ReadLogCached second: %v", err)
	}
	if es2 != nil && len(es2) != 0 {
		t.Fatalf("second call entries = %v want nil/empty (cached)", es2)
	}
}

// TestReadLogCachedExternalAppendDetected proves that an append done by
// another process (simulated by directly bumping the cache.db meta row + the
// log file) is detected via the O(1) LastLogSeq check, so the snapshot
// re-scans. This is the cross-process freshness guarantee.
func TestReadLogCachedExternalAppendDetected(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x"}))
	first, _ := s.ReadLogCached("ATM")
	if len(first) != 1 {
		t.Fatalf("first call entries = %d want 1", len(first))
	}
	// Simulate another process appending to the log + bumping the meta row
	// directly (bypassing this Store's in-memory state).
	line := []byte(`{"seq":2,"at":"2026-07-09T12:00:00Z","actor":"ext","action":"task.created","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","title":"ext"}}` + "\n")
	f, _ := os.OpenFile(s.logPath("ATM"), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.Write(line)
	_ = f.Close()
	db, _ := s.cacheDB()
	_ = cacheSetLastLogSeq(db, "ATM", 2)

	second, _ := s.ReadLogCached("ATM")
	if len(second) != 2 {
		t.Fatalf("after external append entries = %d want 2 (snapshot invalidated by LastLogSeq)", len(second))
	}
}