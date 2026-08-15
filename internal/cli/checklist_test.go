package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// The brief's sketch actors ("c@t:u") are not registered personas; the store
// validateActor gate rejects them (built-ins are admin/concierge/developer/
// manager), so, as in Task 6, the mutation tests here use the package's
// established "developer@test:unit" actor. Behavior is unchanged.

// runChecklistErrText runs args against the testCLI harness and returns the
// resulting error's message text (empty on success) plus the exit code. See
// the runChannelErrText comment in channel_test.go for why gate/validation
// tests drive root.Execute() directly.
func runChecklistErrText(t *testing.T, h *testCLI, args ...string) (string, int) {
	t.Helper()
	h.stdout.Reset()
	h.stderr.Reset()
	root := newRootCmdWithState(h.st)
	root.SilenceUsage = true
	root.SilenceErrors = true
	h.st.flags.store = h.store.StorePath()
	h.st.flags.output = h.output
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return "", ExitSuccess
	}
	return err.Error(), ExitCodeForError(err)
}

func TestChecklistAddListShowJSON(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "checklist", "add", "--project", "ATM", "--persona", "developer", "--name", "main",
		"--purpose", "day to day", "--step", "check the notion channel", "--step", "post progress", "--actor", "concierge@test:unit")
	mustContain(t, out, "created checklist developer/main")

	st.output = outputJSON
	out = runArgsOut(t, st, "checklist", "list", "--project", "ATM", "--persona", "developer")
	mustContain(t, out, `"persona": "developer"`)
	mustContain(t, out, `"name": "main"`)
	mustContain(t, out, `"purpose": "day to day"`)

	out = runArgsOut(t, st, "checklist", "show", "--project", "ATM", "--persona", "developer", "--name", "main")
	mustContain(t, out, `"steps"`)
	mustContain(t, out, "check the notion channel")
}

func TestChecklistEnvDefaults(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--persona", "developer", "--name", "main",
		"--step", "a", "--actor", "developer@test:unit")
	t.Setenv("ATM_PROJECT", "ATM")
	t.Setenv("ATM_PERSONA", "developer")
	out := runArgsOut(t, st, "checklist", "list")
	mustContain(t, out, "developer/main")
	out = runArgsOut(t, st, "checklist", "show", "--name", "main")
	mustContain(t, out, "1. a")
}

func TestChecklistEmptyStateAndCollision(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "checklist", "list", "--project", "ATM", "--persona", "developer")
	mustContain(t, out, "No checklists for developer @ ATM")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--persona", "developer", "--name", "main", "--step", "a", "--actor", "developer@test:unit")
	errText, code := runChecklistErrText(t, st, "checklist", "add", "--project", "ATM", "--persona", "developer", "--name", "main", "--step", "b", "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("duplicate must fail")
	}
	mustContain(t, errText, "already exists")
}

func TestChecklistListJSONEmptyEmitsArray(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	st.output = outputJSON
	out := runArgsOut(t, st, "checklist", "list", "--project", "ATM", "--persona", "developer")
	if out != "[]\n" {
		t.Fatalf("persona-scoped empty list: got %q, want %q", out, "[]\n")
	}
	out = runArgsOut(t, st, "checklist", "list", "--project", "ATM", "--all")
	if out != "[]\n" {
		t.Fatalf("--all empty list: got %q, want %q", out, "[]\n")
	}
}

func TestChecklistEditEmptyStepsFileErrors(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--persona", "developer", "--name", "main",
		"--step", "a", "--actor", "developer@test:unit")
	empty := filepath.Join(t.TempDir(), "steps.txt")
	if err := os.WriteFile(empty, []byte("   \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errText, code := runChecklistErrText(t, st, "checklist", "edit", "--project", "ATM", "--persona", "developer",
		"--name", "main", "--steps-file", empty, "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("edit with an empty steps file must fail")
	}
	mustContain(t, errText, "needs at least one step")
}

func TestChecklistGateWhenCapabilityDisabled(t *testing.T) {
	st := newTestCLI(t)
	if _, err := st.store.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if err := st.store.EnableProjectCapability("ATM", "workflow", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	errText, code := runChecklistErrText(t, st, "checklist", "list", "--project", "ATM", "--persona", "developer")
	if code == ExitSuccess {
		t.Fatal("gate must reject")
	}
	mustContain(t, errText, "atm project capability add --project ATM --name checklist")
}
