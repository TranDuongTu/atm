package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"atm/internal/manager"
)

func TestManageCodexCurateLaunchJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO", "--curate")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if c.name != "codex" {
		t.Fatalf("child name = %q, want codex", c.name)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manage-codex-curate-launch", got)
}

func TestManageLaunchAutoCreatesProject(t *testing.T) {
	h := newGoldenHarness(t)
	captureChild(h)

	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO", "--curate")
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

	// No action flag: Curate is the default, so this succeeds.
	_, _, code := h.run("manage", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("no-flag default should succeed (curate); exit=%d stderr=%s", code, h.stderr.String())
	}
}

func TestManageCurateIsDefault(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	out, _, code := h.run("manage", "--agent", "codex", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"ATM_MANAGER_ACTION": "curate"`) {
		t.Fatalf("default action should be curate; got:\n%s", out)
	}
	_ = c
}

func TestManageRejectsConflictingActions(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	for _, args := range [][]string{
		{"manage", "--agent", "codex", "--project", "FOO", "--curate", "--recall"},
		{"manage", "--agent", "codex", "--project", "FOO", "--recall", "--onboarding"},
		{"manage", "--agent", "codex", "--project", "FOO", "--curate", "--onboarding"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail (conflicting actions)", args)
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
		{"manage", "--agent", "codex", "--project", "FOO", "--curate", "--dry-run"},
		{"manage", "--agent", "codex", "--project", "FOO", "--curate", "--actor", "manager@codex:unset"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail", args)
		}
	}
}

func TestMappingActionResolves(t *testing.T) {
	got, err := validateManagerAction(managerOpts{Mapping: true})
	if err != nil {
		t.Fatalf("validateManagerAction: %v", err)
	}
	if got != managerActionMapping {
		t.Errorf("got %q, want %q", got, managerActionMapping)
	}
}

func TestOnboardingAliasStillResolves(t *testing.T) {
	// Deprecated, hidden, but never hard-broken: the flag is on a stable CLI
	// surface. See ATM-0113.
	got, err := validateManagerAction(managerOpts{Onboarding: true})
	if err != nil {
		t.Fatalf("validateManagerAction: %v", err)
	}
	if got != managerActionMapping {
		t.Errorf("--onboarding must resolve to %q, got %q", managerActionMapping, got)
	}
}

func TestMappingAndOnboardingTogetherIsOneAction(t *testing.T) {
	// Both names for the same action must not count as two selections.
	if _, err := validateManagerAction(managerOpts{Mapping: true, Onboarding: true}); err != nil {
		t.Errorf("alias + canonical must be accepted as one action, got %v", err)
	}
}

func TestNoActionDefaultsToCurate(t *testing.T) {
	// ATM-0120: Curate is the default when no action flag is passed.
	got, err := validateManagerAction(managerOpts{})
	if err != nil {
		t.Fatalf("validateManagerAction: %v", err)
	}
	if got != managerActionCurate {
		t.Errorf("no action: got %q, want %q (default)", got, managerActionCurate)
	}
}

func TestMultipleActionsIsUsageError(t *testing.T) {
	if _, err := validateManagerAction(managerOpts{Curate: true, Recall: true}); err == nil {
		t.Error("want usage error when more than one action is selected")
	}
}

func TestManageOllamaOnboarding(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("manage", "--agent", "ollama:opencode", "--project", "FOO", "--onboarding")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(strings.Join(c.argv, " "), "--auto --prompt") {
		t.Fatalf("onboarding argv = %v, want non-interactive prompt argv", c.argv)
	}
	if !strings.Contains(strings.Join(c.env, "\n"), "ATM_ONBOARD=1") {
		t.Fatalf("onboarding env missing ATM_ONBOARD=1:\n%s", strings.Join(c.env, "\n"))
	}
}

func TestManagePersonaEnvAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("persona", "create", "--name", "ops", "--prompt", "curate well", "--actor", "admin@cli:unset")
	captureChild(h)
	h.reset()

	out, _, code := h.run("manage", "--agent", "claude", "--project", "FOO", "--curate", "--persona", "ops")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{
		`"ATM_PERSONA": "ops"`,
		`"ATM_MANAGER_ACTION": "curate"`,
		`"ATM_ACTOR": "ops@claude:unset"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("manager env missing %q:\n%s", want, out)
		}
	}
}

func TestManagerCommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("manager", "codex", "--project", "FOO", "--curate")
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
	h.output = outputText
	_, _, code := h.run("manage-context", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := h.stdout.String()
	// "Mapping" is no longer asserted here: the manager prompt's Roles list is
	// now composed from CapabilityActions (internal/manager.RenderContext),
	// and this CLI command does not yet pass any (that wiring lands with the
	// capability registry in a later task). See TestRenderCapabilityRoles in
	// internal/manager for coverage of the composed role.
	for _, want := range []string{"ATM manager", "autonomous owner", "Curate", "Recall", "conventions"} {
		if !strings.Contains(got, want) {
			t.Errorf("manage-context output missing %q", want)
		}
	}
	for _, old := range []string{"Tracking request", "Inquiry", "Vocabulary", "Planning", "Grooming", "Tracking", "Asking", "Glossary", "Onboarding"} {
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

func TestManagerOnboardEnvHasATMOnboard(t *testing.T) {
	got := managerEnvValues("FOO", "/bin/atm", "manager@opencode:unset", "FOO-RUNID", "/tmp/ctx.md", true, "manager", "onboarding")
	joined := strings.Join(gotToSlice(got), "\n")
	for _, want := range []string{
		"ATM_ONBOARD=1",
		"ATM_PERSONA=manager",
		"ATM_MANAGER_ACTION=onboarding",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("onboard env missing %q; got:\n%s", want, joined)
		}
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
