package tui

import (
	"strings"
	"testing"
)

func momentumTitleLine(lines []string) int {
	for i, l := range lines {
		if strings.Contains(l, "momentum ·") {
			return i
		}
	}
	return -1
}

func TestPaneLayoutMomentumBoxAtBottom(t *testing.T) {
	m := setupMomentum(t)
	m.focused = paneTasks
	lines := paneLines(t, m)
	title := momentumTitleLine(lines)
	if title < 0 {
		t.Fatalf("no momentum box in pane [2]:\n%s", strings.Join(lines, "\n"))
	}
	if want := len(lines) - momentumBoxHeight; title != want {
		t.Fatalf("momentum title on line %d, want %d (box of %d rows at the bottom)", title, want, momentumBoxHeight)
	}
	if !strings.Contains(lines[title], "scrum") || !strings.Contains(lines[title], "One month") {
		t.Fatalf("title = %q, want capability and range", lines[title])
	}
	if lines[len(lines)-1] == "" {
		t.Fatalf("pane no longer ends on the box's bottom border")
	}
}

func TestPaneLayoutListShrinksForMomentumAndGrowsBackOnToggle(t *testing.T) {
	m := setupMomentum(t)
	m.focused = paneTasks
	shown := m.tasks.listPageSize()
	update(t, m, "m")
	if m.momentum.visible() {
		t.Fatalf("m did not collapse the chart")
	}
	hidden := m.tasks.listPageSize()
	if hidden != shown+momentumBoxHeight {
		t.Fatalf("page size shown=%d hidden=%d, want a difference of %d", shown, hidden, momentumBoxHeight)
	}
	if momentumTitleLine(paneLines(t, m)) >= 0 {
		t.Fatalf("collapsed chart still rendered")
	}
	update(t, m, "m")
	if !m.momentum.visible() || m.tasks.listPageSize() != shown {
		t.Fatalf("second m did not restore the chart")
	}
}

func TestPaneMomentumRangeKeysAreIndependentOfProjectsPane(t *testing.T) {
	m := setupMomentum(t)
	m.focused = paneTasks
	before := m.projects.chartRange
	update(t, m, "ctrl+up")
	if m.momentum.rangeIdx != momentumDefaultRange+1 {
		t.Fatalf("rangeIdx = %d after ctrl+up, want %d", m.momentum.rangeIdx, momentumDefaultRange+1)
	}
	if m.projects.chartRange != before {
		t.Fatalf("projects chartRange moved with pane [2]'s key")
	}
	update(t, m, "ctrl+down")
	if m.momentum.rangeIdx != momentumDefaultRange {
		t.Fatalf("rangeIdx = %d after ctrl+down, want %d", m.momentum.rangeIdx, momentumDefaultRange)
	}
	lines := paneLines(t, m)
	if title := momentumTitleLine(lines); title < 0 || !strings.Contains(lines[title], "One month") {
		t.Fatalf("title does not follow the range")
	}
}

func TestPaneMomentumHiddenWhenShort(t *testing.T) {
	m := setupMomentum(t)
	m.SetSize(120, momentumBoxHeight+laneStripHeight+6) // too short for a usable list
	m.refreshAll()
	if momentumTitleLine(paneLines(t, m)) >= 0 {
		t.Fatalf("chart drawn on a pane too short to keep a list")
	}
}

func TestKeymapDocumentsMomentumKeys(t *testing.T) {
	found := 0
	for _, e := range menuEntries {
		if e.hidden && (e.key == "m" || strings.Contains(e.label, "Momentum")) {
			found++
		}
	}
	if found < 1 {
		t.Fatalf("keymap lacks a hidden entry for the momentum toggle")
	}
}
