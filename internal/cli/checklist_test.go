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
	out := runArgsOut(t, st, "checklist", "add", "--project", "ATM", "--name", "main",
		"--purpose", "day to day", "--suits", "developer", "--requires-capability", "scrum",
		"--step", "check the notion channel", "--step", "post progress", "--actor", "concierge@test:unit")
	mustContain(t, out, "created checklist main")

	st.output = outputJSON
	out = runArgsOut(t, st, "checklist", "list", "--project", "ATM", "--persona", "developer")
	mustContain(t, out, `"name": "main"`)
	mustContain(t, out, `"purpose": "day to day"`)
	mustContain(t, out, `"suits"`)
	mustContain(t, out, `"origin": "user"`)

	out = runArgsOut(t, st, "checklist", "show", "--project", "ATM", "--name", "main")
	mustContain(t, out, `"steps"`)
	mustContain(t, out, `"text": "check the notion channel"`)
	mustContain(t, out, `"capabilities"`)
	mustContain(t, out, `"scrum"`)
}

func TestChecklistShowTextRendersHeaderAndNestedSteps(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	steps := filepath.Join(t.TempDir(), "steps.md")
	if err := os.WriteFile(steps, []byte("- top\n  - child\n    - grand\n- second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--name", "nested",
		"--suits", "developer", "--steps-file", steps, "--actor", "developer@test:unit")
	out := runArgsOut(t, st, "checklist", "show", "--project", "ATM", "--name", "nested")
	mustContain(t, out, "origin: user")
	mustContain(t, out, "suits: developer")
	mustContain(t, out, "  1. top\n    1.1 child\n      1.1.1 grand\n  2. second\n")
}

func TestChecklistPersonaFlagDroppedOutsideList(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	for _, args := range [][]string{
		{"checklist", "show", "--project", "ATM", "--persona", "developer", "--name", "main"},
		{"checklist", "add", "--project", "ATM", "--persona", "developer", "--name", "main", "--step", "a", "--actor", "developer@test:unit"},
		{"checklist", "edit", "--project", "ATM", "--persona", "developer", "--name", "main", "--actor", "developer@test:unit"},
		{"checklist", "remove", "--project", "ATM", "--persona", "developer", "--name", "main", "--actor", "developer@test:unit"},
	} {
		errText, code := runChecklistErrText(t, st, args...)
		if code == ExitSuccess {
			t.Fatalf("%v: --persona must be rejected", args)
		}
		mustContain(t, errText, "unknown flag")
	}
}

func TestChecklistListSuitsFilterAndAll(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--name", "dev-routine",
		"--suits", "developer", "--step", "a", "--actor", "developer@test:unit")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--name", "suitless",
		"--purpose", "for anyone", "--step", "a", "--actor", "developer@test:unit")
	out := runArgsOut(t, st, "checklist", "list", "--project", "ATM", "--persona", "developer")
	mustContain(t, out, "dev-routine")
	mustNotContain(t, out, "suitless")
	out = runArgsOut(t, st, "checklist", "list", "--project", "ATM", "--all")
	mustContain(t, out, "dev-routine\tdeveloper\t")
	mustContain(t, out, "suitless\t-\tfor anyone")
}

func TestChecklistEnvDefaults(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--name", "main",
		"--suits", "developer", "--step", "a", "--actor", "developer@test:unit")
	t.Setenv("ATM_PROJECT", "ATM")
	t.Setenv("ATM_PERSONA", "developer")
	out := runArgsOut(t, st, "checklist", "list")
	mustContain(t, out, "main")
	out = runArgsOut(t, st, "checklist", "show", "--name", "main")
	mustContain(t, out, "1. a")
}

func TestChecklistSetReplacesFromFile(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--name", "main",
		"--purpose", "p", "--suits", "developer", "--step", "a", "--actor", "developer@test:unit")
	doc := filepath.Join(t.TempDir(), "main.md")
	if err := os.WriteFile(doc, []byte("---\nname: main\npurpose: replaced purpose\nrequires_capabilities: [qa]\n---\n1. first\n   1. nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runArgsOut(t, st, "checklist", "set", "--project", "ATM", "--name", "main",
		"--file", doc, "--actor", "developer@test:unit")
	mustContain(t, out, "set checklist main")
	st.output = outputJSON
	out = runArgsOut(t, st, "checklist", "show", "--project", "ATM", "--name", "main")
	mustContain(t, out, "replaced purpose")
	mustContain(t, out, `"qa"`)
	mustContain(t, out, "nested")
	// the whole record was replaced: the old suits are gone, provenance stays
	mustNotContain(t, out, "developer")
	mustContain(t, out, `"origin": "user"`)
}

func TestChecklistSetNameMustMatchFile(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--name", "main",
		"--step", "a", "--actor", "developer@test:unit")
	doc := filepath.Join(t.TempDir(), "other.md")
	if err := os.WriteFile(doc, []byte("---\nname: other\npurpose: p\n---\n1. a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errText, code := runChecklistErrText(t, st, "checklist", "set", "--project", "ATM",
		"--name", "main", "--file", doc, "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("set with a mismatched frontmatter name must fail")
	}
	mustContain(t, errText, "must match")
}

