package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/setup"
	"atm/skills"
)

// --- helpers ---

// setupActionsModel builds a model whose three harnesses are all on PATH but
// have NO plugin installed under a fresh $HOME: every agent row glyphs ◐,
// which is exactly the state the fix ladder exists to repair.
func setupActionsModel(t *testing.T) *Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	installFakeAgentsOnPath(t, home)
	return newTestModel(t)
}

// seedActionProject creates a project with the checklist capability on and
// scopes the model to it, so the wizard has CHANNELS and PERSONAS sections.
func seedActionProject(t *testing.T, m *Model, code string) {
	t.Helper()
	if _, err := m.store.CreateProject(code, code, testActor); err != nil {
		t.Fatalf("CreateProject %s: %v", code, err)
	}
	if err := m.store.EnableProjectCapability(code, "checklist", testActor); err != nil {
		t.Fatalf("EnableProjectCapability checklist: %v", err)
	}
	m.projectScope = code
}

// seedNotionChannel authors a notion channel, the type whose recipe names an
// MCP server (see setup.RecipeFor).
func seedNotionChannel(t *testing.T, m *Model, code, name string) {
	t.Helper()
	if _, err := m.store.CreateChannel(code, core.ChannelRecord{Name: name, Type: core.ChannelTypeNotion}, testActor); err != nil {
		t.Fatalf("CreateChannel %s: %v", name, err)
	}
}

// focusAgent puts the wizard's cursor on the named agent row, the way ↑/↓
// would. The row order comes from agent.Harnesses(), so no test may assume an
// index.
func focusAgent(t *testing.T, m *Model, name string) {
	t.Helper()
	m.setup.section, m.setup.drilled = setupSectionAgents, false
	for i, r := range m.setup.model.Agents {
		if r.Agent == name {
			m.setup.cursor = i
			return
		}
	}
	t.Fatalf("no agent row %q in %+v", name, m.setup.model.Agents)
}

func agentRow(t *testing.T, m *Model, name string) setup.AgentRow {
	t.Helper()
	for _, r := range m.setup.model.Agents {
		if r.Agent == name {
			return r
		}
	}
	t.Fatalf("no agent row %q", name)
	return setup.AgentRow{}
}

// recordingRun is a setup.RunFunc that answers `--version` and `mcp list` from
// a fixture and records every command it was asked to run, so a test can prove
// which subprocesses an action really launched — and which it did not.
type recordingRun struct {
	// mcpList maps agent -> `mcp list` stdout. An absent key means the probe
	// could not answer, which must land as unknown, never as "no servers".
	mcpList map[string]string
	calls   [][]string // each entry is the binary followed by its args
}

func (r *recordingRun) run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	switch {
	case len(args) == 1 && args[0] == "--version":
		return []byte("9.9.9\n"), nil
	case len(args) >= 2 && args[0] == "mcp" && args[1] == "list":
		out, ok := r.mcpList[name]
		if !ok {
			return nil, errProbeUnavailable
		}
		return []byte(out), nil
	}
	return nil, nil
}

// ranWith reports whether the recorder saw exactly this command.
func (r *recordingRun) ranWith(argv ...string) bool {
	for _, c := range r.calls {
		if reflect.DeepEqual(c, argv) {
			return true
		}
	}
	return false
}

// claudeListWithNotionDown is `claude mcp list` output naming the notion
// server as configured but NOT connected — the state `mcp login` fixes.
const claudeListWithNotionDown = "Checking MCP server health...\n\nnotion: https://mcp.notion.com/mcp (HTTP) - ✗ Failed to connect\n"

// probeOnce drains the async tier deterministically: open() returns the tier-2
// command, and Update folds its answer in exactly as Bubble Tea would.
func probeOnce(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.setup.open()
	if cmd == nil {
		t.Fatal("open must return the async tier's command")
	}
	m.Update(cmd())
	if m.setup.probing {
		t.Fatal("the probe should have landed")
	}
}

