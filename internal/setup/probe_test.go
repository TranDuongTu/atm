package setup

import (
	"errors"
	"testing"

	"atm/internal/core"
)

func lookPathWith(present ...string) func(string) (string, error) {
	set := map[string]bool{}
	for _, p := range present {
		set[p] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestInstantMarksMissingBinaries(t *testing.T) {
	m := Instant(core.AgentsConfig{}, Probes{LookPath: lookPathWith("claude"), Home: t.TempDir()})
	byName := map[string]AgentRow{}
	for _, r := range m.Agents {
		byName[r.Agent] = r
	}
	if byName["claude"].Binary != FactPresent {
		t.Fatalf("claude binary = %v", byName["claude"].Binary)
	}
	if byName["codex"].Binary != FactAbsent {
		t.Fatalf("codex binary = %v", byName["codex"].Binary)
	}
}

// ollama on PATH is ONE global fact, not one per agent. This is the whole
// point of the reshape: three rows must not encode it three times.
func TestInstantOllamaIsOneGlobalFact(t *testing.T) {
	m := Instant(core.AgentsConfig{}, Probes{LookPath: lookPathWith("ollama", "claude"), Home: t.TempDir()})
	if m.Ollama != FactPresent {
		t.Fatalf("ollama = %v", m.Ollama)
	}
	for _, r := range m.Agents {
		if r.OllamaOK != FactPresent {
			t.Fatalf("%s OllamaOK = %v; the ollama cell follows the global fact", r.Agent, r.OllamaOK)
		}
	}
}

// The default is a CELL: which agent AND which launcher.
func TestInstantDefaultIsACell(t *testing.T) {
	cfg := core.AgentsConfig{Selected: "ollama:claude", Models: map[string]string{"ollama:claude": "glm-5.2"}}
	m := Instant(cfg, Probes{LookPath: lookPathWith("ollama", "claude"), Home: t.TempDir()})
	for _, r := range m.Agents {
		if r.Agent != "claude" {
			if r.IsDefault {
				t.Fatalf("%s must not be default", r.Agent)
			}
			continue
		}
		if !r.IsDefault || r.DefaultVia != "ollama" || r.Model != "glm-5.2" {
			t.Fatalf("claude row = %+v", r)
		}
	}
}

// A hand-edited agents.json naming an agent that does not exist must not
// drop the row or panic — the user has to be able to SEE what to fix.
func TestInstantSurvivesUnknownSelection(t *testing.T) {
	cfg := core.AgentsConfig{Selected: "pi"}
	m := Instant(cfg, Probes{LookPath: lookPathWith(), Home: t.TempDir()})
	if len(m.Agents) != 3 {
		t.Fatalf("expected 3 agent rows, got %d", len(m.Agents))
	}
}

// Version is an async fact. Tier 1 must leave it blank, not guess.
func TestInstantLeavesVersionsBlank(t *testing.T) {
	m := Instant(core.AgentsConfig{}, Probes{LookPath: lookPathWith("claude"), Home: t.TempDir()})
	for _, r := range m.Agents {
		if r.Version != "" {
			t.Fatalf("%s version = %q; versions belong to the async tier", r.Agent, r.Version)
		}
	}
}
