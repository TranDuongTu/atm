package tui

import (
	"strings"
	"testing"

	"atm/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// --- Boards pane tests ---

// newTestStore opens a fresh temp-dir store for direct store-API tests that
// do not need a full TUI Model. Auto-initialized.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

// newTestBoardsModel builds a *boardsModel scoped to code over the given store,
// for direct row assertions without driving the full key harness.
func newTestBoardsModel(t *testing.T, s *store.Store, code string) *boardsModel {
	t.Helper()
	mm, err := NewModel(NewModelOpts{StorePath: s.StorePath(), Actor: testActor})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	mm.projectScope = code
	return &mm.boards
}

// rowNames returns the display Names of the Boards pane's flat row list.
func (b *boardsModel) rowNames() []string {
	out := make([]string, 0, len(b.rows))
	for _, r := range b.rows {
		out = append(out, r.Name)
	}
	return out
}

// row returns the first boardRow whose Name matches, or ok=false.
func (b *boardsModel) row(name string) (boardRow, bool) {
	for _, r := range b.rows {
		if r.Name == name {
			return r, true
		}
	}
	return boardRow{}, false
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestBoardsPaneListsComputedLabelsFlat(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:next-sprint", "the sprint board", "status:open", testActor)

	m := newTestBoardsModel(t, s, "ATM")
	m.refresh()

	rows := m.rowNames()
	// Boards and namespaces sit in ONE flat list, indistinguishable by design.
	if !contains(rows, "next-sprint") {
		t.Errorf("board missing from rows: %v", rows)
	}
	if !contains(rows, "status") {
		t.Errorf("namespace missing from rows: %v", rows)
	}
	// A board is not a namespace, so it must not appear as one.
	if contains(rows, "next-sprint:*") {
		t.Errorf("a board must not render as a namespace: %v", rows)
	}
}

// TestBoardsPaneBoardCountSumsMatchingTasks guards the boardCount fix: a
// board's FullName is never a wildcard, so GroupTasksErr's no-wildcard branch
// returns the matching tasks as the second return value. The board's Count
// must equal the number of tasks matching its expression, not 0.
func TestBoardsPaneBoardCountSumsMatchingTasks(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("ATM:next-sprint", "the sprint board", "status:open", testActor); err != nil {
		t.Fatalf("LabelAdd: %v", err)
	}
	mk := func(title string, labels ...string) {
		if _, err := s.CreateTask("ATM", title, "", labels, testActor); err != nil {
			t.Fatalf("CreateTask %q: %v", title, err)
		}
	}
	mk("open1", "ATM:status:open")
	mk("open2", "ATM:status:open")
	mk("done1", "ATM:status:done")

	b := newTestBoardsModel(t, s, "ATM")
	b.refresh()

	row, ok := b.row("next-sprint")
	if !ok {
		t.Fatalf("next-sprint board missing from rows: %v", b.rowNames())
	}
	if row.Count != 2 {
		t.Errorf("next-sprint Count = %d want 2 (matching tasks)", row.Count)
	}
	if row.Broken {
		t.Errorf("next-sprint marked broken; expression status:open is valid")
	}
}

func TestBoardsPaneFlagsUndescribedRows(t *testing.T) {
	// An agent invents a namespace without describing it -> the human's
	// review signal (conventions rule 6) appears in the pane automatically.
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "t", "", []string{"ATM:sprint:next"}, testActor)

	m := newTestBoardsModel(t, s, "ATM")
	m.refresh()

	row, ok := m.row("sprint")
	if !ok {
		t.Fatalf("sprint namespace missing from rows: %v", m.rowNames())
	}
	if !row.NeedsDescription {
		t.Error("an undescribed namespace must be flagged for human reconciliation")
	}
	row, ok = m.row("status")
	if !ok {
		t.Fatalf("status namespace missing from rows: %v", m.rowNames())
	}
	if row.NeedsDescription {
		t.Error("a seeded namespace has a description and must not be flagged")
	}
}

// --- Boards pane tests (ported from the Labels pane) ---

func TestBoardsTabEmptyStateNoProject(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "3") // focus Boards pane
	if m.focused != paneLabels {
		t.Fatalf("focus = %v want paneLabels", m.focused)
	}
	v := m.boards.View()
	mustContain(t, v, "no project selected")
	mustContain(t, v, "press [s] in the Projects pane")
}

func TestBoardsTabAddLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "a") // add label form
	if m.form == nil {
		t.Fatalf("add-label form not open")
	}
	for _, r := range "patch:urgent" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if m.form != nil {
		t.Fatalf("form should be closed after submit")
	}
	if _, err := m.store.LabelShow("ATM:patch:urgent"); err != nil {
		t.Errorf("ATM:patch:urgent not in registry after add: %v", err)
	}
}

func TestBoardsTabSeedKey(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	_, _ = m.store.LabelRemove("ATM:context:question", testActor)
	m.refreshAll()
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "S")
	if !strings.Contains(m.toastMsg, "seeded 16 labels into ATM") {
		t.Fatalf("toast = %q, want seeded 16 labels into ATM", m.toastMsg)
	}
	if _, err := m.store.LabelShow("ATM:context:question"); err != nil {
		t.Errorf("ATM:context:question not restored after seed: %v", err)
	}
}

// TestBoardsL0FlatCounts replaces TestLabelsL0NamespaceTableCounts. The L0
// view is now a flat list of computed labels (boards + namespaces). The old
// synthetic "tags" and "(none)" rows are gone — bare tags are not computed
// labels and do not appear; tasks with no labels are not a board. Namespace
// rows still carry a distinct-task count.
func TestBoardsL0FlatCounts(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	mk := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "ATM:status:open")
	mk("b", "ATM:status:done", "ATM:priority:high")
	mk("c", "ATM:priority:high")
	update(t, m, "s")
	update(t, m, "3")

	byName := map[string]boardRow{}
	for _, r := range m.boards.rows {
		byName[r.Name] = r
	}
	if r, ok := byName["status"]; !ok || !r.Expandable {
		t.Fatalf("status namespace row missing or not expandable: %+v", byName["status"])
	} else if got := r.Count; got != 2 {
		t.Errorf("status count = %d want 2", got)
	}
	if r, ok := byName["priority"]; !ok || !r.Expandable {
		t.Fatalf("priority namespace row missing or not expandable: %+v", byName["priority"])
	} else if got := r.Count; got != 2 {
		t.Errorf("priority count = %d want 2", got)
	}
	// Bare tags and (none) are NOT boards; they must not appear in the flat list.
	if _, ok := byName["tags"]; ok {
		t.Errorf("bare-tags row must not appear in the flat boards list")
	}
	if _, ok := byName["(none)"]; ok {
		t.Errorf("(none) row must not appear in the flat boards list")
	}
	v := m.boards.View()
	mustContain(t, v, "BOARD")
	mustContain(t, v, "status")
}

func TestBoardsL0FlatListUsesFullWidth(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.boards.SetSize(72, 10)
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")

	lines := strings.Split(m.boards.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("table rendered too few lines:\n%s", m.boards.View())
	}
	if got := lipgloss.Width(lines[0]); got != m.boards.width {
		t.Fatalf("header width = %d want %d: %q", got, m.boards.width, lines[0])
	}
	if got := lipgloss.Width(lines[1]); got != m.boards.width {
		t.Fatalf("row width = %d want %d: %q", got, m.boards.width, lines[1])
	}
}

