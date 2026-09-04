package cli

import (
	"strings"
	"testing"
)

// statusCLI applies the demo profile (a coder persona, a work checklist
// requiring #design, a design channel) and selects claude.
func statusCLI(t *testing.T) *testCLI {
	t.Helper()
	st := applyCLI(t)
	installDemo(t, st, "1.0.0")
	runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--name", "demo", "--actor", "admin@cli:unset")
	runArgsOut(t, st, "agents", "select", "claude")
	return st
}

func TestProfileStatusWalksTheLadderWithNextCommands(t *testing.T) {
	st := statusCLI(t)
	out := runArgsOut(t, st, "profile", "status", "--project", "DEMO")
	mustContain(t, out, "demo@1.0.0\t3 in sync, 0 modified, 0 missing")
	mustContain(t, out, "configured: claude")
	mustContain(t, out, "no endpoint yet")
	mustContain(t, out, "readiness for claude")
	mustContain(t, out, "work\tcoder\tapplied\tchannel design has no endpoint → atm channel endpoint add --project DEMO --name design")
	mustContain(t, out, "ready: no — 1 of 1 action(s) below attested for claude")

	runArgsOut(t, st, "channel", "endpoint", "add", "--project", "DEMO", "--name", "design", "--type", "notion", "--page", "p1", "--actor", "admin@cli:unset")
	out = runArgsOut(t, st, "profile", "status", "--project", "DEMO")
	mustContain(t, out, "design\tnotion\thome\tyes\tno\t—")
	mustContain(t, out, "work\tcoder\taddressed\tchannel design: notion endpoint is not wired on this machine → atm channel wire --project DEMO --name design --type notion --mcp-server")

	runArgsOut(t, st, "channel", "wire", "--project", "DEMO", "--name", "design", "--type", "notion", "--mcp-server", "notion", "--actor", "admin@cli:unset")
	out = runArgsOut(t, st, "profile", "status", "--project", "DEMO")
	mustContain(t, out, "design\tnotion\thome\tyes\tmcp:notion\t—")
	mustContain(t, out, "work\tcoder\twired\tchannel design: notion endpoint has never been reached by claude → atm profile verify --project DEMO --agent claude")

	runArgsOut(t, st, "channel", "stamp", "--project", "DEMO", "--name", "design", "--type", "notion", "--kind", "probe", "--note", "attest: saw the page", "--actor", "manager@claude:unit")
	out = runArgsOut(t, st, "profile", "status", "--project", "DEMO")
	mustContain(t, out, "design\tnotion\thome\tyes\tmcp:notion\tprobe 0d")
	mustContain(t, out, "work\tcoder\tattested")
	mustContain(t, out, "ready: yes — every action attested for claude")
}

func TestProfileStatusIsAgentRelative(t *testing.T) {
	st := statusCLI(t)
	runArgsOut(t, st, "channel", "endpoint", "add", "--project", "DEMO", "--name", "design", "--type", "notion", "--page", "p1", "--actor", "admin@cli:unset")
	runArgsOut(t, st, "channel", "wire", "--project", "DEMO", "--name", "design", "--type", "notion", "--mcp-server", "notion", "--actor", "admin@cli:unset")
	runArgsOut(t, st, "channel", "stamp", "--project", "DEMO", "--name", "design", "--type", "notion", "--note", "used it", "--actor", "developer@claude:unit")
	// codex is configured by having stored args.
	runArgsOut(t, st, "agents", "args", "codex", "--", "--yolo")

	out := runArgsOut(t, st, "profile", "status", "--project", "DEMO")
	mustContain(t, out, "configured: claude, codex")
	mustContain(t, out, "design\tnotion\thome\tyes\tmcp:notion\tuse 0d\t—")
	mustContain(t, out, "ready: yes — every action attested for claude")
	mustContain(t, out, "ready: no — 1 of 1 action(s) below attested for codex")

	out = runArgsOut(t, st, "profile", "status", "--project", "DEMO", "--agent", "codex")
	mustNotContain(t, out, "readiness for claude")
	mustContain(t, out, "never been reached by codex")
}

func TestProfileStatusStrictExitsBelowTheRung(t *testing.T) {
	st := statusCLI(t)
	if _, _, code := runArgs(st, "profile", "status", "--project", "DEMO", "--strict", "applied"); code != ExitSuccess {
		t.Fatalf("applied is reached, yet --strict applied exited %d", code)
	}
	stdout, stderr, code := runArgs(st, "profile", "status", "--project", "DEMO", "--strict", "addressed")
	if code == ExitSuccess {
		t.Fatal("--strict addressed passed with an unaddressed channel")
	}
	mustContain(t, stdout+stderr, "below addressed")
	msg, code := runChecklistErrText(t, st, "profile", "status", "--project", "DEMO", "--strict", "bogus")
	if code == ExitSuccess || !strings.Contains(msg, "valid, applied, addressed, wired, attested") {
		t.Fatalf("bogus rung: %d %q", code, msg)
	}
}

func TestProfileStatusWithoutAgentsSaysSo(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "profile", "status", "--project", "DEMO")
	mustContain(t, out, "no agent configured — atm agents select")
	mustContain(t, out, "nothing can attest until an agent is configured")
	mustContain(t, out, "demo@dev\tnot installed here\t3 record(s) unverifiable")
}

