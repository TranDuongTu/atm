package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestOnboardingOpencodeDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("onboarding", "opencode", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	// Normalize the run-id and prompt path (timestamp + uuid + store path are
	// run-specific) before comparing to the golden file.
	got := normalizeOnboardingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "onboarding-dry-run-opencode", got)
}

func TestOnboardingOllamaDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("onboarding", "ollama", "--project", "FOO", "--integration", "opencode", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeOnboardingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "onboarding-dry-run-ollama", got)
}

func TestOnboardingMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	h.reset()
	_, stderrStr, code := h.run("onboarding", "opencode", "--project", "NOPE", "--dry-run")
	if code != ExitNotFound {
		t.Fatalf("exit = %d, want %d (not found)", code, ExitNotFound)
	}
	// Error goes to stderr in the existing envelope (JSON mode harness).
	got := normalizeOnboardingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "onboarding-missing-project", got)
}

func TestOnboardingUnknownPromptVersion(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, stderrStr, code := h.run("onboarding", "opencode", "--project", "FOO", "--prompt-version", "vNoSuch", "--dry-run")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
	got := normalizeOnboardingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "onboarding-unknown-prompt-version", got)
}

// normalizeOnboardingOutput scrubs run-specific values (run-id, prompt path,
// timestamps) so golden comparison is stable. It reuses normalizeOutput for the
// store-path regex and adds an onboarding-specific run-id regex.
func normalizeOnboardingOutput(s, storePath string) string {
	s = normalizeOutput(s)
	// prompt path: <store>/onboarding/<run-id>.md
	promptPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/onboarding/FOO-\d{14}-[0-9a-f]{6}\.md`)
	s = promptPathRe.ReplaceAllString(s, "/STORE/onboarding/FOO-RUNID.md")
	// run-id: <CODE>-<14 digits>-<6 hex>
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	return s
}
