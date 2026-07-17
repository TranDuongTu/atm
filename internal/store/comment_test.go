package store

import (
	"atm/internal/core"
	"testing"
)

func TestCreateCommentRequiresBodyAndActor(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	if _, err := s.CreateComment(tk.ID, "", nil, "", testActor); !core.IsUsage(err) {
		t.Fatalf("empty body should be core.ErrUsage, got %v", err)
	}
	if _, err := s.CreateComment(tk.ID, "x", nil, "", ""); !core.IsUsage(err) {
		t.Fatalf("empty actor should be core.ErrUsage, got %v", err)
	}
}

func TestGetCommentReturnsCreated(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := s.CreateComment(tk.ID, "hello", []string{"ATM:comment:open-question"}, "", testActor)
	got, err := s.GetComment(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "hello" || len(got.Labels) != 1 || got.Labels[0] != "ATM:comment:open-question" {
		t.Fatalf("got = %+v", got)
	}
	if got.Ordinal != c.Ordinal {
		t.Fatalf("Ordinal mismatch: got %d want %d", got.Ordinal, c.Ordinal)
	}
}

func TestGetCommentMalformedID(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if _, err := s.GetComment("ATM-0001"); !core.IsUsage(err) {
		t.Fatalf("malformed comment id should be core.ErrUsage, got %v", err)
	}
}

func TestGetCommentLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := s.CreateComment(tk.ID, "persist", nil, "", testActor)
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM comments WHERE id = ?`, c.ID)
	got, err := s.GetComment(c.ID)
	if err != nil {
		t.Fatalf("GetComment after cache delete: %v", err)
	}
	if got.Body != "persist" {
		t.Fatalf("rebuilt comment body = %q want %q", got.Body, "persist")
	}
	if _, ok, _ := cacheGetComment(db, c.ID); !ok {
		t.Fatal("cache row was not rewritten after lazy miss")
	}
}
func TestListCommentsSortedAndFilteredByTask(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t1", "", nil, testActor)
	tk2, _ := s.CreateTask("ATM", "t2", "", nil, testActor)
	c1, _ := s.CreateComment(tk.ID, "a", nil, "", testActor)
	_, _ = s.CreateComment(tk2.ID, "on other", nil, "", testActor)
	c2, _ := s.CreateComment(tk.ID, "c", nil, "", testActor)
	got, err := s.ListComments(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 comments on tk, got %d", len(got))
	}
	// Thread order is CREATION order (the per-task fold ordinal), not id order:
	// a v2 comment alias is a content hash, so id-asc would render the thread in
	// hash order. c1 was created before c2, so it must come first.
	if got[0].ID != c1.ID || got[1].ID != c2.ID {
		t.Fatalf("comments not in creation order: got %s, %s; want %s, %s", got[0].ID, got[1].ID, c1.ID, c2.ID)
	}
	for _, c := range got {
		if c.TaskID != tk.ID {
			t.Fatalf("comment from other task in list: %+v", c)
		}
	}
}

func TestListCommentsEmpty(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	got, err := s.ListComments(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil && len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
}
func TestSetCommentBodyAppendsAndUpdates(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := s.CreateComment(tk.ID, "original", nil, "", testActor)
	before, _ := s.LastLogSeq("ATM")
	if err := s.SetCommentBody(c.ID, "edited", testActor); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("seq jumped %d → %d, want %d (comment.body-changed)", before, after, before+1)
	}
	got, _ := s.GetComment(c.ID)
	if got.Body != "edited" {
		t.Fatalf("body = %q want edited", got.Body)
	}
	if got.UpdatedBy != testActor {
		t.Fatalf("updated_by = %q want ttran", got.UpdatedBy)
	}
	hv := s.History("ATM", Subject{Kind: "comment", ID: c.ID})
	if len(hv) != 2 || hv[1].Action != ActionCommentBodyChanged {
		t.Fatalf("history = %+v", hv)
	}
}

func TestCommentLabelAddAutoRegistersAndAppends(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := s.CreateComment(tk.ID, "body", nil, "", testActor)
	before, _ := s.LastLogSeq("ATM")
	if err := s.CommentLabelAdd(c.ID, "ATM:comment:clarification", testActor); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+2 {
		t.Fatalf("seq jumped %d → %d, want %d (label.upserted + comment.label-added)", before, after, before+2)
	}
	if _, err := s.LabelShow("ATM:comment:clarification"); err != nil {
		t.Fatalf("label not auto-registered: %v", err)
	}
}

func TestCommentLabelAddDedup(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := s.CreateComment(tk.ID, "body", []string{"ATM:comment:open-question"}, "", testActor)
	before, _ := s.LastLogSeq("ATM")
	_ = s.CommentLabelAdd(c.ID, "ATM:comment:open-question", testActor)
	after, _ := s.LastLogSeq("ATM")
	if after != before {
		t.Fatalf("dup label add should append nothing, got %d → %d", before, after)
	}
}

func TestCommentLabelRemoveDoesNotTouchRegistry(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := s.CreateComment(tk.ID, "body", []string{"ATM:comment:open-question"}, "", testActor)
	before, _ := s.LastLogSeq("ATM")
	if err := s.CommentLabelRemove(c.ID, "ATM:comment:open-question", testActor); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("seq jumped %d → %d, want %d (comment.label-removed)", before, after, before+1)
	}
	if _, err := s.LabelShow("ATM:comment:open-question"); err != nil {
		t.Fatalf("registry must still contain label: %v", err)
	}
}

func TestRemoveCommentAppendsTombstoneAndDeletesCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	c, _ := s.CreateComment(tk.ID, "doomed", nil, "", testActor)
	before, _ := s.LastLogSeq("ATM")
	if err := s.RemoveComment(c.ID, testActor); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("seq jumped %d → %d, want %d (comment.removed tombstone)", before, after, before+1)
	}
	if _, err := s.GetComment(c.ID); !core.IsNotFound(err) {
		t.Fatalf("GetComment after remove: %v want core.ErrNotFound", err)
	}
	db, _ := s.cacheDB()
	if _, ok, _ := cacheGetComment(db, c.ID); ok {
		t.Fatal("cache row must be deleted")
	}
	hv := s.History("ATM", Subject{Kind: "comment", ID: c.ID})
	if len(hv) == 0 || hv[len(hv)-1].Action != ActionCommentRemoved {
		t.Fatalf("tombstone missing from history: %+v", hv)
	}
}
