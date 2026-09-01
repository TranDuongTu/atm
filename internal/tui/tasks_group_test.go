package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"atm/internal/core"
)

// linkPartOf records a parent link through the real payload surface — the
// same path scrum's Recorder.writePayload uses (SetTaskCapabilityMeta with a
// full-replace JSON payload string).
func linkPartOf(t *testing.T, m *Model, childID, parentID string) {
	t.Helper()
	if err := m.store.SetTaskCapabilityMeta(childID, "scrum",
		fmt.Sprintf(`{"part_of":%q}`, parentID), testActor); err != nil {
		t.Fatal(err)
	}
}

// rowByID finds a row by id, failing the test if absent.
func rowByID(t *testing.T, m *Model, id string) taskRow {
	t.Helper()
	for _, r := range m.tasks.rows {
		if r.id == id {
			return r
		}
	}
	t.Fatalf("row %s not found in rows %v", id, rowIDs(m.tasks.rows))
	return taskRow{}
}

func TestGroupedChildNestsUnderParent(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)

	parent := seedTask(t, m, "ATM", "parent", "ATM:scrum:task")
	child := seedTask(t, m, "ATM", "child", "ATM:scrum:task")
	linkPartOf(t, m, child.ID, parent.ID)

	m.refreshAll()
	m.lanes.selectDefault()
	m.tasks.grouped = true
	m.tasks.refresh()

	ids := rowIDs(m.tasks.rows)
	pIdx, cIdx := -1, -1
	for i, id := range ids {
		if id == parent.ID {
			pIdx = i
		}
		if id == child.ID {
			cIdx = i
		}
	}
	if pIdx < 0 || cIdx < 0 {
		t.Fatalf("expected both rows present, got %v", ids)
	}
	if cIdx != pIdx+1 {
		t.Fatalf("expected child directly after parent, got order %v", ids)
	}
	if got := rowByID(t, m, parent.ID).depth; got != 0 {
		t.Fatalf("parent depth = %d, want 0", got)
	}
	if got := rowByID(t, m, child.ID).depth; got != 1 {
		t.Fatalf("child depth = %d, want 1", got)
	}
	if rowByID(t, m, parent.ID).synthetic {
		t.Fatalf("parent should not be synthetic (it is in the lane's own result set)")
	}
}

// TestGroupedBestRowLiftsStaleParent exercises bestRow's freshness
// comparator directly: rows are built by hand with explicit UpdatedAt
// values (the store round-trip truncates UpdatedAt to whole seconds —
// internal/store/cache.go's RFC3339 encoding has no sub-second field — so
// seeding through the store and racing that resolution with sleeps cannot
// reliably separate timestamps within one test). applyGrouping is called
// directly over the hand-built rows, mirroring the fixture pattern at
// tasks_columns_test.go:140-148 (sortFixtureRows).
func TestGroupedBestRowLiftsStaleParent(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.refreshAll()
	m.lanes.selectDefault() // scopes annReg + capability.current ("scrum")
	if m.tasks.sortMode != sortUpdatedDesc {
		t.Fatalf("precondition: default sort must be updated-desc, got %v", m.tasks.sortMode)
	}

	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	parent := &core.Task{ID: "ATM-parent", Title: "stale parent", UpdatedAt: base}
	orphan := &core.Task{ID: "ATM-orphan", Title: "orphan", UpdatedAt: base.Add(time.Hour)}
	child := &core.Task{
		ID: "ATM-child", Title: "fresh child", UpdatedAt: base.Add(2 * time.Hour),
		Meta: map[string]string{"scrum": `{"part_of":"ATM-parent"}`},
	}
	rows := []taskRow{
		{id: parent.ID, title: parent.Title, task: parent},
		{id: orphan.ID, title: orphan.Title, task: orphan},
		{id: child.ID, title: child.Title, task: child},
	}

	got := m.tasks.applyGrouping(m.tasks.applySort(rows))

	// The group's best row is the fresh child (newest), so it outranks the
	// orphan even though the parent itself is the oldest of the three. If
	// bestRow degenerated to "always the node's own row" (ignoring
	// children), the group's best would be the stale parent and the orphan
	// would sort ahead of it — this assertion would then fail.
	ids := rowIDs(got)
	want := []string{parent.ID, child.ID, orphan.ID}
	if len(ids) != len(want) {
		t.Fatalf("rows = %v, want ids matching %v", ids, want)
	}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("rows[%d] = %s, want %s (order %v)", i, ids[i], id, ids)
		}
	}
}

