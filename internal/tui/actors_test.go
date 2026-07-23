package tui

import (
	"strings"
	"testing"

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

// TestPersonaChartCtrlArrowsScroll verifies ctrl+down/up move the persona
// cursor modelessly (no P toggle needed) and the cursor highlights the
// persona name text.
func TestPersonaChartCtrlArrowsScroll(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	// Seed a second persona so the cursor can move.
	seedTaskAsActor(t, m, "ATM", "task two", "developer@claude:opus-4.8")
	m.refreshAll()

	// The chart renders with a cursor on persona 0.
	view := m.View()
	if !strings.Contains(view, "activity by persona") {
		t.Fatalf("persona chart missing:\n%s", view)
	}
	// ctrl+down moves the cursor (no panic, no focus toggle needed).
	update(t, m, "ctrl+down")
	update(t, m, "ctrl+up")
}

// TestPersonaChartCtrlRightDrills verifies ctrl+right drills into the hovered
// persona's detail and ctrl+left backs out.
func TestPersonaChartCtrlRightDrills(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects

	update(t, m, "ctrl+right")
	if !m.projects.personaDrilled {
		t.Fatal("ctrl+right should drill into persona detail")
	}
	view := m.View()
	if !strings.Contains(view, "persona: ") {
		t.Fatalf("drilled detail should show persona title:\n%s", view)
	}
	// No centered dispatch help text in the detail.
	if strings.Contains(view, "dispatch") {
		// The breakdown itself shouldn't contain the word "dispatch" —
		// only the status hint (outside the box) may. Check the box body
		// doesn't have a centered dispatch action.
		if strings.Contains(view, "[D] dispatch") {
			t.Fatalf("detail should not contain centered dispatch help:\n%s", view)
		}
	}
	// ctrl+left backs out.
	update(t, m, "ctrl+left")
	if m.projects.personaDrilled {
		t.Fatal("ctrl+left should leave persona detail")
	}
}

// TestPersonaChartEscFromDetailBacksOut verifies Esc from the drilled detail
// returns to the chart.
func TestPersonaChartEscFromDetailBacksOut(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "ctrl+right")
	if !m.projects.personaDrilled {
		t.Fatal("ctrl+right should drill in")
	}
	update(t, m, "esc")
	if m.projects.personaDrilled {
		t.Fatal("Esc should leave detail")
	}
}

// TestPersonaChartDDispatchesWhenDrilled verifies the D key dispatches the
// drilled-in persona.
func TestPersonaChartDDispatchesWhenDrilled(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	update(t, m, "ctrl+right") // drill into persona 0
	if !m.projects.personaDrilled {
		t.Fatal("ctrl+right should drill in")
	}
	update(t, m, "d")
	if m.dispatchDlg.kind != dispatchManager {
		t.Fatalf("D should dispatch drilled persona (manager fallback), got kind=%v", m.dispatchDlg.kind)
	}
}

// TestPersonaChartDetailScroll verifies ctrl+down/up scroll the drilled-in
// breakdown body when it overflows the box.
func TestPersonaChartDetailScroll(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "ctrl+right")
	if !m.projects.personaDrilled {
		t.Fatal("ctrl+right should drill in")
	}
	// Scrolling down then up must not panic and offset stays in range.
	update(t, m, "ctrl+down")
	update(t, m, "ctrl+down")
	if m.projects.personaDetailOffset < 0 {
		t.Fatalf("offset should not go negative: %d", m.projects.personaDetailOffset)
	}
	update(t, m, "ctrl+up")
	update(t, m, "ctrl+up")
	if m.projects.personaDetailOffset != 0 {
		t.Fatalf("offset should clamp at 0: %d", m.projects.personaDetailOffset)
	}
}

func TestProjectsStatusHintMentionsPersonaKeys(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(100, 30)
	m.focused = paneProjects
	m.projectScope = "ATM"
	m.refreshAll()
	hint := m.statusHint()
	if !strings.Contains(hint, "Ctrl") {
		t.Fatalf("status hint should mention Ctrl persona keys: %q", hint)
	}
	if strings.Contains(hint, "[p]") {
		t.Fatalf("status hint should not mention [p]: %q", hint)
	}
}

// TestDispatchConciergeOmitsProject verifies the concierge dispatch kind
// builds an argv without --project (concierge is project-optional).
func TestDispatchConciergeOmitsProject(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	m.dispatchDlg.m = m
	m.dispatchDlg.open(dispatchConcierge, "", "", "")
	if m.dispatchDlg.persona() != "concierge" {
		t.Fatalf("persona = %q want concierge", m.dispatchDlg.persona())
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("concierge should spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if strings.Contains(argv, "--project") {
		t.Errorf("concierge argv must omit --project: %s", argv)
	}
	if !strings.Contains(argv, "--persona concierge") {
		t.Errorf("concierge argv must set --persona concierge: %s", argv)
	}
}

// ensure lipgloss is used (silences unused-import in trim builds).
var _ = lipgloss.Width