// TestBoardsL0CountColumnAlignsDespiteMarkers guards the boardTableLine
// display-width fix: rows carrying the ⚠ warning glyph (ANSI-styled and
// multi-byte) must not push the COUNT column out of alignment. Every row —
// plain or ⚠-flagged — must render at the pane's display width, so the count
// column's right edge lines up.
func TestBoardsL0CountColumnAlignsDespiteMarkers(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.boards.SetSize(72, 12)
	// A seeded namespace (status) gets a description; an emergent one
	// (sprint) does not, so it renders the ⚠ marker.
	seedTask(t, m, "ATM", "in-sprint", "ATM:sprint:next", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "3")

	lines := strings.Split(m.boards.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("table rendered too few lines:\n%s", m.boards.View())
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if got := lipgloss.Width(ln); got != m.boards.width {
			t.Errorf("line %d display width = %d want %d (count column drifted on a marked row): %q", i, got, m.boards.width, ln)
		}
	}
	// Confirm at least one marked row was actually rendered, so the
	// test is not vacuously passing on plain rows only.
	if !strings.Contains(m.boards.View(), "⚠") {
		t.Fatalf("no ⚠ marker rendered — test is vacuous:\n%s", m.boards.View())
	}
}

func TestBoardsL0EnterDrillsIntoNamespaceAndFocusesTasks(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")
	if m.boards.level != lLevelChart {
		t.Fatalf("level = %v want chart", m.boards.level)
	}
	if m.tasks.filter != "ATM:status:*" {
		t.Fatalf("filter = %q want ATM:status:*", m.tasks.filter)
	}
	if m.tasks.focus.mode != focusPresent || m.tasks.focus.ns != "status" {
		t.Fatalf("focus = %+v want present/status", m.tasks.focus)
	}
	update(t, m, "esc")
	if m.boards.level != lLevelTable {
		t.Fatalf("level = %v want table after esc", m.boards.level)
	}
	if m.tasks.filter != "" || m.tasks.focus.mode != focusOff {
		t.Fatalf("focus/filter not cleared after esc: %q %+v", m.tasks.filter, m.tasks.focus)
	}
}

// TestBoardsL0EnterBoardFiltersTasksByLabel replaces the old L0 enter test
// for namespace drill-down: a board row (a computed label) selects straight
// to tasks via QueryFilters{Labels: [FullName]} — no chart level.
func TestBoardsL0EnterBoardFiltersTasksByLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.LabelAdd("ATM:next-sprint", "the sprint board", "status:open", m.actor); err != nil {
		t.Fatal(err)
	}
	seedTask(t, m, "ATM", "open1", "ATM:status:open")
	seedTask(t, m, "ATM", "done1", "ATM:status:done")
	update(t, m, "s")
	update(t, m, "3")
	cursorToBoardRow(t, m, "next-sprint")
	update(t, m, "enter")
	if m.boards.level != lLevelTable {
		t.Fatalf("board selection must not leave L0: level = %v", m.boards.level)
	}
	if m.tasks.filter != "ATM:next-sprint" {
		t.Fatalf("tasks filter = %q want ATM:next-sprint", m.tasks.filter)
	}
	if m.tasks.focus.mode != focusOff {
		t.Fatalf("tasks focus = %+v want focusOff (board is an exact-label filter)", m.tasks.focus)
	}
	// The Tasks pane should show only the matching task.
	mustContain(t, m.tasks.View(), "open1")
	mustNotContain(t, m.tasks.View(), "done1")
}