func TestGroupedDanglingParentLeavesChildTopLevel(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)

	child := seedTask(t, m, "ATM", "orphaned child", "ATM:scrum:task")
	linkPartOf(t, m, child.ID, "ATM-zzzzzz")

	m.refreshAll()
	m.lanes.selectDefault()
	m.tasks.grouped = true
	m.tasks.refresh()

	row := rowByID(t, m, child.ID)
	if row.depth != 0 {
		t.Fatalf("child depth = %d, want 0 (dangling parent)", row.depth)
	}
	if row.synthetic {
		t.Fatalf("child should not be synthetic")
	}
}

func TestGroupedOutOfSetParentSynthesized(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)

	// Parent left unclaimed by scrum: it has no ATM:scrum:* label, so it
	// sits in the Inbox lane, not Pipeline.
	parent := seedTask(t, m, "ATM", "unclaimed parent")
	// Child claimed as a task: it has ATM:scrum:task, so it is in Pipeline.
	child := seedTask(t, m, "ATM", "claimed child", "ATM:scrum:task")
	linkPartOf(t, m, child.ID, parent.ID)

	m.refreshAll()
	m.lanes.selectDefault() // focuses Pipeline: parent is out of this result set
	if m.tasks.filter == "" {
		t.Fatalf("precondition: lane focus must set a filter")
	}
	m.tasks.grouped = true
	m.tasks.refresh()

	childRow := rowByID(t, m, child.ID)
	if childRow.depth != 1 {
		t.Fatalf("child depth = %d, want 1 (nested under synthesized parent)", childRow.depth)
	}
	parentRow := rowByID(t, m, parent.ID)
	if parentRow.depth != 0 {
		t.Fatalf("synthesized parent depth = %d, want 0", parentRow.depth)
	}
	if !parentRow.synthetic {
		t.Fatalf("parent row should be synthetic (out of the lane's own result set)")
	}
}

// TestGroupedCycleDegradesToFlat builds its two rows by hand rather than
// seeding through the store: which of the pair is "first-encountered" would
// otherwise ride on store/creation order, which the store's whole-second
// UpdatedAt truncation (internal/store/cache.go) can leave ambiguous. The
// assertions below therefore avoid depending on which member comes first —
// they only require that the cycle degrades safely: no row lost, no hang,
// and exactly one member anchors the tree at depth 0 (the other may nest
// beneath it, per the controller's ruling on this test).
func TestGroupedCycleDegradesToFlat(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.refreshAll()
	m.lanes.selectDefault() // scopes annReg + capability.current ("scrum")

	a := &core.Task{ID: "ATM-a", Title: "cycle a", Meta: map[string]string{"scrum": `{"part_of":"ATM-b"}`}}
	b := &core.Task{ID: "ATM-b", Title: "cycle b", Meta: map[string]string{"scrum": `{"part_of":"ATM-a"}`}}
	rows := []taskRow{
		{id: a.ID, title: a.Title, task: a},
		{id: b.ID, title: b.Title, task: b},
	}

	done := make(chan []taskRow, 1)
	go func() {
		done <- m.tasks.applyGrouping(m.tasks.applySort(rows))
	}()
	var got []taskRow
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("applyGrouping did not terminate — cycle caused a hang")
	}

	ids := rowIDs(got)
	if len(ids) != 2 {
		t.Fatalf("rows = %v, want both cycle members present", ids)
	}
	depth0 := 0
	for _, r := range got {
		if r.depth == 0 {
			depth0++
		}
	}
	if depth0 != 1 {
		t.Fatalf("want exactly one cycle member at depth 0, got %d: %+v", depth0, got)
	}
}

