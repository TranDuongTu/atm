package tui

import (
	"strings"
	"testing"
)

// mkActorsTestModel builds a Model on top of the shared newTestModel harness,
// seeding a project "ATM" with a couple of actor-stamped events (persona
// "staff", agent "claude", model "opus-4.8" per the actor.Resolve convention
// "persona@agent:model").
func mkActorsTestModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModelWithActor(t, "staff@claude:opus-4.8")
	seedProjectAsActor(t, m, "ATM", "Acme Task Manager", "staff@claude:opus-4.8")
	seedTaskAsActor(t, m, "ATM", "task one", "staff@claude:opus-4.8")
	return m
}

// seedProjectAsActor creates a project stamped with the given actor string.
func seedProjectAsActor(t *testing.T, m *Model, code, name, actor string) {
	t.Helper()
	if _, err := m.store.CreateProject(code, name, actor); err != nil {
		t.Fatalf("CreateProject %s: %v", code, err)
	}
	m.refreshAll()
}

// seedTaskAsActor creates a task stamped with the given actor string.
func seedTaskAsActor(t *testing.T, m *Model, projectCode, title, actor string) {
	t.Helper()
	if _, err := m.store.CreateTask(projectCode, title, "", nil, actor); err != nil {
		t.Fatalf("CreateTask %s: %v", title, err)
	}
	m.refreshAll()
}

func TestActorsPaneRendersChart(t *testing.T) {
	m := mkActorsTestModel(t)
	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.focused = paneActors
	m.actors.refresh()
	view := m.actors.View()
	if !strings.Contains(view, "staff") {
		t.Fatalf("actors view missing persona row:\n%s", view)
	}
}

func TestTabReachesActorsPane(t *testing.T) {
	m := mkActorsTestModel(t)
	m.SetSize(80, 24)
	update(t, m, "4")
	if m.focused != paneActors {
		t.Fatalf("focused = %v, want paneActors", m.focused)
	}
}