// TestBoardsL0EditNamespaceOpensDescriptorEditor guards that [e] on a
// namespace row (which has no Expr) opens a description-only editor for its
// <ns>:* descriptor, and that saving upserts the descriptor so the ⚠ flag
// clears. A human curates undescribed namespaces this way (conventions rule 6).
func TestBoardsL0EditNamespaceOpensDescriptorEditor(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// sprint is emergent (a task carries ATM:sprint:next) and undescribed.
	seedTask(t, m, "ATM", "in-sprint", "ATM:sprint:next", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "3")

	// Before: the sprint row is flagged.
	cursorToNamespaceRow(t, m, "sprint")
	r, ok := m.boards.row("sprint")
	if !ok || !r.NeedsDescription {
		t.Fatalf("sprint must be flagged before describing: %+v", r)
	}

	// [e] on the namespace row opens the descriptor form, not the board editor.
	update(t, m, "e")
	if m.form == nil || m.formKind != formNamespaceDescribe {
		t.Fatalf("[e] on namespace must open formNamespaceDescribe; form=%v kind=%v", m.form, m.formKind)
	}
	// The form's read-only namespace field is pre-filled; the description
	// field is the second field and is empty (it was undescribed).
	if got := m.form.Fields[0].Value; got != "sprint" {
		t.Errorf("namespace field = %q want sprint", got)
	}

	// Type a description into the description field.
	update(t, m, "tab") // move from namespace to description
	for _, r := range "work slated for the next sprint" {
		update(t, m, string(r))
	}
	// Enter on the last field submits.
	update(t, m, "enter")
	if m.form != nil {
		t.Fatalf("form should be closed after submit")
	}

	// The descriptor was upserted: ATM:sprint:* now has a description.
	l, err := m.store.LabelShow("ATM:sprint:*")
	if err != nil {
		t.Fatalf("LabelShow ATM:sprint:*: %v", err)
	}
	if l.Description != "work slated for the next sprint" {
		t.Errorf("descriptor description = %q want the typed text", l.Description)
	}

	// After refresh the ⚠ flag clears.
	m.boards.refresh()
	r, ok = m.boards.row("sprint")
	if !ok || r.NeedsDescription {
		t.Errorf("sprint must not be flagged after describing: %+v", r)
	}
}

func TestBoardsEscFromChartRestoresTableCursor(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")
	update(t, m, "esc")

	if m.boards.level != lLevelTable {
		t.Fatalf("level = %v want table", m.boards.level)
	}
	if got := m.boards.rows[m.boards.cursor].Name; got != "status" {
		t.Fatalf("table cursor = %q want status", got)
	}
}

// TestBoardsL0HasNoNoneRow replaces TestLabelsL0EnterNoneFiltersUnlabeled.
// The flat boards list has no synthetic "(none)" row: a task with no labels
// is not a computed label. The focusUnlabeled mode still exists in the Tasks
// pane (driven elsewhere), but the Boards pane no longer surfaces it as a row.
func TestBoardsL0HasNoNoneRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "naked", "", nil, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	for _, r := range m.boards.rows {
		if r.Name == "(none)" {
			t.Fatalf("(none) row must not appear in flat boards list: %+v", r)
		}
	}
}

func TestBoardsDetailIsCompactAtDefaultAndSmallTerminals(t *testing.T) {
	for _, size := range []struct {
		name string
		w    int
		h    int
	}{
		{name: "default", w: 100, h: 30},
		{name: "small", w: 80, h: 24},
	} {
		t.Run(size.name, func(t *testing.T) {
			m := newTestModel(t)
			seedProject(t, m, "ATM", "Acme")
			if err := m.store.LabelAdd("ATM:status:open", "selected status description", "", m.actor); err != nil {
				t.Fatal(err)
			}
			seedTask(t, m, "ATM", "open", "ATM:status:open")
			m.SetSize(size.w, size.h)
			update(t, m, "s")
			update(t, m, "3")
			cursorToNamespaceRow(t, m, "status")
			update(t, m, "enter")
			cursorToChartLabel(t, m, "ATM:status:open")
			update(t, m, "enter")

			view := m.boards.View()
			mustContain(t, view, "name        ATM:status:open")
			mustContain(t, view, "usage       1 use")
			mustContain(t, view, "description selected status description")
		})
	}
}

func cursorToNamespaceRow(t *testing.T, m *Model, ns string) {
	t.Helper()
	for i, r := range m.boards.rows {
		if r.Name == ns && r.Expandable {
			m.boards.cursor = i
			return
		}
	}
	t.Fatalf("namespace row %q not found in boards rows: %v", ns, m.boards.rowNames())
}

