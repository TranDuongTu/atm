package cli

import (
	"strings"
	"testing"
	"time"

	"atm/internal/store"
)

func ageTaskUpdated(t *testing.T, h *goldenHarness, id string, hours int) {
	t.Helper()
	s, err := store.Open(h.store.StorePath())
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	task.UpdatedAt = time.Now().UTC().Add(time.Duration(hours) * time.Hour)
	if err := store.WriteJSON(s.StorePath()+"/projects/"+task.ProjectCode+"/tasks/"+id+".json", task); err != nil {
		t.Fatal(err)
	}
}

func seedScenario2(h *goldenHarness) {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "DEMO", "--name", "Demo",
		"--label", "type:impl", "--label", "area:cli", "--actor", "human:alice")
	h.run("project", "label", "add", "--store", sp, "--code", "DEMO",
		"--label", "type:bug", "--description", "Bug fix", "--actor", "human:alice")
	h.run("project", "set-type-axis", "--store", sp, "--code", "DEMO",
		"--namespace", "type", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "DEMO", "--title", "Old task",
		"--label", "area:cli", "--actor", "human:alice")
}

func TestGoldenProjectDuplicateCodeConflict(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "first",
		"--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")
	_, _, code := h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "second",
		"--label", "type:impl", "--actor", "human:alice")
	if code != ExitConflict {
		t.Fatalf("expected exit %d (conflict), got %d", ExitConflict, code)
	}
}

func TestGoldenProjectLabelRemoveRetainedUsage(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario2(h)
	out, _, code := h.run("project", "label", "remove", "--store", h.store.StorePath(),
		"--code", "DEMO", "--label", "area:cli", "--actor", "human:alice")
	if code != 0 {
		t.Fatalf("label remove exit = %d", code)
	}
	if !strings.Contains(out, `"retained_usage": 1`) {
		t.Fatalf("expected retained_usage 1, got %s", out)
	}
	compareGolden(t, "project-label-remove-retained", out)
}

func TestGoldenProjectSetTypeAxisNoLabelsRejected(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x",
		"--label", "type:impl", "--actor", "human:alice")
	_, _, code := h.run("project", "set-type-axis", "--store", sp, "--code", "ATM",
		"--namespace", "kind", "--actor", "human:alice")
	if code != ExitUsage {
		t.Fatalf("expected exit %d (usage), got %d", ExitUsage, code)
	}
}

func TestGoldenProjectLabelListAfterAdd(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario2(h)
	out, _, code := h.run("project", "label", "list", "--store", h.store.StorePath(), "--code", "DEMO")
	if code != 0 {
		t.Fatalf("label list exit = %d", code)
	}
	if !strings.Contains(out, `"name": "type:impl"`) {
		t.Fatalf("missing type:impl: %s", out)
	}
	if !strings.Contains(out, `"name": "type:bug"`) {
		t.Fatalf("missing type:bug: %s", out)
	}
	if !strings.Contains(out, `"name": "area:cli"`) {
		t.Fatalf("missing area:cli: %s", out)
	}
	compareGolden(t, "project-label-list", out)
}

func TestScenario2SoftRemovalRejectsNewAssignment(t *testing.T) {
	h := newGoldenHarness(t)
	seedScenario2(h)
	sp := h.store.StorePath()
	h.run("project", "label", "remove", "--store", sp, "--code", "DEMO",
		"--label", "area:cli", "--actor", "human:alice")
	_, _, code := h.run("task", "create", "--store", sp, "--project", "DEMO",
		"--title", "New task", "--label", "area:cli", "--actor", "human:alice")
	if code != ExitUsage {
		t.Fatalf("expected new task creation to be rejected (exit %d), got %d", ExitUsage, code)
	}
}

