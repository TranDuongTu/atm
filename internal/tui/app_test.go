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
	if got := m.focusedPaneName(); got != "Projects" {
		t.Fatalf("expected default focus Projects, got %s", got)
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

func TestWorkspace_DefaultFocusAndNoActorsTab(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	if m.focusedPaneName() != "Projects" {
		t.Fatalf("expected default focus Projects, got %s", m.focusedPaneName())
	}
	view := m.View()
	for _, label := range []string{"[1] Projects", "[2] Tasks", "[3] Summary", "[4] Help"} {
		if !strings.Contains(view, label) {
			t.Errorf("expected workspace label %q in view", label)
		}
	}
	if strings.Contains(view, "[4] Actors") || strings.Contains(view, "4 Actors") || strings.Contains(view, "5 Help") {
		t.Fatalf("actors/tab-era navigation should be absent:\n%s", view)
	}
	if !strings.Contains(view, "actor: human:alice") {
		t.Errorf("expected actor in header")
	}
	if !strings.Contains(view, root) {
		t.Errorf("expected store path %s in header", root)
	}
}

func TestWorkspace_NumberKeysFocusPanes(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)

	cases := []struct {
		key  string
		want string
	}{
		{"2", "Tasks"},
		{"3", "Summary"},
		{"4", "Help"},
		{"1", "Projects"},
	}
	for _, c := range cases {
		mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(c.key)})
		m = mod.(*Model)
		if got := m.focusedPaneName(); got != c.want {
			t.Errorf("after key %q: expected focus %s, got %s", c.key, c.want, got)
		}
	}
}

func TestWorkspace_RendersPersistentLeftPanes(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(120, 32)
	view := m.View()
	for _, label := range []string{"[1] Projects", "[2] Tasks", "[3] Summary", "[4] Help"} {
		if strings.Count(view, label) < 2 {
			t.Fatalf("expected persistent left pane label %q in body and header:\n%s", label, view)
		}
	}
}

func TestWorkspace_MovementOnlyAffectsFocusedPane(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	s, _ := store.Open(root)
	if _, err := s.CreateTask("ATM", "ATM task 1", "", []string{"type:impl"}, "human:alice"); err != nil {
		t.Fatalf("create task 1: %v", err)
	}
	if _, err := s.CreateTask("ATM", "ATM task 2", "", []string{"type:bug"}, "human:alice"); err != nil {
		t.Fatalf("create task 2: %v", err)
	}
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	projectCursor := m.projects.cursor
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = mod.(*Model)
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = mod.(*Model)
	if m.projects.cursor != projectCursor {
		t.Fatalf("projects cursor changed while Tasks focused")
	}
	if m.tasks.cursor != 1 {
		t.Fatalf("expected tasks cursor 1, got %d", m.tasks.cursor)
	}
}

func TestWorkspace_ProjectScopeTogglesWithSpace(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mod.(*Model)
	if m.projectScope != "ATM" {
		t.Fatalf("expected scope ATM, got %q", m.projectScope)
	}
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mod.(*Model)
	if m.projectScope != "" {
		t.Fatalf("expected cleared scope, got %q", m.projectScope)
	}
}

func TestWorkspace_TasksDefaultAllProjectsAndFilterWhenScoped(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	s, _ := store.Open(root)
	_, err := s.CreateProject("DEMO", "Demo", "type", []store.Label{{Name: "type:impl"}}, nil, "human:alice")
	if err != nil {
		t.Fatalf("create DEMO: %v", err)
	}
	if _, err := s.CreateTask("ATM", "ATM task", "", []string{"type:impl"}, "human:alice"); err != nil {
		t.Fatalf("create ATM task: %v", err)
	}
	if _, err := s.CreateTask("DEMO", "Demo task", "", []string{"type:impl"}, "human:alice"); err != nil {
		t.Fatalf("create DEMO task: %v", err)
	}
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if len(m.tasks.tasks) != 2 {
		t.Fatalf("expected all tasks by default, got %d", len(m.tasks.tasks))
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mod.(*Model)
	if len(m.tasks.tasks) != 1 || m.tasks.tasks[0].ProjectCode != "ATM" {
		t.Fatalf("expected scoped ATM task, got %#v", m.tasks.tasks)
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
