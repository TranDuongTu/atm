package store

import "testing"

func TestListTasksANDIntersectsExactLabels(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "a", "", []string{"ATM:type:bug", "ATM:status:open"}, testActor)
	_, _ = s.CreateTask("ATM", "b", "", []string{"ATM:type:bug"}, testActor)
	got := s.ListTasks(QueryFilters{Project: "ATM", Labels: []string{"ATM:type:bug", "ATM:status:open"}})
	if len(got) != 1 || got[0].Title != "a" {
		t.Fatalf("got %v", got)
	}
}

func TestListTasksIgnoresWildcardTokensForScoping(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, testActor)
	_, _ = s.CreateTask("ATM", "b", "", []string{"ATM:status:done"}, testActor)
	// ATM:status:* is a wildcard (facet) — must NOT restrict; all 2 tasks returned.
	got := s.ListTasks(QueryFilters{Project: "ATM", Labels: []string{"ATM:status:*"}})
	if len(got) != 2 {
		t.Fatalf("wildcard must not restrict; got %d", len(got))
	}
}

func TestGroupTasksMultiMembership(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	t1, _ := s.CreateTask("ATM", "a", "", []string{"ATM:status:open", "ATM:status:done"}, testActor)
	_, _ = s.CreateTask("ATM", "b", "", []string{"ATM:status:open"}, testActor)
	_, _ = s.CreateTask("ATM", "c", "", nil, testActor)
	groups, others := s.GroupTasks(QueryFilters{Project: "ATM", Labels: []string{"ATM:status:*"}})
	// open group has 2 (t1 multi-members + b); done group has 1 (t1).
	open := findGroup(t, groups, "ATM:status:open")
	done := findGroup(t, groups, "ATM:status:done")
	if len(open.Tasks) != 2 || len(done.Tasks) != 1 {
		t.Fatalf("open=%d done=%d", len(open.Tasks), len(done.Tasks))
	}
	if !containsID(others, t1.ID) && !inGroup(open, t1.ID) {
		// t1 carries a matching label so it's in groups, not others
	}
	if len(others) != 1 || others[0].Title != "c" {
		t.Fatalf("others = %v want [c]", others)
	}
}

func TestGroupTasksNoWildcardsReturnsAllInOthers(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "a", "", nil, testActor)
	groups, others := s.GroupTasks(QueryFilters{Project: "ATM"})
	if len(groups) != 0 || len(others) != 1 {
		t.Fatalf("groups=%d others=%d", len(groups), len(others))
	}
}

func findGroup(t *testing.T, groups []LabelGroup, name string) LabelGroup {
	t.Helper()
	for _, g := range groups {
		if g.Label == name {
			return g
		}
	}
	t.Fatalf("group %q not found", name)
	return LabelGroup{}
}

func inGroup(g LabelGroup, id string) bool {
	for _, tk := range g.Tasks {
		if tk.ID == id {
			return true
		}
	}
	return false
}

func containsID(tasks []*Task, id string) bool {
	for _, tk := range tasks {
		if tk.ID == id {
			return true
		}
	}
	return false
}