func checklistSeedNamed(t *testing.T, persona, name string) skills.ChecklistSeed {
	t.Helper()
	for _, s := range skills.ChecklistSeeds() {
		if s.Name == name && slices.Contains(s.Suits, persona) {
			return s
		}
	}
	t.Fatalf("no shipped starter %s suited to %s", name, persona)
	return skills.ChecklistSeed{}
}

// seedOneStarterAndEditIt authors project ATM with one shipped starter already
// present AND edited away from the shipped steps — the record BuildPersonas
// classifies as Customised, and the one seeding must never touch.
func seedOneStarterAndEditIt(t *testing.T, m *Model) {
	t.Helper()
	seedActionProject(t, m, "ATM")
	seed := checklistSeedNamed(t, "concierge", "empty-project")
	if _, err := m.store.CreateChecklist("ATM", setup.SeedRecord("ATM", seed), testActor); err != nil {
		t.Fatalf("CreateChecklist: %v", err)
	}
	mine := setup.SeedRecord("ATM", seed)
	mine.Steps = []core.ChecklistStep{{Text: "my own first step"}, {Text: "my own second step"}}
	if err := m.store.SetChecklist("ATM", seed.Name, mine, testActor); err != nil {
		t.Fatalf("SetChecklist: %v", err)
	}
}

// readChecklistSteps flattens the record's step tree to its texts, depth-first.
func readChecklistSteps(t *testing.T, m *Model, name string) []string {
	t.Helper()
	rec, err := m.store.GetChecklist("ATM", name)
	if err != nil {
		t.Fatalf("GetChecklist %s: %v", name, err)
	}
	var flat func(steps []core.ChecklistStep) []string
	flat = func(steps []core.ChecklistStep) []string {
		var out []string
		for _, s := range steps {
			out = append(out, s.Text)
			out = append(out, flat(s.Children)...)
		}
		return out
	}
	return flat(rec.Steps)
}

// --- the bootstrap paradox ---

// The bootstrap paradox: on an empty store, with no agent ready, every
// action that makes the FIRST agent ready must still work.
func TestDirectActionsNeedNoReadyAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no plugin anywhere, so no agent is ready
	m := newTestModelEmptyStore(t)
	m.setup.open()
	if setupAnyReady(m.setup.model.Agents) {
		t.Fatalf("precondition: nothing may be ready here: %+v", m.setup.model.Agents)
	}
	// install plugin, make default, mcp add, stamp/seed, enable capability
	for _, key := range []string{"i", "d", "a", "s", "e"} {
		if gated := m.setup.actionGated(key); gated {
			t.Fatalf("%q must not be gated on an agent being ready", key)
		}
	}
}

func TestConciergeActionsAreGatedUntilAnAgentIsReady(t *testing.T) {
	// A fresh, empty $HOME has no agent plugin installed for any harness, so
	// no row can glyph ●. Without it this test would assert the state of the
	// MACHINE it runs on: a developer with ATM's plugins already installed has
	// a ready agent even on an empty store, and the gate would correctly be
	// open (see TestNudgeFiresFromANormalSessionWithoutOpeningTheWizard, which
	// isolates $HOME for the same reason).
	t.Setenv("HOME", t.TempDir())
	m := newTestModelEmptyStore(t)
	m.setup.open()
	if !m.setup.actionGated("interview") {
		t.Fatal("a concierge dispatch needs a ready agent first")
	}
	if !m.setup.actionGated("author") {
		t.Fatal("authoring through a concierge session needs a ready agent too")
	}
}

// The gate must LIFT — an actionGated that always refused would pass the test
// above while making the concierge permanently unreachable.
func TestConciergeActionsUngatedOnceAnAgentIsReady(t *testing.T) {
	m := newTestModelWithFullyReadyAgents(t)
	m.setup.open()
	if m.setup.actionGated("interview") {
		t.Fatalf("every agent is ready: %+v", m.setup.model.Agents)
	}
}

