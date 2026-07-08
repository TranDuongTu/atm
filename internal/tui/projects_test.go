package tui

import (
	"strings"
	"testing"

	"atm/internal/actor"
	"atm/internal/store"
)

func seedAlias(t *testing.T, m *Model, raw, persona, agent string) {
	t.Helper()
	if err := m.store.SetAlias(raw, actor.AliasEntry{Persona: persona, Agent: agent}); err != nil {
		t.Fatalf("SetAlias %s: %v", raw, err)
	}
}

func TestRenderPersonaActivityChart(t *testing.T) {
	m := newTestModelWithActor(t, "staff@claude:opus-4.8")
	if _, err := m.store.CreateProject("ATM", "Acme Task Manager", "staff@claude:opus-4.8"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateTask("ATM", "task one", "", nil, "staff@claude:opus-4.8"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateTask("ATM", "task two", "", nil, "staff@claude:opus-4.8"); err != nil {
		t.Fatal(err)
	}
	seedAlias(t, m, "claude", "developer", "claude")

	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()

	entries, err := m.store.ReadLog("ATM")
	if err != nil && !store.IsIntegrity(err) {
		t.Fatalf("ReadLog: %v", err)
	}
	lines := m.projects.renderPersonaActivityChart(entries, 8)
	view := strings.Join(lines, "\n")
	if !strings.Contains(view, "activity by persona") {
		t.Fatalf("chart title wrong:\n%s", view)
	}
	if !strings.Contains(view, "staff") {
		t.Fatalf("missing persona row 'staff':\n%s", view)
	}
}

func TestRenderPersonaActivityChartEmpty(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()
	entries, _ := m.store.ReadLog("ATM")
	lines := m.projects.renderPersonaActivityChart(entries, 1)
	view := strings.Join(lines, "\n")
	if !strings.Contains(view, "activity by persona") {
		t.Fatalf("degenerate title wrong:\n%s", view)
	}
}