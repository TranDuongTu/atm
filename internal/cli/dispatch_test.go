package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/core"
)

// seedDispatchProject creates a project with two actions: a project-target
// one suited to manager, and a task-target interactive one suited to
// developer. Between them they cover both target shapes and both modes.
func seedDispatchProject(t *testing.T, h *goldenHarness) {
	t.Helper()
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	h.run("project", "capability", "add", "--project", "ATM", "--name", "checklist", "--actor", "admin@cli:unset")
	if _, stderr, code := h.run("checklist", "add", "--project", "ATM", "--name", "planning", "--purpose", "The weekly pass.",
		"--step", "sweep the boards", "--suits", "manager", "--actor", "admin@cli:unset"); code != ExitSuccess {
		t.Fatalf("seed planning failed: %s", stderr)
	}
	if _, stderr, code := h.run("checklist", "add", "--project", "ATM", "--name", "code-it", "--purpose", "Implement one task.",
		"--step", "build", "--suits", "developer", "--target", "task", "--mode", "interactive",
		"--actor", "admin@cli:unset"); code != ExitSuccess {
		t.Fatalf("seed code-it failed: %s", stderr)
	}
}

// TestDispatchVerbMirrorsTheRootLaunch: `atm dispatch` is the NAMED form of
// what the root flags do (plan §3.7). It must bind identically — same
// derived persona, same action, same mode — because two spellings of one
// dispatch that disagree are worse than one spelling.
func TestDispatchVerbMirrorsTheRootLaunch(t *testing.T) {
	h := newGoldenHarness(t)
	h.registryFn = productionRegistry
	seedDispatchProject(t, h)
	c := captureChild(h)
	stubLookPath(h)

	h.reset()
	if _, _, code := h.run("dispatch", "--checklist", "planning", "--project", "ATM", "--agent", "claude"); code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	viaVerb := strings.Join(c.env, "\n")
	// The root command takes arbitrary positional args and passes them to the
	// host agent, so before this verb existed `atm dispatch ...` "worked" by
	// handing the word "dispatch" to the agent as an argument. Assert the
	// verb is a COMMAND, not a passthrough arg — otherwise every assertion
	// below would pass against the wrong code path.
	for _, a := range c.argv {
		if a == "dispatch" {
			t.Fatalf("`dispatch` leaked into the agent argv %v — it is being parsed as a positional arg, not a command", c.argv)
		}
	}

	h.reset()
	if _, _, code := h.run("--checklist", "planning", "--project", "ATM", "--agent", "claude"); code != ExitSuccess {
		t.Fatalf("root exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	viaRoot := strings.Join(c.env, "\n")

	for _, want := range []string{"ATM_CHECKLIST=planning", "ATM_PERSONA=manager", "ATM_MODE=eager"} {
		if !strings.Contains(viaVerb, want) {
			t.Errorf("dispatch verb env missing %q:\n%s", want, viaVerb)
		}
		if !strings.Contains(viaRoot, want) {
			t.Errorf("root launch env missing %q:\n%s", want, viaRoot)
		}
	}
}

// TestDispatchVerbRequiresAnAction: `atm dispatch` dispatches an ACTION. The
// bare-persona ad-hoc form lives on the root flags, where it always did.
func TestDispatchVerbRequiresAnAction(t *testing.T) {
	h := newGoldenHarness(t)
	h.registryFn = productionRegistry
	seedDispatchProject(t, h)
	captureChild(h)
	stubLookPath(h)
	h.reset()

	if _, _, code := h.run("dispatch", "--project", "ATM", "--agent", "claude"); code == ExitSuccess {
		t.Fatal("dispatch without --checklist must fail")
	}
}

// TestDispatchVerbCarriesTheOverrides: --persona and --mode are the two
// documented overrides, and both reach the binding.
func TestDispatchVerbCarriesTheOverrides(t *testing.T) {
	h := newGoldenHarness(t)
	h.registryFn = productionRegistry
	seedDispatchProject(t, h)
	c := captureChild(h)
	stubLookPath(h)
	h.reset()

	if _, _, code := h.run("dispatch", "--checklist", "planning", "--project", "ATM", "--agent", "claude",
		"--persona", "developer", "--mode", "interactive"); code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	joined := strings.Join(c.env, "\n")
	if !strings.Contains(joined, "ATM_PERSONA=developer") {
		t.Errorf("--persona override did not reach the binding:\n%s", joined)
	}
	if !strings.Contains(joined, "ATM_MODE=interactive") {
		t.Errorf("--mode override did not reach the binding:\n%s", joined)
	}
}

// TestDispatchVerbRefusesResident: resident is refused at the binding layer,
// so every dispatch surface inherits the refusal without restating it.
func TestDispatchVerbRefusesResident(t *testing.T) {
	h := newGoldenHarness(t)
	h.registryFn = productionRegistry
	seedDispatchProject(t, h)
	captureChild(h)
	stubLookPath(h)
	h.reset()

	if _, _, code := h.run("dispatch", "--checklist", "planning", "--project", "ATM", "--agent", "claude", "--mode", "resident"); code == ExitSuccess {
		t.Fatal("mode resident must be refused")
	}
	if !strings.Contains(h.stderr.String(), "resident is not launchable yet") {
		t.Fatalf("stderr must say why:\n%s", h.stderr.String())
	}
}

// TestDispatchDryRunShowsTheBindingWithoutLaunching: --dry-run answers "what
// would this dispatch do" without starting a host agent. It is what makes a
// dispatch inspectable — the alternative is spawning an agent to find out.
func TestDispatchDryRunShowsTheBindingWithoutLaunching(t *testing.T) {
	h := newGoldenHarness(t)
	h.registryFn = productionRegistry
	seedDispatchProject(t, h)
	c := captureChild(h)
	stubLookPath(h)
	h.reset()

	// code-it is task-target, so it needs a task to dispatch on.
	h.run("task", "create", "--project", "ATM", "--title", "some work", "--actor", "admin@cli:unset")
	var id string
	for _, task := range h.store.ListTasks(core.QueryFilters{Project: "ATM"}) {
		if task.Title == "some work" {
			id = task.ID
		}
	}
	if id == "" {
		t.Fatal("task seed failed")
	}

	h.reset()
	out, _, code := h.run("dispatch", "--checklist", "code-it", "--task", id, "--project", "ATM", "--agent", "claude", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	if c.name != "" {
		t.Fatalf("--dry-run must not launch a host agent, but ran %q", c.name)
	}
	// The binding it reports is the one a real launch would use: the derived
	// persona, the action's own mode, and where the context was rendered.
	for _, want := range []string{"code-it", "developer", "interactive", id} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

// TestDispatchDryRunRendersTheContextFile: the point of inspecting a
// dispatch is reading what the session would be told, so the dry run writes
// the context file exactly as a launch does.
func TestDispatchDryRunRendersTheContextFile(t *testing.T) {
	h := newGoldenHarness(t)
	h.registryFn = productionRegistry
	seedDispatchProject(t, h)
	captureChild(h)
	stubLookPath(h)
	h.reset()

	out, _, code := h.run("dispatch", "--checklist", "planning", "--project", "ATM", "--agent", "claude", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	path := filepath.Join(h.store.StorePath(), "projects", "ATM", "cache", "session-manager.md")
	if !strings.Contains(out, path) {
		t.Fatalf("dry-run must name the context file it wrote:\n%s", out)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dry-run must WRITE the context file, not just name it: %v", err)
	}
	body := string(raw)
	for _, want := range []string{"# Who you are", "## Persona: manager", "## Checklist: planning", "# Where you work"} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered context missing %q:\n%s", want, body)
		}
	}
}
