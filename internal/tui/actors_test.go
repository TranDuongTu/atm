package tui

import (
	"strings"
	"testing"

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

func TestActorsOverlayOpensAndShowsPersona(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	if !m.actorsOverlay {
		t.Fatal("P should open actors overlay")
	}
	view := m.View()
	if !strings.Contains(view, "Activity by persona") {
		t.Fatalf("overlay title missing:\n%s", view)
	}
	if !strings.Contains(view, "staff") {
		t.Fatalf("persona row missing:\n%s", view)
	}
}

func TestActorsOverlayDrilldownAndEsc(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	update(t, m, "enter")
	view := m.View()
	if !strings.Contains(view, "persona: staff") && !strings.Contains(view, "agents") {
		t.Fatalf("detail not shown:\n%s", view)
	}
	update(t, m, "esc")
	if m.actors.detail {
		t.Fatal("Esc should leave detail")
	}
	update(t, m, "esc")
	if m.actorsOverlay {
		t.Fatal("Esc should close overlay")
	}
}

func TestActorsOverlayNoProjectToasts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.focused = paneProjects
	m.projectScope = ""
	update(t, m, "P")
	if m.actorsOverlay {
		t.Fatal("overlay must not open without a project")
	}
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "select a project") {
		t.Fatalf("expected a 'select a project' toast, got %q", m.toastMsg)
	}
}

func TestActorsOverlayDetailBarsAlignToWidth(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	m.actors.SetSize(96, 26)
	m.actors.refresh()
	m.actors.detail = true
	view := m.actors.renderDetail(m.actors.groups[0])
	ansiRe := strings.NewReplacer("\x1b[0m", "", "\x1b[1m", "")
	var barCols []int
	for _, line := range strings.Split(view, "\n") {
		stripped := ansiRe.Replace(line)
		if !strings.Contains(stripped, "█") && !strings.Contains(stripped, "░") {
			continue
		}
		if w := lipgloss.Width(line); w != 96 {
			t.Fatalf("detail bar line width = %d, want 96:\n%q", w, line)
		}
		idx := strings.IndexAny(stripped, "█░")
		if idx < 0 {
			continue
		}
		barCols = append(barCols, idx)
	}
	if len(barCols) < 2 {
		t.Fatalf("expected at least 2 bar rows, got %d\n%s", len(barCols), view)
	}
	first := barCols[0]
	for i, c := range barCols {
		if c != first {
			t.Fatalf("bar start column differs across rows: row 0 at col %d, row %d at col %d (all=%v)\n%s", first, i, c, barCols, view)
		}
	}
}

func TestProjectsStatusHintMentionsPandp(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(100, 30)
	m.focused = paneProjects
	m.projectScope = "ATM"
	m.refreshAll()
	hint := m.statusHint()
	if !strings.Contains(hint, "P") {
		t.Fatalf("status hint should mention P (expand): %q", hint)
	}
	if !strings.Contains(hint, "p") {
		t.Fatalf("status hint should mention p (add persona): %q", hint)
	}
}
