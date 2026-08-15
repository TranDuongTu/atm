package tui

import (
	"testing"

	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEscFromSpotlightSpawnedFormReopensSpotlight(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Add project")
	row := m.spotlight.cursor
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.form == nil {
		t.Fatal("-> must open the project-create form")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.spotlight.open {
		t.Fatal("Esc from a spotlight-spawned form must reopen the spotlight")
	}
	if m.spotlight.cursor != row {
		t.Errorf("cursor must be restored to %d, got %d", row, m.spotlight.cursor)
	}
	if m.spotlightReturn != -1 {
		t.Errorf("the return must be cleared once used, got %d", m.spotlightReturn)
	}
}

// TestSuccessfulSubmitLandsOnTheWorkspace drives the project-create form (code,
// name fields) to submission. With only two fields, Enter on the last field
// (cursor == len(Fields)-1) validates and submits directly — it never passes
// through the buttons zone, so a single Enter is the real submit key, not the
// Tab-into-buttons-then-Enter shape the brief guessed.
func TestSuccessfulSubmitLandsOnTheWorkspace(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Add project")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})

	for _, r := range "ACME" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "Acme" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // last field: validates and submits directly

	if m.spotlight.open {
		t.Error("a successful submit must land on the workspace, not the spotlight")
	}
	if m.spotlightReturn != -1 {
		t.Errorf("submit must clear the return, got %d", m.spotlightReturn)
	}
}

// TestConfirmAcceptLandsOnTheWorkspace drives a spotlight-spawned confirm
// (Remove project) to acceptance: like a successful submit, it must clear
// spotlightReturn and land on the workspace rather than reopening the
// spotlight.
func TestConfirmAcceptLandsOnTheWorkspace(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.spotlight.openSpotlight()
	walkTo(t, m, "Remove project")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.confirm != confirmRemoveProject {
		t.Fatalf("-> must open the remove-project confirm, confirm=%v", m.confirm)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if m.spotlight.open {
		t.Error("a completed confirm must land on the workspace, not the spotlight")
	}
	if m.spotlightReturn != -1 {
		t.Errorf("confirm-accept must clear the return, got %d", m.spotlightReturn)
	}
}

func TestEscFromSpotlightSpawnedOverlayReopensSpotlight(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Channels")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if !m.channelsOv.open {
		t.Fatal("-> must open the channels overlay")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.spotlight.open {
		t.Error("Esc from a spotlight-spawned overlay must reopen the spotlight")
	}
}

// Regression: a kindDialog entry whose replay opens something INSIDE a pane
// (not one of workspaceIdle's eight overlay/form/confirm states) used to
// look inert. activate() records spotlightReturn for every kindDialog entry
// after its replay loop finishes, but that finish happens beneath the SAME
// outer handleKey call the activating -> keypress triggered (see handleKey's
// comment) — so the wrapper's reopen check fires immediately, in the same
// keystroke, and covers the freshly-opened in-pane view with the spotlight
// again. History overlay (task detail) and Toggle capability (projects
// detail) are both in-pane views, so they must be kindAction, not
// kindDialog. Activation must be driven through the real outer m.handleKey
// (not spotlightModel.handleKey directly) to exercise the wrapper.
func TestActivateHistoryOverlayLeavesItVisibleAndSpotlightClosed(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	tk := seedTask(t, m, "ATM", "task one")
	m.focused = paneTasks
	m.tasks.openDetail(tk.ID)

	m.spotlight.openSpotlight()
	walkTo(t, m, "History overlay")
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})

	if !m.tasks.historyOverlay.active {
		t.Fatal("activating History overlay must open the task's history")
	}
	if m.spotlight.open {
		t.Error("History overlay is an in-pane view; the spotlight must not reopen over it in the same keystroke")
	}
}

func TestActivateToggleCapabilityLeavesSpotlightClosed(t *testing.T) {
	// Two capabilities so the cursor cycle is observable (a one-capability
	// registry cycles back to 0).
	m := newTestModelWithCaps(t, workflow.New(), contextmap.New())
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")

	m.spotlight.openSpotlight()
	walkTo(t, m, "Toggle capability")
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})

	if m.projects.capCursor != 1 {
		t.Errorf("activating Toggle capability must cycle the cursor, capCursor=%d want 1", m.projects.capCursor)
	}
	if m.spotlight.open {
		t.Error("Toggle capability is an in-pane cursor cycle; the spotlight must not reopen over it")
	}
}

// Regression for finding #4: dispatchModel.submit's success path set
// d.active = false but never cleared spotlightReturn, so the wrapper's
// "workspace idle again" check fires the moment the dialog closes and
// reopens the spotlight over the dispatch toast instead of landing on the
// workspace (spec decision 5).
func TestSuccessfulDispatchFromSpotlightLandsOnTheWorkspace(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}
	m.agentOptionsFn = testAgents

	m.spotlight.openSpotlight()
	walkTo(t, m, "Dispatch a session")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if !m.dispatchDlg.active {
		t.Fatal("-> must open the dispatch dialog")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // preselected agent is ready: submits directly

	if m.dispatchDlg.active {
		t.Fatal("a successful dispatch must close the dialog")
	}
	if m.spotlight.open {
		t.Error("a successful dispatch must land on the workspace, not the spotlight")
	}
	if m.spotlightReturn != -1 {
		t.Errorf("a successful dispatch must clear the return, got %d", m.spotlightReturn)
	}
}

// Sibling of the dispatch regression above: capabilityModel.switchTo's
// success path set c.open = false but never cleared spotlightReturn.
func TestSuccessfulCapabilitySwitchFromSpotlightLandsOnTheWorkspace(t *testing.T) {
	m := newTestModelWithCaps(t, workflow.New(), contextmap.New())
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneTasks

	m.spotlight.openSpotlight()
	walkTo(t, m, "Capabilities")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if !m.capability.open {
		t.Fatal("-> must open the capabilities switcher")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // switch to the row under the cursor

	if m.capability.open {
		t.Fatal("a successful switch must close the capabilities switcher")
	}
	if m.spotlight.open {
		t.Error("a successful capability switch must land on the workspace, not the spotlight")
	}
	if m.spotlightReturn != -1 {
		t.Errorf("a successful capability switch must clear the return, got %d", m.spotlightReturn)
	}
}

// A key pressed with no pending return must never resurrect the spotlight.
func TestNoReturnWithoutSpotlightActivation(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.open {
		t.Error("a directly-opened overlay must not reopen the spotlight on Esc")
	}
}