func TestChecklistEmptyStateAndCollision(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "checklist", "list", "--project", "ATM", "--persona", "developer")
	mustContain(t, out, "No checklists suited to developer @ ATM")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--name", "main", "--step", "a", "--actor", "developer@test:unit")
	errText, code := runChecklistErrText(t, st, "checklist", "add", "--project", "ATM", "--name", "main", "--step", "b", "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("duplicate must fail")
	}
	mustContain(t, errText, "already exists")
}

func TestChecklistRemoveDisambiguatesByTask(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	// two legacy v1 records sharing a name under different personas
	mkV1 := func(persona string) string {
		label := "ATM:checklist:" + persona
		if err := st.store.LabelSeed(label, "v1", "", "developer@test:unit"); err != nil {
			t.Fatal(err)
		}
		tk, err := st.store.CreateTask("ATM", persona+"/x", "v1 checklist", []string{label}, "developer@test:unit")
		if err != nil {
			t.Fatal(err)
		}
		payload := `{"v":1,"persona":"` + persona + `","name":"x","steps":["one"]}`
		if err := st.store.SetTaskCapabilityMeta(tk.ID, "checklist", payload, "developer@test:unit"); err != nil {
			t.Fatal(err)
		}
		return tk.ID
	}
	a := mkV1("developer")
	_ = mkV1("manager")
	errText, code := runChecklistErrText(t, st, "checklist", "remove", "--project", "ATM", "--name", "x", "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("ambiguous remove must fail")
	}
	mustContain(t, errText, "ambiguous")
	out := runArgsOut(t, st, "checklist", "remove", "--project", "ATM", "--name", "x", "--task", a, "--actor", "developer@test:unit")
	mustContain(t, out, "removed checklist x")
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

func TestChecklistSetWithoutStepsErrors(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "checklist", "add", "--project", "ATM", "--name", "main",
		"--step", "a", "--actor", "developer@test:unit")
	doc := filepath.Join(t.TempDir(), "main.md")
	if err := os.WriteFile(doc, []byte("---\nname: main\npurpose: p\n---\nprose, no steps\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errText, code := runChecklistErrText(t, st, "checklist", "set", "--project", "ATM",
		"--name", "main", "--file", doc, "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("set with a stepless document must fail")
	}
	mustContain(t, errText, "at least one numbered or dashed step")
}

func TestChecklistGateWhenCapabilityDisabled(t *testing.T) {
	st := newTestCLI(t)
	if _, err := st.store.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if err := st.store.EnableProjectCapability("ATM", "scrum", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	errText, code := runChecklistErrText(t, st, "checklist", "list", "--project", "ATM", "--persona", "developer")
	if code == ExitSuccess {
		t.Fatal("gate must reject")
	}
	mustContain(t, errText, "atm project capability add --project ATM --name checklist")
}

// TestChecklistSeedIdempotent proves `atm capability checklist seed` creates
// the shipped starter checklists once (create-if-absent) and never overwrites
// them: a second run skips, and an edited record survives re-seeding.
func TestChecklistSeedIdempotent(t *testing.T) {
	st := newRegistryTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "capability", "checklist", "seed", "--project", "ATM", "--actor", "concierge@test:unit")
	mustContain(t, out, "created 3")
	out = runArgsOut(t, st, "capability", "checklist", "seed", "--project", "ATM", "--actor", "concierge@test:unit")
	mustContain(t, out, "skipped 3")
	// JSON re-seed: created emits [] (not null) and skipped names every existing seed.
	st.output = outputJSON
	out = runArgsOut(t, st, "capability", "checklist", "seed", "--project", "ATM", "--actor", "concierge@test:unit")
	mustContain(t, out, `"created": []`)
	mustContain(t, out, `"skipped": [`)
	mustContain(t, out, `"empty-project"`)
	// seeded records carry the seed's suits and shipped origin
	out = runArgsOut(t, st, "checklist", "show", "--project", "ATM", "--name", "empty-project")
	mustContain(t, out, `"suits"`)
	mustContain(t, out, `"concierge"`)
	mustContain(t, out, `"origin": "shipped:atm"`)
	// a replaced seed survives re-seeding, and keeps its shipped provenance
	st.output = outputText
	doc := filepath.Join(t.TempDir(), "setup-channels.md")
	if err := os.WriteFile(doc, []byte("---\nname: setup-channels\npurpose: edited\n---\n1. my own step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runArgsOut(t, st, "checklist", "set", "--project", "ATM", "--name", "setup-channels",
		"--file", doc, "--actor", "developer@test:unit")
	mustContain(t, out, "set checklist setup-channels")
	_, _, _ = runArgs(st, "capability", "checklist", "seed", "--project", "ATM", "--actor", "concierge@test:unit")
	st.output = outputJSON
	out = runArgsOut(t, st, "checklist", "show", "--project", "ATM", "--name", "setup-channels")
	mustContain(t, out, `"purpose": "edited"`)
	mustContain(t, out, `"origin": "shipped:atm"`)
}
