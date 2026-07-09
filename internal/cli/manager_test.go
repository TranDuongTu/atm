package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"atm/internal/manager"
)

func TestManagerCodexDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "codex", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-dry-run-codex", got)
}

func TestManagerLaunchWarnsWhenPluginMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, stderrStr, code := h.run("manager", "codex", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"warning: manager plugin", "atm manager plugin install"} {
		if !strings.Contains(stderrStr, want) {
			t.Errorf("stderr missing %q; got:\n%s", want, stderrStr)
		}
	}
}

func TestManagerMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	_, stderrStr, code := h.run("manager", "codex", "--project", "NOPE", "--dry-run")
	if code != ExitNotFound {
		t.Fatalf("exit = %d, want %d", code, ExitNotFound)
	}
	got := normalizeManagerOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "manager-missing-project", got)
}

func TestManagerPluginStatusJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	_, _, code := h.run("manager", "plugin", "status", "codex")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeHome(h.stdout.String(), home)
	compareGolden(t, "manager-plugin-status", got)
}

func TestManagerPluginInstallDryRunJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	_, _, code := h.run("manager", "plugin", "install", "opencode", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeHome(h.stdout.String(), home)
	compareGolden(t, "manager-plugin-install-dry-run", got)
}

func TestManagerEnvIncludesATMValues(t *testing.T) {
	got := managerEnvValues("FOO", "/bin/atm", "codex-manager", "FOO-RUNID", "/tmp/context.md", false)
	joined := strings.Join(gotToSlice(got), "\n")
	for _, want := range []string{
		"ATM_ROLE=manager",
		"ATM_PROJECT=FOO",
		"ATM_BIN=/bin/atm",
		"ATM_ACTOR=codex-manager",
		"ATM_RUN_ID=FOO-RUNID",
		"ATM_CONTEXT_FILE=/tmp/context.md",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("manager env missing %q", want)
		}
	}
}

func TestManagerClaudeExtraArgsDryRunJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "claude", "--project", "FOO", "--dry-run", "--", "--dangerously-skip-permission")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-dry-run-claude-extra", got)
}

func TestManagerOllamaDryRunJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "ollama", "--project", "FOO", "--integration", "opencode", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-dry-run-ollama", got)
}

func TestManagerOllamaRequiresIntegration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "ollama", "--project", "FOO", "--dry-run")
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d (generic; cobra required-flag error)", code, ExitGeneric)
	}
}

func TestManagerRenderContextTextHasPrompt(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	_, _, code := h.run("manager", "render-context", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	for _, want := range []string{"ATM manager", "autonomous owner", "Tracking request", "Onboarding"} {
		if !strings.Contains(got, want) {
			t.Errorf("render-context output missing %q", want)
		}
	}
}

func TestManagerRenderContextGenericKeepsPlaceholders(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	_, _, code := h.run("manager", "render-context")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	for _, placeholder := range []string{"<CODE>", "<ATM_BIN>", "<ACTOR>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic render-context stripped %s", placeholder)
		}
	}
}

func TestManagerRenderContextFillsProjectName(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo Project", "--actor", "ttran")
	h.reset()
	h.output = outputText
	_, _, code := h.run("manager", "render-context", "--project", "FOO", "--actor", "m")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	if !strings.Contains(got, "Foo Project") {
		t.Errorf("render-context did not fill <PROJECT_NAME> from the store:\n%s", got)
	}
	for _, ph := range []string{"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>"} {
		if strings.Contains(got, ph) {
			t.Errorf("render-context left placeholder %s when --project given", ph)
		}
	}
}

func TestManagerOnboardOpencodeDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "opencode", "--project", "FOO", "--onboard", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-onboard-dry-run-opencode", got)
}

func TestManagerOnboardOllamaDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "ollama", "--project", "FOO", "--integration", "opencode", "--onboard", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-onboard-dry-run-ollama", got)
}

func TestManagerOnboardEnvHasATMOnboard(t *testing.T) {
	got := managerEnvValues("FOO", "/bin/atm", "opencode-manager", "FOO-RUNID", "/tmp/ctx.md", true)
	joined := strings.Join(gotToSlice(got), "\n")
	if !strings.Contains(joined, "ATM_ONBOARD=1") {
		t.Errorf("onboard env missing ATM_ONBOARD=1; got:\n%s", joined)
	}
}

func TestManagerOnboardArgvUsesAutoPrompt(t *testing.T) {
	l, ok := manager.LauncherFor("opencode")
	if !ok {
		t.Fatal("LauncherFor(opencode) not found")
	}
	argv := l.BuildArgvOnboard("/tmp/ctx.md")
	if argv[1] != "--auto" || argv[2] != "--prompt" {
		t.Fatalf("onboard argv = %v, want --auto --prompt <msg>", argv)
	}
}

func TestOnboardingCommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("onboarding", "opencode", "--project", "FOO", "--dry-run")
	if code == ExitSuccess {
		t.Fatalf("onboarding command should be removed; got exit 0")
	}
}

func gotToSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func normalizeManagerOutput(s, storePath string) string {
	s = normalizeOutput(s)
	if storePath != "" {
		contextPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/manager/FOO-\d{14}-[0-9a-f]{6}\.md`)
		s = contextPathRe.ReplaceAllString(s, "/STORE/manager/FOO-RUNID.md")
	}
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	atmBinRe := regexp.MustCompile(`"ATM_BIN": "[^"]+"`)
	s = atmBinRe.ReplaceAllString(s, `"ATM_BIN": "/ATM_BIN"`)
	return s
}
