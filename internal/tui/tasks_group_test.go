package tui

import (
	"fmt"
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

func TestGroupedBestRowLiftsStaleParent(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)

	// Create in sequence, with a sleep between each so distinct wall-clock
	// UpdatedAt values are guaranteed regardless of the store's timestamp
	// resolution: P oldest, then O (orphan, middle age), then C (newest).
	parent := seedTask(t, m, "ATM", "stale parent", "ATM:scrum:task")
	time.Sleep(2 * time.Millisecond)
	orphan := seedTask(t, m, "ATM", "orphan", "ATM:scrum:task")
	time.Sleep(2 * time.Millisecond)
	child := seedTask(t, m, "ATM", "fresh child", "ATM:scrum:task")
	linkPartOf(t, m, child.ID, parent.ID)

	m.refreshAll()
	m.lanes.selectDefault()
	if m.tasks.sortMode != sortUpdatedDesc {
		t.Fatalf("precondition: default sort must be updated-desc, got %v", m.tasks.sortMode)
	}
	m.tasks.grouped = true
	m.tasks.refresh()

	ids := rowIDs(m.tasks.rows)
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

func TestGroupedCycleDegradesToFlat(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)

	a := seedTask(t, m, "ATM", "cycle a", "ATM:scrum:task")
	b := seedTask(t, m, "ATM", "cycle b", "ATM:scrum:task")
	linkPartOf(t, m, a.ID, b.ID)
	linkPartOf(t, m, b.ID, a.ID)

	m.refreshAll()
	m.lanes.selectDefault()
	m.tasks.grouped = true

	done := make(chan struct{})
	go func() {
		m.tasks.refresh()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh() did not terminate — cycle caused a hang")
	}

	ids := rowIDs(m.tasks.rows)
	if len(ids) != 2 {
		t.Fatalf("rows = %v, want both cycle members present", ids)
	}
	// The first-encountered cycle member (store order: a before b) sits at
	// depth 0. The controller ruling: the partner MAY nest beneath it rather
	// than also rendering at depth 0.
	if got := rowByID(t, m, a.ID).depth; got != 0 {
		t.Fatalf("first cycle member (a) depth = %d, want 0", got)
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
