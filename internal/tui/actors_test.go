package tui

import (
	"strings"
	"testing"

	"atm/internal/core"
	"github.com/charmbracelet/lipgloss"
)

func mkActorsOverlayTestModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModelWithActor(t, "staff@claude:opus-4.8")
	seedStaffPersona(t, m)
	seedProjectAsActor(t, m, "ATM", "Acme Task Manager", "staff@claude:opus-4.8")
	seedTaskAsActor(t, m, "ATM", "task one", "staff@claude:opus-4.8")
	return m
}

// seedStaffPersona registers the "staff" persona so actor strings of the form
// "staff@..." satisfy the store's validateActor gate.
func seedStaffPersona(t *testing.T, m *Model) {
	t.Helper()
	if _, err := m.store.CreatePersona("staff", "high bar", "Staff engineer", "admin@cli:unset"); err != nil {
		t.Fatalf("CreatePersona staff: %v", err)
	}
}

func seedProjectAsActor(t *testing.T, m *Model, code, name, actor string) {
	t.Helper()
	if _, err := m.store.CreateProject(code, name, actor); err != nil {
		t.Fatalf("CreateProject %s: %v", code, err)
	}
	m.refreshAll()
}

func seedTaskAsActor(t *testing.T, m *Model, projectCode, title, actor string) {
	t.Helper()
	if _, err := m.store.CreateTask(projectCode, title, "", nil, actor); err != nil {
		t.Fatalf("CreateTask %s: %v", title, err)
	}
	m.refreshAll()
}

func TestChartCtrlArrowsWrapAndClampRangeWithoutStickyFocus(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	// Seed a second persona so carousel traversal can wrap.
	seedTaskAsActor(t, m, "ATM", "task two", "developer@claude:opus-4.8")
	m.refreshAll()

	update(t, m, "ctrl+left")
	if got := m.projects.chartPersona; got != "developer" {
		t.Fatalf("ctrl+left from All selected %q, want developer", got)
	}
	update(t, m, "ctrl+right")
	if got := m.projects.chartPersona; got != "" {
		t.Fatalf("ctrl+right should wrap developer to All, got %q", got)
	}
	for range chartRanges {
		update(t, m, "ctrl+up")
	}
	if got, want := m.projects.chartRange, len(chartRanges)-1; got != want {
		t.Fatalf("chartRange after ctrl+up = %d, want %d", got, want)
	}
	for range chartRanges {
		update(t, m, "ctrl+down")
	}
	if got := m.projects.chartRange; got != 0 {
		t.Fatalf("chartRange after ctrl+down = %d, want 0", got)
	}
}

func TestChartEnterEscAreGatedByFocus(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	m.refreshAll()

	if _, handled := m.projects.handleChartKey(keyMsg("enter")); handled {
		t.Fatal("enter should reach the project list while chart is unfocused")
	}
	if _, handled := m.projects.handleChartKey(keyMsg("esc")); handled {
		t.Fatal("esc should reach normal pane handling while chart is unfocused")
	}
	update(t, m, "ctrl+right")
	if _, handled := m.projects.handleChartKey(keyMsg("enter")); handled {
		t.Fatal("enter should still reach the project list after ctrl chart navigation")
	}
	if _, handled := m.projects.handleChartKey(keyMsg("esc")); handled {
		t.Fatal("esc should still reach normal pane handling after ctrl chart navigation")
	}
}

func TestChartStateResetsOnProjectSwitch(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	seedProject(t, m, "SCY", "Scylla")
	m.focused = paneProjects
	update(t, m, "s")
	update(t, m, "ctrl+right")
	update(t, m, "ctrl+up")
	if m.projects.chartRange == 0 {
		t.Fatal("setup failed to change chart state")
	}
	m.projects.cursor = 1
	update(t, m, "s")
	if m.projectScope != "SCY" {
		t.Fatalf("projectScope = %q, want SCY", m.projectScope)
	}
	if m.projects.chartPersona != "" || m.projects.chartRange != 0 {
		t.Fatalf("chart state after project switch = (%q, %d), want zero state", m.projects.chartPersona, m.projects.chartRange)
	}
}

func TestRenderSummaryCombinedChart(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.refreshAll()

	body := m.projects.renderSummary(12)
	mustContain(t, body, "activity \u00b7 1w")
	mustContain(t, body, "All")
	mustNotContain(t, body, "activity by persona")
	mustNotContain(t, body, "activity stripe")

	m.projects.summaryEntries = nil
	empty := m.projects.renderSummary(12)
	if !strings.Contains(empty, "no activity yet") {
		t.Fatalf("empty summary missing activity placeholder:\n%s", empty)
	}
}

func TestRenderSummaryPersonaCardsShowIconsRangeTotalsAndTopModels(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	m.projectScope = "ATM"
	now := core.Now()
	m.projects.summaryOK = true
	m.projects.summaryProject = &core.Project{Code: "ATM", Name: "Acme Task Manager"}
	m.projects.summaryEntries = []core.LogEntry{
		{At: now, Actor: "developer@codex:gpt-5", Action: "task.created"},
		{At: now.AddDate(0, 0, -1), Actor: "developer@codex:o3", Action: "task.updated"},
		{At: now.AddDate(0, 0, -2), Actor: "developer@claude:sonnet", Action: "task.commented"},
		{At: now.AddDate(0, 0, -3), Actor: "developer@codex:gpt-5", Action: "task.closed"},
		{At: now.AddDate(0, 0, -1), Actor: "manager@claude:opus", Action: "project.reviewed"},
		{At: now.AddDate(0, 0, -10), Actor: "developer@claude:old", Action: "outside.range"},
	}

	body := stripANSI(m.projects.renderSummary(16))
	for _, want := range []string{
		personaIcon("") + " All",
		personaIcon("developer") + " developer",
		personaIcon("manager") + " manager",
		"5 activities",
		"4 activities",
		"1 activity",
		"gpt-5, o3, opus",
		"gpt-5, o3, sonnet",
		"opus",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("summary missing %q\n--- body ---\n%s", want, body)
		}
	}
	if strings.Contains(body, "old") {
		t.Fatalf("summary included a model outside the selected range:\n%s", body)
	}
}

// ensure lipgloss is used (silences unused-import in trim builds).
var _ = lipgloss.Width
