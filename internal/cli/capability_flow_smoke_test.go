package cli

import (
	"strings"
	"testing"
)

// This is the end-to-end proof that the flow contract composes: one task walks
// the whole road — pool -> scrum -> (qa AND codereview, independently) ->
// release — through nothing but the shipped CLI verbs and the project's own
// wiring. No capability names another in code anywhere along the way; the only
// thing joining them is the board expressions this test writes.

func newFlowCLI(t *testing.T) *testCLI {
	t.Helper()
	h := newRegistryTestCLI(t)
	_, stderr, code := h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management",
		"--capabilities", "scrum,qa,codereview,release", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("project create: exit=%d stderr=%s", code, stderr)
	}
	return h
}

func mustRun(t *testing.T, h *testCLI, args ...string) string {
	t.Helper()
	out, stderr, code := h.run(args...)
	if code != 0 {
		t.Fatalf("%v: exit=%d stderr=%s", args, code, stderr)
	}
	return out
}

func createFlowTask(t *testing.T, h *testCLI, title string) string {
	t.Helper()
	prev := h.output
	h.output = outputJSON
	out := mustRun(t, h, "task", "create", "--project", "ATM", "--title", title, "--actor", "admin@cli:unset")
	h.output = prev
	return taskIDFromCreateJSON(t, out)
}

// inLane asks the store which tasks the BOARD selects — the same query the
// pane makes. It runs in JSON mode because the text renderer for `task list`
// writes straight to os.Stdout rather than the state's writer.
func inLane(t *testing.T, h *testCLI, board, taskID string) bool {
	t.Helper()
	prev := h.output
	h.output = outputJSON
	out := mustRun(t, h, "task", "list", "--project", "ATM", "--label", board)
	h.output = prev
	return strings.Contains(out, taskID)
}

func TestFlowCapabilitiesComposeEndToEnd(t *testing.T) {
	h := newFlowCLI(t)
	actor := []string{"--actor", "admin@cli:unset"}

	// The project wires its own road. qa verifies buildable units; codereview
	// looks only at implementation tasks. Neither expression exists in code.
	mustRun(t, h, append([]string{"project", "wiring", "set", "--project", "ATM", "--capability", "qa",
		"--expr", "scrum-stage:done AND (scrum:task OR scrum:bug OR scrum:story)"}, actor...)...)
	mustRun(t, h, append([]string{"project", "wiring", "set", "--project", "ATM", "--capability", "codereview",
		"--expr", "scrum-stage:done AND scrum:task"}, actor...)...)

	work := createFlowTask(t, h, "OAuth login for the CLI")
	design := createFlowTask(t, h, "Design the OAuth flow")

	// Jotted work starts in the pool, which is scrum's inbox and nobody else's.
	if !inLane(t, h, "ATM:scrum-inbox", work) {
		t.Fatal("a jotted task must land in scrum's inbox")
	}
	if inLane(t, h, "ATM:qa-inbox", work) || inLane(t, h, "ATM:codereview-inbox", work) {
		t.Fatal("unfinished work must not reach a downstream inbox")
	}

	// scrum claims both, and finishes them.
	mustRun(t, h, append([]string{"capability", "scrum", "absorb", "--task", work, "--type", "task", "--stage", "implementing"}, actor...)...)
	mustRun(t, h, append([]string{"capability", "scrum", "absorb", "--task", design, "--type", "design", "--stage", "planned"}, actor...)...)
	if inLane(t, h, "ATM:scrum-inbox", work) {
		t.Fatal("an absorbed task must leave the inbox it came from")
	}
	mustRun(t, h, append([]string{"capability", "scrum", "stage", "--task", work, "--stage", "done"}, actor...)...)
	mustRun(t, h, append([]string{"capability", "scrum", "plan", "--task", design, "--path", "docs/plans/oauth.md"}, actor...)...)
	mustRun(t, h, append([]string{"capability", "scrum", "stage", "--task", design, "--stage", "done"}, actor...)...)

	// The moment it is done, the SAME task appears in both downstream inboxes:
	// two independent claims, no copies.
	if !inLane(t, h, "ATM:qa-inbox", work) {
		t.Fatal("finished work must reach qa's inbox")
	}
	if !inLane(t, h, "ATM:codereview-inbox", work) {
		t.Fatal("finished work must reach codereview's inbox")
	}

	// A design task never flows downstream — the wiring excludes it, and no
	// code anywhere has to know what a design task is.
	if inLane(t, h, "ATM:qa-inbox", design) || inLane(t, h, "ATM:codereview-inbox", design) {
		t.Fatal("a design task must never reach a downstream inbox")
	}

	// qa verifies through a scaffold; only the original is certified.
	mustRun(t, h, append([]string{"capability", "qa", "absorb", "--task", work}, actor...)...)
	prev := h.output
	h.output = outputJSON
	scOut := mustRun(t, h, append([]string{"capability", "qa", "scaffold", "--task", work, "--title", "staging run"}, actor...)...)
	h.output = prev
	scaffold := taskIDFromCreateJSON(t, scOut)

	if _, _, code := h.run(append([]string{"capability", "qa", "pass", "--task", work}, actor...)...); code == 0 {
		t.Fatal("an original with a live scaffold must not pass")
	}
	mustRun(t, h, append([]string{"capability", "qa", "pass", "--task", scaffold}, actor...)...)
	mustRun(t, h, append([]string{"capability", "qa", "pass", "--task", work}, actor...)...)

	// codereview runs its own road over the same task, independently.
	mustRun(t, h, append([]string{"capability", "codereview", "absorb", "--task", work, "--pr", "#142"}, actor...)...)
	mustRun(t, h, append([]string{"capability", "codereview", "begin", "--task", work}, actor...)...)
	mustRun(t, h, append([]string{"capability", "codereview", "finish", "--task", work, "--report", "docs/reviews/142.md"}, actor...)...)

	h.output = outputJSON
	shown := mustRun(t, h, "task", "show", "--task", work)
	h.output = prev
	for _, want := range []string{"ATM:scrum-stage:done", "ATM:qa:done", "ATM:codereview:done"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("task is missing %s: %s", want, shown)
		}
	}
	// The scaffold is certified by nobody: that is the originals-only rule
	// holding all the way through the real CLI.
	h.output = outputJSON
	scShown := mustRun(t, h, "task", "show", "--task", scaffold)
	h.output = prev
	if strings.Contains(scShown, "ATM:qa:done") {
		t.Fatalf("a scaffold was certified: %s", scShown)
	}

	// release assembles what is certified and ships it.
	h.output = outputJSON
	relOut := mustRun(t, h, append([]string{"capability", "release", "cut", "--project", "ATM", "--version", "v1.2"}, actor...)...)
	h.output = prev
	container := taskIDFromCreateJSON(t, relOut)

	mustRun(t, h, append([]string{"capability", "release", "include", "--task", work, "--release", container}, actor...)...)
	mustRun(t, h, append([]string{"capability", "release", "ship", "--release", container}, actor...)...)

	h.output = outputJSON
	shipped := mustRun(t, h, "task", "show", "--task", work)
	h.output = prev
	for _, want := range []string{"ATM:release:v1-2", "ATM:release:done"} {
		if !strings.Contains(shipped, want) {
			t.Fatalf("shipped member is missing %s: %s", want, shipped)
		}
	}
}

