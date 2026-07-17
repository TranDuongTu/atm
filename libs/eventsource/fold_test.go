package eventsource

import (
	"slices"
	"testing"
	"time"
)

// fold is a shorthand: build the DAG, fold, return state.
func fold(t *testing.T, events ...*Event) *State {
	t.Helper()
	st, err := FoldEvents(events)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestFoldLinearHistory(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0", "description": "d0", "labels": []string{"P:x"}})
	retitle := testEvent(t, c, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "v1"})
	st := fold(t, created, retitle)
	task := st.Tasks[created.ID]
	if task == nil {
		t.Fatal("task missing")
	}
	if task.Alias != "T-1" || task.Title != "v1" || task.Description != "d0" || task.Tombstoned {
		t.Errorf("task = %+v", task)
	}
	if !slices.Equal(task.Labels, []string{"P:x"}) {
		t.Errorf("labels = %v", task.Labels)
	}
	if task.CreatedBy != "developer@claude:test" || task.CreatedHLC != created.HLC || task.CreatedReplica != replicaA {
		t.Errorf("creation meta = %+v", task.EntityMeta)
	}
	if len(st.Contested) != 0 {
		t.Errorf("linear history contested: %+v", st.Contested)
	}
	if !slices.Equal(st.Frontier, []string{retitle.ID}) {
		t.Errorf("frontier = %v", st.Frontier)
	}
}

func TestFoldScalarLWWAndContested(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000) // B's stamps are later → B wins LWW
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	a := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "from A"})
	b := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "from B"})
	st := fold(t, created, a, b)
	if got := st.Tasks[created.ID].Title; got != "from B" {
		t.Errorf("LWW winner = %q, want the higher HLC", got)
	}
	if len(st.Contested) != 1 {
		t.Fatalf("contested = %+v, want exactly the title slot", st.Contested)
	}
	cs := st.Contested[0]
	if cs.Entity != created.ID || cs.Kind != SlotScalar || cs.Field != "title" {
		t.Errorf("contested slot = %+v", cs)
	}
	if !slices.Equal(cs.Writers, []string{a.ID, b.ID}) {
		t.Errorf("writers = %v, want ascending [a, b]", cs.Writers)
	}

	// A resolution write parented on BOTH contested events becomes the
	// unique maximal writer: the slot stops being contested and board
	// membership evaporates (L2-5).
	fix := testEvent(t, ca, replicaA, []string{a.ID, b.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "settled"})
	st = fold(t, created, a, b, fix)
	if st.Tasks[created.ID].Title != "settled" || len(st.Contested) != 0 {
		t.Errorf("resolution did not clear: title=%q contested=%+v", st.Tasks[created.ID].Title, st.Contested)
	}
}

func TestFoldMembershipAddWins(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t", "labels": []string{"P:x"}})
	rm := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskLabelRemoved,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"label": "P:x"})
	add := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"label": "P:x"})
	st := fold(t, created, rm, add)
	if !slices.Equal(st.Tasks[created.ID].Labels, []string{"P:x"}) {
		t.Errorf("labels = %v, want add-wins", st.Tasks[created.ID].Labels)
	}
	// A remove that OBSERVED the add wins (it dominates).
	rm2 := testEvent(t, cb, replicaB, []string{rm.ID, add.ID}, ActionTaskLabelRemoved,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"label": "P:x"})
	st = fold(t, created, rm, add, rm2)
	if len(st.Tasks[created.ID].Labels) != 0 {
		t.Errorf("labels = %v, want observed remove to win", st.Tasks[created.ID].Labels)
	}
}

func TestFoldTombstoneRestoreAndConcurrentEdit(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	rm := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: created.ID}, nil)
	edit := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "concurrent edit"})
	// D4: a tombstone wins over a concurrent edit.
	st := fold(t, created, rm, edit)
	if !st.Tasks[created.ID].Tombstoned {
		t.Fatal("tombstone should beat concurrent edit")
	}
	// task.restored, authored after observing both, revives the ORIGINAL
	// identity — the concurrent edit's title is there waiting.
	restore := testEvent(t, cb, replicaB, []string{rm.ID, edit.ID}, ActionTaskRestored,
		Subject{Kind: "task", ID: created.ID}, nil)
	st = fold(t, created, rm, edit, restore)
	task := st.Tasks[created.ID]
	if task.Tombstoned || task.Title != "concurrent edit" || task.Alias != "T-1" {
		t.Errorf("restored task = %+v", task)
	}
}

func TestFoldConcurrentRemoveRestoreIsRestoreWinsAndContested(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	rmA := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: created.ID}, nil)
	rmB := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: created.ID}, nil)
	restore := testEvent(t, ca, replicaA, []string{rmA.ID}, ActionTaskRestored,
		Subject{Kind: "task", ID: created.ID}, nil)
	st := fold(t, created, rmA, rmB, restore)
	if st.Tasks[created.ID].Tombstoned {
		t.Error("restore-wins: concurrent removed+restored must resolve live")
	}
	found := false
	for _, cs := range st.Contested {
		if cs.Kind == SlotExistence && cs.Entity == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("existence slot should be contested: %+v", st.Contested)
	}
}

