package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// The machine-global persona store is pruned (plan §7): `persona create`
// and `persona remove` are gone, and list/show outside a project carry the
// built-ins only. A persona someone wants on a machine goes into a project
// with `atm persona set`.
func TestPersonaListShowAreBuiltinOnlyOutsideAProject(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()

	out, _, code := h.run("persona", "list", "--store", sp, "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("list: code=%d", code)
	}
	var listed struct {
		Personas []struct{ Name string } `json:"personas"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Personas) != 3 {
		t.Fatalf("built-ins only: %s", out)
	}
	for _, want := range []string{"developer", "manager", "admin"} {
		found := false
		for _, p := range listed.Personas {
			if p.Name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("list missing built-in %s: %s", want, out)
		}
	}

	// The removed verbs are gone from the command tree. Cobra answers an
	// unknown subcommand with the parent's help and a zero exit, so absence
	// is asserted against the help text, not the exit code.
	help, _, code := h.run("persona", "-h", "--store", sp)
	if code != 0 {
		t.Fatalf("persona -h: code=%d", code)
	}
	for _, gone := range []string{"create", "remove"} {
		if strings.Contains(help, "\n  "+gone+"\t") {
			t.Fatalf("persona %s must be gone from the command tree (plan §7):\n%s", gone, help)
		}
	}
}

func TestPersonaShowPositional(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.output = outputText

	out, se, code := h.run("persona", "show", "--store", sp, "manager", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("show manager: code=%d stderr=%s", code, se)
	}
	if !strings.Contains(out, "manager\t") {
		t.Fatalf("show manager output missing name line: %s", out)
	}
	if strings.Contains(out, "modes:") {
		t.Fatalf("manager declares no modes; output must not contain a modes line: %s", out)
	}
}
