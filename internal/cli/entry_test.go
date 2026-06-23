package cli

import (
	"strings"
	"testing"
)

func seedScenario4(h *goldenHarness) {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management",
		"--label", "type:epic", "--label", "type:impl", "--label", "type:bug",
		"--label", "area:cli", "--label", "kind:convention",
		"--type-axis", "type", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "PR conventions for bug fixes",
		"--label", "kind:convention", "--label", "type:bug", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Fix claim race",
		"--label", "type:bug", "--label", "area:cli", "--actor", "human:alice")
	h.run("task", "todo", "add", "--store", sp, "--id", "ATM-0002", "--text", "Write tests for claim",
		"--actor", "agent:claude-1")
	h.run("task", "followup", "add", "--store", sp, "--id", "ATM-0002", "--text", "Decide storage format",
		"--assignee", "human:alice", "--actor", "human:alice")
	h.run("task", "discussion", "add", "--store", sp, "--id", "ATM-0002", "--text", "Use file-level locking.",
		"--actor", "human:alice")
	h.reset()
}

func TestGoldenTimelineOrdering(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario4(h)

	out, _, code := h.run("task", "timeline", "--id", "ATM-0002")
	if code != 0 {
		t.Fatalf("timeline exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "scenario4-timeline", out)

	wantKinds := []string{"history", "todo", "followup", "discussion"}
	for _, k := range wantKinds {
		if !strings.Contains(out, `"kind": "`+k+`"`) {
			t.Fatalf("timeline missing kind %q, got %s", k, out)
		}
	}

	if !strings.Contains(out, `"entries"`) {
		t.Fatalf("expected entries field, got %s", out)
	}
	if !strings.Contains(out, `"id": "h1"`) {
		t.Fatalf("expected history entry h1, got %s", out)
	}
	if !strings.Contains(out, `"id": "t1"`) {
		t.Fatalf("expected todo entry t1, got %s", out)
	}
	if !strings.Contains(out, `"id": "f1"`) {
		t.Fatalf("expected followup entry f1, got %s", out)
	}
	if !strings.Contains(out, `"id": "d1"`) {
		t.Fatalf("expected discussion entry d1, got %s", out)
	}

	lines := strings.Count(out, `"kind":`)
	if lines < 4 {
		t.Fatalf("expected at least 4 timeline entries, got %d in %s", lines, out)
	}
}

func TestGoldenFollowupResolveSetsResolvedFields(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario4(h)

	h.run("task", "followup", "resolve", "--id", "ATM-0002", "--followup", "f1",
		"--actor", "human:alice")

	out, _, code := h.run("task", "show", "--id", "ATM-0002")
	if code != 0 {
		t.Fatalf("show exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "scenario4-followup-resolved-show", out)

	if !strings.Contains(out, `"status": "resolved"`) {
		t.Fatalf("expected followup status resolved, got %s", out)
	}
	if !strings.Contains(out, `"resolved_at": "TIMESTAMP"`) && !strings.Contains(out, `"resolved_at"`) {
		t.Fatalf("expected resolved_at field, got %s", out)
	}
	if !strings.Contains(out, `"resolved_by": "human:alice"`) {
		t.Fatalf("expected resolved_by human:alice, got %s", out)
	}
}

func TestGoldenTodoToggle(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario4(h)

	out, _, code := h.run("task", "todo", "toggle", "--id", "ATM-0002", "--todo", "t1",
		"--actor", "human:alice")
	if code != 0 {
		t.Fatalf("todo toggle exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"done": true`) {
		t.Fatalf("expected todo t1 done=true, got %s", out)
	}
}

func TestGoldenScenario4Integration(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario4(h)

	outTL, _, code := h.run("task", "timeline", "--id", "ATM-0002")
	if code != 0 {
		t.Fatalf("timeline exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "scenario4-integration-timeline", outTL)

	for _, k := range []string{"history", "todo", "followup", "discussion"} {
		if !strings.Contains(outTL, `"kind": "`+k+`"`) {
			t.Fatalf("Scenario 4: timeline missing kind %q, got %s", k, outTL)
		}
	}

	_, _, code = h.run("task", "followup", "resolve", "--id", "ATM-0002", "--followup", "f1",
		"--actor", "human:alice")
	if code != 0 {
		t.Fatalf("Scenario 4: followup resolve exit = %d stderr=%s", code, h.stderr.String())
	}

	outShow, _, code := h.run("task", "show", "--id", "ATM-0002")
	if code != 0 {
		t.Fatalf("Scenario 4: show exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(outShow, `"status": "resolved"`) {
		t.Fatalf("Scenario 4: expected followup status resolved, got %s", outShow)
	}
	if !strings.Contains(outShow, `"resolved_at"`) {
		t.Fatalf("Scenario 4: expected resolved_at field, got %s", outShow)
	}
	if !strings.Contains(outShow, `"resolved_by": "human:alice"`) {
		t.Fatalf("Scenario 4: expected resolved_by human:alice, got %s", outShow)
	}
}
