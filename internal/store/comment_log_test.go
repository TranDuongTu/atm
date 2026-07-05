package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReplayCommentCreatedAndRemoved(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x", NextTaskN: 1}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", Labels: []string{}}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0001"}, Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "first"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0002"}, Comment{ID: "ATM-0001-c0002", TaskID: "ATM-0001", Body: "second"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentBodyChanged, Subject{Kind: "comment", ID: "ATM-0001-c0001"}, Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "edited first"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentRemoved, Subject{Kind: "comment", ID: "ATM-0001-c0002"}, Comment{ID: "ATM-0001-c0002", TaskID: "ATM-0001", Body: "second"}))
	st, err := s.Replay("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Comments) != 1 {
		t.Fatalf("expected 1 live comment, got %d", len(st.Comments))
	}
	if st.Comments[0].ID != "ATM-0001-c0001" || st.Comments[0].Body != "edited first" {
		t.Fatalf("rebuilt comment = %+v", st.Comments[0])
	}
}

func TestReplayTaskMetaChanged(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", NextTaskN: 1}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskMetaChanged, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", NextCommentN: 3}))
	st, err := s.Replay("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Tasks) != 1 || st.Tasks[0].NextCommentN != 3 {
		t.Fatalf("replay did not apply task.meta-changed: %+v", st.Tasks)
	}
}

func TestHistoryForCommentSubject(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", NextTaskN: 1}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0001"}, Comment{ID: "ATM-0001-c0001", Body: "x"}))
	hv := s.History("ATM", Subject{Kind: "comment", ID: "ATM-0001-c0001"})
	if len(hv) != 1 || hv[0].Action != ActionCommentCreated {
		t.Fatalf("history = %+v", hv)
	}
}

func TestAppendLogRejectsUnknownCommentAction(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, err := s.AppendLog("ATM", LogEntry{At: Now(), Actor: "a", Action: "comment.bogus", Subject: Subject{Kind: "comment", ID: "ATM-0001-c0001"}})
	if !IsUsage(err) {
		t.Fatalf("expected ErrUsage for unknown comment action, got %v", err)
	}
}

func TestReplayDeterministicComments(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", NextTaskN: 1}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0002"}, Comment{ID: "ATM-0001-c0002", TaskID: "ATM-0001", Body: "z"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0001"}, Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "a"}))
	st1, _ := s.Replay("ATM")
	st2, _ := s.Replay("ATM")
	if len(st1.Comments) != 2 || len(st2.Comments) != 2 {
		t.Fatalf("replay count mismatch: %d vs %d", len(st1.Comments), len(st2.Comments))
	}
	if st1.Comments[0].ID != st2.Comments[0].ID {
		t.Fatalf("non-deterministic comment sort")
	}
}

func init() {
	_ = json.RawMessage(nil)
}
