package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

type capturedChild struct {
	name string
	argv []string
	env  []string
}

// captureChild wires a stub runChildFn that records the agent invocation and
// returns success (exit 0). Tests assert the captured argv/env.
func captureChild(h *goldenHarness) *capturedChild {
	var c capturedChild
	h.st.runChildFn = func(name string, argv []string, env []string, notFoundHint string) (int, error) {
		c.name = name
		c.argv = append([]string(nil), argv...)
		c.env = append([]string(nil), env...)
		return 0, nil
	}
	return &c
}

// stubLookPath makes the `atm` PATH guard pass without putting the real binary
// on PATH.
func stubLookPath(h *goldenHarness) {
	h.st.lookPathFn = func(string) (string, error) { return "/fake/atm", nil }
}

// gotToSlice flattens an env map to "k=v" strings for contains-based assertions.
func gotToSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// TestPersonaFlagDefaultsToTUI verifies that `atm` with no --persona launches
// the TUI as admin@tui:unset.
func TestPersonaFlagDefaultsToTUI(t *testing.T) {
	h := newGoldenHarness(t)
	var gotStore, gotActor string
	h.st.runTUI = func(storePath, actor string) error {
		gotStore = storePath
		gotActor = actor
		return nil
	}
	_, _, code := h.run()
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if gotStore != h.store.StorePath() {
		t.Fatalf("tui store = %q, want %q", gotStore, h.store.StorePath())
	}
	if gotActor != "admin@tui:unset" {
		t.Fatalf("tui actor = %q, want admin@tui:unset", gotActor)
	}
}

// TestPersonaAdminLaunchesTUI verifies `atm --persona admin` also goes to TUI.
func TestPersonaAdminLaunchesTUI(t *testing.T) {
	h := newGoldenHarness(t)
	var gotActor string
	h.st.runTUI = func(storePath, actor string) error {
		gotActor = actor
		return nil
	}
	_, _, code := h.run("--persona", "admin")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if gotActor != "admin@tui:unset" {
		t.Fatalf("tui actor = %q, want admin@tui:unset", gotActor)
	}
}

// TestPersonaAdminWithArgsFails verifies TUI mode rejects positional args.
func TestPersonaAdminWithArgsFails(t *testing.T) {
	h := newGoldenHarness(t)
	h.st.runTUI = func(storePath, actor string) error { return nil }
	_, _, code := h.run("--persona", "admin", "extra")
	if code == ExitSuccess {
		t.Fatal("expected non-zero exit when TUI mode gets positional args")
	}
}

// TestPersonaDeveloperLaunchesHookStyle verifies the developer persona (launch:
// hook) launches the agent bare (no initial prompt message) and sets
// ATM_ROLE=developing for back-compat with installed session-start hooks.
func TestPersonaDeveloperLaunchesHookStyle(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	c := captureChild(h)
	stubLookPath(h)
	h.reset()

	_, _, code := h.run("--persona", "developer", "--project", "ATM", "--agent", "claude")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	if c.name != "claude" {
		t.Fatalf("child name = %q, want claude", c.name)
	}
	if want := []string{"claude"}; !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv = %v, want %v (developer is launch:hook — bare)", c.argv, want)
	}
	joined := strings.Join(c.env, "\n")
	for _, want := range []string{
		"ATM_ROLE=developing",
		"ATM_PERSONA=developer",
		"ATM_AGENT=claude",
		"ATM_ACTOR=developer@claude:unset",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("developer env missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "session-developer.md") {
		t.Errorf("ATM_CONTEXT_FILE should end with session-developer.md:\n%s", joined)
	}
	if strings.Contains(joined, "ATM_MODE=") {
		t.Errorf("developer declares no modes; ATM_MODE must not be set:\n%s", joined)
	}
}

