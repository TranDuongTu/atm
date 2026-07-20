package tui

import (
	"fmt"
	"strings"
	"testing"

	"atm/internal/capability/workflow"
	"github.com/charmbracelet/lipgloss"
)

// TestListContentHeightConstantAcrossPins is the core fixed-slot invariant: the
// scrollable task list height must NOT change as boards are pinned or
// unpinned. The single tabbed pinned box always reserves pinnedBoxHeight lines,
// so listContentHeight subtracts a constant. Proven across 0..maxPins pins.
//
// Rule 3 of the Task 4 brief: pin workflow's exposed boards (ATM:all-tasks,
// ATM:open-tasks, ATM:in-progress-tasks) — custom boards are no longer ring
// rows, so they are no longer pin candidates. Seeding workflow's vocabulary
// first makes them real ring members so togglePin actually appends them; the
// height invariant is then asserted over real pins (not vacuously over ghost
// ad-hoc labels that ringIndex cannot find).
func TestListContentHeightConstantAcrossPins(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	boards := []string{
		"ATM:all-tasks", "ATM:open-tasks", "ATM:in-progress-tasks",
	}
	if len(boards) > maxPins {
		t.Fatalf("fixture pins %d exceeds maxPins %d", len(boards), maxPins)
	}
	m.refreshAll()
	m.SetSize(100, 40)

	// Sanity: each fixture board is a real ring member so togglePin will
	// actually append it (ringIndex >= 0). Without this, the height invariant
	// would pass vacuously.
	for _, full := range boards {
		found := false
		for _, r := range m.boards.rows {
			if r.FullName == full {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("fixture board %s is not a ring member: %v", full, m.boards.rowNames())
		}
	}

	want := m.tasks.listContentHeight() // 0 pins
	// The reservation is a constant: pane content height minus the strip minus
	// the fixed pinnedBoxHeight tabbed slot.
	if exp := m.tasks.contentHeight - stripHeight - pinnedBoxHeight; want != exp {
		t.Fatalf("listContentHeight (0 pins) = %d, want %d (contentHeight - strip - pinnedBoxHeight)", want, exp)
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
// SELECTED board (pinFocus == -1) advertises "to inspect" in its title, and
// that the hint disappears once a pin takes the highlight (Shift-N jump).
func TestSelectedCellShowsInspectHintWhenHighlighted(t *testing.T) {
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
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
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
	// Grouped body reserves 3 chrome lines: header + blank + "showing" footer.
	want := m.tasks.listContentHeight() - 3
	if got := m.tasks.listPageSize(); got != want {
		t.Errorf("grouped listPageSize() = %d, want listContentHeight()-3 = %d", got, want)
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

// TestProjectColumnWidthsFitPaneWithGutterPrefix verifies the data row —
// including the 2-char "gutter + space" prefix renderListRows prepends —
// fits p.width so the rightmost UPDATED column is never clipped ("3m ago"
// must not become "3m ag"). NAME is the flexible column and absorbs the
// gutter overhead; UPDATED stays fixed at 10.
func TestProjectColumnWidthsFitPaneWithGutterPrefix(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 60
	codeW, tasksW, labelsW, updatedW, nameW := p.projectColumnWidths()
	if codeW != 6 || tasksW != 6 || labelsW != 7 || updatedW != 10 {
		t.Errorf("fixed widths = %d/%d/%d/%d, want 6/6/7/10", codeW, tasksW, labelsW, updatedW)
	}
	// Full data row = fixed + nameW + 5 (format overhead) + 2 (gutter+space).
	rowW := codeW + tasksW + labelsW + updatedW + nameW + 5 + 2
	if rowW > p.width {
		t.Errorf("data row width = %d, exceeds pane width %d (UPDATED would clip)", rowW, p.width)
	}
}

// TestProjectColumnWidthsNameAbsorbsShrinkage verifies NAME is the flexible
// column: at a wider pane it grows, and it never forces the row to overflow.
func TestProjectColumnWidthsNameAbsorbsShrinkage(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 100
	_, _, _, _, nameW100 := p.projectColumnWidths()
	p.width = 50
	_, _, _, _, nameW50 := p.projectColumnWidths()
	if nameW100 <= nameW50 {
		t.Errorf("nameW at width 100 = %d, not greater than nameW at width 50 = %d", nameW100, nameW50)
	}
}

// TestProjectColumnWidthsNameFloorIsEight verifies the NAME floor is 8 (lowered
// from 20) so NAME keeps absorbing shrinkage at narrow panes instead of
// forcing the row to overflow and clip UPDATED.
func TestProjectColumnWidthsNameFloorIsEight(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 30 // below the floor: nameW would go negative without the clamp
	_, _, _, _, nameW := p.projectColumnWidths()
	if nameW != 8 {
		t.Errorf("nameW floor = %d, want 8", nameW)
	}
}

// TestProjectListDataRowRendersFullUpdatedColumn verifies a rendered data row
// keeps the full UPDATED value ("3m ago", not "3m ag") at a realistic pane
// width. The row is formatted exactly as renderListRows does, including the
// "gutter + space" prefix, and must fit the pane width.
func TestProjectListDataRowRendersFullUpdatedColumn(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 60
	p.list = []projRow{
		{code: "ATM", name: "Acme Task Manager", tasks: 3, labels: 5, updated: "3m ago"},
	}
	codeW, tasksW, labelsW, updatedW, nameW := p.projectColumnWidths()
	r := p.list[0]
	gutter := " "
	line := fmt.Sprintf(" %-*s %-*s %*d %*d %*s", codeW, r.code, nameW, truncateRunes(r.name, nameW), tasksW, r.tasks, labelsW, r.labels, updatedW, r.updated)
	full := gutter + " " + line
	if w := lipgloss.Width(full); w > p.width {
		t.Errorf("data row width = %d, exceeds pane width %d (UPDATED would clip): %q", w, p.width, full)
	}
	if !strings.Contains(full, "3m ago") {
		t.Errorf("data row = %q, want the full UPDATED value \"3m ago\" (not clipped)", full)
	}
}

// TestProjectListDataRowRendersFullUpdatedColumnAtNarrowPane verifies the
// UPDATED value stays intact even when the pane is narrow enough to push NAME
// to its floor — NAME truncates with an ellipsis, UPDATED does not clip.
func TestProjectListDataRowRendersFullUpdatedColumnAtNarrowPane(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 44 // the smallest width at which the row still fits with nameW=8
	p.list = []projRow{
		{code: "ATM", name: "Acme Task Manager", tasks: 3, labels: 5, updated: "3m ago"},
	}
	codeW, tasksW, labelsW, updatedW, nameW := p.projectColumnWidths()
	if nameW != 8 {
		t.Fatalf("nameW = %d, want 8 (floor) at p.width=44", nameW)
	}
	r := p.list[0]
	gutter := " "
	line := fmt.Sprintf(" %-*s %-*s %*d %*d %*s", codeW, r.code, nameW, truncateRunes(r.name, nameW), tasksW, r.tasks, labelsW, r.labels, updatedW, r.updated)
	full := gutter + " " + line
	if w := lipgloss.Width(full); w > p.width {
		t.Errorf("narrow data row width = %d, exceeds pane width %d: %q", w, p.width, full)
	}
	if !strings.Contains(full, "3m ago") {
		t.Errorf("narrow data row = %q, want the full UPDATED value \"3m ago\"", full)
	}
	if !strings.Contains(line, "...") {
		t.Errorf("narrow data row = %q, want NAME truncated with ellipsis", line)
	}
}
