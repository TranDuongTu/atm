package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/scrum"
	"atm/internal/core"
	"atm/internal/dispatch"
	"atm/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeDispatcher struct {
	preview       string
	previewErr    error
	spawned       []dispatch.Spec
	spawnErr      error
	previewTarget func(string) (string, error)
}

func (f *fakeDispatcher) Preview() (string, error) { return f.preview, f.previewErr }
func (f *fakeDispatcher) PreviewTarget(target string) (string, error) {
	if f.previewTarget != nil {
		return f.previewTarget(target)
	}
	return f.preview, f.previewErr
}
func (f *fakeDispatcher) Spawn(s dispatch.Spec) error {
	f.spawned = append(f.spawned, s)
	return f.spawnErr
}

func testAgents() []agentOption {
	return []agentOption{
		{name: "claude", ready: true},
		{name: "codex", ready: false, hint: "missing bin: codex (https://developers.openai.com/codex)"},
	}
}

// dispatchKey delivers one key press to the model, mirroring the
// tea.KeyMsg construction used elsewhere in this package (see keyMsg).
func dispatchKey(m *Model, s string) {
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

// sizeDispatchModel gives the model a real size the way other overlay tests
// do (see capabilities_test.go / tasks_test.go): the renderOverlay box-width
// math assumes a nonzero m.width.
func sizeDispatchModel(m *Model) {
	m.SetSize(120, 40)
}

func TestDispatchManagerFromProjectsPane(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D on projects pane must open the dialog")
	}
	if m.dispatchDlg.persona() != "manager" {
		t.Fatalf("persona = %q, want manager", m.dispatchDlg.persona())
	}
	view := m.dispatchDlg.renderOverlay()
	for _, want := range []string{"Persona:", "manager", "claude", "tmux · new window"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
		}
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("enter on ready agent must spawn")
	}
	got := fd.spawned[0]
	wantArgv := []string{"atm", "--persona", "manager", "--project", "ATM", "--agent", "claude"}
	if strings.Join(got.Argv, " ") != strings.Join(wantArgv, " ") {
		t.Errorf("argv = %v, want %v", got.Argv, wantArgv)
	}
	if got.Title != "ATM · manager" {
		t.Errorf("title = %q, want ATM · manager", got.Title)
	}
	if m.dispatchDlg.active {
		t.Error("dialog must close after dispatch")
	}
}

// TestDispatchDOnTaskRowNoProjectRefusesInline drives the real D handler on a
// task row whose project resolves empty: the dialog must OPEN (developer
// preselected, no early "no project scope" toast) and Enter must refuse inline
// without spawning. A store task always carries a non-empty ProjectCode, so the
// empty state is arranged by clearing the scoped project and the selected
// row's project code after refresh; openDispatch then falls back to the empty
// m.projectScope.
func TestDispatchDOnTaskRowNoProjectRefusesInline(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	m.projectScope = ""
	m.tasks.rows[0].task.ProjectCode = ""
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D on a task row with no project scope must open the dialog, not refuse")
	}
	if m.toastMsg != "" {
		t.Fatalf("opening the dialog must not toast, got %q", m.toastMsg)
	}
	if m.dispatchDlg.persona() != "developer" {
		t.Fatalf("persona = %q, want developer", m.dispatchDlg.persona())
	}
	if m.dispatchDlg.project != "" {
		t.Fatalf("project = %q, want empty", m.dispatchDlg.project)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 0 {
		t.Fatal("enter with no project scope must not spawn")
	}
	if !strings.Contains(m.toastMsg, "requires a project scope") {
		t.Errorf("toast = %q, want project-scope error", m.toastMsg)
	}
	if !m.dispatchDlg.active {
		t.Error("dialog must stay open after refusal")
	}
}

func TestDispatchEscCloses(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D must open the dialog")
	}
	dispatchKey(m, "esc")
	if m.dispatchDlg.active {
		t.Error("esc must close the dialog")
	}
}

