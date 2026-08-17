package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/setup"

	tea "github.com/charmbracelet/bubbletea"
)

// errProbeUnavailable stands in for a harness that cannot answer a probe —
// not installed, or its verb failed.
var errProbeUnavailable = errors.New("probe unavailable")

// writeAgentsConfigOutOfBand writes agents.json directly, the way a `atm
// agents select` run in another terminal would — behind the TUI's back, with
// no in-memory model updated. It is the only honest way to test that an
// action re-reads before it writes.
func writeAgentsConfigOutOfBand(t *testing.T, m *Model, body string) {
	t.Helper()
	path := filepath.Join(m.store.StorePath(), "agents.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write agents.json: %v", err)
	}
}

// readAgentsConfig reads agents.json back from disk (not from any cached
// model), so an assertion sees exactly what the next process would.
func readAgentsConfig(t *testing.T, m *Model) core.AgentsConfig {
	t.Helper()
	cfg, err := m.store.GetAgentsConfig()
	if err != nil {
		t.Fatalf("read agents.json: %v", err)
	}
	return cfg
}

func TestSetupViewReplacesWorkspaceNotOverlaysIt(t *testing.T) {
	m := newTestModel(t)
	m.setup.open()
	out := m.View()
	if strings.Contains(out, "[1] Projects") {
		t.Fatal("the setup view replaces the workspace; it is not an overlay")
	}
	if !strings.Contains(out, "AGENTS") {
		t.Fatal("setup view not rendered")
	}
}

func TestSetupTabCyclesSectionsAndEscPeels(t *testing.T) {
	m := newTestModel(t)
	m.projectScope = "ATM"
	m.setup.open()
	if m.setup.section != setupSectionAgents {
		t.Fatalf("opens on agents, got %v", m.setup.section)
	}
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.setup.section != setupSectionChannels {
		t.Fatalf("tab -> channels, got %v", m.setup.section)
	}
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // drill
	if !m.setup.drilled {
		t.Fatal("enter should drill")
	}
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.setup.drilled {
		t.Fatal("esc peels the drill first")
	}
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.setup.active {
		t.Fatal("a second esc closes")
	}
}

// With no project selected the wizard is honestly global: the project
// sections are ABSENT, not empty-with-a-hint.
func TestSetupWithoutProjectHasNoProjectSections(t *testing.T) {
	m := newTestModel(t)
	m.projectScope = ""
	m.setup.open()
	if m.setup.model.Project != nil {
		t.Fatal("no project selected => no project sections")
	}
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.setup.section != setupSectionAgents {
		t.Fatal("tab must not reach absent sections")
	}
}

// A disabled checklist capability is not an empty personas section: there is
// nothing to account for until it is on.
func TestSetupChecklistCapabilityDisabled(t *testing.T) {
	m := newTestModel(t)
	m.projectScope = "ATM" // capability NOT enabled
	m.setup.open()
	if m.setup.model.Project.ChecklistCapEnabled {
		t.Fatal("capability is off")
	}
	if len(m.setup.model.Project.Personas) != 0 {
		t.Fatal("no starter accounting until the capability is enabled")
	}
	if !strings.Contains(m.setup.render(100, 30), "enable") {
		t.Fatal("offer to enable it instead of rendering an empty section")
	}
}

// A CLI in another terminal can change agents.json underneath us. Every
// action re-reads first, so a fix never writes over a stale read.
func TestSetupActionRereadsBeforeWriting(t *testing.T) {
	m := newTestModel(t)
	m.setup.open()
	writeAgentsConfigOutOfBand(t, m, `{"selected":"codex"}`)
	m.setup.setModelFor("claude", "opus-5")
	cfg := readAgentsConfig(t, m)
	if cfg.Selected != "codex" {
		t.Fatalf("selected = %q; the out-of-band selection was clobbered", cfg.Selected)
	}
	if cfg.Models["claude"] != "opus-5" {
		t.Fatalf("models = %+v", cfg.Models)
	}
}

