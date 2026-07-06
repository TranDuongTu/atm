package store

import (
	"os"
	"testing"
)

func TestVerifyCleanStore(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_ = tk
	report, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if report.Diverged {
		t.Fatalf("clean store reports Diverged=true: %+v", report)
	}
	if !report.LogOK {
		t.Errorf("LogOK = false on clean log")
	}
	for _, c := range report.Caches {
		if c.Status != "ok" {
			t.Errorf("cache %s:%s status = %q want ok", c.Kind, c.ID, c.Status)
		}
	}
}

func TestVerifyDetectsStaleTaskCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_ = s.SetTitle(tk.ID, "changed", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`UPDATE tasks SET log_seq = 1 WHERE id = ?`, tk.ID)
	report, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Diverged {
		t.Fatal("Diverged=false with stale cache")
	}
	found := false
	for _, c := range report.Caches {
		if c.Status == "stale" {
			found = true
		}
	}
	if !found {
		t.Errorf("no stale cache reported: %+v", report.Caches)
	}
}

func TestVerifyDetectsMissingCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM tasks WHERE id = ?`, tk.ID)
	report, _ := s.VerifyProject("ATM")
	if !report.Diverged {
		t.Fatal("Diverged=false with missing cache")
	}
}

func TestVerifyDetectsMalformedLogTail(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	// Append garbage to the log.
	f, _ := os.OpenFile(s.logPath("ATM"), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("{bad json")
	_ = f.Close()
	report, _ := s.VerifyProject("ATM")
	if report.LogOK {
		t.Errorf("LogOK=true with malformed tail")
	}
	if report.Truncated == 0 {
		t.Errorf("Truncated = 0, want > 0")
	}
}

func TestVerifyDetectsSeqGap(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	// Hand-edit the log: remove the second line by truncating to after line 1.
	data, _ := os.ReadFile(s.logPath("ATM"))
	// Keep only line 1 + the trailing newline.
	lines := splitLines(data)
	if len(lines) < 3 {
		t.Skip("not enough lines to test gap")
	}
	// Drop the second line — keep line 1, then jump to line 3 onwards
	// (seq will skip from 1 to 3).
	newData := append(lines[0], '\n')
	for _, l := range lines[2:] {
		newData = append(newData, l...)
		newData = append(newData, '\n')
	}
	_ = os.WriteFile(s.logPath("ATM"), newData, 0o644)
	report, _ := s.VerifyProject("ATM")
	// Seq gap is surfaced in report.SeqGaps (per spec: reported, never auto-repaired).
	if len(report.SeqGaps) == 0 {
		t.Fatalf("expected at least one seq gap, got %+v", report.SeqGaps)
	}
	// And the log is not OK because of the gap.
	if report.LogOK {
		t.Errorf("LogOK = true with seq gap, want false")
	}
}

func TestVerifyReportsCommentCacheStale(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "x", nil, "", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`UPDATE comments SET log_seq = 0 WHERE id = ?`, c.ID)
	rep, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ck := range rep.Caches {
		if ck.Kind == "comment" && ck.ID == c.ID {
			if ck.Status == "stale" || ck.Status == "corrupt" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("comment cache stale not reported: %+v", rep.Caches)
	}
}

func splitLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}
