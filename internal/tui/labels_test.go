package tui

import (
	"strings"
	"testing"
)

// --- Labels tab tests ---

func TestLabelsTabEmptyStateNoProject(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "3") // switch to Labels tab
	if m.focused != paneLabels {
		t.Fatalf("focus = %v want paneLabels", m.focused)
	}
	v := m.View()
	mustContain(t, v, "no project selected")
	mustContain(t, v, "press [s] in the Projects tab")
}

func TestLabelsTabListSeededLabels(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // select ATM
	update(t, m, "3") // Labels tab
	v := m.View()
	body := m.labels.View()
	if strings.HasPrefix(body, "Labels\n") {
		t.Fatalf("labels body repeats tab title\n--- body ---\n%s", body)
	}
	mustContain(t, body, "─ Overview ─")
	mustContain(t, v, "total labels: 17")
	mustContain(t, v, "─ Namespaces ─")
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
	seedProject(t, m, "ATM", "Acme")
	seedLabel(t, m, "ATM:patch:urgent", "")
	update(t, m, "s")
	update(t, m, "3")
	v := m.View()
	mustContain(t, v, "ATM:patch:urgent")
	mustContain(t, v, "needs description")
}

func TestLabelDetailDashboardSections(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "enter")
	v := m.View()
	mustContain(t, v, "Label ")
	mustContain(t, v, "─ Facts ─")
	mustContain(t, v, "usage")
	mustContain(t, v, "description")
	mustContain(t, v, "─ Actions ─")
	mustContain(t, v, "[d] describe")
	mustContain(t, v, "[l] remove")
	mustContain(t, v, "[Esc] back")
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
	if !strings.Contains(m.toastMsg, "seeded 17 labels into ATM") {
		t.Fatalf("toast = %q, want seeded 17 labels into ATM", m.toastMsg)
	}
	// The removed label is back.
	if _, err := m.store.LabelShow("ATM:context:fixit"); err != nil {
		t.Errorf("ATM:context:fixit not restored after seed: %v", err)
	}
}
