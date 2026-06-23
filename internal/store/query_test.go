package store

import "testing"

func TestListTasksFilters(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", []Label{
		{Name: "type:impl"}, {Name: "type:bug"}, {Name: "area:cli"}, {Name: "area:tui"},
	}, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", []string{"type:impl", "area:cli"}, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", []string{"type:bug", "area:tui"}, "human:alice")
	_, _ = s.CreateTask("ATM", "t3", "", []string{"type:impl"}, "human:alice")
	_ = s.SetStatus("ATM-0001", "in-progress", "human:alice")
	_ = s.SetStatus("ATM-0001", "done", "human:alice")

	all := s.ListTasks(QueryFilters{})
	if len(all) != 3 {
		t.Fatalf("all = %d want 3", len(all))
	}

	byStatus := s.ListTasks(QueryFilters{Status: "done"})
	if len(byStatus) != 1 || byStatus[0].ID != "ATM-0001" {
		t.Fatalf("status filter: got %v", byStatus)
	}

	byLabel := s.ListTasks(QueryFilters{Labels: []string{"type:impl"}})
	if len(byLabel) != 2 {
		t.Fatalf("label filter type:impl: got %d want 2", len(byLabel))
	}

	byLabelAnd := s.ListTasks(QueryFilters{Labels: []string{"type:impl", "area:cli"}})
	if len(byLabelAnd) != 1 || byLabelAnd[0].ID != "ATM-0001" {
		t.Fatalf("AND-intersect labels: got %v", byLabelAnd)
	}

	byProject := s.ListTasks(QueryFilters{Project: "ATM"})
	if len(byProject) != 3 {
		t.Fatalf("project filter: got %d want 3", len(byProject))
	}
}

func TestListTasksSortedByID(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t3", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", nil, "human:alice")
	tasks := s.ListTasks(QueryFilters{})
	if len(tasks) != 3 {
		t.Fatalf("got %d want 3", len(tasks))
	}
	if tasks[0].ID != "ATM-0001" || tasks[1].ID != "ATM-0002" || tasks[2].ID != "ATM-0003" {
		ids := []string{}
		for _, t := range tasks {
			ids = append(ids, t.ID)
		}
		t.Fatalf("order = %v want [ATM-0001 ATM-0002 ATM-0003]", ids)
	}
}

func TestListTasksClaimantFilter(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t1", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t2", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t3", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t4", "", nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t5", "", nil, "human:alice")

	t2, _ := s.GetTask("ATM-0002")
	t2.Claim = &Claim{Actor: "agent:claude-1", At: Now()}
	_ = WriteJSON(s.taskPath(t2.ID), t2)

	byClaimant := s.ListTasks(QueryFilters{Claimant: "agent:claude-1"})
	if len(byClaimant) != 1 || byClaimant[0].ID != "ATM-0002" {
		t.Fatalf("claimant filter: got %v", byClaimant)
	}
}
