package manager

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:   "FOO",
		Name:   "Foo Project",
		Actor:  "manager@codex:unset",
		Action: "autopilot",
	})
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ACTOR>", "<ACTION_BLOCK>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, placeholder := range []string{
		"<RUN_ID>", "<TIMESTAMP>", "<ATM_BIN>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains volatile placeholder %s", placeholder)
		}
	}
	for _, want := range []string{
		"# ATM manager — FOO",
		"Project `FOO` (`Foo Project`)",
		"actor `manager@codex:unset`",
		"atm conventions",
		"atm capability list --project FOO",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
	if strings.Contains(got, "/usr/local/bin/atm") {
		t.Errorf("rendered context must not contain an absolute atm path")
	}
	// The action block builds commands with the literal "atm", not <ATM_BIN>.
	if !strings.Contains(got, "atm capability <name> guide") {
		t.Errorf("action block should reference literal `atm capability <name> guide`")
	}
}

func TestRenderContextPrinciplesPresent(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "ATM", Actor: "m"})
	for _, frag := range []string{
		"autonomous owner",
		"relentlessly and frequently organize",
		"self-improvement",
		"label substrate",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("principle missing %q", frag)
		}
	}
}

func TestRenderContextGenericKeepsPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{})
	for _, placeholder := range []string{"<CODE>", "<ACTOR>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic render stripped %s", placeholder)
		}
	}
	// <ATM_BIN> is gone from the template; the generic render must not contain it.
	if strings.Contains(got, "<ATM_BIN>") {
		t.Errorf("generic render must not contain <ATM_BIN>; literal `atm` is used")
	}
}

func TestManagerContextInjectsPersona(t *testing.T) {
	out := RenderContext(ContextData{
		Persona:            "manager",
		PersonaPrompt:      "curate the ledger",
		PersonaDescription: "Curates the ledger and oversees work.",
	})
	if !strings.Contains(out, "curate the ledger") {
		t.Error("manager context missing persona prompt")
	}
}

func TestRenderContextActionBlocks(t *testing.T) {
	base := ContextData{Code: "ATM", Name: "Agent Tasks Management", Actor: "manager@claude:test"}

	brief := base
	brief.Action = "brief"
	out := RenderContext(brief)
	if !strings.Contains(out, "Focus this session on **brief**") ||
		!strings.Contains(out, `"Brief" section`) ||
		!strings.Contains(out, "atm capability list --project ATM") {
		t.Errorf("brief block wrong:\n%s", out)
	}

	scoped := base
	scoped.Action = "autopilot"
	scoped.Capability = "contextmap"
	out = RenderContext(scoped)
	if !strings.Contains(out, "the `contextmap` capability") || !strings.Contains(out, `"Autopilot" section`) {
		t.Errorf("scoped autopilot block wrong:\n%s", out)
	}

	if out := RenderContext(base); strings.Contains(out, "<CAPABILITY_ROLES>") || strings.Contains(out, "## Current manager action") {
		t.Errorf("action-less render leaked placeholders:\n%s", out)
	}
	if strings.Contains(RenderContext(base), "**Curate**") {
		t.Error("prompt still hardcodes the Curate role bullet")
	}
}
