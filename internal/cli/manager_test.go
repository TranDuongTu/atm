package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
	got := managerEnvValues("FOO", "/bin/atm", "codex-manager", "FOO-RUNID", "/tmp/context.md")
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

func TestManagerRenderContextTextHasPrompt(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	_, _, code := h.run("manager", "render-context", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	for _, want := range []string{"ATM manager session", "ledger owner", "needs clarification", "atm task set-title"} {
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
