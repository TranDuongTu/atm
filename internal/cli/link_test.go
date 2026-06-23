package cli

import (
	"strings"
	"testing"
)

func TestGoldenIDAssignmentOrdering(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "A",
		"--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")

	wantIDs := []string{"ATM-0001", "ATM-0002", "ATM-0003", "ATM-0004", "ATM-0005"}
	for i, want := range wantIDs {
		out, _, code := h.run("task", "create", "--store", sp, "--project", "ATM",
			"--title", "task", "--label", "type:impl", "--actor", "human:alice")
		if code != 0 {
			t.Fatalf("create %d exit = %d stderr=%s", i, code, h.stderr.String())
		}
		if !strings.Contains(out, `"id": "`+want+`"`) {
			t.Fatalf("create %d: expected id %s, got %s", i, want, out)
		}
	}
}

func TestGoldenLinkListOutAndInWithBlockedBy(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "A",
		"--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "A",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "B",
		"--label", "type:impl", "--actor", "human:alice")

	h.run("task", "link", "add", "--store", sp, "--id", "ATM-0001", "--type", "blocks",
		"--target", "ATM-0002", "--actor", "human:alice")
	h.run("task", "link", "add", "--store", sp, "--id", "ATM-0001", "--type", "implements",
		"--target", "ATM-0002", "--actor", "human:alice")

	out, _, code := h.run("task", "link", "list", "--id", "ATM-0001")
	if code != 0 {
		t.Fatalf("link list out exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "link-list-out", out)
	if !strings.Contains(out, `"links_out"`) {
		t.Fatalf("expected links_out in output, got %s", out)
	}
	if !strings.Contains(out, `"direction": "out"`) {
		t.Fatalf("expected out direction tag, got %s", out)
	}

	inOut, _, code := h.run("task", "link", "list", "--id", "ATM-0002")
	if code != 0 {
		t.Fatalf("link list in exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "link-list-in", inOut)
	if !strings.Contains(inOut, `"links_in"`) {
		t.Fatalf("expected links_in in output, got %s", inOut)
	}
	if !strings.Contains(inOut, `"direction": "in"`) {
		t.Fatalf("expected in direction tag, got %s", inOut)
	}
	if !strings.Contains(inOut, `"type": "blocked-by"`) {
		t.Fatalf("expected implied blocked-by in-edge, got %s", inOut)
	}
	if !strings.Contains(inOut, `"type": "implements"`) {
		t.Fatalf("expected implements in-edge, got %s", inOut)
	}
}

func TestGoldenScenario3Links(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management",
		"--label", "type:epic", "--label", "type:impl",
		"--type-axis", "type", "--actor", "human:alice")

	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Epic: agent workflow",
		"--label", "type:epic", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Impl: claim command",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "link", "add", "--store", sp, "--id", "ATM-0002", "--type", "implements",
		"--target", "ATM-0001", "--actor", "human:alice")

	outEpic, _, code := h.run("task", "link", "list", "--id", "ATM-0001")
	if code != 0 {
		t.Fatalf("link list epic exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "scenario3-link-list-epic", outEpic)
	if !strings.Contains(outEpic, `"links_in"`) {
		t.Fatalf("epic: expected links_in, got %s", outEpic)
	}
	if !strings.Contains(outEpic, `"target": "ATM-0002"`) {
		t.Fatalf("epic: expected ATM-0002 in links_in, got %s", outEpic)
	}
	if !strings.Contains(outEpic, `"type": "implements"`) {
		t.Fatalf("epic: expected implements type, got %s", outEpic)
	}
	if !strings.Contains(outEpic, `"direction": "in"`) {
		t.Fatalf("epic: expected direction in, got %s", outEpic)
	}

	outImpl, _, code := h.run("task", "link", "list", "--id", "ATM-0002")
	if code != 0 {
		t.Fatalf("link list impl exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "scenario3-link-list-impl", outImpl)
	if !strings.Contains(outImpl, `"links_out"`) {
		t.Fatalf("impl: expected links_out, got %s", outImpl)
	}
	if !strings.Contains(outImpl, `"target": "ATM-0001"`) {
		t.Fatalf("impl: expected ATM-0001 in links_out, got %s", outImpl)
	}
	if !strings.Contains(outImpl, `"type": "implements"`) {
		t.Fatalf("impl: expected implements type, got %s", outImpl)
	}
	if !strings.Contains(outImpl, `"direction": "out"`) {
		t.Fatalf("impl: expected direction out, got %s", outImpl)
	}
}

func TestGoldenScenario3BlocksExcludesTargetFromNext(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "A",
		"--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "blocker",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "blocked",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "link", "add", "--store", sp, "--id", "ATM-0001", "--type", "blocks",
		"--target", "ATM-0002", "--actor", "human:alice")

	out, _, code := h.run("task", "next", "--project", "ATM")
	if code != 0 {
		t.Fatalf("next exit = %d stderr=%s", code, h.stderr.String())
	}
	if strings.Contains(out, `"id": "ATM-0002"`) {
		t.Fatalf("blocked task ATM-0002 must not be returned by next, got %s", out)
	}
	if !strings.Contains(out, `"id": "ATM-0001"`) {
		t.Fatalf("expected next to return the blocker ATM-0001, got %s", out)
	}
}
