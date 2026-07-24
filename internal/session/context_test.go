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