func seedScenario6Guide(h *goldenHarness) {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Testing conventions",
		"--label", "kind:convention", "--label", "area:cli", "--actor", "human:alice")
	h.run("project", "guide", "section", "add", "--store", sp, "--code", "ATM",
		"--name", "conventions", "--actor", "human:alice")
	h.run("project", "guide", "section", "add", "--store", sp, "--code", "ATM",
		"--name", "testing", "--actor", "human:alice")
	h.run("project", "guide", "ref", "add", "--store", sp, "--code", "ATM",
		"--section", "conventions", "--kind", "task", "--target", "ATM-0001", "--actor", "human:alice")
	h.run("project", "guide", "ref", "add", "--store", sp, "--code", "ATM",
		"--section", "testing", "--kind", "task", "--target", "ATM-0004", "--actor", "human:alice")
	h.run("project", "guide", "set-freshness", "--store", sp, "--code", "ATM",
		"--threshold", "720h", "--actor", "human:alice")
}

func TestGoldenGuideShow(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	seedScenario6Guide(h)
	out, _, code := h.run("project", "guide", "show", "--store", h.store.StorePath(), "--code", "ATM")
	if code != 0 {
		t.Fatalf("guide show exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"conventions"`) {
		t.Fatalf("expected conventions section, got %s", out)
	}
	if !strings.Contains(out, `"testing"`) {
		t.Fatalf("expected testing section, got %s", out)
	}
	if !strings.Contains(out, `"ATM-0001"`) {
		t.Fatalf("expected ATM-0001 ref, got %s", out)
	}
	if !strings.Contains(out, `"ATM-0004"`) {
		t.Fatalf("expected ATM-0004 ref, got %s", out)
	}
	compareGolden(t, "project-guide-show", out)
}

func TestGoldenGuideShowNullWhenNone(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x",
		"--label", "type:impl", "--actor", "human:alice")
	out, _, code := h.run("project", "guide", "show", "--store", sp, "--code", "ATM")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, `"guide": null`) {
		t.Fatalf("expected guide null, got %s", out)
	}
}

func TestGoldenGuideSectionRenameRemoveMove(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("project", "guide", "section", "add", "--store", sp, "--code", "ATM",
		"--name", "conventions", "--actor", "human:alice")
	h.run("project", "guide", "section", "add", "--store", sp, "--code", "ATM",
		"--name", "testing", "--actor", "human:alice")
	h.run("project", "guide", "section", "rename", "--store", sp, "--code", "ATM",
		"--name", "conventions", "--new-name", "convs", "--actor", "human:alice")
	h.run("project", "guide", "section", "move", "--store", sp, "--code", "ATM",
		"--name", "testing", "--before", "convs", "--actor", "human:alice")
	out, _, code := h.run("project", "guide", "show", "--store", sp, "--code", "ATM")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	compareGolden(t, "project-guide-section-rename-move", out)
	if !strings.Contains(out, `"name": "testing"`) {
		t.Fatalf("expected testing section, got %s", out)
	}
	if !strings.Contains(out, `"name": "convs"`) {
		t.Fatalf("expected convs section, got %s", out)
	}
	h.run("project", "guide", "section", "remove", "--store", sp, "--code", "ATM",
		"--name", "convs", "--actor", "human:alice")
	out, _, _ = h.run("project", "guide", "show", "--store", sp, "--code", "ATM")
	if strings.Contains(out, `"convs"`) {
		t.Fatalf("convs should be removed, got %s", out)
	}
}

