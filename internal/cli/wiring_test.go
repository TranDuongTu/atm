package cli

import (
	"strings"
	"testing"
)

// runWiringErrText drives root.Execute() directly and returns the resulting
// error's message plus the exit code — gate errors surface as the returned
// error, not on the captured stderr buffer.
func runWiringErrText(t *testing.T, h *testCLI, args ...string) (string, int) {
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

func newWiringCLI(t *testing.T) *testCLI {
	t.Helper()
	h := newRegistryTestCLI(t)
	if _, _, code := h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("project create exit %d", code)
	}
	return h
}

func inboxExpr(t *testing.T, h *testCLI, board string) string {
	t.Helper()
	prev := h.output
	h.output = outputJSON
	out, stderr, code := h.run("label", "show", "--name", board)
	h.output = prev
	if code != 0 {
		t.Fatalf("label show %s: exit=%d stderr=%s", board, code, stderr)
	}
	return out
}

// The whole point of storing wiring as a board expression: it is ordinary
// label data, so `atm label show` can read it back.
func TestWiringSetWritesTheEligibilityAndKeepsTheTail(t *testing.T) {
	h := newWiringCLI(t)
	out, stderr, code := h.run("project", "wiring", "set",
		"--project", "ATM", "--capability", "qa",
		"--expr", "scrum-stage:done AND (scrum:task OR scrum:bug)",
		"--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("wiring set: exit=%d stderr=%s", code, stderr)
	}
	want := "(scrum-stage:done AND (scrum:task OR scrum:bug)) AND NOT qa:* AND NOT qa-out:*"
	if !strings.Contains(out, want) {
		t.Fatalf("output = %q, want it to contain %q", out, want)
	}
	if got := inboxExpr(t, h, "ATM:qa-inbox"); !strings.Contains(got, "NOT qa:*") || !strings.Contains(got, "scrum-stage:done") {
		t.Fatalf("stored expr = %s", got)
	}
}

// Only the one board moves: wiring qa must not touch scrum's lane, or anyone
// else's labels.
func TestWiringSetTouchesOnlyThatInboxBoard(t *testing.T) {
	h := newWiringCLI(t)
	before := inboxExpr(t, h, "ATM:scrum-inbox")
	if _, _, code := h.run("project", "wiring", "set", "--project", "ATM", "--capability", "qa",
		"--expr", "scrum-stage:done", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatal("wiring set failed")
	}
	if after := inboxExpr(t, h, "ATM:scrum-inbox"); after != before {
		t.Fatalf("scrum's inbox moved: %s -> %s", before, after)
	}
	for _, board := range []string{"ATM:qa-pipeline", "ATM:qa-out-board"} {
		if got := inboxExpr(t, h, board); strings.Contains(got, "scrum-stage:done") {
			t.Fatalf("%s picked up the eligibility: %s", board, got)
		}
	}
}

// A registry capability has no inbox, and the error should say that rather
// than fail later on a missing lane.
func TestWiringSetRefusesARegistryCapability(t *testing.T) {
	h := newWiringCLI(t)
	msg, code := runWiringErrText(t, h, "project", "wiring", "set",
		"--project", "ATM", "--capability", "release", "--expr", "x", "--actor", "admin@cli:unset")
	if code == 0 || !strings.Contains(msg, "not a flow capability") {
		t.Fatalf("err = %q (exit %d)", msg, code)
	}
}

func TestWiringSetRefusesAnUnknownCapability(t *testing.T) {
	h := newWiringCLI(t)
	msg, code := runWiringErrText(t, h, "project", "wiring", "set",
		"--project", "ATM", "--capability", "nope", "--expr", "x", "--actor", "admin@cli:unset")
	if code == 0 || !strings.Contains(msg, "no flow capability") {
		t.Fatalf("err = %q (exit %d)", msg, code)
	}
}

// The store parses the expression on write, so a broken wiring is refused
// before it can hide every task from an inbox.
func TestWiringSetRefusesAnUnparseableExpression(t *testing.T) {
	h := newWiringCLI(t)
	msg, code := runWiringErrText(t, h, "project", "wiring", "set",
		"--project", "ATM", "--capability", "qa", "--expr", "scrum-stage:done AND (", "--actor", "admin@cli:unset")
	if code == 0 {
		t.Fatalf("an unparseable expression was accepted (%q)", msg)
	}
	if got := inboxExpr(t, h, "ATM:qa-inbox"); strings.Contains(got, "scrum-stage:done") {
		t.Fatalf("the refused expression was stored anyway: %s", got)
	}
}

// default + tail must equal the registry's unclaimed pool, without saying the
// capability's own exclusion twice.
func TestWiringDefaultIsTheUnclaimedPool(t *testing.T) {
	h := newWiringCLI(t)
	out, stderr, code := h.run("project", "wiring", "default",
		"--project", "ATM", "--capability", "scrum", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("wiring default: exit=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{"NOT qa:*", "NOT codereview:*", "NOT scrum:*", "NOT scrum-out:*"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output = %q, want it to contain %q", out, want)
		}
	}
	if strings.Count(out, "NOT scrum:*") != 1 {
		t.Fatalf("the capability's own exclusion is repeated: %q", out)
	}
}

func TestWiringShowSplitsEligibilityFromTheTail(t *testing.T) {
	h := newWiringCLI(t)
	if _, _, code := h.run("project", "wiring", "set", "--project", "ATM", "--capability", "codereview",
		"--expr", "scrum-stage:done AND scrum:task", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatal("wiring set failed")
	}
	out, stderr, code := h.run("project", "wiring", "show", "--project", "ATM")
	if code != 0 {
		t.Fatalf("wiring show: exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(out, "codereview\tATM:codereview-inbox\teligibility: scrum-stage:done AND scrum:task") {
		t.Fatalf("show output = %q", out)
	}
	// An untouched capability reports the seeded state honestly.
	if !strings.Contains(out, "eligibility: (none — only unclaimed work)") {
		t.Fatalf("show output = %q", out)
	}
	// Registry capabilities have no row: they have no inbox to wire.
	if strings.Contains(out, "release") {
		t.Fatalf("a registry capability appeared in the wiring report: %q", out)
	}
}

// A board somebody edited by hand is reported as-is rather than split into a
// shape it does not have.
func TestWiringShowReportsAHandEditedBoard(t *testing.T) {
	h := newWiringCLI(t)
	if _, _, code := h.run("label", "add", "--name", "ATM:qa-inbox", "--expr", "scrum-stage:done", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatal("label add failed")
	}
	out, _, code := h.run("project", "wiring", "show", "--project", "ATM")
	if code != 0 {
		t.Fatalf("wiring show exit %d", code)
	}
	if !strings.Contains(out, "hand-edited: scrum-stage:done") {
		t.Fatalf("show output = %q", out)
	}
}
