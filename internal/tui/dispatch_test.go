package tui

import (
	"errors"
	"strings"
	"testing"

	"atm/internal/dispatch"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeDispatcher struct {
	preview    string
	previewErr error
	spawned    []dispatch.Spec
	spawnErr   error
}

func (f *fakeDispatcher) Preview() (string, error) { return f.preview, f.previewErr }
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
	if m.dispatchDlg.kind != dispatchManager {
		t.Fatal("D on projects pane must open the manager dialog")
	}
	view := m.dispatchDlg.renderOverlay()
	for _, want := range []string{"Dispatch manager", "claude", "tmux · new window"} {
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
	if m.dispatchDlg.kind != dispatchNone {
		t.Error("dialog must close after dispatch")
	}
}

func TestDispatchDeveloperFromTaskRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	// Seed a task and refresh so the tasks pane has a row under the cursor.
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	// Default tasks focus is focusOff with an empty filter -> a flat row list.
	// Cursor sits on row 0 after refresh.
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.kind != dispatchDeveloper {
		t.Fatal("D on tasks pane must open the developer dialog")
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
	if want := "ATM · developer · " + task.ID; fd.spawned[0].Title != want {
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
	if m.dispatchDlg.kind == dispatchNone {
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
