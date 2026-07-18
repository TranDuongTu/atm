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
	compareGolden(t, "conventions-json", out)
}

func TestConventionsIsMinimalPrimer(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	stdout, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"atm capability list", "atm capability <name> guide", "## Substrate", "## Actor identity"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("primer missing %q", want)
		}
	}
	for _, gone := range []string{"first-contact", "code-of-conduct", "First-time human sequence", "atm label seed"} {
		if strings.Contains(stdout, gone) {
			t.Errorf("primer still contains removed prose %q", gone)
		}
	}
}

func TestConventionsJSONEnvelopeIsMinimalPrimer(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("conventions")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var envl struct {
		Conventions map[string]any `json:"conventions"`
	}
	if err := json.Unmarshal([]byte(out), &envl); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"what_atm_is", "substrate", "capabilities", "actor_identity"} {
		if _, ok := envl.Conventions[want]; !ok {
			t.Errorf("JSON envelope missing key %q", want)
		}
	}
	for _, gone := range []string{"seeded_labels", "code_of_conduct", "first_time_human_sequence", "agent_first_contact_sequence", "day_to_day_development", "advisory"} {
		if _, exists := envl.Conventions[gone]; exists {
			t.Errorf("JSON envelope still carries removed key %q", gone)
		}
	}
	if caps, ok := envl.Conventions["capabilities"].([]any); ok {
		t.Errorf("JSON envelope 'capabilities' is a list (%d entries), want a one-line pointer string", len(caps))
	}
}

func TestConventionsIncludesMemorySubstrate(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.output = outputText
	out, _, _ := h.run("conventions", "--store", sp)
	for _, frag := range []string{"atm task comment", "atm search"} {
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
	if !strings.Contains(out, "persona@agent:model") {
		t.Error("conventions does not describe the enforced actor convention")
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

func TestConventionsNoProjectFlag(t *testing.T) {
	h := newGoldenHarness(t)
	root := newRootCmdWithState(h.st)
	convCmd, _, err := root.Find([]string{"conventions"})
	if err != nil {
		t.Fatalf("find conventions: %v", err)
	}
	if convCmd.Flags().Lookup("project") != nil {
		t.Error("conventions command should not register a --project flag")
	}
}
