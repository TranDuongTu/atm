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
//
// The out-of-band selection is `ollama:claude` rather than another agent
// deliberately: the model is keyed by (agent, launcher), so a re-read
// resolves claude's key to "ollama:claude" while a write from the snapshot
// taken at open() would resolve it to "claude". Any selection that left both
// paths agreeing would make this test unable to fail.
func TestSetupActionRereadsBeforeWriting(t *testing.T) {
	m := newTestModel(t)
	m.setup.open()
	writeAgentsConfigOutOfBand(t, m, `{"selected":"ollama:claude"}`)
	m.setup.setModelFor("claude", "opus-5")
	cfg := readAgentsConfig(t, m)
	if cfg.Selected != "ollama:claude" {
		t.Fatalf("selected = %q; the out-of-band selection was clobbered", cfg.Selected)
	}
	if cfg.Models["ollama:claude"] != "opus-5" {
		t.Fatalf("models = %+v; the model was keyed off a stale read", cfg.Models)
	}
	if cfg.Models["claude"] != "" {
		t.Fatalf("models = %+v; the native key is not the selection's key", cfg.Models)
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

// A hung harness sits on the 10s timeouts, the user presses `r`, and the
// second probe answers first. When the first finally lands it must be
// dropped: applying it would revert every row to the timed-out answer for
// good — nothing lands afterwards to correct it — and would clear `probing`,
// presenting the stale facts as settled.
func TestSetupStaleProbeIsDropped(t *testing.T) {
	m := newTestModel(t)
	m.setup.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("harness 1.1.1\n"), nil
	}
	slow := m.setup.open()
	// The command captured the runner (and the generation) when it was
	// built, so swapping the runner now only affects the next firing.
	m.setup.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("harness 2.2.2\n"), nil
	}
	fast := m.setup.refresh()
	slowMsg, fastMsg := slow(), fast()

	// The superseded firing lands first: dropped whole, probing untouched.
	m.Update(slowMsg)
	if !m.setup.probing {
		t.Fatal("a dropped message must not clear probing; the current firing has not answered")
	}
	for _, row := range m.setup.model.Agents {
		if row.Version != "" {
			t.Fatalf("%s version = %q from a superseded probe", row.Agent, row.Version)
		}
	}

	m.Update(fastMsg)
	if m.setup.probing {
		t.Fatal("the current firing landed")
	}

	// And again after it landed: the late answer must not win by arriving
	// last.
	m.Update(slowMsg)
	for _, row := range m.setup.model.Agents {
		if row.Version != "2.2.2" {
			t.Fatalf("%s version = %q; a late probe overwrote the newer facts", row.Agent, row.Version)
		}
	}
	if m.setup.probing {
		t.Fatal("a dropped message must not reopen probing either")
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

// stubGitOnPath puts a `git` at the front of PATH that logs every invocation
// and answers `rev-parse --is-inside-work-tree` with "true", so a probed path
// runs the WHOLE repo probe (rev-parse, status, rev-list) instead of stopping
// at the first check. A real t.TempDir() is not a git repo, so a fixture wired
// to one stops after a single call — which is why the existing suite could not
// see the difference this test is about. It returns the log's path.
func stubGitOnPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "git-calls.log")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> '" + log + "'\n" +
		"case \"$*\" in *is-inside-work-tree*) echo true ;; esac\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write stub git: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// gitCalls counts the invocations the stub recorded, and forgets them, so each
// assertion measures one action rather than the whole test.
func gitCalls(t *testing.T, log string) int {
	t.Helper()
	body, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read git call log: %v", err)
	}
	if err := os.Remove(log); err != nil {
		t.Fatalf("reset git call log: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// seedWiredRepoChannel gives the project a repo channel wired to a directory
// on this machine, which is what makes store.ProjectChannels shell out to git.
func seedWiredRepoChannel(t *testing.T, m *Model, code, name string) {
	t.Helper()
	if _, err := m.store.CreateChannel(code, core.ChannelRecord{Name: name, Type: core.ChannelTypeRepo}, testActor); err != nil {
		t.Fatalf("CreateChannel %s: %v", name, err)
	}
	if err := m.store.SetChannelWiring(code, name, t.TempDir(), "", testActor); err != nil {
		t.Fatalf("SetChannelWiring %s: %v", name, err)
	}
}

// refreshAll runs on every 10s tick and after every mutation, whether or not
// the wizard was ever opened, and its only consumer of this model is the
// status-bar nudge — which reads Agents alone. Reloading the project half
// there would put three untimed `git` calls per wired repo channel behind a
// background timer forever, and a path on a stale mount would wedge Update.
func TestRefreshAllSkipsRepoProbesWhileTheWizardIsClosed(t *testing.T) {
	log := stubGitOnPath(t)
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedWiredRepoChannel(t, m, "ATM", "atm-repo")
	m.projectScope = "ATM"
	gitCalls(t, log) // forget whatever the seeding itself did

	m.refreshAll()
	if n := gitCalls(t, log); n != 0 {
		t.Fatalf("refreshAll ran %d git calls with the wizard closed; the background path must probe no repos", n)
	}
	// Skipping the project half must not skip the half the nudge needs.
	if len(m.setup.model.Agents) == 0 {
		t.Fatal("refreshAll must still populate the agent rows; the nudge reads them")
	}

	// And the fixture has to be one where the difference is visible at all:
	// opening the wizard is exactly when the project data is wanted.
	m.setup.open()
	if n := gitCalls(t, log); n == 0 {
		t.Fatal("open() probed no repo — this fixture cannot tell the two reloads apart, so the assertion above proves nothing")
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