func TestProfileStatusJSONCarriesTheComputation(t *testing.T) {
	st := statusCLI(t)
	st.output = outputJSON
	out := runArgsOut(t, st, "profile", "status", "--project", "DEMO")
	for _, key := range []string{`"ref": "demo@1.0.0"`, `"in_sync": 3`, `"actions"`, `"rung"`, `"selected_agents"`, `"ready"`} {
		mustContain(t, out, key)
	}
}

// An ollama-launched session attests as "ollama" — sessionActor composes
// persona@LAUNCHER:model — so its selection key fills the ollama column,
// never the harness column. --agent accepts a selection key and dispatches
// it verbatim; ATM_AGENT (the launcher name inside a session) is skipped.
func TestProfileStatusKeysAttestationByAttestingSegment(t *testing.T) {
	st := statusCLI(t)
	runArgsOut(t, st, "channel", "endpoint", "add", "--project", "DEMO", "--name", "design", "--type", "notion", "--page", "p1", "--actor", "admin@cli:unset")
	runArgsOut(t, st, "channel", "wire", "--project", "DEMO", "--name", "design", "--type", "notion", "--mcp-server", "notion", "--actor", "admin@cli:unset")
	// The ollama launcher's stamp reads "ollama", not "claude".
	runArgsOut(t, st, "channel", "stamp", "--project", "DEMO", "--name", "design", "--type", "notion", "--note", "used it", "--actor", "developer@ollama:unit")
	// The ollama launcher is configured by having stored args.
	runArgsOut(t, st, "agents", "args", "ollama:claude", "--", "--model", "glm-5.2")

	out := runArgsOut(t, st, "profile", "status", "--project", "DEMO")
	mustContain(t, out, "configured: claude, ollama")
	mustContain(t, out, "design\tnotion\thome\tyes\tmcp:notion\t—\tuse 0d")
	mustContain(t, out, "ready: yes — every action attested for ollama")

	out = runArgsOut(t, st, "profile", "status", "--project", "DEMO", "--agent", "ollama:claude")
	mustContain(t, out, "readiness for ollama")
	mustNotContain(t, out, "readiness for claude")
}

func TestProfileVerifySelectionKeyIsTheLaunchAxis(t *testing.T) {
	st := statusCLI(t)
	runArgsOut(t, st, "checklist", "add", "--project", "DEMO", "--name", "attest", "--step", "Reach every endpoint read-only.", "--suits", "manager", "--actor", "admin@cli:unset")

	// A launcher-prefixed key is a valid dispatch target.
	out := runArgsOut(t, st, "profile", "verify", "--project", "DEMO", "--agent", "ollama:claude", "--dry-run")
	mustContain(t, out, "would dispatch attest as manager on ollama:claude for DEMO")
	// Without --agent, verify reads the STORED selection, not ATM_AGENT:
	// inside an ollama-launched session that env names the launcher.
	t.Setenv("ATM_AGENT", "ollama")
	out = runArgsOut(t, st, "profile", "verify", "--project", "DEMO", "--dry-run")
	mustContain(t, out, "would dispatch attest as manager on claude for DEMO")
	// An unknown name still fails loudly.
	if _, _, code := runArgs(st, "profile", "verify", "--project", "DEMO", "--agent", "bogus", "--dry-run"); code == ExitSuccess {
		t.Fatal("bogus agent dispatch succeeded")
	}
}

func TestProfileVerifyDispatchesAttestPerAgent(t *testing.T) {
	st := statusCLI(t)
	// The demo profile ships no attest; verify says how to get one.
	msg, code := runChecklistErrText(t, st, "profile", "verify", "--project", "DEMO")
	if code == ExitSuccess || !strings.Contains(msg, "no attest checklist") {
		t.Fatalf("%d %q", code, msg)
	}
	runArgsOut(t, st, "checklist", "add", "--project", "DEMO", "--name", "attest", "--step", "Reach every endpoint read-only.", "--suits", "manager", "--actor", "admin@cli:unset")
	runArgsOut(t, st, "agents", "args", "codex", "--", "--yolo")

	out := runArgsOut(t, st, "profile", "verify", "--project", "DEMO", "--all-agents", "--dry-run")
	mustContain(t, out, "would dispatch attest as manager on claude for DEMO")
	mustContain(t, out, "would dispatch attest as manager on codex for DEMO")

	var launches []string
	st.st.runChildFn = func(name string, argv []string, env []string, notFoundHint string) (int, error) {
		launches = append(launches, name+" "+strings.Join(env, " "))
		return 0, nil
	}
	st.st.lookPathFn = func(string) (string, error) { return "/fake/atm", nil }
	out = runArgsOut(t, st, "profile", "verify", "--project", "DEMO", "--agent", "codex")
	mustContain(t, out, "verifying DEMO on codex")
	if len(launches) != 1 || !strings.HasPrefix(launches[0], "codex ") || !strings.Contains(launches[0], "ATM_CHECKLISTS=attest") || !strings.Contains(launches[0], "ATM_PERSONA=manager") {
		t.Fatalf("launches = %v", launches)
	}
	launches = nil
	runArgsOut(t, st, "profile", "verify", "--project", "DEMO", "--all-agents")
	if len(launches) != 2 {
		t.Fatalf("--all-agents launched %d sessions: %v", len(launches), launches)
	}
}