// TestGroupToggleKeyAndIndentRender exercises the 't' key end to end: it
// toggles tasksModel.grouped, a refresh nests the child under the parent, and
// the rendered ID cell carries the "↳ " tree marker one indent level in.
// Pressing 't' again must return to the flat list with no marker at all.
func TestGroupToggleKeyAndIndentRender(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.SetSize(120, 40)

	parent := seedTask(t, m, "ATM", "parent", "ATM:scrum:task")
	child := seedTask(t, m, "ATM", "child", "ATM:scrum:task")
	linkPartOf(t, m, child.ID, parent.ID)

	m.refreshAll()
	m.lanes.selectDefault()

	m.tasks.handleKey(keyMsg("t"))
	if !m.tasks.grouped {
		t.Fatalf("t must toggle grouped on")
	}

	out := stripANSI(m.tasks.View())
	lines := strings.Split(out, "\n")
	parentIdx, childIdx := -1, -1
	for i, ln := range lines {
		if parentIdx < 0 && strings.Contains(ln, parent.ID) {
			parentIdx = i
		}
		if strings.Contains(ln, "↳ "+child.ID) {
			childIdx = i
		}
	}
	if parentIdx < 0 || childIdx < 0 {
		t.Fatalf("expected parent line and an indented %q child line, got:\n%s", "↳ "+child.ID, out)
	}
	if childIdx != parentIdx+1 {
		t.Fatalf("expected child line directly below parent (parent@%d, child@%d):\n%s", parentIdx, childIdx, out)
	}

	m.tasks.handleKey(keyMsg("t"))
	if m.tasks.grouped {
		t.Fatalf("second t must toggle grouped off")
	}
	if out2 := stripANSI(m.tasks.View()); strings.Contains(out2, "↳") {
		t.Fatalf("flat view must not show the tree marker:\n%s", out2)
	}
}

// TestSyntheticParentRendersInList covers the out-of-set parent case from
// applyGrouping: a parent outside the lane's own result set is synthesized
// as a real, selectable row, and the toggle-driven render must show it (and
// the child nested under it) even though the lane filter excludes it.
func TestSyntheticParentRendersInList(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.SetSize(120, 40)

	// Parent left unclaimed by scrum: it has no ATM:scrum:* label, so it
	// sits in the Inbox lane, not Pipeline.
	parent := seedTask(t, m, "ATM", "unclaimed parent")
	// Child claimed as a task: it has ATM:scrum:task, so it is in Pipeline.
	child := seedTask(t, m, "ATM", "claimed child", "ATM:scrum:task")
	linkPartOf(t, m, child.ID, parent.ID)

	m.refreshAll()
	m.lanes.selectDefault() // focuses Pipeline: parent is out of this result set
	if m.tasks.filter == "" {
		t.Fatalf("precondition: lane focus must set a filter")
	}

	m.tasks.handleKey(keyMsg("t"))

	out := stripANSI(m.tasks.View())
	if !strings.Contains(out, parent.ID) {
		t.Fatalf("expected synthesized parent %s to appear in the grouped list:\n%s", parent.ID, out)
	}
	if !strings.Contains(out, "↳ "+child.ID) {
		t.Fatalf("expected child %s indented under the synthesized parent:\n%s", child.ID, out)
	}
}

func TestUngroupedUnchanged(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)

	parent := seedTask(t, m, "ATM", "parent", "ATM:scrum:task")
	child := seedTask(t, m, "ATM", "child", "ATM:scrum:task")
	linkPartOf(t, m, child.ID, parent.ID)

	m.refreshAll()
	m.lanes.selectDefault()

	m.tasks.grouped = false
	m.tasks.refresh()
	ungroupedIDs := rowIDs(m.tasks.rows)

	// Compare directly against applySort's output over the same scope/filter.
	filters := core.ParseFilter(m.tasks.filter)
	tasks := m.store.ListTasks(core.QueryFilters{Project: m.projectScope, Labels: filters})
	rows := make([]taskRow, 0, len(tasks))
	for _, tk := range tasks {
		rows = append(rows, m.tasks.toRow(tk))
	}
	want := m.tasks.applySort(rows)
	if len(ungroupedIDs) != len(want) {
		t.Fatalf("ungrouped rows = %v, want same length as applySort output %v", ungroupedIDs, want)
	}
	for i, r := range want {
		if ungroupedIDs[i] != r.id {
			t.Fatalf("ungrouped rows[%d] = %s, want %s (applySort order)", i, ungroupedIDs[i], r.id)
		}
		if m.tasks.rows[i].depth != 0 || m.tasks.rows[i].synthetic {
			t.Fatalf("ungrouped rows[%d] has depth/synthetic set: %+v", i, m.tasks.rows[i])
		}
	}
}
