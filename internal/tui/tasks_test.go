package tui

import (
	"strings"
	"testing"

	"atm/internal/store"
	"atm/internal/workflow"
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

func containsLabelTUI(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func TestTaskCreateWithLabelsField(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // select ATM
	update(t, m, "2") // Tasks pane
	update(t, m, "a") // open create form
	if m.form == nil {
		t.Fatalf("create form not open")
	}
	// Verify the labels field exists.
	found := false
	for _, f := range m.form.Fields {
		if f.Label == "labels" {
			found = true
		}
	}
	if !found {
		t.Fatalf("create form has no 'labels' field; fields = %+v", m.form.Fields)
	}
	// Type a title.
	for _, r := range "Multi-label task" {
		update(t, m, string(r))
	}
	update(t, m, "tab") // title -> description
	// Skip description (leave empty), tab to labels.
	update(t, m, "tab") // description -> labels
	for _, r := range "status:open type:bug" {
		update(t, m, string(r))
	}
	update(t, m, "enter") // submit (last field)
	if m.form != nil {
		t.Fatalf("form should be closed after submit")
	}
	// The task should exist with both labels.
	ts := m.store.ListTasks(store.QueryFilters{Project: "ATM"})
	if len(ts) != 1 {
		t.Fatalf("expected 1 task, got %d", len(ts))
	}
	tk := ts[0]
	if !containsLabelTUI(tk.Labels, "ATM:status:open") || !containsLabelTUI(tk.Labels, "ATM:type:bug") {
		t.Fatalf("task labels = %v, want ATM:status:open + ATM:type:bug", tk.Labels)
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

func TestFilterTokenHelpers(t *testing.T) {
	if got := facetToken("ATM", "status"); got != "ATM:status:*" {
		t.Fatalf("facetToken = %q want ATM:status:*", got)
	}
	if !filterHasToken("ATM:status:* ATM:type:*", "ATM:type:*") {
		t.Fatalf("filterHasToken should find ATM:type:*")
	}
	if filterHasToken("ATM:status:*", "ATM:type:*") {
		t.Fatalf("filterHasToken should not find absent token")
	}
	if got := filterAddToken("ATM:status:*", "ATM:type:*"); got != "ATM:status:* ATM:type:*" {
		t.Fatalf("filterAddToken = %q want two tokens", got)
	}
	if got := filterAddToken("ATM:status:*", "ATM:status:*"); got != "ATM:status:*" {
		t.Fatalf("filterAddToken should not duplicate, got %q", got)
	}
	if got := filterAddToken("", "ATM:status:*"); got != "ATM:status:*" {
		t.Fatalf("filterAddToken onto empty = %q want ATM:status:*", got)
	}
	if got := filterRemoveToken("ATM:status:* ATM:type:*", "ATM:status:*"); got != "ATM:type:*" {
		t.Fatalf("filterRemoveToken = %q want ATM:type:*", got)
	}
	if got := filterRemoveToken("ATM:status:*", "ATM:status:*"); got != "" {
		t.Fatalf("filterRemoveToken last token = %q want empty", got)
	}
}

func TestTaskHasBareTag(t *testing.T) {
	mk := func(labels ...string) *store.Task { return &store.Task{ID: "ATM-0001", Labels: labels} }
	if taskHasBareTag("ATM", mk("ATM:status:open")) {
		t.Error("namespaced label must not count as a bare tag")
	}
	if !taskHasBareTag("ATM", mk("ATM:urgent")) {
		t.Error("unnamespaced label must count as a bare tag")
	}
	if taskHasBareTag("ATM", mk()) {
		t.Error("no labels means no bare tag")
	}
	if !taskHasBareTag("ATM", mk("ATM:status:open", "ATM:urgent")) {
		t.Error("mixed labels with one bare tag must count")
	}
}

func TestTasksFocusRendersSubset(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	mustCreate := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mustCreate("has-open", "ATM:status:open")
	mustCreate("has-done", "ATM:status:done")
	mustCreate("prio-no-status", "ATM:priority:high")
	mustCreate("bare", "ATM:urgent")
	mustCreate("naked")
	m.projectScope = "ATM"

	// present on status -> grouped, only tasks with a status (others hidden).
	m.tasks.setFocus(taskFocus{mode: focusPresent, ns: "status"}, "ATM:status:*")
	v := m.tasks.View()
	mustContain(t, v, "has-open")
	mustContain(t, v, "has-done")
	mustNotContain(t, v, "prio-no-status")
	mustNotContain(t, v, "naked")
	mustNotContain(t, v, "(no matching labels)")

	// absent on status -> only tasks lacking a status.
	m.tasks.setFocus(taskFocus{mode: focusAbsent, ns: "status"}, "ATM:status:*")
	v = m.tasks.View()
	mustContain(t, v, "prio-no-status")
	mustContain(t, v, "bare")
	mustContain(t, v, "naked")
	mustNotContain(t, v, "has-open")

	// present on bare tags -> only tasks carrying a bare tag.
	m.tasks.setFocus(taskFocus{mode: focusPresent, bareTags: true}, "")
	v = m.tasks.View()
	mustContain(t, v, "bare")
	mustNotContain(t, v, "has-open")
	mustNotContain(t, v, "naked")

	// unlabeled -> only the naked task.
	m.tasks.setFocus(taskFocus{mode: focusUnlabeled}, "")
	v = m.tasks.View()
	mustContain(t, v, "naked")
	mustNotContain(t, v, "has-open")
	mustNotContain(t, v, "bare")

	// off with empty filter -> everything.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	v = m.tasks.View()
	mustContain(t, v, "has-open")
	mustContain(t, v, "naked")
	mustContain(t, v, "bare")
}

// TestTasksPaneRendersStripAndPinnedRow verifies the Tasks pane list view
// renders the board thumbnail strip above the task list (Task 7: merging the
// Boards pane into Tasks).
func TestTasksPaneRendersStripAndPinnedRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	v := m.tasks.View()
	// "open-tasks" already appears once via the header FOCUS caption (the
	// board's filter token, ATM:open-tasks); the strip renders the board name
	// again as its thumbnail title, so a passing render must contain it at
	// least twice.
	if got := strings.Count(v, "open-tasks"); got < 2 {
		t.Errorf("tasks view missing strip board name (got %d occurrences, want >= 2):\n%s", got, v)
	}
}

// TestBracketKeysSwitchBoard verifies "["/"]" cycle the board ring from the
// Tasks pane (relocated from task-list paging, which now lives on
// pgup/pgdown).
func TestBracketKeysSwitchBoard(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	// Two distinct namespaces so the ring has more than one entry to cycle
	// (matches the established pattern in TestCycleBoardMovesRing).
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "high one", "ATM:priority:high")
	m.boards.refresh()
	m.boards.selectDefault()
	first := m.boards.selected
	m.tasks.handleKey(keyMsg("]"))
	if m.boards.selected == first {
		t.Error("] did not advance the board ring")
	}
	m.tasks.handleKey(keyMsg("["))
	if m.boards.selected != first {
		t.Errorf("[ did not return to first board: got %q want %q", m.boards.selected, first)
	}
}

func TestTasksFocusPresentEmptyNamespace(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedLabel(t, m, "ATM:comment:open-question", "comment-only label")
	m.projectScope = "ATM"

	m.tasks.setFocus(taskFocus{mode: focusPresent, ns: "comment"}, "ATM:comment:*")
	v := m.tasks.View()
	mustContain(t, v, "no tasks match this focus")
	mustNotContain(t, v, "showing 1-1 of 1")
}

// TestListHintOrderPutsNavFirstAndInspectLast verifies the reordered [2] pane
// list-view hint: task/board nav and the everyday actions come first, with
// the ">" drill relegated to last since it is a hint-only reflection of the
// existing "> / <" keys (no new key introduced).
func TestListHintOrderPutsNavFirstAndInspectLast(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	want := "[↑/↓]tasks  [ [ / ] ]board  [s]ort  [a]dd  [p]in  [Enter]detail  [>]inspect board  [?]keys"
	if got := m.tasks.statusHint(); got != want {
		t.Errorf("statusHint() = %q, want %q", got, want)
	}
}

// TestListViewLayoutOrderListPinsStripBottom verifies change 5's layout
// inversion: top-to-bottom the list view now stacks task list -> pinned
// stack -> board strip, so the strip is the LAST stripHeight lines of the
// rendered pane and the pinned pill sits directly above it.
func TestListViewLayoutOrderListPinsStripBottom(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	m.boards.togglePin()
	m.SetSize(100, 40)

	view := m.tasks.View()
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "PROJECT:") {
		t.Fatalf("first line = %q, want the task list header first", lines[0])
	}
	stripBlock := strings.Join(lines[len(lines)-stripHeight:], "\n")
	if !strings.Contains(stripBlock, "open-tasks") {
		t.Errorf("last %d lines missing the board strip:\n%s", stripHeight, stripBlock)
	}
	pinBlock := strings.Join(lines[len(lines)-stripHeight-3:len(lines)-stripHeight], "\n")
	if !strings.Contains(pinBlock, "open-tasks") {
		t.Errorf("3 lines directly above the strip = %q, want the pinned open-tasks box", pinBlock)
	}
}

// TestListPageSizeShrinksAsPinsAdded verifies listPageSize derives from the
// live pin count (via listContentHeight) rather than a value cached at
// SetSize, so pgup/pgdown keep landing on the page boundary the renderer
// actually draws as pins are added. Each pin is a 3-line box, so one pin
// shrinks the page by 3.
func TestListPageSizeShrinksAsPinsAdded(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	m.SetSize(100, 40)

	before := m.tasks.listPageSize()
	m.boards.togglePin()
	after := m.tasks.listPageSize()
	if after != before-3 {
		t.Errorf("listPageSize after pinning 1 board = %d, want %d (3 less than unpinned %d)", after, before-3, before)
	}
}
