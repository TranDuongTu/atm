package cli

import (
	"encoding/json"
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

func TestConventionsSoftenedWorkflowWording(t *testing.T) {
	// The store stays neutral; the paved road lives in a capability.
	if !strings.Contains(conventionsCoreText, "capability") {
		t.Error("conventions text must mention that workflow lives in a capability")
	}
}

// Conventions enumerate capabilities; they no longer restate any
// capability's semantics. The enumeration is rendered from the registry, so
// a fake registry must surface its own entries verbatim.
func TestConventionsEnumerateCapabilities(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"## Capabilities",
		"workflow", "`atm capability workflow guide`",
		"contextmap", "`atm capability contextmap guide`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("conventions missing %q", want)
		}
	}
}

// The de-restatement guarantee: capability-owned prose is gone from the
// static text. These fragments now live only in the capability guides.
func TestConventionsCarryNoCapabilityProse(t *testing.T) {
	for _, banned := range []string{
		"atm workflow start", "exactly-one-status",
		"atm context stamp", "atm context supersede",
		"DRIFT", "UNVERIFIED",
	} {
		if strings.Contains(conventionsCoreText, banned) {
			t.Errorf("conventionsCoreText still restates capability prose %q", banned)
		}
	}
}

func TestConventionsJSONEnumeratesCapabilities(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("--output", "json", "conventions")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var envl struct {
		Conventions map[string]any `json:"conventions"`
	}
	if err := json.Unmarshal([]byte(out), &envl); err != nil {
		t.Fatal(err)
	}
	caps, ok := envl.Conventions["capabilities"].([]any)
	if !ok || len(caps) != 2 {
		t.Fatalf("capabilities = %v, want 2 entries", envl.Conventions["capabilities"])
	}
	for _, gone := range []string{"workflow_verbs", "context_map"} {
		if _, exists := envl.Conventions[gone]; exists {
			t.Errorf("JSON still carries removed key %q", gone)
		}
	}
}
