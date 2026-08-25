package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"atm/internal/capability"
	"atm/internal/capability/qa"
	"atm/internal/capability/scrum"
	"atm/internal/core"
	"atm/internal/store"
)

// newCapTestModel builds a Model over a two-FLOW registry (scrum, qa —
// registration order matters: it drives the flow order and therefore the
// resolution fallback). Both must be flows: pane [2] lists and resolves only
// flow capabilities. The shared newTestModel in app_test.go registers a
// non-flow, so we use a local fixture here rather than mutating the shared
// helper that ~9 other test files depend on.
func newCapTestModel(t *testing.T) *Model {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	reg := capability.NewRegistry(scrum.New(), qa.New())
	m, err := NewModel(NewModelOpts{Service: s, Actor: testActor, Registry: reg})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return m
}

// setupCapProject seeds a project with the scrum+qa vocabularies, one claimed
// task and one label no capability owns, and points the model at it. Mirrors
// the seeding helpers in labels_test.go.
func setupCapProject(t *testing.T, m *Model) {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:scrum:task")
	seedTask(t, m, "ATM", "stray", "ATM:needs-triage")
	m.refreshAll()
}

func TestCapabilityResolutionDefaultsToFirstEnabled(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	// newCapTestModel registers scrum then qa, so the first enabled flow is
	// "scrum".
	if m.capability.current != "scrum" {
		t.Fatalf("current = %q, want scrum (first enabled)", m.capability.current)
	}
}

func TestCapabilityResolutionUsesPersistedValue(t *testing.T) {
	m := newCapTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{Capability: "qa"}, m.actor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	m.projectScope = "ATM"
	m.refreshAll()
	if m.capability.current != "qa" {
		t.Fatalf("current = %q, want persisted qa", m.capability.current)
	}
}

func TestCapabilityResolutionFallsBackWhenPersistedInvalid(t *testing.T) {
	m := newCapTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{Capability: "ghost"}, m.actor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	m.projectScope = "ATM"
	m.refreshAll()
	if m.capability.current != "scrum" {
		t.Fatalf("current = %q, want scrum fallback", m.capability.current)
	}
	// Resolution must NOT write back: persisted value stays "ghost".
	cfg, _ := m.store.GetBoardsConfig("ATM")
	if cfg.Capability != "ghost" {
		t.Fatalf("persisted = %q; resolution must not write back", cfg.Capability)
	}
}

// With no flow capability enabled there is nothing for pane [2] to be scoped
// to: resolution yields "" rather than inventing a pseudo-capability.
func TestCapabilityResolutionZeroEnabledResolvesToNothing(t *testing.T) {
	m := newCapTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	for _, name := range m.reg.Names() {
		if err := m.store.DisableProjectCapability("ATM", name, m.actor); err != nil {
			t.Fatalf("disable %s: %v", name, err)
		}
	}
	m.projectScope = "ATM"
	m.refreshAll()
	if m.capability.current != "" {
		t.Fatalf("current = %q, want no current capability", m.capability.current)
	}
}

func TestSwitchToPersistsAndKeepsInMemoryCurrent(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.capability.switchTo("qa")
	if m.capability.current != "qa" {
		t.Fatalf("current = %q, want qa", m.capability.current)
	}
	cfg, err := m.store.GetBoardsConfig("ATM")
	if err != nil || cfg.Capability != "qa" {
		t.Fatalf("persisted = %+v (%v), want capability=qa", cfg, err)
	}
	// A later refresh keeps the in-session current even though other values
	// are also valid.
	m.refreshAll()
	if m.capability.current != "qa" {
		t.Fatalf("current after refresh = %q, want qa", m.capability.current)
	}
}

func TestCKeyOpensSwitcherOnlyInTasksPane(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.focused = paneProjects
	m.handleKey(keyMsg("C"))
	if m.capability.open {
		t.Fatalf("switcher opened from Projects pane; C must be a no-op there")
	}
	if m.spotlight.open {
		t.Fatalf("C opened the menu from the Projects pane")
	}
	m.focused = paneTasks
	m.handleKey(keyMsg("C"))
	if !m.capability.open {
		t.Fatalf("switcher did not open from Tasks pane")
	}
	if m.spotlight.open {
		t.Fatalf("menu opened alongside the switcher")
	}
}

func TestOverlayCursorOpensOnCurrent(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.capability.switchTo("qa")
	m.capability.openOverlay()
	e := m.capability.entries[m.capability.cursor]
	if e.name != "qa" {
		t.Fatalf("cursor on %q, want qa (the current)", e.name)
	}
}

func TestOverlayEnterSwitches(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.capability.openOverlay()
	// Move to the last listed flow and select it.
	m.capability.cursor = len(m.capability.entries) - 1
	want := m.capability.entries[m.capability.cursor].name
	m.capability.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.capability.open {
		t.Fatalf("overlay still open after Enter")
	}
	if m.capability.current != want {
		t.Fatalf("current = %q, want %q", m.capability.current, want)
	}
}

func TestOverlayEnterOnDisabledEnablesAndSwitches(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	if err := m.store.DisableProjectCapability("ATM", "qa", m.actor); err != nil {
		t.Fatalf("disable: %v", err)
	}
	m.refreshAll()
	m.capability.openOverlay()
	for i, e := range m.capability.entries {
		if e.name == "qa" {
			m.capability.cursor = i
		}
	}
	m.capability.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.capability.current != "qa" {
		t.Fatalf("current = %q, want qa", m.capability.current)
	}
	p, err := m.store.GetProject("ATM")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	enabled := false
	for _, n := range p.Capabilities {
		if n == "qa" {
			enabled = true
		}
	}
	if !enabled {
		t.Fatalf("qa not enabled after Enter; capabilities = %v", p.Capabilities)
	}
}

func TestOverlaySpaceDisablesCurrentAndFallsBack(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.capability.openOverlay()
	for i, e := range m.capability.entries {
		if e.name == "scrum" {
			m.capability.cursor = i
		}
	}
	m.capability.handleKey(keyMsg(" "))
	if !m.capability.open {
		t.Fatalf("space must not close the overlay")
	}
	if m.capability.current == "scrum" {
		t.Fatalf("current still scrum after disabling it; want fallback")
	}
}
