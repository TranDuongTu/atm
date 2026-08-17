package tui

import (
	"strings"
	"testing"

	"atm/internal/setup"

	tea "github.com/charmbracelet/bubbletea"
)

// newTestModelEmptyStore builds a Model against a fresh temp-dir store with
// no projects, then explicitly applies the empty-store landing rule — the
// same call Run makes right after NewModel in a real launch (see run.go).
// The rule is NOT folded into NewModel itself: NewModel is also what every
// other internal/tui test uses to build a bare fixture for one pane, and
// none of those seed a project just to see their own pane render normally;
// auto-opening the wizard inside NewModel would hijack every one of them.
func newTestModelEmptyStore(t *testing.T) *Model {
	t.Helper()
	m := newTestModelWithActor(t, testActor)
	m.applyLandingRule()
	return m
}

// An empty store has nothing to show and nothing to press — the wizard IS
// the TUI there — once.
func TestEmptyStoreLandsOnSetup(t *testing.T) {
	m := newTestModelEmptyStore(t)
	if !m.setup.active {
		t.Fatal("a store with no projects lands on the setup view")
	}
}

func TestStoreWithProjectsDoesNotAutoOpen(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme") // has at least one project
	m.applyLandingRule()
	if m.setup.active {
		t.Fatal("the wizard never auto-opens once projects exist")
	}
}

func TestNudgeAppearsOnlyWhileSomethingIsUnready(t *testing.T) {
	m := newTestModel(t)
	m.setup.model = setup.Model{Agents: []setup.AgentRow{
		{Agent: "claude", Binary: setup.FactPresent, Plugin: setup.FactAbsent},
	}}
	if !strings.Contains(m.renderStatusLine(), "setup") {
		t.Fatal("an unready agent must raise the nudge")
	}
	m.setup.model = setup.Model{Agents: []setup.AgentRow{
		{Agent: "claude", Binary: setup.FactPresent, Plugin: setup.FactPresent},
	}}
	if strings.Contains(m.renderStatusLine(), "setup") {
		t.Fatal("nothing unready means no nudge at all")
	}
}

// The wizard hands off; it never creates projects.
func TestFooterPointsAtProjectCreationOnceAnAgentIsReady(t *testing.T) {
	m := newTestModelEmptyStore(t)
	m.setup.model = setup.Model{Agents: []setup.AgentRow{
		{Agent: "claude", Binary: setup.FactPresent, Plugin: setup.FactPresent},
	}}
	if !strings.Contains(m.setup.render(100, 30), "first project") {
		t.Fatal("once an agent is ready the footer points at creating a project")
	}
}

// TestOpenSetupViaKeyReachesHandleKey exercises the real key-routing path
// end to end: dispatchKey's "W" case opens the wizard (rather than a test
// calling m.setup.open() directly), and a follow-up key reaches
// setupModel.handleKey through the same m.handleKey entry point a real
// keypress would use. Every other setup test drives setupModel.handleKey
// directly; this one proves the wiring between the two actually holds.
func TestOpenSetupViaKeyReachesHandleKey(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme") // NewModel itself never auto-opens (see newTestModelEmptyStore)
	m.projectScope = "ATM"           // gives Tab a second section to land on
	if m.setup.active {
		t.Fatal("precondition: wizard must start closed")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	if !m.setup.active {
		t.Fatal("the W key must open the setup wizard through the real dispatch path")
	}
	if m.setup.section != setupSectionAgents {
		t.Fatalf("opens on agents, got %v", m.setup.section)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.setup.section != setupSectionChannels {
		t.Fatalf("Tab must reach setupModel.handleKey through m.handleKey, not just m.setup.handleKey directly; section = %v", m.setup.section)
	}
}
