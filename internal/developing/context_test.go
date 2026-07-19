package developing

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:      "FOO",
		Name:      "Foo Project",
		Actor:     "codex-dev",
	})
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ACTOR>",
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
		"# ATM developing session",
		"Project `FOO` (`Foo Project`)",
		"actor `codex-dev`",
		"atm conventions",
		"capability list --project FOO",
		"search --project FOO",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
	if strings.Contains(got, "/usr/local/bin/atm") {
		t.Errorf("rendered context must not contain an absolute atm path")
	}
}

func TestRenderContext_Persona(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", Name: "ATM", Actor: "staff@claude",
		Persona: "staff", PersonaPrompt: "hold a high bar",
	})
	if !strings.Contains(out, "Persona: staff") || !strings.Contains(out, "hold a high bar") {
		t.Fatalf("persona block missing:\n%s", out)
	}

	out2 := RenderContext(ContextData{Code: "ATM", Name: "ATM", Actor: "claude-dev"})
	if strings.Contains(out2, "## Persona") {
		t.Fatalf("no-persona render should omit persona block:\n%s", out2)
	}
}

func TestRenderContextPromptsJournaling(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", Actor: "ollama-dev"})
	for _, frag := range []string{
		"atm search",
		"visible ledger",
		"Journal",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("developing context missing %q", frag)
		}
	}
}

func TestRenderContextIncludesPersonaDescription(t *testing.T) {
	out := RenderContext(ContextData{
		Persona:            "developer",
		PersonaPrompt:      "do good work",
		PersonaDescription: "Default working persona.",
		Actor:              "developer@claude:unset",
	})
	for _, want := range []string{"developer", "Default working persona.", "do good work"} {
		if !strings.Contains(out, want) {
			t.Errorf("context missing %q", want)
		}
	}
}

func TestRenderContextModelStampInstruction(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Name: "ATM", Actor: "developer@claude:unset"})
	if !strings.Contains(out, ":unset") {
		t.Errorf("context missing model-stamp instruction referencing :unset")
	}
}