func TestGoldenGuideRefAddRemoveMove(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "A", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "B", "--actor", "human:alice")
	h.run("project", "guide", "section", "add", "--store", sp, "--code", "ATM",
		"--name", "conventions", "--actor", "human:alice")
	h.run("project", "guide", "ref", "add", "--store", sp, "--code", "ATM",
		"--section", "conventions", "--kind", "task", "--target", "ATM-0001", "--actor", "human:alice")
	h.run("project", "guide", "ref", "add", "--store", sp, "--code", "ATM",
		"--section", "conventions", "--kind", "task", "--target", "ATM-0002", "--actor", "human:alice")
	h.run("project", "guide", "ref", "move", "--store", sp, "--code", "ATM",
		"--section", "conventions", "--kind", "task", "--target", "ATM-0002",
		"--before", "ATM-0001", "--actor", "human:alice")
	out, _, code := h.run("project", "guide", "show", "--store", sp, "--code", "ATM")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	compareGolden(t, "project-guide-ref-move", out)
	h.run("project", "guide", "ref", "remove", "--store", sp, "--code", "ATM",
		"--section", "conventions", "--kind", "task", "--target", "ATM-0001", "--actor", "human:alice")
	out, _, _ = h.run("project", "guide", "show", "--store", sp, "--code", "ATM")
	if strings.Contains(out, `"ATM-0001"`) {
		t.Fatalf("ATM-0001 ref should be removed, got %s", out)
	}
}

func TestGoldenGuideSetFreshness(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x",
		"--label", "type:impl", "--actor", "human:alice")
	_, _, code := h.run("project", "guide", "set-freshness", "--store", sp, "--code", "ATM",
		"--threshold", "720h", "--actor", "human:alice")
	if code != 0 {
		t.Fatalf("set-freshness exit = %d", code)
	}
	_, _, code = h.run("project", "guide", "set-freshness", "--store", sp, "--code", "ATM",
		"--threshold", "unset", "--actor", "human:alice")
	if code != 0 {
		t.Fatalf("unset exit = %d", code)
	}
	_, _, code = h.run("project", "guide", "set-freshness", "--store", sp, "--code", "ATM",
		"--threshold", "not-a-duration", "--actor", "human:alice")
	if code != ExitUsage {
		t.Fatalf("bad duration should be usage exit, got %d", code)
	}
}

func TestGoldenGuideStatusCoverageFreshness(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	seedScenario6Guide(h)
	ageTaskUpdated(t, h, "ATM-0004", -2000)
	out, _, code := h.run("project", "guide", "status", "--store", h.store.StorePath(), "--code", "ATM")
	if code != 0 {
		t.Fatalf("guide status exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"total_sections": 2`) {
		t.Fatalf("expected total_sections 2, got %s", out)
	}
	if !strings.Contains(out, `"total_refs": 2`) {
		t.Fatalf("expected total_refs 2, got %s", out)
	}
	if !strings.Contains(out, `"empty_sections": []`) {
		t.Fatalf("expected empty empty_sections, got %s", out)
	}
	for _, want := range []string{`"fresh"`, `"stale"`, `"ATM-0001"`, `"ATM-0004"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %s in freshness, got %s", want, out)
		}
	}
	compareGolden(t, "project-guide-status", out)
}

func TestGoldenGuideStatusUnknownWhenThresholdUnset(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "C", "--actor", "human:alice")
	h.run("project", "guide", "section", "add", "--store", sp, "--code", "ATM",
		"--name", "conventions", "--actor", "human:alice")
	h.run("project", "guide", "ref", "add", "--store", sp, "--code", "ATM",
		"--section", "conventions", "--kind", "task", "--target", "ATM-0001", "--actor", "human:alice")
	out, _, code := h.run("project", "guide", "status", "--store", sp, "--code", "ATM")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, `"state": "unknown"`) {
		t.Fatalf("expected unknown state with threshold unset, got %s", out)
	}
}

func TestScenario6GuideInNextAndDashboard(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	seedScenario6Guide(h)
	out, _, code := h.run("task", "next", "--store", h.store.StorePath(), "--project", "ATM")
	if code != 0 {
		t.Fatalf("next exit = %d", code)
	}
	if !strings.Contains(out, `"guide": {`) {
		t.Fatalf("expected guide object in next response, got %s", out)
	}
	if !strings.Contains(out, `"conventions"`) {
		t.Fatalf("expected conventions section in guide, got %s", out)
	}
}
