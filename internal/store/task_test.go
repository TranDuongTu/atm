package store

import (
	"atm/internal/core"
	"errors"
	"testing"
)

func TestCreateTaskAutoRegistersLabels(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if _, err := s.CreateTask("ATM", "t", "", []string{"ATM:type:bug"}, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LabelShow("ATM:type:bug"); err != nil {
		t.Fatalf("label not auto-registered: %v", err)
	}
}

func TestCreateTaskNoAutoStatus(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	for _, l := range tk.Labels {
		if l == "ATM:status:open" {
			t.Fatal("create must not auto-assign ATM:status:open")
		}
	}
}
func TestTaskLabelAddAutoRegisters(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	if err := s.TaskLabelAdd(tk.ID, "ATM:type:bug", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LabelShow("ATM:type:bug"); err != nil {
		t.Fatalf("TaskLabelAdd did not auto-register label: %v", err)
	}
}

func TestTaskLabelAddDedupSorted(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	_ = s.TaskLabelAdd(tk.ID, "ATM:type:bug", testActor)
	_ = s.TaskLabelAdd(tk.ID, "ATM:status:open", testActor)
	_ = s.TaskLabelAdd(tk.ID, "ATM:type:bug", testActor) // dup
	got, _ := s.GetTask(tk.ID)
	if len(got.Labels) != 2 || got.Labels[0] != "ATM:status:open" || got.Labels[1] != "ATM:type:bug" {
		t.Fatalf("labels = %v", got.Labels)
	}
}

func TestTaskLabelRemoveDoesNotTouchRegistry(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:type:bug"}, testActor)
	_ = s.TaskLabelRemove(tk.ID, "ATM:type:bug", testActor)
	if _, err := s.LabelShow("ATM:type:bug"); err != nil {
		t.Fatalf("registry must still contain label: %v", err)
	}
}
func TestSetTitleAppendsTitleChanged(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	_ = s.SetTitle(tk.ID, "new", testActor)
	hv := s.History("ATM", Subject{Kind: "task", ID: tk.ID})
	if len(hv) != 2 || hv[1].Action != ActionTaskTitleChanged {
		t.Fatalf("history = %+v", hv)
	}
}

func TestTaskLabelAddAppendsLabelAdded(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	_ = s.TaskLabelAdd(tk.ID, "ATM:type:bug", testActor)
	// Existing label (seeded) → only 1 entry (task.label-added). The label was already
	// in the registry from the seed, so no label.upserted.
	hv := s.History("ATM", Subject{Kind: "task", ID: tk.ID})
	if len(hv) != 2 {
		t.Fatalf("history len = %d want 2 (created + label-added)", len(hv))
	}
	if hv[1].Action != ActionTaskLabelAdded {
		t.Fatalf("history[1].action = %q want %q", hv[1].Action, ActionTaskLabelAdded)
	}
}

func TestTaskLabelAddNewLabelAppendsTwoEntries(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	before, _ := s.LastLogSeq("ATM")
	_ = s.TaskLabelAdd(tk.ID, "ATM:madeup:thing", testActor)
	after, _ := s.LastLogSeq("ATM")
	if after != before+2 {
		t.Fatalf("seq jumped %d → %d, want %d (label.upserted + task.label-added)", before, after, before+2)
	}
}

func TestRemoveTaskAppendsTombstoneDeletesCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	before, _ := s.LastLogSeq("ATM")
	_ = s.RemoveTask(tk.ID, testActor)
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("seq jumped %d → %d, want %d (task.removed tombstone)", before, after, before+1)
	}
	if _, err := s.GetTask(tk.ID); !core.IsNotFound(err) {
		t.Fatalf("GetTask after remove: %v want core.ErrNotFound", err)
	}
	db, _ := s.cacheDB()
	if _, ok, _ := cacheGetTask(db, tk.ID); ok {
		t.Fatal("cache row must be deleted")
	}
	// Tombstone visible in log.
	hv := s.History("ATM", Subject{Kind: "task", ID: tk.ID})
	if len(hv) == 0 || hv[len(hv)-1].Action != ActionTaskRemoved {
		t.Fatalf("tombstone missing from history: %+v", hv)
	}
}

func TestGetTaskLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	db, _ := s.cacheDB()
	// Hand-delete the cache row. Next read must rebuild from log.
	_, _ = db.Exec(`DELETE FROM tasks WHERE id = ?`, tk.ID)
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatalf("GetTask after cache delete: %v", err)
	}
	if got.ID != tk.ID || got.Title != tk.Title {
		t.Fatalf("rebuilt task = %+v want %+v", got, tk)
	}
	if _, ok, _ := cacheGetTask(db, tk.ID); !ok {
		t.Fatal("cache row was not rewritten after lazy miss")
	}
}

// I1: computed labels are never stored on tasks.
func TestCreateTaskRejectsComputedLabel(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:next-sprint", "board", "status:open", testActor)

	if _, err := s.CreateTask("ATM", "t", "", []string{"ATM:next-sprint"}, testActor); !errors.Is(err, ErrComputedLabelOnTask) {
		t.Fatalf("board on task: err = %v, want ErrComputedLabelOnTask", err)
	}
	if _, err := s.CreateTask("ATM", "t2", "", []string{"ATM:status:*"}, testActor); !errors.Is(err, ErrComputedLabelOnTask) {
		t.Fatalf("namespace on task: err = %v, want ErrComputedLabelOnTask", err)
	}
}
