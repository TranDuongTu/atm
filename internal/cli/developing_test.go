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

func TestDeveloperCodexLaunchJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("dev", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if c.name != "codex" {
		t.Fatalf("child name = %q, want codex", c.name)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developer-codex-launch", got)
}

func TestDeveloperLaunchAutoCreatesProject(t *testing.T) {
	h := newGoldenHarness(t)
	captureChild(h)

	_, _, code := h.run("dev", "--agent", "codex", "--project", "FOO")
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

func TestDevelopingTailSummaryJSON(t *testing.T) {
	st := &cliState{flags: globalFlags{output: outputJSON}}
	buf := &bytes.Buffer{}
	st.out = buf
	if err := emitLaunchTail(st, "developing", "FOO", "FOO-RUNID", "/STORE/developing/FOO-RUNID.md", "codex", 0); err != nil {
		t.Fatalf("emitLaunchTail: %v", err)
	}
	got := normalizeDevelopingOutput(buf.String(), "")
	compareGolden(t, "developing-tail-summary", got)
}

func TestDevelopingEnvIncludesATMValues(t *testing.T) {
	os.Unsetenv("ATM_BIN")
	got := assembleEnv(developingEnvValues("FOO", "developer@codex:unset", "FOO-RUNID", "/tmp/context.md", "codex", "developer", "2026-07-19T00:00:00Z"))
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
			t.Errorf("developing env missing %q", want)
		}
	}
	if strings.Contains(joined, "ATM_BIN=") {
		t.Errorf("developing env must not set ATM_BIN; got:\n%s", joined)
	}
}

func TestDevPATHGuard(t *testing.T) {
	// PATH must NOT resolve `atm`. Use a PATH that has only the harness's
	// own directories (none contain `atm`).
	t.Setenv("PATH", "/nonexistent")
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()
	_, stderr, code := h.run("dev", "--agent", "codex", "--project", "FOO")
	if code == ExitSuccess {
		t.Fatalf("expected non-zero exit when atm is not on PATH")
	}
	if !strings.Contains(stderr, "atm is not on PATH") {
		t.Fatalf("expected 'atm is not on PATH' in stderr; got:\n%s", stderr)
	}
}

func TestDevWriteIfDiffNoOp(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()

	// First launch writes the context file.
	if _, _, code := h.run("dev", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("first dev exit=%d stderr=%s", code, h.stderr.String())
	}
	path := filepath.Join(h.store.StorePath(), "projects", "FOO", "cache", "dev-developer.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("context file not created at %s: %v", path, err)
	}
	prev := info.ModTime()

	// Sleep so a rewrite would change mtime.
	time.Sleep(15 * time.Millisecond)

	// Second launch of the same tuple should be a no-op on the file.
	if _, _, code := h.run("dev", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("second dev exit=%d stderr=%s", code, h.stderr.String())
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("context file disappeared: %v", err)
	}
	if !info.ModTime().Equal(prev) {
		t.Fatalf("context file mtime changed on second launch; write-if-diff should be a no-op")
	}
}

func TestDevelopingLauncherNotFound(t *testing.T) {
	// The PATH guard (exec.LookPath("atm")) runs before runChild and exits
	// early if `atm` is not on PATH. To exercise the launcher-not-found path
	// (codex absent), put a directory containing an `atm` binary on PATH while
	// leaving `codex` unresolvable. Build the project's own binary into a temp
	// dir and prepend it to PATH.
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
	_, stderrStr, code := h.run("dev", "--agent", "codex", "--project", "FOO")
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d", code, ExitGeneric)
	}
	got := normalizeDevelopingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "developing-launcher-not-found", got)
}

func TestDeveloperCodexExtraArgs(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("dev", "--agent", "codex", "--project", "FOO", "--", "--yolo", "--auto")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := []string{"codex", "--yolo", "--auto"}
	if !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
}

func TestDeveloperOllamaLaunch(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("dev", "--agent", "ollama:codex", "--project", "FOO", "--", "--yolo")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := []string{"ollama", "launch", "codex", "--", "--yolo"}
	if !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
}

func TestDeveloperCodexEnvArgs(t *testing.T) {
	h := newGoldenHarness(t)
	prev := os.Getenv("ATM_CODEX_ARGS")
	os.Setenv("ATM_CODEX_ARGS", "--yolo")
	t.Cleanup(func() { os.Setenv("ATM_CODEX_ARGS", prev) })
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("dev", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := []string{"codex", "--yolo"}
	if !reflect.DeepEqual(c.argv, want) {
		t.Fatalf("argv = %v, want %v", c.argv, want)
	}
}

func TestDeveloperPersonaEnvAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("persona", "create", "--name", "staff", "--prompt", "high bar", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()

	out, _, code := h.run("dev", "--agent", "claude", "--project", "FOO", "--persona", "staff")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{
		`"ATM_PERSONA": "staff"`,
		`"ATM_AGENT": "claude"`,
		`"ATM_ACTOR": "staff@claude:unset"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("persona launch env missing %q:\n%s", want, out)
		}
	}
}

func TestDevelopingCommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("developing", "codex", "--project", "FOO")
	if code == ExitSuccess {
		t.Fatalf("atm developing should be removed")
	}
}

func TestDeveloperLaunchRejectsDryRunAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	for _, args := range [][]string{
		{"dev", "--agent", "codex", "--project", "FOO", "--dry-run"},
		{"dev", "--agent", "codex", "--project", "FOO", "--actor", "developer@codex:unset"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail", args)
		}
	}
}

func TestDevLaunchesSelectedAgent(t *testing.T) {
	h := newGoldenHarness(t)
	captureChild(h)

	// no selection and no --agent -> non-zero exit
	if _, _, code := h.run("dev", "--project", "FOO"); code == ExitSuccess {
		t.Fatal("expected non-zero exit with no agent selected")
	}

	// selecting then launching resolves the entry from config
	if _, _, code := h.run("agents", "select", "opencode"); code != ExitSuccess {
		t.Fatalf("select exit=%d", code)
	}
	h.reset()
	out, _, code := h.run("dev", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("dev exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"agent": "opencode"`) {
		t.Fatalf("dev did not resolve selected agent: %s", out)
	}
}

func TestDevAgentFlagOverridesSelected(t *testing.T) {
	h := newGoldenHarness(t)
	c := captureChild(h)
	h.run("agents", "select", "opencode")
	h.reset()
	if _, _, code := h.run("dev", "--agent", "codex", "--project", "FOO"); code != ExitSuccess {
		t.Fatalf("dev exit=%d", code)
	}
	if c.name != "codex" {
		t.Fatalf("expected --agent override to launch codex, got %q", c.name)
	}
}

func normalizeDevelopingOutput(s, storePath string) string {
	s = normalizeOutput(s)
	if storePath != "" {
		contextPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/projects/FOO/cache/dev-developer\.md`)
		s = contextPathRe.ReplaceAllString(s, "/STORE/projects/FOO/cache/dev-developer.md")
	}
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	timestampRe := regexp.MustCompile(`"ATM_TIMESTAMP": "[^"]+"`)
	s = timestampRe.ReplaceAllString(s, `"ATM_TIMESTAMP": "TIMESTAMP"`)
	return s
}

func normalizeHome(s, home string) string {
	return strings.ReplaceAll(normalizeOutput(s), filepath.ToSlash(home), "/HOME")
}
