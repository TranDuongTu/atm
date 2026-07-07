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

func TestLabelsTabListSeededLabels(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 80)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // select ATM
	update(t, m, "3") // Labels pane
	v := m.labels.View()
	body := m.labels.View()
	if strings.HasPrefix(body, "Labels\n") {
		t.Fatalf("labels body repeats tab title\n--- body ---\n%s", body)
	}
	mustNotContain(t, body, "─ Overview ─")
	mustContain(t, v, "total labels: 22")
	mustNotContain(t, v, "─ Namespaces ─")
	// Namespace headings for seeded namespaces.
	mustContain(t, v, "context:")
	mustContain(t, v, "status:")
	mustContain(t, v, "type:")
	mustContain(t, v, "priority:")
	// A seeded label's description is rendered.
	mustContain(t, v, "workflow state: open")
}

func TestLabelsTabCallsOutMissingDescriptions(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 60)
	seedProject(t, m, "ATM", "Acme")
	seedLabel(t, m, "ATM:patch:urgent", "")
	update(t, m, "s")
	update(t, m, "3")
	v := m.labels.View()
	mustContain(t, v, "ATM:patch:urgent")
	mustContain(t, v, "needs description")
}

// TestLabelsListScrollsWithCursor verifies the list window follows the
// cursor: a namespace-grouped label past the first page is not rendered
// until the cursor reaches it (regression guard: the list previously never
// scrolled, so the cursor could run off the bottom of the pane while the
// rendered rows stayed fixed).
func TestLabelsListScrollsWithCursor(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(200, 20) // shrink the labels pane so the seeded set needs paging
	update(t, m, "s")
	update(t, m, "3")

	rows := m.labels.rows
	if len(rows) < 10 {
		t.Fatalf("expected several seeded labels, got %d", len(rows))
	}
	last := rows[len(rows)-1]
	if strings.Contains(m.labels.View(), last.full) {
		t.Fatalf("expected %s to be scrolled out of view initially:\n%s", last.full, m.labels.View())
	}
	m.labels.cursor = len(m.labels.entries) - 1
	view := m.labels.View()
	if !strings.Contains(view, last.full) {
		t.Fatalf("cursor on %s but it is not visible:\n%s", last.full, view)
	}
}

// TestLabelsBracketKeysPageThroughList verifies "]"/"[" jump the cursor a
// full page forward/backward.
func TestLabelsBracketKeysPageThroughList(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(200, 20)
	update(t, m, "s")
	update(t, m, "3")
	start := m.labels.cursor
	update(t, m, "]")
	if m.labels.cursor <= start {
		t.Fatalf("] should move cursor forward, got %d (was %d)", m.labels.cursor, start)
	}
	after := m.labels.cursor
	update(t, m, "[")
	if m.labels.cursor >= after {
		t.Fatalf("[ should move cursor backward, got %d (was %d)", m.labels.cursor, after)
	}
}

func TestLabelDetailDashboardSections(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "j") // cursor 0 is a namespace header; step onto the first row
	update(t, m, "enter")
	v := m.View()
	mustContain(t, v, "Label ")
	mustContain(t, v, "FACTS")
	mustContain(t, v, "usage")
	mustContain(t, v, "description")
	mustNotContain(t, v, "Actions")
	hint := m.labels.statusHint()
	if hint != "[d]esc [l]remove [Esc]back" {
		t.Fatalf("labels detail statusHint = %q want [d]esc [l]remove [Esc]back", hint)
	}
	mustContain(t, v, "[d]esc")
	mustContain(t, v, "[l]remove")
	mustContain(t, v, "[Esc]back")
}