func TestFoldComputednessWins(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	seed := testEvent(t, ca, replicaA, []string{created.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:board"}, map[string]any{"description": "", "expr": ""})
	// Replica A makes the label computed; replica B concurrently assigns it.
	mkBoard := testEvent(t, ca, replicaA, []string{seed.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:board"}, map[string]any{"expr": "status:open"})
	assign := testEvent(t, cb, replicaB, []string{seed.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"label": "P:board"})
	// Replica A also concurrently removes the same label from the same
	// task — a sibling of `assign` off the same parent, so neither reaches
	// the other. This gives the membership slot TWO maximal writers, so
	// len(ws) > 1 would trip the contested block were it not for the
	// computed() guard: the test now actually exercises L2-6b (a computed
	// label's membership slot is never reported contested), not just L2-6a.
	unassign := testEvent(t, ca, replicaA, []string{seed.ID}, ActionTaskLabelRemoved,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"label": "P:board"})
	st := fold(t, created, seed, mkBoard, assign, unassign)
	if l := st.Labels["P:board"]; l == nil || l.Expr != "status:open" || !l.IsComputed() {
		t.Fatalf("label = %+v", st.Labels["P:board"])
	}
	if len(st.Tasks[created.ID].Labels) != 0 {
		t.Errorf("computed-ness must win: assignment is inert, got %v", st.Tasks[created.ID].Labels)
	}
	for _, cs := range st.Contested {
		if cs.Kind == SlotMembership && cs.Entity == created.ID && cs.Field == "P:board" {
			t.Errorf("inert membership slot reported contested: %+v", cs)
		}
	}
}

func TestFoldLabelRemoveThenUpsertResurrects(t *testing.T) {
	c := testClock(1000)
	up1 := testEvent(t, c, replicaA, nil, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:x"}, map[string]any{"description": "first", "expr": ""})
	rm := testEvent(t, c, replicaA, []string{up1.ID}, ActionLabelRemoved,
		Subject{Kind: "label", Name: "P:x"}, nil)
	st := fold(t, up1, rm)
	if !st.Labels["P:x"].Tombstoned {
		t.Fatal("label should be tombstoned after remove")
	}
	up2 := testEvent(t, c, replicaA, []string{rm.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:x"}, map[string]any{"description": "second", "expr": ""})
	st = fold(t, up1, rm, up2)
	l := st.Labels["P:x"]
	if l.Tombstoned || l.Description != "second" {
		t.Errorf("re-upsert should resurrect: %+v", l)
	}
}

func TestFoldCommentAttachesToTask(t *testing.T) {
	c := testClock(1000)
	task := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	c1 := testEvent(t, c, replicaA, []string{task.ID}, ActionCommentCreated, Subject{Kind: "comment"},
		map[string]any{"alias": "T-1-c1", "task_ref": task.ID, "body": "first"})
	c2 := testEvent(t, c, replicaA, []string{c1.ID}, ActionCommentCreated, Subject{Kind: "comment"},
		map[string]any{"alias": "T-1-c2", "task_ref": task.ID, "reply_to_ref": c1.ID, "body": "reply"})
	st := fold(t, task, c1, c2)
	if got := st.Comments[c1.ID]; got == nil || got.TaskRef != task.ID || got.Body != "first" {
		t.Fatalf("comment 1 = %+v", got)
	}
	if got := st.Comments[c2.ID]; got.ReplyToRef != c1.ID {
		t.Errorf("comment 2 reply ref = %+v", got)
	}
	ordered := st.CommentsByCreation(task.ID)
	if len(ordered) != 2 || ordered[0].ID != c1.ID || ordered[1].ID != c2.ID {
		t.Errorf("CommentsByCreation order wrong")
	}
}

func TestFoldDanglingWritesAreInert(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	ghostEdit := testEvent(t, c, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		map[string]any{"title": "ghost"})
	st := fold(t, created, ghostEdit)
	if len(st.Tasks) != 1 {
		t.Errorf("dangling write materialized an entity: %d tasks", len(st.Tasks))
	}
}

func TestFoldTasksByCreationUsesHLCStamp(t *testing.T) {
	ca, cb := testClock(5000), testClock(1000) // replica B created its task EARLIER
	t1 := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-a", "title": "later"})
	t2 := testEvent(t, cb, replicaB, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-b", "title": "earlier"})
	st := fold(t, t1, t2)
	got := st.TasksByCreation()
	if len(got) != 2 || got[0].ID != t2.ID || got[1].ID != t1.ID {
		t.Errorf("creation order wrong: %v", []string{got[0].Alias, got[1].Alias})
	}
}

func TestFoldTasksByCreationStableOnHLCTie(t *testing.T) {
	// Two clocks seeded identically both produce {P: 1001, L: 0} on their
	// first Tick(), so two tasks authored on the SAME replica — one per
	// clock — get byte-identical CreatedHLC and CreatedReplica. Only the
	// entity id can break the tie. compareCreation's final
	// strings.Compare(a.ID, b.ID) exists exactly for this case: without it
	// these two entities compare equal, and TasksByCreation ranges a Go map
	// (randomized iteration order per range) into sort.Slice, which is
	// UNSTABLE — so the returned order would vary from call to call.
	ca, cb := testClock(1000), testClock(1000)
	t1 := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-a", "title": "a"})
	t2 := testEvent(t, cb, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-b", "title": "b"})
	if t1.HLC != t2.HLC || t1.Replica != t2.Replica {
		t.Fatalf("test setup broken: want identical creation hlc/replica, got %+v/%s vs %+v/%s",
			t1.HLC, t1.Replica, t2.HLC, t2.Replica)
	}
	if t1.ID == t2.ID {
		t.Fatal("test setup broken: want distinct entity ids")
	}
	want := []string{t1.ID, t2.ID}
	if t2.ID < t1.ID {
		want = []string{t2.ID, t1.ID}
	}
	st := fold(t, t1, t2)
	for i := 0; i < 50; i++ {
		got := st.TasksByCreation()
		if len(got) != 2 || got[0].ID != want[0] || got[1].ID != want[1] {
			t.Fatalf("run %d: order = [%s, %s], want the id-sorted order %v", i, got[0].ID, got[1].ID, want)
		}
	}
}

func TestFoldUpdatedMetaTracksWinningWriter(t *testing.T) {
	// Author the edit directly (not via testEvent) so its actor and at
	// differ from the creation's — otherwise the assertion is vacuous.
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	edit, err := NewEvent(cb, replicaB, []string{created.ID}, Draft{
		At:      time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC),
		Actor:   "manager@claude:test",
		Action:  ActionTaskTitleChanged,
		Subject: Subject{Kind: "task", ID: created.ID},
		Payload: map[string]any{"title": "v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	st := fold(t, created, edit)
	task := st.Tasks[created.ID]
	if task.UpdatedBy != "manager@claude:test" || !task.UpdatedAt.Equal(edit.At) {
		t.Errorf("updated meta = %s @ %v", task.UpdatedBy, task.UpdatedAt)
	}
	if task.CreatedBy != created.Actor || !task.CreatedAt.Equal(created.At) {
		t.Errorf("creation meta must not move: %s @ %v", task.CreatedBy, task.CreatedAt)
	}
}

// TestFoldCommentActivityBumpsTaskUpdatedMeta reproduces ATM-fe669c: under
// the v2 fold, commenting never touched the parent task's UpdatedAt/UpdatedBy
// (comment.created writes only comment slots, and Pass 3 only looked at the
// task's own slots). The fix: a task's effective activity timestamp is the
// max of its own last write and its live comments' last writes — a pure
// read-time function of the event set (D4-safe), so it inherits the
// maximal-writer determinism of the rest of the fold.
func TestFoldCommentActivityBumpsTaskUpdatedMeta(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	comment, _, err := NewCommentCreated(cb, replicaB, []string{created.ID}, CommentCreateDraft{
		TaskAlias: "T-1",
		TaskRef:   created.ID,
		At:        time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC),
		Actor:     "developer@claude:test",
		Body:      "a comment after creation",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := fold(t, created, comment)
	task := st.Tasks[created.ID]
	if !task.UpdatedAt.Equal(comment.At) {
		t.Errorf("task.UpdatedAt = %v, want comment time %v (comment activity must bump parent task)",
			task.UpdatedAt, comment.At)
	}
	if task.UpdatedBy != comment.Actor {
		t.Errorf("task.UpdatedBy = %q, want %q (comment actor must propagate to parent task)",
			task.UpdatedBy, comment.Actor)
	}
}

// TestFoldCommentActivityBumpsTaskUpdatedMetaTombstonedExcluded verifies the
// tombstone carve-out: a tombstoned comment is inert (D5/decision 10) and
// must not bump the parent task's activity. The task's UpdatedAt stays at
// its own last write.
func TestFoldCommentActivityBumpsTaskUpdatedMetaTombstonedExcluded(t *testing.T) {
	ca := testClock(1000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	comment, _, err := NewCommentCreated(ca, replicaA, []string{created.ID}, CommentCreateDraft{
		TaskAlias: "T-1",
		TaskRef:   created.ID,
		At:        time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC),
		Actor:     "developer@claude:test",
		Body:      "later removed",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	removed := testEvent(t, ca, replicaA, []string{comment.ID}, ActionCommentRemoved,
		Subject{Kind: "comment", ID: comment.ID}, nil)
	st := fold(t, created, comment, removed)
	task := st.Tasks[created.ID]
	if !task.UpdatedAt.Equal(created.At) {
		t.Errorf("task.UpdatedAt = %v, want creation time %v (tombstoned comment must not bump parent)",
			task.UpdatedAt, created.At)
	}
	if task.UpdatedBy != created.Actor {
		t.Errorf("task.UpdatedBy = %q, want %q (tombstoned comment must not propagate actor)",
			task.UpdatedBy, created.Actor)
	}
}
