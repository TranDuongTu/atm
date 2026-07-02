package store

import "testing"

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

func TestSetTitleAppendsHistory(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "old", "", nil, "claude")
	_ = s.SetTitle(tk.ID, "new", "ttran")
	got, _ := s.GetTask(tk.ID)
	if got.Title != "new" {
		t.Fatalf("title = %q", got.Title)
	}
	if len(got.History) < 2 {
		t.Fatalf("history len = %d want >=2", len(got.History))
	}
	if got.History[1].Action != "title-changed" {
		t.Fatalf("history[1].action = %q want title-changed", got.History[1].Action)
	}
}