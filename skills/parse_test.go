package skills

import (
	"reflect"
	"strings"
	"testing"
)

const managerDoc = `---
name: manager
description: Curates the ledger and oversees work.
expects: [CODE, PROJECT_NAME, ACTOR]
optional: [TASK_ID]
---
# Persona: manager

Core prompt line.

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
	if got := strings.Join(p.Expects, ","); got != "CODE,PROJECT_NAME,ACTOR" {
		t.Fatalf("expects = %v (want declaration order)", got)
	}
	if got := strings.Join(p.Optional, ","); got != "TASK_ID" {
		t.Fatalf("optional = %v (want [TASK_ID])", got)
	}
	if !strings.Contains(p.Personality, "Calm and terse.") {
		t.Fatalf("personality = %q", p.Personality)
	}
	if !strings.Contains(p.CorePrompt, "Core prompt line.") ||
		strings.Contains(p.CorePrompt, "Calm and terse.") {
		t.Fatalf("core prompt must exclude personality section: %q", p.CorePrompt)
	}
	if !strings.Contains(p.Body, "Calm and terse.") {
		t.Fatalf("body must be the full document body")
	}
}

func TestParsePersonaMinimal(t *testing.T) {
	doc := "---\nname: admin\ndescription: Human operator.\n---\nBody.\n"
	p, err := ParsePersona("admin", []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Expects) != 0 || len(p.Optional) != 0 || p.Personality != "" || p.ProjectOptional {
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
		"bad launch":         {"x", "---\nname: x\ndescription: d\nlaunch: warp\n---\nb"},
		"invalid name chars": {"X!", "---\nname: X!\ndescription: d\n---\nb"},
		"bad expects":        {"x", "---\nname: x\ndescription: d\nexpects: [UNKNOWN]\n---\nb"},
		"bad optional":       {"x", "---\nname: x\ndescription: d\noptional: [UNKNOWN]\n---\nb"},
	}
	for label, c := range cases {
		if _, err := ParsePersona(c.stem, []byte(c.doc)); err == nil {
			t.Errorf("%s: expected error", label)
		}
	}
}

const flowDoc = `---
name: demoflow
description: Status transitions.
labels: [status:*, priority:*]
boards: [backlog, all-tasks]
---
# Demoflow

## Semantics

S.

## Actions

A.

## Converge

C.
`

func TestParseCapability(t *testing.T) {
	c, err := ParseCapability("demoflow", []byte(flowDoc))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "demoflow" || c.Description != "Status transitions." {
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

func TestParseStepsNested(t *testing.T) {
	body := `
1. Triage the inbox
   - list the inbox
   - decide per task
     1. absorb
     2. evict
2. Advance
`
	steps, err := ParseSteps(body)
	if err != nil {
		t.Fatal(err)
	}
	want := []SeedStep{
		{Text: "Triage the inbox", Children: []SeedStep{
			{Text: "list the inbox"},
			{Text: "decide per task", Children: []SeedStep{
				{Text: "absorb"},
				{Text: "evict"},
			}},
		}},
		{Text: "Advance"},
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("tree:\n got %+v\nwant %+v", steps, want)
	}
}

func TestParseStepsTabsAndUnevenIndent(t *testing.T) {
	body := "- top\n\t- tab child\nprose between steps is ignored\n- second top\n      - deep child\n  - shallow child\n"
	steps, err := ParseSteps(body)
	if err != nil {
		t.Fatal(err)
	}
	want := []SeedStep{
		{Text: "top", Children: []SeedStep{{Text: "tab child"}}},
		{Text: "second top", Children: []SeedStep{
			{Text: "deep child"},
			{Text: "shallow child"},
		}},
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("tree:\n got %+v\nwant %+v", steps, want)
	}
}

func TestParseStepsEmpty(t *testing.T) {
	steps, err := ParseSteps("just prose\n\nno list items\n")
	if err != nil || steps != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", steps, err)
	}
}

func TestParseChecklistSeedV2Frontmatter(t *testing.T) {
	src := []byte(`---
name: scrum-backlog
purpose: sweep the scrum flow
suits: [manager]
requires_capabilities: [scrum]
requires_channels: [journal]
origin: shipped:scrum
---
1. top
   - child
