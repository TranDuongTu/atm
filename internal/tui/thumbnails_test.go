package tui

import (
	"fmt"
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
// emits a full-width, 3-line rounded box per pinned board (title
// "[Shift-N] name", description as the content line), stacked in pin order.
// The stack is a FIXED slot of exactly maxPins boxes; the trailing empty slot
// renders a muted placeholder rather than collapsing.
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
	if len(lines) != 3*maxPins {
		t.Fatalf("renderPinnedStack lines = %d, want %d (3 per slot, %d fixed slots):\n%s", len(lines), 3*maxPins, maxPins, stack)
	}
	box1, box2 := strings.Join(lines[0:3], "\n"), strings.Join(lines[3:6], "\n")
	if !strings.Contains(box1, "[Shift-1]") || !strings.Contains(box1, "next-sprint") || !strings.Contains(box1, "work slated for the next sprint") {
		t.Errorf("pin box 1 = %q, want [Shift-1], name, and full description", box1)
	}
	if !strings.Contains(box2, "[Shift-2]") || !strings.Contains(box2, "blocked-items") || !strings.Contains(box2, "tasks currently blocked") {
		t.Errorf("pin box 2 = %q, want [Shift-2], name, and full description", box2)
	}
	box3 := strings.Join(lines[6:9], "\n")
	if !strings.Contains(box3, "[Shift-3]") || !strings.Contains(box3, "empty") {
		t.Errorf("pin box 3 = %q, want the [Shift-3] empty placeholder", box3)
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

// TestRenderPinnedStackPlaceholdersWhenNoPins verifies the FIXED-slot
// contract: with nothing pinned the stack still renders exactly maxPins
// placeholder boxes (3*maxPins lines) so the task list height never shifts
// when the first board is pinned. Each empty slot advertises its Shift-N key
// and the [p]in affordance.
func TestRenderPinnedStackPlaceholdersWhenNoPins(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.boards.refresh()
	stack := m.boards.renderPinnedStack(80)
	lines := strings.Split(stack, "\n")
	if len(lines) != 3*maxPins {
		t.Fatalf("renderPinnedStack with no pins = %d lines, want %d (fixed slot):\n%s", len(lines), 3*maxPins, stack)
	}
	for n := 1; n <= maxPins; n++ {
		if !strings.Contains(stack, fmt.Sprintf("[Shift-%d]", n)) {
			t.Errorf("empty stack missing the [Shift-%d] placeholder slot:\n%s", n, stack)
		}
	}
	if !strings.Contains(stack, "empty") || !strings.Contains(stack, "pin a board with [p]") {
		t.Errorf("empty slots missing the placeholder text:\n%s", stack)
	}
}