// The concierge key refuses with a toast rather than dispatching into nothing.
func TestConciergeKeyRefusesWhileGated(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // nothing installed, so nothing is ready
	m := newTestModelEmptyStore(t)
	m.setup.open()
	m.setup.handleKey(keyMsg("c"))
	if m.dispatchDlg.active {
		t.Fatal("no agent is ready; the dispatch dialog must not open")
	}
	if !strings.Contains(m.toastMsg, "ready") {
		t.Fatalf("toast = %q, want an explanation naming readiness", m.toastMsg)
	}
}

func TestConciergeKeyOpensTheDispatchDialog(t *testing.T) {
	m := newTestModelWithFullyReadyAgents(t)
	m.setup.open()
	m.setup.handleKey(keyMsg("c"))
	if !m.dispatchDlg.active {
		t.Fatal("with an agent ready, c hands off to the dispatch dialog")
	}
	if m.dispatchDlg.persona() != "concierge" {
		t.Fatalf("persona = %q, want concierge", m.dispatchDlg.persona())
	}
}

// --- spawned actions ---

// dispatch fails when there is no herdr/tmux/terminal_cmd — plausibly the
// first-run case. Dead-ending there would strand the user.
func TestNoDispatchTargetShowsTheCommandInstead(t *testing.T) {
	m := newTestModel(t)
	m.dispatcher = &fakeDispatcher{spawnErr: errors.New("no dispatch target")}
	m.setup.open()
	m.setup.runSpawnAction("claude", []string{"mcp", "login", "notion"})
	if !strings.Contains(m.toastMsg, "claude mcp login notion") {
		t.Fatalf("toast = %q, want the literal command to run", m.toastMsg)
	}
}

// The fallback is only worth anything if the command it shows can be PASTED.
// Harnesses really do return server names with spaces in them — `claude mcp
// list` reports "claude.ai Google Drive", which is why the parser splits on
// the first ": " — and a bare join would turn that into three arguments. The
// other extreme (quoting everything, as dispatch.ShellCommand does) is noise
// on the common case AND fails the plain-command assertion above, so the rule
// is: quote only what a shell would otherwise mangle.
func TestNoDispatchTargetQuotesOnlyWhatNeedsIt(t *testing.T) {
	m := newTestModel(t)
	m.dispatcher = &fakeDispatcher{spawnErr: errors.New("no dispatch target")}
	m.setup.open()
	m.setup.runSpawnAction("claude", []string{"mcp", "login", "claude.ai Google Drive"})
	if !strings.Contains(m.toastMsg, `claude mcp login 'claude.ai Google Drive'`) {
		t.Fatalf("toast = %q, want the space-containing server quoted as ONE argument", m.toastMsg)
	}
}

