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
	m.store.LabelAdd("ATM:urgent", "", m.actor)
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
	update(t, m, "esc")
	if m.tasks.focus.mode != focusOff || m.labels.level != lLevelTable {
		t.Fatalf("esc from none leaf did not return to table/clear: %+v %v", m.tasks.focus, m.labels.level)
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
