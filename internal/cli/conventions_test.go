package cli

import (
	"strings"
	"testing"
)

func TestConventionsText(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "What ATM is") {
		t.Fatalf("expected 'What ATM is' in text output: %s", out)
	}
	if !strings.Contains(out, "Agent code-of-conduct") {
		t.Fatalf("expected 'Agent code-of-conduct' in text output")
	}
	if !strings.Contains(out, "read every label's description first") {
		t.Fatalf("expected 'read every label's description first' in text output")
	}
	compareGolden(t, "conventions-text", out)
}

func TestConventionsJSON(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"conventions"`) {
		t.Fatalf("expected 'conventions' key in JSON output: %s", out)
	}
	if !strings.Contains(out, `"code_of_conduct"`) {
		t.Fatalf("expected 'code_of_conduct' key in JSON output")
	}
	if !strings.Contains(out, `"seeded_labels"`) {
		t.Fatalf("expected 'seeded_labels' key in JSON output")
	}
	if !strings.Contains(out, `"how_to_search"`) {
		t.Fatalf("expected 'how_to_search' key in JSON output")
	}
	compareGolden(t, "conventions-json", out)
}

func TestConventionsIncludesMemorySubstrate(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.output = outputText
	out, _, _ := h.run("conventions", "--store", sp)
	for _, frag := range []string{"ATM:comment:open-question", "atm search", "atm index", "atm project set-embedding"} {
		if !strings.Contains(out, frag) {
			t.Errorf("conventions text missing %q", frag)
		}
	}
}

func TestConventionsActorText(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, _ := h.run("conventions")
	if strings.Contains(out, "actor migrate") || strings.Contains(out, "actor alias") {
		t.Error("conventions still references the removed alias subsystem")
	}
	if !strings.Contains(out, "persona@agent:model") || !strings.Contains(out, "registered persona") {
		t.Error("conventions does not describe the enforced actor convention")
	}
}

func TestConventionsPointAtCurrentKnowledgeBoard(t *testing.T) {
	if !strings.Contains(conventionsText, "context-current") {
		t.Error("first-contact sequence must send agents to the context-current board, " +
			"not the raw context:* namespace -- otherwise they read superseded knowledge")
	}
	if !strings.Contains(conventionsText, "atm context check") {
		t.Error("conventions must mention `atm context check` so an agent can find the capability")
	}
	for _, verb := range []string{"atm context add", "atm context stamp", "atm context retarget", "atm context supersede"} {
		if !strings.Contains(conventionsText, verb) {
			t.Errorf("conventions text missing %q", verb)
		}
	}
	if !strings.Contains(conventionsText, "The context map") {
		t.Error("conventions text missing the context-map section heading")
	}
	js := conventionsStructured()
	seq, _ := js["agent_first_contact_sequence"].([]string)
	joined := strings.Join(seq, "\n")
	if !strings.Contains(joined, "context-current") {
		t.Error("agent_first_contact_sequence JSON must reference the context-current board")
	}
	cm, _ := js["context_map"].(string)
	if !strings.Contains(cm, "atm context check") {
		t.Error("context_map JSON must mention atm context check")
	}
	if !strings.Contains(cm, "atm context add") || !strings.Contains(cm, "atm context stamp") ||
		!strings.Contains(cm, "atm context retarget") || !strings.Contains(cm, "atm context supersede") {
		t.Error("context_map JSON must mention all five context verbs")
	}
	if strings.Contains(conventionsText, "--onboarding") {
		t.Error("conventions text still advertises the deprecated --onboarding flag; use --mapping")
	}
	if strings.Contains(js["day_to_day_development"].(string), "--onboarding") {
		t.Error("day_to_day_development JSON still advertises the deprecated --onboarding flag; use --mapping")
	}
}

func TestConventionsFirstRunUsesInitSetup(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("conventions")
	if code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "First run setup: `atm init`") {
		t.Fatalf("conventions missing first-run init setup guidance:\n%s", out)
	}
	if strings.Contains(out, "first pick your host agent once with `atm agents select <name>`") {
		t.Fatalf("conventions still makes atm agents select the primary path:\n%s", out)
	}
}
