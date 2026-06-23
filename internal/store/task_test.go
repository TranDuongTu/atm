package store

import (
	"testing"
)

func TestTaskCreateAssignsID(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	t1, err := s.CreateTask("ATM", "first", "", nil, "human:alice")
	if err != nil {
		t.Fatal(err)
	}
	if t1.ID != "ATM-0001" {
		t.Fatalf("id = %q want ATM-0001", t1.ID)
	}
	t2, _ := s.CreateTask("ATM", "second", "", nil, "human:alice")
	if t2.ID != "ATM-0002" {
		t.Fatalf("id = %q want ATM-0002", t2.ID)
	}
	p, _ := s.GetProject("ATM")
	if p.NextTaskN != 3 {
		t.Fatalf("next_task_n = %d want 3", p.NextTaskN)
	}
}

func TestTaskCreateIDWidening(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	for i := 1; i <= 10000; i++ {
		_, err := s.CreateTask("ATM", "t", "", nil, "human:alice")
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	t1, err := s.CreateTask("ATM", "tenk", "", nil, "human:alice")
	if err != nil {
		t.Fatal(err)
	}
	if t1.ID != "ATM-10001" {
		t.Fatalf("id = %q want ATM-10001", t1.ID)
	}
}

func TestTaskGetNotFound(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, err := s.GetTask("ATM-9999")
	if !IsNotFound(err) {
		t.Fatalf("expected not-found, got %v", err)
	}
}

func TestTaskCreateValidatesLabelInProject(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}}, nil, "human:alice")
	_, err := s.CreateTask("ATM", "t", "", []string{"type:missing"}, "human:alice")
	if !IsUsage(err) {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestTaskHistoryGrows(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}, {Name: "area:cli"}}, nil, "human:alice")
	t1, _ := s.CreateTask("ATM", "t", "", []string{"type:impl"}, "human:alice")
	if len(t1.History) != 1 || t1.History[0].Action != "created" {
		t.Fatalf("history = %v", t1.History)
	}
	if err := s.SetTitle("ATM-0001", "new title", "human:alice"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask("ATM-0001")
	if len(got.History) != 2 {
		t.Fatalf("history len = %d want 2", len(got.History))
	}
	if got.History[1].Action != "title-changed" {
		t.Fatalf("action = %q want title-changed", got.History[1].Action)
	}
	if got.Title != "new title" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestTaskStatusTransitionsAllowed(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	allowed := []struct{ from, to string }{
		{"open", "in-progress"},
		{"in-progress", "review"},
		{"review", "done"},
		{"done", "open"},
	}
	for _, tr := range allowed {
		if err := s.SetStatus("ATM-0001", tr.to, "human:alice"); err != nil {
			t.Fatalf("%s -> %s: %v", tr.from, tr.to, err)
		}
	}
}

func TestTaskStatusTransitionsRejected(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	rejected := []struct{ from, to string }{
		{"open", "done"},
		{"open", "review"},
		{"in-progress", "cancelled"},
		{"blocked", "done"},
		{"review", "blocked"},
	}
	for _, tr := range rejected {
		t1, _ := s.GetTask("ATM-0001")
		if err := s.setStatusDirect(t1, tr.from); err != nil {
			t.Fatalf("setup %s: %v", tr.from, err)
		}
		err := s.SetStatus("ATM-0001", tr.to, "human:alice")
		if !IsConflict(err) {
			t.Fatalf("%s -> %s: expected conflict, got %v", tr.from, tr.to, err)
		}
	}
}

func (s *Store) setStatusDirect(t *Task, status string) error {
	t.Status = status
	return WriteJSON(s.taskPath(t.ID), t)
}

func TestTaskLabelAddRemove(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}, {Name: "area:cli"}}, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	if err := s.TaskLabelAdd("ATM-0001", "area:cli", "human:alice"); err != nil {
		t.Fatal(err)
	}
	t1, _ := s.GetTask("ATM-0001")
	if len(t1.Labels) != 1 || t1.Labels[0] != "area:cli" {
		t.Fatalf("labels = %v", t1.Labels)
	}
	if err := s.TaskLabelRemove("ATM-0001", "area:cli", "human:alice"); err != nil {
		t.Fatal(err)
	}
	t1, _ = s.GetTask("ATM-0001")
	if len(t1.Labels) != 0 {
		t.Fatalf("labels = %v want empty", t1.Labels)
	}
}

func TestTaskHistoryCountersMonotonic(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{{Name: "type:impl"}, {Name: "area:cli"}}, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	_ = s.SetTitle("ATM-0001", "a", "human:alice")
	_ = s.SetTitle("ATM-0001", "b", "human:alice")
	_ = s.SetDescription("ATM-0001", "desc", "human:alice")
	t1, _ := s.GetTask("ATM-0001")
	if len(t1.History) != 4 {
		t.Fatalf("history len = %d want 4", len(t1.History))
	}
	if t1.History[0].ID != "h1" || t1.History[1].ID != "h2" || t1.History[2].ID != "h3" || t1.History[3].ID != "h4" {
		ids := []string{}
		for _, h := range t1.History {
			ids = append(ids, h.ID)
		}
		t.Fatalf("history ids = %v want [h1 h2 h3 h4]", ids)
	}
}
