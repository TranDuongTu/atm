package cli

import (
	"strings"
	"testing"
)

// runWorkflowRPIErrText drives root.Execute() directly and returns the
// resulting error's message text plus the exit code — gate errors surface as
// the returned error, not on the captured stderr buffer. See the
// runChecklistErrText comment in checklist_test.go.
func runWorkflowRPIErrText(t *testing.T, h *testCLI, args ...string) (string, int) {
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

// createRPITask mints a task and returns its id. The registry harness runs in
// text mode (that is what the RPI assertions below read), so the create call
// alone flips to JSON — the minted hex alias is only machine-readable there.
func createRPITask(t *testing.T, h *testCLI, title string) string {
	t.Helper()
	prev := h.output
	h.output = outputJSON
	out, stderr, code := h.run("task", "create", "--project", "ATM", "--title", title, "--actor", "admin@cli:unset")
	h.output = prev
	if code != 0 {
		t.Fatalf("create task %q: exit=%d stderr=%s", title, code, stderr)
	}
	return taskIDFromCreateJSON(t, out)
}

func TestWorkflowRPICLIHappyPath(t *testing.T) {
	h := newRegistryTestCLI(t)
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	productID := createRPITask(t, h, "Roadmap item")
	workID := createRPITask(t, h, "Build item")
	_, _, code := h.run("capability", "workflow_rpi", "product", "--task", productID, "--status", "clarified", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("product exit %d", code)
	}
	_, _, code = h.run("capability", "workflow_rpi", "pipeline", "--task", workID, "--product", productID, "--status", "planned", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("pipeline exit %d", code)
	}
	out, _, code := h.run("capability", "workflow_rpi", "links", "--task", productID)
	if code != 0 {
		t.Fatalf("links exit %d", code)
	}
	if !strings.Contains(out, "pipeline_child") || !strings.Contains(out, workID) {
		t.Fatalf("links output missing pipeline child %s: %s", workID, out)
	}
	out, _, code = h.run("capability", "workflow_rpi", "report", "--project", "ATM")
	if code != 0 {
		t.Fatalf("report exit %d", code)
	}
	if !strings.Contains(out, "backlog") || !strings.Contains(out, "product") || !strings.Contains(out, "pipeline") {
		t.Fatalf("report missing sections: %s", out)
	}
}

func TestWorkflowRPICLIRejectsPipelineWithoutProductParent(t *testing.T) {
	h := newRegistryTestCLI(t)
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	parentID := createRPITask(t, h, "Plain parent")
	workID := createRPITask(t, h, "Build item")
	errText, code := runWorkflowRPIErrText(t, h, "capability", "workflow_rpi", "pipeline", "--task", workID, "--product", parentID, "--actor", "admin@cli:unset")
	if code == 0 || !strings.Contains(errText, "product parent") {
		t.Fatalf("pipeline with non-product parent code=%d err=%s", code, errText)
	}
}

// TestWorkflowRPICLIReleaseReturnsToBacklog covers the other half of the lane
// cycle: release withdraws only this capability's state and says why.
func TestWorkflowRPICLIReleaseReturnsToBacklog(t *testing.T) {
	h := newRegistryTestCLI(t)
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	productID := createRPITask(t, h, "Roadmap item")
	workID := createRPITask(t, h, "Build item")
	h.run("capability", "workflow_rpi", "product", "--task", productID, "--actor", "admin@cli:unset")
	h.run("capability", "workflow_rpi", "pipeline", "--task", workID, "--product", productID, "--actor", "admin@cli:unset")

	out, _, code := h.run("capability", "workflow_rpi", "release", "--task", workID, "--reason", "not ours to build", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("release exit %d: %s", code, out)
	}
	// task show and task comment list render text straight to os.Stdout.
	out = runArgsStdoutOut(t, h, "task", "show", "--task", workID)
	if strings.Contains(out, ":rpi") {
		t.Errorf("released task still carries RPI labels: %s", out)
	}
	out = runArgsStdoutOut(t, h, "task", "comment", "list", "--task", workID)
	if !strings.Contains(out, "not ours to build") {
		t.Errorf("release reason not recorded as a comment: %s", out)
	}
}
