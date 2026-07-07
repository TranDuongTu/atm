package store

import (
	"encoding/json"
	"testing"
)

func TestCreateTaskAutoRegistersLabels(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	if _, err := s.CreateTask("ATM", "t", "", []string{"ATM:type:bug"}, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LabelShow("ATM:type:bug"); err != nil {
		t.Fatalf("label not auto-registered: %v", err)
	}
}

func TestCreateTaskNoAutoStatus(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	for _, l := range tk.Labels {
		if l == "ATM:status:open" {
			t.Fatal("create must not auto-assign ATM:status:open")
		}
	}
}

func TestCreateTaskAssignsNextId(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	a, _ := s.CreateTask("ATM", "a", "", nil, "claude")
	b, _ := s.CreateTask("ATM", "b", "", nil, "claude")
	if a.ID != "ATM-0001" || b.ID != "ATM-0002" {
		t.Fatalf("ids = %s, %s", a.ID, b.ID)
	}
}

func TestTaskLabelAddAutoRegisters(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	if err := s.TaskLabelAdd(tk.ID, "ATM:type:bug", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LabelShow("ATM:type:bug"); err != nil {
		t.Fatalf("TaskLabelAdd did not auto-register label: %v", err)
	}
}

func TestTaskLabelAddDedupSorted(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_ = s.TaskLabelAdd(tk.ID, "ATM:type:bug", "claude")
	_ = s.TaskLabelAdd(tk.ID, "ATM:status:open", "claude")
	_ = s.TaskLabelAdd(tk.ID, "ATM:type:bug", "claude") // dup
	got, _ := s.GetTask(tk.ID)
	if len(got.Labels) != 2 || got.Labels[0] != "ATM:status:open" || got.Labels[1] != "ATM:type:bug" {
		t.Fatalf("labels = %v", got.Labels)
	}
}

func TestTaskLabelRemoveDoesNotTouchRegistry(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:type:bug"}, "claude")
	_ = s.TaskLabelRemove(tk.ID, "ATM:type:bug", "claude")
	if _, err := s.LabelShow("ATM:type:bug"); err != nil {
		t.Fatalf("registry must still contain label: %v", err)
	}
}

func TestCreateTaskAppendsLogEntry(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	seqBefore, _ := s.LastLogSeq("ATM")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	entries, _ := s.ReadLog("ATM")
	var created *LogEntry
	for i := range entries {
		if entries[i].Seq > seqBefore && entries[i].Action == ActionTaskCreated {
			created = &entries[i]
			break
		}
	}
	if created == nil {
		t.Fatal("no task.created entry appended")
	}
	var got Task
	_ = json.Unmarshal(created.Payload, &got)
	if got.ID != tk.ID {
		t.Fatalf("payload id = %q want %q", got.ID, tk.ID)
	}
}

func TestSetTitleAppendsTitleChanged(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_ = s.SetTitle(tk.ID, "new", "ttran")
	hv := s.History("ATM", Subject{Kind: "task", ID: tk.ID})
	if len(hv) != 2 || hv[1].Action != ActionTaskTitleChanged {
		t.Fatalf("history = %+v", hv)
	}
}

func TestTaskLabelAddAppendsLabelAdded(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_ = s.TaskLabelAdd(tk.ID, "ATM:type:bug", "claude")
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
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	before, _ := s.LastLogSeq("ATM")
	_ = s.TaskLabelAdd(tk.ID, "ATM:madeup:thing", "claude")
	after, _ := s.LastLogSeq("ATM")
	if after != before+2 {
		t.Fatalf("seq jumped %d → %d, want %d (label.upserted + task.label-added)", before, after, before+2)
	}
}

func TestRemoveTaskAppendsTombstoneDeletesCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	before, _ := s.LastLogSeq("ATM")
	_ = s.RemoveTask(tk.ID, "claude")
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("seq jumped %d → %d, want %d (task.removed tombstone)", before, after, before+1)
	}
	if _, err := s.GetTask(tk.ID); !IsNotFound(err) {
		t.Fatalf("GetTask after remove: %v want ErrNotFound", err)
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
	// Replay excludes the tombstoned task.
	st, _ := s.Replay("ATM")
	for _, tk := range st.Tasks {
		if tk.ID == "ATM-0001" {
			t.Fatal("tombstoned task appeared in replay live set")
		}
	}
}

func TestGetTaskLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
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

func TestGetTaskStaleLogSeqTriggersRebuild(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_ = s.SetTitle(tk.ID, "changed", "claude")
	// Stomp the cache back to an old LogSeq (simulate cache write failure after the log append).
	db, _ := s.cacheDB()
	_, _ = db.Exec(`UPDATE tasks SET log_seq = 1 WHERE id = ?`, tk.ID)
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatalf("GetTask with stale cache: %v", err)
	}
	if got.Title != "changed" {
		t.Fatalf("lazy miss did not rebuild: title = %q want %q", got.Title, "changed")
	}
	if got.LogSeq != 21 {
		t.Fatalf("rebuilt LogSeq = %d, want 21 (seq of title-changed entry)", got.LogSeq)
	}
}

func TestGetTaskFutureLogSeqIntegrity(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	db, _ := s.cacheDB()
	// Hand-write a cache row that claims a seq higher than the log's last.
	_, _ = db.Exec(`UPDATE tasks SET log_seq = 9999 WHERE id = ?`, tk.ID)
	_, err := s.GetTask(tk.ID)
	if !IsIntegrity(err) {
		t.Fatalf("expected ErrIntegrity, got %v", err)
	}
}
