package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"atm/internal/capability"
	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"
	"atm/internal/core"
	"atm/internal/store"
)

// newCapTestModel builds a Model over a two-capability registry
// (workflow, contextmap — registration order matters: it drives Names()
// order and therefore the resolution fallback). The brief's tests assume
// both capabilities are registered; the shared newTestModel in app_test.go
// only registers workflow, so we use a local fixture here rather than
// mutating the shared helper that ~9 other test files depend on.
func newCapTestModel(t *testing.T) *Model {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	reg := capability.NewRegistry(workflow.New(), contextmap.New())
	m, err := NewModel(NewModelOpts{Service: s, Actor: testActor, Registry: reg})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return m
}

// setupCapProject seeds a project with workflow+contextmap vocabularies and
// one unmanaged label, and points the model at it. Mirrors the seeding
// helpers in labels_test.go.
func setupCapProject(t *testing.T, m *Model) {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "stray", "ATM:needs-triage")
	m.refreshAll()
}

func TestCapabilityResolutionDefaultsToFirstEnabled(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	// newCapTestModel registers workflow then contextmap, so the first
	// enabled name is "workflow".
	if m.capability.current != "workflow" {
		t.Fatalf("current = %q, want workflow (first enabled)", m.capability.current)
	}
}

func TestCapabilityResolutionUsesPersistedValue(t *testing.T) {
	m := newCapTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{Capability: "contextmap"}, m.actor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	m.projectScope = "ATM"
	m.refreshAll()
	if m.capability.current != "contextmap" {
		t.Fatalf("current = %q, want persisted contextmap", m.capability.current)
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
	if m.capability.current != "workflow" {
		t.Fatalf("current = %q, want workflow fallback", m.capability.current)
	}
	// Resolution must NOT write back: persisted value stays "ghost".
	cfg, _ := m.store.GetBoardsConfig("ATM")
	if cfg.Capability != "ghost" {
		t.Fatalf("persisted = %q; resolution must not write back", cfg.Capability)
	}
}

func TestCapabilityResolutionZeroEnabledIsUnmanaged(t *testing.T) {
	m := newCapTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	for _, name := range m.reg.Names() {
		if err := m.store.DisableProjectCapability("ATM", name, m.actor); err != nil {
			t.Fatalf("disable %s: %v", name, err)
		}
	}
	m.projectScope = "ATM"
	m.refreshAll()
	if m.capability.current != unmanagedCapability {
		t.Fatalf("current = %q, want unmanaged", m.capability.current)
	}
}

func TestSwitchToPersistsAndKeepsInMemoryCurrent(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.capability.switchTo("contextmap")
	if m.capability.current != "contextmap" {
		t.Fatalf("current = %q, want contextmap", m.capability.current)
	}
	cfg, err := m.store.GetBoardsConfig("ATM")
	if err != nil || cfg.Capability != "contextmap" {
		t.Fatalf("persisted = %+v (%v), want capability=contextmap", cfg, err)
	}
	// A later refresh keeps the in-session current even though other values
	// are also valid.
	m.refreshAll()
	if m.capability.current != "contextmap" {
		t.Fatalf("current after refresh = %q, want contextmap", m.capability.current)
	}
}

func TestCapabilityTaskCountOwnershipBased(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	// "open one" carries ATM:status:open (workflow-owned); "stray" carries
	// only ATM:needs-triage (unmanaged).
	if got := m.capabilityTaskCount("workflow"); got != 1 {
		t.Errorf("workflow count = %d, want 1", got)
	}
	if got := m.capabilityTaskCount(unmanagedCapability); got != 1 {
		t.Errorf("unmanaged count = %d, want 1", got)
	}
	if got := m.capabilityTaskCount("contextmap"); got != 0 {
		t.Errorf("contextmap count = %d, want 0", got)
	}
}

func TestCKeyOpensSwitcherOnlyInTasksPane(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.focused = paneProjects
	m.handleKey(keyMsg("C"))
	if m.capability.open {
		t.Fatalf("switcher opened from Projects pane; C must keep conventions there")
	}
	if m.helpOverlay != helpConventions {
		t.Fatalf("helpOverlay = %v, want conventions", m.helpOverlay)
	}
	m.closeHelp()
	m.focused = paneTasks
	m.handleKey(keyMsg("C"))
	if !m.capability.open {
		t.Fatalf("switcher did not open from Tasks pane")
	}
	if m.helpOverlay != helpNone {
		t.Fatalf("conventions overlay opened alongside the switcher")
	}
}

func TestOverlayCursorOpensOnCurrent(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.capability.switchTo("contextmap")
	m.capability.openOverlay()
	e := m.capability.entries[m.capability.cursor]
	if e.name != "contextmap" {
		t.Fatalf("cursor on %q, want contextmap (the current)", e.name)
	}
}

func TestOverlayEnterSwitches(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.capability.openOverlay()
	// Move to the unmanaged entry (always last) and select it.
	m.capability.cursor = len(m.capability.entries) - 1
	m.capability.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.capability.open {
		t.Fatalf("overlay still open after Enter")
	}
	if !m.capability.unmanagedCurrent() {
		t.Fatalf("current = %q, want unmanaged", m.capability.current)
	}
}

func TestOverlayEnterOnDisabledEnablesAndSwitches(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	if err := m.store.DisableProjectCapability("ATM", "contextmap", m.actor); err != nil {
		t.Fatalf("disable: %v", err)
	}
	m.refreshAll()
	m.capability.openOverlay()
	for i, e := range m.capability.entries {
		if e.name == "contextmap" {
			m.capability.cursor = i
		}
	}
	m.capability.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.capability.current != "contextmap" {
		t.Fatalf("current = %q, want contextmap", m.capability.current)
	}
	p, err := m.store.GetProject("ATM")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	enabled := false
	for _, n := range p.Capabilities {
		if n == "contextmap" {
			enabled = true
		}
	}
	if !enabled {
		t.Fatalf("contextmap not enabled after Enter; capabilities = %v", p.Capabilities)
	}
}

func TestOverlaySpaceDisablesCurrentAndFallsBack(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	m.capability.openOverlay()
	for i, e := range m.capability.entries {
		if e.name == "workflow" {
			m.capability.cursor = i
		}
	}
	m.capability.handleKey(keyMsg(" "))
	if !m.capability.open {
		t.Fatalf("space must not close the overlay")
	}
	if m.capability.current == "workflow" {
		t.Fatalf("current still workflow after disabling it; want fallback")
	}
}

func TestStatusHintLeadsWithCapabilities(t *testing.T) {
	m := newCapTestModel(t)
	setupCapProject(t, m)
	hint := m.tasks.statusHint()
	if !strings.HasPrefix(hint, "[C]apabilities") {
		t.Fatalf("hint = %q, want [C]apabilities first", hint)
	}
}

// TestProjectSelectWithPersistedUnmanagedEstablishesIdle verifies the
// user-observable invariant after project-select into a project whose
// persisted capability is `unmanaged`: the Tasks pane ends in
// focusUmbrellaIdle with no rows listed (capability-view spec §4 — no
// unfiltered list renders at idle). The accompanying reordering in
// projects.go (boards.selectDefault before tasks.refresh) additionally
// avoids a wasted intermediate ListTasks call before the idle focus is
// set; this test guards the final-state guarantee that the reorder
// preserves.
func TestProjectSelectWithPersistedUnmanagedEstablishesIdle(t *testing.T) {
	m := newCapTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Persist unmanaged as the project's capability. Disable all registered
	// capabilities so resolution lands on unmanaged after project-select.
	for _, name := range m.reg.Names() {
		if err := m.store.DisableProjectCapability("ATM", name, m.actor); err != nil {
			t.Fatalf("disable %s: %v", name, err)
		}
	}
	if err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{Capability: unmanagedCapability}, m.actor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	// Seed a stray task carrying only an unmanaged label; if an unfiltered
	// sweep ran at project-select, this task would land in m.tasks.rows.
	seedTask(t, m, "ATM", "stray", "ATM:needs-triage")
	// Project-select handler requires a project to be selected in pane [1].
	m.projects.refresh()
	if len(m.projects.list) == 0 {
		t.Fatalf("no projects in pane [1] list")
	}
	m.projects.cursor = 0
	// Drive the "s" handler. boards.reset + setFocus(focusOff) precede the
	// refresh sequence; the fix reorders boards.selectDefault before
	// tasks.refresh so enterUnmanagedBase's setFocus(focusUmbrellaIdle)
	// establishes the idle focus before tasks.refresh can sweep.
	m.projects.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if m.capability.current != unmanagedCapability {
		t.Fatalf("current = %q, want unmanaged after project-select", m.capability.current)
	}
	if m.tasks.focus.mode != focusUmbrellaIdle {
		t.Fatalf("focus.mode = %v, want focusUmbrellaIdle (no unfiltered sweep may run at idle)", m.tasks.focus.mode)
	}
	if len(m.tasks.rows) != 0 {
		t.Fatalf("tasks rows = %d, want 0 (unmanaged idle must not list tasks): %v",
			len(m.tasks.rows), m.tasks.rows)
	}
}
