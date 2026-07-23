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

// TestPersonaFocusActivatesAndShowsPersona verifies P toggles the inline
// persona-focus mode and the persona rows render with the cursor marker.
func TestPersonaFocusActivatesAndShowsPersona(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	if !m.projects.personaFocus {
		t.Fatal("P should activate persona focus")
	}
	view := m.View()
	if !strings.Contains(view, "activity by persona") {
		t.Fatalf("persona chart missing:\n%s", view)
	}
	if !strings.Contains(view, "staff") {
		t.Fatalf("persona row missing:\n%s", view)
	}
	// P again closes it.
	update(t, m, "P")
	if m.projects.personaFocus {
		t.Fatal("second P should close persona focus")
	}
}

// TestPersonaFocusNoProjectToasts verifies P without a project scope toasts.
func TestPersonaFocusNoProjectToasts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.focused = paneProjects
	m.projectScope = ""
	update(t, m, "P")
	if m.projects.personaFocus {
		t.Fatal("persona focus must not activate without a project")
	}
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "select a project") {
		t.Fatalf("expected a 'select a project' toast, got %q", m.toastMsg)
	}
}

// TestPersonaFocusCtrlArrowsMoveAndDrill verifies ctrl+down/up move the
// persona cursor and ctrl+right drills into the detail view.
func TestPersonaFocusCtrlArrowsMoveAndDrill(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	if len(m.projects.personaGroups) < 1 {
		t.Fatalf("need at least 1 persona group, got %d", len(m.projects.personaGroups))
	}
	// ctrl+down moves cursor (wrapping not expected; just don't panic).
	update(t, m, "ctrl+down")
	// ctrl+right drills into the hovered persona.
	update(t, m, "ctrl+right")
	if !m.projects.personaDrilled {
		t.Fatal("ctrl+right should drill into persona detail")
	}
	view := m.View()
	if !strings.Contains(view, "dispatch") {
		t.Fatalf("drilled detail should show dispatch action:\n%s", view)
	}
	// ctrl+left backs out of the detail.
	update(t, m, "ctrl+left")
	if m.projects.personaDrilled {
		t.Fatal("ctrl+left should leave persona detail")
	}
	if !m.projects.personaFocus {
		t.Fatal("persona focus should still be active after ctrl+left")
	}
	// Esc closes focus (not drilled).
	update(t, m, "esc")
	if m.projects.personaFocus {
		t.Fatal("Esc should close persona focus")
	}
}

// TestPersonaFocusEscFromDetailBacksOut verifies Esc from the drilled detail
// returns to the chart (not closing focus entirely).
func TestPersonaFocusEscFromDetailBacksOut(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	update(t, m, "ctrl+right")
	if !m.projects.personaDrilled {
		t.Fatal("ctrl+right should drill in")
	}
	update(t, m, "esc")
	if m.projects.personaDrilled {
		t.Fatal("Esc should leave detail")
	}
	if !m.projects.personaFocus {
		t.Fatal("Esc from detail should keep focus active")
	}
}

// TestPersonaFocusDetailBarsAlignToWidth verifies the drilled-in detail
// breakdown bars align across agents/models/actions rows.
func TestPersonaFocusDetailBarsAlignToWidth(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	if len(m.projects.personaGroups) == 0 {
		t.Fatal("need persona groups")
	}
	update(t, m, "ctrl+right")
	view := m.View()
	ansiRe := strings.NewReplacer("\x1b[0m", "", "\x1b[1m", "")
	var barCols []int
	for _, line := range strings.Split(view, "\n") {
		stripped := ansiRe.Replace(line)
		if !strings.Contains(stripped, "█") && !strings.Contains(stripped, "░") {
			continue
		}
		idx := strings.IndexAny(stripped, "█░")
		if idx < 0 {
			continue
		}
		barCols = append(barCols, idx)
	}
	if len(barCols) < 1 {
		t.Fatalf("expected at least 1 bar row in detail, got %d\n%s", len(barCols), view)
	}
	first := barCols[0]
	for i, c := range barCols {
		if c != first {
			t.Fatalf("bar start column differs: row 0 at col %d, row %d at col %d\n%s", first, i, c, view)
		}
	}
}

// TestPersonaFocusDispatchOpensDialog verifies that pressing D on a drilled-in
// persona opens the dispatch dialog pre-set to that persona, and the dialog
// takes over key routing (Esc closes the dialog, not the focus).
func TestPersonaFocusDispatchOpensDialog(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	update(t, m, "P")
	update(t, m, "ctrl+right") // drill into "staff" persona
	if !m.projects.personaDrilled {
		t.Fatal("ctrl+right should drill in")
	}
	view := m.View()
	if !strings.Contains(view, "dispatch staff") {
		t.Fatalf("detail should show dispatch action for hovered persona:\n%s", view)
	}
	// "staff" is an unknown persona → falls back to manager.
	update(t, m, "d")
	if m.dispatchDlg.kind != dispatchManager {
		t.Fatalf("D should open manager dispatch (fallback), got kind=%v", m.dispatchDlg.kind)
	}
	if m.dispatchDlg.project != "ATM" {
		t.Errorf("dispatch project = %q want ATM", m.dispatchDlg.project)
	}
	// Dispatch dialog owns key routing: Esc closes it, persona focus stays.
	update(t, m, "esc")
	if m.dispatchDlg.kind != dispatchNone {
		t.Fatal("Esc should close the dispatch dialog")
	}
	if !m.projects.personaFocus || !m.projects.personaDrilled {
		t.Fatal("persona focus/detail should still be active after dispatch Esc")
	}
}

func TestProjectsStatusHintMentionsP(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(100, 30)
	m.focused = paneProjects
	m.projectScope = "ATM"
	m.refreshAll()
	hint := m.statusHint()
	if !strings.Contains(hint, "P") {
		t.Fatalf("status hint should mention P (persona focus): %q", hint)
	}
	if strings.Contains(hint, "[p]") {
		t.Fatalf("status hint should no longer mention [p] (add persona removed): %q", hint)
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
