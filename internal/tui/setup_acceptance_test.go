package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/setup"
	"atm/internal/store"
)

// The setup wizard's three acceptance narratives, driven through the real
// state machine: no hand-assigned model, no hand-computed readiness. Each one
// is a whole story the spec asks for — empty store, a fourth project, repair —
// rather than a unit of one function, so a refactor that keeps every unit test
// green but breaks the story still fails here.
//
// Two helpers the plan named for this file already exist in the package and
// are reused rather than written twice:
//
//	awaitProbe -> probeOnce      (setup_actions_test.go) — open() returns the
//	                             tier-2 tea.Cmd; probeOnce runs it and feeds
//	                             the message through m.Update exactly as the
//	                             Bubble Tea runtime would. Deterministic, no
//	                             sleep, no goroutine.
//	agentRowNamed -> agentRow    (setup_actions_test.go)
//
// The channel/persona lookups have no equivalent yet, so they are written
// here as channelRowNamed/personaRowNamed.

// --- fixtures: real harness output ---
//
// Every fixture below was captured from the harness actually installed on the
// machine this was written on, not invented. That matters: the parsers exist
// to be true about real output, and inventing a plausible shape is exactly how
// a parser bug survives a green suite.

// claudeMCPOutWithNotion is `claude mcp list` with the notion server added AND
// authorized — the state an agent reaches once it has done this work for ANY
// project, since a harness's MCP configuration is global to the harness.
// Captured live and trimmed to the one server this narrative is about; the
// health banner and the blank line after it are kept because they are what the
// parser has to skip past.
const claudeMCPOutWithNotion = "Checking MCP server health…\n" +
	"\n" +
	"notion: https://mcp.notion.com/mcp (HTTP) - ✔ Connected\n"

// The empty-catalog outputs are realEmptyMCPLists (setup_actions_test.go) —
// the same live captures, kept in one place in this package. Note what
// opencode's is NOT: an empty string. It prints its box banner and says
// "No MCP servers configured" in words, which is why setup.mcpEmptyCatalogBanner
// has to exist for these narratives to read absent rather than unknown.

// --- helpers ---

// fakeMCPRun answers the tier-2 probes from a per-agent fixture of `mcp list`
// stdout. An agent absent from the map cannot answer at all, which must land
// as unknown — never as "no servers" (see recordingRun.run).
func fakeMCPRun(byAgent map[string]string) setup.RunFunc {
	return (&recordingRun{mcpList: byAgent}).run
}

