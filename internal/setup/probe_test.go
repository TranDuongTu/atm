package setup

import (
	"errors"
	"testing"
	"time"

	"atm/internal/core"
	"atm/internal/developing"
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

// A model set on a NON-default agent is still configured and still used the
// moment that agent is selected. Showing it only for the default row made the
// wizard and `atm agents list` disagree about the same agents.json — the user
// set claude's model, saw it in the CLI, and saw an empty MODEL cell here.
func TestInstantShowsEachRowsOwnModel(t *testing.T) {
	cfg := core.AgentsConfig{
		Selected: "codex",
		Models:   map[string]string{"claude": "opus5", "ollama:claude": "glm-5.2"},
	}
	m := Instant(cfg, Probes{LookPath: lookPathWith("claude", "codex"), Home: t.TempDir()})
	for _, r := range m.Agents {
		switch r.Agent {
		case "claude":
			// Not the default, so the bare-name key is the one it was stored
			// under and the one to show.
			if r.Model != "opus5" {
				t.Fatalf("claude model = %q, want opus5 — a non-default row shows its own model", r.Model)
			}
		case "opencode":
			if r.Model != "" {
				t.Fatalf("opencode model = %q, want empty — nothing is configured for it", r.Model)
			}
		}
	}
}

// The default row prefers the launcher-qualified key, because that is what
// SetAgentModel writes once a launcher is chosen.
func TestInstantDefaultRowPrefersTheLauncherQualifiedModel(t *testing.T) {
	cfg := core.AgentsConfig{
		Selected: "ollama:claude",
		Models:   map[string]string{"claude": "opus5", "ollama:claude": "glm-5.2"},
	}
	m := Instant(cfg, Probes{LookPath: lookPathWith("ollama", "claude"), Home: t.TempDir()})
	for _, r := range m.Agents {
		if r.Agent != "claude" {
			continue
		}
		if r.Model != "glm-5.2" {
			t.Fatalf("claude model = %q, want glm-5.2", r.Model)
		}
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

// A name PluginInstallRoot does not recognize is a probe that could not
// answer — it must map to FactUnknown, not a guessed FactAbsent. This is
// the honesty bug: an earlier version of pluginFact treated everything
// except "installed" as absent, which would report "unknown" as missing.
func TestPluginFactMapsUnknownStateToUnknownFact(t *testing.T) {
	st := developing.PluginStatus("not-a-real-agent", t.TempDir())
	if st.State != "unknown" {
		t.Fatalf("PluginStatus(unrecognized agent).State = %q, want %q", st.State, "unknown")
	}
	if got := pluginFact(st.State); got != FactUnknown {
		t.Fatalf("pluginFact(%q) = %v, want FactUnknown", st.State, got)
	}
	if got := pluginFact("installed"); got != FactPresent {
		t.Fatalf("pluginFact(installed) = %v, want FactPresent", got)
	}
	// A partial install is genuinely not installed, and fixable inside ATM
	// — it must not be conflated with "unknown".
	if got := pluginFact("partial"); got != FactAbsent {
		t.Fatalf("pluginFact(partial) = %v, want FactAbsent", got)
	}
	if got := pluginFact("missing"); got != FactAbsent {
		t.Fatalf("pluginFact(missing) = %v, want FactAbsent", got)
	}
}

// Fill derives ChannelsOK/ChannelsAll from a built project without touching
// Instant's own two-argument signature (pinned by Task 2's tests).
func TestFillDerivesChannelCoverageFromProject(t *testing.T) {
	m := Instant(core.AgentsConfig{}, Probes{LookPath: lookPathWith(), Home: t.TempDir()})
	views := []core.ChannelView{
		{
			ChannelRecord: core.ChannelRecord{Name: "atm", Type: core.ChannelTypeRepo},
			Wiring:        &core.ChannelWiring{Path: "/tmp/atm"},
			Probe:         &core.ChannelProbe{PathExists: true, IsGitRepo: true},
		},
		{
			ChannelRecord: core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion},
			Wiring:        &core.ChannelWiring{MCPServer: "notion"},
		},
	}
	servers := map[string][]MCPServer{
		"claude": {{Name: "notion", Connected: FactPresent}},
	}
	states := map[string]Fact{"claude": FactPresent, "codex": FactPresent, "opencode": FactPresent}
	ps := BuildProject("ATM", views, servers, states, time.Now())
	Fill(&m, ps)
	for _, r := range m.Agents {
		if r.ChannelsAll != 2 {
			t.Fatalf("%s ChannelsAll = %d, want 2", r.Agent, r.ChannelsAll)
		}
		want := 1 // the repo channel only, for every agent except claude
		if r.Agent == "claude" {
			want = 2 // repo + notion, since claude has the notion server
		}
		if r.ChannelsOK != want {
			t.Fatalf("%s ChannelsOK = %d, want %d", r.Agent, r.ChannelsOK, want)
		}
	}
}

// No project selected must leave counts at zero, not panic or guess.
func TestFillWithNilProjectLeavesCountsZero(t *testing.T) {
	m := Instant(core.AgentsConfig{}, Probes{LookPath: lookPathWith(), Home: t.TempDir()})
	Fill(&m, nil)
	for _, r := range m.Agents {
		if r.ChannelsAll != 0 || r.ChannelsOK != 0 {
			t.Fatalf("%s counts = %d/%d, want 0/0", r.Agent, r.ChannelsOK, r.ChannelsAll)
		}
	}
}
