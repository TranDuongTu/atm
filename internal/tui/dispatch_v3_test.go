package tui

import (
	"strings"
	"testing"

	"atm/internal/compose"
	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
)

// TestDispatchAgentCycleRecomputesWarningsAndKeepsOverrides: warnings are
// AGENT-relative (§3.10), so cycling the agent refetches the list for that
// harness. The user's overrides survive it — they were chosen against this
// ACTION, and the action has not moved.
func TestDispatchAgentCycleRecomputesWarningsAndKeepsOverrides(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(120, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	var asked []string
	m.dispatchActionsFn = func(code, agentName string) ([]compose.ActionRow, error) {
		asked = append(asked, agentName)
		return []compose.ActionRow{{
			Name: "dev-cycle", Persona: "developer", Purpose: "one task's flow",
			Target: core.ChecklistTargetProject, Mode: core.ChecklistModeEager, Origin: "user",
			Warnings: []string{"checklist dev-cycle: never attested on " + agentName},
		}}, nil
	}
	openDevDispatch(m)
	d := &m.dispatchDlg
	dispatchKey(m, "m") // an override against this action
	if d.modeOverride == "" {
		t.Fatal("m must set a mode override")
	}
	if !strings.Contains(d.renderOverlay(), "never attested on claude") {
		t.Fatalf("warnings must be the current agent's:\n%s", d.renderOverlay())
	}

	dispatchKey(m, "a") // → codex
	if len(asked) < 2 || asked[len(asked)-1] != "codex" {
		t.Fatalf("action list asked for %v; cycling the agent must refetch for it", asked)
	}
	if !strings.Contains(d.renderOverlay(), "never attested on codex") {
		t.Fatalf("warnings must recompute for the new agent:\n%s", d.renderOverlay())
	}
	if d.modeOverride == "" {
		t.Fatal("cycling the AGENT must not discard an override chosen against the action")
	}
}

// TestDispatchTaskCyclerWalksEligibleTasksOnly: the cycler offers what the
// action's targets expression admits, and says how many — an empty or
// surprising list is a question about the expression, and the user cannot ask
// it without seeing the count.
func TestDispatchTaskCyclerWalksEligibleTasksOnly(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	seedTaskAction(t, m)
	m.SetSize(120, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	m.eligibleTasksFn = func(code string, row compose.ActionRow) ([]*core.Task, error) {
		return []*core.Task{
			{ID: "ATM-one", Title: "first"},
			{ID: "ATM-two", Title: "second"},
		}, nil
	}
	openDevDispatch(m)

	d := &m.dispatchDlg
	if d.action().Name != "code-it" || !d.needsTask() {
		t.Fatalf("expected the task-target action, got %+v", d.action())
	}
	if d.taskID() != "ATM-one" {
		t.Fatalf("task = %q, want the first eligible", d.taskID())
	}
	if !strings.Contains(d.renderOverlay(), "2 eligible") {
		t.Errorf("the count must be visible:\n%s", d.renderOverlay())
	}
	d.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if d.taskID() != "ATM-two" {
		t.Fatalf("task = %q, want the second after l", d.taskID())
	}
	d.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if d.taskID() != "ATM-one" {
		t.Fatalf("task = %q, want to wrap back", d.taskID())
	}
	// A project-target action has no task line at all.
	d.actionCursor = 1 // dev-cycle
	d.selectAction()
	if d.needsTask() || strings.Contains(d.renderOverlay(), "Task:") {
		t.Errorf("a project-target action must not render a task cycler:\n%s", d.renderOverlay())
	}
}

// TestDispatchPrefillIneligibleWarns: opening on a task the chosen action may
// not run on is allowed — the launcher warns and proceeds — so the dialog
// says so rather than silently swapping the task.
func TestDispatchPrefillIneligibleWarns(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	seedTaskAction(t, m)
	m.SetSize(120, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	m.eligibleTasksFn = func(code string, row compose.ActionRow) ([]*core.Task, error) {
		return []*core.Task{{ID: "ATM-eligible", Title: "allowed"}}, nil
	}
	m.dispatchDlg.m = m
	m.dispatchDlg.open("developer", "ATM", "ATM-elsewhere", "not allowed", dispatchScope{})

	d := &m.dispatchDlg
	if !d.prefillIneligible() {
		t.Fatal("a prefill outside the action's targets must be reported")
	}
	if !strings.Contains(d.renderOverlay(), "ATM-elsewhere is not eligible") {
		t.Fatalf("the overlay must name the ineligible prefill:\n%s", d.renderOverlay())
	}
	// It still dispatches — on an eligible task, since that is what the
	// cycler holds.
	if d.taskID() != "ATM-eligible" {
		t.Fatalf("task = %q, want the eligible one", d.taskID())
	}
}

// TestDispatchProfileCyclerScopesTheActionList: with several profiles applied
// the cycler narrows the list; with one it is static, because a cycler over a
// single value is a control that does nothing.
func TestDispatchProfileCyclerScopesTheActionList(t *testing.T) {
	m := newTestModel(t)
	seedDispatchProject(t, m)
	m.SetSize(120, 40)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	m.dispatchActionsFn = func(code, agentName string) ([]compose.ActionRow, error) {
		return []compose.ActionRow{
			{Name: "from-profile", Persona: "manager", Purpose: "shipped", Target: core.ChecklistTargetProject, Mode: core.ChecklistModeEager, Origin: "scrumban@1.0.0"},
			{Name: "home-grown", Persona: "developer", Purpose: "local", Target: core.ChecklistTargetProject, Mode: core.ChecklistModeEager, Origin: "user"},
		}, nil
	}
	openDevDispatch(m)

	d := &m.dispatchDlg
	if len(d.visible) != 2 {
		t.Fatalf("visible = %d, want both actions unscoped", len(d.visible))
	}
	dispatchKey(m, "P")
	if len(d.visible) != 1 || d.actions[d.visible[0]].Origin != "scrumban@1.0.0" {
		t.Fatalf("P must scope the list to one profile, got %v", d.visible)
	}
	if !strings.Contains(d.renderOverlay(), "scrumban@1.0.0") {
		t.Errorf("the cycler must show the selected profile:\n%s", d.renderOverlay())
	}
	dispatchKey(m, "P")
	if len(d.visible) != 1 || d.actions[d.visible[0]].Origin != "user" {
		t.Fatalf("P must advance to the next profile, got %v", d.visible)
	}
}