func TestLabelsEntriesIncludeNamespaceHeaders(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")

	if len(m.labels.entries) == 0 {
		t.Fatalf("entries not built")
	}
	// The seeded set has a status: namespace; there must be a header entry for
	// it that precedes its first row entry.
	headerIdx, rowIdx := -1, -1
	for i, e := range m.labels.entries {
		if e.kind == entryHeaderNS && e.ns == "status" && headerIdx == -1 {
			headerIdx = i
		}
		if e.kind == entryRow && strings.HasPrefix(e.row.suffix, "status:") && rowIdx == -1 {
			rowIdx = i
		}
	}
	if headerIdx == -1 {
		t.Fatalf("no status namespace header entry")
	}
	if rowIdx == -1 || rowIdx <= headerIdx {
		t.Fatalf("status row (%d) should follow its header (%d)", rowIdx, headerIdx)
	}
	// entries must contain more items than rows (headers add slots).
	if len(m.labels.entries) <= len(m.labels.rows) {
		t.Fatalf("entries (%d) should exceed rows (%d) due to headers", len(m.labels.entries), len(m.labels.rows))
	}
}

// TestLabelsCursorCanReachNamespaceHeader verifies the handleListKey cursor
// clamp uses len(entries) (not len(rows)): with an unnamespaced tag appended,
// the trailing tags: header sits at an entry index beyond len(rows)-1, so j
// navigation can only reach it under the entries-based clamp.
func TestLabelsCursorCanReachNamespaceHeader(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Add an unnamespaced tag so a tags: header appears at the tail of the
	// entries list, at an index the pre-fix len(rows)-1 clamp would block.
	if err := m.store.LabelAdd("ATM:urgent", "", "claude"); err != nil {
		t.Fatalf("LabelAdd ATM:urgent: %v", err)
	}
	m.refreshAll()
	update(t, m, "s")
	update(t, m, "3")

	tagsHeaderIdx := -1
	for i, e := range m.labels.entries {
		if e.kind == entryHeaderTags {
			tagsHeaderIdx = i
			break
		}
	}
	if tagsHeaderIdx == -1 {
		t.Fatalf("no tags: header entry; entries=%d", len(m.labels.entries))
	}
	if tagsHeaderIdx <= len(m.labels.rows)-1 {
		t.Fatalf("test setup error: tags header at %d must exceed len(rows)-1=%d to exercise the clamp", tagsHeaderIdx, len(m.labels.rows)-1)
	}

	// Drive j to saturation: the cursor must reach the last entry under the
	// len(entries) clamp; the old len(rows)-1 clamp would stop it short.
	for i := 0; i < len(m.labels.entries)+2; i++ {
		prev := m.labels.cursor
		update(t, m, "j")
		if m.labels.cursor == prev {
			break
		}
	}
	if m.labels.cursor != len(m.labels.entries)-1 {
		t.Fatalf("j saturation stopped at cursor %d, want %d (entries clamp)", m.labels.cursor, len(m.labels.entries)-1)
	}
	// Step up onto the tags: header and confirm it is selectable.
	update(t, m, "k")
	if m.labels.cursor != tagsHeaderIdx {
		t.Fatalf("cursor %d, want tags header idx %d", m.labels.cursor, tagsHeaderIdx)
	}
	if got := m.labels.entries[m.labels.cursor].kind; got != entryHeaderTags {
		t.Fatalf("cursor at entry kind %v, want entryHeaderTags", got)
	}
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
	// The label is now in the registry.
	if _, err := m.store.LabelShow("ATM:patch:urgent"); err != nil {
		t.Errorf("ATM:patch:urgent not in registry after add: %v", err)
	}
}

func TestLabelsTabDescribeLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "d") // describe form
	if m.form == nil {
		t.Fatalf("describe form not open")
	}
	// First field is the label name (suffix).
	for _, r := range "status:open" {
		update(t, m, string(r))
	}
	update(t, m, "tab")
	for _, r := range "curated description" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	l, _ := m.store.LabelShow("ATM:status:open")
	if l.Description != "curated description" {
		t.Fatalf("description = %q want \"curated description\"", l.Description)
	}
}

func TestLabelsTabRemoveLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Attach the label to a task so retained_usage > 0.
	seedTask(t, m, "ATM", "t", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "l") // remove form
	if m.form == nil {
		t.Fatalf("remove form not open")
	}
	for _, r := range "status:open" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if !strings.Contains(m.toastMsg, "retained usage") {
		t.Fatalf("toast = %q, want retained usage", m.toastMsg)
	}
}