func TestShellArgQuotesOnlyWhatNeedsIt(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"notion", "notion"},
		{"claude.ai Google Drive", `'claude.ai Google Drive'`},
		{"--transport", "--transport"},
		{"https://mcp.notion.com/mcp", "https://mcp.notion.com/mcp"},
		{"it's", `'it'\''s'`},
		{"a;rm -rf b", `'a;rm -rf b'`},
		{"~/x", `'~/x'`}, // bare, a shell would expand it to somewhere else
		{"", "''"},       // an empty argument must survive as an argument
	} {
		if got := setupShellArg(tc.in); got != tc.want {
			t.Errorf("setupShellArg(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A build with no dispatcher at all is the same dead end, and must not panic.
func TestNoDispatcherAtAllShowsTheCommandInstead(t *testing.T) {
	m := newTestModel(t)
	m.dispatcher = nil
	m.setup.open()
	m.setup.runSpawnAction("claude", []string{"update"})
	if !strings.Contains(m.toastMsg, "claude update") {
		t.Fatalf("toast = %q, want the literal command to run", m.toastMsg)
	}
}

func TestSpawnedActionUsesTheDispatcher(t *testing.T) {
	m := newTestModel(t)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.setup.open()
	m.setup.runSpawnAction("claude", []string{"mcp", "login", "notion"})
	if len(fd.spawned) != 1 {
		t.Fatalf("spawned %d specs, want 1", len(fd.spawned))
	}
	spec := fd.spawned[0]
	if want := []string{"claude", "mcp", "login", "notion"}; !reflect.DeepEqual(spec.Argv, want) {
		t.Fatalf("argv = %v, want %v", spec.Argv, want)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if spec.Dir != wd {
		t.Fatalf("dir = %q, want the working directory %q", spec.Dir, wd)
	}
}

// An update is interactive and long-running: it is handed to the dispatcher,
// never run headless behind the wizard.
func TestUpdateIsSpawnedNotRun(t *testing.T) {
	m := setupActionsModel(t)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	run := &recordingRun{mcpList: map[string]string{"claude": "", "codex": "[]", "opencode": ""}}
	m.setup.run = run.run
	probeOnce(t, m)
	focusAgent(t, m, "claude")
	m.setup.handleKey(keyMsg("u"))

	if len(fd.spawned) != 1 {
		t.Fatalf("spawned %d specs, want 1", len(fd.spawned))
	}
	if want := []string{"claude", "update"}; !reflect.DeepEqual(fd.spawned[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", fd.spawned[0].Argv, want)
	}
	if run.ranWith("claude", "update") {
		t.Fatal("an update must never be run headless from the wizard")
	}
}

// `mcp login` is interactive — the harness owns the credential prompt — so it
// is spawned, and only for a server the agent has actually configured.
func TestLoginIsSpawnedForAConfiguredButUnauthorizedServer(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "ATM")
	seedNotionChannel(t, m, "ATM", "specs")
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	run := &recordingRun{mcpList: map[string]string{
		"claude": claudeListWithNotionDown, "codex": "[]", "opencode": "",
	}}
	m.setup.run = run.run
	probeOnce(t, m)

	focusAgent(t, m, "claude")
	m.setup.handleKey(keyMsg("l"))
	if len(fd.spawned) != 1 {
		t.Fatalf("spawned %d specs, want 1: toast %q", len(fd.spawned), m.toastMsg)
	}
	if want := []string{"claude", "mcp", "login", "notion"}; !reflect.DeepEqual(fd.spawned[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", fd.spawned[0].Argv, want)
	}

	// opencode has not configured the server at all: logging in would fail, so
	// the wizard points at the step that comes first instead.
	focusAgent(t, m, "opencode")
	m.setup.handleKey(keyMsg("l"))
	if len(fd.spawned) != 1 {
		t.Fatalf("spawned %d specs; login must not run before the server is added", len(fd.spawned))
	}
	if !strings.Contains(m.toastMsg, "[a]") {
		t.Fatalf("toast = %q, want it to point at adding the server first", m.toastMsg)
	}
}

// --- direct actions ---

// Installing the plugin is the action that ends the bootstrap: it runs the
// same developing.InstallPlugin production path `atm agents plugin install`
// uses, with no agent session anywhere in it.
func TestInstallPluginMakesTheRowReady(t *testing.T) {
	m := setupActionsModel(t)
	m.setup.open()
	if g := agentRow(t, m, "claude").Glyph(); g != "◐" {
		t.Fatalf("precondition: binary present, plugin missing => ◐, got %q", g)
	}
	focusAgent(t, m, "claude")
	m.setup.handleKey(keyMsg("i"))
	if g := agentRow(t, m, "claude").Glyph(); g != "●" {
		t.Fatalf("after install glyph = %q, want ● (toast %q)", g, m.toastMsg)
	}
}

// ATM can install its own plugin for a harness that is not installed at all;
// it cannot install the harness. The ○ grade means the rest of the fix is
// outside ATM, and the toast has to say so rather than read as "done".
func TestInstallPluginSaysWhenTheBinaryIsStillMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	empty := filepath.Join(home, "empty-bin")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("PATH", empty) // REPLACED: a real claude here would make this vacuous
	m := newTestModel(t)
	m.setup.open()
	focusAgent(t, m, "claude")
	m.setup.handleKey(keyMsg("i"))
	if !strings.Contains(m.toastMsg, "still missing") {
		t.Fatalf("toast = %q, want it to say the binary is still missing", m.toastMsg)
	}
	if g := agentRow(t, m, "claude").Glyph(); g != "○" {
		t.Fatalf("glyph = %q, want ○ — the remaining fix is outside ATM", g)
	}
}

// Making an agent the default re-reads agents.json first: a CLI in another
// terminal may have chosen the ollama launcher for this same agent, and
// writing "claude" back from the wizard's snapshot would silently demote it.
func TestMakeDefaultKeepsTheLauncherTheStoreAlreadyChose(t *testing.T) {
	m := setupActionsModel(t)
	m.setup.open()
	writeAgentsConfigOutOfBand(t, m, `{"selected":"ollama:claude"}`)
	focusAgent(t, m, "claude")
	m.setup.handleKey(keyMsg("d"))
	if cfg := readAgentsConfig(t, m); cfg.Selected != "ollama:claude" {
		t.Fatalf("selected = %q; the out-of-band launcher was clobbered", cfg.Selected)
	}

	// A different agent has no launcher to preserve, so it takes the native one
	// — which is the launcher that is actually installed here.
	focusAgent(t, m, "codex")
	m.setup.handleKey(keyMsg("d"))
	if cfg := readAgentsConfig(t, m); cfg.Selected != "codex" {
		t.Fatalf("selected = %q, want codex", cfg.Selected)
	}
}

// With only ollama on PATH, `ollama launch <agent> --` is the one launcher
// that can actually start this harness, so that is what the selection records.
func TestMakeDefaultFallsBackToOllamaWhenTheNativeBinaryIsAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ollama"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake ollama: %v", err)
	}
	// PATH is REPLACED, not prepended: a real claude installed on this machine
	// would otherwise make the native launcher present and the test vacuous.
	t.Setenv("PATH", binDir)
	m := newTestModel(t)
	m.setup.open()
	focusAgent(t, m, "claude")
	m.setup.handleKey(keyMsg("d"))
	if cfg := readAgentsConfig(t, m); cfg.Selected != "ollama:claude" {
		t.Fatalf("selected = %q, want ollama:claude", cfg.Selected)
	}
}

// `mcp add` is non-interactive, so ATM runs it itself — but only for the
// servers this agent's own `mcp list` did not report.
func TestMCPAddConfiguresOnlyTheServersTheAgentLacks(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "ATM")
	seedNotionChannel(t, m, "ATM", "specs")
	run := &recordingRun{mcpList: map[string]string{
		"claude": claudeListWithNotionDown, "codex": "[]", "opencode": "",
	}}
	m.setup.run = run.run
	probeOnce(t, m)
	run.calls = nil // the probe's own calls are not what this test is about

	focusAgent(t, m, "opencode")
	m.setup.handleKey(keyMsg("a"))
	if !run.ranWith("opencode", "mcp", "add", "notion", "--url", "https://mcp.notion.com/mcp") {
		t.Fatalf("calls = %v, want opencode's own mcp add verb", run.calls)
	}

	// claude already has notion configured (it is merely not connected); adding
	// it again would fail, and re-running a write ATM did not need is exactly
	// what "seed only what is absent" rules out.
	run.calls = nil
	focusAgent(t, m, "claude")
	m.setup.handleKey(keyMsg("a"))
	for _, c := range run.calls {
		if len(c) > 1 && c[1] == "mcp" && len(c) > 2 && c[2] == "add" {
			t.Fatalf("re-added a server claude already has: %v", c)
		}
	}
}