// channelRowNamed is the wizard's row for one channel handle.
func channelRowNamed(t *testing.T, m *Model, name string) setup.ChannelRow {
	t.Helper()
	if m.setup.model.Project == nil {
		t.Fatalf("no project section to find channel %q in", name)
	}
	for _, c := range m.setup.model.Project.Channels {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no channel row %q", name)
	return setup.ChannelRow{}
}

// personaRowNamed is the wizard's row for one persona.
func personaRowNamed(t *testing.T, m *Model, name string) setup.PersonaRow {
	t.Helper()
	if m.setup.model.Project == nil {
		t.Fatalf("no project section to find persona %q in", name)
	}
	for _, p := range m.setup.model.Project.Personas {
		if p.Persona == name {
			return p
		}
	}
	t.Fatalf("no persona row %q", name)
	return setup.PersonaRow{}
}

// mustAddRepoChannel authors a repo channel AND wires it to a real directory
// on this machine — both halves, because a repo channel's coverage is decided
// by the wiring, not by the ledger record.
func mustAddRepoChannel(t *testing.T, m *Model, code, name, path string) {
	t.Helper()
	if _, err := m.store.CreateChannel(code, core.ChannelRecord{Name: name, Type: core.ChannelTypeRepo}, testActor); err != nil {
		t.Fatalf("CreateChannel %s: %v", name, err)
	}
	if err := m.store.SetChannelWiring(code, name, "", path, "", testActor); err != nil {
		t.Fatalf("SetChannelWiring %s: %v", name, err)
	}
}

// mustAddNotionChannel authors a notion channel and records which MCP server
// this machine reaches it through.
func mustAddNotionChannel(t *testing.T, m *Model, code, name, mcpServer string) {
	t.Helper()
	seedNotionChannel(t, m, code, name)
	if err := m.store.SetChannelWiring(code, name, "", "", mcpServer, testActor); err != nil {
		t.Fatalf("SetChannelWiring %s: %v", name, err)
	}
}

// stampChannelDaysAgo records a verification stamp and then back-dates it in
// config.json. There is no store verb for stamping in the past — and there
// should not be — but staleness is a function of the clock, so the only way to
// test the stale grade without waiting two months is to age the record. The
// stamp is written by the real AddChannelStamp first, so everything about it
// except the timestamp is exactly what production writes.
func stampChannelDaysAgo(t *testing.T, m *Model, code, name string, days int) {
	t.Helper()
	if err := m.store.AddChannelStamp(code, name, "", core.StampKindUse, "verified", testActor); err != nil {
		t.Fatalf("AddChannelStamp %s: %v", name, err)
	}
	path := filepath.Join(m.store.StorePath(), "projects", code, "config.json")
	var cfg core.ProjectConfig
	if err := store.ReadJSON(path, &cfg); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	w, ok := cfg.Channels[name]
	if !ok {
		t.Fatalf("no wiring recorded for %q", name)
	}
	// Stamps live under the endpoint that was reached.
	typ := ""
	for k, e := range w.Endpoints {
		if len(e.Stamps) > 0 {
			typ = k
			break
		}
	}
	if typ == "" {
		t.Fatalf("no stamp recorded for %q", name)
	}
	e := w.Endpoints[typ]
	e.Stamps[len(e.Stamps)-1].At = core.RFC3339UTC(core.Now().AddDate(0, 0, -days))
	w.Endpoints[typ] = e
	cfg.Channels[name] = w
	if err := store.WriteFileAtomic(path, &cfg); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// seedOnlyStarters authors exactly the named shipped starters, with the
// shipped steps, and nothing else — the state of a project seeded by an OLDER
// atm that shipped fewer starters than this one does.
func seedOnlyStarters(t *testing.T, m *Model, code string, names ...string) {
	t.Helper()
	for _, name := range names {
		seed := checklistSeedNamed(t, "concierge", name)
		if _, err := m.store.CreateChecklist(code, setup.SeedRecord(code, seed), testActor); err != nil {
			t.Fatalf("CreateChecklist %s: %v", name, err)
		}
	}
}

// --- narrative 1: the empty store ---

// Narrative 1: empty store. The wizard IS the TUI, agents only, and every
// action on the screen works with no agent ready.
func TestNarrativeEmptyStore(t *testing.T) {
	// A hermetic harness environment: the three binaries on PATH, a fresh
	// $HOME with no plugin installed anywhere. Without this the narrative
	// would assert a glyph computed from whatever happens to be installed on
	// the machine running the suite.
	home := t.TempDir()
	t.Setenv("HOME", home)
	installFakeAgentsOnPath(t, home)

	m := newTestModelEmptyStore(t)
	if !m.setup.active {
		t.Fatal("empty store must land on the wizard")
	}
	if m.setup.model.Project != nil {
		t.Fatal("no project selected: no project sections")
	}
	row := agentRow(t, m, "claude")
	if row.Glyph() != "◐" {
		t.Fatalf("binary present, plugin missing => ◐, got %q", row.Glyph())
	}
	if m.setup.actionGated("i") {
		t.Fatal("installing the first plugin cannot require a ready agent")
	}
	// The other side of the same rule, and the one that makes the assertion
	// above mean something: the gate is real, it just never stands in front of
	// the action that ENDS the bootstrap.
	if !m.setup.actionGated("interview") {
		t.Fatal("a concierge session needs an agent to run in; with none ready it must be gated")
	}

	m.setup.installPlugin("claude")
	if got := agentRow(t, m, "claude").Glyph(); got != "●" {
		t.Fatalf("after install glyph = %q, want ●", got)
	}
	if m.setup.actionGated("interview") {
		t.Fatal("one ready agent lifts the concierge gate")
	}
	if !strings.Contains(m.setup.render(100, 30), "first project") {
		t.Fatal("a ready agent should hand off to project creation")
	}
}

// --- narrative 2: a fourth project ---

// Narrative 2: a fourth project. The payoff of global-per-agent MCP — an
// agent that authorized notion for another project shows the NEW project's
// notion channel green immediately, with no work at all. Nothing here
// re-authorizes anything: the only claude fact in play is its own `mcp list`,
// which is a property of the HARNESS, not of any project.
func TestNarrativeFourthProject(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "WEB")
	mustAddRepoChannel(t, m, "WEB", "web-repo", t.TempDir())
	mustAddNotionChannel(t, m, "WEB", "specs", "notion")
	// claude already has the notion server; opencode and codex do not.
	m.setup.run = fakeMCPRun(map[string]string{
		"claude":   claudeMCPOutWithNotion,
		"codex":    realEmptyMCPLists["codex"],
		"opencode": realEmptyMCPLists["opencode"],
	})
	probeOnce(t, m)

	row := channelRowNamed(t, m, "specs")
	if row.PerAgent["claude"] != setup.FactPresent {
		t.Fatalf("claude authorized notion elsewhere; the new project inherits it — got %v", row.PerAgent["claude"])
	}
	if row.PerAgent["codex"] != setup.FactAbsent {
		t.Fatalf("codex = %v, want absent", row.PerAgent["codex"])
	}
	// opencode is absent for the same reason codex is: it answered, and its
	// answer was "none". That it says so in prose rather than in a parseable
	// row is what the empty-catalog banner exists to recognize — this
	// assertion read "unknown" before that fix, which is how the fix was
	// found (see setup.mcpEmptyCatalogBanner).
	if row.PerAgent["opencode"] != setup.FactAbsent {
		t.Fatalf("opencode = %v, want absent", row.PerAgent["opencode"])
	}
	// The repo channel is machine-scoped: it counts for every agent.
	repo := channelRowNamed(t, m, "web-repo")
	for _, ag := range []string{"claude", "codex", "opencode"} {
		if repo.PerAgent[ag] != setup.FactPresent {
			t.Fatalf("%s: a wired repo channel counts for every agent, got %v", ag, repo.PerAgent[ag])
		}
	}
	// And the agent-row coverage counts derived from those two channels: 2/2
	// for the agent that has notion, 1/2 for the ones that only have the repo.
	if got := agentRow(t, m, "claude"); got.ChannelsOK != 2 || got.ChannelsAll != 2 {
		t.Fatalf("claude coverage = %d/%d, want 2/2", got.ChannelsOK, got.ChannelsAll)
	}
	if got := agentRow(t, m, "opencode"); got.ChannelsOK != 1 || got.ChannelsAll != 2 {
		t.Fatalf("opencode coverage = %d/%d, want 1/2", got.ChannelsOK, got.ChannelsAll)
	}
}

// --- narrative 3: repair ---

// Narrative 3: repair. Three distinct drifts side by side, each with its fix
// beside it — including a starter that is missing only because a newer atm
// shipped it after this project was seeded.
func TestNarrativeRepair(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "ATM")
	mustAddNotionChannel(t, m, "ATM", "specs", "notion")
	stampChannelDaysAgo(t, m, "ATM", "specs", 61)
	seedOnlyStarters(t, m, "ATM", "empty-project", "setup-agent-launcher") // a third now ships
	m.setup.run = fakeMCPRun(map[string]string{
		"claude": claudeMCPOutWithNotion,
		"codex":  realEmptyMCPLists["codex"], "opencode": realEmptyMCPLists["opencode"],
	})
	probeOnce(t, m)

	// Drift 1: the channel is wired and was verified — two months ago.
	if g := channelRowNamed(t, m, "specs").Glyph; g != "○" {
		t.Fatalf("61d-old stamp should read stale (○), got %q", g)
	}
	// Drift 2: one harness never got the server this project's channel needs.
	if got := channelRowNamed(t, m, "specs").PerAgent["codex"]; got != setup.FactAbsent {
		t.Fatalf("codex is missing the notion server, got %v", got)
	}
	// Drift 3: the project was seeded before this atm shipped a third starter.
	concierge := personaRowNamed(t, m, "concierge")
	if len(concierge.MissingStarters) != 1 || concierge.MissingStarters[0] != "setup-channels" {
		t.Fatalf("missing starters = %v", concierge.MissingStarters)
	}
	// The three drifts are distinct, and each is reported where its own fix
	// is: a stale stamp is [s] on the channel row, a missing server is [a] on
	// the agent row, a missing starter is [s] on the personas section. A
	// wizard that merged them into one "not ready" verdict would name no fix
	// at all.
	m.setup.section = setupSectionChannels
	if !strings.Contains(m.setup.actionHints(), "[s]stamp") {
		t.Fatalf("channels ladder must offer the stamp fix: %q", m.setup.actionHints())
	}
	m.setup.section = setupSectionAgents
	if !strings.Contains(m.setup.actionHints(), "[a]mcp add") {
		t.Fatalf("agents ladder must offer the mcp add fix: %q", m.setup.actionHints())
	}
	m.setup.section = setupSectionPersonas
	if !strings.Contains(m.setup.actionHints(), "[s]seed starters") {
		t.Fatalf("personas ladder must offer the seed fix: %q", m.setup.actionHints())
	}

	// And the repair actually repairs: seeding authors the one missing starter
	// and adds nothing else. The checklist count is what proves the second
	// half — three records, not five — since re-authoring the two that were
	// already there would land as duplicates rather than as a visible failure.
	m.setup.seedStarters("ATM")
	got := personaRowNamed(t, m, "concierge")
	if len(got.MissingStarters) != 0 || got.StartersSeeded != got.StartersTotal {
		t.Fatalf("after seeding: missing = %v, seeded %d/%d", got.MissingStarters, got.StartersSeeded, got.StartersTotal)
	}
	if got.Checklists != 3 {
		t.Fatalf("after seeding the concierge has %d checklists, want exactly the 3 shipped starters", got.Checklists)
	}
}
