package tui

import (
	"strings"
	"testing"

	"atm/internal/core"
	tea "github.com/charmbracelet/bubbletea"
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

func TestChartCtrlArrowsWrapClampRangeAndKeepChartFocused(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	// Seed a second persona so carousel traversal can wrap.
	seedTaskAsActor(t, m, "ATM", "task two", "developer@claude:opus-4.8")
	m.refreshAll()

	update(t, m, "ctrl+left")
	mustContain(t, stripANSI(m.projects.renderSummary(12)), "\u25b8 activity")
	if got := m.projects.chartPersona; got != "developer" {
		t.Fatalf("ctrl+left from All selected %q, want developer", got)
	}
	update(t, m, "j")
	mustContain(t, stripANSI(m.projects.renderSummary(12)), "\u25b8 activity")

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
	if _, handled := m.projects.handleChartKey(keyMsg("enter")); !handled {
		t.Fatal("enter should drill into the chart after ctrl chart navigation focuses it")
	}

	m = mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	m.refreshAll()
	update(t, m, "ctrl+right")
	if _, handled := m.projects.handleChartKey(keyMsg("esc")); handled {
		t.Fatal("esc should still reach normal pane handling after ctrl chart navigation")
	}
}

func TestChartFocusedEnterOpensInlineDrillInsteadOfProjectDetail(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(120, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	m.refreshAll()

	update(t, m, "ctrl+right")
	update(t, m, "enter")

	if m.projects.view != pViewList {
		t.Fatalf("focused chart enter opened projects view %v, want list with inline drill", m.projects.view)
	}
	if !m.projects.chartDrill {
		t.Fatal("focused chart enter should open inline drill")
	}
	body := stripANSI(m.projects.renderSummary(16))
	mustContain(t, body, "[Esc] back")
}

func TestChartCtrlEnterOpensInlinePersonaDrillWithoutOverlay(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(160, 44)
	m.projectScope = "ATM"
	m.focused = paneProjects
	now := core.Now()
	m.projects.summaryOK = true
	m.projects.summaryProject = &core.Project{Code: "ATM", Name: "Acme Task Manager"}
	m.projects.summaryEntries = []core.LogEntry{
		{At: now, Actor: "developer@codex:gpt-5", Action: "task.created"},
		{At: now, Actor: "developer@codex:gpt-5", Action: "task.updated"},
		{At: now, Actor: "manager@claude:opus", Action: "project.reviewed"},
	}

	update(t, m, "ctrl+left")
	if got := m.projects.chartPersona; got != "manager" {
		t.Fatalf("setup selected persona = %q, want manager", got)
	}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = mm.(*Model)

	if m.personaAct.open {
		t.Fatal("ctrl+enter must not open the persona activity overlay")
	}
	body := stripANSI(m.projects.renderSummary(16))
	for _, want := range []string{
		personaIcon("manager") + " manager",
		"1 event",
		"models",
		"opus",
		"agents",
		"claude",
		"actions",
		"project.reviewed",
		"[Esc] back",
	} {
		mustContain(t, body, want)
	}
}

func TestChartEscBacksOutOfInlinePersonaDrill(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(120, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	m.refreshAll()

	update(t, m, "ctrl+right")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = mm.(*Model)
	if body := stripANSI(m.projects.renderSummary(14)); !strings.Contains(body, "models") {
		t.Fatalf("setup: inline drill did not render\n%s", body)
	}

	update(t, m, "esc")
	body := stripANSI(m.projects.renderSummary(14))
	mustNotContain(t, body, "[Esc] back")
	mustNotContain(t, body, "models")
	mustContain(t, body, "Range: One week")
}

func TestChartDrillDoesNotStealProjectDetailEsc(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(120, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	m.refreshAll()

	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = mm.(*Model)
	m.projects.openDetail("ATM")
	if m.projects.view != pViewDetail {
		t.Fatalf("setup: project view = %v, want detail", m.projects.view)
	}

	update(t, m, "esc")
	if m.projects.view != pViewList {
		t.Fatalf("esc from project detail with chart drill state = %v, want list", m.projects.view)
	}
}

func TestChartCtrlEnterFocusesAndOpensInlinePersonaDrill(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(120, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	m.refreshAll()
	m.projects.chartPersona = "staff"
	m.projects.chartFocused = false

	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = mm.(*Model)

	if !m.projects.chartFocused {
		t.Fatal("ctrl+enter should focus the activity chart")
	}
	if !m.projects.chartDrill {
		t.Fatal("ctrl+enter should open the inline persona breakdown")
	}
	body := stripANSI(m.projects.renderSummary(16))
	mustContain(t, body, personaIcon("staff")+" staff")
	mustContain(t, body, "[Esc] back")
}

func TestChartInlineDrillRendersInCompactSummary(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	m.refreshAll()
	m.projects.chartPersona = "staff"
	m.projects.chartDrill = true

	body := stripANSI(m.projects.renderSummary(6))
	mustContain(t, body, personaIcon("staff")+" staff")
	mustContain(t, body, "models")
	mustContain(t, body, "[Esc] back")
	mustNotContain(t, body, "⣀")
}

func TestChartInlineDrillKeepsExitHintAtTwelveRows(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(160, 44)
	m.projectScope = "ATM"
	m.focused = paneProjects
	now := core.Now()
	m.projects.summaryOK = true
	m.projects.summaryProject = &core.Project{Code: "ATM", Name: "Acme Task Manager"}
	m.projects.summaryEntries = []core.LogEntry{
		{At: now, Actor: "manager@claude:opus", Action: "project.reviewed"},
	}
	m.projects.chartPersona = "manager"
	m.projects.chartFocused = true
	m.projects.chartDrill = true

	body := stripANSI(m.projects.renderSummary(12))
	for _, want := range []string{
		personaIcon("manager") + " manager",
		"models",
		"agents",
		"actions",
		"[Esc] back",
	} {
		mustContain(t, body, want)
	}
}

func TestChartTightPulseKeepsRangeLegendAndPulse(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(120, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	m.refreshAll()

	for _, height := range []int{7, 8, 9, 10, 12} {
		body := stripANSI(m.projects.renderSummary(height))
		mustContain(t, body, "Range: One week")
		if height >= 8 {
			mustContain(t, body, "└")
		}
		if height <= 10 {
			mustNotContain(t, body, "╭")
		}
	}
}

func TestChartCtrlNavigationDoesNotScheduleFocusExpiry(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	m.refreshAll()

	_, cmd := m.Update(keyMsg("ctrl+right"))
	if cmd != nil {
		t.Fatal("ctrl chart navigation must not schedule a focus expiry timer")
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
	mustContain(t, body, "activity")
	mustNotContain(t, body, "activity \u00b7 1w")
	mustContain(t, body, "Range: One week")
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

func TestRenderSummaryUsesFullEnglishRangeLegendAtBottom(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(140, 44)
	m.projectScope = "ATM"
	m.projects.chartRange = 4
	m.refreshAll()

	lines := strings.Split(stripANSI(m.projects.renderSummary(14)), "\n")
	found := -1
	for i, line := range lines {
		if strings.Contains(line, "Range: One year") {
			found = i
			break
		}
	}
	if found == -1 {
		t.Fatalf("summary missing full English bottom legend:\n%s", strings.Join(lines, "\n"))
	}
	for i := 0; i < found; i++ {
		if strings.Contains(lines[i], "One year") {
			t.Fatalf("range legend appeared above chart bottom on line %d:\n%s", i, strings.Join(lines, "\n"))
		}
	}
}

func TestRenderRangeLegendUsesStrongStyle(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	tag := func(s lipgloss.Style, name string) lipgloss.Style {
		return s.Transform(func(v string) string { return "<" + name + ">" + v + "</" + name + ">" })
	}
	m.styles.HeaderLabel = tag(m.styles.HeaderLabel, "strong")
	m.styles.Muted = tag(m.styles.Muted, "muted")

	got := renderRangeLegend(chartRanges[0], 40, m.styles)
	mustContain(t, got, "<strong>Range: One week")
	mustNotContain(t, got, "<muted>Range: One week")
}

func TestRenderSummaryChartLinesKeepFixedWidthAcrossPersonaSwitch(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(180, 44)
	m.projectScope = "ATM"
	now := core.Now()
	m.projects.summaryOK = true
	m.projects.summaryProject = &core.Project{Code: "ATM", Name: "Acme Task Manager"}
	for i := 0; i < 14; i++ {
		m.projects.summaryEntries = append(m.projects.summaryEntries, core.LogEntry{
			At:     now.AddDate(0, 0, -(i % 7)),
			Actor:  "developer@codex:gpt-5",
			Action: "task.updated",
		})
	}
	m.projects.summaryEntries = append(m.projects.summaryEntries, core.LogEntry{
		At:     now,
		Actor:  "manager@claude:opus",
		Action: "project.reviewed",
	})

	all := strings.Split(stripANSI(m.projects.renderSummary(16)), "\n")
	m.projects.chartPersona = "manager"
	manager := strings.Split(stripANSI(m.projects.renderSummary(16)), "\n")
	for name, lines := range map[string][]string{"all": all, "manager": manager} {
		if len(lines) != 16 {
			t.Fatalf("%s chart line count = %d, want 16", name, len(lines))
		}
		wantW := lipgloss.Width(lines[0])
		for i, line := range lines {
			if got := lipgloss.Width(line); got != wantW {
				t.Fatalf("%s chart line %d width = %d, want %d\n--- chart ---\n%s", name, i, got, wantW, strings.Join(lines, "\n"))
			}
		}
	}
	if lipgloss.Width(all[0]) != lipgloss.Width(manager[0]) {
		t.Fatalf("chart width changed across persona switch: all=%d manager=%d", lipgloss.Width(all[0]), lipgloss.Width(manager[0]))
	}
}

func TestRenderSummaryPulseAxisOriginStableAcrossPersonaSwitch(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(180, 44)
	m.projectScope = "ATM"
	now := core.Now()
	m.projects.summaryOK = true
	m.projects.summaryProject = &core.Project{Code: "ATM", Name: "Acme Task Manager"}
	for i := 0; i < 12; i++ {
		m.projects.summaryEntries = append(m.projects.summaryEntries, core.LogEntry{
			At:     now,
			Actor:  "developer@codex:gpt-5",
			Action: "task.updated",
		})
	}
	m.projects.summaryEntries = append(m.projects.summaryEntries, core.LogEntry{
		At:     now,
		Actor:  "manager@claude:opus",
		Action: "project.reviewed",
	})

	all := stripANSI(m.projects.renderSummary(16))
	m.projects.chartPersona = "manager"
	manager := stripANSI(m.projects.renderSummary(16))
	if got, want := pulseAxisOrigin(manager), pulseAxisOrigin(all); got != want {
		t.Fatalf("pulse axis origin changed across persona switch: manager=%d all=%d\n--- manager ---\n%s\n--- all ---\n%s", got, want, manager, all)
	}
}

func pulseAxisOrigin(chart string) int {
	for _, line := range strings.Split(chart, "\n") {
		if col := strings.IndexRune(line, '└'); col >= 0 {
			return col
		}
	}
	return -1
}

// ensure lipgloss is used (silences unused-import in trim builds).
var _ = lipgloss.Width
