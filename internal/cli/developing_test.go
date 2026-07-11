package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
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

	_, _, code := h.run("codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if c.name != "codex" {
		t.Fatalf("child name = %q, want codex", c.name)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developer-codex-launch", got)
}

func TestDeveloperMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	_, stderrStr, code := h.run("codex", "--project", "NOPE")
	if code != ExitNotFound {
		t.Fatalf("exit = %d, want %d", code, ExitNotFound)
	}
	got := normalizeDevelopingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "developing-missing-project", got)
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
	got := assembleEnv(developingEnvValues("FOO", "/bin/atm", "developer@codex:unset", "FOO-RUNID", "/tmp/context.md", "codex", "developer"))
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"ATM_ROLE=developing",
		"ATM_PROJECT=FOO",
		"ATM_BIN=/bin/atm",
		"ATM_ACTOR=developer@codex:unset",
		"ATM_RUN_ID=FOO-RUNID",
		"ATM_CONTEXT_FILE=/tmp/context.md",
		"ATM_AGENT=codex",
		"ATM_PERSONA=developer",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("developing env missing %q", want)
		}
	}
}

func TestDevelopingLauncherNotFound(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.reset()
	_, stderrStr, code := h.run("codex", "--project", "FOO")
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

	_, _, code := h.run("codex", "--project", "FOO", "--", "--yolo", "--auto")
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

	_, _, code := h.run("ollama", "--project", "FOO", "--integration", "codex", "--", "--yolo")
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

	_, _, code := h.run("codex", "--project", "FOO")
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

	out, _, code := h.run("claude", "--project", "FOO", "--persona", "staff")
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
		{"codex", "--project", "FOO", "--dry-run"},
		{"codex", "--project", "FOO", "--actor", "developer@codex:unset"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail", args)
		}
	}
}

func TestDeveloperOllamaRequiresIntegration(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.reset()
	_, _, code := h.run("ollama", "--project", "FOO")
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d (generic; cobra required-flag error)", code, ExitGeneric)
	}
}

func normalizeDevelopingOutput(s, storePath string) string {
	s = normalizeOutput(s)
	if storePath != "" {
		contextPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/developing/FOO-\d{14}-[0-9a-f]{6}\.md`)
		s = contextPathRe.ReplaceAllString(s, "/STORE/developing/FOO-RUNID.md")
	}
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	atmBinRe := regexp.MustCompile(`"ATM_BIN": "[^"]+"`)
	s = atmBinRe.ReplaceAllString(s, `"ATM_BIN": "/ATM_BIN"`)
	return s
}

func normalizeHome(s, home string) string {
	return strings.ReplaceAll(normalizeOutput(s), filepath.ToSlash(home), "/HOME")
}
