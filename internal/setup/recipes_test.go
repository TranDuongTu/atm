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
