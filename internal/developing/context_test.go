package developing

import (
	"strings"
	"testing"
)

func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:      "FOO",
		Name:      "Foo Project",
		ATMBin:    "/usr/local/bin/atm",
		Actor:     "codex-dev",
		RunID:     "FOO-20260705120000-a1b2c3",
		Timestamp: "2026-07-05T12:00:00Z",
	})
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>",
		"<RUN_ID>", "<TIMESTAMP>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, want := range []string{
		"ATM developing session FOO-20260705120000-a1b2c3",
		"Project `FOO` (`Foo Project`)",
		"actor `codex-dev`",
		"atm `/usr/local/bin/atm`",
		"conventions",
		"search --project FOO",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
}

func TestRenderContext_Persona(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", Name: "ATM", ATMBin: "/atm", Actor: "staff@claude",
		RunID: "R", Timestamp: "T", Persona: "staff", PersonaPrompt: "hold a high bar",
	})
	if !strings.Contains(out, "Persona: staff") || !strings.Contains(out, "hold a high bar") {
		t.Fatalf("persona block missing:\n%s", out)
	}

	out2 := RenderContext(ContextData{Code: "ATM", Name: "ATM", ATMBin: "/atm", Actor: "claude-dev", RunID: "R", Timestamp: "T"})
	if strings.Contains(out2, "## Persona") {
		t.Fatalf("no-persona render should omit persona block:\n%s", out2)
	}
}

func TestRenderContextDelegatesWritesToManager(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "/usr/local/bin/atm", Actor: "ollama-dev", RunID: "R1", Timestamp: "2026-07-08T00:00:00Z"})
	for _, frag := range []string{"atm search", "Delegate every write", "atm-manager", "dispatch"} {
		if !strings.Contains(got, frag) {
			t.Errorf("developing context missing %q", frag)
		}
	}
}
