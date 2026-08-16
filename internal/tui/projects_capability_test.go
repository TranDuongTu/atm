package tui

import (
	"reflect"
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

// TestProjectDetailCapabilityToggleRemoved verifies the project detail view
// no longer toggles capabilities: capability management lives entirely in
// the C overlay now, so c (cursor) and space (toggle) are inert on the
// detail view. Uses EXP, whose Capabilities are explicit and non-empty
// (["workflow"]), so the assertion would genuinely fail if either key still
// mutated the stored set.
func TestProjectDetailCapabilityToggleRemoved(t *testing.T) {
	m := newCapabilityFixtureModel(t)
	openProjectDetail(t, m, "EXP")
	before, err := modelStore(m).GetProject("EXP")
	if err != nil {
		t.Fatal(err)
	}
	beforeCaps := append([]string(nil), before.Capabilities...)

	sendKeys(t, m, "c", " ")

	after, err := modelStore(m).GetProject("EXP")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeCaps, after.Capabilities) {
		t.Fatalf("c then space must not change capabilities, before=%v after=%v", beforeCaps, after.Capabilities)
	}
}