// The brief's disabled-capability test scopes a project that is not in the
// store, so it proves the unreadable-project path. This one proves the rule
// itself against a real project whose enabled set omits checklist.
func TestSetupChecklistCapabilityDisabledOnRealProject(t *testing.T) {
	m := newTestModel(t)
	if _, err := m.store.CreateProject("ATM", "Acme", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// Recording ANY capability makes the enabled set explicit, so checklist
	// is no longer covered by the legacy "nil means all built-ins" rule.
	if err := m.store.EnableProjectCapability("ATM", "workflow", testActor); err != nil {
		t.Fatalf("EnableProjectCapability: %v", err)
	}
	m.projectScope = "ATM"
	m.setup.open()
	if m.setup.model.Project == nil || m.setup.model.Project.ChecklistCapEnabled {
		t.Fatalf("checklist is not in the project's enabled set: %+v", m.setup.model.Project)
	}
	if len(m.setup.model.Project.Personas) != 0 {
		t.Fatal("no starter accounting until the capability is enabled")
	}
	if !strings.Contains(m.setup.render(100, 30), "enable") {
		t.Fatal("offer to enable it instead of rendering an empty section")
	}
}

// With the capability on, the personas section accounts for the starters ATM
// ships — the section exists precisely because there is something to say.
func TestSetupChecklistCapabilityEnabledAccountsStarters(t *testing.T) {
	m := newTestModel(t)
	if _, err := m.store.CreateProject("ATM", "Acme", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := m.store.EnableProjectCapability("ATM", "checklist", testActor); err != nil {
		t.Fatalf("EnableProjectCapability: %v", err)
	}
	m.projectScope = "ATM"
	m.setup.open()
	if !m.setup.model.Project.ChecklistCapEnabled {
		t.Fatal("checklist is in the project's enabled set")
	}
	if len(m.setup.model.Project.Personas) == 0 {
		t.Fatal("an enabled capability accounts for every persona's starters")
	}
}

// Tier 2 lands as a message, not a blocking call: the cmd open() returns is
// what pays the 1.6-3s per agent, and Update folds its answers in.
func TestSetupProbeCmdLandsAsyncFacts(t *testing.T) {
	m := newTestModel(t)
	m.setup.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			return []byte("harness 9.9.9 (build 7)\n"), nil
		}
		// `mcp list` fails here: a probe that cannot answer must land as
		// unknown, never as a missing server list.
		return nil, errProbeUnavailable
	}
	cmd := m.setup.open()
	if cmd == nil {
		t.Fatal("open must return the async tier's command")
	}
	if !m.setup.probing {
		t.Fatal("probing until the message lands")
	}
	for _, row := range m.setup.model.Agents {
		if row.Version != "" {
			t.Fatalf("%s version %q before the probe landed", row.Agent, row.Version)
		}
	}

	msg := cmd()
	if _, ok := msg.(setupProbedMsg); !ok {
		t.Fatalf("cmd returned %T, want setupProbedMsg", msg)
	}
	m.Update(msg)

	if m.setup.probing {
		t.Fatal("the probe landed")
	}
	if len(m.setup.model.Agents) == 0 {
		t.Fatal("no agent rows")
	}
	for _, row := range m.setup.model.Agents {
		if row.Version != "9.9.9" {
			t.Fatalf("%s version = %q, want 9.9.9", row.Agent, row.Version)
		}
		if row.MCPState != setup.FactUnknown {
			t.Fatalf("%s mcp state = %v; an unanswerable probe is unknown", row.Agent, row.MCPState)
		}
	}
}

// An action rebuilds tier 1 from the store, which must not throw away the
// async answers already on screen — the columns would blank until the next
// probe returned.
func TestSetupReloadKeepsProbedFacts(t *testing.T) {
	m := newTestModel(t)
	m.setup.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("harness 1.2.3\n"), nil
	}
	cmd := m.setup.open()
	m.Update(cmd())
	m.setup.setModelFor("claude", "opus-5")
	for _, row := range m.setup.model.Agents {
		if row.Version != "1.2.3" {
			t.Fatalf("%s version = %q after a write; the probed tier was dropped", row.Agent, row.Version)
		}
	}
}

// The wizard covers the workspace, so the background art must freeze — the
// art tick only advances while workspaceIdle().
func TestSetupActiveFreezesBackgroundArt(t *testing.T) {
	m := newTestModel(t)
	m.setup.open()
	if m.workspaceIdle() {
		t.Fatal("the setup view replaces the workspace; art must not animate under it")
	}
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.workspaceIdle() {
		t.Fatal("closing the view returns the plain workspace")
	}
}

// The render path must never block on a subprocess.
func TestSetupOpenRunsNoSubprocess(t *testing.T) {
	m := newTestModel(t)
	var ran int
	m.setup.run = func(context.Context, string, ...string) ([]byte, error) {
		ran++
		return nil, nil
	}
	m.setup.open()
	_ = m.View()
	if ran != 0 {
		t.Fatalf("open+View ran %d subprocesses; tier 2 is async only", ran)
	}
}