func TestDispatchTargetCycle(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	before := m.dispatchDlg.targetCursor
	dispatchKey(m, "t")
	want := (before + 1) % len(m.dispatchDlg.targets)
	if m.dispatchDlg.targetCursor != want {
		t.Fatalf("targetCursor = %d, want %d after t", m.dispatchDlg.targetCursor, want)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, m.dispatchDlg.targets[m.dispatchDlg.targetCursor]) {
		t.Errorf("overlay must show the cycled target:\n%s", view)
	}
}

func TestDispatchDeveloperFromTaskRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D on tasks pane must open the dialog")
	}
	if m.dispatchDlg.persona() != "developer" {
		t.Fatalf("persona = %q, want developer", m.dispatchDlg.persona())
	}
	if m.dispatchDlg.taskID != task.ID {
		t.Fatalf("task = %q, want %q", m.dispatchDlg.taskID, task.ID)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--persona developer") || !strings.Contains(argv, "--task "+task.ID) {
		t.Errorf("argv = %s", argv)
	}
	if want := task.ID; fd.spawned[0].Title != want {
		t.Errorf("title = %q, want %q", fd.spawned[0].Title, want)
	}
}

func TestDispatchUnreadyAgentRefused(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRight}) // move to codex (unready)
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 0 {
		t.Fatal("unready agent must not spawn")
	}
	if !strings.Contains(m.toastMsg, "not ready") {
		t.Errorf("toast = %q, want not-ready error", m.toastMsg)
	}
	if !m.dispatchDlg.active {
		t.Error("dialog must stay open after refusal")
	}
}

func TestDispatchNoTargetDisables(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)

	m.dispatcher = &fakeDispatcher{previewErr: errors.New(`no dispatch target: not inside herdr or tmux and no known terminal detected — set "terminal_cmd" in dispatch.json at the store root`)}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "no dispatch target") {
		t.Errorf("overlay must show detection error:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.dispatcher.(*fakeDispatcher).spawned) != 0 {
		t.Fatal("enter with no target must not spawn")
	}
}

func TestDispatchDeveloperWithRepoSpawnsIntoRepoPath(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	repoDir := t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", repoDir, "https://example.com/atm.git", testActor); err != nil {
		t.Fatal(err)
	}
	_, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.persona() != "developer" {
		t.Fatal("D on tasks pane must default to developer")
	}
	if len(m.dispatchDlg.repos) != 1 || m.dispatchDlg.repos[0].Path != repoDir {
		t.Fatalf("repos = %+v, want one main -> %s", m.dispatchDlg.repos, repoDir)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Repo:") || !strings.Contains(view, "main") {
		t.Errorf("overlay must show Repo: line with the repo name:\n%s", view)
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != repoDir {
		t.Errorf("Spec.Dir = %q, want repo path %q", fd.spawned[0].Dir, repoDir)
	}
}