// realEmptyMCPLists is what `mcp list` REALLY prints on a machine with no MCP
// server configured at all, captured live from claude 2.1.233, opencode
// 1.18.18 and codex 0.145.0 under an isolated $HOME. Note how little it
// resembles the "" that the fixtures above use for an empty opencode: two of
// the three harnesses answer in prose, which is why the parsers need
// setup.mcpEmptyCatalogBanner to tell that answer apart from output they
// simply could not read.
var realEmptyMCPLists = map[string]string{
	"claude":   "No MCP servers configured. Use `claude mcp add` to add a server.\n",
	"codex":    "[]",
	"opencode": "┌  MCP Servers\n│\n▲  No MCP servers configured\n│\n└  Add servers with: opencode mcp add\n",
}

// The bootstrap rung, on the machine it exists for. A fresh machine has no MCP
// server anywhere, and `[a]` is what fixes that — but `[a]` refuses while the
// mcp state is unknown, so a harness's empty-catalog answer being MISREAD as
// unknown made this rung dead-end permanently for claude and opencode
// (re-probing cannot change the answer). This drives the real key against the
// real captured output.
func TestMCPAddWorksOnAMachineWithNoServersConfiguredAtAll(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "ATM")
	seedNotionChannel(t, m, "ATM", "specs")
	run := &recordingRun{mcpList: realEmptyMCPLists}
	m.setup.run = run.run
	probeOnce(t, m)

	for _, ag := range []string{"claude", "codex", "opencode"} {
		if got := agentRow(t, m, ag).MCPState; got != setup.FactPresent {
			t.Fatalf("%s: mcp state = %v — the harness answered, and its answer was 'none'", ag, got)
		}
	}
	for _, ag := range []string{"claude", "opencode"} {
		run.calls = nil
		focusAgent(t, m, ag)
		m.setup.handleKey(keyMsg("a"))
		adapter, _ := setup.MCPAdapterFor(ag)
		recipe, _ := setup.RecipeFor(core.ChannelTypeNotion)
		want := append([]string{ag}, adapter.AddArgv(recipe)...)
		if !run.ranWith(want...) {
			t.Fatalf("%s: calls = %v, want %v — [a] must configure the server on a machine that has none", ag, run.calls, want)
		}
	}
}

