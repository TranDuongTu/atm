package cli

import (
	"bytes"
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

func TestOnboardingOpencodeExtraArgsDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("onboarding", "opencode", "--project", "FOO", "--dry-run", "--", "--foo")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeOnboardingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "onboarding-dry-run-opencode-extra", got)
}

func TestOnboardingOllamaExtraArgsDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("onboarding", "ollama", "--project", "FOO", "--integration", "codex", "--dry-run", "--", "--bar")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeOnboardingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "onboarding-dry-run-ollama-extra", got)
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

// TestOnboardingLauncherNotFound exercises the Launcher-not-on-PATH error
// path (spec: onboarding-v1-design.md:431-432). It forces PATH to a
// non-existent directory so exec.LookPath("opencode") fails before any child
// process spawns. The command runs without --dry-run (dry-run exits before the
// launch step) and in JSON mode (the harness default) so the error envelope
// lands on stderr. Exit code is ExitGeneric (1).
func TestOnboardingLauncherNotFound(t *testing.T) {
	// exec.LookPath reads PATH at call time; isolating PATH to a directory
	// that contains no binaries guarantees the not-found branch.
	t.Setenv("PATH", "/nonexistent")
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, stderrStr, code := h.run("onboarding", "opencode", "--project", "FOO")
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d (generic)", code, ExitGeneric)
	}
	got := normalizeOnboardingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "onboarding-launcher-not-found", got)
}

// TestOnboardingTailSummaryJSON drives emitOnboardingTail directly as a pure
// function (spec: onboarding-v1-design.md:426-430) and asserts the JSON output
// shape via golden comparison. The tail is not reached via a live child exec
// because that would require an external launcher; emitOnboardingTail is a
// same-package helper callable from the test.
func TestOnboardingTailSummaryJSON(t *testing.T) {
	st := &cliState{flags: globalFlags{output: outputJSON}}
	buf := &bytes.Buffer{}
	st.out = buf
	if err := emitOnboardingTail(st, "opencode", "FOO", "FOO-RUNID", "v1",
		"/STORE/onboarding/FOO-RUNID.md", 3, 12, 0); err != nil {
		t.Fatalf("emitOnboardingTail: %v", err)
	}
	got := normalizeOnboardingOutput(buf.String(), "")
	compareGolden(t, "onboarding-tail-summary", got)
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
