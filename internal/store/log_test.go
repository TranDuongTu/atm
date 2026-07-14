package store

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
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
	last, _ := s.LastLogSeq("ATM")
	if last != 2 {
		t.Fatalf("LastLogSeq = %d want 2", last)
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
	// After truncation, LastLogSeq reflects committed state only.
	last, _ := s.LastLogSeq("ATM")
	if last != 1 {
		t.Fatalf("LastLogSeq after truncation = %d want 1", last)
	}
}

func TestIsIntegrity(t *testing.T) {
	if !IsIntegrity(fmt.Errorf("%w: x", ErrIntegrity)) {
		t.Fatal("IsIntegrity should match wrapped ErrIntegrity")
	}
}

func newLogEntry(seq int, action string, subj Subject, payload any) LogEntry {
	raw, _ := json.Marshal(payload)
	return LogEntry{Seq: seq, At: time.Now().UTC(), Actor: "a", Action: action, Subject: subj, Payload: raw}
}

func TestReplayDeterministicAndTombstones(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	// project.created
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x", NextTaskN: 2}))
	// task.created
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t1", Labels: []string{}}))
	// task.label-added (full Task after state with label)
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskLabelAdded, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t1", Labels: []string{"ATM:type:bug"}}))
	// task.removed (tombstone)
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskRemoved, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t1", Labels: []string{"ATM:type:bug"}}))

	st1, err := s.Replay("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if st1.Project == nil || st1.Project.Code != "ATM" {
		t.Fatalf("replay missing project: %+v", st1.Project)
	}
	if len(st1.Tasks) != 0 {
		t.Fatalf("tombstoned task must not be in live set, got %d tasks", len(st1.Tasks))
	}
	// Determinism: replay again, identical result.
	st2, _ := s.Replay("ATM")
	if len(st2.Tasks) != len(st1.Tasks) || st2.Project.Code != st1.Project.Code {
		t.Fatalf("non-deterministic replay: %+v vs %+v", st1, st2)
	}
}

func TestReplayLabelUpsertedAndRemoved(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionLabelUpserted, Subject{Kind: "label", Name: "ATM:type:bug"}, Label{Name: "ATM:type:bug", Description: "first"}))
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionLabelUpserted, Subject{Kind: "label", Name: "ATM:type:bug"}, Label{Name: "ATM:type:bug", Description: "second"}))
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionLabelRemoved, Subject{Kind: "label", Name: "ATM:type:bug"}, Label{Name: "ATM:type:bug", Description: "second"}))
	st, _ := s.Replay("ATM")
	for _, l := range st.Labels {
		if l.Name == "ATM:type:bug" {
			t.Fatalf("removed label must not be in live registry, got %+v", l)
		}
	}
}

// TestReplayStampsLogSeqFromEntrySeq is a regression test proving that
// Replay() stamps each entity's LogSeq from the log entry's own seq (as
// assigned by appendLog), not from whatever LogSeq value happened to be
// baked into the JSON payload at marshal time (which is always 0, since
// payloads are marshaled before the entry's seq is known). For a task
// updated multiple times, the final LogSeq must equal the seq of the LAST
// matching event, not the first.
func TestReplayStampsLogSeqFromEntrySeq(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x", NextTaskN: 2}))
	// task.created (payload LogSeq baked in as 0)
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t1", Labels: []string{}}))
	// task.title-changed (payload LogSeq still baked in as 0)
	e3, _ := s.appendLog("ATM", newLogEntry(0, ActionTaskTitleChanged, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t2", Labels: []string{}}))

	st, err := s.Replay("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if st.Project == nil {
		t.Fatal("replay missing project")
	}
	if st.Project.LogSeq != 1 {
		t.Fatalf("project LogSeq = %d want 1 (the project.created entry seq)", st.Project.LogSeq)
	}
	if len(st.Tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(st.Tasks))
	}
	if st.Tasks[0].LogSeq != e3.Seq {
		t.Fatalf("task LogSeq = %d want %d (the LAST matching entry seq, i.e. title-changed, not created)", st.Tasks[0].LogSeq, e3.Seq)
	}
}

// TestReplayReconstructsNextTaskNFromTaskLogEntriesNotProjectPayload pins the
// exact NextTaskN reconstruction logic in Replay() independent of the cache
// layer. The project.created payload here is intentionally stamped with a
// stale NextTaskN=2 (as if it were never updated after task creation, which
// is exactly what happens in production since CreateTask never appends a
// project.* log event). Replay() must ignore that stale payload value and
// instead derive NextTaskN from the highest task-ID N seen across ALL
// task.* log entries for the project -- including a task.removed tombstone,
// whose N must never be reused.
func TestReplayReconstructsNextTaskNFromTaskLogEntriesNotProjectPayload(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	// project.created payload carries a stale NextTaskN=2.
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x", NextTaskN: 2}))
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t1", Labels: []string{}}))
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0002"}, Task{ID: "ATM-0002", ProjectCode: "ATM", Title: "t2", Labels: []string{}}))
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0003"}, Task{ID: "ATM-0003", ProjectCode: "ATM", Title: "t3", Labels: []string{}}))
	// Remove the highest-numbered task (tombstone at N=3); its number must
	// never be reused even though it no longer appears in st.Tasks.
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskRemoved, Subject{Kind: "task", ID: "ATM-0003"}, Task{ID: "ATM-0003", ProjectCode: "ATM", Title: "t3", Labels: []string{}}))

	st, err := s.Replay("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if st.Project == nil {
		t.Fatal("replay missing project")
	}
	if st.Project.NextTaskN != 4 {
		t.Fatalf("Project.NextTaskN = %d want 4 (highest task N seen was 3 via the removed tombstone, stale payload said 2)", st.Project.NextTaskN)
	}
	if len(st.Tasks) != 2 {
		t.Fatalf("want 2 live tasks (ATM-0003 removed), got %d", len(st.Tasks))
	}
}

func TestHistoryProjection(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t"}))
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskTitleChanged, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", Title: "t2"}))
	_, _ = s.appendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0002"}, Task{ID: "ATM-0002", Title: "other"}))
	hv := s.History("ATM", Subject{Kind: "task", ID: "ATM-0001"})
	if len(hv) != 2 {
		t.Fatalf("history for ATM-0001 len = %d want 2", len(hv))
	}
	if hv[0].Action != ActionTaskCreated || hv[1].Action != ActionTaskTitleChanged {
		t.Fatalf("history actions = %q, %q", hv[0].Action, hv[1].Action)
	}
}
