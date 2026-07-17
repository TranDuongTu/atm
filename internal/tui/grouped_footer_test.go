package tui

import (
	"strings"
	"testing"

	"atm/internal/capability/workflow"
)

// TestGroupedListFooterVisibleWithPins guards the grouped-list off-by-one: the
// "showing X of Y" footer must not be truncated by padToHeight (and hidden by
// the pinned stack) when the grouped body fills the visible window.
func TestGroupedListFooterVisibleWithPins(t *testing.T) {
	m := newTestModel(t)
	workflow.EnsureVocabulary(m.store, "ATM", m.actor)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	// Many rows across statuses so the grouped body exceeds the window.
	for i := 0; i < 8; i++ {
		for _, s := range []string{"open", "todo", "done", "blocked", "in-progress"} {
			seedTask(t, m, "ATM", "t", "ATM:status:"+s)
		}
	}
	m.boards.refresh()
	m.boards.selectDefault()
	m.boards.togglePin() // 1 pin so the fixed slot is engaged
	// select the status:* namespace board -> grouped list
	for _, r := range m.boards.rows {
		if r.Expandable && r.Name == "status" {
			m.boards.selected = r.FullName
			m.boards.pinFocus = -1
			break
		}
	}
	m.boards.applyFocus()
	m.tasks.SetSize(100, 30)
	m.tasks.refresh()
	v := m.tasks.View()
	if !strings.Contains(v, "showing 1-") {
		t.Errorf("grouped list 'showing' footer missing from View (truncated + hidden by pins):\n%s", v)
	}
}
