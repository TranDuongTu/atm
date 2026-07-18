package manager

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:   "ATM",
		Name:   "Agent Tasks Management",
		ATMBin: "/usr/local/bin/atm",
		Actor:  "opencode-manager",
	})
	for _, placeholder := range []string{"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>"} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, want := range []string{
		"ATM manager — ATM",
		"Project `ATM` (`Agent Tasks Management`)",
		"atm `/usr/local/bin/atm`",
		"autonomous owner",
		"conventions",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
}

func TestRenderContextPrinciplesPresent(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "ATM", ATMBin: "/bin/atm", Actor: "m"})
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
	// The generic body (no project) is produced by leaving placeholders in place
	// so `atm manage-context` with no --project still renders a template.
	got := RenderContext(ContextData{})
	for _, placeholder := range []string{"<CODE>", "<ATM_BIN>"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("generic render stripped %s; placeholders must survive for template use", placeholder)
		}
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
	base := ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "/usr/local/bin/atm", Actor: "manager@claude:test"}

	brief := base
	brief.Action = "brief"
	out := RenderContext(brief)
	if !strings.Contains(out, "Focus this session on **brief**") ||
		!strings.Contains(out, `"Brief" section`) ||
		!strings.Contains(out, "/usr/local/bin/atm capability list --project ATM") {
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