// A probe that could not answer is unknown, never missing: `mcp add` must not
// "repair" a configuration it was never able to read.
func TestMCPAddRefusesWhenTheProbeCouldNotAnswer(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "ATM")
	seedNotionChannel(t, m, "ATM", "specs")
	run := &recordingRun{mcpList: map[string]string{ // claude absent: its probe fails
		"codex": "[]", "opencode": "",
	}}
	m.setup.run = run.run
	probeOnce(t, m)
	if got := agentRow(t, m, "claude").MCPState; got != setup.FactUnknown {
		t.Fatalf("precondition: mcp state = %v, want unknown", got)
	}
	run.calls = nil

	focusAgent(t, m, "claude")
	m.setup.handleKey(keyMsg("a"))
	for _, c := range run.calls {
		if len(c) > 2 && c[1] == "mcp" && c[2] == "add" {
			t.Fatalf("ran %v against an agent whose mcp state is unknown", c)
		}
	}
	if !strings.Contains(m.toastMsg, "unknown") {
		t.Fatalf("toast = %q, want it to say the state is unknown", m.toastMsg)
	}
}

// Stamping is ATM vouching for what the user just verified — a store write,
// no agent involved.
func TestStampChannelRecordsAStamp(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "ATM")
	seedNotionChannel(t, m, "ATM", "specs")
	m.setup.open()
	m.setup.section, m.setup.cursor = setupSectionChannels, 0
	m.setup.handleKey(keyMsg("s"))

	v, err := m.store.GetChannelByName("ATM", "specs")
	if err != nil {
		t.Fatalf("GetChannelByName: %v (toast %q)", err, m.toastMsg)
	}
	if v.Wiring == nil || len(v.Wiring.Stamps) != 1 {
		t.Fatalf("wiring = %+v, want exactly one stamp", v.Wiring)
	}
	if v.Wiring.Stamps[0].By != m.actor {
		t.Fatalf("stamped by %q, want %q", v.Wiring.Stamps[0].By, m.actor)
	}
}

