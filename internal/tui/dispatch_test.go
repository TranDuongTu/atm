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
	"github.com/charmbracelet/lipgloss"
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
	seedDispatchProject(t, m)
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
	wantArgv := []string{"atm", "dispatch", "--checklist", "mgr-sweep", "--project", "ATM", "--agent", "claude"}
	if strings.Join(got.Argv, " ") != strings.Join(wantArgv, " ") {
		t.Errorf("argv = %v, want %v", got.Argv, wantArgv)
	}
	// The title names the ACTION and what it runs on — what a human scanning
	// their windows is actually looking for.
	if got.Title != "mgr-sweep: ATM" {
		t.Errorf("title = %q, want mgr-sweep: ATM", got.Title)
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
	seedDispatchProject(t, m)
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
	if m.dispatchDlg.project != "" {
		t.Fatalf("project = %q, want empty", m.dispatchDlg.project)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 0 {
		t.Fatal("enter with no project scope must not spawn")
	}
	// Checklists are project records, so with no project there is no action
	// to dispatch at all — that is what the refusal says.
	if !strings.Contains(m.toastMsg, "no actions available") {
		t.Errorf("toast = %q, want a no-actions error", m.toastMsg)
	}
	if !m.dispatchDlg.active {
		t.Error("dialog must stay open after refusal")
	}
}

func TestDispatchEscCloses(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
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
	seedDispatchProject(t, m)
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
	seedDispatchProject(t, m)
	seedTaskAction(t, m) // "code-it" sorts first and suits developer
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
	if m.dispatchDlg.taskID() != task.ID {
		t.Fatalf("task = %q, want %q", m.dispatchDlg.taskID(), task.ID)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--checklist code-it") || !strings.Contains(argv, "--task "+task.ID) {
		t.Errorf("argv = %s", argv)
	}
	if want := "code-it: " + task.ID; fd.spawned[0].Title != want {
		t.Errorf("title = %q, want %q", fd.spawned[0].Title, want)
	}
}

func TestDispatchUnreadyAgentRefused(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	dispatchKey(m, "a") // move to codex (unready)
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
	seedDispatchProject(t, m)
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
	seedDispatchProject(t, m)
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
	seedDispatchProject(t, m)
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
	seedDispatchProject(t, m)
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
	// Repo cycling lives on "r" — the arrow keys move the checklist cursor.
	dispatchKey(m, "r")
	if m.dispatchDlg.repoCursor != 1 {
		t.Fatalf("repoCursor = %d, want 1 after r", m.dispatchDlg.repoCursor)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "docs") {
		t.Errorf("overlay must show second repo name after r:\n%s", view)
	}
	dispatchKey(m, "r")
	if m.dispatchDlg.repoCursor != 0 {
		t.Fatalf("repoCursor = %d, want 0 after the cycle wraps", m.dispatchDlg.repoCursor)
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
	seedDispatchProject(t, m)
	m.projectScope = "ATM"
	dir := t.TempDir()
	if _, err := m.store.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetChannelWiring("ATM", "code", "", dir, "", testActor); err != nil {
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
	seedDispatchProject(t, m)
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
	seedDispatchProject(t, m)
	m.projectScope = "ATM"
	dir := t.TempDir() // deliberately never `git init`-ed
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChannelWiring("ATM", "code", "", dir, "", testActor); err != nil {
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
	seedDispatchProject(t, m)
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
	seedDispatchProject(t, m)
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
	seedDispatchProject(t, m)
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	// The action list needs a project to exist at all, so the dialog opens
	// scoped and then loses the scope — the state an unscoped launch is in.
	m.dispatchDlg.open("manager", "ATM", "", "", dispatchScope{})
	m.dispatchDlg.project = ""
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

func TestDispatchTaskPersistsAcrossPersonaOverride(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	seedTaskAction(t, m) // "code-it" sorts first and suits developer
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
	if m.dispatchDlg.taskID() != task.ID {
		t.Fatalf("task = %q, want %q after the persona override", m.dispatchDlg.taskID(), task.ID)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--checklist code-it") || !strings.Contains(argv, "--task "+task.ID) {
		t.Errorf("argv = %s", argv)
	}
}

// A project-optional persona still launches without a project: the record
// carries the document's own say (project_optional), and the dialog passes no
// --project when none is in scope. Replaces the concierge-omits-project test
// the pruned persona used to carry (plan §7).
func TestDispatchProjectOptionalPersonaOmitsProject(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	if _, err := m.store.SetPersonaRecord("ATM", core.Persona{Name: "rover", Description: "projectless guide", Prompt: "You guide the setup.", ProjectOptional: true, Origin: "user"}, testActor); err != nil {
		t.Fatalf("SetPersonaRecord: %v", err)
	}
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	// The dialog sees the project's own records (plan §7: personas are
	// project records), and a project-optional one dispatches with NO
	// --project even though the dialog knows the project.
	m.dispatchDlg.open("developer", "ATM", "", "", dispatchScope{})
	m.dispatchDlg.personaOverride = "rover" // what [p] sets
	if m.dispatchDlg.persona() != "rover" {
		t.Fatalf("persona = %q want rover", m.dispatchDlg.persona())
	}
	if m.dispatchDlg.projectRequired() {
		t.Fatal("a project_optional record must not require --project")
	}
	// Clear the scope the way an unscoped launch does: projectRequired() is
	// the gate, and the argv must follow it.
	m.dispatchDlg.project = ""
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("rover should spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if strings.Contains(argv, "--project") {
		t.Errorf("project-optional argv must omit --project: %s", argv)
	}
	if !strings.Contains(argv, "--persona rover") {
		t.Errorf("argv must set --persona rover: %s", argv)
	}
}

func TestDispatchNoCapabilityByDefault(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.projectScope = "ATM"
	m.SetSize(120, 40)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	// testAgents (not the real catalog) so submit() always finds a ready
	// agent and spawns — on a machine with no ready agent the real catalog
	// refuses, spawned stays empty, and fd.spawned[0] below would panic.
	m.agentOptionsFn = testAgents
	m.openDispatch() // no capability selected → no scope rides the argv
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
	seedDispatchProject(t, m)
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("developer", "ATM", "", "", dispatchScope{})
	m.dispatchDlg.personaOverride = "admin" // what [p] sets
	if m.dispatchDlg.persona() != "admin" {
		t.Fatalf("persona = %q want admin", m.dispatchDlg.persona())
	}
	// admin is not gated on agent readiness: move to the unready codex and
	// dispatch anyway.
	dispatchKey(m, "a")
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
// behavior reads the persona's launch mode, not the "admin" name: a PROJECT
// RECORD with launch: tui gets the same treatment admin does — project not
// required, bare title, bare argv, no agent-readiness gate — and the
// rendered overlay still shows it as ready.
func TestDispatchTUILaunchPersonaIsDataDriven(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	if _, err := m.store.SetPersonaRecord("ATM", core.Persona{Name: "console", Description: "Console operator.", Prompt: "# Persona: console\n\nBody.", Launch: "tui", Origin: "user"}, testActor); err != nil {
		t.Fatalf("SetPersonaRecord: %v", err)
	}
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("developer", "ATM", "ATM-1", "Some task", dispatchScope{Capability: "scrum"})
	m.dispatchDlg.personaOverride = "console" // what [p] sets
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
	// [a] cycles the agent now; the override survives it because the action
	// does not change.
	dispatchKey(m, "a")
	m.dispatchDlg.personaOverride = "console"
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
	seedDispatchProject(t, m)
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
	// The persona is DERIVED from the action, so the argv names the action
	// instead of restating it.
	for _, want := range []string{"--checklist mgr-sweep", "--project ATM", "--agent claude", "--capability scrum"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv missing %s: %s", want, argv)
		}
	}
}

func TestDispatchDOpensFromTasksPaneWithoutTask(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.projectScope = "ATM"
	m.focused = paneTasks // no tasks seeded, no row selected
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D must open the dialog on the tasks pane with no task")
	}
	// With no task row selected there is no default persona, so the cursor
	// sits on the first ACTION and the persona follows it.
	if got, want := m.dispatchDlg.persona(), m.dispatchDlg.action().Persona; got != want {
		t.Fatalf("persona = %q, want the first action's %q", got, want)
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
	// An empty workspace has no project, so there is nothing to dispatch —
	// the dialog still opens and says so rather than refusing to appear.
	if m.dispatchDlg.action() != nil {
		t.Fatalf("action = %v, want none without a project", m.dispatchDlg.action())
	}
}

// Once the model is part of the selection, a dialog that hides it is lying by
// omission about what will start.
func TestDispatchAgentRowShowsConfiguredModel(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
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

// seedDispatchChecklists creates the two-checklist fixture the multi-select
// tests drive: one suited to developer, one to manager (store order).
func seedDispatchChecklists(t *testing.T, m *Model) {
	t.Helper()
	if _, err := m.store.CreateChecklist("ATM", core.ChecklistRecord{
		Name: "dev-cycle", Purpose: "one task's flow",
		Steps: []core.ChecklistStep{{Text: "claim"}}, Suits: []string{"developer"}, Origin: "user",
	}, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateChecklist("ATM", core.ChecklistRecord{
		Name: "mgr-sweep", Purpose: "sweep the board",
		Steps: []core.ChecklistStep{{Text: "sweep"}}, Suits: []string{"manager"}, Origin: "user",
	}, testActor); err != nil {
		t.Fatal(err)
	}
}

// seedDispatchProject creates project ATM together with the actions the
// dialog selects between. v3 dispatches ACTIONS, so a project with no
// checklists has nothing to dispatch at all — every dialog test needs a
// roster, which is why this replaces the bare seedProject here.
func seedDispatchProject(t *testing.T, m *Model) {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	seedDispatchChecklists(t, m)
}

// seedTaskAction adds a task-target action so the task cycler has something
// to walk. No targets expression: every task of the project is eligible.
func seedTaskAction(t *testing.T, m *Model) {
	t.Helper()
	if _, err := m.store.CreateChecklist("ATM", core.ChecklistRecord{
		Name: "code-it", Purpose: "implement one task",
		Steps: []core.ChecklistStep{{Text: "build"}}, Suits: []string{"developer"},
		Target: core.ChecklistTargetTask, Origin: "user",
	}, testActor); err != nil {
		t.Fatal(err)
	}
}

// openDevDispatch opens the dialog for developer on project ATM the way the
// checklist tests need it, mirroring the direct-open pattern used above.
func openDevDispatch(m *Model) {
	m.dispatchDlg.m = m
	m.dispatchDlg.open("developer", "ATM", "", "", dispatchScope{})
}

// TestDispatchActionListRendersPersonaAndPurpose: v3's list IS the dialog's
// primary axis. Each row names the action, the persona its suits derive, and
// its purpose — everything the user needs to choose without opening anything
// else. It replaces the v2 checkbox multi-select entirely.
func TestDispatchActionListRendersPersonaAndPurpose(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(120, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	openDevDispatch(m)

	view := m.dispatchDlg.renderOverlay()
	for _, want := range []string{"Action:", "dev-cycle", "developer", "one task's flow", "mgr-sweep", "manager", "sweep the board"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
		}
	}
	// The v2 multi-select and the launch field are gone with the axes they
	// served: one dispatch runs one action, and vehicle is not user-facing.
	for _, gone := range []string{"[x] ", "[ ] ", "Checklists:", "Launch:"} {
		if strings.Contains(view, gone) {
			t.Errorf("v2 element %q survived:\n%s", gone, view)
		}
	}
}

// The persona is DERIVED and labelled as such, so a reader can tell a value
// that came from the checklist from one the user chose.
func TestDispatchPersonaIsDerivedFromTheAction(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(120, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	openDevDispatch(m)

	d := &m.dispatchDlg
	if d.persona() != "developer" || d.personaSource() != "from suits" {
		t.Fatalf("persona = %q (%s), want developer from suits", d.persona(), d.personaSource())
	}
	if !strings.Contains(d.renderOverlay(), "from suits") {
		t.Errorf("the overlay must say where the persona came from:\n%s", d.renderOverlay())
	}
	// Moving to the manager-suited action moves the persona with it.
	d.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if d.persona() != "manager" {
		t.Fatalf("persona = %q, want manager after moving to mgr-sweep", d.persona())
	}
}

// TestDispatchActionCursorWalksTheListAndResetsOverrides: the cursor is the
// selection now — there is nothing to toggle. Moving it re-derives
// everything, and DROPS the overrides, which were chosen against the action
// the user just left.
func TestDispatchActionCursorWalksTheListAndResetsOverrides(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(120, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	openDevDispatch(m)

	d := &m.dispatchDlg
	if d.actionCursor != 0 || d.action().Name != "dev-cycle" {
		t.Fatalf("cursor = %d on %v, want 0 on dev-cycle", d.actionCursor, d.action())
	}
	dispatchKey(m, "m") // an override against dev-cycle
	if d.modeOverride == "" {
		t.Fatal("m must set a mode override")
	}
	d.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if d.actionCursor != 1 || d.action().Name != "mgr-sweep" {
		t.Fatalf("cursor = %d on %v, want 1 on mgr-sweep", d.actionCursor, d.action())
	}
	if d.modeOverride != "" {
		t.Fatalf("modeOverride = %q; moving to another action must drop an override chosen against the previous one", d.modeOverride)
	}
	d.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if d.actionCursor != 1 {
		t.Fatalf("cursor = %d, want to stay on the last row", d.actionCursor)
	}
	d.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if d.actionCursor != 0 {
		t.Fatalf("cursor = %d, want back to 0", d.actionCursor)
	}
}

// TestDispatchGreyedActionStaysSelectable: unmet requires WARN, they never
// gate (spec decision 4). The row carries its reason and still dispatches.
func TestDispatchGreyedActionStaysSelectable(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateChecklist("ATM", core.ChecklistRecord{
		Name: "journaled", Purpose: "needs a channel",
		Steps:    []core.ChecklistStep{{Text: "post"}},
		Suits:    []string{"developer"},
		Requires: core.ChecklistRequires{Channels: []string{"journal"}},
		Origin:   "user",
	}, testActor); err != nil {
		t.Fatal(err)
	}
	m.SetSize(120, 40)
	fd := &fakeDispatcher{preview: "tmux"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	openDevDispatch(m)

	d := &m.dispatchDlg
	view := d.renderOverlay()
	if !strings.Contains(view, "requires channel journal") {
		t.Fatalf("greyed row must carry its reason:\n%s", view)
	}
	d.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("an action with unmet requires must still dispatch — warn never blocks")
	}
}

// TestDispatchModeCycleAnnotatesItsSource: the mode comes from the action,
// and [m] departs from it. The annotation is the whole point — a value that
// does not say whether it is the default is not readable.
func TestDispatchModeCycleAnnotatesItsSource(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateChecklist("ATM", core.ChecklistRecord{
		Name: "designing", Purpose: "brainstorm with the user",
		Steps: []core.ChecklistStep{{Text: "talk"}}, Suits: []string{"developer"},
		Mode: core.ChecklistModeInteractive, Origin: "user",
	}, testActor); err != nil {
		t.Fatal(err)
	}
	m.SetSize(120, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	openDevDispatch(m)

	d := &m.dispatchDlg
	if d.mode() != core.ChecklistModeInteractive || d.modeSource() != "checklist default" {
		t.Fatalf("mode = %q (%s), want interactive as the checklist default", d.mode(), d.modeSource())
	}
	view := d.renderOverlay()
	if !strings.Contains(view, "interactive") || !strings.Contains(view, "checklist default") {
		t.Fatalf("overlay must show the mode and where it came from:\n%s", view)
	}
	// resident is shown as coming, never cycled into: Compose refuses it, so
	// offering it would offer a dispatch that cannot start.
	if !strings.Contains(view, "resident (future)") {
		t.Errorf("overlay must show resident as future:\n%s", view)
	}
	dispatchKey(m, "m")
	if d.mode() != core.ChecklistModeEager || d.modeSource() != "override (default: interactive)" {
		t.Fatalf("mode = %q (%s), want eager as an override", d.mode(), d.modeSource())
	}
	dispatchKey(m, "m")
	if d.mode() != core.ChecklistModeInteractive || d.modeOverride != "" {
		t.Fatalf("cycling back to the action's own value must clear the override, got %q/%q", d.mode(), d.modeOverride)
	}
}

// TestDispatchArgvIsTheDispatchVerb: the spawned command is the v3 CLI form,
// so it is a reproducible record of exactly this dialog state — running it by
// hand does the same thing.
func TestDispatchArgvIsTheDispatchVerb(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(120, 40)
	fd := &fakeDispatcher{preview: "tmux"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	openDevDispatch(m)

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	want := "atm dispatch --checklist dev-cycle --project ATM --agent claude"
	if got := strings.Join(fd.spawned[0].Argv, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

// TestDispatchArgvCarriesOnlyRealOverrides: an argv restating the action's
// own persona and mode would record a decision the user never made, and
// would go stale the moment the checklist changed. Only departures ride.
func TestDispatchArgvCarriesOnlyRealOverrides(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(120, 40)
	fd := &fakeDispatcher{preview: "tmux"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	openDevDispatch(m)

	// No overrides: neither flag appears.
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if strings.Contains(argv, "--persona") || strings.Contains(argv, "--mode") {
		t.Fatalf("argv must not restate derived values: %s", argv)
	}

	// With both overridden, both ride.
	openDevDispatch(m)
	dispatchKey(m, "m")
	dispatchKey(m, "p")
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 2 {
		t.Fatal("must spawn the overridden dispatch")
	}
	argv = strings.Join(fd.spawned[1].Argv, " ")
	if !strings.Contains(argv, "--mode ") || !strings.Contains(argv, "--persona ") {
		t.Fatalf("argv must carry both overrides: %s", argv)
	}
}

// TestDispatchUnknownDefaultLandsOnTheFirstAction: the caller's default
// persona only PRESELECTS a row — it names no persona of its own in v3. One
// nobody suits leaves the cursor at the top of the list rather than failing,
// because a dialog that will not open teaches the user nothing.
func TestDispatchUnknownDefaultLandsOnTheFirstAction(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(100, 30)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("ghost", "ATM", "", "", dispatchScope{})
	if m.dispatchDlg.actionCursor != 0 {
		t.Fatalf("actionCursor = %d, want 0 (first action)", m.dispatchDlg.actionCursor)
	}
	if got := m.dispatchDlg.action().Name; got != "dev-cycle" {
		t.Fatalf("action = %q, want the first listed action", got)
	}
	// And the persona follows that action, not the unknown default.
	if got := m.dispatchDlg.persona(); got != "developer" {
		t.Fatalf("persona = %q, want the action's own developer", got)
	}
}

// TestDispatchProjectWithNoActionsRefuses: v3 dispatches ACTIONS. A project
// with no checklists has nothing to dispatch, and the dialog says so instead
// of spawning something undefined. The ad-hoc bare-persona form stays on the
// CLI, where it always was.
func TestDispatchProjectWithNoActionsRefuses(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(120, 40)
	fd := &fakeDispatcher{preview: "tmux"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	openDevDispatch(m)

	if !strings.Contains(m.dispatchDlg.renderOverlay(), "no checklists") {
		t.Fatalf("the dialog must say why there is nothing to dispatch:\n%s", m.dispatchDlg.renderOverlay())
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 0 {
		t.Fatal("enter with no actions must not spawn")
	}
	if !strings.Contains(m.toastMsg, "no actions available") {
		t.Errorf("toast = %q, want an explanation", m.toastMsg)
	}
}

// TestDispatchOverlayWidthHugsContent: on a wide terminal the box hugs the
// widest content line instead of spanning 60% of the screen; every rendered
// line is the same width (box integrity), and content wider than the cap
// still truncates (long repo paths cannot blow the dialog open).
func TestDispatchOverlayWidthHugsContent(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(200, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	openDevDispatch(m)

	lines := strings.Split(m.dispatchDlg.renderOverlay(), "\n")
	boxW := lipgloss.Width(lines[0])
	for i, l := range lines {
		if lipgloss.Width(l) != boxW {
			t.Fatalf("line %d width %d != box width %d:\n%s", i, lipgloss.Width(l), boxW, l)
		}
	}
	// The old formula gave 200*60/100 = 120; the content needs far less.
	if boxW >= 100 {
		t.Fatalf("box width = %d, want content-hugging (< 100)", boxW)
	}
	// The cap still binds: a repo path longer than the cap truncates rather
	// than widening the dialog past the old fixed width.
	long := filepath.Join(t.TempDir(), strings.Repeat("very-long-path-segment/", 8), "leaf")
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetProjectRepo("ATM", "deep", long, "", testActor); err != nil {
		t.Fatal(err)
	}
	openDevDispatch(m)
	lines = strings.Split(m.dispatchDlg.renderOverlay(), "\n")
	if w := lipgloss.Width(lines[0]); w > 120 {
		t.Fatalf("box width = %d, must stay capped at the old fixed width (120)", w)
	}
}

// TestDispatchTUIPersonaHidesSessionOnlyFields: a tui-vehicle dispatch opens
// a fresh TUI, which has no repo to start in. The v2 "Launch:" line is gone
// with the axis it showed — vehicle is launcher plumbing, and the mode field
// is what the user actually steers now (§3.7).
func TestDispatchTUIPersonaHidesSessionOnlyFields(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(120, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m
	m.dispatchDlg.open("developer", "ATM", "", "", dispatchScope{})
	m.dispatchDlg.personaOverride = "admin" // a tui-vehicle persona

	view := m.dispatchDlg.renderOverlay()
	if strings.Contains(view, "Repo:") {
		t.Fatalf("a tui dispatch has no repo to start in:\n%s", view)
	}
	if strings.Contains(view, "Launch:") {
		t.Fatalf("the vehicle is not a user-facing field in v3:\n%s", view)
	}
	if !strings.Contains(view, "Mode:") {
		t.Fatalf("the mode field is what replaced it:\n%s", view)
	}
}
