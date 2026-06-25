package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	for _, label := range []string{"[1] - Projects", "[2] - Tasks", "[3] - Summary", "[4] - Help"} {
		if !strings.Contains(view, label) {
			t.Errorf("expected pane border title %q in view", label)
		}
	}
	if strings.Contains(view, "[1] Projects") || strings.Contains(view, "[2] Tasks") || strings.Contains(view, "[4] Actors") || strings.Contains(view, "4 Actors") || strings.Contains(view, "5 Help") {
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
	for _, label := range []string{"[1] - Projects", "[2] - Tasks", "[3] - Summary", "[4] - Help"} {
		if strings.Count(view, label) != 1 {
			t.Fatalf("expected one bordered pane title %q in body only:\n%s", label, view)
		}
	}
	if count := strings.Count(view, "╭"); count < 8 {
		t.Fatalf("expected stacked pane borders, got %d top-left corners:\n%s", count, view)
	}
}

func TestWorkspace_LeftColumnUsesWiderMinimum(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	if m.leftWidth != 30 {
		t.Fatalf("expected 30-column left pane at 100 columns, got %d", m.leftWidth)
	}
	m.SetSize(80, 30)
	if m.leftWidth < 28 {
		t.Fatalf("expected wider minimum left pane, got %d", m.leftWidth)
	}
}

func TestWorkspace_RightColumnAllPanesAreFullHeightBordered(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	s, _ := store.Open(root)
	if _, err := s.CreateTask("ATM", "Sample task", "", []string{"type:impl"}, "human:alice"); err != nil {
		t.Fatalf("create task: %v", err)
	}
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 34)
	for _, tc := range []struct {
		key   string
		title string
	}{
		{"2", "Task Details"},
		{"3", "Summary"},
		{"4", "Help"},
	} {
		mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
		m = mod.(*Model)
		right := m.renderRightColumn()
		if !strings.Contains(right, tc.title) {
			t.Fatalf("right column for key %s missing bordered title %q:\n%s", tc.key, tc.title, right)
		}
		if len(strings.Split(right, "\n")) != m.contentHeight {
			t.Fatalf("right column for key %s should span content height %d, got %d\n%s", tc.key, m.contentHeight, len(strings.Split(right, "\n")), right)
		}
		if !strings.Contains(right, "╭") || !strings.Contains(right, "╯") {
			t.Fatalf("right column for key %s should be bordered:\n%s", tc.key, right)
		}
	}
}

func TestWorkspace_HeaderOmitsTopNavigationLabels(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	view := m.renderHeader()
	for _, label := range []string{"[1]", "[2]", "[3]", "[4]", "[1] Projects", "[2] Tasks", "[3] Summary", "[4] Help"} {
		if strings.Contains(view, label) {
			t.Fatalf("header should not contain top nav label %q:\n%s", label, view)
		}
	}
}

func TestWorkspace_ProjectsRightColumnUsesStackedPanesWithKeyMenus(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(120, 34)
	view := m.projects.rightView()
	for _, label := range []string{"Project Details", "Labels", "Repos", "Guide", "Advanced"} {
		if !strings.Contains(view, label) {
			t.Fatalf("expected right stacked pane %q:\n%s", label, view)
		}
	}
	for _, menu := range []string{"keys: [N] name [T] type", "keys: [L] add [l] remove", "keys: [R] add [r] remove"} {
		if !strings.Contains(view, menu) {
			t.Fatalf("expected per-pane key menu %q:\n%s", menu, view)
		}
	}
}

func TestWorkspace_ProjectsRightColumnPanesSpanFullHeight(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(120, 60)
	right := m.projects.rightView()
	lines := strings.Split(right, "\n")
	if len(lines) != m.contentHeight {
		t.Fatalf("projects right stack should span content height %d, got %d:\n%s", m.contentHeight, len(lines), right)
	}
	for _, label := range []string{"Project Details", "Labels", "Repos", "Guide", "Advanced"} {
		if !strings.Contains(right, label) {
			t.Fatalf("expected project right pane %q:\n%s", label, right)
		}
	}
}

