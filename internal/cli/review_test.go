package cli

import (
	"strings"
	"testing"
)

func seedScenario5(h *goldenHarness) {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management",
		"--label", "type:epic", "--label", "type:impl", "--label", "type:bug",
		"--label", "area:cli", "--label", "kind:convention",
		"--type-axis", "type", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Epic: agent workflow",
		"--label", "type:epic", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Impl: claim command",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "claim", "--store", sp, "--id", "ATM-0002", "--actor", "agent:claude-1")
	h.run("review", "request", "--store", sp, "--id", "ATM-0002", "--actor", "agent:claude-1")
	h.reset()
}

func TestGoldenReviewQueueGroupedByClaimant(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "A",
		"--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "T1",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "T2",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "T3",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "claim", "--store", sp, "--id", "ATM-0001", "--actor", "agent:claude-1")
	h.run("task", "claim", "--store", sp, "--id", "ATM-0002", "--actor", "agent:claude-2")
	h.run("task", "claim", "--store", sp, "--id", "ATM-0003", "--actor", "agent:claude-1")
	for _, id := range []string{"ATM-0001", "ATM-0002", "ATM-0003"} {
		h.run("task", "set-status", "--store", sp, "--id", id, "--status", "in-progress",
			"--actor", "agent:claude-1")
		h.run("review", "request", "--store", sp, "--id", id, "--actor", "agent:claude-1")
	}

	out, _, code := h.run("review", "queue", "--project", "ATM")
	if code != 0 {
		t.Fatalf("review queue exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "review-queue-grouped", out)

	if !strings.Contains(out, `"claimant": "agent:claude-1"`) {
		t.Fatalf("expected group for agent:claude-1, got %s", out)
	}
	if !strings.Contains(out, `"claimant": "agent:claude-2"`) {
		t.Fatalf("expected group for agent:claude-2, got %s", out)
	}
	if !strings.Contains(out, `"id": "ATM-0001"`) {
		t.Fatalf("expected ATM-0001 in queue, got %s", out)
	}
	if !strings.Contains(out, `"id": "ATM-0002"`) {
		t.Fatalf("expected ATM-0002 in queue, got %s", out)
	}
	if !strings.Contains(out, `"id": "ATM-0003"`) {
		t.Fatalf("expected ATM-0003 in queue, got %s", out)
	}
}

func TestGoldenReviewApproveTransitionsAndComment(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario5(h)
	sp := h.store.StorePath()

	out, _, code := h.run("review", "approve", "--store", sp, "--id", "ATM-0002",
		"--comment", "Looks good", "--actor", "human:alice")
	if code != 0 {
		t.Fatalf("review approve exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "review-approve", out)

	if !strings.Contains(out, `"status": "done"`) {
		t.Fatalf("expected status done, got %s", out)
	}
	if !strings.Contains(out, `"text": "Looks good"`) {
		t.Fatalf("expected discussion comment text, got %s", out)
	}
	if !strings.Contains(out, `"author": "human:alice"`) {
		t.Fatalf("expected discussion author human:alice, got %s", out)
	}
	if !strings.Contains(out, `"action": "approved"`) {
		t.Fatalf("expected approved history entry, got %s", out)
	}
}

func TestGoldenReviewRejectTransitionsAndComment(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario5(h)
	sp := h.store.StorePath()

	out, _, code := h.run("review", "reject", "--store", sp, "--id", "ATM-0002",
		"--comment", "Please fix tests", "--actor", "human:alice")
	if code != 0 {
		t.Fatalf("review reject exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "review-reject", out)

	if !strings.Contains(out, `"status": "in-progress"`) {
		t.Fatalf("expected status in-progress, got %s", out)
	}
	if !strings.Contains(out, `"text": "Please fix tests"`) {
		t.Fatalf("expected discussion comment text, got %s", out)
	}
	if !strings.Contains(out, `"author": "human:alice"`) {
		t.Fatalf("expected discussion author human:alice, got %s", out)
	}
	if !strings.Contains(out, `"action": "rejected"`) {
		t.Fatalf("expected rejected history entry, got %s", out)
	}
}

func TestGoldenReviewDashboard(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "A",
		"--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "T",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "claim", "--store", sp, "--id", "ATM-0001", "--actor", "agent:claude-1")
	h.run("task", "set-status", "--store", sp, "--id", "ATM-0001", "--status", "in-progress",
		"--actor", "agent:claude-1")
	h.run("review", "request", "--store", sp, "--id", "ATM-0001", "--actor", "agent:claude-1")
	h.run("task", "followup", "add", "--store", sp, "--id", "ATM-0001", "--text", "fu",
		"--assignee", "human:alice", "--actor", "human:alice")

	out, _, code := h.run("review", "dashboard", "--project", "ATM")
	if code != 0 {
		t.Fatalf("dashboard exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "review-dashboard", out)

	if !strings.Contains(out, `"project": "ATM"`) {
		t.Fatalf("expected project ATM, got %s", out)
	}
	if !strings.Contains(out, `"review_queue"`) {
		t.Fatalf("expected review_queue, got %s", out)
	}
	if !strings.Contains(out, `"open_followups"`) {
		t.Fatalf("expected open_followups, got %s", out)
	}
	if !strings.Contains(out, `"guide_status"`) {
		t.Fatalf("expected guide_status, got %s", out)
	}
}

func TestGoldenScenario5Integration(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario5(h)
	sp := h.store.StorePath()

	outQueue, _, code := h.run("review", "queue", "--project", "ATM")
	if code != 0 {
		t.Fatalf("Scenario 5: review queue exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(outQueue, `"claimant": "agent:claude-1"`) {
		t.Fatalf("Scenario 5: expected claimant agent:claude-1, got %s", outQueue)
	}
	if !strings.Contains(outQueue, `"id": "ATM-0002"`) {
		t.Fatalf("Scenario 5: queue should contain ATM-0002, got %s", outQueue)
	}

	if _, _, code := h.run("review", "approve", "--store", sp, "--id", "ATM-0002",
		"--comment", "Looks good", "--actor", "human:alice"); code != 0 {
		t.Fatalf("Scenario 5: approve exit = %d stderr=%s", code, h.stderr.String())
	}

	outShow, _, code := h.run("task", "show", "--id", "ATM-0002")
	if code != 0 {
		t.Fatalf("Scenario 5: task show exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(outShow, `"status": "done"`) {
		t.Fatalf("Scenario 5: expected status done, got %s", outShow)
	}
	if !strings.Contains(outShow, `"action": "approved"`) {
		t.Fatalf("Scenario 5: expected approved history entry, got %s", outShow)
	}
	if !strings.Contains(outShow, `"actor": "human:alice"`) {
		t.Fatalf("Scenario 5: expected approved by human:alice, got %s", outShow)
	}
}

func TestGoldenActorListAndShow(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "A",
		"--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "T",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "claim", "--store", sp, "--id", "ATM-0001", "--actor", "agent:claude-1")
	h.run("task", "followup", "add", "--store", sp, "--id", "ATM-0001", "--text", "fu",
		"--assignee", "human:alice", "--actor", "human:alice")

	outList, _, code := h.run("actor", "list")
	if code != 0 {
		t.Fatalf("actor list exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "actor-list", outList)
	if !strings.Contains(outList, `"id": "agent:claude-1"`) {
		t.Fatalf("expected agent:claude-1, got %s", outList)
	}
	if !strings.Contains(outList, `"id": "human:alice"`) {
		t.Fatalf("expected human:alice, got %s", outList)
	}

	outShow, _, code := h.run("actor", "show", "--id", "agent:claude-1")
	if code != 0 {
		t.Fatalf("actor show exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "actor-show", outShow)
	if !strings.Contains(outShow, `"id": "agent:claude-1"`) {
		t.Fatalf("expected actor id, got %s", outShow)
	}
	if !strings.Contains(outShow, `"claimed_tasks": [`) {
		t.Fatalf("expected claimed_tasks, got %s", outShow)
	}
	if !strings.Contains(outShow, `"ATM-0001"`) {
		t.Fatalf("expected ATM-0001 in claimed_tasks, got %s", outShow)
	}
	if !strings.Contains(outShow, `"open_followups": 1`) {
		t.Fatalf("expected open_followups 1, got %s", outShow)
	}
}
