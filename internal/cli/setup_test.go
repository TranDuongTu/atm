package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	atmsetup "atm/internal/setup"
)

// runSetupJSON runs `setup status` in JSON mode and decodes the payload into
// a generic map, the way the brief's mustRunJSON would have.
func runSetupJSON(t *testing.T, h *testCLI, args ...string) map[string]any {
	t.Helper()
	h.output = outputJSON
	full := append([]string{"setup", "status"}, args...)
	out, stderr, code := h.run(full...)
	h.output = outputText
	if code != ExitSuccess {
		t.Fatalf("run %v: exit=%d stderr=%s", full, code, stderr)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	return v
}

func TestSetupStatusJSONShape(t *testing.T) {
	// Isolate from the invoking shell's ATM_PROJECT (set by an ATM ledger
	// session) so the no-flag case genuinely exercises "no project selected".
	t.Setenv("ATM_PROJECT", "")
	st := newTestCLI(t)
	out := runSetupJSON(t, st)
	if _, ok := out["agents"]; !ok {
		t.Fatal("missing agents")
	}
	if _, ok := out["project"]; ok {
		t.Fatal("project must be ABSENT with no --project, not null")
	}
}

func TestSetupStatusWithProjectIncludesChannels(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runSetupJSON(t, st, "--project", "ATM")
	p, ok := out["project"].(map[string]any)
	if !ok {
		t.Fatalf("project = %#v", out["project"])
	}
	if p["code"] != "ATM" {
		t.Fatalf("code = %v", p["code"])
	}
	if _, ok := p["channels"]; !ok {
		t.Fatalf("missing channels")
	}
	// The personas table went with the starter checklists it accounted for
	// (plan §7): operating checklists are profile content, and `atm
	// profile status` is their readiness surface.
	if _, ok := p["personas"]; ok {
		t.Fatalf("personas key must be gone (plan §7): %#v", p)
	}
}

func TestSetupStatusUnknownProjectIsUsageError(t *testing.T) {
	st := newTestCLI(t)
	if _, _, code := runArgs(st, "setup", "status", "--project", "NOPE"); code != ExitUsage {
		t.Fatalf("exit = %d", code)
	}
}

// TestSetupStatusTextModeRendersAgentsAndProject exercises the default
// (text) output, which the three JSON-mode tests above never touch.
// Assertions are on stable anchors — the table header and a harness name
// that agent.Harnesses() always contributes (regardless of whether that
// binary happens to be on this machine's PATH) — not on exact column
// widths, so a reflow of writeSetupText's format string doesn't turn this
// into a brittle golden file.
func TestSetupStatusTextModeRendersAgentsAndProject(t *testing.T) {
	t.Setenv("ATM_PROJECT", "")
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "setup", "status", "--project", "ATM")
	mustContain(t, out, "AGENT")
	mustContain(t, out, "GLYPH")
	mustContain(t, out, "claude")
	mustContain(t, out, "project ATM")
	// "MCP SERVER" (not "CHANNEL" alone: the agent table's own coverage
	// column is headed "CHANNELS" and would match regardless of whether the
	// project's channel table rendered at all).
	mustContain(t, out, "MCP SERVER")
	// The PERSONA table is gone with the starter checklists (plan §7).
	mustNotContain(t, out, "PERSONA\t")
}

// TestSetupStatusTextModeNoProjectOmitsProjectSection pins the early-return
// at the bottom of writeSetupText: with no project selected there is no
// "project" header, no CHANNEL table — the agent table is the whole
// picture, honestly, rather than an empty/misleading section.
func TestSetupStatusTextModeNoProjectOmitsProjectSection(t *testing.T) {
	t.Setenv("ATM_PROJECT", "")
	st := newTestCLI(t)
	out := runArgsOut(t, st, "setup", "status")
	mustContain(t, out, "AGENT")
	// Not "CHANNEL" alone: the agent table's own coverage column is headed
	// "CHANNELS" and is always present, so that substring would false-fail.
	mustNotContain(t, out, "MCP SERVER")
}

// TestUpdateArgvContractPresentAndAbsent pins setup.UpdateArgv's contract at
// the two call sites this command owns (JSON's update_argv, text's UPDATE
// column): claude/codex/opencode — the only agents setup.Instant ever rows —
// always have an update verb, so the "absent" case can only be exercised
// with a name outside that set. ollama is the correct one to use: it really
// has no update verb (installed out of band), so asserting its absence here
// is asserting a true fact, not manufacturing a test double.
func TestUpdateArgvContractPresentAndAbsent(t *testing.T) {
	present := setupAgentToJSON(atmsetup.AgentRow{Agent: "claude"})
	if len(present.UpdateArgv) == 0 || present.UpdateArgv[0] != "update" {
		t.Fatalf("claude update_argv = %v, want [update ...]", present.UpdateArgv)
	}
	absent := setupAgentToJSON(atmsetup.AgentRow{Agent: "ollama"})
	if absent.UpdateArgv != nil {
		t.Fatalf("ollama update_argv = %v, want nil (no update verb)", absent.UpdateArgv)
	}

	// omitempty must actually omit it on the wire, not just leave the Go
	// value nil in memory — that is the part a bolted-on "latest version"
	// lookup could break without any of the field-level checks above
	// noticing.
	data, err := json.Marshal(absent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "update_argv") {
		t.Fatalf("ollama JSON must omit update_argv entirely, got %s", data)
	}
	data, err = json.Marshal(present)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"update_argv":["update"]`) {
		t.Fatalf("claude JSON must carry update_argv, got %s", data)
	}

	// Text mode: the UPDATE column renders the verb for an agent that has
	// one, and a plain "-" (never "update available") for one that does not.
	var buf bytes.Buffer
	writeSetupText(&buf, atmsetup.Model{Agents: []atmsetup.AgentRow{
		{Agent: "claude"},
		{Agent: "ollama"},
	}})
	lines := strings.Split(buf.String(), "\n")
	var claudeLine, ollamaLine string
	for _, l := range lines {
		if strings.Contains(l, "claude") {
			claudeLine = l
		}
		if strings.Contains(l, "ollama") {
			ollamaLine = l
		}
	}
	if claudeLine == "" || !strings.HasSuffix(claudeLine, "update") {
		t.Fatalf("claude row = %q, want it to end with the update verb", claudeLine)
	}
	if ollamaLine == "" || !strings.HasSuffix(ollamaLine, "-") {
		t.Fatalf("ollama row = %q, want it to end with the empty-update dash", ollamaLine)
	}
	if strings.Contains(ollamaLine, "update") || strings.Contains(ollamaLine, "upgrade") {
		t.Fatalf("ollama row = %q, must not claim an update verb it doesn't have", ollamaLine)
	}
}
