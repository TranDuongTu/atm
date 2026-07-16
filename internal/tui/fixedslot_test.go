package tui

import (
	"fmt"
	"strings"
	"testing"

	"atm/internal/workflow"
	"github.com/charmbracelet/lipgloss"
)

// TestListContentHeightConstantAcrossPins is the core fixed-slot invariant: the
// scrollable task list height must NOT change as boards are pinned or
// unpinned. The pinned region always reserves 3*maxPins lines (filled or
// placeholder), so listContentHeight subtracts a constant. Proven across
// 0..maxPins pins.
func TestListContentHeightConstantAcrossPins(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	var boards []string
	for i := 0; i < maxPins; i++ {
		name := fmt.Sprintf("ATM:board-%02d", i)
		if err := m.store.LabelAdd(name, "", "status:open", m.actor); err != nil {
			t.Fatal(err)
		}
		boards = append(boards, name)
	}
	m.boards.refresh()
	m.SetSize(100, 40)

	want := m.tasks.listContentHeight() // 0 pins
	// The reservation is a constant: pane content height minus the strip minus
	// the fixed 3*maxPins pinned slot.
	if exp := m.tasks.contentHeight - stripHeight - 3*maxPins; want != exp {
		t.Fatalf("listContentHeight (0 pins) = %d, want %d (contentHeight - strip - 3*maxPins)", want, exp)
	}
	for i, full := range boards {
		m.boards.selected = full
		m.boards.togglePin()
		if len(m.boards.pins) != i+1 {
			t.Fatalf("pins = %d after %d toggles, want %d", len(m.boards.pins), i+1, i+1)
		}
		if got := m.tasks.listContentHeight(); got != want {
			t.Errorf("listContentHeight with %d pin(s) = %d, want constant %d", i+1, got, want)
		}
	}
}

// TestTaskColumnWidthsSizesIdToLongestID verifies idW grows to fit the widest
// task ID present (production IDs like DEMO-f7d632 are 11 chars, not the 9 the
// old fixed idW assumed). Go's %-9s does not truncate a longer value, so an
// under-sized idW pushed the trailing UPDATED column off the pane and clipped
// it. The formatted row must never exceed the pane width.
func TestTaskColumnWidthsSizesIdToLongestID(t *testing.T) {
	m := newTestModel(t)
	m.tasks.width = 100
	m.tasks.rows = []taskRow{
		{id: "DEMO-f7d632", title: "a longer task title here", labels: []string{"ATM:status:open"}, updated: "1d ago"},
		{id: "DEMO-0001", title: "short", updated: "now"},
	}

	idW, labelsW, updatedW, titleW := m.tasks.taskColumnWidths()
	if idW < len("DEMO-f7d632") {
		t.Errorf("idW = %d, want >= %d (the longest id)", idW, len("DEMO-f7d632"))
	}
	if idW+labelsW+updatedW+titleW+4 != 100 {
		t.Errorf("column widths %d+%d+%d+%d+4 = %d, want == pane width 100 (no overflow)", idW, labelsW, updatedW, titleW, idW+labelsW+updatedW+titleW+4)
	}

	// The widest row, formatted exactly as renderFlatList does, must fit the
	// pane so the UPDATED value ("1d ago") is never clipped.
	r := m.tasks.rows[0]
	labels := strings.Join(r.labels, " ")
	line := fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, truncateRunes(r.id, idW), titleW, truncateRunes(r.title, titleW), labelsW, truncateRunes(labels, labelsW), updatedW, r.updated)
	if w := lipgloss.Width(line); w > 100 {
		t.Errorf("flat row width = %d, exceeds pane width 100 (UPDATED column would clip): %q", w, line)
	}
	if !strings.Contains(line, "1d ago") {
		t.Errorf("flat row = %q, want the full UPDATED value \"1d ago\" (not clipped)", line)
	}
}

// TestTaskColumnWidthsClampsIdWidth verifies idW is clamped to a sane maximum
// so a pathologically long id cannot starve the TITLE column; renderFlatList
// truncates such an id defensively.
func TestTaskColumnWidthsClampsIdWidth(t *testing.T) {
	m := newTestModel(t)
	m.tasks.width = 100
	m.tasks.rows = []taskRow{{id: strings.Repeat("X", 40), title: "t", updated: "now"}}
	idW, _, _, _ := m.tasks.taskColumnWidths()
	if idW != 14 {
		t.Errorf("idW for a 40-char id = %d, want 14 (clamp)", idW)
	}
}

// TestSelectedCellShowsInspectHintWhenHighlighted verifies the highlighted
// SELECTED board (pinFocus == -1) advertises "> to inspect" in its title, and
// that the hint disappears once a pin takes the highlight (Shift-N jump).
func TestSelectedCellShowsInspectHintWhenHighlighted(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault() // pinFocus == -1: the SELECTED cell holds the highlight

	strip := m.boards.renderStrip(80, stripHeight)
	if !strings.Contains(strip, "to inspect") {
		t.Errorf("highlighted SELECTED cell missing the \"> to inspect\" hint:\n%s", strip)
	}

	// Once a pin is jumped to, the highlight leaves the strip and so must the hint.
	m.boards.togglePin()
	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 1 pin")
	}
	strip = m.boards.renderStrip(80, stripHeight)
	if strings.Contains(strip, "to inspect") {
		t.Errorf("SELECTED cell should drop the inspect hint once a pin holds the highlight:\n%s", strip)
	}
}

// TestGroupedPagingDerivesFromListContentHeight verifies the grouped page-jump
// size matches what the renderer windows by: at keypress time listPageSize()
// must equal listContentHeight()-2 (the grouped body reserves 2 lines of
// chrome), so pgup/pgdown land on the exact page boundary the grouped renderer
// draws. It must also be stable across pin toggles (fixed slot).
func TestGroupedPagingDerivesFromListContentHeight(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	// Select the status namespace board so the Tasks pane groups by facet.
	for _, r := range m.boards.rows {
		if r.Expandable && r.Name == "status" {
			m.boards.selected = r.FullName
			break
		}
	}
	m.boards.applyFocus()
	m.SetSize(100, 40)

	if !m.tasks.grouped() {
		t.Fatalf("expected a grouped (namespace) focus, got %+v", m.tasks.focus)
	}
	want := m.tasks.listContentHeight() - 2
	if got := m.tasks.listPageSize(); got != want {
		t.Errorf("grouped listPageSize() = %d, want listContentHeight()-2 = %d", got, want)
	}

	// Pinning must not shift the grouped page boundary.
	m.boards.selectDefault()
	m.boards.togglePin()
	for _, r := range m.boards.rows {
		if r.Expandable && r.Name == "status" {
			m.boards.selected = r.FullName
			break
		}
	}
	m.boards.applyFocus()
	if got := m.tasks.listPageSize(); got != want {
		t.Errorf("grouped listPageSize() after pinning = %d, want unchanged %d", got, want)
	}
}
