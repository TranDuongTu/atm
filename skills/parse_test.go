package skills

import (
	"strings"
	"testing"
)

const managerDoc = `---
name: manager
description: Curates the ledger and oversees work.
modes:
  brief: Interview the human.
  autopilot: Converge autonomously.
default_mode: autopilot
---
# Persona: manager

Core prompt line.

## Mode: brief

Brief instructions.

## Mode: autopilot

Autopilot instructions.

## Personality

Calm and terse.
`

func TestParsePersonaFull(t *testing.T) {
	p, err := ParsePersona("manager", []byte(managerDoc))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "manager" || p.Description != "Curates the ledger and oversees work." {
		t.Fatalf("frontmatter: %+v", p)
	}
	if p.Launch != "prompt" {
		t.Fatalf("launch default = %q, want prompt", p.Launch)
	}
	if p.DefaultMode != "autopilot" {
		t.Fatalf("default_mode = %q", p.DefaultMode)
	}
	if got := p.ModeNames(); strings.Join(got, ",") != "brief,autopilot" {
		t.Fatalf("mode order = %v (want declaration order)", got)
	}
	m, ok := p.Mode("brief")
	if !ok || !strings.Contains(m.Instructions, "Brief instructions.") || m.Summary != "Interview the human." {
		t.Fatalf("mode brief = %+v ok=%v", m, ok)
	}
	if !strings.Contains(p.Personality, "Calm and terse.") {
		t.Fatalf("personality = %q", p.Personality)
	}
	if !strings.Contains(p.CorePrompt, "Core prompt line.") ||
		strings.Contains(p.CorePrompt, "Brief instructions.") ||
		strings.Contains(p.CorePrompt, "Calm and terse.") {
		t.Fatalf("core prompt must exclude mode/personality sections: %q", p.CorePrompt)
	}
	if !strings.Contains(p.Body, "Brief instructions.") {
		t.Fatalf("body must be the full document body")
	}
}

func TestParsePersonaMinimal(t *testing.T) {
	doc := "---\nname: admin\ndescription: Human operator.\n---\nBody.\n"
	p, err := ParsePersona("admin", []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Modes) != 0 || p.Personality != "" || p.ProjectOptional {
		t.Fatalf("minimal persona: %+v", p)
	}
}

func TestParsePersonaOptionalFlags(t *testing.T) {
	doc := "---\nname: concierge\ndescription: Guide.\nproject_optional: true\n---\nBody.\n"
	p, err := ParsePersona("concierge", []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if !p.ProjectOptional {
		t.Fatal("project_optional not parsed")
	}
	doc2 := "---\nname: developer\ndescription: Dev.\nlaunch: hook\n---\nBody.\n"
	p2, err := ParsePersona("developer", []byte(doc2))
	if err != nil {
		t.Fatal(err)
	}
	if p2.Launch != "hook" {
		t.Fatalf("launch = %q", p2.Launch)
	}
}

func TestParsePersonaErrors(t *testing.T) {
	cases := map[string]struct{ stem, doc string }{
		"no frontmatter":     {"x", "just text"},
		"name mismatch":      {"other", "---\nname: x\ndescription: d\n---\nb"},
		"missing desc":       {"x", "---\nname: x\n---\nb"},
		"mode no section":    {"x", "---\nname: x\ndescription: d\nmodes:\n  brief: b\n---\nb"},
		"section no mode":    {"x", "---\nname: x\ndescription: d\n---\n## Mode: brief\n\nb"},
		"bad default_mode":   {"x", "---\nname: x\ndescription: d\nmodes:\n  a: s\ndefault_mode: z\n---\n## Mode: a\n\nb"},
		"bad launch":         {"x", "---\nname: x\ndescription: d\nlaunch: warp\n---\nb"},
		"default no modes":   {"x", "---\nname: x\ndescription: d\ndefault_mode: a\n---\nb"},
		"invalid name chars": {"X!", "---\nname: X!\ndescription: d\n---\nb"},
	}
	for label, c := range cases {
		if _, err := ParsePersona(c.stem, []byte(c.doc)); err == nil {
			t.Errorf("%s: expected error", label)
		}
	}
}

const workflowDoc = `---
name: workflow
description: Status transitions.
labels: [status:*, priority:*]
boards: [backlog, all-tasks]
---
# Workflow

## Semantics

S.

## Actions

A.

## Converge

C.
`

func TestParseCapability(t *testing.T) {
	c, err := ParseCapability("workflow", []byte(workflowDoc))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "workflow" || c.Description != "Status transitions." {
		t.Fatalf("%+v", c)
	}
	if strings.Join(c.Labels, ",") != "status:*,priority:*" || strings.Join(c.Boards, ",") != "backlog,all-tasks" {
		t.Fatalf("labels=%v boards=%v", c.Labels, c.Boards)
	}
	if !strings.Contains(c.Body, "## Converge") {
		t.Fatal("body lost")
	}
}

func TestParseCapabilityErrors(t *testing.T) {
	cases := map[string]string{
		"missing labels":    "---\nname: x\ndescription: d\nboards: [b]\n---\n## Semantics\ns\n## Actions\na\n## Converge\nc",
		"missing boards":    "---\nname: x\ndescription: d\nlabels: [l]\n---\n## Semantics\ns\n## Actions\na\n## Converge\nc",
		"missing converge":  "---\nname: x\ndescription: d\nlabels: [l]\nboards: [b]\n---\n## Semantics\ns\n## Actions\na",
		"missing actions":   "---\nname: x\ndescription: d\nlabels: [l]\nboards: [b]\n---\n## Semantics\ns\n## Converge\nc",
		"missing semantics": "---\nname: x\ndescription: d\nlabels: [l]\nboards: [b]\n---\n## Actions\na\n## Converge\nc",
	}
	for label, doc := range cases {
		if _, err := ParseCapability("x", []byte(doc)); err == nil {
			t.Errorf("%s: expected error", label)
		}
	}
}

func TestParseIgnoresUnknownScalarKeys(t *testing.T) {
	doc := "---\nname: x\ndescription: d\ncreated_at: 2026-07-22T00:00:00Z\ncreated_by: a@b:c\n---\nBody."
	if _, err := ParsePersona("x", []byte(doc)); err != nil {
		t.Fatalf("unknown scalar keys must be tolerated (store audit fields): %v", err)
	}
}
