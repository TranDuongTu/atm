package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// --- Labels pane tests ---

func TestLabelsTabEmptyStateNoProject(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "3") // focus Labels pane
	if m.focused != paneLabels {
		t.Fatalf("focus = %v want paneLabels", m.focused)
	}
	v := m.labels.View()
	mustContain(t, v, "no project selected")
	mustContain(t, v, "press [s] in the Projects pane")
}

func TestLabelsTabAddLabel(t *testing.T) {
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

func TestLabelsTabSeedKey(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	_, _ = m.store.LabelRemove("ATM:context:question", testActor)
	m.refreshAll()
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "S")
	if !strings.Contains(m.toastMsg, "seeded 12 labels into ATM") {
		t.Fatalf("toast = %q, want seeded 12 labels into ATM", m.toastMsg)
	}
	if _, err := m.store.LabelShow("ATM:context:question"); err != nil {
		t.Errorf("ATM:context:question not restored after seed: %v", err)
	}
}

func TestLabelsL0NamespaceTableCounts(t *testing.T) {
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
	mk("d", "ATM:urgent") // bare tag
	mk("e")               // no labels
	m.store.LabelAdd("ATM:urgent", "", "", m.actor)
	update(t, m, "s")
	update(t, m, "3")

	byKey := map[string]nsRow{}
	for _, r := range m.labels.nsRows {
		k := r.key
		if r.bareTags {
			k = "__tags__"
		}
		if r.none {
			k = "__none__"
		}
		byKey[k] = r
	}
	if got := byKey["status"].tasks; got != 2 {
		t.Errorf("status tasks = %d want 2", got)
	}
	if got := byKey["priority"].tasks; got != 2 {
		t.Errorf("priority tasks = %d want 2", got)
	}
	if got := byKey["__tags__"].tasks; got != 1 {
		t.Errorf("tags tasks = %d want 1", got)
	}
	if got := byKey["__none__"].tasks; got != 1 {
		t.Errorf("none tasks = %d want 1", got)
	}
	v := m.labels.View()
	mustContain(t, v, "NAMESPACE")
	mustContain(t, v, "status")
}

func TestLabelsL0NamespaceTableUsesFullWidth(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.labels.SetSize(72, 10)
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")

	lines := strings.Split(m.labels.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("table rendered too few lines:\n%s", m.labels.View())
	}
	if got := lipgloss.Width(lines[0]); got != m.labels.width {
		t.Fatalf("header width = %d want %d: %q", got, m.labels.width, lines[0])
	}
	if got := lipgloss.Width(lines[1]); got != m.labels.width {
		t.Fatalf("row width = %d want %d: %q", got, m.labels.width, lines[1])
	}
}

func TestLabelsL0EnterDrillsIntoNamespaceAndFocusesTasks(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")
	if m.labels.level != lLevelChart {
		t.Fatalf("level = %v want chart", m.labels.level)
	}
	if m.tasks.filter != "ATM:status:*" {
		t.Fatalf("filter = %q want ATM:status:*", m.tasks.filter)
	}
	if m.tasks.focus.mode != focusPresent || m.tasks.focus.ns != "status" {
		t.Fatalf("focus = %+v want present/status", m.tasks.focus)
	}
	update(t, m, "esc")
	if m.labels.level != lLevelTable {
		t.Fatalf("level = %v want table after esc", m.labels.level)
	}
	if m.tasks.filter != "" || m.tasks.focus.mode != focusOff {
		t.Fatalf("focus/filter not cleared after esc: %q %+v", m.tasks.filter, m.tasks.focus)
	}
}

func TestLabelsEscFromChartRestoresTableCursor(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")
	update(t, m, "esc")

	if m.labels.level != lLevelTable {
		t.Fatalf("level = %v want table", m.labels.level)
	}
	if got := m.labels.nsRows[m.labels.cursor].key; got != "status" {
		t.Fatalf("table cursor = %q want status", got)
	}
}

func TestLabelsL0EnterNoneFiltersUnlabeled(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "naked", "", nil, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNoneRow(t, m)
	update(t, m, "enter")
	if m.tasks.focus.mode != focusUnlabeled {
		t.Fatalf("focus = %+v want unlabeled", m.tasks.focus)
	}
	mustContain(t, m.labels.View(), "1 task with no labels")
	update(t, m, "esc")
	if m.tasks.focus.mode != focusOff || m.labels.level != lLevelTable {
		t.Fatalf("esc from none leaf did not return to table/clear: %+v %v", m.tasks.focus, m.labels.level)
	}
	if got := m.labels.nsRows[m.labels.cursor].display; got != "(none)" {
		t.Fatalf("table cursor = %q want (none)", got)
	}
}

