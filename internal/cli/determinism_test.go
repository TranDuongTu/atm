package cli

import (
	"strings"
	"testing"
)

// seedDeterminismStore seeds two projects and three tasks, returning the tasks'
// minted ids (in creation order: ATM's two, then DEMO's one). Born-v2 makes
// those ids stable hex aliases rather than the v1 ATM-0001 sequence, so callers
// must reference the captured values; the determinism guarantee (B1 seams) is
// exactly what makes them identical between the two seeded stores.
func seedDeterminismStore(h *goldenHarness) (atm1, atm2, demo1 string) {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "DEMO", "--name", "Demo", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:epic", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:status:open", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "DEMO:status:open", "--actor", "admin@cli:unset")
	out1, _, _ := h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Fix label reconciliation",
		"--label", "ATM:type:bug", "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	out2, _, _ := h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Seed index tasks",
		"--label", "ATM:context:agent", "--actor", "admin@cli:unset")
	out3, _, _ := h.run("task", "create", "--store", sp, "--project", "DEMO", "--title", "Demo task",
		"--label", "DEMO:status:open", "--actor", "admin@cli:unset")
	h.run("project", "set-name", "--store", sp, "--code", "DEMO", "--name", "Demo Project", "--actor", "admin@cli:unset")
	h.reset()
	return taskIDFromCreateJSON(h.t, out1), taskIDFromCreateJSON(h.t, out2), taskIDFromCreateJSON(h.t, out3)
}

func determinismReadCommands(atm1, atm2, demo1 string) [][]string {
	return [][]string{
		{"project", "list"},
		{"project", "show", "--code", "ATM"},
		{"project", "show", "--code", "DEMO"},
		{"label", "list"},
		{"label", "list", "--project", "ATM"},
		{"label", "list", "--project", "ATM", "--namespace", "type"},
		{"task", "list"},
		{"task", "list", "--project", "ATM"},
		{"task", "list", "--project", "DEMO"},
		{"task", "list", "--project", "ATM", "--label", "ATM:status:*", "--facets"},
		{"task", "show", "--id", atm1},
		{"task", "show", "--id", atm2},
		{"task", "show", "--id", demo1},
		{"conventions"},
	}
}

func TestDeterminismByteIdentical(t *testing.T) {
	h1 := newGoldenHarness(t)
	atm1, atm2, demo1 := seedDeterminismStore(h1)
	h2 := newGoldenHarness(t)
	atm1b, atm2b, demo1b := seedDeterminismStore(h2)
	// The two independently seeded stores must mint identical aliases — the
	// determinism guarantee. If they diverge, the read commands below would
	// target different ids and the byte-identical check would be meaningless.
	if atm1 != atm1b || atm2 != atm2b || demo1 != demo1b {
		t.Fatalf("seeded ids diverged between stores: h1=(%s,%s,%s) h2=(%s,%s,%s)", atm1, atm2, demo1, atm1b, atm2b, demo1b)
	}

	for _, args := range determinismReadCommands(atm1, atm2, demo1) {
		out1, _, code1 := h1.run(args...)
		if code1 != 0 {
			t.Fatalf("%v: first run exit = %d stderr=%s", args, code1, h1.stderr.String())
		}
		out2, _, code2 := h2.run(args...)
		if code2 != 0 {
			t.Fatalf("%v: second run exit = %d stderr=%s", args, code2, h2.stderr.String())
		}
		n1, n2 := normalizeOutput(out1), normalizeOutput(out2)
		if n1 != n2 {
			t.Fatalf("determinism violated for %v\n--- run1 ---\n%s\n--- run2 ---\n%s", args, n1, n2)
		}
		name := "determinism-" + sanitizedDeterminismPath(args)
		compareGolden(t, name, out1)
	}
}

func sanitizedDeterminismPath(args []string) string {
	out := ""
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		if a == "" {
			continue
		}
		if out != "" {
			out += "-"
		}
		out += a
	}
	if out == "" {
		out = "root"
	}
	return out
}
