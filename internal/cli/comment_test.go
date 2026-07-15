package cli

import (
	"encoding/json"
	"testing"
)

// commentIDFromCreateJSON extracts the "id" field from a `task comment add`
// JSON envelope, so tests can capture a just-created comment's id instead of
// hardcoding it.
func commentIDFromCreateJSON(t *testing.T, out string) string {
	t.Helper()
	var env struct {
		Comment struct {
			ID string `json:"id"`
		} `json:"comment"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parse comment id from create output %q: %v", out, err)
	}
	if env.Comment.ID == "" {
		t.Fatalf("no comment id in create output: %q", out)
	}
	return env.Comment.ID
}

func TestGoldenCommentAdd(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	out, _, code := h.run("task", "comment", "add", "--task", tk1, "--body", "First comment",
		"--label", "ATM:comment:open-question", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-add", out)
}

func TestGoldenCommentList(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	h.run("task", "comment", "add", "--task", tk1, "--body", "First", "--actor", "admin@cli:unset")
	h.run("task", "comment", "add", "--task", tk1, "--body", "Second",
		"--label", "ATM:comment:clarification", "--actor", "admin@cli:unset")
	out, _, code := h.run("task", "comment", "list", "--task", tk1)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-list", out)
}

func TestGoldenCommentShow(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	cOut, _, _ := h.run("task", "comment", "add", "--task", tk1, "--body", "Body here", "--actor", "admin@cli:unset")
	c1 := commentIDFromCreateJSON(t, cOut)
	out, _, code := h.run("task", "comment", "show", "--id", c1)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-show", out)
}

func TestGoldenCommentSetBody(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	cOut, _, _ := h.run("task", "comment", "add", "--task", tk1, "--body", "orig", "--actor", "admin@cli:unset")
	c1 := commentIDFromCreateJSON(t, cOut)
	out, _, code := h.run("task", "comment", "set-body", "--id", c1, "--body", "new", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-set-body", out)
}

func TestGoldenCommentLabelAddRemove(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	cOut, _, _ := h.run("task", "comment", "add", "--task", tk1, "--body", "x", "--actor", "admin@cli:unset")
	c1 := commentIDFromCreateJSON(t, cOut)
	outAdd, _, code := h.run("task", "comment", "label", "add", "--id", c1,
		"--label", "ATM:comment:open-question", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("add exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-label-add", outAdd)
	outRem, _, code := h.run("task", "comment", "label", "remove", "--id", c1,
		"--label", "ATM:comment:open-question", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("remove exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-label-remove", outRem)
}

func TestGoldenCommentRemove(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	cOut, _, _ := h.run("task", "comment", "add", "--task", tk1, "--body", "gone", "--actor", "admin@cli:unset")
	c1 := commentIDFromCreateJSON(t, cOut)
	out, _, code := h.run("task", "comment", "remove", "--id", c1, "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-remove", out)
}

func TestCommentAddRejectsUnregisteredPersona(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	_, _, code := h.run("task", "comment", "add", "--task", tk1, "--body", "x", "--actor", "ghost@cli:unset")
	if code != ExitUsage {
		t.Fatalf("expected exit 2 (unregistered persona), got %d", code)
	}
}

func TestCommentListEmpty(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	out, _, code := h.run("task", "comment", "list", "--task", tk1)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-list-empty", out)
}
