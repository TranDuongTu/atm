package eventlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func countEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	root := t.TempDir()
	e := New(root, Options{Now: func() time.Time { return time.Unix(0, 0).UTC() }})
	dir := filepath.Join(root, "projects", "ATM")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return e, e.EventsV2Path("ATM")
}

// ChangeCount must reflect the file as it stands NOW on every call from the
// same Engine instance: local appends, external rewrites, and deletion all
// invalidate whatever the engine may have memoized (the memo is keyed on the
// file's stat identity).
func TestChangeCountTracksFileMutations(t *testing.T) {
	e, path := countEngine(t)

	if n, err := e.ChangeCount("ATM"); err != nil || n != 0 {
		t.Fatalf("missing file: got n=%d err=%v, want 0, nil", n, err)
	}

	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"b\":2}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if n, err := e.ChangeCount("ATM"); err != nil || n != 2 {
		t.Fatalf("after create: got n=%d err=%v, want 2, nil", n, err)
	}

	// Append (the normal advance) — count must move on the next call.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteString("{\"c\":3}\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n, err := e.ChangeCount("ATM"); err != nil || n != 3 {
		t.Fatalf("after append: got n=%d err=%v, want 3, nil", n, err)
	}

	// Unterminated tail is uncommitted and must not count.
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.WriteString("{\"d\":4}"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n, err := e.ChangeCount("ATM"); err != nil || n != 3 {
		t.Fatalf("unterminated tail: got n=%d err=%v, want 3, nil", n, err)
	}

	// External rewrite to a smaller file (prune/upgrade replaces media).
	if err := os.WriteFile(path, []byte("{\"a\":1}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if n, err := e.ChangeCount("ATM"); err != nil || n != 1 {
		t.Fatalf("after rewrite: got n=%d err=%v, want 1, nil", n, err)
	}

	// Deletion returns to zero, not a stale memo.
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if n, err := e.ChangeCount("ATM"); err != nil || n != 0 {
		t.Fatalf("after remove: got n=%d err=%v, want 0, nil", n, err)
	}
}

// Two projects must not share a memo slot.
func TestChangeCountPerProject(t *testing.T) {
	e, path := countEngine(t)
	other := e.EventsV2Path("INFRA")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"a\":1}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(other, []byte("{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if n, _ := e.ChangeCount("ATM"); n != 1 {
		t.Fatalf("ATM: got %d, want 1", n)
	}
	if n, _ := e.ChangeCount("INFRA"); n != 3 {
		t.Fatalf("INFRA: got %d, want 3", n)
	}
	if n, _ := e.ChangeCount("ATM"); n != 1 {
		t.Fatalf("ATM re-read: got %d, want 1", n)
	}
}

// BenchmarkChangeCountFresh measures the repeated-probe path the TUI hits:
// the file has not changed between calls, so the stat-keyed memo should make
// this O(stat), not O(file).
func BenchmarkChangeCountFresh(b *testing.B) {
	root := b.TempDir()
	e := New(root, Options{Now: func() time.Time { return time.Unix(0, 0).UTC() }})
	dir := filepath.Join(root, "projects", "ATM")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatalf("MkdirAll: %v", err)
	}
	line := []byte("{\"kind\":\"task.created\",\"payload\":{\"title\":\"a benchmark event line of realistic width for the ATM event log\"}}\n")
	var buf []byte
	for i := 0; i < 2400; i++ {
		buf = append(buf, line...)
	}
	if err := os.WriteFile(e.EventsV2Path("ATM"), buf, 0o644); err != nil {
		b.Fatalf("WriteFile: %v", err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := e.ChangeCount("ATM"); err != nil {
			b.Fatalf("ChangeCount: %v", err)
		}
	}
}
