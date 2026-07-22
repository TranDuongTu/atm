package session

import (
	"strings"
	"testing"

	"atm/skills"
)

func spec(t *testing.T) skills.PersonaSpec {
	t.Helper()
	doc := `---
name: manager
description: Curates.
modes:
  brief: Interview.
  autopilot: Converge.
default_mode: autopilot
---
Core prompt.

## Mode: brief

Do the interview.

## Mode: autopilot

Run the loop.

## Personality

Default temperament.
`
	p, err := skills.ParsePersona("manager", []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRenderContextModeSelection(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", Name: "Agent Tasks Management", Actor: "manager@claude:unset",
		Spec: spec(t), Mode: "brief",
	})
	if !strings.Contains(out, "## Mode: brief") || !strings.Contains(out, "Do the interview.") {
		t.Fatalf("selected mode missing:\n%s", out)
	}
	if strings.Contains(out, "Run the loop.") {
		t.Fatal("unselected mode leaked into the prompt")
	}
	if !strings.Contains(out, "Core prompt.") || !strings.Contains(out, "Default temperament.") {
		t.Fatal("persona core/personality missing")
	}
	if !strings.Contains(out, "Project `ATM`") {
		t.Fatal("project header missing")
	}
}

func TestRenderContextPersonalityOverride(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Name: "n", Actor: "a", Spec: spec(t),
		Personality: "Custom voice.", Mode: "autopilot"})
	if !strings.Contains(out, "Custom voice.") || strings.Contains(out, "Default temperament.") {
		t.Fatal("overlay must replace the default personality")
	}
}

func TestRenderContextCapabilityScope(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Name: "n", Actor: "a", Spec: spec(t),
		Mode: "autopilot", Capability: "workflow_ai"})
	if !strings.Contains(out, "`workflow_ai`") {
		t.Fatal("capability scope line missing")
	}
}

func TestRenderContextNoProjectLeavesPlaceholders(t *testing.T) {
	out := RenderContext(ContextData{Actor: "concierge@claude:unset", Spec: spec(t)})
	if !strings.Contains(out, "<CODE>") {
		t.Fatal("empty project must leave <CODE> placeholders literal (session-context without --project)")
	}
}

func TestRenderContextNoMode(t *testing.T) {
	s := spec(t)
	out := RenderContext(ContextData{Code: "ATM", Name: "n", Actor: "a", Spec: s})
	if strings.Contains(out, "## Mode:") {
		t.Fatal("no mode selected → no mode block")
	}
}
