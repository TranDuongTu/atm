package cli

import "testing"

func TestGoldenCommentAdd(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "First comment",
		"--label", "ATM:comment:open-question", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-add", out)
}

func TestGoldenCommentList(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "First", "--actor", "claude")
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "Second",
		"--label", "ATM:comment:clarification", "--actor", "claude")
	out, _, code := h.run("task", "comment", "list", "--task", "ATM-0001")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-list", out)
}

func TestGoldenCommentShow(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "Body here", "--actor", "claude")
	out, _, code := h.run("task", "comment", "show", "--id", "ATM-0001-c0001")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-show", out)
}

func TestGoldenCommentSetBody(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "orig", "--actor", "claude")
	out, _, code := h.run("task", "comment", "set-body", "--id", "ATM-0001-c0001", "--body", "new", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-set-body", out)
}

func TestGoldenCommentLabelAddRemove(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "x", "--actor", "claude")
	outAdd, _, code := h.run("task", "comment", "label", "add", "--id", "ATM-0001-c0001",
		"--label", "ATM:comment:open-question", "--actor", "claude")
	if code != 0 {
		t.Fatalf("add exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-label-add", outAdd)
	outRem, _, code := h.run("task", "comment", "label", "remove", "--id", "ATM-0001-c0001",
		"--label", "ATM:comment:open-question", "--actor", "claude")
	if code != 0 {
		t.Fatalf("remove exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-label-remove", outRem)
}

func TestGoldenCommentRemove(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "gone", "--actor", "claude")
	out, _, code := h.run("task", "comment", "remove", "--id", "ATM-0001-c0001", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-remove", out)
}

func TestCommentAddRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	_, _, code := h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "x")
	if code != ExitUsage {
		t.Fatalf("expected exit 2 (missing actor), got %d", code)
	}
}

func TestCommentListEmpty(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "comment", "list", "--task", "ATM-0001")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-list-empty", out)
}
