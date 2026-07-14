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
	//
	// Task and comment reads happen to survive on an UPGRADED project only
	// because its frozen v1 log supplies a LastLogSeq large enough to clear the
	// staleness check. They are NOT generally safe: on a V2-BORN project (no
	// log.jsonl, so LastLogSeq is 0) GetTask/GetComment hard-fail with
	// ErrIntegrity, because cacheProjectFromV2State stores the v2 creation
	// ordinal in LogSeq. TestV2ReadPathIsBrokenUntilTask9 pins that.
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

// mustRead returns the file's bytes, or nil when it does not exist.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return raw
}

// TestMutatorRechecksFormatUnderLock covers the TOCTOU between a mutator's
// PRE-LOCK format read (which picks the v1 or v2 body) and its acquisition of
// the project lock. `atm` is multi-process — WithLock is a cross-process flock —
// so another process can cut the project over (upgrade) or back (rollback) in
// exactly that window. testHookAfterDispatchFormat makes the window
// deterministic instead of racing goroutines at it.
//
// Without the under-lock re-check the upgrade direction is silent corruption:
// the v1 body appends to log.jsonl on a project that is now v2-active (the
// plan's hardest constraint is that log.jsonl stays byte-identical) and the
// task never reaches events.v2.jsonl. The rollback direction is the mirror.
func TestMutatorRechecksFormatUnderLock(t *testing.T) {
	t.Run("upgrade lands in the window: no v1 append", func(t *testing.T) {
		s := testStore(t)
		if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
			t.Fatal(err)
		}
		before := mustRead(t, s.logPath("ATM"))

		fired := false
		testHookAfterDispatchFormat = func(code string) {
			if fired || code != "ATM" {
				return
			}
			fired = true
			// Another process upgrades the project while our mutator sits
			// between its format read and the project lock.
			if _, err := s.UpgradeProjectToV2(code); err != nil {
				t.Errorf("upgrade in hook: %v", err)
			}
		}
		t.Cleanup(func() { testHookAfterDispatchFormat = nil })

		_, err := s.CreateTask("ATM", "racy", "d", nil, "admin@cli:unset")
		if !fired {
			t.Fatal("hook never fired: CreateTask no longer reads the format before taking the lock")
		}
		// The corruption first, the error contract second.
		if after := mustRead(t, s.logPath("ATM")); string(after) != string(before) {
			t.Error("v1 body ran on a now-v2-active project: log.jsonl changed")
		}
		if !IsConflict(err) {
			t.Errorf("CreateTask across an upgrade = %v, want ErrConflict", err)
		}
	})

	t.Run("rollback lands in the window: no v2 append", func(t *testing.T) {
		s := testStore(t)
		if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
			t.Fatal(err)
		}
		before := mustRead(t, s.eventsV2Path("ATM"))

		fired := false
		testHookAfterDispatchFormat = func(code string) {
			if fired || code != "ATM" {
				return
			}
			fired = true
			if _, err := s.RollbackProjectToV1(code); err != nil {
				t.Errorf("rollback in hook: %v", err)
			}
		}
		t.Cleanup(func() { testHookAfterDispatchFormat = nil })

		_, err := s.CreateTask("ATM", "racy", "d", nil, "admin@cli:unset")
		if !fired {
			t.Fatal("hook never fired: CreateTask no longer reads the format before taking the lock")
		}
		if after := mustRead(t, s.eventsV2Path("ATM")); string(after) != string(before) {
			t.Error("v2 body ran on a now-v1 project: events.v2.jsonl changed")
		}
		if !IsConflict(err) {
			t.Errorf("CreateTask across a rollback = %v, want ErrConflict", err)
		}
	})
}

