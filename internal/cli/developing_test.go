package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDevelopingCodexDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "codex", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developing-dry-run-codex", got)
}

func TestDevelopingMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	_, stderrStr, code := h.run("developing", "codex", "--project", "NOPE", "--dry-run")
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

func TestDevelopingPluginStatusJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	_, _, code := h.run("developing", "plugin", "status", "codex")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeHome(h.stdout.String(), home)
	compareGolden(t, "developing-plugin-status", got)
}

func TestDevelopingPluginInstallDryRunJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	_, _, code := h.run("developing", "plugin", "install", "claude", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeHome(h.stdout.String(), home)
	compareGolden(t, "developing-plugin-install-dry-run", got)
}

func TestDevelopingEnvIncludesATMValues(t *testing.T) {
	got := assembleEnv(developingEnvValues("FOO", "/bin/atm", "codex-dev", "FOO-RUNID", "/tmp/context.md", "codex", ""))
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"ATM_ROLE=developing",
		"ATM_PROJECT=FOO",
		"ATM_BIN=/bin/atm",
		"ATM_ACTOR=codex-dev",
		"ATM_RUN_ID=FOO-RUNID",
		"ATM_CONTEXT_FILE=/tmp/context.md",
		"ATM_AGENT=codex",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("developing env missing %q", want)
		}
	}
}

func TestDevelopingLauncherNotFound(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, stderrStr, code := h.run("developing", "codex", "--project", "FOO")
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d", code, ExitGeneric)
	}
	got := normalizeDevelopingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "developing-launcher-not-found", got)
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

func TestDevelopingCodexExtraArgsDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "codex", "--project", "FOO", "--dry-run", "--", "--yolo", "--auto")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developing-dry-run-codex-extra", got)
}

func TestDevelopingOllamaDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "ollama", "--project", "FOO", "--integration", "codex", "--dry-run", "--", "--yolo")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developing-dry-run-ollama", got)
}

func TestDevelopingCodexEnvArgsDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	prev := os.Getenv("ATM_CODEX_ARGS")
	os.Setenv("ATM_CODEX_ARGS", "--yolo")
	t.Cleanup(func() { os.Setenv("ATM_CODEX_ARGS", prev) })
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "codex", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developing-dry-run-codex-env", got)
}

func TestDeveloping_PersonaEnvAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.run("persona", "create", "--name", "staff", "--prompt", "high bar", "--actor", "ttran")
	h.reset()
	out, _, code := h.run("developing", "claude", "--project", "FOO", "--persona", "staff", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{
		`"ATM_PERSONA": "staff"`,
		`"ATM_AGENT": "claude"`,
		`"ATM_ACTOR": "staff@claude"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("persona launch env missing %q:\n%s", want, out)
		}
	}
}

func TestDevelopingOllamaRequiresIntegration(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "ollama", "--project", "FOO", "--dry-run")
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d (generic; cobra required-flag error)", code, ExitGeneric)
	}
}
