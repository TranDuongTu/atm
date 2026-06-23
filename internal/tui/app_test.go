package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

func setupTempStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "atm-home")
	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("init store: %v", err)
	}
	return root
}

func seedProject(t *testing.T, root, actor string) string {
	t.Helper()
	s, _ := store.Open(root)
	_, err := s.CreateProject("ATM", "Agent Tasks Management", "type", []store.Label{
		{Name: "type:impl"}, {Name: "type:bug"}, {Name: "kind:convention"},
	}, nil, actor)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return "ATM"
}

func TestNewModel_ExistingStore(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if !m.storeSet {
		t.Fatal("storeSet should be true when store exists")
	}
	if m.startup.promptInit {
		t.Fatal("promptInit should be false when store exists")
	}
	if m.startup.promptActor {
		t.Fatal("promptActor should be false when actor set")
	}
	if m.tab != tabDashboard {
		t.Fatalf("expected default tab %d, got %d", tabDashboard, m.tab)
	}
}

func TestNewModel_MissingStore_PromptsInit(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "nonexistent")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if !m.startup.promptInit {
		t.Fatal("promptInit should be true when store missing")
	}
	if m.storeSet {
		t.Fatal("storeSet should be false when store missing")
	}
	view := m.View()
	if !strings.Contains(view, "No store found") {
		t.Fatalf("expected 'No store found' in view, got:\n%s", view)
	}
}

func TestNewModel_MissingActor_PromptsActor(t *testing.T) {
	root := setupTempStore(t)
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: ""})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if !m.startup.promptActor {
		t.Fatal("promptActor should be true when actor unset")
	}
	view := m.View()
	if !strings.Contains(view, "Actor id required") {
		t.Fatalf("expected actor prompt, got:\n%s", view)
	}
}

func TestModel_View_RendersTabs(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	view := m.View()
	for _, label := range []string{"1 Dashboard", "2 Projects", "3 Tasks", "4 Actors", "5 Help"} {
		if !strings.Contains(view, label) {
			t.Errorf("expected tab label %q in view", label)
		}
	}
	if !strings.Contains(view, "actor: human:alice") {
		t.Errorf("expected actor in header")
	}
	if !strings.Contains(view, root) {
		t.Errorf("expected store path %s in header", root)
	}
}

func TestModel_TabSwitching(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)

	cases := []struct {
		key  string
		want int
	}{
		{"2", tabProjects},
		{"3", tabTasks},
		{"4", tabActors},
		{"5", tabHelp},
		{"1", tabDashboard},
	}
	for _, c := range cases {
		mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(c.key)})
		m = mod.(*Model)
		if m.tab != c.want {
			t.Errorf("after key %q: expected tab %d, got %d", c.key, c.want, m.tab)
		}
	}

	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mod.(*Model)
	if m.tab != tabProjects {
		t.Errorf("after tab: expected tab %d, got %d", tabProjects, m.tab)
	}
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = mod.(*Model)
	if m.tab != tabDashboard {
		t.Errorf("after shift+tab: expected tab %d, got %d", tabDashboard, m.tab)
	}
}

func TestModel_Quit(t *testing.T) {
	root := setupTempStore(t)
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	mod, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = mod.(*Model)
	if !m.quitting {
		t.Error("expected quitting=true after q")
	}
	if cmd == nil {
		t.Error("expected a quit command")
	}
}

func TestStartup_InitFlow(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "newstore")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if !m.startup.promptInit {
		t.Fatal("expected init prompt")
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = mod.(*Model)
	if m.startup.promptInit {
		t.Error("init prompt should clear after [I]")
	}
	if !m.storeSet {
		t.Error("storeSet should be true after init")
	}
	if _, err := os.Stat(filepath.Join(root, "projects")); err != nil {
		t.Errorf("projects dir not created: %v", err)
	}
}

func TestDashboardModel_Renders(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	m.dash.refresh()
	view := m.dash.view()
	if !strings.Contains(view, "REVIEW QUEUE") || !strings.Contains(view, "OPEN FOLLOWUPS") || !strings.Contains(view, "GUIDE STATUS") {
		t.Errorf("dashboard missing sections:\n%s", view)
	}
}

func TestProjectsModel_RendersList(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	m.projects.refresh()
	view := m.projects.view()
	if !strings.Contains(view, "PROJECTS") || !strings.Contains(view, "ATM") {
		t.Errorf("projects list missing content:\n%s", view)
	}
}

func TestTasksModel_RendersList(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	s, _ := store.Open(root)
	_, err = s.CreateTask("ATM", "Sample task", "", []string{"type:impl"}, "human:alice")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	m.tasks.refresh()
	view := m.tasks.view()
	if !strings.Contains(view, "TASKS") || !strings.Contains(view, "ATM-0001") {
		t.Errorf("tasks list missing content:\n%s", view)
	}
}

func TestActorsModel_RendersList(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	m.actors.refresh()
	view := m.actors.view()
	if !strings.Contains(view, "ACTORS") || !strings.Contains(view, "human:alice") {
		t.Errorf("actors list missing content:\n%s", view)
	}
}

func TestHelpModel_RendersParityTable(t *testing.T) {
	root := setupTempStore(t)
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	view := m.help.view()
	if !strings.Contains(view, "CLI / TUI parity") || !strings.Contains(view, "atm task create") {
		t.Errorf("help missing parity table:\n%s", view)
	}
}

func TestForm_Validation(t *testing.T) {
	f := NewForm("test", []formField{{Label: "name", Required: true}})
	f.SetWidth(60)
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Done {
		t.Error("expected Done=false on missing required field")
	}
	if f.Err == "" {
		t.Error("expected validation error on empty required field")
	}
	f2 := NewForm("test", []formField{{Label: "name", Required: true}})
	f2.SetWidth(60)
	f2, _ = f2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	f2, _ = f2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !f2.Done {
		t.Error("expected Done=true after providing required value and enter")
	}
}
