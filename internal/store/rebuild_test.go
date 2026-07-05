package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRebuildWritesCommentCachesAndSweepsOrphans(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "hello", nil, "", "claude")
	// Hand-add an orphan comment cache file (no log entry for it).
	orphan := Comment{ID: "ATM-0001-c0099", TaskID: tk.ID, Body: "orphan"}
	orphanRaw, _ := json.Marshal(orphan)
	_ = os.MkdirAll(s.commentsDir("ATM"), 0o755)
	_ = os.WriteFile(s.commentPath("ATM-0001-c0099"), orphanRaw, 0o644)
	// Hand-delete the live comment cache.
	_ = os.Remove(s.commentPath(c.ID))
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.commentPath(c.ID)); os.IsNotExist(err) {
		t.Fatal("live comment cache not rebuilt")
	}
	if _, err := os.Stat(s.commentPath("ATM-0001-c0099")); !os.IsNotExist(err) {
		t.Fatal("orphan comment cache not swept")
	}
}
