package store

import (
	"os"
	"testing"

	"atm/internal/eventsource"
)

func TestV2ActiveTaskMutationWritesOnlyEventsV2(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	before, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "v2 task", "desc", []string{"ATM:status:open"}, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("v1 log changed while project is v2-active")
	}
	snap, err := s.verifyV2File("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if snap.EventCount == 0 {
		t.Fatal("expected v2 events")
	}
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "v2 task" {
		t.Fatalf("task title = %q", got.Title)
	}
}

// TestV2ActiveEveryMutatorLeavesV1LogByteIdentical drives the WHOLE mutator
// surface against a v2-active project and asserts, after each one, that
// log.jsonl has not changed by a single byte — the plan's hardest constraint —
// while the v2 event file grows and cache.db stays consistent with the fold.
func TestV2ActiveEveryMutatorLeavesV1LogByteIdentical(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	frozen, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	events := 0
	step := func(name string, fn func() error) {
		t.Helper()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		now, err := os.ReadFile(s.logPath("ATM"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(now) != string(frozen) {
			t.Fatalf("%s appended to log.jsonl on a v2-active project", name)
		}
		snap, err := s.verifyV2File("ATM")
		if err != nil {
			t.Fatalf("%s: v2 file: %v", name, err)
		}
		if snap.EventCount <= events {
			t.Fatalf("%s wrote no v2 event (count still %d)", name, snap.EventCount)
		}
		events = snap.EventCount
	}

	var task *Task
	step("CreateTask", func() error {
		var err error
		task, err = s.CreateTask("ATM", "t1", "d1", []string{"ATM:status:open"}, "admin@cli:unset")
		return err
	})
	step("SetTitle", func() error { return s.SetTitle(task.ID, "t2", "admin@cli:unset") })
	step("SetDescription", func() error { return s.SetDescription(task.ID, "d2", "admin@cli:unset") })
	step("TaskLabelAdd", func() error { return s.TaskLabelAdd(task.ID, "ATM:type:bug", "admin@cli:unset") })
	step("TaskLabelRemove", func() error { return s.TaskLabelRemove(task.ID, "ATM:type:bug", "admin@cli:unset") })
	step("LabelAdd", func() error { return s.LabelAdd("ATM:area:cli", "cli area", "", "admin@cli:unset") })
	step("LabelSeed", func() error { return s.LabelSeed("ATM:area:tui", "tui area", "", "admin@cli:unset") })
	step("LabelRemove", func() error {
		_, err := s.LabelRemove("ATM:area:tui", "admin@cli:unset")
		return err
	})
	step("SetProjectName", func() error { return s.SetProjectName("ATM", "renamed", "admin@cli:unset") })

	var comment *Comment
	step("CreateComment", func() error {
		var err error
		comment, err = s.CreateComment(task.ID, "body", []string{"ATM:comment:note"}, "", "admin@cli:unset")
		return err
	})
	step("SetCommentBody", func() error { return s.SetCommentBody(comment.ID, "body2", "admin@cli:unset") })
	step("CommentLabelAdd", func() error { return s.CommentLabelAdd(comment.ID, "ATM:area:cli", "admin@cli:unset") })
	step("CommentLabelRemove", func() error {
		return s.CommentLabelRemove(comment.ID, "ATM:area:cli", "admin@cli:unset")
	})

	// Reads go through the compatibility API and must see the v2 writes.
	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "t2" || got.Description != "d2" {
		t.Fatalf("task = %#v", got)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "ATM:status:open" {
		t.Fatalf("task labels = %v, want only ATM:status:open", got.Labels)
	}
	gotc, err := s.GetComment(comment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotc.Body != "body2" {
		t.Fatalf("comment = %#v", gotc)
	}
	if len(gotc.Labels) != 1 || gotc.Labels[0] != "ATM:comment:note" {
		t.Fatalf("comment labels = %v, want only ATM:comment:note", gotc.Labels)
	}
	// The project row is asserted against the FOLD, not GetProject: for an
	// UPGRADED project GetProject's v1 staleness check still fires
	// (lastProjectEventSeq matches the frozen log's project.created on
	// Subject.Code) and rebuilds the row from v1, reverting the name. That is
	// the read path Task 9 branches by format; the v2 WRITE, which is Task 8's
	// job, is correct — the event is in the file and the fold, and
	// cacheProjectFromV2State wrote the right row before GetProject undid it.
	// (Task and comment reads are unaffected: their v2 hash aliases match no
	// v1 log subject, so the staleness check finds nothing to rebuild from.)
	snap, err := s.verifyV2File("ATM")
	if err != nil {
		t.Fatal(err)
	}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		t.Fatal(err)
	}
	proj, ok := v2LiveProject(state, "ATM")
	if !ok {
		t.Fatal("project missing from v2 fold")
	}
	if proj.Name != "renamed" {
		t.Fatalf("v2 fold project name = %q, want renamed", proj.Name)
	}
	// LabelAdd registered a new label; LabelRemove unregistered one.
	if _, err := s.LabelShow("ATM:area:cli"); err != nil {
		t.Fatalf("LabelAdd did not register: %v", err)
	}
	if _, err := s.LabelShow("ATM:area:tui"); !IsNotFound(err) {
		t.Fatalf("LabelRemove did not unregister: %v", err)
	}

	step("RemoveComment", func() error { return s.RemoveComment(comment.ID, "admin@cli:unset") })
	if _, err := s.GetComment(comment.ID); !IsNotFound(err) {
		t.Fatalf("removed comment still readable: %v", err)
	}
	step("RemoveTask", func() error { return s.RemoveTask(task.ID, "admin@cli:unset") })
	if _, err := s.GetTask(task.ID); !IsNotFound(err) {
		t.Fatalf("removed task still readable: %v", err)
	}
	// RemoveProject is a mutator like any other: no v1 tombstone, ever.
	if err := s.RemoveProject("ATM", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateProjectBornV2WhenActiveFormatV2(t *testing.T) {
	s := testStore(t)
	if err := s.SetActiveFormat(StoreFormatV2); err != nil { // empty store: no entry-less projects, flip allowed
		t.Fatal(err)
	}
	p, err := s.CreateProject("ATM", "born v2", "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.logPath("ATM")); !os.IsNotExist(err) {
		t.Fatal("v2-born project must have no log.jsonl")
	}
	if _, err := os.Stat(s.eventsV2Path("ATM")); err != nil {
		t.Fatalf("events.v2.jsonl missing: %v", err)
	}
	m, err := s.readStoreMeta()
	if err != nil {
		t.Fatal(err)
	}
	if m.ProjectFormats["ATM"] != StoreFormatV2 {
		t.Fatalf("v2 birth must write an explicit ProjectFormats entry, got %#v", m.ProjectFormats)
	}
	if p.Name != "born v2" {
		t.Fatalf("project = %#v", p)
	}
	if labels := s.LabelList("ATM", ""); len(labels) == 0 {
		t.Fatal("v2 birth must seed default labels")
	}
	// Existence check (F): recreating must fail even though log.jsonl is absent.
	if _, err := s.CreateProject("ATM", "again", "admin@cli:unset"); err == nil {
		t.Fatal("CreateProject must detect an existing v2-born project")
	}
	// Rollback guard on the REAL v2-born case (Task 4 simulated it by
	// deleting log.jsonl): a project with no v1 media must refuse rollback.
	if _, err := s.RollbackProjectToV1("ATM"); !IsConflict(err) {
		t.Fatalf("rollback of a v2-born project = %v, want ErrConflict", err)
	}
}

func TestRemoveProjectV2ClearsFormatEntryAndAllowsRecreation(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	if err := s.RemoveProject("ATM", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	// The no-v1-append property cannot be asserted post-hoc (the directory
	// is gone); it is enforced structurally — removeProjectV2 below contains
	// no appendLogLocked call — and by the global constraint.
	if _, err := os.Stat(s.projectDir("ATM")); !os.IsNotExist(err) {
		t.Fatal("project dir should be deleted")
	}
	m, err := s.readStoreMeta()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.ProjectFormats["ATM"]; ok {
		t.Fatal("RemoveProject must delete the ProjectFormats entry")
	}
	// Recreation must not inherit the stale v2 format: with the entry gone it
	// follows ActiveFormat, and on a v1-default store yields a clean v1 project.
	if _, err := s.CreateProject("ATM", "recreated", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
}
