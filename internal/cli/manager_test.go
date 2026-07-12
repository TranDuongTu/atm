package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"atm/internal/manager"
)

func TestManageCodexPlanningLaunchJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("manage", "codex", "--project", "FOO", "--planning")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if c.name != "codex" {
		t.Fatalf("child name = %q, want codex", c.name)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manage-codex-planning-launch", got)
}

func TestManageRequiresExactlyOneAction(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	captureChild(h)
	for _, args := range [][]string{
		{"manage", "codex", "--project", "FOO"},
		{"manage", "codex", "--project", "FOO", "--planning", "--grooming"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail", args)
		}
	}
}

func TestManageRejectsDryRunAndActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	for _, args := range [][]string{
		{"manage", "codex", "--project", "FOO", "--planning", "--dry-run"},
		{"manage", "codex", "--project", "FOO", "--planning", "--actor", "manager@codex:unset"},
	} {
		_, _, code := h.run(args...)
		if code == ExitSuccess {
			t.Fatalf("%v should fail", args)
		}
	}
}

func TestManageOllamaOnboarding(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	c := captureChild(h)
	h.reset()

	_, _, code := h.run("manage", "ollama", "--project", "FOO", "--integration", "opencode", "--onboarding")
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

	out, _, code := h.run("manage", "claude", "--project", "FOO", "--planning", "--persona", "ops")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{
		`"ATM_PERSONA": "ops"`,
		`"ATM_MANAGER_ACTION": "planning"`,
		`"ATM_ACTOR": "ops@claude:unset"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("manager env missing %q:\n%s", want, out)
		}
	}
}

func TestManagerCommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("manager", "codex", "--project", "FOO", "--planning")
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
	for _, want := range []string{"ATM manager", "autonomous owner", "Tracking", "Asking", "Glossary", "Onboarding", "conventions"} {
		if !strings.Contains(got, want) {
			t.Errorf("manage-context output missing %q", want)
		}
	}
	for _, old := range []string{"Tracking request", "Inquiry", "Vocabulary"} {
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
