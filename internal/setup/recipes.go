package setup

import (
	"time"

	"atm/internal/agent"
	"atm/internal/core"
)

// Recipe is everything an MCP adapter needs to add a server via the
// harness's own `mcp add` verb. ATM never writes a harness config file
// itself — Recipe only carries the argv-building inputs; the harness owns
// storage and any credential prompt.
type Recipe struct {
	Server    string
	Transport string
	URL       string
	NeedsAuth bool
}

// recipes is keyed by channel type — only channel types that need an MCP
// server appear here. A repo channel has no entry: it is wired by cloning a
// path, not by adding an MCP server.
var recipes = map[string]Recipe{
	core.ChannelTypeNotion: {
		Server: "notion", Transport: "http",
		URL: "https://mcp.notion.com/mcp", NeedsAuth: true,
	},
}

// RecipeFor returns the Recipe for a channel type, or false when that type
// needs no MCP server at all (e.g. repo).
func RecipeFor(channelType string) (Recipe, bool) {
	r, ok := recipes[channelType]
	return r, ok
}

// BuildProject turns a project's channel views into the wizard's picture of
// them, filling PerAgent by the count rule: a repo channel is machine-scoped
// (wired identically for every agent, so it never consults states), while a
// notion channel counts for an agent only when that agent's own `mcp list`
// reports the channel's server as connected. An agent whose mcp probe could
// not answer (states[agent] == FactUnknown) gets FactUnknown on every notion
// channel — never a guessed miss.
func BuildProject(code string, views []core.ChannelView, servers map[string][]MCPServer, states map[string]Fact, now time.Time) *ProjectSetup {
	ps := &ProjectSetup{Code: code}
	agents := agent.Harnesses()
	for _, v := range views {
		glyph, note := core.ChannelStatus(v, now)
		row := ChannelRow{
			Name:     v.Name,
			Type:     v.Type,
			Glyph:    glyph,
			Note:     note,
			PerAgent: make(map[string]Fact, len(agents)),
		}
		// The recipe names the server a fresh `mcp add` would use; the
		// channel's own wiring.mcp_server — set once a concierge session or
		// a hand edit has actually recorded one — wins when the two
		// disagree, because that is the name this machine's harnesses were
		// actually configured against.
		if r, ok := RecipeFor(v.Type); ok {
			row.MCPServer = r.Server
		}
		if v.Wiring != nil && v.Wiring.MCPServer != "" {
			row.MCPServer = v.Wiring.MCPServer
		}
		// A repo channel counts only when it is actually reachable on this
		// machine. core.ChannelStatus buckets both "unwired" and "path
		// missing" under the same not-ready glyph "○" — so coverage must
		// track that same boundary: Wiring == nil is never present, and
		// neither is a wired path that no longer exists. Everything past
		// that (dirty, not-a-git-repo, ahead/behind, a stale verification
		// stamp) is a ◐-grade health nuance, not a reachability failure —
		// the channel is still there, so it still counts. A nil Probe means
		// there was nothing to probe (not a negative answer), so it counts.
		wired := v.Wiring != nil && (v.Probe == nil || v.Probe.PathExists)
		for _, h := range agents {
			switch v.Type {
			case core.ChannelTypeRepo:
				if wired {
					row.PerAgent[h.Name] = FactPresent
				} else {
					row.PerAgent[h.Name] = FactAbsent
				}
			default:
				row.PerAgent[h.Name] = notionCoverage(row.MCPServer, servers[h.Name], states[h.Name])
			}
		}
		ps.Channels = append(ps.Channels, row)
	}
	return ps
}

// notionCoverage applies the count rule for a single agent on a single
// notion channel. An agent whose mcp probe could not answer is unknown,
// regardless of what its (necessarily empty) server list holds — the
// distinction between "asked and got nothing" and "never asked" must
// survive here.
//
// Within a probe that DID answer there are three outcomes, not two, and the
// third is not optional:
//
//	configured and connected      -> present
//	configured, health unknown    -> UNKNOWN
//	not configured at all         -> absent
//
// The middle case is codex, every time: codexMCPAdapter.ParseList sets
// Connected: FactUnknown for every server because `codex mcp list --json`
// reports configuration without health. Folding that into absent made the
// cell say "codex is missing the notion server" about a codex that HAS it —
// the cardinal rule broken in the exact place it was written for, and worse
// than reporting a real gap, because it sends the user to repair a setup that
// was never broken. A server the harness reports as ✗ is genuinely not
// reachable, so that one stays absent: it is a known negative, and `[l]` is
// its fix.
func notionCoverage(mcpServer string, agentServers []MCPServer, state Fact) Fact {
	if state == FactUnknown {
		return FactUnknown
	}
	for _, s := range agentServers {
		if s.Name != mcpServer {
			continue
		}
		// A harness that explicitly reports the server as not working (an
		// opencode ✗, a codex `enabled: false`) is a known negative, and `[l]`
		// or `[a]` is its fix.
		if s.Connected == FactAbsent {
			return FactAbsent
		}
		// Otherwise the server IS configured, and that is what coverage asks:
		// the rule is that the agent's `mcp list` CONTAINS the channel's
		// server. Health is a separate, finer fact — claude runs a real check
		// and codex runs none — and gating the count on it made a codex the
		// user had authorized read 1/2 forever, because codex reports no
		// health for an OAuth server and never can. The detail pane still
		// shows the health honestly, including "unknown".
		return FactPresent
	}
	return FactAbsent
}
