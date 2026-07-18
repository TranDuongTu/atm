package tui

import (
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"
	"atm/internal/core"
	"atm/internal/store"
)

// --- fixture + helpers (mirror app_test.go's newTestModel/update style) ---

// newCapabilityFixtureModel builds a Model over a two-capability registry
// (workflow, contextmap — registration order matters: it drives Names()
// order and therefore the capability cursor order) with:
//
//	project EXP — explicit capabilities: only "workflow" enabled
//	project LEG — legacy: no capability events recorded (Capabilities == nil)
func newCapabilityFixtureModel(t *testing.T) *Model {
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
	// Wide enough that the "capabilities: [x] workflow  [ ] contextmap"
	// line isn't clipped by the Projects pane's fitLine truncation (mirrors
	// the wide SetSize other detail-view tests use, e.g.
	// TestProjectDetailDashboardSections in app_test.go).
	m.SetSize(200, 50)
	if _, err := m.store.CreateProject("EXP", "Explicit Caps", testActor); err != nil {
		t.Fatalf("CreateProject EXP: %v", err)
	}
	if err := m.store.EnableProjectCapability("EXP", "workflow", testActor); err != nil {
		t.Fatalf("EnableProjectCapability EXP/workflow: %v", err)
	}
	if _, err := m.store.CreateProject("LEG", "Legacy", testActor); err != nil {
		t.Fatalf("CreateProject LEG: %v", err)
	}
	m.refreshAll()
	return m
}

// openProjectDetail opens the project detail view for code directly (bypasses
// list navigation, which is exercised elsewhere in projects_test.go/app_test.go).
func openProjectDetail(t *testing.T, m *Model, code string) {
	t.Helper()
	m.focused = paneProjects
	m.projects.openDetail(code)
}

// sendKeys feeds a sequence of key strings into the model via the package's
// existing keyMsg/Update plumbing (see app_test.go's update helper).
func sendKeys(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		update(t, m, k)
	}
}

// modelStore exposes the model's store for assertions (m.store is core.Service).
func modelStore(m *Model) core.Service {
	return m.store
}

// --- tests ---

func TestDetailViewRendersCapabilities(t *testing.T) {
	m := newCapabilityFixtureModel(t)
	openProjectDetail(t, m, "EXP")
	v := m.View()
	if !strings.Contains(v, "[x] workflow") || !strings.Contains(v, "[ ] contextmap") {
		t.Fatalf("detail view must render the enabled set, got:\n%s", v)
	}
	if strings.Contains(v, "(default)") {
		t.Fatalf("explicit project must not render the (default) marker, got:\n%s", v)
	}

	openProjectDetail(t, m, "LEG")
	v = m.View()
	if !strings.Contains(v, "[x] workflow") || !strings.Contains(v, "[x] contextmap") {
		t.Fatalf("legacy project must render all capabilities enabled, got:\n%s", v)
	}
	if !strings.Contains(v, "(default)") {
		t.Fatalf("legacy project must render the all-enabled default marker, got:\n%s", v)
	}
}

// TestToggleCapabilityFromDetail verifies " " (toggle) disables the
// capability under the detail view's cursor, which defaults to the first
// registered name (workflow) on open, on an explicit project.
func TestToggleCapabilityFromDetail(t *testing.T) {
	m := newCapabilityFixtureModel(t)
	openProjectDetail(t, m, "EXP")
	sendKeys(t, m, " ") // cursor defaults to workflow (index 0); toggle it off
	p, err := modelStore(m).GetProject("EXP")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Capabilities {
		if c == "workflow" {
			t.Fatalf("workflow should have been disabled by the toggle, got Capabilities=%v", p.Capabilities)
		}
	}
	v := m.View()
	if !strings.Contains(v, "[ ] workflow") {
		t.Fatalf("detail view should reflect the toggle immediately, got:\n%s", v)
	}
}

// TestToggleCapabilityFromDetailLegacyGoesExplicit verifies the legacy-nil
// semantics: toggling off one capability of a legacy (nil Capabilities)
// project first makes the choice explicit by enabling every OTHER registered
// name, so the stored set reads "all but this one" rather than losing every
// other capability the (default) view implied was on.
func TestToggleCapabilityFromDetailLegacyGoesExplicit(t *testing.T) {
	m := newCapabilityFixtureModel(t)
	openProjectDetail(t, m, "LEG")
	sendKeys(t, m, " ") // cursor defaults to workflow (index 0); toggle it off
	p, err := modelStore(m).GetProject("LEG")
	if err != nil {
		t.Fatal(err)
	}
	if p.Capabilities == nil {
		t.Fatalf("toggling a legacy project's capability should make Capabilities explicit (non-nil), got nil")
	}
	foundWorkflow := false
	foundContextmap := false
	for _, c := range p.Capabilities {
		if c == "workflow" {
			foundWorkflow = true
		}
		if c == "contextmap" {
			foundContextmap = true
		}
	}
	if foundWorkflow {
		t.Errorf("workflow should be disabled, got Capabilities=%v", p.Capabilities)
	}
	if !foundContextmap {
		t.Errorf("contextmap should have been made explicit (still enabled), got Capabilities=%v", p.Capabilities)
	}
}

// TestCapabilityCursorCyclesAndWraps verifies "c" advances the detail
// capability cursor and wraps around after the last registered name.
func TestCapabilityCursorCyclesAndWraps(t *testing.T) {
	m := newCapabilityFixtureModel(t)
	openProjectDetail(t, m, "EXP")
	if m.projects.capCursor != 0 {
		t.Fatalf("capCursor on open = %d, want 0", m.projects.capCursor)
	}
	sendKeys(t, m, "c")
	if m.projects.capCursor != 1 {
		t.Fatalf("capCursor after one c = %d, want 1", m.projects.capCursor)
	}
	sendKeys(t, m, "c")
	if m.projects.capCursor != 0 {
		t.Fatalf("capCursor after wrap = %d, want 0", m.projects.capCursor)
	}
}
