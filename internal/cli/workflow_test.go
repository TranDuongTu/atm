package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// seedWorkflowProject boots a store with project ATM, returns its store path.
// The golden harness defaults to JSON output, so `task create` returns JSON
// and task ids are extracted with taskIDFromCreateJSON (already defined in
// harness_test.go).
func seedWorkflowProject(t *testing.T, h *goldenHarness) string {
	t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	return sp
}

func createTaskWithLabels(t *testing.T, h *goldenHarness, sp, title string, labels ...string) string {
	t.Helper()
	args := []string{"task", "create", "--store", sp, "--project", "ATM", "--title", title, "--actor", "admin@cli:unset"}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	out, _, code := h.run(args...)
	if code != 0 {
		t.Fatalf("task create exit=%d stderr=%s", code, h.stderr.String())
	}
	return taskIDFromCreateJSON(t, out)
}

func TestWorkflowStartSwapsStatus(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "jotting", "ATM:status:open")

	out, stderr, code := h.run("workflow", "start", "--store", sp, "--task", id, "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	// Swap semantics: the target must be present AND the prior status gone.
	// Asserting only the target would pass even if BOTH labels survived,
	// which is exactly the bug the swap exists to prevent.
	if !strings.Contains(out, "ATM:status:in-progress") {
		t.Fatalf("output missing ATM:status:in-progress: %s", out)
	}
	if strings.Contains(out, "ATM:status:open") {
		t.Fatalf("prior status ATM:status:open survived the swap: %s", out)
	}
}

func TestWorkflowStartRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t")

	_, _, code := h.run("workflow", "start", "--store", sp, "--task", id)
	if code == 0 {
		t.Fatal("expected non-zero exit when --actor missing on mutating verb")
	}
}

func TestWorkflowStatusReporter(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t", "ATM:status:done")

	out, _, code := h.run("workflow", "status", "--store", sp, "--task", id)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	// Parse the envelope rather than substring-matching: a bare
	// strings.Contains(out, "done") would also pass on a task id containing
	// "done".
	var env struct {
		Task   string `json:"task"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if env.Status != "done" {
		t.Fatalf("status = %q, want \"done\"", env.Status)
	}
}

func TestWorkflowStatusUntriaged(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t")

	out, _, code := h.run("workflow", "status", "--store", sp, "--task", id)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	// The CLI pretty-prints JSON (store.MarshalSorted uses SetIndent("", "  ")),
	// so a substring match on `"status":""` would NOT match the real output.
	// Parse the envelope and assert the field instead.
	var env struct {
		Task   string `json:"task"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if env.Status != "" {
		t.Fatalf("expected status \"\" for untriaged, got %q", env.Status)
	}
}

func TestWorkflowStatusReporterIsReadOnly(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t", "ATM:status:open")

	before, err := h.store.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq before: %v", err)
	}
	if _, _, code := h.run("workflow", "status", "--store", sp, "--task", id); code != 0 {
		t.Fatalf("status exit=%d stderr=%s", code, h.stderr.String())
	}
	after, err := h.store.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq after: %v", err)
	}
	if before != after {
		t.Fatalf("workflow status advanced log seq %d -> %d — reporter must be read-only", before, after)
	}
}

func TestWorkflowSeedEnsuresAllThreeBoards(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	_, _, code := h.run("workflow", "seed", "--store", sp, "--project", "ATM", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("seed exit=%d stderr=%s", code, h.stderr.String())
	}
	for _, name := range []string{"ATM:backlog", "ATM:open-tasks", "ATM:in-progress-tasks"} {
		if _, err := h.store.LabelShow(name); err != nil {
			t.Errorf("%s not ensured by workflow seed: %v", name, err)
		}
	}
}

func TestWorkflowSeedIdempotent(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	if _, _, code := h.run("workflow", "seed", "--store", sp, "--project", "ATM", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("first seed exit=%d stderr=%s", code, h.stderr.String())
	}
	_, _, code := h.run("workflow", "seed", "--store", sp, "--project", "ATM", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatal("second seed exited non-zero")
	}
}

func TestWorkflowSeedRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	_, _, code := h.run("workflow", "seed", "--store", sp, "--project", "ATM")
	if code == 0 {
		t.Fatal("expected non-zero exit when --actor missing on seed")
	}
}

func TestWorkflowCompleteSwapsToDone(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t", "ATM:status:in-progress")

	out, _, code := h.run("workflow", "complete", "--store", sp, "--task", id, "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "ATM:status:done") {
		t.Fatalf("output missing ATM:status:done: %s", out)
	}
	if strings.Contains(out, "ATM:status:in-progress") {
		t.Fatalf("prior status ATM:status:in-progress survived the swap: %s", out)
	}
}

// TestWorkflowStartAlreadyInProgressIsNoop covers the spec requirement that a
// verb targeting the task's current sole status is a no-op with an "already
// <status>" text message, not a transition line that reads as if something
// happened (e.g. "status done -> done"). Text mode is required here since the
// message in question is the text-mode line, not the JSON envelope.
func TestWorkflowStartAlreadyInProgressIsNoop(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t", "ATM:status:in-progress")

	before, err := h.store.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq before: %v", err)
	}
	h.output = outputText
	out, stderr, code := h.run("workflow", "start", "--store", sp, "--task", id, "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	after, err := h.store.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq after: %v", err)
	}
	if before != after {
		t.Fatalf("no-op start advanced log seq %d -> %d", before, after)
	}
	want := id + ": already in-progress\n"
	if out != want {
		t.Fatalf("no-op text message = %q, want %q", out, want)
	}
	if strings.Contains(out, "->") {
		t.Fatalf("no-op message should not read like a transition: %q", out)
	}
}

func TestBacklogBoardSurfacesNakedJotting(t *testing.T) {
	// The driver: a task created with no labels carries no status:* label, so
	// it is invisible under every board in the ring. The backlog board
	// (NOT status:*) is what surfaces it again. This asserts board MEMBERSHIP
	// resolves, not merely that the label row exists.
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	jotting := createTaskWithLabels(t, h, sp, "quick jotting")           // no labels
	tracked := createTaskWithLabels(t, h, sp, "tracked", "ATM:status:open")

	out, _, code := h.run("task", "list", "--store", sp, "--project", "ATM", "--label", "ATM:backlog")
	if code != 0 {
		t.Fatalf("task list --label ATM:backlog exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, jotting) {
		t.Errorf("naked jotting %s missing from the backlog board: %s", jotting, out)
	}
	if strings.Contains(out, tracked) {
		t.Errorf("task %s carries status:open and must NOT be in backlog (NOT status:*): %s", tracked, out)
	}

	out, _, code = h.run("task", "list", "--store", sp, "--project", "ATM", "--label", "ATM:open-tasks")
	if code != 0 {
		t.Fatalf("task list --label ATM:open-tasks exit=%d stderr=%s", code, h.stderr.String())
	}
	if strings.Contains(out, jotting) {
		t.Errorf("naked jotting %s must NOT appear under open-tasks (status:open): %s", jotting, out)
	}
	if !strings.Contains(out, tracked) {
		t.Errorf("task %s carries status:open and must appear under open-tasks: %s", tracked, out)
	}
}

func TestInProgressBoardMembership(t *testing.T) {
	// The in-progress-tasks board is the other new ring member. Assert it
	// resolves, and that `atm workflow start` moves a task onto it -- the
	// capability's verbs and its boards agreeing end to end.
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t", "ATM:status:open")

	if _, _, code := h.run("workflow", "start", "--store", sp, "--task", id, "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("workflow start exit=%d stderr=%s", code, h.stderr.String())
	}
	out, _, code := h.run("task", "list", "--store", sp, "--project", "ATM", "--label", "ATM:in-progress-tasks")
	if code != 0 {
		t.Fatalf("task list exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, id) {
		t.Errorf("task %s is in-progress and must appear under in-progress-tasks: %s", id, out)
	}
	out, _, _ = h.run("task", "list", "--store", sp, "--project", "ATM", "--label", "ATM:open-tasks")
	if strings.Contains(out, id) {
		t.Errorf("task %s left status:open and must no longer appear under open-tasks: %s", id, out)
	}
}