// The renderer already tells the user to press [e]; the key has to do it.
func TestEnableChecklistCapability(t *testing.T) {
	m := setupActionsModel(t)
	if _, err := m.store.CreateProject("WEB", "WEB", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// Recording any capability makes the enabled set explicit, so checklist is
	// no longer covered by the legacy "nil means all built-ins" rule.
	if err := m.store.EnableProjectCapability("WEB", "scrum", testActor); err != nil {
		t.Fatalf("EnableProjectCapability: %v", err)
	}
	m.projectScope = "WEB"
	m.setup.open()
	if m.setup.model.Project.ChecklistCapEnabled {
		t.Fatal("precondition: checklists are off for WEB")
	}
	// The section is empty while the capability is off, so this action must
	// work with no row under the cursor.
	m.setup.section, m.setup.cursor = setupSectionPersonas, 0
	m.setup.handleKey(keyMsg("e"))
	if !m.setup.model.Project.ChecklistCapEnabled {
		t.Fatalf("[e] must enable the checklist capability (toast %q)", m.toastMsg)
	}
}

// The model editor is direct: ATM's own form, ATM's own write, no agent
// session — so it works on a store where nothing is ready yet. This drives the
// real key path end to end, including submitForm's routing.
func TestModelFormWritesTheSelectionsModel(t *testing.T) {
	m := setupActionsModel(t)
	m.SetSize(120, 40)
	m.handleKey(keyMsg("W")) // open the wizard the way a keypress does
	focusAgent(t, m, "claude")
	m.handleKey(keyMsg("m"))
	if m.form == nil || m.formKind != formSetupAgentModel {
		t.Fatalf("form = %v, kind = %v; [m] must open the model editor", m.form, m.formKind)
	}
	for _, r := range "opus-5" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if m.form != nil {
		t.Fatal("the form closes on submit")
	}
	if cfg := readAgentsConfig(t, m); cfg.Models["claude"] != "opus-5" {
		t.Fatalf("models = %+v, want claude -> opus-5", cfg.Models)
	}
}

// The wizard writes to the row the user chose, not to whatever the cursor
// happens to be on when the form lands: a refresh tick can reload the model
// (and clamp the cursor) while the form is up.
func TestModelFormWritesTheRowItWasOpenedOn(t *testing.T) {
	m := setupActionsModel(t)
	m.SetSize(120, 40)
	m.setup.open()
	focusAgent(t, m, "codex")
	m.setup.handleKey(keyMsg("m"))
	m.setup.cursor = 0 // as a reload/clamp underneath the form would
	m.form.Fields[0].Value = "gpt-mini"
	m.submitForm()
	if cfg := readAgentsConfig(t, m); cfg.Models["codex"] != "gpt-mini" {
		t.Fatalf("models = %+v, want codex -> gpt-mini", cfg.Models)
	}
}

// Wiring a repo channel is a store write, no agent involved — and the path is
// asked for, never guessed from the working directory.
func TestWireFormRecordsThePathOnThisMachine(t *testing.T) {
	m := setupActionsModel(t)
	m.SetSize(120, 40)
	seedActionProject(t, m, "ATM")
	if _, err := m.store.CreateChannel("ATM", core.ChannelRecord{Name: "atm-repo", Type: core.ChannelTypeRepo}, testActor); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	clone := t.TempDir()
	m.setup.open()
	m.setup.section, m.setup.cursor = setupSectionChannels, 0
	m.setup.handleKey(keyMsg("w"))
	if m.form == nil || m.formKind != formSetupChannelWire {
		t.Fatalf("form = %v, kind = %v; [w] must open the wire editor", m.form, m.formKind)
	}
	m.form.Fields[0].Value = clone
	m.submitForm()

	v, err := m.store.GetChannelByName("ATM", "atm-repo")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if v.Wiring == nil || v.Wiring.Path != clone {
		t.Fatalf("wiring = %+v, want path %q (toast %q)", v.Wiring, clone, m.toastMsg)
	}
}

// A notion channel has no path — it is reached through an MCP server — so the
// wizard says so rather than collecting a value it cannot use.
func TestWireRefusesANotionChannel(t *testing.T) {
	m := setupActionsModel(t)
	m.SetSize(120, 40)
	seedActionProject(t, m, "ATM")
	seedNotionChannel(t, m, "ATM", "specs")
	m.setup.open()
	m.setup.section, m.setup.cursor = setupSectionChannels, 0
	m.setup.handleKey(keyMsg("w"))
	if m.form != nil {
		t.Fatal("a notion channel has no path to wire")
	}
	if !strings.Contains(m.toastMsg, "MCP server") {
		t.Fatalf("toast = %q, want it to name how a notion channel IS reached", m.toastMsg)
	}
}

// A ladder nobody can see is not a ladder: the footer names the fixes the
// FOCUSED section offers, and every key it names has to be bound.
func TestFooterAdvertisesTheFocusedSectionsFixes(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "ATM")
	m.setup.open()
	if out := m.setup.render(100, 30); !strings.Contains(out, "[i]plugin") {
		t.Fatal("the agents section offers the plugin install")
	}
	m.setup.section = setupSectionPersonas
	out := m.setup.render(100, 30)
	if strings.Contains(out, "[i]plugin") {
		t.Fatal("the personas section cannot install a plugin; its hints are its own")
	}
	if !strings.Contains(out, "[s]seed starters") {
		t.Fatalf("personas footer = %q", out)
	}
}

