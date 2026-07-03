package cli

import (
	"strings"
	"testing"
)

func seedDeterminismStore(h *goldenHarness) {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "DEMO", "--name", "Demo", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:epic", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:status:open", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "DEMO:status:open", "--actor", "claude")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Fix label reconciliation",
		"--label", "ATM:type:bug", "--label", "ATM:status:open", "--actor", "claude")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Seed index tasks",
		"--label", "ATM:context:agent", "--actor", "claude")
	h.run("task", "create", "--store", sp, "--project", "DEMO", "--title", "Demo task",
		"--label", "DEMO:status:open", "--actor", "claude")
	h.run("project", "set-name", "--store", sp, "--code", "DEMO", "--name", "Demo Project", "--actor", "claude")
	h.reset()
}

func determinismReadCommands() [][]string {
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
		{"task", "show", "--id", "ATM-0001"},
		{"task", "show", "--id", "ATM-0002"},
		{"task", "show", "--id", "DEMO-0001"},
		{"conventions"},
	}
}

func TestDeterminismByteIdentical(t *testing.T) {
	h1 := newGoldenHarness(t)
	seedDeterminismStore(h1)
	h2 := newGoldenHarness(t)
	seedDeterminismStore(h2)

	for _, args := range determinismReadCommands() {
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