func TestWorkspace_ViewDoesNotRenderContentUnderFooter(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 24)
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) > m.height {
		t.Fatalf("view should not exceed terminal height %d, got %d", m.height, len(lines))
	}
	footerStart := m.height - bottomPaneHeight(m.width)
	for i, line := range lines[:footerStart] {
		if strings.Contains(line, "actor: human:alice | store:") {
			t.Fatalf("footer rendered inside content at line %d:\n%s", i, view)
		}
	}
}

func TestWorkspace_LeftPaneStackExtendsToFooter(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 40)
	for _, key := range []string{"1", "2"} {
		mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = mod.(*Model)
		view := m.View()
		lines := strings.Split(view, "\n")
		lastContentLine := topPaneHeight(m.width) + m.contentHeight - 1
		if lastContentLine >= len(lines) {
			t.Fatalf("content line %d outside view with %d lines", lastContentLine, len(lines))
		}
		line := lines[lastContentLine]
		if !strings.Contains(line, "╯") {
			t.Fatalf("left pane stack should end immediately above footer after key %s; line %d was %q\n%s", key, lastContentLine, line, view)
		}
	}
}

func TestTitledBoxDoesNotCorruptANSISequences(t *testing.T) {
	got := titledBox(activePaneStyle, 36, "[1] - Projects", "body\n")
	if strings.Contains(got, "\x1b [") || strings.Contains(got, "\x1b ") {
		t.Fatalf("title insertion corrupted ANSI escape sequence:\n%q", got)
	}
	if !strings.Contains(got, "[1] - Projects") {
		t.Fatalf("missing title:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if lipgloss.Width(line) != 36 {
			t.Fatalf("line width = %d, want 36 for %q\nfull:\n%s", lipgloss.Width(line), line, got)
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

func TestWorkspace_SummaryDefaultsAllAndScopesToProject(t *testing.T) {
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
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = mod.(*Model)
	view := m.View()
	if !strings.Contains(view, "scope: all") || !strings.Contains(view, "2 task(s)") {
		t.Fatalf("expected all-project summary:\n%s", view)
	}
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = mod.(*Model)
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mod.(*Model)
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = mod.(*Model)
	view = m.View()
	if !strings.Contains(view, "scope: ATM") || !strings.Contains(view, "1 task(s)") {
		t.Fatalf("expected scoped summary:\n%s", view)
	}
}

func TestWorkspace_ProjectsRightColumnSectionsNavigateWithLeftRight(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	view := m.projects.rightView()
	for _, label := range []string{"Project Details", "Labels", "Repos", "Guide", "Advanced"} {
		if !strings.Contains(view, label) {
			t.Fatalf("expected project section %q:\n%s", label, view)
		}
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = mod.(*Model)
	if m.projects.paneCursor != 1 {
		t.Fatalf("expected paneCursor 1, got %d", m.projects.paneCursor)
	}
}

func TestWorkspace_TaskRightColumnIncludesActorClaimsContext(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	s, _ := store.Open(root)
	if _, err := s.CreateTask("ATM", "Sample task", "", []string{"type:impl"}, "human:alice"); err != nil {
		t.Fatalf("create task: %v", err)
	}
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = mod.(*Model)
	view := m.View()
	if !strings.Contains(view, "Actor / claims") || !strings.Contains(view, "current actor: human:alice") {
		t.Fatalf("task right column missing actor context:\n%s", view)
	}
}

func TestWorkspace_TabEraKeysDoNotExposeActors(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m = mod.(*Model)
	if got := m.focusedPaneName(); got != "Projects" {
		t.Fatalf("key 5 should not change focus, got %s", got)
	}
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mod.(*Model)
	if got := m.focusedPaneName(); got != "Projects" {
		t.Fatalf("tab should not cycle panes, got %s", got)
	}
	view := m.View()
	if strings.Contains(view, "[4] Actors") || strings.Contains(view, "4 Actors") {
		t.Fatalf("actors primary navigation should be absent:\n%s", view)
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

func TestSummaryModel_Renders(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	m.SetSize(100, 30)
	m.dash.refresh()
	view := m.dash.view()
	if !strings.Contains(view, "SUMMARY scope: all") || !strings.Contains(view, "REVIEW QUEUE") || !strings.Contains(view, "OPEN FOLLOWUPS") || !strings.Contains(view, "GUIDE HEALTH") {
		t.Errorf("summary missing sections:\n%s", view)
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
