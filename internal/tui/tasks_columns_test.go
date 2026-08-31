package tui

import (
	"strings"
	"testing"
	"time"

	"atm/internal/capability"
	"atm/internal/core"
)

// columnHeader returns the ID/TITLE/ANNOTATE/UPDATED header row of the task
// list, which is the line naming the ID column.
func columnHeader(t *testing.T, m *Model) string {
	t.Helper()
	for _, ln := range strings.Split(stripANSI(m.tasks.View()), "\n") {
		if strings.Contains(ln, "ID") && strings.Contains(ln, "TITLE") {
			return ln
		}
	}
	t.Fatalf("no column header in the task list:\n%s", stripANSI(m.tasks.View()))
	return ""
}

func newColumnsTestModel(t *testing.T) *Model {
	t.Helper()
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.SetSize(120, 40)
	seedTask(t, m, "ATM", "older claimed", "ATM:scrum:task", "ATM:scrum-stage:building")
	seedTask(t, m, "ATM", "newer claimed", "ATM:scrum:bug", "ATM:scrum-stage:building")
	m.refreshAll()
	m.lanes.selectDefault()
	return m
}

func TestTaskListSortIndicatorSitsOnTheSortedColumn(t *testing.T) {
	m := newColumnsTestModel(t)

	if got := columnHeader(t, m); !strings.Contains(got, "UPDATED ↓") {
		t.Fatalf("default header = %q, want the UPDATED column marked ↓", got)
	}
	m.tasks.handleKey(keyMsg("s"))
	if got := columnHeader(t, m); !strings.Contains(got, "UPDATED ↑") {
		t.Fatalf("after one [s] header = %q, want UPDATED ↑", got)
	}
	m.tasks.handleKey(keyMsg("s"))
	got := columnHeader(t, m)
	if !strings.Contains(got, "ID ↑") {
		t.Fatalf("after two [s] header = %q, want the indicator on ID", got)
	}
	if strings.Contains(got, "UPDATED ↑") || strings.Contains(got, "UPDATED ↓") {
		t.Fatalf("the indicator stayed on UPDATED after the sort moved: %q", got)
	}
	m.tasks.handleKey(keyMsg("s"))
	if got := columnHeader(t, m); !strings.Contains(got, "TITLE ↑") {
		t.Fatalf("after three [s] header = %q, want the indicator on TITLE", got)
	}
	m.tasks.handleKey(keyMsg("s"))
	if got := columnHeader(t, m); !strings.Contains(got, "UPDATED ↓") {
		t.Fatalf("the sort cycle did not return to updated-desc: %q", got)
	}
}

func TestTaskListHasTheFourFixedColumns(t *testing.T) {
	m := newColumnsTestModel(t)
	head := columnHeader(t, m)
	for _, col := range []string{"ID", "TITLE", "ANNOTATE", "UPDATED"} {
		if !strings.Contains(head, col) {
			t.Fatalf("column header %q missing %s", head, col)
		}
	}
}

func TestTaskListDropsTheOldCapabilityHeaderRow(t *testing.T) {
	m := newColumnsTestModel(t)
	v := stripANSI(m.tasks.View())
	for _, gone := range []string{"CAPABILITY:", "TOTAL:", "SORT:"} {
		if strings.Contains(v, gone) {
			t.Fatalf("task list still renders the old %q header row:\n%s", gone, v)
		}
	}
}

func TestPaneTitleCarriesTheCurrentCapability(t *testing.T) {
	m := newColumnsTestModel(t)
	if got := m.tasksPaneTitle(); !strings.Contains(got, "scrum") {
		t.Fatalf("pane title = %q, want the current capability named", got)
	}
}

func TestTaskListRendersDashForAnUnannotatedTask(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.SetSize(120, 40)
	// Unclaimed work: scrum has nothing to say about it, so its Cell is nil.
	seedTask(t, m, "ATM", "undecided")
	m.refreshAll()
	m.lanes.selectDefault()
	m.lanes.move(-1) // Inbox

	var row string
	for _, ln := range strings.Split(stripANSI(m.tasks.View()), "\n") {
		if strings.Contains(ln, "undecided") {
			row = ln
			break
		}
	}
	if row == "" {
		t.Fatalf("the unclaimed task is not in the Inbox lane:\n%s", stripANSI(m.tasks.View()))
	}
	if !strings.Contains(row, "—") {
		t.Fatalf("row %q does not render the nil-annotation dash", row)
	}
}

