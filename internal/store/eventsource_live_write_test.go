package store

import (
	"errors"
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
	// The project name is asserted against the FOLD as well as through
	// GetProject: this test's job is the WRITE path, so it must fail if the
	// event never reached the file even in the (Task-9) world where the read
	// path can no longer paper over it. GetProject agreeing with the fold is
	// pinned by TestV2ReadPathReturnsV2Truth.
	if p, err := s.GetProject("ATM"); err != nil {
		t.Fatal(err)
	} else if p.Name != "renamed" {
		t.Fatalf("GetProject name = %q, want renamed", p.Name)
	}
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

// TestV2ReadPathReturnsV2Truth is the FLIPPED marker test (it was
// TestV2ReadPathIsBrokenUntilTask9, which pinned each of these reads at its
// known-broken v1-only answer while Task 8 shipped the write path alone). Task 9
// branched getProjectWithRebuild / getTaskWithRebuild / getCommentWithRebuild on
// the effective format, so each assertion below now demands the CORRECT answer,
// against the same scenarios that used to expose the breakage:
//
//   - a v2-BORN project has no log.jsonl, so LastLogSeq is 0 while
//     cacheProjectFromV2State stores a v2 CREATION ORDINAL in
//     Task.LogSeq/Comment.LogSeq: the v1 freshness check `LogSeq > LastSeq` used
//     to hard-fail with ErrIntegrity. The v2 branch precedes it.
//   - on an UPGRADED project lastProjectEventSeq still matches the FROZEN v1
//     log's project.created (keyed on Subject.Code), so the v1 staleness check
//     used to rebuild the project row from v1 and revert a v2 rename.
func TestV2ReadPathReturnsV2Truth(t *testing.T) {
	t.Run("v2-born task and comment reads return the v2 state", func(t *testing.T) {
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
		gotT, err := s.GetTask(tk.ID)
		if err != nil {
			t.Fatalf("GetTask on a v2-born task: %v", err)
		}
		if gotT.Title != "v2 born task" || gotT.Description != "d" {
			t.Fatalf("task = %#v", gotT)
		}
		gotC, err := s.GetComment(cm.ID)
		if err != nil {
			t.Fatalf("GetComment on a v2-born comment: %v", err)
		}
		if gotC.Body != "body" || gotC.TaskID != tk.ID {
			t.Fatalf("comment = %#v", gotC)
		}
		if got := s.ListTasks(QueryFilters{Project: "ATM"}); len(got) != 1 {
			t.Fatalf("ListTasks = %d tasks, want 1 (the on-disk truth is intact)", len(got))
		}
	})

	t.Run("upgraded-project GetProject returns the v2 rename", func(t *testing.T) {
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
		p, err := s.GetProject("ATM")
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "renamed" {
			t.Fatalf("GetProject name = %q, want %q: the read path reverted a durable v2 write", p.Name, "renamed")
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

// TestRemoveProjectRefusesUnprojectedV2Task pins the final-review Critical: the
// "is this project empty?" guard must be answered from the FOLD, not from cache
// rows. Under v2 a lagging cache is a designed-for state (an external append, or
// a writer that died between the append commit point and its reprojection), and
// RemoveProject's os.RemoveAll of the project dir is IRREVERSIBLE — it takes
// events.v2.jsonl with it. Every other v2 read path got a freshness gate; this
// one, the only one whose failure destroys data, did not.
func TestRemoveProjectRefusesUnprojectedV2Task(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	// A writer that fsynced its commit point and died before reprojecting: the
	// event line is truth, the cache holds no task row for it.
	var alias string
	if err := s.WithLock("ATM", func() error {
		_, a, err := s.appendV2TaskCreatedLocked("ATM", "unprojected live task", "", nil, "admin@cli:unset")
		alias = a
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := s.RemoveProject("ATM", "admin@cli:unset")
	if err == nil {
		t.Fatalf("RemoveProject with unprojected live task %q = nil: DATA LOSS, the event file was deleted", alias)
	}
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("RemoveProject err = %v, want ErrConflict (same refusal v1's hasTasksGuard produces)", err)
	}
	if _, statErr := os.Stat(s.eventsV2Path("ATM")); statErr != nil {
		t.Fatalf("events.v2.jsonl gone after a refused RemoveProject: %v", statErr)
	}
	// And the task is still there once the cache catches up.
	if _, err := s.GetTask(alias); err != nil {
		t.Fatalf("GetTask(%q) after the refused removal: %v", alias, err)
	}
}

// TestV2ActiveMissingEntityWritesReturnErrNotFound is the WRITE-surface mirror of
// TestV2ActiveMissingEntityReadsReturnErrNotFound. The read side was pinned; the
// write side regressed, because every v2 mutator resolves through
// v2AuthorCtx.resolveTaskRef/resolveCommentRef, which returned the raw
// eventsource.ErrNoMatch. That is not wrapped in store.ErrNotFound, so
// cli.CodeForError fell through to "generic" and `atm task set-title --task
// <typo>` exited 1 instead of 3.
func TestV2ActiveMissingEntityWritesReturnErrNotFound(t *testing.T) {
	s := testStore(t)
	if err := s.SetActiveFormat(StoreFormatV2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "real", "", nil, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	missingTask := "ATM-999999"
	missingComment := "ATM-999999-c1111"
	realTaskMissingComment := tk.ID + "-c9999"

	cases := []struct {
		name string
		call func() error
	}{
		{"SetTitle", func() error { return s.SetTitle(missingTask, "t", "admin@cli:unset") }},
		{"SetDescription", func() error { return s.SetDescription(missingTask, "d", "admin@cli:unset") }},
		{"TaskLabelAdd", func() error { return s.TaskLabelAdd(missingTask, "ATM:status:open", "admin@cli:unset") }},
		{"TaskLabelRemove", func() error { return s.TaskLabelRemove(missingTask, "ATM:status:open", "admin@cli:unset") }},
		{"RemoveTask", func() error { return s.RemoveTask(missingTask, "admin@cli:unset") }},
		{"SetCommentBody", func() error { return s.SetCommentBody(missingComment, "b", "admin@cli:unset") }},
		{"CommentLabelAdd", func() error {
			return s.CommentLabelAdd(missingComment, "ATM:status:open", "admin@cli:unset")
		}},
		{"CommentLabelRemove", func() error {
			return s.CommentLabelRemove(missingComment, "ATM:status:open", "admin@cli:unset")
		}},
		{"RemoveComment", func() error { return s.RemoveComment(missingComment, "admin@cli:unset") }},
		{"CreateComment/missing-task", func() error {
			_, err := s.CreateComment(missingTask, "b", nil, "", "admin@cli:unset")
			return err
		}},
		{"CreateComment/missing-reply-to", func() error {
			_, err := s.CreateComment(tk.ID, "b", nil, realTaskMissingComment, "admin@cli:unset")
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if !IsNotFound(err) {
				t.Fatalf("%s on a missing v2 entity = %v, want ErrNotFound (CLI exit 3, not generic exit 1)", c.name, err)
			}
			if errors.Is(err, eventsource.ErrNoMatch) {
				t.Fatalf("%s leaked eventsource.ErrNoMatch through the compatibility API: %v", c.name, err)
			}
		})
	}
}
