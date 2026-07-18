package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestValidateManagerAction(t *testing.T) {
	enabled := []string{"workflow"}
	registered := []string{"workflow", "contextmap"}
	if err := validateManagerAction("autopilot", "", enabled, registered); err != nil {
		t.Errorf("default action rejected: %v", err)
	}
	if err := validateManagerAction("curate", "", enabled, registered); err == nil {
		t.Error("curate accepted; want unknown-action error")
	}
	if err := validateManagerAction("brief", "nope", enabled, registered); err == nil || !strings.Contains(err.Error(), "registered: workflow, contextmap") {
		t.Errorf("unknown capability error wrong: %v", err)
	}
	if err := validateManagerAction("ask", "contextmap", enabled, registered); err == nil || !strings.Contains(err.Error(), "not enabled for project") {
		t.Errorf("not-enabled error wrong: %v", err)
	}
	if err := validateManagerAction("ask", "workflow", enabled, registered); err != nil {
		t.Errorf("enabled capability rejected: %v", err)
	}
}

func TestManageCodexAutopilotLaunchJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if c.name != "codex" {
		t.Fatalf("child name = %q, want codex", c.name)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manage-codex-autopilot-launch", got)
}

func TestManageCodexActionBriefLaunch(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO", "--action", "brief")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if c.name != "codex" {
		t.Fatalf("child name = %q, want codex", c.name)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	if !strings.Contains(got, `"ATM_MANAGER_ACTION": "brief"`) {
		t.Fatalf("expected ATM_MANAGER_ACTION brief, got:\n%s", got)
	}
	compareGolden(t, "manage-codex-action-brief-launch", got)
}

func TestManageLaunchAutoCreatesProject(t *testing.T) {
	h := newGoldenHarness(t)
	captureChild(h)

	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO")
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

func TestManageActionSelection(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)

	// No action flag: autopilot is the default, so this succeeds.
	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("no-flag default should succeed (autopilot); exit=%d stderr=%s", code, h.stderr.String())
	}
}

func TestManageAutopilotIsDefault(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	out, _, code := h.run("manage", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"ATM_MANAGER_ACTION": "autopilot"`) {
		t.Fatalf("default action should be autopilot; got:\n%s", out)
	}
	if !strings.Contains(out, `"ATM_MANAGER_CAPABILITY": ""`) {
		t.Fatalf("default capability should be empty string; got:\n%s", out)
	}
	_ = c
}

func TestManageOldActionFlagsRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	// Old boolean action flags are gone; passing them must error as unknown
	// flags (the action vocabulary is now --action only).
	for _, args := range [][]string{
		{"manage", "--agent", "codex", "--project", "FOO", "--curate"},
		{"manage", "--agent", "codex", "--project", "FOO", "--recall"},
		{"manage", "--agent", "codex", "--project", "FOO", "--mapping"},
		{"manage", "--agent", "codex", "--project", "FOO", "--onboarding"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail (old action flag removed)", args)
		}
	}
}

func TestManageOldFlagsRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	for _, flag := range []string{"--planning", "--grooming", "--tracking", "--glossary", "--asking"} {
		_, stderr, code := h.run("manage", "--agent", "codex", "--project", "FOO", flag)
		if code == ExitSuccess {
			t.Fatalf("old flag %q should be unknown, but exit was 0", flag)
		}
		if !strings.Contains(stderr, "unknown flag") {
			t.Fatalf("old flag %q should error as 'unknown flag'; got stderr=%s", flag, stderr)
		}
	}
}

func TestManageRejectsDryRunAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	for _, args := range [][]string{
		{"manage", "--agent", "codex", "--project", "FOO", "--dry-run"},
		{"manage", "--agent", "codex", "--project", "FOO", "--actor", "manager@codex:unset"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail", args)
		}
	}
}

func TestManageCapabilityScopeEnv(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()

	out, _, code := h.run("manage", "--agent", "codex", "--project", "FOO", "--action", "ask", "--capability", "contextmap")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	for _, want := range []string{
		`"ATM_MANAGER_ACTION": "ask"`,
		`"ATM_MANAGER_CAPABILITY": "contextmap"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("manager env missing %q:\n%s", want, out)
		}
	}
}

func TestManageCapabilityUnknownIsUsageError(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)

	_, stderr, code := h.run("manage", "--agent", "codex", "--project", "FOO", "--capability", "nope")
	if code == ExitSuccess {
		t.Fatalf("unknown capability must error")
	}
	if !strings.Contains(stderr, "registered:") {
		t.Fatalf("unknown capability error must list registered; got stderr=%s", stderr)
	}
}

func TestManagePersonaEnvAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("persona", "create", "--name", "ops", "--prompt", "curate well", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()

	out, _, code := h.run("manage", "--agent", "claude", "--project", "FOO", "--persona", "ops")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{
		`"ATM_PERSONA": "ops"`,
		`"ATM_MANAGER_ACTION": "autopilot"`,
		`"ATM_ACTOR": "ops@claude:unset"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("manager env missing %q:\n%s", want, out)
		}
	}
}

func TestManagerCommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("manager", "codex", "--project", "FOO")
	if code == ExitSuccess {
		t.Fatalf("atm manager should be removed")
	}
}

func TestManagePluginCommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("manage", "plugin", "status")
	if code == ExitSuccess {
		t.Fatalf("atm manage plugin should be removed")
	}
}

func TestManageContextHiddenFromRootHelp(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("--help")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(out, "manage-context") {
		t.Fatalf("manage-context should be hidden from root help:\n%s", out)
	}
}

func TestManageContextRendersPrompt(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.reset()
	h.output = outputText
	_, _, code := h.run("manage-context", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	for _, want := range []string{"ATM manager", "autonomous owner", "conventions", "capability list --project FOO"} {
		if !strings.Contains(got, want) {
			t.Errorf("manage-context output missing %q", want)
		}
	}
	for _, old := range []string{"**Curate**", "**Recall**", "**Mapping**", "Tracking request", "Inquiry", "Vocabulary", "Planning", "Grooming", "Tracking", "Asking", "Glossary", "Onboarding", "<CAPABILITY_ROLES>"} {
		if strings.Contains(got, old) {
			t.Errorf("manage-context output still contains old term %q", old)
		}
	}
}

func TestManageContextGenericKeepsPlaceholders(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	_, _, code := h.run("manage-context")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	for _, placeholder := range []string{"<CODE>", "<ATM_BIN>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic manage-context stripped %s", placeholder)
		}
	}
}

func TestManageContextFillsProjectName(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo Project", "--actor", "admin@cli:unset")
	h.reset()
	h.output = outputText
	_, _, code := h.run("manage-context", "--project", "FOO", "--actor", "admin@cli:unset")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	if !strings.Contains(got, "Foo Project") {
		t.Errorf("manage-context did not fill <PROJECT_NAME> from the store:\n%s", got)
	}
	for _, ph := range []string{"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>"} {
		if strings.Contains(got, ph) {
			t.Errorf("manage-context left placeholder %s when --project given", ph)
		}
	}
}

func TestManagerEnvSetsActionAndCapability(t *testing.T) {
	got := managerEnvValues("FOO", "/bin/atm", "manager@opencode:unset", "FOO-RUNID", "/tmp/ctx.md", "manager", "autopilot", "")
	joined := strings.Join(gotToSlice(got), "\n")
	for _, want := range []string{
		"ATM_PERSONA=manager",
		"ATM_MANAGER_ACTION=autopilot",
		"ATM_MANAGER_CAPABILITY=",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("manager env missing %q; got:\n%s", want, joined)
		}
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