func cursorToBoardRow(t *testing.T, m *Model, name string) {
	t.Helper()
	for i, r := range m.boards.rows {
		if r.Name == name {
			m.boards.cursor = i
			return
		}
	}
	t.Fatalf("board row %q not found in boards rows: %v", name, m.boards.rowNames())
}

func TestBoardsChartCursorAndUnsetRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(120, 80)
	if err := m.store.LabelAdd("ATM:status:blocked", "", "", m.actor); err != nil {
		t.Fatal(err)
	}
	mk := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "ATM:status:open")
	mk("b", "ATM:status:open")
	mk("c", "ATM:status:done")
	mk("d", "ATM:priority:high") // no status -> unset

	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter") // into chart

	rows := m.boards.chartRows()
	// open(2), blocked(0), done(1), unset(1) in this fixture.
	var openCount, blockedCount, unsetCount int
	sawUnset := false
	sawBlocked := false
	for _, r := range rows {
		if r.unset {
			sawUnset = true
			unsetCount = r.count
		}
		if r.full == "ATM:status:open" {
			openCount = r.count
		}
		if r.full == "ATM:status:blocked" {
			sawBlocked = true
			blockedCount = r.count
		}
	}
	if openCount != 2 {
		t.Errorf("open count = %d want 2", openCount)
	}
	if !sawUnset || unsetCount != 1 {
		t.Errorf("unset row missing or wrong: saw=%v count=%d want 1", sawUnset, unsetCount)
	}
	if !sawBlocked || blockedCount != 0 {
		t.Errorf("blocked row missing or wrong: saw=%v count=%d want 0", sawBlocked, blockedCount)
	}
	v := m.boards.View()
	mustContain(t, v, "(unset)")
	mustContain(t, v, "█")
}

func TestBoardsChartHighlightsOnlyName(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")
	cursorToChartLabel(t, m, "ATM:status:open")

	line := ""
	for _, candidate := range strings.Split(m.boards.View(), "\n") {
		if strings.Contains(candidate, "ATM:status:open") {
			line = candidate
			break
		}
	}
	if line == "" {
		t.Fatalf("status:open chart row not found:\n%s", m.boards.View())
	}
	barAt := strings.Index(line, "█")
	resetAt := strings.Index(line, "\x1b[0m")
	if barAt < 0 {
		t.Fatalf("chart row has no bar:\n%q", line)
	}
	if resetAt < 0 {
		t.Fatalf("chart row has no cursor reset:\n%q", line)
	}
	if resetAt > barAt {
		t.Fatalf("chart cursor styling reaches the bar; reset=%d bar=%d line=%q", resetAt, barAt, line)
	}
}

func TestBoardsChartCursorCanStayOnUnset(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateTask("ATM", "b", "", []string{"ATM:priority:high"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")

	unset := -1
	for i, r := range m.boards.chartRows() {
		if r.unset {
			unset = i
			break
		}
	}
	if unset < 0 {
		t.Fatalf("unset row not found")
	}
	for m.boards.cursor < unset {
		update(t, m, "j")
	}
	if !m.boards.chartRows()[m.boards.cursor].unset {
		t.Fatalf("cursor = %d want unset row %d before render", m.boards.cursor, unset)
	}
	_ = m.boards.View()
	if !m.boards.chartRows()[m.boards.cursor].unset {
		t.Fatalf("cursor moved after render: got %d want unset row %d", m.boards.cursor, unset)
	}
	if err := m.store.LabelAdd("ATM:status:later", "", "", m.actor); err != nil {
		t.Fatal(err)
	}
	m.boards.refresh()
	if !m.boards.chartRows()[m.boards.cursor].unset {
		t.Fatalf("cursor moved after refresh: got %d want unset row %d", m.boards.cursor, unset)
	}
}

func TestBoardsChartHeadlineCountsDistinctPresentTasks(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "both", "ATM:status:open", "ATM:status:done")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	seedTask(t, m, "ATM", "unset", "ATM:priority:high")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")

	mustContain(t, m.boards.View(), "status  ·  2 tasks")
}

func TestBoardsChartEnterRowOpensDetailAndFocusesExactLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter") // chart
	cursorToChartLabel(t, m, "ATM:status:open")
	update(t, m, "enter") // detail

	if m.boards.level != lLevelDetail {
		t.Fatalf("level = %v want detail", m.boards.level)
	}
	if m.tasks.filter != "ATM:status:open" || m.tasks.focus.mode != focusOff {
		t.Fatalf("tasks focus/filter = %+v %q want off/exact", m.tasks.focus, m.tasks.filter)
	}
	mustContain(t, m.boards.View(), "name        ATM:status:open")

	// Esc returns to the chart and re-applies present focus.
	update(t, m, "esc")
	if m.boards.level != lLevelChart {
		t.Fatalf("level = %v want chart after esc", m.boards.level)
	}
	if m.tasks.filter != "ATM:status:*" || m.tasks.focus.mode != focusPresent {
		t.Fatalf("chart focus not restored: %+v %q", m.tasks.focus, m.tasks.filter)
	}
}

