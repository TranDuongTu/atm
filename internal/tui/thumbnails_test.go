package tui

import (
	"fmt"
	"strings"
	"testing"

	"atm/internal/capability/workflow"
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

func TestRenderStripShowsSelectedAllTasks(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	strip := m.boards.renderStrip(80, 8)
	if !strings.Contains(strip, "all-tasks") {
		t.Errorf("strip missing all-tasks (the default-selected board):\n%s", strip)
	}
}

// TestRenderPinnedTabsShowsTabsAndActiveDescription verifies the single tabbed
// pinned box: a fixed-height (pinnedBoxHeight) box whose top border carries the
// four KEY tabs (Shift-0 = center board, Shift-1..3 = pins) and whose body line
// names the ACTIVE board and shows its description. With pinFocus == -1 the
// active board is the center (b.selected).
func TestRenderPinnedTabsShowsTabsAndActiveDescription(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// Pin two workflow-exposed boards: open-tasks and in-progress-tasks.
	m.boards.refresh()
	m.boards.selected = "ATM:open-tasks"
	m.boards.togglePin()
	m.boards.selected = "ATM:in-progress-tasks"
	m.boards.togglePin()
	// Focus the center board (the ring selection), not a pin.
	m.boards.selected = "ATM:open-tasks"
	m.boards.pinFocus = -1

	box := m.boards.renderPinnedTabs(100)
	lines := strings.Split(box, "\n")
	if len(lines) != pinnedBoxHeight {
		t.Fatalf("renderPinnedTabs lines = %d, want %d (single fixed box):\n%s", len(lines), pinnedBoxHeight, box)
	}
	for n := 0; n <= maxPins; n++ {
		if !strings.Contains(box, fmt.Sprintf("Shift-%d", n)) {
			t.Errorf("tabbed box missing the Shift-%d tab:\n%s", n, box)
		}
	}
	if !strings.Contains(box, "open-tasks") {
		t.Errorf("tabbed box body should name the active board:\n%s", box)
	}
	// open-tasks's workflow-seeded description must show in the body.
	l, err := m.store.LabelShow("ATM:open-tasks")
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if l.Description == "" {
		t.Fatal("open-tasks has no seeded description")
	}
	if !strings.Contains(box, l.Description) {
		t.Errorf("tabbed box body should show the active board's description:\n%s", box)
	}
}

// TestPinnedTabsHighlightsCenterTabWhenPinFocusIsStrip verifies exactly one tab
// carries the strong (bold accent) style: with pinFocus == -1 it is the Shift-0
// (center-board) tab, and the pin tabs are NOT strong-highlighted. The strip's
// SELECTED cell no longer carries the strong border (the tab bar is the sole
// active-filter indicator).
func TestPinnedTabsHighlightsCenterTabWhenPinFocusIsStrip(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	m.boards.selected = "ATM:open-tasks"
	m.boards.togglePin()
	m.boards.pinFocus = -1 // center board is the active filter

	box := m.boards.renderPinnedTabs(100)
	if !strings.Contains(box, m.styles.PaneActiveStrong.Render("Shift-0")) {
		t.Errorf("Shift-0 tab should carry the strong highlight while pinFocus == -1:\n%s", box)
	}
	if strings.Contains(box, m.styles.PaneActiveStrong.Render("Shift-1")) {
		t.Errorf("Shift-1 tab must NOT be strong-highlighted while pinFocus == -1:\n%s", box)
	}

	// The strip's SELECTED cell must not carry the strong (bold) border: the
	// highlight now lives on the tab bar, not the strip.
	sel := m.boards.renderSelectedCell(40, stripHeight, m.boards.rows[m.boards.ringIndex()])
	top := strings.SplitN(sel, "\n", 2)[0]
	if strings.Contains(top, "\x1b[1") {
		t.Errorf("strip SELECTED cell top border should not be strong/bold:\n%q", top)
	}
	if !strings.Contains(sel, "[Shift-0]") {
		t.Errorf("strip SELECTED cell should still carry the [Shift-0] label:\n%s", sel)
	}
}

// TestSelectedCellAlwaysShowsShiftZeroLabel verifies the strip's SELECTED
// cell carries a permanent "[Shift-0]" label — the key that (re)focuses it —
// mirroring the pinned boxes' permanent "[Shift-N]" labels. Unlike the
// "to inspect" hint, this label must show regardless of pinFocus: it
// documents the key, not the current highlight state.
func TestSelectedCellAlwaysShowsShiftZeroLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault() // pinFocus == -1: the SELECTED cell holds the highlight

	strip := m.boards.renderStrip(80, stripHeight)
	if !strings.Contains(strip, "[Shift-0]") {
		t.Errorf("SELECTED cell missing the [Shift-0] label while pinFocus == -1:\n%s", strip)
	}

	// Jump focus to a pin: the label must still show on the (now muted)
	// SELECTED cell — it names the key, independent of the highlight.
	m.boards.togglePin()
	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 1 pin")
	}
	strip = m.boards.renderStrip(80, stripHeight)
	if !strings.Contains(strip, "[Shift-0]") {
		t.Errorf("SELECTED cell lost the [Shift-0] label once a pin holds the highlight:\n%s", strip)
	}
}