// Every capability's report must run clean over a converged project: a
// reporter that cannot read the state the verbs wrote is a broken reporter.
func TestEveryCapabilityReportsOverAConvergedProject(t *testing.T) {
	h := newFlowCLI(t)
	actor := []string{"--actor", "admin@cli:unset"}
	work := createFlowTask(t, h, "some work")
	mustRun(t, h, append([]string{"capability", "scrum", "absorb", "--task", work, "--type", "task", "--stage", "done"}, actor...)...)

	for _, name := range []string{"scrum", "qa", "codereview", "release"} {
		out, stderr, code := h.run("capability", name, "report", "--project", "ATM")
		if code != 0 {
			t.Fatalf("%s report: exit=%d stderr=%s", name, code, stderr)
		}
		if strings.Contains(out, "finding") && strings.Contains(out, "unparseable") {
			t.Fatalf("%s report found unreadable state on a converged project: %s", name, out)
		}
	}
	// And every guide is served, since that is the only discovery surface.
	// The guide is GENERATED from the capability's declarations, so these
	// are the sections a definition always produces.
	for _, name := range []string{"scrum", "qa", "codereview", "release"} {
		out := mustRun(t, h, "capability", name, "guide")
		for _, want := range []string{"capability — definition", "## Axes", "## Actions", "## Converged"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s guide missing %s", name, want)
			}
		}
	}
	// Lanes and sockets are the FLOW half of the vocabulary; a registry
	// capability must not claim either, and the guide is where that shows.
	for _, name := range []string{"scrum", "qa", "codereview"} {
		out := mustRun(t, h, "capability", name, "guide")
		for _, want := range []string{"## Lanes", "## Sockets", "FINISH:", "EVICT:"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s is a flow but its guide is missing %s", name, want)
			}
		}
	}
	if out := mustRun(t, h, "capability", "release", "guide"); strings.Contains(out, "## Lanes") || strings.Contains(out, "## Sockets") {
		t.Fatalf("release is a registry capability; its guide must declare no lanes or sockets:\n%s", out)
	}
}
