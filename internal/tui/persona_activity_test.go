package tui

import (
	"strings"
	"testing"

	"atm/internal/core"
)

func TestPersonaActivityOverlayRendersSnapshotAndCloses(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	now := core.Now()
	entries := []core.LogEntry{
		{At: now, Actor: "developer@codex:gpt-5", Action: "task.created"},
		{At: now, Actor: "developer@codex:gpt-5", Action: "task.updated"},
		{At: now, Actor: "manager@claude:opus", Action: "task.created"},
	}

	m.personaAct.openFor("developer", chartRanges[0], entries)
	view := stripANSI(m.personaAct.renderOverlay())
	for _, want := range []string{
		personaIcon("developer") + " developer \u00b7 1w",
		"2 events",
		"models",
		"gpt-5",
		"agents",
		"codex",
		"actions",
		"task.created",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
		}
	}
	for _, section := range [][2]string{{"models", "agents"}, {"agents", "actions"}} {
		if strings.Index(view, section[0]) > strings.Index(view, section[1]) {
			t.Errorf("%s must precede %s:\n%s", section[0], section[1], view)
		}
	}

	update(t, m, "esc")
	if m.personaAct.open {
		t.Fatal("Esc must close the persona activity overlay")
	}
}

func TestPersonaActivityOverlayContainsNoDispatchAffordance(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.personaAct.openFor("", chartRanges[1], []core.LogEntry{
		{At: core.Now(), Actor: "developer@codex:gpt-5", Action: "task.created"},
	})

	if view := strings.ToLower(stripANSI(m.personaAct.renderOverlay())); strings.Contains(view, "dispatch") {
		t.Fatalf("persona activity overlay must be read-only, got:\n%s", view)
	}
}

func TestChartEnterOpensPersonaActivityOverlayForAllAndCurrentRange(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.focused = paneProjects
	m.projects.chartFocused = true
	m.projects.chartRange = 1
	m.projects.chartPersona = ""
	m.projects.summaryEntries = []core.LogEntry{
		{At: core.Now(), Actor: "developer@codex:gpt-5", Action: "task.created"},
	}

	update(t, m, "enter")
	if !m.personaAct.open {
		t.Fatal("Enter while the chart is focused must open the persona activity overlay")
	}
	if m.personaAct.key != "" {
		t.Fatalf("overlay key = %q, want All", m.personaAct.key)
	}
	if got := m.personaAct.spec.key; got != "1m" {
		t.Fatalf("overlay range = %q, want 1m", got)
	}
}
