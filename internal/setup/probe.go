package setup

import (
	"context"
	"time"

	"atm/internal/agent"
	"atm/internal/core"
	"atm/internal/developing"
)

// RunFunc runs a command and returns stdout. Injected so every probe is
// testable without a real binary.
type RunFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// Probes are the only ways this package may touch the outside world.
type Probes struct {
	LookPath func(string) (string, error)
	Run      RunFunc
	Home     string
	Now      func() time.Time
}

func (p Probes) now() time.Time {
	if p.Now == nil {
		return time.Now()
	}
	return p.Now()
}

func (p Probes) on(binary string) Fact {
	if p.LookPath == nil {
		return FactUnknown
	}
	if _, err := p.LookPath(binary); err != nil {
		return FactAbsent
	}
	return FactPresent
}

// pluginFact translates developing.PluginStatus's four states into a Fact.
// "unknown" means PluginInstallRoot did not recognize the agent — the probe
// could not answer, so it maps to FactUnknown, never a guessed miss. A
// "partial" install is genuinely not installed (and fixable inside ATM), so
// it joins "missing" as FactAbsent.
func pluginFact(state string) Fact {
	switch state {
	case "installed":
		return FactPresent
	case "unknown":
		return FactUnknown
	default: // "partial", "missing"
		return FactAbsent
	}
}

// Instant computes every fact reachable without a subprocess: PATH lookups,
// plugin files on disk, and the stored selection. Versions and MCP servers
// are deliberately absent — they cost 1.6-3s each and belong to the async
// tier.
func Instant(cfg core.AgentsConfig, p Probes) Model {
	m := Model{Ollama: p.on("ollama"), ProbedAt: p.now()}
	sel, selErr := agent.ParseSelectionKey(cfg.Selected)
	for _, h := range agent.Harnesses() {
		row := AgentRow{
			Agent:    h.Name,
			Binary:   p.on(h.Name),
			NativeOK: p.on(h.Name),
			OllamaOK: m.Ollama,
		}
		row.Plugin = pluginFact(developing.PluginStatus(h.Plugin, p.Home).State)
		// Every row shows ITS OWN model, not just the default's. A model set
		// on a non-default agent is still configured and still used the moment
		// that agent is selected, so hiding it made `atm agents list` and this
		// view disagree about the same agents.json. The bare agent name is the
		// key `SetAgentModel` writes for a non-default row; the default row
		// upgrades to the launcher-qualified key when one is stored there.
		row.Model = cfg.Models[h.Name]
		if selErr == nil && sel.Agent == h.Name {
			row.IsDefault = true
			row.DefaultVia = string(sel.Launcher)
			if m := cfg.Models[sel.Key()]; m != "" {
				row.Model = m
			}
		}
		m.Agents = append(m.Agents, row)
	}
	return m
}

// Fill derives each AgentRow's ChannelsOK/ChannelsAll from a built project.
// It is a separate pass rather than an Instant parameter because a
// project's channels require store reads and (for notion coverage) async
// mcp probes that Instant's cheap, subprocess-free contract must not carry.
// A nil project leaves every row's counts at zero — the wizard is honestly
// global when no project is selected.
func Fill(m *Model, ps *ProjectSetup) {
	if ps == nil {
		return
	}
	for i := range m.Agents {
		row := &m.Agents[i]
		row.ChannelsAll = len(ps.Channels)
		row.ChannelsOK = 0
		for _, ch := range ps.Channels {
			if ch.PerAgent[row.Agent] == FactPresent {
				row.ChannelsOK++
			}
		}
	}
}
