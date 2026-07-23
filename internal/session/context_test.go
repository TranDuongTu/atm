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

// TestRenderContextTaskBlock verifies ContextData.Task renders an assigned-task
// block naming the task, and that an empty Task renders neither the block nor
// a literal placeholder.
func TestRenderContextTaskBlock(t *testing.T) {
	spec, ok := skills.Persona("developer")
	if !ok {
		t.Fatal("built-in developer persona missing")
	}
	out := RenderContext(ContextData{
		Code: "ATM", Name: "Agent Tasks Management",
		Actor: "developer@claude:unset", Spec: spec, Task: "ATM-4b7e24",
	})
	if !strings.Contains(out, "## Assigned task") {
		t.Fatalf("missing assigned-task block:\n%s", out)
	}
	if !strings.Contains(out, "`ATM-4b7e24`") || !strings.Contains(out, "atm task show ATM-4b7e24") {
		t.Fatalf("task block must name the task and the show command:\n%s", out)
	}
	if strings.Contains(out, "<TASK_BLOCK>") {
		t.Fatalf("literal placeholder leaked:\n%s", out)
	}

	out = RenderContext(ContextData{Code: "ATM", Name: "x", Actor: "a", Spec: spec})
	if strings.Contains(out, "## Assigned task") || strings.Contains(out, "<TASK_BLOCK>") {
		t.Fatalf("no-task render must omit block and placeholder:\n%s", out)
	}
}