func TestLabelsDetailIsCompactAtDefaultAndSmallTerminals(t *testing.T) {
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

			view := m.labels.View()
			mustContain(t, view, "name        ATM:status:open")
			mustContain(t, view, "usage       1 use")
			mustContain(t, view, "description selected status description")
		})
	}
}

func cursorToNamespaceRow(t *testing.T, m *Model, ns string) {
	t.Helper()
	for i, r := range m.labels.nsRows {
		if r.key == ns && !r.bareTags && !r.none {
			m.labels.cursor = i
			return
		}
	}
	t.Fatalf("namespace row %q not found", ns)
}

func cursorToNoneRow(t *testing.T, m *Model) {
	t.Helper()
	for i, r := range m.labels.nsRows {
		if r.none {
			m.labels.cursor = i
			return
		}
	}
	t.Fatalf("(none) row not found")
}

func TestLabelsChartCursorAndUnsetRow(t *testing.T) {
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

	rows := m.labels.chartRows()
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
	v := m.labels.View()
	mustContain(t, v, "(unset)")
	mustContain(t, v, "█")
}

func TestLabelsChartHighlightsOnlyName(t *testing.T) {
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
	for _, candidate := range strings.Split(m.labels.View(), "\n") {
		if strings.Contains(candidate, "ATM:status:open") {
			line = candidate
			break
		}
	}
	if line == "" {
		t.Fatalf("status:open chart row not found:\n%s", m.labels.View())
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

func TestLabelsChartCursorCanStayOnUnset(t *testing.T) {
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
	for i, r := range m.labels.chartRows() {
		if r.unset {
			unset = i
			break
		}
	}
	if unset < 0 {
		t.Fatalf("unset row not found")
	}
	for m.labels.cursor < unset {
		update(t, m, "j")
	}
	if !m.labels.chartRows()[m.labels.cursor].unset {
		t.Fatalf("cursor = %d want unset row %d before render", m.labels.cursor, unset)
	}
	_ = m.labels.View()
	if !m.labels.chartRows()[m.labels.cursor].unset {
		t.Fatalf("cursor moved after render: got %d want unset row %d", m.labels.cursor, unset)
	}
	if err := m.store.LabelAdd("ATM:status:later", "", "", m.actor); err != nil {
		t.Fatal(err)
	}
	m.labels.refresh()
	if !m.labels.chartRows()[m.labels.cursor].unset {
		t.Fatalf("cursor moved after refresh: got %d want unset row %d", m.labels.cursor, unset)
	}
}

func TestLabelsChartHeadlineCountsDistinctPresentTasks(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "both", "ATM:status:open", "ATM:status:done")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	seedTask(t, m, "ATM", "unset", "ATM:priority:high")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")

	mustContain(t, m.labels.View(), "status  ·  2 tasks")
}

func TestLabelsChartEnterRowOpensDetailAndFocusesExactLabel(t *testing.T) {
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

	if m.labels.level != lLevelDetail {
		t.Fatalf("level = %v want detail", m.labels.level)
	}
	if m.tasks.filter != "ATM:status:open" || m.tasks.focus.mode != focusOff {
		t.Fatalf("tasks focus/filter = %+v %q want off/exact", m.tasks.focus, m.tasks.filter)
	}
	mustContain(t, m.labels.View(), "name        ATM:status:open")

	// Esc returns to the chart and re-applies present focus.
	update(t, m, "esc")
	if m.labels.level != lLevelChart {
		t.Fatalf("level = %v want chart after esc", m.labels.level)
	}
	if m.tasks.filter != "ATM:status:*" || m.tasks.focus.mode != focusPresent {
		t.Fatalf("chart focus not restored: %+v %q", m.tasks.focus, m.tasks.filter)
	}
}

func TestLabelsChartEnterUnsetFiltersAbsent(t *testing.T) {
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
	mustContain(t, m.labels.View(), "1 task with no status")
	update(t, m, "esc")
	if m.labels.level != lLevelChart || m.tasks.focus.mode != focusPresent {
		t.Fatalf("esc from unset leaf did not restore chart present focus: %v %+v", m.labels.level, m.tasks.focus)
	}
}

func TestLabelsChartRemovePrefillsCursorLabel(t *testing.T) {
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

func TestLabelsDetailRemovePrefillsDisplayedLabel(t *testing.T) {
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

func TestLabelsSyntheticUnsetRemoveIsNoOp(t *testing.T) {
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
	for i, r := range m.labels.chartRows() {
		if r.full == full {
			m.labels.cursor = i
			return
		}
	}
	t.Fatalf("chart label %q not found", full)
}

func cursorToChartUnset(t *testing.T, m *Model) {
	t.Helper()
	for i, r := range m.labels.chartRows() {
		if r.unset {
			m.labels.cursor = i
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
