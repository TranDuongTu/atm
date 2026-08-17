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
			Plugin:   FactAbsent,
		}
		if developing.PluginStatus(h.Plugin, p.Home).State == "installed" {
			row.Plugin = FactPresent
		}
		if selErr == nil && sel.Agent == h.Name {
			row.IsDefault = true
			row.DefaultVia = string(sel.Launcher)
			row.Model = cfg.Models[sel.Key()]
		}
		m.Agents = append(m.Agents, row)
	}
	return m
}