// seed adds ONLY what is absent; a customised checklist must survive.
func TestSeedStartersLeavesCustomisedRecordsAlone(t *testing.T) {
	m := newTestModel(t)
	seedOneStarterAndEditIt(t, m)
	before := readChecklistSteps(t, m, "empty-project")
	m.setup.seedStarters("ATM")
	after := readChecklistSteps(t, m, "empty-project")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("seeding must not overwrite a customised starter")
	}
}

// The other half of the same rule: everything the store is missing IS seeded,
// exactly once.
func TestSeedStartersAddsEveryAbsentStarterOnce(t *testing.T) {
	m := newTestModel(t)
	seedOneStarterAndEditIt(t, m)
	m.setup.seedStarters("ATM")
	for _, s := range skills.ChecklistSeeds() {
		if _, err := m.store.GetChecklist("ATM", s.Name); err != nil {
			t.Fatalf("starter %s not seeded: %v", s.Name, err)
		}
	}
	// A second press must be a no-op, not a pile of duplicates or an error.
	m.setup.seedStarters("ATM")
	records, err := m.store.ChecklistRecords("ATM")
	if err != nil {
		t.Fatalf("ChecklistRecords: %v", err)
	}
	if len(records) != len(skills.ChecklistSeeds()) {
		t.Fatalf("%d checklist records, want %d", len(records), len(skills.ChecklistSeeds()))
	}
}

// The seeded steps are the shipped ones with <CODE> resolved, matching what
// `atm checklist seed` writes — a step telling the user to run a command with
// a literal <CODE> in it is not a step they can follow.
func TestSeedStartersResolvesTheProjectCode(t *testing.T) {
	m := newTestModel(t)
	seedActionProject(t, m, "ATM")
	m.setup.seedStarters("ATM")
	for _, step := range readChecklistSteps(t, m, "empty-project") {
		if strings.Contains(step, "<CODE>") {
			t.Fatalf("unsubstituted step: %q", step)
		}
	}
}

// The personas section's seed key routes to the same action — and the key
// matcher is CASE-SENSITIVE, so a shifted key is a different key entirely and
// never a second way to trigger a write.
func TestSeedKeySeedsTheScopedProject(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "ATM")
	m.setup.open()
	m.setup.section, m.setup.cursor = setupSectionPersonas, 0

	m.setup.handleKey(keyMsg("S"))
	if _, err := m.store.GetChecklist("ATM", "empty-project"); err == nil {
		t.Fatal("[S] is not [s]: an uppercase key must not run the lowercase action")
	}

	m.setup.handleKey(keyMsg("s"))
	if _, err := m.store.GetChecklist("ATM", "empty-project"); err != nil {
		t.Fatalf("the personas section's [s] must seed: %v (toast %q)", err, m.toastMsg)
	}
}