func TestLabelsTabSeedKey(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Remove a seed label.
	_, _ = m.store.LabelRemove("ATM:context:fixit", "claude")
	m.refreshAll()
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "S") // seed key
	if !strings.Contains(m.toastMsg, "seeded 22 labels into ATM") {
		t.Fatalf("toast = %q, want seeded 22 labels into ATM", m.toastMsg)
	}
	// The removed label is back.
	if _, err := m.store.LabelShow("ATM:context:fixit"); err != nil {
		t.Errorf("ATM:context:fixit not restored after seed: %v", err)
	}
}

// cursorToNamespaceHeader moves the Labels cursor onto the first header entry
// for ns and returns its index.
func cursorToNamespaceHeader(t *testing.T, m *Model, ns string) {
	t.Helper()
	for i, e := range m.labels.entries {
		if e.kind == entryHeaderNS && e.ns == ns {
			m.labels.cursor = i
			return
		}
	}
	t.Fatalf("no namespace header entry for %q", ns)
}

func TestLabelsEnterOnNamespaceTogglesFacetAndChart(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")

	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	if m.tasks.filter != "ATM:status:*" {
		t.Fatalf("filter = %q want ATM:status:*", m.tasks.filter)
	}
	if m.labels.chartNS != "status" {
		t.Fatalf("chartNS = %q want status", m.labels.chartNS)
	}

	// Enter again on the same namespace toggles it off. Esc back to the list
	// first (chart mode is esc-only), preserving the filter, then Enter on
	// the header removes the facet token.
	update(t, m, "esc")
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	if m.tasks.filter != "" {
		t.Fatalf("filter = %q want empty after toggle off", m.tasks.filter)
	}
	if m.labels.chartNS != "" {
		t.Fatalf("chartNS = %q want empty after toggle off", m.labels.chartNS)
	}
}

func TestLabelsEnterOnTagsHeaderIsNoop(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Add an unnamespaced tag so a tags header exists.
	if err := m.store.LabelAdd("ATM:urgent", "", m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")

	found := false
	for i, e := range m.labels.entries {
		if e.kind == entryHeaderTags {
			m.labels.cursor = i
			found = true
		}
	}
	if !found {
		t.Fatalf("no tags header entry")
	}
	update(t, m, "enter")
	if m.tasks.filter != "" {
		t.Fatalf("filter = %q want empty (tags header is a no-op)", m.tasks.filter)
	}
	if m.labels.chartNS != "" {
		t.Fatalf("chartNS = %q want empty", m.labels.chartNS)
	}
}

func TestLabelsChartSelfHealsWhenFilterEditedAway(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	if m.labels.chartNS != "status" {
		t.Fatalf("precondition: chartNS should be status")
	}
	// Simulate the user clearing the Tasks filter out from under the chart.
	m.tasks.filter = ""
	if got := m.labels.activeChartNS(); got != "" {
		t.Fatalf("activeChartNS = %q want empty after filter cleared", got)
	}
	if m.labels.chartNS != "" {
		t.Fatalf("chartNS should self-heal to empty")
	}
}

func TestLabelsEscClosesChartWithoutClearingFilter(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	update(t, m, "esc")
	if m.labels.chartNS != "" {
		t.Fatalf("chartNS = %q want empty after esc", m.labels.chartNS)
	}
	if m.tasks.filter != "ATM:status:*" {
		t.Fatalf("filter = %q want ATM:status:* preserved after esc", m.tasks.filter)
	}
}

func TestLabelsChartShowsUsageBars(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Give status:open a usage count so a non-empty bar renders.
	if _, err := m.store.CreateTask("ATM", "t1", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter") // open chart

	v := m.labels.View()
	mustContain(t, v, "chart: status")
	mustContain(t, v, "namespace: status")
	mustContain(t, v, "ATM:status:open")
	mustContain(t, v, "█") // at least one filled meter cell
	mustContain(t, v, "[Esc] back")
}

func TestLabelsChartStatusHint(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	hint := m.labels.statusHint()
	mustContain(t, hint, "[Esc]back")
}

func TestLabelsListStatusHintShowsSelect(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	mustContain(t, m.labels.statusHint(), "[Enter]")
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
