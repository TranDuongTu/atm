package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const personaDocument = `---
name: manager
description: Runs the flow.
---
# Persona: manager

You are the manager of <CODE>.

## Principles

1. **Multiply, don't produce.**
`

func writePersonaFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "manager.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func personaCLI(t *testing.T) *testCLI {
	t.Helper()
	// The ambient ATM_PROJECT of a developing session must not decide what
	// these tests exercise.
	t.Setenv("ATM_PROJECT", "")
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	return st
}

func TestPersonaSetCreatesThenUpdatesTheProjectRecord(t *testing.T) {
	st := personaCLI(t)
	out := runArgsOut(t, st, "persona", "set", "--project", "ATM",
		"--file", writePersonaFile(t, personaDocument), "--actor", "developer@test:unit")
	mustContain(t, out, "created persona manager")

	out = runArgsOut(t, st, "persona", "show", "--project", "ATM", "manager")
	mustContain(t, out, "Runs the flow.")
	mustContain(t, out, "## Principles")
	mustContain(t, out, "user")

	revised := strings.Replace(personaDocument, "Runs the flow.", "Runs everything.", 1)
	out = runArgsOut(t, st, "persona", "set", "--project", "ATM",
		"--file", writePersonaFile(t, revised), "--actor", "developer@test:unit")
	mustContain(t, out, "updated persona manager")

	out = runArgsOut(t, st, "persona", "show", "--project", "ATM", "manager")
	mustContain(t, out, "Runs everything.")
}

func TestPersonaSetReadsStdin(t *testing.T) {
	st := personaCLI(t)
	st.st.in = strings.NewReader(personaDocument)
	out := runArgsOut(t, st, "persona", "set", "--project", "ATM", "--file", "-", "--actor", "developer@test:unit")
	mustContain(t, out, "created persona manager")
}

func TestPersonaListShowsProjectRecordsWhenAProjectIsInScope(t *testing.T) {
	st := personaCLI(t)
	_ = runArgsOut(t, st, "persona", "set", "--project", "ATM",
		"--file", writePersonaFile(t, personaDocument), "--actor", "developer@test:unit")

	out := runArgsOut(t, st, "persona", "list", "--project", "ATM")
	mustContain(t, out, "manager")
	mustContain(t, out, "Runs the flow.")
	// The machine-global built-ins are a different listing; a project's
	// records are only its own.
	if strings.Contains(out, "admin") {
		t.Fatalf("project listing leaked the machine-global personas:\n%s", out)
	}

	st.output = outputJSON
	out = runArgsOut(t, st, "persona", "list", "--project", "ATM")
	mustContain(t, out, `"personas"`)
	mustContain(t, out, `"origin": "user"`)
	mustContain(t, out, `"task_id"`)
}

// A document that disagrees with the ledger is a mistake to stop on.
func TestPersonaSetRejectsAnInvalidDocument(t *testing.T) {
	st := personaCLI(t)
	msg, code := runChecklistErrText(t, st, "persona", "set", "--project", "ATM",
		"--file", writePersonaFile(t, "---\nname: manager\n---\nbody\n"), "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("set accepted a document with no description")
	}
	if !strings.Contains(msg, "description") {
		t.Fatalf("error = %q", msg)
	}
}

func TestPersonaResetRefusesARecordTheProjectAuthored(t *testing.T) {
	st := personaCLI(t)
	_ = runArgsOut(t, st, "persona", "set", "--project", "ATM",
		"--file", writePersonaFile(t, personaDocument), "--actor", "developer@test:unit")

	msg, code := runChecklistErrText(t, st, "persona", "reset", "--project", "ATM", "--name", "manager", "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("reset accepted a user-origin record")
	}
	if !strings.Contains(msg, "nothing to restore") {
		t.Fatalf("error = %q, want it to say why", msg)
	}
}

func TestPersonaSetNeedsAProject(t *testing.T) {
	st := personaCLI(t)
	msg, code := runChecklistErrText(t, st, "persona", "set",
		"--file", writePersonaFile(t, personaDocument), "--actor", "developer@test:unit")
	if code == ExitSuccess || !strings.Contains(msg, "--project") {
		t.Fatalf("error = %q (code %d), want a usage error naming --project", msg, code)
	}
}
