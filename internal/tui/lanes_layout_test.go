package tui

import (
	"strings"
	"testing"
)

// paneLines renders the Tasks pane list view and returns its plain lines.
func paneLines(t *testing.T, m *Model) []string {
	t.Helper()
	return strings.Split(stripANSI(m.tasks.View()), "\n")
}

// TestPaneLayoutStripTopFooterBottom pins the approved pane composition,
// top to bottom: the lane strip, the column header and rows, the art gap,
// then the footer on the pane's last line. The strip is what the user reads
// FIRST — it says which lane the list below belongs to — so a strip under
// the list would caption the rows after they had already been read.
func TestPaneLayoutStripTopFooterBottom(t *testing.T) {
	m := setupLaneWalk(t)
	lines := paneLines(t, m)

	strip := strings.Join(lines[:laneStripHeight], "\n")
	for _, lane := range []string{"Inbox", "Pipeline", "Out"} {
		if !strings.Contains(strip, lane) {
			t.Fatalf("lane strip is not the first %d lines (missing %s):\n%s", laneStripHeight, lane, strip)
		}
	}
	below := strings.Join(lines[laneStripHeight:], "\n")
	if strings.Contains(below, "Pipeline ") && !strings.Contains(below, "being built") {
		t.Fatalf("the strip leaked below its slot:\n%s", below)
	}

	// The column header sits directly under the strip, above every row.
	headerIdx, rowIdx := -1, -1
	for i, ln := range lines {
		if headerIdx < 0 && strings.Contains(ln, "TITLE") && strings.Contains(ln, "UPDATED") {
			headerIdx = i
		}
		if rowIdx < 0 && strings.Contains(ln, "being built") {
			rowIdx = i
		}
	}
	if headerIdx < laneStripHeight {
		t.Fatalf("column header at line %d, want below the %d-line strip", headerIdx, laneStripHeight)
	}
	if rowIdx < headerIdx {
		t.Fatalf("a task row (line %d) renders above the column header (line %d)", rowIdx, headerIdx)
	}

	// The footer is the pane's last line, its divider the one before.
	last := lines[len(lines)-1]
	if !strings.Contains(last, "showing 1-1 of 1") {
		t.Fatalf("last pane line = %q, want the showing-count footer", last)
	}
	if div := strings.TrimSpace(lines[len(lines)-2]); div == "" || strings.Trim(div, "─") != "" {
		t.Fatalf("line above the footer = %q, want the footer divider", lines[len(lines)-2])
	}
}

// TestPaneArtFillsTheGapAboveTheFooter pins where the background art goes:
// the dead space between the last row and the footer divider. Art below the
// footer would read as a second, empty pane.
func TestPaneArtFillsTheGapAboveTheFooter(t *testing.T) {
	m := setupLaneWalk(t)
	m.SetSize(140, 46)
	if err := m.store.SetProjectArtOn("ATM", true, nil, m.actor); err != nil {
		t.Fatalf("SetProjectArtOn: %v", err)
	}
	m.refreshAll()

	lines := paneLines(t, m)
	footerIdx := len(lines) - 2 // the divider
	rowIdx := -1
	for i, ln := range lines {
		if strings.Contains(ln, "being built") {
			rowIdx = i
		}
	}
	if rowIdx < 0 {
		t.Fatalf("no task row rendered:\n%s", strings.Join(lines, "\n"))
	}
	gap := strings.Join(lines[rowIdx+1:footerIdx], "\n")
	if strings.TrimSpace(gap) == "" {
		t.Fatalf("the gap between the last row and the footer is empty; art did not fill it:\n%s", gap)
	}
}
