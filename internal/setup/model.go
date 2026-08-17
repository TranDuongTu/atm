// Package setup computes ATM's setup readiness — agents, their plugins and
// MCP servers, a project's channels, and its checklists — from injected
// probes. It renders nothing and spawns nothing itself.
package setup

import "time"

// Fact is tri-state because a probe that could not answer must not be
// reported as a negative. Unknown is a first-class outcome.
type Fact int

const (
	FactUnknown Fact = iota
	FactPresent
	FactAbsent
)

func (f Fact) String() string {
	switch f {
	case FactPresent:
		return "present"
	case FactAbsent:
		return "absent"
	default:
		return "unknown"
	}
}

// AgentRow is one harness: its facts, both launcher cells, its model, and
// its coverage of the selected project's channels.
type AgentRow struct {
	Agent       string
	Version     string // "" until the async tier lands, or when unknowable
	Binary      Fact
	Plugin      Fact
	NativeOK    Fact
	OllamaOK    Fact
	Model       string
	IsDefault   bool
	DefaultVia  string // "native" | "ollama" — the default is a CELL, not a row
	MCPServers  []string
	MCPState    Fact // unknown when the probe could not answer
	ChannelsOK  int
	ChannelsAll int
}

// Glyph grades by WHO CAN FIX IT, not by how many facts are wrong:
// ● ready · ◐ fixable right here · ○ the fix is outside ATM.
func (r AgentRow) Glyph() string {
	switch {
	case r.Binary == FactAbsent:
		return "○"
	case r.Binary == FactPresent && r.Plugin == FactPresent:
		return "●"
	default:
		return "◐"
	}
}

type ChannelRow struct {
	Name      string
	Type      string
	Glyph     string // from core.ChannelStatus — never recomputed here
	Note      string
	MCPServer string
	PerAgent  map[string]Fact // agent name -> has this channel's server
}

type PersonaRow struct {
	Persona         string
	Checklists      int
	Steps           int
	StartersSeeded  int
	StartersTotal   int
	MissingStarters []string
	Customised      []string // informational: a seeded starter is MEANT to be edited
}

type ProjectSetup struct {
	Code                string
	Channels            []ChannelRow
	Personas            []PersonaRow
	ChecklistCapEnabled bool
}

// Model is the whole picture. Project is nil when no project is selected —
// the wizard is honestly global at that moment, so the project sections are
// absent rather than empty.
type Model struct {
	ATMVersion string
	Ollama     Fact
	Agents     []AgentRow
	Project    *ProjectSetup
	ProbedAt   time.Time
}