`)
	s, err := ParseChecklistSeed("scrum-backlog", src)
	if err != nil {
		t.Fatal(err)
	}
	want := ChecklistSeed{
		Name:    "scrum-backlog",
		Purpose: "sweep the scrum flow",
		Suits:   []string{"manager"},
		Requires: SeedRequires{
			Capabilities: []string{"scrum"},
			Channels:     []string{"journal"},
		},
		Origin: "shipped:scrum",
		Steps:  []SeedStep{{Text: "top", Children: []SeedStep{{Text: "child"}}}},
	}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("seed:\n got %+v\nwant %+v", s, want)
	}
}

func TestParseChecklistSeedLegacyPersona(t *testing.T) {
	src := []byte("---\npersona: concierge\nname: empty-project\npurpose: a fresh project\n---\n1. First step.\n2. Second step.\n")
	seed, err := ParseChecklistSeed("empty-project", src)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seed.Suits, []string{"concierge"}) {
		t.Fatalf("suits = %v, want legacy persona mapped", seed.Suits)
	}
	if seed.Origin != "" {
		t.Fatalf("origin = %q, want empty (loader defaults it)", seed.Origin)
	}
	if len(seed.Steps) != 2 || seed.Steps[0].Text != "First step." {
		t.Fatalf("steps: %+v", seed.Steps)
	}
}

func TestParseChecklistSeedPersonaAndSuitsConflict(t *testing.T) {
	src := []byte("---\npersona: p\nsuits: [q]\nname: x\npurpose: y\n---\n- step\n")
	if _, err := ParseChecklistSeed("x", src); err == nil {
		t.Fatal("persona and suits together must be rejected")
	}
}

func TestParseChecklistSeedBadOrigin(t *testing.T) {
	src := []byte("---\nname: x\npurpose: y\norigin: vendor\n---\n- step\n")
	if _, err := ParseChecklistSeed("x", src); err == nil {
		t.Fatal("bad origin must be rejected")
	}
}

func TestParseChecklistSeedBadSuit(t *testing.T) {
	src := []byte("---\nname: x\npurpose: y\nsuits: [Bad Name]\n---\n- step\n")
	if _, err := ParseChecklistSeed("x", src); err == nil {
		t.Fatal("invalid suits entry must be rejected")
	}
}

func TestParseChecklistSeedStillRequiresNamePurposeSteps(t *testing.T) {
	if _, err := ParseChecklistSeed("x", []byte("---\nname: x\npurpose: y\n---\nno steps here\n")); err == nil {
		t.Fatal("a seed without list items must be rejected")
	}
	if _, err := ParseChecklistSeed("x", []byte("---\nname: x\n---\n- step\n")); err == nil {
		t.Fatal("a seed without purpose must be rejected")
	}
	if _, err := ParseChecklistSeed("stem", []byte("---\nname: other\npurpose: y\n---\n- step\n")); err == nil {
		t.Fatal("frontmatter name must match filename")
	}
}

func TestParseCapabilityBrief(t *testing.T) {
	src := []byte("---\nname: x\ndescription: d\nbrief: Do the thing before working.\nlabels: [x:*]\nboards: [xs]\n---\nbody\n\n## Semantics\ns\n\n## Actions\na\n\n## Converge\nc\n")
	c, err := ParseCapability("x", src)
	if err != nil {
		t.Fatal(err)
	}
	if c.Brief != "Do the thing before working." {
		t.Fatalf("Brief = %q", c.Brief)
	}
	src2 := []byte("---\nname: x\ndescription: d\nlabels: [x:*]\nboards: [xs]\n---\nbody\n\n## Semantics\ns\n\n## Actions\na\n\n## Converge\nc\n")
	c2, err := ParseCapability("x", src2)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Brief != "" {
		t.Fatalf("missing brief must parse to empty, got %q", c2.Brief)
	}
}

const validDutySection = "## Duty: manager\n\n### Triage\nt\n\n### Advance\na\n\n### Route\nr\n"

func TestDutyOf(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{"no duty section", "## Semantics\n\ns\n", "", false},
		{"valid", "intro\n\n" + validDutySection, "manager", false},
		{"other persona", "## Duty: tester\n\n### Triage\nt\n\n### Advance\na\n\n### Route\nr\n", "tester", false},
		{"missing Route", "## Duty: manager\n\n### Triage\nt\n\n### Advance\na\n", "", true},
		{"missing Triage", "## Duty: manager\n\n### Advance\na\n\n### Route\nr\n", "", true},
		{"duplicate sections", validDutySection + "\n" + validDutySection, "", true},
		{"invalid persona name", "## Duty: Not A Name\n\n### Triage\nt\n\n### Advance\na\n\n### Route\nr\n", "", true},
		{"empty persona name", "## Duty:\n\n### Triage\nt\n\n### Advance\na\n\n### Route\nr\n", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DutyOf(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DutyOf(%q) = %q, want error", tc.body, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DutyOf: %v", err)
			}
			if got != tc.want {
				t.Fatalf("DutyOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseCapabilityDuty(t *testing.T) {
	flow := []byte("---\nname: x\ndescription: d\nlabels: [x:*]\n---\n## Semantics\ns\n\n## Actions\na\n\n## Converge\nc\n\n" + validDutySection)
	c, err := ParseCapability("x", flow)
	if err != nil {
		t.Fatal(err)
	}
	if c.Duty != "manager" {
		t.Fatalf("Duty = %q, want manager", c.Duty)
	}
	registry := []byte("---\nname: x\ndescription: d\nlabels: [x:*]\n---\n## Semantics\ns\n\n## Actions\na\n\n## Converge\nc\n")
	c2, err := ParseCapability("x", registry)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Duty != "" {
		t.Fatalf("registry capability Duty = %q, want empty", c2.Duty)
	}
	malformed := []byte("---\nname: x\ndescription: d\nlabels: [x:*]\n---\n## Semantics\ns\n\n## Actions\na\n\n## Converge\nc\n\n## Duty: manager\n\n### Triage\nt\n")
	if _, err := ParseCapability("x", malformed); err == nil {
		t.Fatal("a malformed duty section must be a parse error")
	}
}

func TestDutyPersonaErr(t *testing.T) {
	personas := []PersonaSpec{{Name: "manager"}}
	if err := dutyPersonaErr([]CapabilitySpec{{Name: "a", Duty: "manager"}, {Name: "b"}}, personas); err != nil {
		t.Fatalf("known persona: %v", err)
	}
	if err := dutyPersonaErr([]CapabilitySpec{{Name: "a", Duty: "ghost"}}, personas); err == nil {
		t.Fatal("a duty targeting an unknown persona must error")
	}
}

func TestBuiltinDutyContract(t *testing.T) {
	for _, name := range []string{"scrum", "qa", "codereview"} {
		c := MustCapability(name)
		if c.Duty != "manager" {
			t.Errorf("%s: Duty = %q, want manager", name, c.Duty)
		}
	}
	for _, name := range []string{"release", "channel", "checklist"} {
		c := MustCapability(name)
		if c.Duty != "" {
			t.Errorf("%s: registry capability Duty = %q, want empty", name, c.Duty)
		}
	}
}
