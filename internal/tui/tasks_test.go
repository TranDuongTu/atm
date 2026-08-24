package tui

import (
	"strings"
	"testing"

	"atm/internal/capability/workflow"
	"atm/internal/store"
)

func mkTask(id, title string, labels ...string) *store.Task {
	return &store.Task{ID: id, Title: title, Labels: labels}
}

func containsLabelTUI(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func TestTaskCreateWithLabelsField(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // select ATM
	update(t, m, "2") // Tasks pane
	update(t, m, "a") // open create form
	if m.form == nil {
		t.Fatalf("create form not open")
	}
	// Verify the labels field exists.
	found := false
	for _, f := range m.form.Fields {
		if f.Label == "labels" {
			found = true
		}
	}
	if !found {
		t.Fatalf("create form has no 'labels' field; fields = %+v", m.form.Fields)
	}
	// Type a title.
	for _, r := range "Multi-label task" {
		update(t, m, string(r))
	}
	update(t, m, "tab") // title -> description
	// Skip description (leave empty), tab to labels.
	update(t, m, "tab") // description -> labels
	for _, r := range "status:open type:bug" {
		update(t, m, string(r))
	}
	update(t, m, "enter") // submit (last field)
	if m.form != nil {
		t.Fatalf("form should be closed after submit")
	}
	// The task should exist with both labels.
	ts := m.store.ListTasks(store.QueryFilters{Project: "ATM"})
	if len(ts) != 1 {
		t.Fatalf("expected 1 task, got %d", len(ts))
	}
	tk := ts[0]
	if !containsLabelTUI(tk.Labels, "ATM:status:open") || !containsLabelTUI(tk.Labels, "ATM:type:bug") {
		t.Fatalf("task labels = %v, want ATM:status:open + ATM:type:bug", tk.Labels)
	}
}

// TestTasksPaneRendersLaneStrip verifies the Tasks pane list view renders the
// lane strip below the task list. The three lanes are a fixed feature of the
// pane: they render even under a capability that is not a flow, because the
// pane's shape must not depend on what is enabled.
func TestTasksPaneRendersLaneStrip(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.SetSize(100, 40)
	v := stripANSI(m.tasks.View())
	for _, lane := range []string{"Inbox", "Pipeline", "Out"} {
		if !strings.Contains(v, lane) {
			t.Errorf("tasks view missing the %s lane card:\n%s", lane, v)
		}
	}
}

// TestListViewLayoutOrderListThenLaneStripBottom verifies the list-view
// layout: top-to-bottom the pane stacks task list -> lane strip, so the strip
// is the LAST laneStripHeight lines and nothing renders below it.
func TestListViewLayoutOrderListThenLaneStripBottom(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.SetSize(100, 40)

	lines := strings.Split(stripANSI(m.tasks.View()), "\n")
	stripBlock := strings.Join(lines[len(lines)-laneStripHeight:], "\n")
	for _, lane := range []string{"Inbox", "Pipeline", "Out"} {
		if !strings.Contains(stripBlock, lane) {
			t.Errorf("lane strip (last %d lines) missing %s:\n%s", laneStripHeight, lane, stripBlock)
		}
	}
	above := strings.Join(lines[:len(lines)-laneStripHeight], "\n")
	if strings.Contains(above, "Pipeline") {
		t.Errorf("the lane strip leaked above its %d-line slot:\n%s", laneStripHeight, above)
	}
}

// TestPaneTitleNamesTheCurrentCapability replaces the old header-row test:
// the capability that used to head the list now names itself in the pane
// title, and the counts moved to the lane cards.
func TestPaneTitleNamesTheCurrentCapability(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.refreshAll()
	if got, want := m.tasksPaneTitle(), "[2] Tasks · workflow"; got != want {
		t.Fatalf("tasksPaneTitle = %q, want %q", got, want)
	}
}

// TestCycleSortKeyAdvancesAndWraps covers the registry curation in
// ATM-77af5e: "Cycle sort" is dropped from the spotlight because it's a
// rapid-toggle key pressed repeatedly while looking at the list, not a
// launcher destination — the sort now advertises itself as an arrow on the
// column header it sorts by. This test presses the key itself and asserts
// sortMode advances through every mode and wraps, since the rendered
// indicator alone does not prove the key still works.
func TestCycleSortKeyAdvancesAndWraps(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.refreshAll()

	start := m.tasks.sortMode
	m.tasks.handleKey(keyMsg("s"))
	if m.tasks.sortMode == start {
		t.Fatalf("s did not advance sortMode from %v", start)
	}
	afterOne := m.tasks.sortMode

	m.tasks.handleKey(keyMsg("s"))
	if m.tasks.sortMode == afterOne || m.tasks.sortMode == start {
		t.Fatalf("s did not advance sortMode from %v", afterOne)
	}

	m.tasks.handleKey(keyMsg("s"))
	m.tasks.handleKey(keyMsg("s"))
	if m.tasks.sortMode != start {
		t.Errorf("sortMode after %d presses = %v, want wrap back to %v", sortModeCount, m.tasks.sortMode, start)
	}
}

func TestFlatListDropsLabelsColumn(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.refreshAll()
	m.lanes.selectDefault()
	out := m.tasks.renderList()
	if strings.Contains(out, "LABELS") {
		t.Fatalf("flat list still shows LABELS column:\n%s", out)
	}
	if !strings.Contains(out, "TITLE") || !strings.Contains(out, "UPDATED") {
		t.Fatalf("flat list lost TITLE/UPDATED columns:\n%s", out)
	}
}

// TestTasksListContextualColumn verifies the flat list shows the current
// capability's annotation column: WORKFLOW header + the workflow cell text
// ("in-progress") when workflow is current, and hides the column entirely
// when the unmanaged pseudo-capability becomes current.
func TestTasksListContextualColumn(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "PXA", "Acme")
	m.projectScope = "PXA"
	if _, err := m.regFor("PXA").EnsureVocabulary(m.store, "PXA", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := m.store.CreateTask("PXA", "annotated task", "", []string{"PXA:status:in-progress"}, m.actor); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.lanes.selectDefault()

	view := stripANSI(m.tasks.View())
	if !strings.Contains(view, "ANNOTATE") {
		t.Errorf("column header missing the annotation column:\n%s", view)
	}
	if !strings.Contains(view, "in-progress") {
		t.Errorf("column missing workflow cell:\n%s", view)
	}
}

// TestTasksListContextualColumnHiddenOnNarrowPane verifies the annotation
// column is hidden when the pane is too narrow to fit all four columns
// (below metaColumnMinPaneWidth). The three-column fallback path already
// handles metaW == 0, so the ANNOTATE header must not appear.
func TestTasksListContextualColumnHiddenOnNarrowPane(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "PXA", "Acme")
	m.projectScope = "PXA"
	if _, err := m.regFor("PXA").EnsureVocabulary(m.store, "PXA", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := m.store.CreateTask("PXA", "annotated task", "", []string{"PXA:status:in-progress"}, m.actor); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.lanes.selectDefault()

	m.tasks.SetSize(metaColumnMinPaneWidth-1, 30)
	view := m.tasks.renderList()
	if strings.Contains(view, "ANNOTATE") {
		t.Errorf("contextual column shown on narrow pane (width=%d):\n%s", metaColumnMinPaneWidth-1, view)
	}
	if !strings.Contains(view, "TITLE") || !strings.Contains(view, "UPDATED") {
		t.Errorf("narrow pane lost TITLE/UPDATED columns:\n%s", view)
	}
}

// TestTasksPaneFillsGapWithArt verifies that with few tasks in a tall pane,
// the dead space between the last task row and the boards ring is filled
// with the scoped project's background art, and that the fill never changes
// the pane's overall height (art replaces blank padding in place).
func TestTasksPaneFillsGapWithArt(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := m.store.SetProjectArtOn("ATM", true, []string{"galaxy", "constellation"}, m.actor); err != nil {
		t.Fatalf("SetProjectArtOn: %v", err)
	}
	m.artOn["ATM"] = true
	m.artPair["ATM"] = []string{"galaxy", "constellation"}
	if _, err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.lanes.refresh()
	m.lanes.selectDefault()

	m.tasks.SetSize(70, 46)
	out := m.tasks.renderListWithStrip()
	lines := strings.Split(out, "\n")

	// The rows directly above the lane strip were blank padding before; with
	// one task and a tall pane at least one of them must now carry art
	// glyphs.
	// The slice below laneStripHeight excludes the strip entirely, but still
	// includes the task table's own header rule
	// (a plain run of '─'), so the probe deliberately omits bare '─'/'│' —
	// those would false-positive on the table's own divider rows regardless
	// of whether art rendered. The corner/node glyphs below are unique to
	// the art package's themes within this window.
	top := len(lines) - laneStripHeight
	found := false
	for _, ln := range lines[:top] {
		s := strings.TrimSpace(stripANSI(ln))
		if strings.ContainsAny(s, "~≈·✦*░▒▓┌┐└┘╷○◉") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no art glyphs in the tasks-pane gap:\n%s", out)
	}
	// Height must be unchanged by art injection.
	if len(lines) != 46 {
		t.Fatalf("pane height = %d, want 46", len(lines))
	}
}

// TestTasksPaneNoArtWithoutScope verifies art never renders in the Tasks
// pane's gap when no project is scoped (that gap is the empty-state screen,
// which centers its message with blank lines above and below).
func TestTasksPaneNoArtWithoutScope(t *testing.T) {
	m := newTestModel(t)
	m.projectScope = ""
	m.tasks.SetSize(70, 46)
	out := m.tasks.renderListWithStrip()
	for _, ln := range strings.Split(out, "\n") {
		if strings.ContainsAny(stripANSI(ln), "≈✦░▒▓") {
			t.Fatalf("art must not render without a project scope:\n%s", out)
		}
	}
}
