package store

import "testing"

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
