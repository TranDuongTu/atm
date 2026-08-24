package tui

import (
	"strings"
	"testing"
	"time"

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
	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	in := []*core.Task{
		{ID: "ATM-old", Title: "older", UpdatedAt: base},
		{ID: "ATM-new", Title: "newer", UpdatedAt: base.Add(time.Hour)},
	}
	if got := m.tasks.applySort(in); got[0].ID != "ATM-new" {
		t.Fatalf("first row = %s, want the most recently updated task first", got[0].ID)
	}
	m.tasks.sortMode = sortTitleAsc
	if got := m.tasks.applySort(in); got[0].Title != "newer" {
		t.Fatalf("title sort first row = %q, want %q", got[0].Title, "newer")
	}
}
