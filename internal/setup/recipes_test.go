package setup

import (
	"testing"
	"time"

	"atm/internal/core"
)

func TestRepoChannelCountsForEveryAgent(t *testing.T) {
	views := []core.ChannelView{{
		ChannelRecord: core.ChannelRecord{Name: "atm", Type: core.ChannelTypeRepo},
		Wiring:        &core.ChannelWiring{Path: "/tmp/atm"},
		Probe:         &core.ChannelProbe{PathExists: true, IsGitRepo: true},
	}}
	ps := BuildProject("ATM", views, map[string][]MCPServer{}, map[string]Fact{}, time.Now())
	row := ps.Channels[0]
	for _, ag := range []string{"claude", "codex", "opencode"} {
		if row.PerAgent[ag] != FactPresent {
			t.Fatalf("%s: a wired repo channel is machine-scoped and counts for every agent", ag)
		}
	}
}

// core.ChannelStatus buckets "unwired" and "path missing" under the same
// not-ready glyph "○" — a wired path that no longer exists on this machine
// is not reachable, so it must not count toward coverage either, even
// though Wiring != nil.
func TestRepoChannelWithMissingPathCountsForNoAgent(t *testing.T) {
	views := []core.ChannelView{{
		ChannelRecord: core.ChannelRecord{Name: "atm", Type: core.ChannelTypeRepo},
		Wiring:        &core.ChannelWiring{Path: "/tmp/gone"},
		Probe:         &core.ChannelProbe{PathExists: false},
	}}
	ps := BuildProject("ATM", views, map[string][]MCPServer{}, map[string]Fact{}, time.Now())
	row := ps.Channels[0]
	for _, ag := range []string{"claude", "codex", "opencode"} {
		if row.PerAgent[ag] != FactAbsent {
			t.Fatalf("%s: a wired repo channel whose path is missing is not reachable, want FactAbsent, got %v", ag, row.PerAgent[ag])
		}
	}
}

func TestNotionChannelCountsOnlyForAgentsWithTheServer(t *testing.T) {
	views := []core.ChannelView{{
		ChannelRecord: core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion},
		Wiring:        &core.ChannelWiring{MCPServer: "notion"},
	}}
	servers := map[string][]MCPServer{
		"claude": {{Name: "notion", Connected: FactPresent}},
		"codex":  {},
	}
	states := map[string]Fact{"claude": FactPresent, "codex": FactPresent, "opencode": FactUnknown}
	ps := BuildProject("ATM", views, servers, states, time.Now())
	row := ps.Channels[0]
	if row.PerAgent["claude"] != FactPresent {
		t.Fatalf("claude = %v", row.PerAgent["claude"])
	}
	if row.PerAgent["codex"] != FactAbsent {
		t.Fatalf("codex = %v", row.PerAgent["codex"])
	}
	// opencode's probe could not answer, so we do not know — and must not guess.
	if row.PerAgent["opencode"] != FactUnknown {
		t.Fatalf("opencode = %v, want unknown", row.PerAgent["opencode"])
	}
}

// A harness that reports its servers but not their health — codex, always,
// because `codex mcp list --json` carries no connection state — must not have
// "configured, cannot tell" folded into "not configured". The three outcomes
// are distinct, and only the third is a reason to go fix something.
// The count rule is "the agent's mcp list CONTAINS the channel's server".
// Health is a finer, separate fact: claude runs a real connection check,
// codex runs none and reports none for an OAuth server. Gating the COUNT on
// health made a codex the user had just authorized read 1/2 forever, with no
// action available that could ever change it.
func TestConfiguredServerCountsEvenWhenHealthIsUnreported(t *testing.T) {
	views := []core.ChannelView{{
		ChannelRecord: core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion},
		Wiring:        &core.ChannelWiring{MCPServer: "notion"},
	}}
	servers := map[string][]MCPServer{
		"codex":    {{Name: "notion", Connected: FactUnknown}},
		"claude":   {{Name: "notion", Connected: FactPresent}},
		"opencode": {},
	}
	states := map[string]Fact{"claude": FactPresent, "codex": FactPresent, "opencode": FactPresent}
	m := Model{Agents: []AgentRow{{Agent: "claude"}, {Agent: "codex"}, {Agent: "opencode"}}}
	ps := BuildProject("ATM", views, servers, states, time.Now())
	if got := ps.Channels[0].PerAgent["codex"]; got != FactPresent {
		t.Fatalf("codex = %v, want present — it HAS the server; codex simply reports no health", got)
	}
	Fill(&m, ps)
	for _, r := range m.Agents {
		want := 1
		if r.Agent == "opencode" {
			want = 0
		}
		if r.ChannelsOK != want {
			t.Fatalf("%s coverage = %d/%d, want %d/1", r.Agent, r.ChannelsOK, r.ChannelsAll, want)
		}
	}
}