func TestBoardsChartEnterUnsetFiltersAbsent(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	mk := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "ATM:status:open")
	mk("b", "ATM:priority:high") // no status

	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter") // chart
	cursorToChartUnset(t, m)
	update(t, m, "enter") // unset leaf

	if m.tasks.focus.mode != focusAbsent || m.tasks.focus.ns != "status" {
		t.Fatalf("focus = %+v want absent/status", m.tasks.focus)
	}
	mustContain(t, m.boards.View(), "1 task with no status")
	update(t, m, "esc")
	if m.boards.level != lLevelChart || m.tasks.focus.mode != focusPresent {
		t.Fatalf("esc from unset leaf did not restore chart present focus: %v %+v", m.boards.level, m.tasks.focus)
	}
}

func TestBoardsChartRemovePrefillsCursorLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")
	cursorToChartLabel(t, m, "ATM:status:open")
	update(t, m, "l")

	if m.form == nil || m.formKind != formLabelRemove {
		t.Fatalf("remove form not open: form=%v kind=%v", m.form, m.formKind)
	}
	if got := m.form.Fields[0].Value; got != "status:open" {
		t.Fatalf("remove form name = %q want status:open", got)
	}
}

func TestBoardsDetailRemovePrefillsDisplayedLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")
	cursorToChartLabel(t, m, "ATM:status:open")
	update(t, m, "enter")
	update(t, m, "l")

	if m.form == nil || m.formKind != formLabelRemove {
		t.Fatalf("remove form not open: form=%v kind=%v", m.form, m.formKind)
	}
	if got := m.form.Fields[0].Value; got != "status:open" {
		t.Fatalf("remove form name = %q want status:open", got)
	}
}

func TestBoardsSyntheticUnsetRemoveIsNoOp(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:priority:high"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")
	cursorToChartUnset(t, m)
	update(t, m, "l")
	if m.form != nil {
		t.Fatalf("remove form opened for chart (unset) row")
	}
	update(t, m, "enter")
	update(t, m, "l")
	if m.form != nil {
		t.Fatalf("remove form opened for unset detail leaf")
	}
}

func cursorToChartLabel(t *testing.T, m *Model, full string) {
	t.Helper()
	for i, r := range m.boards.chartRows() {
		if r.full == full {
			m.boards.cursor = i
			return
		}
	}
	t.Fatalf("chart label %q not found", full)
}

func cursorToChartUnset(t *testing.T, m *Model) {
	t.Helper()
	for i, r := range m.boards.chartRows() {
		if r.unset {
			m.boards.cursor = i
			return
		}
	}
	t.Fatalf("chart (unset) row not found")
}

func TestFitLineResetsANSIWhenTruncatingSelectedRows(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newTestModel(t)
	line := m.styles.RowCursor.Render(strings.Repeat("x", 80))

	got := fitLine(line, 20)

	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("truncated selected row does not reset ANSI styling: %q", got)
	}
}
