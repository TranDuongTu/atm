package tui

import (
	"strings"
	"testing"
)

// TestRenderStripHeightClampedWithManyChartMembers guards against strip
// overflow: the SELECTED namespace board renders its member chart inside the
// strip, and a namespace with many members must NOT make the strip taller than
// stripHeight (titledBoxHeight truncates the cell body). Overflow here would
// break the Tasks pane border in the workspace.
func TestRenderStripHeightClampedWithManyChartMembers(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	for _, v := range []string{"open", "todo", "in-progress", "blocked", "done", "wontfix", "dup", "stale", "review", "qa"} {
		seedTask(t, m, "ATM", "t-"+v, "ATM:status:"+v)
	}
	m.boards.refresh()
	for _, r := range m.boards.rows {
		if r.Expandable && r.Name == "status" {
			m.boards.selected = r.FullName
			break
		}
	}
	m.boards.applyFocus()

	strip := m.boards.renderStrip(80, stripHeight)
	if got := strings.Count(strip, "\n") + 1; got != stripHeight {
		t.Errorf("strip rendered %d lines with 10 chart members, want exactly %d", got, stripHeight)
	}

	// The full list view (strip + list + optional pinned) must not exceed the
	// assigned content height.
	m.tasks.SetSize(80, 30)
	if vh := strings.Count(m.tasks.View(), "\n") + 1; vh > 30 {
		t.Errorf("tasks list View rendered %d lines, exceeds contentHeight 30", vh)
	}
}
