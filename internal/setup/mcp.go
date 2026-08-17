package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// MCPServer is one MCP connection reported by a harness's own `mcp list`.
// ATM never stores this itself — it is read fresh from the harness each
// time, because the harness (not ATM) owns the credential and the
// connection.
type MCPServer struct {
	Name      string
	Connected Fact
}

// MCPAdapter translates between ATM's Recipe/MCPServer model and one
// harness's `mcp` verb. ATM never parses or writes a harness config file —
// every read and write goes through the harness's own CLI.
type MCPAdapter interface {
	// ListArgv is the argv (after the harness binary) that lists configured
	// MCP servers.
	ListArgv() []string
	// ParseList turns that command's stdout into servers. A non-nil error
	// means the output could not be understood — it is NOT a report of zero
	// servers, so callers must not treat it as an empty list.
	ParseList([]byte) ([]MCPServer, error)
	// AddArgv is the argv that adds a server from a Recipe.
	AddArgv(r Recipe) []string
	// LoginArgv is the argv that starts this harness's own auth flow for an
	// already-added server. ATM never sees or stores the credential.
	LoginArgv(server string) []string
}

var errParseFailed = errors.New("mcp list: could not parse output")

// mcpEmptyCatalogBanner is the phrase claude and opencode BOTH print when they
// have no MCP server configured at all:
//
//	claude:   No MCP servers configured. Use `claude mcp add` to add a server.
//	opencode: ▲  No MCP servers configured
//
// It exists because "I could not tell" and "the harness told me: zero" are
// different facts, and the row-parsers cannot tell them apart on their own —
// neither harness prints a parseable row for an empty catalog, so the
// format-change rule below would report a clean, explicit answer as unknown.
// That is not a cosmetic misgrade: mcpTarget refuses every MCP write while the
// state is unknown, so `[a] mcp add` — the rung that ENDS the bootstrap — used
// to dead-end on a machine with no servers configured, which is precisely the
// machine the wizard exists for.
//
// The match is deliberately narrow (this exact phrase, and only when no row
// parsed) rather than a heuristic over "no"/"none"/"empty": if a harness ever
// rewords it, this stops matching and the answer falls back to unknown, which
// is the safe direction to fail in.
const mcpEmptyCatalogBanner = "No MCP servers configured"

// saysEmptyCatalog reports whether output is a harness stating outright that it
// has no servers, as opposed to output this parser merely failed to read.
func saysEmptyCatalog(out []byte) bool {
	return bytes.Contains(out, []byte(mcpEmptyCatalogBanner))
}

// MCPAdapterFor returns the adapter for a harness name, or false if no
// adapter is registered for it.
func MCPAdapterFor(agent string) (MCPAdapter, bool) {
	switch agent {
	case "claude":
		return claudeMCPAdapter{}, true
	case "codex":
		return codexMCPAdapter{}, true
	case "opencode":
		return opencodeMCPAdapter{}, true
	default:
		return nil, false
	}
}

// ProbeMCP runs a harness's `mcp list` and parses it. Every failure mode —
// a missing adapter, a run error (including timeout), or a parse error —
// returns (nil, FactUnknown). Only a clean parse returns FactPresent, even
// when the resulting list is empty: an empty list is a fact about the
// harness, not about ATM's ability to ask the question.
func ProbeMCP(ctx context.Context, agent string, run RunFunc) ([]MCPServer, Fact) {
	a, ok := MCPAdapterFor(agent)
	if !ok {
		return nil, FactUnknown
	}
	argv := a.ListArgv()
	out, err := run(ctx, agent, argv...)
	if err != nil {
		return nil, FactUnknown
	}
	servers, err := a.ParseList(out)
	if err != nil {
		return nil, FactUnknown
	}
	return servers, FactPresent
}

// claudeMCPAdapter speaks `claude mcp`. `claude mcp list` prints a health
// banner, a blank line, then one line per server: "<name>: <url> [(transport)]
// - <glyph> <status>". Names may contain spaces (e.g. "claude.ai Google
// Drive"), so the name/rest split is on the FIRST ": ", not a field split.
type claudeMCPAdapter struct{}

func (claudeMCPAdapter) ListArgv() []string { return []string{"mcp", "list"} }

