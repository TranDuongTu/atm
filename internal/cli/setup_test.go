package cli

import (
	"encoding/json"
	"testing"
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

func TestSetupStatusWithProjectIncludesChannelsAndPersonas(t *testing.T) {
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
	for _, k := range []string{"channels", "personas"} {
		if _, ok := p[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
}

func TestSetupStatusUnknownProjectIsUsageError(t *testing.T) {
	st := newTestCLI(t)
	if _, _, code := runArgs(st, "setup", "status", "--project", "NOPE"); code != ExitUsage {
		t.Fatalf("exit = %d", code)
	}
}
