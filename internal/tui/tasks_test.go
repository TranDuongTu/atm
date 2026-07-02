package tui

import (
	"testing"

	"atm/internal/store"
)

// toRowTest builds a taskRow without depending on a live *Model (which
// store.Now()/relTime need). Used only by the nesting-helper tests.
func toRowTest(tk *store.Task) taskRow {
	return taskRow{
		id:     tk.ID,
		title:  tk.Title,
		labels: tk.Labels,
		task:   tk,
	}
}

func mkTask(id, title string, labels ...string) *store.Task {
	return &store.Task{ID: id, Title: title, Labels: labels}
}

// TestBuildNestedGroupsTwoWildcards verifies the TUI-side nesting pass for a
// two-wildcard filter (mockup Screen 7). The store returns flat per-concrete-
// label buckets; buildNestedGroups must turn them into a two-level tree with
// multi-membership preserved and a per-level (no matching labels) sub-bucket.
func TestBuildNestedGroupsTwoWildcards(t *testing.T) {
	// Top-level group "ATM:status:open" tasks (as GroupTasks would bucket them).
	openTasks := []*store.Task{
		mkTask("ATM-0008", "Remove claim/unclaim", "ATM:status:open", "ATM:type:refactor"),
		mkTask("ATM-0011", "Write label_test.go", "ATM:status:open", "ATM:type:task"),
		// Multi-membership: this task carries both ATM:type:bug and
		// ATM:type:refactor — it must appear in BOTH sub-groups.
		mkTask("ATM-0014", "Multi-label task", "ATM:status:open", "ATM:type:bug", "ATM:type:refactor"),
		// Matches no second-wildcard label -> sub-(no matching labels) bucket.
		mkTask("ATM-0020", "Status only", "ATM:status:open"),
	}
	wildcards := []string{"ATM:type:*"}
	subs := buildNestedGroups(openTasks, wildcards, toRowTest)

	// Expect three concrete sub-groups (alphabetical) + one
	// (no matching labels) sub-bucket last:
	//   ATM:type:bug (1), ATM:type:refactor (2), ATM:type:task (1),
	//   (no matching labels) (1)
	wantLabels := []string{"ATM:type:bug", "ATM:type:refactor", "ATM:type:task", ""}
	if len(subs) != len(wantLabels) {
		t.Fatalf("subgroups: got %d want %d", len(subs), len(wantLabels))
	}
	for i, want := range wantLabels {
		if subs[i].label != want {
			t.Errorf("sub[%d].label = %q want %q", i, subs[i].label, want)
		}
	}
	// Multi-membership: ATM-0014 appears in both bug and refactor sub-groups.
	bug := subs[0]
	refactor := subs[1]
	if len(bug.rows) != 1 || bug.rows[0].id != "ATM-0014" {
		t.Errorf("ATM:type:bug rows = %+v want [ATM-0014]", bug.rows)
	}
	if len(refactor.rows) != 2 {
		t.Errorf("ATM:type:refactor rows = %d want 2 (multi-membership)", len(refactor.rows))
	}
	refactorIDs := map[string]bool{}
	for _, r := range refactor.rows {
		refactorIDs[r.id] = true
	}
	if !refactorIDs["ATM-0008"] || !refactorIDs["ATM-0014"] {
		t.Errorf("ATM:type:refactor rows missing multi-membership; got %v", refactorIDs)
	}
	// (no matching labels) sub-bucket holds the status-only task.
	none := subs[3]
	if len(none.rows) != 1 || none.rows[0].id != "ATM-0020" {
		t.Errorf("(no matching labels) sub rows = %+v want [ATM-0020]", none.rows)
	}
	// Leaf rows live at the deepest level only; sub-groups have no nested
	// children for a single remaining wildcard.
	for _, s := range subs {
		if s.subgroups != nil {
			t.Errorf("sub %q should have no further nesting, got %v", s.label, s.subgroups)
		}
	}
}

// TestBuildNestedGroupsThreeWildcards verifies deeper nesting (depth = number
// of wildcards). Top wildcard buckets into sub-groups; each sub-group buckets
// again by the next wildcard.
func TestBuildNestedGroupsThreeWildcards(t *testing.T) {
	tasks := []*store.Task{
		mkTask("ATM-0001", "a", "ATM:status:open", "ATM:type:bug", "ATM:prio:high"),
		mkTask("ATM-0002", "b", "ATM:status:open", "ATM:type:bug", "ATM:prio:low"),
		mkTask("ATM-0003", "c", "ATM:status:open", "ATM:type:task", "ATM:prio:high"),
	}
	wildcards := []string{"ATM:type:*", "ATM:prio:*"}
	subs := buildNestedGroups(tasks, wildcards, toRowTest)

	// Top level (ATM:type:*) : bug (2), task (1)
	if len(subs) != 2 {
		t.Fatalf("top sub-groups: got %d want 2", len(subs))
	}
	bug := subs[0]
	task := subs[1]
	if bug.label != "ATM:type:bug" || len(bug.rows) != 0 || len(bug.subgroups) != 2 {
		t.Errorf("bug group = %+v want 0 rows, 2 sub-groups", bug)
	}
	if task.label != "ATM:type:task" || len(task.subgroups) != 1 {
		t.Errorf("task group = %+v want 1 sub-group", task)
	}
	// bug sub-groups (ATM:prio:*): high (1), low (1)
	if bug.subgroups[0].label != "ATM:prio:high" || len(bug.subgroups[0].rows) != 1 {
		t.Errorf("bug/high = %+v want 1 row", bug.subgroups[0])
	}
	if bug.subgroups[1].label != "ATM:prio:low" || len(bug.subgroups[1].rows) != 1 {
		t.Errorf("bug/low = %+v want 1 row", bug.subgroups[1])
	}
}

// TestGroupLeafCountNested verifies the header count sums across nested
// sub-groups so a collapsed parent still reports its true bucket size.
func TestGroupLeafCountNested(t *testing.T) {
	subs := []taskGroup{
		{label: "ATM:type:bug", rows: []taskRow{{id: "1"}, {id: "2"}}},
		{label: "ATM:type:refactor", rows: []taskRow{{id: "3"}}},
	}
	g := taskGroup{label: "ATM:status:open", subgroups: subs}
	if got := groupLeafCount(g); got != 3 {
		t.Errorf("groupLeafCount = %d want 3", got)
	}
	// Collapsing must not change the reported count.
	g.collapsed = true
	if got := groupLeafCount(g); got != 3 {
		t.Errorf("groupLeafCount (collapsed) = %d want 3", got)
	}
}