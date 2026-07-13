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

func TestRenderContextActionCatalogPresent(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "ATM", ATMBin: "/bin/atm", Actor: "m"})
	for _, frag := range []string{"Tracking", "Asking", "Glossary", "Onboarding"} {
		if !strings.Contains(got, frag) {
			t.Errorf("action catalog missing %q", frag)
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
