package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestAppendLogRejectsUnknownCommentAction(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, err := s.appendLog("ATM", LogEntry{At: Now(), Actor: "a", Action: "comment.bogus", Subject: Subject{Kind: "comment", ID: "ATM-0001-c0001"}})
	if !IsUsage(err) {
		t.Fatalf("expected ErrUsage for unknown comment action, got %v", err)
	}
}

func init() {
	_ = json.RawMessage(nil)
}
