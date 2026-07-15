package store

import (
	"fmt"
	"os"
	"testing"
)

func TestAppendLogMonotoneSeq(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	e1, err := s.appendLog("ATM", LogEntry{At: Now(), Actor: "a", Action: ActionProjectCreated, Subject: Subject{Kind: "project", Code: "ATM"}})
	if err != nil {
		t.Fatal(err)
	}
	if e1.Seq != 1 {
		t.Fatalf("first seq = %d want 1", e1.Seq)
	}
	e2, _ := s.appendLog("ATM", LogEntry{At: Now(), Actor: "a", Action: ActionProjectNameChanged, Subject: Subject{Kind: "project", Code: "ATM"}})
	if e2.Seq != 2 {
		t.Fatalf("second seq = %d want 2", e2.Seq)
	}
}

func TestAppendLogRejectsUnknownAction(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, err := s.appendLog("ATM", LogEntry{At: Now(), Actor: "a", Action: "bogus.action", Subject: Subject{Kind: "project", Code: "ATM"}})
	if !IsUsage(err) {
		t.Fatalf("expected ErrUsage for unknown action, got %v", err)
	}
	last, _ := s.LastLogSeq("ATM")
	if last != 0 {
		t.Fatalf("no line should have been appended; LastLogSeq = %d", last)
	}
}

func TestReadLogTruncatesMalformedTail(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.appendLog("ATM", LogEntry{At: Now(), Actor: "a", Action: ActionProjectCreated, Subject: Subject{Kind: "project", Code: "ATM"}})
	// Append garbage bytes simulating a crash mid-write.
	p := s.logPath("ATM")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("{\"seq\":2,\"at\":\"2026-07-04T12:00:00Z\",\"actor\":\"a\",\"action\":\"project.name-changed\",\"subje") // truncated
	_ = f.Close()
	entries, err := s.ReadLog("ATM")
	if err == nil {
		t.Fatal("expected ErrIntegrity from partial line, got nil")
	}
	if !IsIntegrity(err) {
		t.Fatalf("expected ErrIntegrity, got %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d valid entries, want 1 (the truncated line dropped)", len(entries))
	}
}

func TestIsIntegrity(t *testing.T) {
	if !IsIntegrity(fmt.Errorf("%w: x", ErrIntegrity)) {
		t.Fatal("IsIntegrity should match wrapped ErrIntegrity")
	}
}

