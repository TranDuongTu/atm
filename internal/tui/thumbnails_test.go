package tui

import (
	"strings"
	"testing"

	"atm/internal/workflow"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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

// TestRenderPinnedStackBoxedPerPinWithFullText verifies renderPinnedStack
// emits one full-width, 3-line rounded box per pinned board (title "[N] name",
// description as the content line), stacked in pin order.
func TestRenderPinnedStackBoxedPerPinWithFullText(t *testing.T) {
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
	if len(lines) != 6 {
		t.Fatalf("renderPinnedStack lines = %d, want 6 (3 per pin, 2 pins):\n%s", len(lines), stack)
	}
	box1, box2 := strings.Join(lines[0:3], "\n"), strings.Join(lines[3:6], "\n")
	if !strings.Contains(box1, "[1]") || !strings.Contains(box1, "next-sprint") || !strings.Contains(box1, "work slated for the next sprint") {
		t.Errorf("pin box 1 = %q, want [1], name, and full description", box1)
	}
	if !strings.Contains(box2, "[2]") || !strings.Contains(box2, "blocked-items") || !strings.Contains(box2, "tasks currently blocked") {
		t.Errorf("pin box 2 = %q, want [2], name, and full description", box2)
	}
}

// TestActiveFilterHighlightOnStripWhenPinFocusIsStrip verifies the
// current-filter highlight (the strong, bold accent border) sits on the
// strip's SELECTED cell while pinFocus == -1, and that pinned boxes render
// muted (no bold) in that state.
func TestActiveFilterHighlightOnStripWhenPinFocusIsStrip(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault() // pinFocus == -1
	m.boards.togglePin()     // pin the SELECTED board (open-tasks)

	strip := m.boards.renderStrip(80, stripHeight)
	pinned := m.boards.renderPinnedStack(80)

	if !strings.Contains(strip, "\x1b[1") {
		t.Errorf("strip missing the strong (bold) highlight while pinFocus == -1:\n%s", strip)
	}
	if strings.Contains(pinned, "\x1b[1") {
		t.Errorf("pinned box should be muted while pinFocus == -1:\n%s", pinned)
	}
}

// TestActiveFilterHighlightMovesToPinOnJump verifies Shift-N moves the strong
// highlight from the strip onto the jumped-to pin box.
func TestActiveFilterHighlightMovesToPinOnJump(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

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
	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 1 pin")
	}

	strip := m.boards.renderStrip(80, stripHeight)
	pinned := m.boards.renderPinnedStack(80)

	if strings.Contains(strip, "\x1b[1") {
		t.Errorf("strip should be muted once a pin is the active filter:\n%s", strip)
	}
	if !strings.Contains(pinned, "\x1b[1") {
		t.Errorf("pinned box missing the strong (bold) highlight for the jumped-to pin:\n%s", pinned)
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
