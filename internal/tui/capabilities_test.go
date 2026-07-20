package tui

import (
	"testing"

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