// TestPinnedTabsMovesHighlightToPinTabOnJump verifies Shift-N moves the strong
// highlight onto the jumped-to pin's tab (and the body shows that pin's
// description); the center and the other Shift-N tabs are no longer strong.
func TestPinnedTabsMovesHighlightToPinTabOnJump(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	m.boards.selected = "ATM:open-tasks"
	m.boards.togglePin()
	m.boards.selected = "ATM:in-progress-tasks"
	m.boards.togglePin()
	if !m.boards.jumpPin(1) { // pinFocus == 0, active board == pins[0]
		t.Fatal("jumpPin(1) returned false with 2 pins")
	}

	box := m.boards.renderPinnedTabs(100)
	if !strings.Contains(box, m.styles.PaneActiveStrong.Render("Shift-1")) {
		t.Errorf("Shift-1 tab should carry the strong highlight after jumpPin(1):\n%s", box)
	}
	if strings.Contains(box, m.styles.PaneActiveStrong.Render("Shift-2")) {
		t.Errorf("Shift-2 tab must NOT be strong-highlighted after jumpPin(1):\n%s", box)
	}
	if strings.Contains(box, m.styles.PaneActiveStrong.Render("Shift-0")) {
		t.Errorf("Shift-0 (center) tab must NOT be strong-highlighted after a pin jump:\n%s", box)
	}
	// The body shows the jumped-to pin's description (open-tasks's seeded
	// description).
	l, err := m.store.LabelShow("ATM:open-tasks")
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if l.Description == "" {
		t.Fatal("open-tasks has no seeded description")
	}
	if !strings.Contains(box, l.Description) {
		t.Errorf("body should show the jumped-to pin's description:\n%s", box)
	}
}

// TestRenderPinnedTabsFixedHeightWhenNoPins verifies the FIXED-slot contract:
// with nothing pinned the box still renders exactly pinnedBoxHeight lines and
// still shows the Shift-0 (center) tab plus the three Shift-1..3 pin-slot tabs
// (dimmed, available), so the task list height never shifts when the first
// board is pinned.
func TestRenderPinnedTabsFixedHeightWhenNoPins(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()

	box := m.boards.renderPinnedTabs(80)
	lines := strings.Split(box, "\n")
	if len(lines) != pinnedBoxHeight {
		t.Fatalf("renderPinnedTabs with no pins = %d lines, want %d (fixed slot):\n%s", len(lines), pinnedBoxHeight, box)
	}
	for n := 0; n <= maxPins; n++ {
		if !strings.Contains(box, fmt.Sprintf("Shift-%d", n)) {
			t.Errorf("empty tabbed box missing the Shift-%d tab:\n%s", n, box)
		}
	}
	if !strings.Contains(box, "all-tasks") {
		t.Errorf("empty tabbed box body missing the center board name:\n%s", box)
	}
}
