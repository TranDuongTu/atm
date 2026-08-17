package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// listAgentRows runs `agents list` in JSON mode and returns the rows.
func listAgentRows(t *testing.T, h *goldenHarness) []map[string]any {
	t.Helper()
	stdout, stderr, code := h.run("agents", "list")
	if code != ExitSuccess {
		t.Fatalf("agents list exit=%d stderr=%s", code, stderr)
	}
	var out struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("agents list json: %v\n%s", err, stdout)
	}
	if len(out.Agents) == 0 {
		t.Fatalf("no agent rows in %s", stdout)
	}
	return out.Agents
}

func TestAgentsSelectThenList(t *testing.T) {
	h := newGoldenHarness(t)

	// select an entry
	if _, _, code := h.run("agents", "select", "opencode"); code != ExitSuccess {
		t.Fatalf("agents select exit=%d", code)
	}
	cfg, err := h.store.GetAgentsConfig()
	if err != nil || cfg.Selected != "opencode" {
		t.Fatalf("selected not persisted: %q %v", cfg.Selected, err)
	}

	// unknown name errors
	if _, _, code := h.run("agents", "select", "gemini"); code == ExitSuccess {
		t.Fatal("expected non-zero exit selecting unknown agent")
	}

	// list mentions the selected entry
	stdout, _, code := h.run("agents", "list")
	if code != ExitSuccess {
		t.Fatalf("agents list exit=%d", code)
	}
	if !strings.Contains(stdout, "opencode") {
		t.Fatalf("list output missing opencode: %s", stdout)
	}
}

func TestAgentsArgsGetSet(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("agents", "args", "codex", "--", "--foo", "--bar"); code != ExitSuccess {
		t.Fatalf("set args exit=%d", code)
	}
	cfg, _ := h.store.GetAgentsConfig()
	if got := cfg.Args["codex"]; len(got) != 2 || got[0] != "--foo" || got[1] != "--bar" {
		t.Fatalf("args not stored: %v", got)
	}
}

// The model belongs to a selection KEY, not to an agent: ollama:claude
// carrying a model must leave native claude on the harness's own default.
func TestAgentsListJSONCarriesModelPerSelectionKey(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("agents", "select", "ollama:claude", "--model", "glm-5.2"); code != ExitSuccess {
		t.Fatalf("agents select --model exit=%d", code)
	}
	var found bool
	for _, r := range listAgentRows(t, h) {
		if r["name"] == "ollama:claude" {
			found = true
			if r["model"] != "glm-5.2" {
				t.Fatalf("model = %v", r["model"])
			}
			if r["selected"] != true {
				t.Fatalf("selected = %v", r["selected"])
			}
			if r["launcher"] != "ollama" {
				t.Fatalf("launcher = %v", r["launcher"])
			}
		}
		if r["name"] == "claude" && r["model"] == "glm-5.2" {
			t.Fatal("native claude must not inherit the ollama model: models are per selection key")
		}
	}
	if !found {
		t.Fatal("ollama:claude row missing")
	}
}

func TestAgentsSelectModelIsOptional(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("agents", "select", "codex"); code != ExitSuccess {
		t.Fatalf("agents select exit=%d", code)
	}
	for _, r := range listAgentRows(t, h) {
		if r["name"] == "codex" && r["model"] != nil && r["model"] != "" {
			t.Fatalf("model = %v, want empty", r["model"])
		}
	}
}

// --model "" is an instruction to clear, not a silent no-op.
func TestAgentsSelectEmptyModelClearsIt(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("agents", "select", "codex", "--model", "gpt-5-codex")
	if got := mustAgentModel(t, h, "codex"); got != "gpt-5-codex" {
		t.Fatalf("model = %q", got)
	}
	h.run("agents", "select", "codex", "--model", "")
	if got := mustAgentModel(t, h, "codex"); got != "" {
		t.Fatalf("model = %q, want cleared", got)
	}
}

func mustAgentModel(t *testing.T, h *goldenHarness, key string) string {
	t.Helper()
	cfg, err := h.store.GetAgentsConfig()
	if err != nil {
		t.Fatalf("GetAgentsConfig: %v", err)
	}
	return cfg.Models[key]
}

func TestAgentsSelectRejectsUnknownAgent(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("agents", "select", "pi"); code != ExitUsage {
		t.Fatalf("exit = %d, want ExitUsage", code)
	}
}

func TestAgentsModelsUnknownAgentIsUsageError(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("agents", "models", "pi"); code != ExitUsage {
		t.Fatalf("exit = %d, want ExitUsage", code)
	}
}

// claude has no list verb. That is a clear, actionable message — not a crash
// and not an empty list, which would read as "claude has no models".
func TestAgentsModelsNoListerExplainsItself(t *testing.T) {
	h := newGoldenHarness(t)
	_, stderr, code := h.run("agents", "models", "claude")
	if code == ExitSuccess {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, "manually") {
		t.Fatalf("stderr = %q, want the manual-entry hint", stderr)
	}
}

// Text mode is a row per AGENT with a column per LAUNCHER: whether ollama is
// installed is one global fact, so it must not be repeated as three rows.
func TestAgentsListTextIsAnAgentByLauncherTable(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	h.run("agents", "select", "ollama:claude", "--model", "glm-5.2")
	stdout, stderr, code := h.run("agents", "list")
	if code != ExitSuccess {
		t.Fatalf("agents list exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "AGENT") || !strings.Contains(stdout, "NATIVE") || !strings.Contains(stdout, "OLLAMA") {
		t.Fatalf("missing table header:\n%s", stdout)
	}
	if strings.Contains(stdout, "ollama:claude") {
		t.Fatalf("ollama is a launcher column, not an agent row:\n%s", stdout)
	}
	var claudeRow string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "claude") {
			claudeRow = line
		}
	}
	if claudeRow == "" {
		t.Fatalf("no claude row:\n%s", stdout)
	}
	if !strings.Contains(claudeRow, "glm-5.2") || !strings.Contains(claudeRow, "*") {
		t.Fatalf("claude row must mark the selected ollama cell and its model: %q", claudeRow)
	}
	// The ollama binary is reported once, globally.
	if strings.Count(stdout, "ollama:") > 1 {
		t.Fatalf("ollama readiness repeated per row:\n%s", stdout)
	}
}
