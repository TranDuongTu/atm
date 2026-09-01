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
	if !strings.Contains(out, "## Persona and checklists") {
		t.Fatal("persona-and-checklists closing section missing")
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

func TestRenderContextChecklistSections(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", Name: "Agent Tasks Management", Actor: "a@b:c",
		PersonaPrompt: "P",
		Capability:    "scrum",
		Checklists: []ChecklistSection{
			{Name: "develop-task", Purpose: "How one task flows.", StepsRendered: "1. claim\n2. build\n"},
			{Name: "pr-convention", Purpose: "PR shape.", StepsRendered: "1. title\n"},
		},
	})
	i1 := strings.Index(out, "## Checklist: develop-task")
	i2 := strings.Index(out, "## Checklist: pr-convention")
	iScope := strings.Index(out, "## Session scope")
	if i1 < 0 || i2 < 0 || i2 < i1 {
		t.Fatalf("checklist sections missing/misordered:\n%s", out)
	}
	if iScope >= 0 && i2 > iScope {
		t.Fatalf("checklists must precede the capability scope:\n%s", out)
	}
	for _, want := range []string{
		"How one task flows.", "1. claim\n2. build",
		"atm checklist show --project ATM --name develop-task",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "## Persona Prompting") {
		t.Error("v1 closing section survived")
	}
	if !strings.Contains(out, "operating procedure") {
		t.Error("v2 closing section missing")
	}
	if strings.Contains(out, "<CHECKLISTS_SECTIONS>") {
		t.Fatalf("placeholder residue:\n%s", out)
	}
}

func TestRenderContextNoChecklistsNoResidue(t *testing.T) {
	out := RenderContext(ContextData{Code: "X", PersonaPrompt: "P"})
	// "## Checklist: " with the trailing space is a section header; the
	// closing prose mentions the backticked form, which must not count.
	if strings.Contains(out, "## Checklist: ") || strings.Contains(out, "<CHECKLISTS_SECTIONS>") {
		t.Fatalf("residue without checklists:\n%s", out)
	}
}