// TestPersonaManagerLaunch verifies the manager persona launches with a
// prompt message pointing at the context file and no mode block (manager
// declares no modes).
func TestPersonaManagerLaunch(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	c := captureChild(h)
	stubLookPath(h)
	h.reset()

	_, _, code := h.run("--persona", "manager", "--project", "ATM", "--agent", "claude")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	if c.name != "claude" {
		t.Fatalf("child name = %q, want claude", c.name)
	}
	if c.argv[0] != "claude" {
		t.Fatalf("argv[0] = %q, want claude", c.argv[0])
	}
	if !strings.Contains(strings.Join(c.argv[1:], " "), "Read the session instructions") {
		t.Fatalf("argv[1] should contain the prompt message; got %v", c.argv)
	}
	joined := strings.Join(c.env, "\n")
	for _, want := range []string{
		"ATM_ROLE=manager",
		"ATM_PERSONA=manager",
		"ATM_ACTOR=manager@claude:unset",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("manager env missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "ATM_MODE=") {
		t.Errorf("manager declares no modes; ATM_MODE must not be set:\n%s", joined)
	}
	if !strings.Contains(joined, "session-manager.md") {
		t.Errorf("ATM_CONTEXT_FILE should end with session-manager.md:\n%s", joined)
	}
	got := normalizeSessionOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "session-manager-launch", got)

	// The rendered context file on disk carries no mode section.
	ctxPath := filepath.Join(h.store.StorePath(), "projects", "ATM", "cache", "session-manager.md")
	body, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("read context file %s: %v", ctxPath, err)
	}
	if strings.Contains(string(body), "## Mode:") {
		t.Fatalf("manager context file must not contain a mode block:\n%s", body)
	}
}

// TestModeValidation verifies mode validation: personas that declare no modes
// (manager, developer) reject --mode.
func TestModeValidation(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	stubLookPath(h)
	h.reset()

	// manager --mode nope → manager declares no modes.
	_, stderr, code := h.run("--persona", "manager", "--project", "ATM", "--agent", "claude", "--mode", "nope")
	if code == ExitSuccess {
		t.Fatal("expected non-zero exit for --mode on a no-modes persona")
	}
	if !strings.Contains(stderr, "declares no modes") {
		t.Errorf("no-modes error missing 'declares no modes':\n%s", stderr)
	}

	// developer --mode brief → developer declares no modes.
	_, stderr, code = h.run("--persona", "developer", "--project", "ATM", "--agent", "claude", "--mode", "brief")
	if code == ExitSuccess {
		t.Fatal("expected non-zero exit for --mode on a no-modes persona")
	}
	if !strings.Contains(stderr, "declares no modes") {
		t.Errorf("no-modes error missing 'declares no modes':\n%s", stderr)
	}
}

// TestCapabilityScopeValidation verifies --capability validates against the
// full registry first (typo → registered list), then the enabled set.
func TestCapabilityScopeValidation(t *testing.T) {
	h := newGoldenHarness(t)
	// Create with only workflow enabled so contextmap is known-but-disabled.
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management",
		"--capabilities", "workflow", "--actor", "admin@cli:unset")
	stubLookPath(h)
	captureChild(h)
	h.reset()

	// unknown capability → registered list.
	_, stderr, code := h.run("--persona", "manager", "--project", "ATM", "--agent", "claude", "--capability", "nope")
	if code == ExitSuccess {
		t.Fatal("expected non-zero exit for unknown capability")
	}
	if !strings.Contains(stderr, "unknown capability") || !strings.Contains(stderr, "registered:") {
		t.Errorf("unknown-capability error should list registered:\n%s", stderr)
	}

	// Known-but-disabled capability (contextmap is registered but not enabled).
	_, stderr, code = h.run("--persona", "manager", "--project", "ATM", "--agent", "claude", "--capability", "contextmap")
	if code == ExitSuccess {
		t.Fatal("expected non-zero exit for not-enabled capability")
	}
	if !strings.Contains(stderr, "not enabled for project") {
		t.Errorf("not-enabled error missing 'not enabled for project':\n%s", stderr)
	}

	// Enabled capability succeeds.
	_, _, code = h.run("--persona", "manager", "--project", "ATM", "--agent", "claude", "--capability", "workflow")
	if code != ExitSuccess {
		t.Fatalf("enabled capability should succeed; exit=%d stderr=%s", code, h.stderr.String())
	}
}