func TestConfiguredServerWithUnknownHealthIsUnknownNotAbsent(t *testing.T) {
	views := []core.ChannelView{{
		ChannelRecord: core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion},
		Wiring:        &core.ChannelWiring{MCPServer: "notion"},
	}}
	servers := map[string][]MCPServer{
		// codex's own adapter shape: named, health unreported.
		"codex": {{Name: "notion", Connected: FactUnknown}},
		// claude's ✗ shape: the harness said outright that it is down. That is
		// a known negative, and it stays absent — [l] is its fix.
		"claude": {{Name: "notion", Connected: FactAbsent}},
		// nothing configured at all.
		"opencode": {},
	}
	states := map[string]Fact{"claude": FactPresent, "codex": FactPresent, "opencode": FactPresent}
	m := Model{Agents: []AgentRow{{Agent: "claude"}, {Agent: "codex"}, {Agent: "opencode"}}}
	ps := BuildProject("ATM", views, servers, states, time.Now())
	row := ps.Channels[0]
	if row.PerAgent["claude"] != FactAbsent {
		t.Fatalf("claude = %v, want absent — the harness reported it as not connected", row.PerAgent["claude"])
	}
	if row.PerAgent["opencode"] != FactAbsent {
		t.Fatalf("opencode = %v, want absent — nothing configured", row.PerAgent["opencode"])
	}
	// An explicit negative is the ONLY thing that stops a configured server
	// counting, so only the two absent agents miss the channel.
	Fill(&m, ps)
	for _, r := range m.Agents {
		want := 0
		if r.Agent == "codex" {
			want = 1
		}
		if r.ChannelsOK != want || r.ChannelsAll != 1 {
			t.Fatalf("%s coverage = %d/%d, want %d/1", r.Agent, r.ChannelsOK, r.ChannelsAll, want)
		}
	}
}

func TestChannelGlyphComesFromCoreNotFromHere(t *testing.T) {
	views := []core.ChannelView{{
		ChannelRecord: core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion},
		Wiring:        nil, // unwired
	}}
	ps := BuildProject("ATM", views, nil, nil, time.Now())
	wantGlyph, wantNote := core.ChannelStatus(views[0], time.Now())
	if ps.Channels[0].Glyph != wantGlyph || ps.Channels[0].Note != wantNote {
		t.Fatalf("glyph/note = %q/%q, want %q/%q — single-source it",
			ps.Channels[0].Glyph, ps.Channels[0].Note, wantGlyph, wantNote)
	}
}

// An unwired notion channel still names the server a fresh `mcp add` would
// use, so the wizard can tell the user what it would configure.
func TestChannelRowMCPServerFallsBackToRecipeWhenUnwired(t *testing.T) {
	views := []core.ChannelView{{
		ChannelRecord: core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion},
	}}
	ps := BuildProject("ATM", views, nil, nil, time.Now())
	if ps.Channels[0].MCPServer != "notion" {
		t.Fatalf("MCPServer = %q, want the recipe default %q", ps.Channels[0].MCPServer, "notion")
	}
}

// The channel's own wiring.mcp_server wins over Recipe.Server when the two
// disagree — coverage must be checked against the name this machine was
// actually configured with, not the recipe's default.
func TestWiringMCPServerNameWinsOverRecipeDefault(t *testing.T) {
	views := []core.ChannelView{{
		ChannelRecord: core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion},
		Wiring:        &core.ChannelWiring{MCPServer: "custom-notion"},
	}}
	servers := map[string][]MCPServer{
		"claude": {{Name: "notion", Connected: FactPresent}},        // recipe default name — must NOT count
		"codex":  {{Name: "custom-notion", Connected: FactPresent}}, // the wired name — must count
	}
	states := map[string]Fact{"claude": FactPresent, "codex": FactPresent, "opencode": FactPresent}
	ps := BuildProject("ATM", views, servers, states, time.Now())
	row := ps.Channels[0]
	if row.MCPServer != "custom-notion" {
		t.Fatalf("MCPServer = %q, want wiring's %q", row.MCPServer, "custom-notion")
	}
	if row.PerAgent["claude"] != FactAbsent {
		t.Fatalf("claude = %v, want FactAbsent (has the recipe default name, not the wired one)", row.PerAgent["claude"])
	}
	if row.PerAgent["codex"] != FactPresent {
		t.Fatalf("codex = %v, want FactPresent (has the wired name)", row.PerAgent["codex"])
	}
}

func TestRecipeForNotion(t *testing.T) {
	r, ok := RecipeFor(core.ChannelTypeNotion)
	if !ok || r.Server != "notion" || r.Transport != "http" || !r.NeedsAuth {
		t.Fatalf("recipe = %+v ok=%v", r, ok)
	}
	if _, ok := RecipeFor(core.ChannelTypeRepo); ok {
		t.Fatal("repo channels need no MCP server")
	}
}