// TestTaskListDefaultsToMostRecentlyUpdatedFirst pins the DEFAULT sort mode
// against applySort rather than against a seeded fixture: two tasks created
// in the same test land in the same persisted second, so a fixture could only
// prove the tie-break, not the ordering.
func TestTaskListDefaultsToMostRecentlyUpdatedFirst(t *testing.T) {
	m := newColumnsTestModel(t)
	in := sortFixtureRows()
	if got := m.tasks.applySort(in); got[0].id != "ATM-new" {
		t.Fatalf("first row = %s, want the most recently updated task first", got[0].id)
	}
	m.tasks.sortMode = sortTitleAsc
	if got := m.tasks.applySort(in); got[0].title != "newer" {
		t.Fatalf("title sort first row = %q, want %q", got[0].title, "newer")
	}
}

// sortFixtureRows is the shared comparator fixture: an older and a newer row,
// each with a cell, built the way refresh builds them.
func sortFixtureRows() []taskRow {
	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	older := &core.Task{ID: "ATM-old", Title: "older", UpdatedAt: base}
	newer := &core.Task{ID: "ATM-new", Title: "newer", UpdatedAt: base.Add(time.Hour)}
	return []taskRow{
		{id: older.ID, title: older.Title, cell: &capability.Cell{Text: "older cell"}, task: older},
		{id: newer.ID, title: newer.Title, cell: &capability.Cell{Text: "newer cell"}, task: newer},
	}
}

// Tasks written in the same second share a timestamp, so the UPDATED sorts
// must leave rows they cannot separate in the store's order.
func TestApplySortIsStableAcrossEqualTimestamps(t *testing.T) {
	m := newColumnsTestModel(t)
	same := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	var in []taskRow
	for _, id := range []string{"ATM-a", "ATM-b", "ATM-c", "ATM-d"} {
		in = append(in, taskRow{id: id, title: id, task: &core.Task{ID: id, Title: id, UpdatedAt: same}})
	}

	for _, mode := range []sortMode{sortUpdatedDesc, sortUpdatedAsc} {
		m.tasks.sortMode = mode
		for i, got := range m.tasks.applySort(in) {
			if got.id != in[i].id {
				t.Fatalf("%v: row[%d] = %s, want %s (equal timestamps must keep store order)", mode, i, got.id, in[i].id)
			}
		}
	}
}

// The seam the ANNOTATE sort needs: a cell travels with its row through the
// sort, so a rank comparator has something to read.
func TestApplySortMovesWholeRowsWithTheirCells(t *testing.T) {
	m := newColumnsTestModel(t)

	got := m.tasks.applySort(sortFixtureRows())

	if got[0].id != "ATM-new" {
		t.Fatalf("first row = %s, want the most recently updated row first", got[0].id)
	}
	if got[0].cell == nil || got[0].cell.Text != "newer cell" {
		t.Fatalf("the cell did not travel with its row: %+v", got[0].cell)
	}
}

// id-asc is a no-op that leans on the store's own order; the restructure must
// not disturb it.
func TestTaskListIDSortPreservesStoreOrder(t *testing.T) {
	m := newColumnsTestModel(t)
	m.tasks.sortMode = sortIDAsc
	m.tasks.refresh()

	want := m.store.ListTasks(core.QueryFilters{Project: "ATM", Labels: core.ParseFilter(m.tasks.filter)})
	if len(want) < 2 {
		t.Fatalf("fixture has %d tasks in the lane; need at least 2 to pin an order", len(want))
	}
	if len(m.tasks.rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(m.tasks.rows), len(want))
	}
	for i, tk := range want {
		if m.tasks.rows[i].id != tk.ID {
			t.Fatalf("row[%d] = %s, want %s (store order must survive id-asc)", i, m.tasks.rows[i].id, tk.ID)
		}
	}
}
