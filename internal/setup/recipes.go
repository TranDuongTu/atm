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
		// "wired" is exactly what core.ChannelStatus calls wired: Wiring !=
		// nil. Finer health (dirty, stale, path missing) is already carried
		// by the row's own glyph/note — the per-agent count is a coarser
		// "is this channel configured on this machine" question, not a
		// re-judgment of its health.
		wired := v.Wiring != nil
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
func notionCoverage(mcpServer string, agentServers []MCPServer, state Fact) Fact {
	if state == FactUnknown {
		return FactUnknown
	}
	for _, s := range agentServers {
		if s.Name == mcpServer && s.Connected == FactPresent {
			return FactPresent
		}
	}
	return FactAbsent
}
