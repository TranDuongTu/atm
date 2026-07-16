package tui

import (
	"strings"
	"testing"

	"atm/internal/workflow"
)

func TestSplitStripWidths(t *testing.T) {
	prev, sel, next := splitStripWidths(80)
	if prev != 20 || sel != 40 || next != 20 {
		t.Errorf("splitStripWidths(80) = %d/%d/%d, want 20/40/20", prev, sel, next)
	}
}

func TestSplitStripWidthsClampsSmall(t *testing.T) {
	prev, sel, next := splitStripWidths(20)
	if prev < 6 || sel < 8 || next < 6 {
		t.Errorf("splitStripWidths(20) = %d/%d/%d, each must be >= minimum", prev, sel, next)
	}
	if prev+sel+next > 20 {
		t.Errorf("sum %d exceeds pane width 20", prev+sel+next)
	}
}

func TestRenderStripShowsSelectedOpenTasks(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	strip := m.boards.renderStrip(80, 8)
	if !strings.Contains(strip, "open-tasks") {
		t.Errorf("strip missing open-tasks:\n%s", strip)
	}
}

// TestRenderPinnedStackOneLinePerPinWithFullText verifies renderPinnedStack
// (replacing the old compact renderPinnedRow) emits one full-width line per
// pinned board, each showing the board's name AND description in full.
func TestRenderPinnedStackOneLinePerPinWithFullText(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := m.store.LabelAdd("ATM:next-sprint", "work slated for the next sprint", "status:open", m.actor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.LabelAdd("ATM:blocked-items", "tasks currently blocked", "status:blocked", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	m.boards.selected = "ATM:next-sprint"
	m.boards.togglePin()
	m.boards.selected = "ATM:blocked-items"
	m.boards.togglePin()

	stack := m.boards.renderPinnedStack(100)
	lines := strings.Split(stack, "\n")
	if len(lines) != 2 {
		t.Fatalf("renderPinnedStack lines = %d, want 2 (one per pin):\n%s", len(lines), stack)
	}
	if !strings.Contains(lines[0], "[1]") || !strings.Contains(lines[0], "next-sprint") || !strings.Contains(lines[0], "work slated for the next sprint") {
		t.Errorf("pin line 1 = %q, want [1], name, and full description", lines[0])
	}
	if !strings.Contains(lines[1], "[2]") || !strings.Contains(lines[1], "blocked-items") || !strings.Contains(lines[1], "tasks currently blocked") {
		t.Errorf("pin line 2 = %q, want [2], name, and full description", lines[1])
	}
}

// TestRenderPinnedStackEmptyWhenNoPins verifies the "" (no lines rendered)
// contract when nothing is pinned, matching the old renderPinnedRow behavior.
func TestRenderPinnedStackEmptyWhenNoPins(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.boards.refresh()
	if got := m.boards.renderPinnedStack(80); got != "" {
		t.Errorf("renderPinnedStack with no pins = %q, want empty", got)
	}
}
