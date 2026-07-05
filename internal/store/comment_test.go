package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCreateCommentAssignsPerTaskCounter(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c1, err := s.CreateComment(tk.ID, "first", nil, "", "agent")
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := s.CreateComment(tk.ID, "second", nil, "", "agent")
	if c1.ID != "ATM-0001-c0001" || c2.ID != "ATM-0001-c0002" {
		t.Fatalf("ids = %s, %s", c1.ID, c2.ID)
	}
	got, _ := s.GetTask(tk.ID)
	if got.NextCommentN != 2 {
		t.Fatalf("NextCommentN = %d want 2", got.NextCommentN)
	}
}

func TestCreateCommentAppendsLogEntriesInOrder(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	before, _ := s.LastLogSeq("ATM")
	_, _ = s.CreateComment(tk.ID, "first", []string{"ATM:comment:open-question"}, "", "claude")
	after, _ := s.LastLogSeq("ATM")
	// 1 label.upserted + 1 comment.created + 1 task.meta-changed = 3 entries.
	if after != before+3 {
		t.Fatalf("seq jumped %d → %d, want %d (label+comment+meta)", before, after, before+3)
	}
	entries, _ := s.ReadLog("ATM")
	var actions []string
	for _, e := range entries {
		if e.Seq > before {
			actions = append(actions, e.Action)
		}
	}
	want := []string{ActionLabelUpserted, ActionCommentCreated, ActionTaskMetaChanged}
	if len(actions) != 3 || actions[0] != want[0] || actions[1] != want[1] || actions[2] != want[2] {
		t.Fatalf("action order = %v want %v", actions, want)
	}
}

func TestCreateCommentReplyToSameTaskValidated(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c1, _ := s.CreateComment(tk.ID, "first", nil, "", "claude")
	// Same task: ok
	c2, err := s.CreateComment(tk.ID, "reply", nil, c1.ID, "claude")
	if err != nil {
		t.Fatalf("same-task reply should be ok: %v", err)
	}
	if c2.ReplyTo != c1.ID {
		t.Fatalf("ReplyTo = %q want %q", c2.ReplyTo, c1.ID)
	}
	// Cross-task comment ID: reject
	tk2, _ := s.CreateTask("ATM", "other", "", nil, "claude")
	other1, _ := s.CreateComment(tk2.ID, "on other", nil, "", "claude")
	if _, err := s.CreateComment(tk.ID, "bad reply", nil, other1.ID, "claude"); !IsUsage(err) {
		t.Fatalf("cross-task ReplyTo should be ErrUsage, got %v", err)
	}
	// Malformed ReplyTo: reject
	if _, err := s.CreateComment(tk.ID, "bad", nil, "c0001", "claude"); !IsUsage(err) {
		t.Fatalf("malformed ReplyTo should be ErrUsage, got %v", err)
	}
	// Non-existent parent ID (no orphan check): ok — dangling pointer tolerated
	if _, err := s.CreateComment(tk.ID, "ok dangling", nil, "ATM-0001-c0099", "claude"); err != nil {
		t.Fatalf("non-existent ReplyTo should be allowed (no orphan check): %v", err)
	}
}

func TestCreateCommentRequiresBodyAndActor(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	if _, err := s.CreateComment(tk.ID, "", nil, "", "claude"); !IsUsage(err) {
		t.Fatalf("empty body should be ErrUsage, got %v", err)
	}
	if _, err := s.CreateComment(tk.ID, "x", nil, "", ""); !IsUsage(err) {
		t.Fatalf("empty actor should be ErrUsage, got %v", err)
	}
}

func TestGetCommentReturnsCreated(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "hello", []string{"ATM:comment:open-question"}, "", "claude")
	got, err := s.GetComment(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "hello" || len(got.Labels) != 1 || got.Labels[0] != "ATM:comment:open-question" {
		t.Fatalf("got = %+v", got)
	}
	if got.LogSeq != c.LogSeq {
		t.Fatalf("LogSeq mismatch: got %d want %d", got.LogSeq, c.LogSeq)
	}
}

func TestGetCommentMalformedID(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	if _, err := s.GetComment("ATM-0001"); !IsUsage(err) {
		t.Fatalf("malformed comment id should be ErrUsage, got %v", err)
	}
}

func TestGetCommentLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "persist", nil, "", "claude")
	// Hand-delete cache; next read must rebuild.
	_ = os.Remove(s.commentPath(c.ID))
	got, err := s.GetComment(c.ID)
	if err != nil {
		t.Fatalf("GetComment after cache delete: %v", err)
	}
	if got.Body != "persist" {
		t.Fatalf("rebuilt comment body = %q want %q", got.Body, "persist")
	}
	if _, err := os.Stat(s.commentPath(c.ID)); os.IsNotExist(err) {
		t.Fatal("cache file was not rewritten after lazy miss")
	}
}

func TestGetCommentFutureLogSeqIntegrity(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "x", nil, "", "claude")
	c.LogSeq = 9999
	newRaw, _ := json.Marshal(c)
	_ = os.WriteFile(s.commentPath(c.ID), newRaw, 0o644)
	_, err := s.GetComment(c.ID)
	if !IsIntegrity(err) {
		t.Fatalf("expected ErrIntegrity, got %v", err)
	}
}

func TestListCommentsSortedAndFilteredByTask(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t1", "", nil, "claude")
	tk2, _ := s.CreateTask("ATM", "t2", "", nil, "claude")
	_, _ = s.CreateComment(tk.ID, "a", nil, "", "claude")
	_, _ = s.CreateComment(tk2.ID, "on other", nil, "", "claude")
	_, _ = s.CreateComment(tk.ID, "c", nil, "", "claude")
	got, err := s.ListComments(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 comments on tk, got %d", len(got))
	}
	if got[0].ID >= got[1].ID {
		t.Fatalf("comments not sorted ascending: %s, %s", got[0].ID, got[1].ID)
	}
	for _, c := range got {
		if c.TaskID != tk.ID {
			t.Fatalf("comment from other task in list: %+v", c)
		}
	}
}

func TestListCommentsEmpty(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	got, err := s.ListComments(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil && len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
}

func TestParseReplayNextCommentNFromMetaChanged(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = s.CreateComment(tk.ID, "first", nil, "", "claude")
	// Delete the task cache and let it rebuild from log; counter must come back.
	_ = os.Remove(s.taskPath(tk.ID))
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NextCommentN != 1 {
		t.Fatalf("replay-derived NextCommentN = %d want 1", got.NextCommentN)
	}
}