// TestCreateCommentV2OnMissingTaskAppendsNothing: an event append is DURABLE,
// so every reference a v2 mutation depends on must be validated BEFORE the
// first append. createCommentV2 used to auto-register the comment's labels
// first and only discover the missing task inside appendV2CommentCreatedLocked
// — committing label.upserted events for a comment that never happened, and
// (because the error path skips reprojectV2Locked) leaving cache.db and the v2
// freshness count behind the file.
func TestCreateCommentV2OnMissingTaskAppendsNothing(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, s.eventsV2Path("ATM"))

	// The task ref is resolved through the fold, so the error is eventsource's
	// own "no entity matches" rather than store.ErrNotFound; what this test is
	// about is that it arrives BEFORE anything is written.
	_, err := s.CreateComment("ATM-9999", "body", []string{"ATM:area:orphan"}, "", "admin@cli:unset")
	if err == nil {
		t.Fatal("CreateComment on a nonexistent task must fail")
	}
	if after := mustRead(t, s.eventsV2Path("ATM")); string(after) != string(before) {
		t.Fatal("failed CreateComment appended events to events.v2.jsonl")
	}
	if _, err := s.LabelShow("ATM:area:orphan"); !IsNotFound(err) {
		t.Fatalf("failed CreateComment registered its labels: LabelShow = %v", err)
	}
	// Same rule for the reply-to reference.
	tk, err := s.CreateTask("ATM", "t", "d", nil, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	before = mustRead(t, s.eventsV2Path("ATM"))
	_, err = s.CreateComment(tk.ID, "body", []string{"ATM:area:orphan"}, tk.ID+"-c9999", "admin@cli:unset")
	if err == nil {
		t.Fatal("CreateComment with a nonexistent reply-to must fail")
	}
	if after := mustRead(t, s.eventsV2Path("ATM")); string(after) != string(before) {
		t.Fatal("failed CreateComment (bad reply-to) appended events to events.v2.jsonl")
	}
	if _, err := s.LabelShow("ATM:area:orphan"); !IsNotFound(err) {
		t.Fatalf("failed CreateComment (bad reply-to) registered its labels: LabelShow = %v", err)
	}
}

// TestV2ReadPathIsBrokenUntilTask9 is a MARKER TEST. Every assertion below
// asserts KNOWN-BROKEN behaviour on purpose: Task 8 rewired the WRITE path only,
// and the read path (getProjectWithRebuild / getTaskWithRebuild /
// getCommentWithRebuild) is still v1-only. cacheProjectFromV2State stores a v2
// CREATION ORDINAL in Task.LogSeq/Comment.LogSeq, and a v2-BORN project has no
// log.jsonl at all — so LastLogSeq is 0 and the v1 freshness check `LogSeq >
// LastSeq` hard-fails. On-disk truth is intact in every case here; only the
// read path lies.
//
// WHEN TASK 9 LANDS THIS TEST FAILS. That is its job. Do not delete it — flip
// each assertion to the correct one (reads succeed and return the v2 state).
func TestV2ReadPathIsBrokenUntilTask9(t *testing.T) {
	t.Run("v2-born task and comment reads hard-fail with ErrIntegrity", func(t *testing.T) {
		s := testStore(t)
		if err := s.SetActiveFormat(StoreFormatV2); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateProject("ATM", "born v2", "admin@cli:unset"); err != nil {
			t.Fatal(err)
		}
		tk, err := s.CreateTask("ATM", "v2 born task", "d", nil, "admin@cli:unset")
		if err != nil {
			t.Fatalf("the WRITE must work: %v", err)
		}
		cm, err := s.CreateComment(tk.ID, "body", nil, "", "admin@cli:unset")
		if err != nil {
			t.Fatalf("the WRITE must work: %v", err)
		}
		// BROKEN (Task 9): `atm task show` on a v2-born task exits 5 with
		// "integrity: task ... cache LogSeq=1 > log LastSeq=0".
		if _, err := s.GetTask(tk.ID); !IsIntegrity(err) {
			t.Fatalf("GetTask on a v2-born task = %v; if Task 9 has landed the read path, FLIP this assertion to require success", err)
		}
		if _, err := s.GetComment(cm.ID); !IsIntegrity(err) {
			t.Fatalf("GetComment on a v2-born comment = %v; if Task 9 has landed the read path, FLIP this assertion to require success", err)
		}
		// ListTasks has no staleness check, so it works today — which is why
		// the Task 8 suite was green: nothing read a v2-born task by id.
		if got := s.ListTasks(QueryFilters{Project: "ATM"}); len(got) != 1 {
			t.Fatalf("ListTasks = %d tasks, want 1 (the on-disk truth is intact)", len(got))
		}
	})

	t.Run("upgraded-project GetProject reverts a v2 rename", func(t *testing.T) {
		s := testStore(t)
		if _, err := s.CreateProject("ATM", "original", "admin@cli:unset"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
			t.Fatal(err)
		}
		if err := s.SetProjectName("ATM", "renamed", "admin@cli:unset"); err != nil {
			t.Fatal(err)
		}
		// BROKEN (Task 9): lastProjectEventSeq matches the FROZEN v1 log's
		// project.created (keyed on Subject.Code), so getProjectWithRebuild
		// judges the v2-projected row stale and rebuilds it from v1 — reverting
		// the name that is correctly recorded in events.v2.jsonl and the fold.
		p, err := s.GetProject("ATM")
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "original" {
			t.Fatalf("GetProject name = %q; if Task 9 has landed the read path, FLIP this assertion to want %q", p.Name, "renamed")
		}
	})
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
