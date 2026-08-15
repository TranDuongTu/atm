package session

import (
	"strings"
	"testing"
)

func TestRenderContextFull(t *testing.T) {
	out := RenderContext(ContextData{
		Code:          "ATM",
		Name:          "Agent Tasks Management",
		Actor:         "developer@claude:unset",
		TaskID:        "ATM-212d04",
		PersonaPrompt: "# Persona: developer\n\nTest prompt.",
	})
	if !strings.Contains(out, "- Project: `ATM` (`Agent Tasks Management`)") {
		t.Fatal("project line missing")
	}
	if !strings.Contains(out, "- Actor: `developer@claude:unset`") {
		t.Fatal("actor line missing")
	}
	if !strings.Contains(out, "- Task: `ATM-212d04`") {
		t.Fatal("task line missing")
	}
	if !strings.Contains(out, "# Persona: developer") {
		t.Fatal("persona prompt missing")
	}
	if !strings.Contains(out, "## Orientation") {
		t.Fatal("orientation section missing")
	}
	if !strings.Contains(out, "## Persona Prompting") {
		t.Fatal("persona prompting section missing")
	}
}

func TestRenderContextNoProjectLeavesPlaceholders(t *testing.T) {
	out := RenderContext(ContextData{Actor: "concierge@claude:unset"})
	if !strings.Contains(out, "<CODE>") {
		t.Fatal("empty project must leave <CODE> placeholders literal")
	}
	if !strings.Contains(out, "<TASK_ID>") {
		t.Fatal("empty TaskID must remain as placeholder literal")
	}
}

func TestRenderContextPersonaPromptInjected(t *testing.T) {
	out := RenderContext(ContextData{
		Code:          "ATM",
		Name:          "n",
		Actor:         "a",
		PersonaPrompt: "# Persona: custom\n\nCustom content.\n",
	})
	if !strings.Contains(out, "Custom content.") {
		t.Fatal("persona prompt not injected")
	}
	if strings.Contains(out, "<PERSONA_PROMPT>") {
		t.Fatal("placeholder not replaced")
	}
}

func TestRenderContextEmptyActor(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Name: "n"})
	if !strings.Contains(out, "<ACTOR>") {
		t.Fatal("empty Actor must remain as placeholder literal")
	}
}

func TestRenderContextCapabilityScope(t *testing.T) {
	d := ContextData{Code: "ATM", Name: "Acme", Actor: "concierge@claude:test", Capability: "channel", PersonaPrompt: "PROMPT"}
	out := RenderContext(d)
	for _, want := range []string{
		"## Session scope",
		"scoped to the `channel` capability",
		"atm capability channel guide",
		"for project ATM",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scoped context missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "<CAPABILITY_SCOPE>") {
		t.Error("placeholder leaked into scoped render")
	}
	if idx := strings.Index(out, "## Session scope"); idx > strings.Index(out, "## Orientation") {
		t.Error("scope section must precede Orientation")
	}
}

func TestRenderContextNoCapabilityNoResidue(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Actor: "a@b:c", PersonaPrompt: "PROMPT"})
	if strings.Contains(out, "CAPABILITY_SCOPE") || strings.Contains(out, "Session scope") {
		t.Errorf("unscoped context must carry no scope residue:\n%s", out)
	}
}

func TestRenderContextCapabilitiesBlock(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Capabilities: "## Capabilities\n\n- **channel** — brief text"})
	if !strings.Contains(out, "- **channel** — brief text") {
		t.Fatalf("block not rendered:\n%s", out)
	}
	if strings.Contains(out, "<CAPABILITIES>") {
		t.Fatalf("placeholder residue:\n%s", out)
	}
	if strings.Index(out, "## Capabilities") > strings.Index(out, "## Orientation") {
		t.Fatalf("block after Orientation:\n%s", out)
	}
}

func TestRenderContextNoCapabilitiesNoResidue(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM"})
	if strings.Contains(out, "<CAPABILITIES>") || strings.Contains(out, "## Capabilities") {
		t.Fatalf("empty Capabilities must remove the section:\n%s", out)
	}
}

func TestRenderContextScopeAndCapabilitiesCompose(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Capability: "channel", Capabilities: "## Capabilities\n\n- **channel** — b"})
	iScope, iCaps, iOrient := strings.Index(out, "## Session scope"), strings.Index(out, "## Capabilities"), strings.Index(out, "## Orientation")
	if !(iScope >= 0 && iScope < iCaps && iCaps < iOrient) {
		t.Fatalf("want scope < capabilities < orientation:\n%s", out)
	}
}
