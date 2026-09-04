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
// here as channelRowNamed.

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
// atm that shipped fewer starters than this one does. With the starter
// checklists pruned (plan §7) there are no shipped starters left, so the
// repair narrative uses hand-authored checklist records instead: the drift it
// stands in for (a checklist this atm no longer accounts for) is expressed by
// the records the store actually holds.
func seedOneChecklist(t *testing.T, m *Model, code, name string) {
	t.Helper()
	rec := core.ChecklistRecord{
		Name:    name,
		Purpose: "hand-authored operating checklist",
		Steps:   []core.ChecklistStep{{Text: "one step"}},
	}
	if _, err := m.store.CreateChecklist(code, rec, testActor); err != nil {
		t.Fatalf("CreateChecklist %s: %v", name, err)
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
	// Every fix on the screen is DIRECT (plan §7 took the only SPAWNED
	// action, the concierge session, with it), so the bootstrap paradox is
	// gone too: nothing on the wizard waits for a ready agent.
	m.setup.installPlugin("claude")
	if got := agentRow(t, m, "claude").Glyph(); got != "●" {
		t.Fatalf("after install glyph = %q, want ●", got)
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

// Narrative 3: repair. Two distinct drifts side by side, each with its fix
// beside it. The third drift this narrative used to carry — a missing starter
// checklist — went with the starter checklists themselves (plan §7):
// operating checklists are profile content now, reported by `atm profile
// status`, and the wizard has nothing left to say about them.
func TestNarrativeRepair(t *testing.T) {
	m := setupActionsModel(t)
	seedActionProject(t, m, "ATM")
	mustAddNotionChannel(t, m, "ATM", "specs", "notion")
	stampChannelDaysAgo(t, m, "ATM", "specs", 61)
	seedOneChecklist(t, m, "ATM", "planning")
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
	// The two drifts are distinct, and each is reported where its own fix
	// is: a stale stamp is [s] on the channel row, a missing server is [a]
	// on the agent row. A wizard that merged them into one "not ready"
	// verdict would name no fix at all.
	m.setup.section = setupSectionChannels
	if !strings.Contains(m.setup.agentActionHints(), "[s]stamp") {
		t.Fatalf("channels ladder must offer the stamp fix: %q", m.setup.agentActionHints())
	}
	m.setup.section = setupSectionAgents
	if !strings.Contains(m.setup.agentActionHints(), "[a]mcp add") {
		t.Fatalf("agents ladder must offer the mcp add fix: %q", m.setup.agentActionHints())
	}
}