func (claudeMCPAdapter) ParseList(out []byte) ([]MCPServer, error) {
	var servers []MCPServer
	matched := 0
	for _, line := range strings.Split(string(out), "\n") {
		// Lines without ": " are the health banner or blank separators, not
		// server entries — skip them rather than mis-split on them.
		idx := strings.Index(line, ": ")
		if idx < 0 {
			continue
		}
		matched++
		name := line[:idx]
		rest := line[idx+2:]
		connected := FactAbsent
		if strings.HasSuffix(rest, "Connected") {
			connected = FactPresent
		}
		servers = append(servers, MCPServer{Name: name, Connected: connected})
	}
	// Zero matched lines against non-empty output means the format changed,
	// not that there are no servers — that distinction is the whole point
	// of returning an error here instead of an empty slice. The one
	// exception is the harness saying so itself (see mcpEmptyCatalogBanner),
	// which is an answer, not a failure to read one.
	if matched == 0 && len(bytes.TrimSpace(out)) > 0 && !saysEmptyCatalog(out) {
		return nil, errParseFailed
	}
	return servers, nil
}

func (claudeMCPAdapter) AddArgv(r Recipe) []string {
	return []string{"mcp", "add", "--transport", r.Transport, r.Server, r.URL}
}

func (claudeMCPAdapter) LoginArgv(server string) []string {
	return []string{"mcp", "login", server}
}

// codexMCPAdapter speaks `codex mcp`. `codex mcp list --json` prints a JSON
// array of server objects (or `[]` for an empty store), so parsing is a
// plain unmarshal — a JSON error is unambiguously a parse error.
type codexMCPAdapter struct{}

func (codexMCPAdapter) ListArgv() []string { return []string{"mcp", "list", "--json"} }

func (codexMCPAdapter) ParseList(out []byte) ([]MCPServer, error) {
	var entries []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, err
	}
	servers := make([]MCPServer, len(entries))
	for i, e := range entries {
		// codex's --json list doesn't report per-server connection health,
		// only configuration — unknown, not a guess either way.
		servers[i] = MCPServer{Name: e.Name, Connected: FactUnknown}
	}
	return servers, nil
}

func (codexMCPAdapter) AddArgv(r Recipe) []string {
	return []string{"mcp", "add", r.Server, "--url", r.URL}
}

func (codexMCPAdapter) LoginArgv(server string) []string {
	return []string{"mcp", "login", server}
}

// opencodeMCPAdapter speaks `opencode mcp`. `opencode mcp list` prints a
// box-drawing table; each server line carries a ✓/✗ glyph followed by the
// server name, e.g. "●  ✓ notion connected".
type opencodeMCPAdapter struct{}

func (opencodeMCPAdapter) ListArgv() []string { return []string{"mcp", "list"} }

func (opencodeMCPAdapter) ParseList(out []byte) ([]MCPServer, error) {
	var servers []MCPServer
	matched := 0
	for _, line := range strings.Split(string(out), "\n") {
		glyph := ""
		if strings.Contains(line, "✓") {
			glyph = "✓"
		} else if strings.Contains(line, "✗") {
			glyph = "✗"
		} else {
			continue
		}
		fields := strings.Fields(line)
		name := ""
		for i, f := range fields {
			if f == glyph && i+1 < len(fields) {
				name = fields[i+1]
				break
			}
		}
		if name == "" {
			continue
		}
		matched++
		// The glyph, not the trailing word, decides the fact: "disconnected"
		// contains "connected" as a substring, so text-matching on that word
		// would report a down server as up.
		connected := FactAbsent
		if glyph == "✓" {
			connected = FactPresent
		}
		servers = append(servers, MCPServer{Name: name, Connected: connected})
	}
	// Same rule as the claude adapter, and the same single exception: non-empty
	// output with zero recognized server lines is a format change, UNLESS the
	// harness said outright that there is nothing to list.
	if matched == 0 && len(bytes.TrimSpace(out)) > 0 && !saysEmptyCatalog(out) {
		return nil, errParseFailed
	}
	return servers, nil
}

func (opencodeMCPAdapter) AddArgv(r Recipe) []string {
	return []string{"mcp", "add", r.Server, "--url", r.URL}
}

func (opencodeMCPAdapter) LoginArgv(server string) []string {
	return []string{"mcp", "auth", server}
}