func TestDispatchDeveloperNoRepoFallsBackToCwd(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	_, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dispatchKey(m, "D")
	if m.dispatchDlg.persona() != "developer" {
		t.Fatal("D on tasks pane must default to developer")
	}
	if len(m.dispatchDlg.repos) != 0 {
		t.Fatalf("repos = %+v, want empty", m.dispatchDlg.repos)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Repo:") || !strings.Contains(view, "(cwd)") {
		t.Errorf("overlay must show Repo: (cwd) when no repos recorded:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.dispatchDlg.repoCursor != 0 {
		t.Errorf("repoCursor = %d, want 0 (no-op with empty repos)", m.dispatchDlg.repoCursor)
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != cwd {
		t.Errorf("Spec.Dir = %q, want cwd %q", fd.spawned[0].Dir, cwd)
	}
}

func TestDispatchDeveloperRepoCyclePicker(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	d1, d2 := t.TempDir(), t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", d1, "", testActor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetProjectRepo("ATM", "docs", d2, "", testActor); err != nil {
		t.Fatal(err)
	}
	_, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if len(m.dispatchDlg.repos) != 2 || m.dispatchDlg.repoCursor != 0 {
		t.Fatalf("repos = %+v cursor = %d, want 2 repos cursor 0", m.dispatchDlg.repos, m.dispatchDlg.repoCursor)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.dispatchDlg.repoCursor != 1 {
		t.Fatalf("repoCursor = %d, want 1 after down", m.dispatchDlg.repoCursor)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "docs") {
		t.Errorf("overlay must show second repo name after down:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.dispatchDlg.repoCursor != 0 {
		t.Fatalf("repoCursor = %d, want 0 after up", m.dispatchDlg.repoCursor)
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != d1 {
		t.Errorf("Spec.Dir = %q, want first repo %q", fd.spawned[0].Dir, d1)
	}
}

// TestDispatchReadsRepoChannels pins the new source: with a repo channel
// wired on this machine, the dialog's Repo picker is built from
// RepoChannelTargets, not the legacy ProjectRepos list.
func TestDispatchReadsRepoChannels(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	dir := t.TempDir()
	if _, err := m.store.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetChannelWiring("ATM", "code", dir, "", testActor); err != nil {
		t.Fatal(err)
	}
	m.focused = paneProjects
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if len(m.dispatchDlg.repos) != 1 || m.dispatchDlg.repos[0].Name != "code" || m.dispatchDlg.repos[0].Path != dir {
		t.Fatalf("repos = %+v, want one code -> %s", m.dispatchDlg.repos, dir)
	}
}

// TestDispatchLegacyRepoFallback: a store that never ran migrate-repos (only
// legacy SetProjectRepo entries, no channels) must still populate d.repos —
// the deprecation window the brief describes.
func TestDispatchLegacyRepoFallback(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	dir := t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", dir, "", testActor); err != nil {
		t.Fatal(err)
	}
	m.focused = paneProjects
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if len(m.dispatchDlg.repos) != 1 || m.dispatchDlg.repos[0].Name != "main" || m.dispatchDlg.repos[0].Path != dir {
		t.Fatalf("repos = %+v, want legacy fallback one main -> %s", m.dispatchDlg.repos, dir)
	}
}

// repoReadSpy wraps the real store and records which of the two repo-read
// methods the dialog calls. It embeds core.Service (pattern shared with
// readCountingService in view_purity_test.go and countingService in
// tasks_refresh_calls_test.go) so it only needs to override the two methods
// under test.
type repoReadSpy struct {
	core.Service
	repoChannelTargetsCalls int
	projectChannelsCalls    int
}

func (s *repoReadSpy) RepoChannelTargets(code string) ([]core.RepoConfig, error) {
	s.repoChannelTargetsCalls++
	return s.Service.RepoChannelTargets(code)
}

func (s *repoReadSpy) ProjectChannels(code string) ([]core.ChannelView, error) {
	s.projectChannelsCalls++
	return s.Service.ProjectChannels(code)
}

// TestDispatchDoesNotProbe pins the performance property the RepoChannelTargets
// split exists for: opening the dialog on a repo-channel-backed project must
// call RepoChannelTargets and must NEVER call ProjectChannels, the read that
// probes each wired repo with `git status`/`rev-list`. This is a real
// regression guard — swapping the call in open() back to ProjectChannels
// fails it loudly, unlike a presence assertion on d.repos alone (which
// TestDispatchReadsRepoChannels already covers and cannot distinguish which
// method produced the result).
func TestDispatchDoesNotProbe(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	spy := &repoReadSpy{Service: s}
	m, err := NewModel(NewModelOpts{Service: spy, Actor: testActor, Registry: capability.NewRegistry(scrum.New())})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	dir := t.TempDir() // deliberately never `git init`-ed
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChannelWiring("ATM", "code", dir, "", testActor); err != nil {
		t.Fatal(err)
	}
	m.focused = paneProjects
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if len(m.dispatchDlg.repos) != 1 || m.dispatchDlg.repos[0].Name != "code" || m.dispatchDlg.repos[0].Path != dir {
		t.Fatalf("repos = %+v, want one code -> %s", m.dispatchDlg.repos, dir)
	}
	if spy.repoChannelTargetsCalls == 0 {
		t.Error("open() must call RepoChannelTargets")
	}
	if spy.projectChannelsCalls != 0 {
		t.Errorf("open() must never call ProjectChannels (probes git); called %d times", spy.projectChannelsCalls)
	}
}

// TestDispatchManagerShowsRepoWhenProjectPresent replaces the old
// "manager must not show a Repo line" guard: with the universal dialog the
// Repo picker appears for any persona whenever a project is present.
func TestDispatchManagerShowsRepoWhenProjectPresent(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	repoDir := t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", repoDir, "", testActor); err != nil {
		t.Fatal(err)
	}
	m.focused = paneProjects
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.persona() != "manager" {
		t.Fatalf("persona = %q want manager", m.dispatchDlg.persona())
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Repo:") || !strings.Contains(view, "main") {
		t.Errorf("manager dialog must show Repo line when a project is present:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != repoDir {
		t.Errorf("Spec.Dir = %q, want repo %q", fd.spawned[0].Dir, repoDir)
	}
}

func TestDispatchPersonaCycle(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	first := m.dispatchDlg.persona()
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if m.dispatchDlg.persona() == first {
		t.Fatal("p must change the persona")
	}
}

func TestDispatchProjectRequiredNoScopeRefuses(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("manager", "", "", "", dispatchScope{})
	if m.dispatchDlg.persona() != "manager" {
		t.Fatalf("persona = %q want manager", m.dispatchDlg.persona())
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "requires a project scope") {
		t.Errorf("overlay must show the no-scope warning:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 0 {
		t.Fatal("must not spawn without a project")
	}
	if !strings.Contains(m.toastMsg, "requires a project scope") {
		t.Errorf("toast = %q, want project-scope error", m.toastMsg)
	}
	if !m.dispatchDlg.active {
		t.Error("dialog must stay open")
	}
}

func TestDispatchUnknownDefaultFallsBackToConcierge(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("ghost", "", "", "", dispatchScope{})
	if m.dispatchDlg.persona() != "concierge" {
		t.Fatalf("persona = %q, want concierge fallback", m.dispatchDlg.persona())
	}
}

func TestDispatchTaskPersistsAcrossPersonaSwitch(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)
	fd := &fakeDispatcher{preview: "herdr"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.persona() != "developer" {
		t.Fatalf("persona = %q want developer", m.dispatchDlg.persona())
	}
	for i := 0; i < len(m.dispatchDlg.personas); i++ {
		m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	}
	if m.dispatchDlg.persona() != "developer" {
		t.Fatalf("persona = %q, want developer after full cycle", m.dispatchDlg.persona())
	}
	if m.dispatchDlg.taskID != task.ID {
		t.Fatalf("task = %q, want %q after persona cycle", m.dispatchDlg.taskID, task.ID)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--persona developer") || !strings.Contains(argv, "--task "+task.ID) {
		t.Errorf("argv = %s", argv)
	}
}

func TestDispatchConciergeOmitsProject(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("concierge", "", "", "", dispatchScope{})
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

func TestDispatchNoCapabilityByDefault(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	// testAgents (not the real catalog) so submit() always finds a ready
	// agent and spawns — on a machine with no ready agent the real catalog
	// refuses, spawned stays empty, and fd.spawned[0] below would panic.
	m.agentOptionsFn = testAgents
	m.openDispatch() // empty workspace → concierge, no project, no capability
	if v := m.dispatchDlg.renderOverlay(); strings.Contains(v, "Scope:") {
		t.Errorf("unscoped dialog must not render a Scope line:\n%s", v)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatalf("spawned %d, want 1", len(fd.spawned))
	}
	if argv := strings.Join(fd.spawned[0].Argv, " "); strings.Contains(argv, "--capability") {
		t.Errorf("unscoped argv must omit --capability: %s", argv)
	}
}

func TestDispatchAdminOpensTUI(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("admin", "ATM", "", "", dispatchScope{})
	if m.dispatchDlg.persona() != "admin" {
		t.Fatalf("persona = %q want admin", m.dispatchDlg.persona())
	}
	// admin is not gated on agent readiness: move to the unready codex and
	// dispatch anyway.
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("admin should spawn even with an unready agent")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--persona admin") {
		t.Errorf("admin argv must set --persona admin: %s", argv)
	}
	if strings.Contains(argv, "--project") || strings.Contains(argv, "--task") {
		t.Errorf("admin argv must omit --project/--task: %s", argv)
	}
	if fd.spawned[0].Title != "admin" {
		t.Errorf("title = %q, want just admin (no project prefix)", fd.spawned[0].Title)
	}
}

// TestDispatchTUILaunchPersonaIsDataDriven proves the dialog's TUI-launch
// behavior reads the persona's launch mode, not the "admin" name: a CUSTOM
// persona with launch: tui gets the same treatment admin does — project not
// required, bare title, bare argv, no agent-readiness gate — and the
// rendered overlay still shows it as ready.
func TestDispatchTUILaunchPersonaIsDataDriven(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	dir := filepath.Join(m.store.StorePath(), "personas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nname: console\ndescription: Console operator.\nlaunch: tui\n---\n# Persona: console\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "console.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("console", "ATM", "ATM-1", "Some task", dispatchScope{Capability: "scrum"})
	if m.dispatchDlg.persona() != "console" {
		t.Fatalf("persona = %q want console", m.dispatchDlg.persona())
	}
	if m.dispatchDlg.projectRequired() {
		t.Fatal("tui-launch persona must not require a project")
	}
	if got := m.dispatchDlg.title(); got != "console" {
		t.Fatalf("title = %q, want bare persona name", got)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "ready") {
		t.Fatalf("overlay must show ready for a tui-launch persona:\n%s", view)
	}
	// Not gated on agent readiness: move to the unready codex and dispatch.
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("tui-launch persona should spawn even with an unready agent")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--persona console") {
		t.Errorf("argv must set --persona console: %s", argv)
	}
	for _, gone := range []string{"--project", "--task", "--agent", "--capability"} {
		if strings.Contains(argv, gone) {
			t.Errorf("tui-launch argv must omit %s: %s", gone, argv)
		}
	}
}

// TestDispatchPromptPersonaStillCarriesFlags is the regression guard for the
// launch-mode split: a prompt-launch persona keeps the full argv.
func TestDispatchPromptPersonaStillCarriesFlags(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("manager", "ATM", "", "", dispatchScope{Capability: "scrum"})
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("manager should spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	for _, want := range []string{"--persona manager", "--project ATM", "--agent claude", "--capability scrum"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv missing %s: %s", want, argv)
		}
	}
}

func TestDispatchDOpensFromTasksPaneWithoutTask(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneTasks // no tasks seeded, no row selected
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D must open the dialog on the tasks pane with no task")
	}
	if m.dispatchDlg.persona() != "concierge" {
		t.Fatalf("persona = %q, want concierge fallback", m.dispatchDlg.persona())
	}
}

func TestDispatchDOpensFromEmptyWorkspace(t *testing.T) {
	m := newTestModel(t)
	m.focused = paneProjects // explicit: D works from any pane's default focus
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D must open the dialog from an empty workspace")
	}
	if m.dispatchDlg.persona() != "concierge" {
		t.Fatalf("persona = %q, want concierge fallback", m.dispatchDlg.persona())
	}
}

// Once the model is part of the selection, a dialog that hides it is lying by
// omission about what will start.
func TestDispatchAgentRowShowsConfiguredModel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}
	m.agentOptionsFn = testAgents
	if err := m.store.SetAgentModel("claude", "glm-5.2", "admin@tui:unset"); err != nil {
		t.Fatalf("SetAgentModel: %v", err)
	}

	d := &m.dispatchDlg
	d.m = m
	d.loadFor("developer", "ATM", "", "", dispatchScope{})

	var claude agentOption
	for _, a := range d.agents {
		if a.name == "claude" {
			claude = a
		}
	}
	if claude.model != "glm-5.2" {
		t.Fatalf("claude model = %q, want glm-5.2", claude.model)
	}
	for _, a := range d.agents {
		if a.name == "codex" && a.model != "" {
			t.Fatalf("codex model = %q; models are per selection key", a.model)
		}
	}
	d.cursor = 0
	if view := d.renderOverlay(); !strings.Contains(view, "glm-5.2") {
		t.Fatalf("overlay hides the model that will actually launch:\n%s", view)
	}
}