// TestProjectRequiredUnlessOptional verifies --project is required for personas
// that do not declare project_optional.
func TestProjectRequiredUnlessOptional(t *testing.T) {
	h := newGoldenHarness(t)
	stubLookPath(h)
	h.reset()

	_, stderr, code := h.run("--persona", "manager", "--agent", "claude")
	if code == ExitSuccess {
		t.Fatal("expected non-zero exit when --project is missing for manager")
	}
	if !strings.Contains(stderr, "--project is required") {
		t.Errorf("error missing '--project is required':\n%s", stderr)
	}

	// Concierge is project_optional: a launch without --project succeeds, the
	// child receives the prompt message, and the context file lands under the
	// store-level cache dir.
	h.reset()
	c := captureChild(h)
	if _, _, code := h.run("--persona", "concierge", "--agent", "claude"); code != ExitSuccess {
		t.Fatalf("concierge launch exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	if c.name != "claude" {
		t.Fatalf("child name = %q, want claude", c.name)
	}
	if !strings.Contains(strings.Join(c.argv[1:], " "), "Read the session instructions") {
		t.Fatalf("concierge argv should carry the prompt message; got %v", c.argv)
	}
	joined := strings.Join(c.env, "\n")
	if !strings.Contains(joined, "ATM_PERSONA=concierge") {
		t.Errorf("concierge env missing ATM_PERSONA=concierge:\n%s", joined)
	}
	if !strings.Contains(joined, "ATM_CONTEXT_FILE=") {
		t.Errorf("concierge env missing ATM_CONTEXT_FILE:\n%s", joined)
	}
	ctxPath := filepath.Join(h.store.StorePath(), "cache", "session-concierge.md")
	if _, err := os.Stat(ctxPath); err != nil {
		t.Fatalf("concierge context file not created at %s: %v", ctxPath, err)
	}
}

// TestUnknownPersonaFails verifies an unregistered persona name errors out.
func TestUnknownPersonaFails(t *testing.T) {
	h := newGoldenHarness(t)
	stubLookPath(h)
	h.reset()

	_, stderr, code := h.run("--persona", "ghost", "--project", "ATM", "--agent", "claude")
	if code == ExitSuccess {
		t.Fatal("expected non-zero exit for unknown persona")
	}
	if !strings.Contains(stderr, "ghost") {
		t.Errorf("unknown-persona error should name the persona:\n%s", stderr)
	}
}

// TestDevAndManageAreGone verifies the old dev/manage subcommands are no longer
// mounted (unknown command).
func TestDevAndManageAreGone(t *testing.T) {
	h := newGoldenHarness(t)
	for _, args := range [][]string{
		{"dev", "--project", "FOO"},
		{"manage", "--project", "FOO"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should exit non-zero (dev/manage removed)", args)
		}
	}
}

// TestSessionContextRendersManager verifies session-context renders the manager
// persona prompt, and that manage-context is an alias that prints the same.
func TestSessionContextRendersManager(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	h.reset()
	h.output = outputText

	_, _, code := h.run("session-context", "--persona", "manager", "--project", "ATM")
	if code != ExitSuccess {
		t.Fatalf("session-context exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	scOut := h.stdout.String()
	for _, want := range []string{"## Persona: manager"} {
		if !strings.Contains(scOut, want) {
			t.Errorf("session-context output missing %q", want)
		}
	}

	h.reset()
	_, _, code = h.run("manage-context", "--project", "ATM")
	if code != ExitSuccess {
		t.Fatalf("manage-context exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	mcOut := h.stdout.String()
	if !strings.Contains(mcOut, "## Persona: manager") {
		t.Errorf("manage-context output missing `## Persona: manager`:\n%s", mcOut)
	}
}

// TestSessionContextHiddenFromRootHelp verifies session-context is hidden.
func TestSessionContextHiddenFromRootHelp(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("--help")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(out, "session-context") {
		t.Fatalf("session-context should be hidden from root help:\n%s", out)
	}
}

// TestSessionLaunchAutoCreatesProject mirrors the old auto-create coverage.
func TestSessionLaunchAutoCreatesProject(t *testing.T) {
	h := newGoldenHarness(t)
	captureChild(h)
	stubLookPath(h)
	_, _, code := h.run("--persona", "developer", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	p, err := h.store.GetProject("FOO")
	if err != nil {
		t.Fatalf("auto-created project missing: %v", err)
	}
	if p.Name != "FOO" {
		t.Fatalf("project name = %q, want FOO", p.Name)
	}
}

// TestSessionPATHGuard verifies the launch refuses when `atm` is not on PATH.
func TestSessionPATHGuard(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()
	_, stderr, code := h.run("--persona", "developer", "--agent", "codex", "--project", "FOO")
	if code == ExitSuccess {
		t.Fatal("expected non-zero exit when atm is not on PATH")
	}
	if !strings.Contains(stderr, "atm is not on PATH") {
		t.Fatalf("expected 'atm is not on PATH' in stderr; got:\n%s", stderr)
	}
}

// TestSessionWriteIfDiffNoOp verifies a second launch of the same tuple is a
// no-op on the context file (mtime unchanged).
func TestSessionWriteIfDiffNoOp(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	stubLookPath(h)
	h.reset()

	if _, _, code := h.run("--persona", "developer", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("first launch exit=%d stderr=%s", code, h.stderr.String())
	}
	path := filepath.Join(h.store.StorePath(), "projects", "FOO", "cache", "session-developer.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("context file not created at %s: %v", path, err)
	}
	prev := info.ModTime()

	time.Sleep(15 * time.Millisecond)

	if _, _, code := h.run("--persona", "developer", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("second launch exit=%d stderr=%s", code, h.stderr.String())
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("context file disappeared: %v", err)
	}
	if !info.ModTime().Equal(prev) {
		t.Fatalf("context file mtime changed on second launch; write-if-diff should be a no-op")
	}
}

// TestSessionDeveloperExtraArgs verifies extra args after `--` pass through.
func TestSessionDeveloperExtraArgs(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	stubLookPath(h)
	h.reset()

	_, _, code := h.run("--persona", "developer", "--agent", "codex", "--project", "FOO", "--", "--yolo", "--auto")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := []string{"codex", "--yolo", "--auto"}
	if !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
}

// TestSessionOllamaLaunch verifies the ollama launcher produces the right argv.
func TestSessionOllamaLaunch(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	stubLookPath(h)
	h.reset()

	_, _, code := h.run("--persona", "developer", "--agent", "ollama:codex", "--project", "FOO", "--", "--yolo")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := []string{"ollama", "launch", "codex", "--", "--yolo"}
	if !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
}

// TestSessionCodexEnvArgs verifies ATM_<AGENT>_ARGS env args pass through.
func TestSessionCodexEnvArgs(t *testing.T) {
	h := newGoldenHarness(t)
	prev := os.Getenv("ATM_CODEX_ARGS")
	os.Setenv("ATM_CODEX_ARGS", "--yolo")
	t.Cleanup(func() { os.Setenv("ATM_CODEX_ARGS", prev) })
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	stubLookPath(h)
	h.reset()

	_, _, code := h.run("--persona", "developer", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := []string{"codex", "--yolo"}
	if !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
}

// TestSessionLauncherNotFound verifies the launcher-not-found path: `atm` is on
// PATH (so the PATH guard passes) but the host agent is not.
func TestSessionLauncherNotFound(t *testing.T) {
	atmDir := t.TempDir()
	atmBin := filepath.Join(atmDir, "atm")
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH; cannot build atm for PATH guard")
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", atmBin, "./cmd/atm")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/atm: %v\n%s", err, out)
	}
	t.Setenv("PATH", atmDir)
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.reset()
	_, stderrStr, code := h.run("--persona", "developer", "--agent", "codex", "--project", "FOO")
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d", code, ExitGeneric)
	}
	got := normalizeSessionOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "session-developer-launcher-not-found", got)
}

// TestSessionEnvIncludesATMValues verifies sessionEnvValues builds the right
// env map (no ATM_MANAGER_ACTION / ATM_MANAGER_CAPABILITY).
func TestSessionEnvIncludesATMValues(t *testing.T) {
	os.Unsetenv("ATM_BIN")
	got := assembleEnv(sessionEnvValues("FOO", "developer@codex:unset", "FOO-RUNID", "/tmp/context.md", "codex", "developer", "developing", "", "", "", "2026-07-19T00:00:00Z"))
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"ATM_ROLE=developing",
		"ATM_PROJECT=FOO",
		"ATM_ACTOR=developer@codex:unset",
		"ATM_RUN_ID=FOO-RUNID",
		"ATM_TIMESTAMP=2026-07-19T00:00:00Z",
		"ATM_CONTEXT_FILE=/tmp/context.md",
		"ATM_AGENT=codex",
		"ATM_PERSONA=developer",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("session env missing %q", want)
		}
	}
	for _, gone := range []string{"ATM_BIN=", "ATM_MANAGER_ACTION=", "ATM_MANAGER_CAPABILITY="} {
		if strings.Contains(joined, gone) {
			t.Errorf("session env must not set %s; got:\n%s", gone, joined)
		}
	}
}

// TestSessionManagerEnvSetsCapability verifies manager env carries
// ATM_CAPABILITY (when set) and never the old manager names. Manager
// declares no modes, so ATM_MODE is never set in a manager launch.
func TestSessionManagerEnvSetsCapability(t *testing.T) {
	got := sessionEnvValues("FOO", "manager@opencode:unset", "FOO-RUNID", "/tmp/ctx.md", "opencode", "manager", "manager", "", "", "", "2026-07-19T00:00:00Z")
	joined := strings.Join(gotToSlice(got), "\n")
	for _, want := range []string{
		"ATM_PERSONA=manager",
		"ATM_ROLE=manager",
		"ATM_TIMESTAMP=2026-07-19T00:00:00Z",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("manager env missing %q; got:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "ATM_MODE=") {
		t.Errorf("manager declares no modes; ATM_MODE must not be set; got:\n%s", joined)
	}
	if strings.Contains(joined, "ATM_BIN=") || strings.Contains(joined, "ATM_MANAGER_ACTION=") || strings.Contains(joined, "ATM_MANAGER_CAPABILITY=") {
		t.Errorf("manager env must not set ATM_BIN/ATM_MANAGER_ACTION/ATM_MANAGER_CAPABILITY; got:\n%s", joined)
	}
	// ATM_CAPABILITY is omitted when empty.
	if strings.Contains(joined, "ATM_CAPABILITY=") {
		t.Errorf("ATM_CAPABILITY must be omitted when empty; got:\n%s", joined)
	}
}

// TestSessionLaunchesSelectedAgent verifies no --agent falls back to the stored
// selection.
func TestSessionLaunchesSelectedAgent(t *testing.T) {
	h := newGoldenHarness(t)
	captureChild(h)
	stubLookPath(h)

	if _, _, code := h.run("--persona", "developer", "--project", "FOO"); code == ExitSuccess {
		t.Fatal("expected non-zero exit with no agent selected")
	}
	if _, _, code := h.run("agents", "select", "opencode"); code != ExitSuccess {
		t.Fatalf("select exit=%d", code)
	}
	h.reset()
	out, _, code := h.run("--persona", "developer", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("launch exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"agent": "opencode"`) {
		t.Fatalf("launch did not resolve selected agent: %s", out)
	}
}

// TestSessionAgentFlagOverridesSelected verifies --agent overrides the stored
// selection.
func TestSessionAgentFlagOverridesSelected(t *testing.T) {
	h := newGoldenHarness(t)
	c := captureChild(h)
	stubLookPath(h)
	h.run("agents", "select", "opencode")
	h.reset()
	if _, _, code := h.run("--persona", "developer", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("launch exit=%d", code)
	}
	if c.name != "codex" {
		t.Fatalf("expected --agent override to launch codex, got %q", c.name)
	}
}

// TestSessionCustomPersonaLaunch verifies a custom persona stored via
// `atm persona create` launches with the right ATM_PERSONA/ATM_ACTOR.
func TestSessionCustomPersonaLaunch(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("persona", "create", "--name", "staff", "--prompt", "high bar", "--actor", "admin@cli:unset")
	captureChild(h)
	stubLookPath(h)
	h.reset()

	out, _, code := h.run("--persona", "staff", "--agent", "claude", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	for _, want := range []string{
		`"ATM_PERSONA": "staff"`,
		`"ATM_AGENT": "claude"`,
		`"ATM_ACTOR": "staff@claude:unset"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("custom persona launch env missing %q:\n%s", want, out)
		}
	}
}

// TestSessionTailSummaryJSON verifies the emitLaunchTail JSON shape.
func TestSessionTailSummaryJSON(t *testing.T) {
	st := &cliState{flags: globalFlags{output: outputJSON}}
	buf := new(bytes.Buffer)
	st.out = buf
	if err := emitLaunchTail(st, "developer", "FOO", "FOO-RUNID", "/STORE/projects/FOO/cache/session-developer.md", "codex", 0); err != nil {
		t.Fatalf("emitLaunchTail: %v", err)
	}
	got := normalizeSessionOutput(buf.String(), "")
	compareGolden(t, "session-developer-tail-summary", got)
}

// TestSessionTaskAssignment verifies --task validates the task, exports
// ATM_TASK, keys the context cache on the task, and renders the assignment.
func TestSessionTaskAssignment(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	out, _, code := h.run("task", "create", "--project", "ATM", "--title", "dispatch work", "--actor", "admin@cli:unset", "--output", "json")
	if code != ExitSuccess {
		t.Fatalf("task create failed: %d", code)
	}
	m := regexp.MustCompile(`"id":\s*"(ATM-[0-9a-f]+)"`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no task id in: %s", out)
	}
	taskID := m[1]
	c := captureChild(h)
	stubLookPath(h)
	h.reset()

	_, _, code = h.run("--persona", "developer", "--project", "ATM", "--agent", "claude", "--task", taskID)
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	joined := strings.Join(c.env, "\n")
	if !strings.Contains(joined, "ATM_TASK="+taskID) {
		t.Errorf("env missing ATM_TASK=%s:\n%s", taskID, joined)
	}
	re := regexp.MustCompile(`ATM_CONTEXT_FILE=(\S+)`)
	cm := re.FindStringSubmatch(joined)
	if cm == nil {
		t.Fatalf("no ATM_CONTEXT_FILE in env:\n%s", joined)
	}
	if !strings.Contains(cm[1], "session-developer-atm-") {
		t.Errorf("context cache key must include task: %s", cm[1])
	}
	b, err := os.ReadFile(cm[1])
	if err != nil {
		t.Fatalf("read context: %v", err)
	}
	if !strings.Contains(string(b), "## Assigned task") || !strings.Contains(string(b), taskID) {
		t.Errorf("context missing assignment block:\n%s", b)
	}
}

// TestSessionTaskValidation verifies bad --task values fail before launch.
func TestSessionTaskValidation(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "A", "--actor", "admin@cli:unset")
	h.run("project", "create", "--code", "OTH", "--name", "B", "--actor", "admin@cli:unset")
	out, _, _ := h.run("task", "create", "--project", "OTH", "--title", "other", "--actor", "admin@cli:unset", "--output", "json")
	m := regexp.MustCompile(`"id":\s*"(OTH-[0-9a-f]+)"`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no task id in: %s", out)
	}
	captureChild(h)
	stubLookPath(h)
	h.reset()

	if _, _, code := h.run("--persona", "developer", "--project", "ATM", "--agent", "claude", "--task", "ATM-ffffff"); code == ExitSuccess {
		t.Error("missing task must fail")
	}
	h.reset()
	if _, _, code := h.run("--persona", "developer", "--project", "ATM", "--agent", "claude", "--task", m[1]); code == ExitSuccess {
		t.Error("task from another project must fail")
	}
	h.reset()
	if _, _, code := h.run("--persona", "concierge", "--agent", "claude", "--task", "ATM-ffffff"); code == ExitSuccess {
		t.Error("--task without --project must fail")
	}
}

// normalizeSessionOutput rewrites the test harness's volatile bits to stable
// tokens so golden fixtures are byte-stable across processes: the store path
// prefix collapses to /STORE, the run id (CODE-YYYYMMDDHHMMSS-6hex) to
// FOO-RUNID, and the timestamp to TIMESTAMP.
func normalizeSessionOutput(s, storePath string) string {
	s = normalizeOutput(s)
	if storePath != "" {
		s = strings.ReplaceAll(s, filepath.ToSlash(storePath), "/STORE")
	}
	runIDRe := regexp.MustCompile(`[A-Z]+-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	timestampRe := regexp.MustCompile(`"ATM_TIMESTAMP": "[^"]+"`)
	s = timestampRe.ReplaceAllString(s, `"ATM_TIMESTAMP": "TIMESTAMP"`)
	return s
}
