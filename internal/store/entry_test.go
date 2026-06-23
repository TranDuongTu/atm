package store

import "testing"

func TestTodoAddAndToggle(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")

	task, err := s.TodoAdd("ATM-0001", "write tests", "human:alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Todos) != 1 {
		t.Fatalf("todos = %d want 1", len(task.Todos))
	}
	if task.Todos[0].ID != "t1" {
		t.Fatalf("todo id = %q want t1", task.Todos[0].ID)
	}
	if task.Todos[0].Done {
		t.Fatal("todo should start undone")
	}

	task, err = s.TodoToggle("ATM-0001", "t1", "human:alice")
	if err != nil {
		t.Fatal(err)
	}
	if !task.Todos[0].Done {
		t.Fatal("todo should be done after toggle")
	}
	task, _ = s.TodoToggle("ATM-0001", "t1", "human:alice")
	if task.Todos[0].Done {
		t.Fatal("todo should be undone after second toggle")
	}
}

func TestTodoCounterMonotonic(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	_, _ = s.TodoAdd("ATM-0001", "a", "human:alice")
	task, _ := s.TodoAdd("ATM-0001", "b", "human:alice")
	if task.Todos[1].ID != "t2" {
		t.Fatalf("todo id = %q want t2", task.Todos[1].ID)
	}
}

func TestFollowupAddAndResolve(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")

	task, err := s.FollowupAdd("ATM-0001", "decide storage", "human:alice", "human:alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Followups) != 1 {
		t.Fatalf("followups = %d want 1", len(task.Followups))
	}
	if task.Followups[0].ID != "f1" {
		t.Fatalf("followup id = %q want f1", task.Followups[0].ID)
	}
	if task.Followups[0].Status != "open" {
		t.Fatalf("status = %q want open", task.Followups[0].Status)
	}
	if task.Followups[0].ResolvedAt != nil {
		t.Fatal("resolved_at should be nil initially")
	}

	task, err = s.FollowupResolve("ATM-0001", "f1", "human:alice")
	if err != nil {
		t.Fatal(err)
	}
	if task.Followups[0].Status != "resolved" {
		t.Fatalf("status = %q want resolved", task.Followups[0].Status)
	}
	if task.Followups[0].ResolvedAt == nil {
		t.Fatal("resolved_at should be set")
	}
	if task.Followups[0].ResolvedBy != "human:alice" {
		t.Fatalf("resolved_by = %q", task.Followups[0].ResolvedBy)
	}
}

func TestDiscussionAdd(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")

	task, err := s.DiscussionAdd("ATM-0001", "use file locking", "human:alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Discussions) != 1 {
		t.Fatalf("discussions = %d want 1", len(task.Discussions))
	}
	if task.Discussions[0].ID != "d1" {
		t.Fatalf("discussion id = %q want d1", task.Discussions[0].ID)
	}
}

func TestTimelineMergedAndSorted(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "t", "", nil, "human:alice")
	_, _ = s.TodoAdd("ATM-0001", "todo1", "human:alice")
	_, _ = s.DiscussionAdd("ATM-0001", "disc1", "human:alice")
	_, _ = s.FollowupAdd("ATM-0001", "fu1", "human:alice", "human:alice", nil)
	_ = s.SetTitle("ATM-0001", "new", "human:alice")

	items, err := s.Timeline("ATM-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("timeline len = %d want 8", len(items))
	}
	for i := 1; i < len(items); i++ {
		if items[i].at.Before(items[i-1].at) {
			t.Fatalf("timeline not sorted at index %d", i)
		}
	}
	kinds := map[string]int{}
	for _, it := range items {
		kinds[it.kind]++
	}
	if kinds["history"] != 5 || kinds["todo"] != 1 || kinds["followup"] != 1 || kinds["discussion"] != 1 {
		t.Fatalf("kind counts = %v", kinds)
	}
}
