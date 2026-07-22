# Persona–Capability Skills Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decouple personas from capabilities behind a top-level `skills/` prompt folder, unify session launching under `atm --persona`, and add a new `concierge` onboarding persona.

**Architecture:** A new pure-leaf `skills` package embeds and parses `skills/persona/*.md` and `skills/capability/*.md` (YAML-ish frontmatter + markdown body, format enforced by the parser). Capability packages source `Summary()`/`Guide()` from it; the store resolves built-in personas from it (no more seeding) and persists custom personas as markdown; a new `internal/session` package replaces `internal/developing` + `internal/manager` with one launcher and one context template; the CLI root gains `--persona/--mode/--capability` and `atm dev`/`atm manage` are deleted.

**Tech Stack:** Go 1.25, cobra, `//go:embed`. No new dependencies — frontmatter is parsed by a small hand-rolled parser (the repo avoids incidental deps).

**Spec:** `docs/superpowers/specs/2026-07-22-persona-capability-skills-design.md` — ledger task **ATM-0772ea**.

## Global Constraints

- Module path is `atm`; the new package imports as `atm/skills`.
- `skills` must be a **pure leaf**: no `atm/internal/...` or `atm/libs/...` imports (arch-tested).
- `internal/core` stays a pure leaf; `internal/cli` may not import `internal/store` or `internal/tui`; capability packages may import only `atm/internal/capability` + `atm/internal/core` among internal packages (importing `atm/skills` is allowed — arch tests only inspect `atm/internal/...`/`atm/libs/...` paths).
- New persona name: **concierge**. Mode flag: **`--mode`**. `atm dev`/`atm manage`: removed outright, no aliases. Hidden `atm manage-context` stays as a thin alias (installed thin-pointer plugins call it).
- Persona names match `^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$` (underscore allowed for capability names like `workflow_ai`).
- Commits: `<type>(ATM-0772ea): <summary>`, staged with **explicit paths** (never `git add -A`).
- After every task: `go build ./... && go test ./...` must pass.
- Work on branch `atm-0772ea-persona-capability-skills` (already exists, holds the spec).

---

### Task 1: `skills` package — types and frontmatter parser

**Files:**
- Create: `skills/spec.go`
- Create: `skills/parse.go`
- Test: `skills/parse_test.go`

**Interfaces:**
- Consumes: nothing (pure leaf).
- Produces: `skills.PersonaSpec`, `skills.Mode`, `skills.CapabilitySpec`, `skills.ParsePersona(stem string, src []byte) (PersonaSpec, error)`, `skills.ParseCapability(stem string, src []byte) (CapabilitySpec, error)`. Later tasks rely on these exact names and fields.

- [ ] **Step 1: Write the failing tests**

`skills/parse_test.go`:

```go
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
		"missing labels":   "---\nname: x\ndescription: d\nboards: [b]\n---\n## Semantics\ns\n## Actions\na\n## Converge\nc",
		"missing boards":   "---\nname: x\ndescription: d\nlabels: [l]\n---\n## Semantics\ns\n## Actions\na\n## Converge\nc",
		"missing converge": "---\nname: x\ndescription: d\nlabels: [l]\nboards: [b]\n---\n## Semantics\ns\n## Actions\na",
		"missing actions":  "---\nname: x\ndescription: d\nlabels: [l]\nboards: [b]\n---\n## Semantics\ns\n## Converge\nc",
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./skills/ 2>&1 | head -5`
Expected: build failure — package does not exist / `ParsePersona` undefined.

- [ ] **Step 3: Implement**

`skills/spec.go`:

```go
// Package skills hosts ATM's built-in prompt surface: persona and capability
// prompt files under skills/persona and skills/capability, embedded into the
// binary, plus the parser that enforces their format. Pure leaf — it imports
// nothing from this repository, so every layer (cli, store, capabilities) may
// depend on it.
package skills

// Mode is one operating mode a persona declares: the frontmatter summary and
// the matching `## Mode: <name>` body section.
type Mode struct {
	Name         string
	Summary      string // one-line, from frontmatter (CLI help / validation messages)
	Instructions string // full section body, rendered into the session prompt
}

// PersonaSpec is one parsed persona prompt file.
type PersonaSpec struct {
	Name        string
	Description string
	// Launch selects how the host agent receives the context file: "prompt"
	// (default — an initial message points at the rendered context file) or
	// "hook" (a session-start plugin hook loads it; the agent starts idle).
	Launch string
	// DefaultMode is used when --mode is not given. Empty means no mode block.
	DefaultMode string
	// ProjectOptional personas may launch without --project (concierge: the
	// project may not exist yet).
	ProjectOptional bool
	Modes           []Mode // declaration order
	Body            string // full markdown body (after frontmatter)
	CorePrompt      string // Body minus `## Mode:` and `## Personality` sections
	Personality     string // default `## Personality` section body, "" if none
}

// Mode returns the named mode.
func (p PersonaSpec) Mode(name string) (Mode, bool) {
	for _, m := range p.Modes {
		if m.Name == name {
			return m, true
		}
	}
	return Mode{}, false
}

// ModeNames lists declared mode names in declaration order.
func (p PersonaSpec) ModeNames() []string {
	out := make([]string, 0, len(p.Modes))
	for _, m := range p.Modes {
		out = append(out, m.Name)
	}
	return out
}

// CapabilitySpec is one parsed capability prompt file. Labels and Boards are
// the frontmatter declaration of the vocabulary the capability manages; the
// Go package remains the executable source of truth (a per-capability test
// pins the two in sync).
type CapabilitySpec struct {
	Name        string
	Description string
	Labels      []string
	Boards      []string
	Body        string // the full guide served by `atm capability <name> guide`
}
```

`skills/parse.go`:

```go
package skills

import (
	"fmt"
	"regexp"
	"strings"
)

var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)

// frontmatter is the parsed `---` header: scalar keys, one optional nested
// string map (modes), and inline lists. Unknown scalar keys are tolerated so
// the store can add audit fields (created_at, ...) to custom persona files.
type frontmatter struct {
	scalars map[string]string
	modes   []Mode // name+summary only; Instructions filled from body sections
	lists   map[string][]string
}

// parseFrontmatter splits src into frontmatter and body. The document must
// start with a `---` line; the header ends at the next `---` line.
func parseFrontmatter(src []byte) (frontmatter, string, error) {
	fm := frontmatter{scalars: map[string]string{}, lists: map[string][]string{}}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, "", fmt.Errorf("missing frontmatter: file must start with ---")
	}
	end := -1
	inModes := false
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			end = i
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "  ") { // nested entry (only under modes:)
			if !inModes {
				return fm, "", fmt.Errorf("frontmatter line %d: unexpected indent", i+1)
			}
			k, v, ok := splitKV(strings.TrimSpace(line))
			if !ok {
				return fm, "", fmt.Errorf("frontmatter line %d: want `name: summary`", i+1)
			}
			fm.modes = append(fm.modes, Mode{Name: k, Summary: v})
			continue
		}
		inModes = false
		k, v, ok := splitKV(line)
		if !ok {
			return fm, "", fmt.Errorf("frontmatter line %d: want `key: value`", i+1)
		}
		switch {
		case k == "modes" && v == "":
			inModes = true
		case strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]"):
			inner := strings.TrimSpace(v[1 : len(v)-1])
			if inner != "" {
				for _, item := range strings.Split(inner, ",") {
					fm.lists[k] = append(fm.lists[k], strings.TrimSpace(item))
				}
			} else {
				fm.lists[k] = []string{}
			}
		default:
			fm.scalars[k] = v
		}
	}
	if end < 0 {
		return fm, "", fmt.Errorf("unterminated frontmatter: closing --- not found")
	}
	return fm, strings.Join(lines[end+1:], "\n"), nil
}

// splitKV splits "key: value" (value may be empty, and may contain colons).
func splitKV(line string) (k, v string, ok bool) {
	i := strings.Index(line, ":")
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// sections splits a markdown body into a preamble and its `## `-level
// sections, preserving order. A section runs until the next `## ` heading.
type section struct{ title, body string }

func splitSections(body string) (preamble string, secs []section) {
	lines := strings.Split(body, "\n")
	var cur *section
	var pre []string
	flush := func() {
		if cur != nil {
			cur.body = strings.TrimSpace(cur.body)
			secs = append(secs, *cur)
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			cur = &section{title: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
			continue
		}
		if cur == nil {
			pre = append(pre, line)
		} else {
			cur.body += line + "\n"
		}
	}
	flush()
	return strings.Join(pre, "\n"), secs
}

// ParsePersona parses and validates one persona prompt file. stem is the
// filename without extension; it must equal the frontmatter name.
func ParsePersona(stem string, src []byte) (PersonaSpec, error) {
	fm, body, err := parseFrontmatter(src)
	if err != nil {
		return PersonaSpec{}, fmt.Errorf("persona %s: %w", stem, err)
	}
	p := PersonaSpec{
		Name:        fm.scalars["name"],
		Description: fm.scalars["description"],
		Launch:      fm.scalars["launch"],
		DefaultMode: fm.scalars["default_mode"],
		Modes:       fm.modes,
		Body:        strings.TrimSpace(body),
	}
	if !nameRe.MatchString(p.Name) {
		return PersonaSpec{}, fmt.Errorf("persona %s: invalid or missing name %q", stem, p.Name)
	}
	if p.Name != stem {
		return PersonaSpec{}, fmt.Errorf("persona %s: frontmatter name %q must match filename", stem, p.Name)
	}
	if p.Description == "" {
		return PersonaSpec{}, fmt.Errorf("persona %s: description is required", stem)
	}
	switch p.Launch {
	case "":
		p.Launch = "prompt"
	case "prompt", "hook":
	default:
		return PersonaSpec{}, fmt.Errorf("persona %s: launch must be prompt or hook, got %q", stem, p.Launch)
	}
	if v := fm.scalars["project_optional"]; v != "" {
		if v != "true" && v != "false" {
			return PersonaSpec{}, fmt.Errorf("persona %s: project_optional must be true or false", stem)
		}
		p.ProjectOptional = v == "true"
	}

	// Reconcile frontmatter modes with `## Mode: <name>` sections, and pull
	// out the personality section; everything else is the core prompt.
	pre, secs := splitSections(p.Body)
	core := []string{strings.TrimSpace(pre)}
	seen := map[string]bool{}
	for _, s := range secs {
		if name, ok := strings.CutPrefix(s.title, "Mode: "); ok {
			name = strings.TrimSpace(name)
			found := false
			for i := range p.Modes {
				if p.Modes[i].Name == name {
					p.Modes[i].Instructions = s.body
					found = true
					break
				}
			}
			if !found {
				return PersonaSpec{}, fmt.Errorf("persona %s: section %q has no frontmatter modes entry", stem, s.title)
			}
			seen[name] = true
			continue
		}
		if s.title == "Personality" {
			p.Personality = s.body
			continue
		}
		core = append(core, "## "+s.title+"\n\n"+s.body)
	}
	for _, m := range p.Modes {
		if !seen[m.Name] {
			return PersonaSpec{}, fmt.Errorf("persona %s: mode %q has no `## Mode: %s` section", stem, m.Name, m.Name)
		}
	}
	if p.DefaultMode != "" {
		if _, ok := p.Mode(p.DefaultMode); !ok {
			return PersonaSpec{}, fmt.Errorf("persona %s: default_mode %q is not a declared mode", stem, p.DefaultMode)
		}
	}
	p.CorePrompt = strings.TrimSpace(strings.Join(core, "\n\n"))
	return p, nil
}

// ParseCapability parses and validates one capability prompt file.
func ParseCapability(stem string, src []byte) (CapabilitySpec, error) {
	fm, body, err := parseFrontmatter(src)
	if err != nil {
		return CapabilitySpec{}, fmt.Errorf("capability %s: %w", stem, err)
	}
	c := CapabilitySpec{
		Name:        fm.scalars["name"],
		Description: fm.scalars["description"],
		Labels:      fm.lists["labels"],
		Boards:      fm.lists["boards"],
		Body:        strings.TrimSpace(body),
	}
	if !nameRe.MatchString(c.Name) {
		return CapabilitySpec{}, fmt.Errorf("capability %s: invalid or missing name %q", stem, c.Name)
	}
	if c.Name != stem {
		return CapabilitySpec{}, fmt.Errorf("capability %s: frontmatter name %q must match filename", stem, c.Name)
	}
	if c.Description == "" {
		return CapabilitySpec{}, fmt.Errorf("capability %s: description is required", stem)
	}
	if len(c.Labels) == 0 {
		return CapabilitySpec{}, fmt.Errorf("capability %s: labels is required", stem)
	}
	if len(c.Boards) == 0 {
		return CapabilitySpec{}, fmt.Errorf("capability %s: boards is required", stem)
	}
	_, secs := splitSections(c.Body)
	have := map[string]bool{}
	for _, s := range secs {
		have[s.title] = true
	}
	for _, required := range []string{"Semantics", "Actions", "Converge"} {
		if !have[required] {
			return CapabilitySpec{}, fmt.Errorf("capability %s: missing required section `## %s`", stem, required)
		}
	}
	return c, nil
}
```

Also add `go.work` awareness: the repo has a `go.work`; `skills/` is inside module `atm`, no work-file change needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./skills/`
Expected: `ok  	atm/skills`

- [ ] **Step 5: Commit**

```bash
git add skills/spec.go skills/parse.go skills/parse_test.go
git commit -m "feat(ATM-0772ea): skills package — persona/capability prompt format parser"
```

---

### Task 2: Built-in persona files (developer, manager, admin) + embed

**Files:**
- Create: `skills/persona/developer.md`
- Create: `skills/persona/manager.md`
- Create: `skills/persona/admin.md`
- Create: `skills/skills.go` (embed + load + accessors)
- Test: `skills/skills_test.go`

**Interfaces:**
- Consumes: Task 1's parser.
- Produces: `skills.Personas() []PersonaSpec`, `skills.Persona(name string) (PersonaSpec, bool)` (Task 3 adds the capability accessors to the same file).

The manager persona absorbs the persona-specific content currently in `internal/manager/context_v1.md` ("Your Principles", "Your Roles") and the mode procedures currently hard-coded in `internal/manager/context.go` — generalized to reference capability `Semantics`/`Actions`/`Converge` sections instead of `Brief`/`Autopilot`.

- [ ] **Step 1: Write the failing test**

`skills/skills_test.go`:

```go
package skills

import (
	"strings"
	"testing"
)

func TestBuiltinPersonasLoad(t *testing.T) {
	ps := Personas()
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"developer", "manager", "admin"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("built-ins %v missing %s", names, want)
		}
	}
}

func TestManagerPersonaShape(t *testing.T) {
	m, ok := Persona("manager")
	if !ok {
		t.Fatal("manager not found")
	}
	if m.DefaultMode != "autopilot" {
		t.Fatalf("default mode = %q", m.DefaultMode)
	}
	if got := strings.Join(m.ModeNames(), ","); got != "brief,autopilot,ask" {
		t.Fatalf("modes = %s", got)
	}
	for _, banned := range []string{"\"Brief\" section", "\"Autopilot\" section"} {
		if strings.Contains(m.Body, banned) {
			t.Fatalf("manager prompt must not reference capability guide sections by the old names: %s", banned)
		}
	}
	if !strings.Contains(m.Body, "Converge") {
		t.Fatal("manager modes should drive toward capability Converge sections")
	}
}

func TestDeveloperPersonaShape(t *testing.T) {
	d, ok := Persona("developer")
	if !ok {
		t.Fatal("developer not found")
	}
	if d.Launch != "hook" {
		t.Fatalf("developer launches via plugin hook, got %q", d.Launch)
	}
	if len(d.Modes) != 0 {
		t.Fatalf("developer declares no modes: %v", d.ModeNames())
	}
}

func TestPersonaUnknown(t *testing.T) {
	if _, ok := Persona("nope"); ok {
		t.Fatal("unknown persona must report !ok")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./skills/ 2>&1 | head -3`
Expected: FAIL — `Personas` undefined.

- [ ] **Step 3: Write the persona files and the embed loader**

`skills/persona/developer.md`:

```markdown
---
name: developer
description: Default working persona: implements features, fixes, and chores.
launch: hook
---
# Persona: developer

You are a developer working in an ATM developing session. Implement features,
fixes, and chores to a high standard: small, well-bounded changes; tests
before implementation; frequent commits; and clear task-comment records of
intent, decisions, and results.
```

`skills/persona/admin.md`:

```markdown
---
name: admin
description: Human operator persona: a person driving ATM directly via the CLI or TUI, not an autonomous agent.
---
# Persona: admin

You are the human operator of ATM, acting directly through the CLI or TUI
rather than as an autonomous agent. Keep the ledger honest and legible: record
intent and outcomes plainly, and prefer small, reversible changes.
```

`skills/persona/manager.md`:

```markdown
---
name: manager
description: Curates the ledger and oversees work.
modes:
  brief: Interview the human to set up or adjust each capability's data.
  autopilot: Autonomously converge each capability's data toward its guide.
  ask: Read-only standby for questions about the ledger.
default_mode: autopilot
---
# Persona: manager

You are a manager persona. Keep the ATM ledger accurate and legible: organize
tasks and labels, summarize progress, surface blockers, and hold a high bar on
scope and clarity rather than writing feature code yourself.

## Principles

- **Ownership**: you are the autonomous owner of everything in this project's
  ledger. You keep track of all of it and present it — organized and easy to
  digest — for the AI agents and humans you serve, and for yourself: clients
  ask you to recall and curate what the project knows, so your own memory must
  stay legible.
- **Dive deep**: stay connected to the details and work relentlessly to
  surface current information. Understand the project's past, present, and
  future, and stay informed in every conversation — the code itself and all
  documentation — to better assist humans and agents alike.
- **Simplify**: relentlessly and frequently organize the project. Create order
  from chaos and turn complex things into simple narratives. Keep
  documentation easy to digest to aid external communication.
- **Earn trust**: watch for the errors and friction that surface during
  sessions and track them down. Manage your own self-improvement as its own
  tasks, kept separate from project work. Your improvement window is the label
  substrate itself — you sharpen how its logic is expressed; you do not edit
  this prompt.

## Working over capabilities

Capabilities define the semantics: how this project's data is organized, the
verbs that move it, and what a converged state looks like. You are the
function that operates over them — you learn them at runtime and assume
nothing about them in advance.

- Enumerate the enabled set with `atm capability list --project <CODE>`.
- For each capability, run `atm capability <name> guide`: its `Semantics`
  section is the data model, `Actions` the verbs, and `Converge` the target
  state you drive toward. The whole guide is your reference when the human
  asks questions.
- Triage the unmanaged tail — last, once. After every capability's work is
  done, run `atm capability unmanaged --project <CODE>`. Use what you learned
  from each capability to decide, for each unmanaged label, whether its tasks
  should carry a capability-owned label instead (replace via `atm task label
  remove` + `atm task label add`); hide namespaces deliberately kept out of
  view with `atm project boards hide --project <CODE> --name <CODE>:<ns>:*`.
  Re-run `capability unmanaged` to verify the tail shrank. Do not delete
  labels or hide boards the human curated without asking.

Whatever the mode: keep the ledger legible, ground every answer in cited
task/comment IDs, and ask the human one-by-one when a task's intent is
unclear.

## Mode: brief

Interview the human to set up or adjust each capability's data — one
capability at a time, one question at a time. For each capability in scope:

1. Read its guide. Walk the human through its `Semantics`: the vocabulary,
   the boards, and what each is for. Confirm the project will use them as-is;
   record requested deviations where the guide directs (label descriptions,
   task comments) rather than inventing new vocabulary.
2. Treat its `Converge` section as a checklist of what a set-up project has
   recorded. For every item that is missing or stale, ask the human for it
   and record the answer where the guide says it lives.
3. Learn by example: pick a real task from the project's boards, ask the
   human how it should be handled under this capability, and record the
   answer as a task comment. If the answer reveals a convention rather than a
   one-off, record it where the guide keeps conventions — after the human
   confirms.

## Mode: autopilot

Autonomously converge each capability's data toward its guide. For each
capability in scope: read its guide's `Converge` section and drive the
project's data toward that state using the verbs in `Actions`. Exercise good
judgement autonomously — decide what you can, escalate only what genuinely
needs a human. When a decision is ambiguous and a human is reachable, surface
it; when not, decide, record the reasoning as a task comment, and flag it for
the next review. Never let ambiguity stall the loop silently, and never
rework data a human deliberately curated without recording why.

## Mode: ask

Standby for the human's questions; do not act proactively and do not mutate
the ledger. Read the guides of the capabilities in scope so answers are
grounded, and cite task/comment IDs in every answer.
```

`skills/skills.go`:

```go
package skills

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
)

// Task 3 widens this to `persona/*.md capability/*.md` once capability files
// exist (go:embed fails to compile against an empty glob).
//
//go:embed persona/*.md
var files embed.FS

var (
	builtinPersonas     []PersonaSpec
	builtinCapabilities []CapabilitySpec
)

func init() {
	builtinPersonas = mustLoadPersonas()
	builtinCapabilities = mustLoadCapabilities()
}

func mustLoadPersonas() []PersonaSpec {
	var out []PersonaSpec
	for _, name := range mustList("persona") {
		src, err := files.ReadFile(path.Join("persona", name))
		if err != nil {
			panic(fmt.Sprintf("skills: read %s: %v", name, err))
		}
		p, err := ParsePersona(strings.TrimSuffix(name, ".md"), src)
		if err != nil {
			panic(fmt.Sprintf("skills: %v", err))
		}
		out = append(out, p)
	}
	return out
}

func mustLoadCapabilities() []CapabilitySpec {
	var out []CapabilitySpec
	for _, name := range mustList("capability") {
		src, err := files.ReadFile(path.Join("capability", name))
		if err != nil {
			panic(fmt.Sprintf("skills: read %s: %v", name, err))
		}
		c, err := ParseCapability(strings.TrimSuffix(name, ".md"), src)
		if err != nil {
			panic(fmt.Sprintf("skills: %v", err))
		}
		out = append(out, c)
	}
	return out
}

func mustList(dir string) []string {
	entries, err := files.ReadDir(dir)
	if err != nil {
		// capability/ may be empty until Task 3; treat missing dir as empty.
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// Personas returns the built-in persona specs (stable, name-sorted order).
func Personas() []PersonaSpec { return append([]PersonaSpec(nil), builtinPersonas...) }

// Persona returns the named built-in persona.
func Persona(name string) (PersonaSpec, bool) {
	for _, p := range builtinPersonas {
		if p.Name == name {
			return p, true
		}
	}
	return PersonaSpec{}, false
}

// Capabilities returns the built-in capability specs (name-sorted order).
func Capabilities() []CapabilitySpec { return append([]CapabilitySpec(nil), builtinCapabilities...) }

// Capability returns the named built-in capability spec.
func Capability(name string) (CapabilitySpec, bool) {
	for _, c := range builtinCapabilities {
		if c.Name == name {
			return c, true
		}
	}
	return CapabilitySpec{}, false
}

// MustCapability is Capability for compile-time-known names (capability
// packages naming their own file); it panics on a missing file, which a unit
// test in the capability package catches.
func MustCapability(name string) CapabilitySpec {
	c, ok := Capability(name)
	if !ok {
		panic(fmt.Sprintf("skills: no capability file for %q", name))
	}
	return c
}
```

Note: `//go:embed capability/*.md` fails to compile while `skills/capability/` is empty. Create a placeholder that Task 3 replaces — add the real `workflow.md` in this task instead is wrong (Task 3 owns guide content). Resolution: in THIS task create the directory with the three capability files as **verbatim copies** prefixed with minimal frontmatter is also Task 3's job. Simplest correct sequencing: in this task, embed only `persona/*.md` (`//go:embed persona/*.md`) and have `mustList("capability")` return nil; Task 3 widens the embed directive to `persona/*.md capability/*.md` when the files exist. Implement it that way.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./skills/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add skills/skills.go skills/skills_test.go skills/persona/
git commit -m "feat(ATM-0772ea): built-in persona files — developer, manager (modes), admin"
```

---

### Task 3: Capability prompt files + rewire capability packages

**Files:**
- Create: `skills/capability/workflow.md`, `skills/capability/workflow_ai.md`, `skills/capability/contextmap.md`
- Modify: `skills/skills.go` (embed directive → `//go:embed persona/*.md capability/*.md`)
- Modify: `internal/capability/workflow/guide.go`, `internal/capability/workflowai/guide.go`, `internal/capability/contextmap/guide.go`
- Delete: `internal/capability/workflow/guide.md`, `internal/capability/workflowai/guide.md`, `internal/capability/contextmap/guide.md`
- Modify: `internal/capability/capability.go:72-77` (interface contract comment)
- Modify: `internal/capability/guide_test.go` (assert new sections)
- Test: `skills/skills_test.go` additions; per-capability vocabulary-consistency tests

**Interfaces:**
- Consumes: `skills.MustCapability(name).Body` / `.Description`.
- Produces: rewritten guides whose sections are `Semantics` / `Actions` / `Converge` (plus capability-specific extras). No `## Brief` / `## Autopilot` anywhere.

- [ ] **Step 1: Write the failing tests**

Append to `skills/skills_test.go`:

```go
func TestBuiltinCapabilitiesLoad(t *testing.T) {
	cs := Capabilities()
	if len(cs) != 3 {
		t.Fatalf("want 3 built-in capabilities, got %d", len(cs))
	}
	for _, c := range cs {
		if strings.Contains(c.Body, "## Brief") || strings.Contains(c.Body, "## Autopilot") {
			t.Errorf("%s: persona-specific Brief/Autopilot sections must not appear in capability files", c.Name)
		}
	}
	if _, ok := Capability("workflow_ai"); !ok {
		t.Fatal("workflow_ai missing")
	}
}
```

In `internal/capability/workflowai/` add `guide_skills_test.go` (mirror the same file for `workflow` and `contextmap` with their names):

```go
package workflowai

import (
	"strings"
	"testing"

	"atm/skills"
)

// The skills file's frontmatter labels must agree with the vocabulary the
// package actually manages, so documentation and code cannot drift.
func TestSkillsFileMatchesVocabulary(t *testing.T) {
	spec := skills.MustCapability(Cap{}.Name())
	if spec.Description != (Cap{}).Summary() {
		t.Fatalf("Summary() %q != frontmatter description %q", (Cap{}).Summary(), spec.Description)
	}
	guide := (Cap{}).Guide()
	for _, sec := range []string{"## Semantics", "## Actions", "## Converge"} {
		if !strings.Contains(guide, sec) {
			t.Fatalf("guide missing %s", sec)
		}
	}
}
```

(If `Cap` has a different zero-value constructor in a package — check `New()` in each package and use it: `New().Summary()` etc. Adjust to the package's actual constructor; all three expose `New()`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./skills/ ./internal/capability/... 2>&1 | head -5`
Expected: FAIL — capability files missing / `atm/skills` not imported.

- [ ] **Step 3: Write the capability files**

`skills/capability/workflow.md` — full content (Semantics merges the old "What it means" + "Vocabulary"; Actions lists the verbs; Converge is the persona-agnostic rewrite of the old Brief/Autopilot):

```markdown
---
name: workflow
description: Status transitions and planning priority for tasks: the paved road for the status:* and priority:* namespaces.
labels: [status:*, priority:*]
boards: [backlog, open-tasks, in-progress-tasks, all-tasks]
---
# Workflow capability — agent guide

Status transitions and planning priority for tasks: the paved road for the `status:*` and `priority:*` namespaces.

## Semantics

Each mutating verb swaps the task's `status:*` label (adds the target, removes any other), so exactly-one-status is an invariant the capability maintains. The store enforces nothing: raw `atm task label add/remove --label <CODE>:status:<value>` works; a human may hand-assign, rename, or delete any status or priority label. This is a paved road, not a fence.

Status (lifecycle — exactly one per task):
- `status:open` — not done.
- `status:in-progress` — someone is on it.
- `status:blocked` — stuck.
- `status:done` — stop.

Priority (planning — at most one per task, absent means default):
- `priority:high` — do this first.
- `priority:medium` — do after high-priority work.
- `priority:low` — do when no higher-priority work remains.

Boards (declared by this capability, seeded by `atm capability workflow seed` / project create):
- `<CODE>:backlog` (`NOT status:*`) — untriaged jottings.
- `<CODE>:open-tasks` (`status:open`).
- `<CODE>:in-progress-tasks` (`status:in-progress`).
- `<CODE>:all-tasks` (`*`) — every task; the TUI's default-selected board.

The vocabulary is fixed — four status values and three priority values, no more. If the humans want extra values (e.g. `status:review`, `priority:critical`), they may hand-add them via `atm label add`; the verbs only swap the four seeded statuses, so any extra state must be hand-assigned, and the decision belongs in the affected label's description.

## Actions

- `atm capability workflow start --task <ID>` — swap to `status:in-progress`.
- `atm capability workflow open --task <ID>` — swap to `status:open`.
- `atm capability workflow block --task <ID>` — swap to `status:blocked`.
- `atm capability workflow complete --task <ID>` — swap to `status:done`.
- `atm capability workflow status --project <CODE>` — read-only status report.
- `atm capability workflow seed --project <CODE>` — idempotently ensure the status/priority vocabulary and the boards.

Priority is never touched by the verbs — assign it by hand with `atm task label add --label <CODE>:priority:<value>`.

## Converge

A converged project under this capability looks like:

- **Invariants hold.** Every task carries exactly one `status:*`; at most one `priority:*`. The boards never present conflicting views.
- **The backlog is triaged, not a graveyard.** Every task on `<CODE>:backlog` either gets a status or is rejected — a task not worth doing is labeled `status:done` with a comment saying why, or removed.
- **The in-progress list is honest.** Each `status:in-progress` task is actually being worked; finished work is completed, stuck work is blocked so the blocker is visible.
- **Open work is ordered.** `<CODE>:open-tasks` and `<CODE>:in-progress-tasks` carry `priority:*` labels that reflect the current plan.
- **Done tasks have honest ledgers.** The title matches what was actually done, comments record the decision and result, and any `context:` pointers touched are stamped. A closed task with a stale title or missing outcome is a future agent's trap.
- **The boards stay alive.** `atm capability workflow seed --project <CODE>` is idempotent; a missing board means it should be run.
- **The vocabulary stays deliberate.** New status/priority values appear only by explicit human decision, recorded in the label's description — never invented on the human's behalf.
```

`skills/capability/workflow_ai.md`:

```markdown
---
name: workflow_ai
description: AI-native task cycle (brainstorm→clarify→plan→ready) with links, plan tracking, and stage boards.
labels: [stage:*, wfai:*]
boards: [new-tasks, brainstormed-tasks, planned-tasks, revisions, done-tasks]
---
# workflow_ai capability — agent guide

The AI-native task cycle: brainstorm → clarify → plan → ready → implement → done, over the `stage:*` namespace, with task links and plan tracking in this capability's metadata key. Fully independent of the `workflow` capability (`status:*`): disjoint namespaces, no interplay — a task may carry both views.

## Semantics

Stage verbs climb the ladder one rung at a time; each swaps the task's `stage:*` label (adds the target, removes any other), so exactly-one-stage is an invariant the capability maintains. "New" is the absence of any stage label, not a label. The store enforces nothing: raw `atm task label add/remove` works. This is a paved road, not a fence.

There is no `implement` verb: implementation is a dev session. The gate is doctrine — **never implement a task whose stage is not `stage:implementable`**; check the stage first and refuse otherwise.

Stages (exactly one per task; absence = new):
- `stage:brainstormed` — the idea has been explored.
- `stage:clarified` — scope and success criteria settled.
- `stage:planned` — a plan locator is recorded in this capability's metadata.
- `stage:implementable` — planned AND sized for one implementation session; cleared for implementation.
- `stage:done` — completed the cycle.

Markers:
- `wfai:revision` — this task is a revision follow-up of a bigger planned task; the link itself (`revision_of`) lives in the metadata payload, the marker only makes it board-visible.
- `wfai:framework` — a stored label (not stamped on tasks) carrying the project's framework conventions in its description; written during setup, read at session start. See Converge.

Boards (seeded by `atm capability workflow_ai seed` / project create):
- `<CODE>:new-tasks` (`NOT stage:*`) — the intake queue, not yet brainstormed.
- `<CODE>:brainstormed-tasks` (brainstormed or clarified) — in refinement.
- `<CODE>:planned-tasks` (planned or implementable) — has a recorded plan.
- `<CODE>:revisions` (revision marker, not done) — follow-ups still needing refinement.
- `<CODE>:done-tasks` (`stage:done`).

Sizing doctrine: a task is sized to one plan a framework like superpowers can execute in a single session. When a planned task is bigger than that, split it: create follow-up tasks linked with `--revision-of`, each entering the cycle at its own stage. The revisions board is the refinement queue.

## Actions

- `atm capability workflow_ai brainstorm|clarify|ready|done --task <ID>` — climb one rung (swap the stage label).
- `atm capability workflow_ai plan --task <ID> --kind file|commit|ephemeral --ref <ref>` — record WHERE the plan lives: `--kind file --ref docs/...` (repo-relative), `--kind commit --ref <rev>`, or `--kind ephemeral --ref "session ..."` for a plan that lives in a conversation. Record ephemeral plans honestly — they are unverifiable and always at-risk.
- `atm capability workflow_ai demote --task <ID> --reason "..."` — reset any stage back to new, clear the plan record, log the reason as a comment.
- `atm capability workflow_ai link --task X --revision-of Y` — X is a refinement follow-up of Y (one parent max). `--relates-to Y` — generic association. `unlink` reverses either. `links --task X` shows both directions.
- `atm capability workflow_ai report --project <CODE>` — the staleness check: verifies every planned/implementable task's plan (file existence, commit resolvability from the current directory) and lists what is at risk. The reporter never demotes; the operator decides, then `demote --reason`.
- `atm capability workflow_ai seed --project <CODE>` — idempotently ensure vocabulary and boards.

## Converge

A converged project under this capability looks like:

- **The framework conventions are recorded and current.** The `<CODE>:wfai:framework` label's description says which framework the project uses (superpowers, speckit, grillme, or none), where plans normally live (committed plan docs vs ephemeral sessions), sizing expectations, and any customizations. Agents read it at session start (`atm label show <CODE>:wfai:framework`) and bend accordingly; when practice drifts from what it says, it gets updated — convention changes are confirmed with the decider before the label is rewritten. Where plans normally live is also recorded in the `stage:planned` label description. Specific one-off answers stay as task comments; only conventions live in `wfai:framework`.
- **Every stage is evidenced.** A `stage:brainstormed` task has exploration notes; `stage:clarified` has settled scope and success criteria; `stage:planned`/`stage:implementable` has a locatable plan (`report` verifies). Tasks whose evidence has decayed are demoted with a reason — replanning is cheaper than implementing against a ghost.
- **The intake queue is triaged.** Tasks on `<CODE>:new-tasks` worth pursuing are brainstormed; the rest are left deliberately. Ambiguity goes to the decider — or is decided, recorded as a task comment, and flagged for the next decider review. Never let ambiguity stall silently.
- **No skipped rungs, no premature implementation.** Tasks advance one rung at a time and only when the next rung's evidence exists; nothing below `stage:implementable` is implemented.
- **Links hold.** Every `revision_of`/`relates_to` link points at a live task and a relationship that still holds; stale links are unlinked. Oversized planned tasks are split into `--revision-of` follow-ups.
- **The vocabulary is fixed.** Five stages, absence-as-new, five boards, two link types. Extra stages are not part of the paved road.
```

`skills/capability/contextmap.md`:

```markdown
---
name: contextmap
description: Context pointers with provenance: record what knowledge derives from, so drift can be detected.
labels: [context:*, knowledge:superseded, comment:provenance]
boards: [context-current]
---
# Context map capability — agent guide

Context pointers with provenance: record what knowledge derives from, so drift can be detected.

## Semantics

Context pointers record what they were derived from, so drift can be detected. `atm capability contextmap check --project <CODE>` reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means.

**Ground truth is the code.** This map is reference-only: always verify what a pointer claims against the functioning repo before acting on it. Use the map to discover the bigger picture — repos, external systems, docs, conventions — that lives outside the code itself, not as a substitute for reading the code.

Vocabulary:
- `context:agent` / `context:repository` / `context:documentation` / `context:convention` — pointer kinds.
- `knowledge:superseded` — lifecycle: this pointer is obsolete; its successor is named in the description.
- `comment:provenance` — machine-written provenance stamp on a pointer's task; do not hand-edit.
- `<CODE>:context-current` board (`context:* AND NOT knowledge:superseded`) — current knowledge.

Read the project's current knowledge from `<CODE>:context-current` (`atm task list --project <CODE> --label <CODE>:context-current`): pointers not superseded. Narrow by kind with `--label <CODE>:context:agent`.

## Actions

- `atm capability contextmap add --task <ID> --kind <kind> --source <kinded-locator>` — make a task a context pointer, stamp provenance.
- `atm capability contextmap stamp --task <ID>` — re-verify: the subject is unchanged in meaning.
- `atm capability contextmap retarget --task <ID> --source <kinded-locator>` — the subject survived but moved.
- `atm capability contextmap supersede --task <ID> --by <NEW-ID> --reason "..."` — the subject died; history kept.
- `atm capability contextmap check --project <CODE>` — report drift (read-only).

## Converge

A converged map answers, with provenance, what an agent joining the project needs to know:

- **The territory is mapped.** The map records the project's repositories (`context:repository` — local path and/or remote URL, the branches that matter), its authoritative and complementary documents (`context:documentation` — READMEs, architecture notes, ADRs, specs, external trackers/runbooks), its day-to-day process and any agent workflow (`context:agent` — spec → plan → issues → implementation, harness in use), and its conventions (`context:convention` — branch naming, PR template, commit style, hygiene). The default naming convention, unless the project records otherwise: work happens on a worktree branch named `worktree-atm-<taskid>-<slug>` (or a feature branch `feature/ATM-<taskid>-<slug>` for merge-style work), and commits follow `<type>(ATM-<taskid>): <summary>` — both keyed off the ATM task the work serves. A pointer with a known unknown noted in its description beats a guess.
- **Every pointer is witnessed.** `check` reports are worked to closure: `DRIFT` pointers are re-read against the actual change and stamped, retargeted, or superseded; `AGE` pointers naming external systems are re-read at the source and stamped; `UNVERIFIED` hand-written pointers are read, confirmed, and given a `--source`.
- **New territory is claimed.** Changes in git that no pointer covers are either recorded as new pointers or deliberately ignored — that judgement belongs to the operator, and `check` never makes it: a changed file is not a wrong pointer.
- **History is kept.** Dead knowledge is superseded, never deleted; the successor is named.
```

- [ ] **Step 4: Rewire the capability packages**

Widen the embed directive in `skills/skills.go`:

```go
//go:embed persona/*.md capability/*.md
var files embed.FS
```

Replace `internal/capability/workflowai/guide.go` entirely:

```go
package workflowai

import "atm/skills"

// Summary is the capability's one-line description for enumeration surfaces.
// Single source: the skills file's frontmatter description.
func (Cap) Summary() string { return skills.MustCapability("workflow_ai").Description }

// Guide is the capability's full agent-facing semantics; `atm capability
// workflow_ai guide` prints it verbatim from the skills file.
func (Cap) Guide() string { return skills.MustCapability("workflow_ai").Body }
```

Do the same for `internal/capability/workflow/guide.go` (name `"workflow"`) and `internal/capability/contextmap/guide.go` (name `"contextmap"`), keeping each package's existing receiver type (check the file — mirror whatever receiver `Summary`/`Guide` currently use). Delete the three `guide.md` files and their `//go:embed` variables.

Update the `Capability` interface doc comment at `internal/capability/capability.go:74-77` to:

```go
	// Guide is the capability's full agent-facing semantics: `## Semantics`
	// (data model and vocabulary), `## Actions` (exposed verbs), and
	// `## Converge` (what a healthy, converged data state looks like) —
	// persona-agnostic; personas decide what to DO with the converged state.
	// Served verbatim by the uniform `guide` subcommand.
	Guide() string
```

Update `internal/capability/guide_test.go`: wherever it asserts `## Brief` / `## Autopilot` presence, assert `## Semantics` / `## Actions` / `## Converge` instead (read the file first; keep its structure).

Also update `internal/developing/context_v1.md:15` comment text later (Task 5 deletes the file) — no action here.

Update `tests/arch/imports_test.go`:
- In `TestCapabilityPackagesImportOnlyRegistryAndCore`, add `"internal/capability/workflowai"` to the dir list (it was missing).
- Add:

```go
// TestSkillsIsAPureLeaf pins the prompt-hosting package as importable by
// every layer: it may import nothing from this repository.
func TestSkillsIsAPureLeaf(t *testing.T) {
	for f, imps := range internalImports(t, "skills") {
		t.Errorf("%s imports %v; skills may import nothing from this repository", f, imps)
	}
}
```

- [ ] **Step 5: Run the full test suite**

Run: `go build ./... && go test ./skills/ ./internal/capability/... ./tests/arch/`
Expected: PASS. (If `guide_test.go` or golden tests elsewhere assert old guide text, update those assertions to the new section names — the guide content intentionally changed.)

- [ ] **Step 6: Commit**

```bash
git add skills/ internal/capability/ tests/arch/imports_test.go
git commit -m "feat(ATM-0772ea): capability prompts move to skills/ — Semantics/Actions/Converge, Brief/Autopilot removed"
```

---

### Task 4: Store — built-ins from skills, markdown custom personas, personality overlays

**Files:**
- Modify: `internal/store/persona.go` (rewrite)
- Modify: `internal/store/store.go` (remove `ensureBuiltinPersonas`/`builtinsOnce` usage — find `builtinsOnce` declaration via `grep -n builtins internal/store/*.go` and delete the mechanism; `validateActor` call sites switch to the new existence check)
- Modify: `internal/core/service.go` `PersonaService` (add methods)
- Delete: `internal/seed/persona.go` (and the now-empty `internal/seed` package)
- Modify: `tests/arch/imports_test.go` (`TestOnlyEventlogImportsEventsourceLib` dir list: remove `"internal/seed"`, add `"skills"`, `"internal/session"` — session exists after Task 5; add it there if the helper fails on a missing dir)
- Test: `internal/store/persona_test.go` (update + extend)

**Interfaces:**
- Consumes: `skills.Personas()`, `skills.Persona(name)`.
- Produces (on `PersonaService` and `*Store`):
  - existing signatures unchanged: `CreatePersona(name, prompt, description, actor string) (*core.Persona, error)`, `GetPersona(name string) (*core.Persona, error)`, `ListPersonas() []*core.Persona`, `EditPersona(name string, prompt, description *string, actor string) (*core.Persona, error)`, `RemovePersona(name string) error`
  - new: `PersonaDoc(name string) (string, error)` — raw markdown document of a **custom** persona (usage error for built-ins); `GetPersonality(name string) (string, error)` — overlay text, `""` when none; `SetPersonality(name, text, actor string) error`; `ClearPersonality(name string) error`.

Semantics: built-ins (developer/manager/admin/concierge) resolve from `skills`, are never files in the store, cannot be removed or edited (`EditPersona` on a built-in → `core.ErrUsage`; personality overlay is the customization path). Custom personas persist as `<store>/personas/<name>.md` in the skills persona format with audit fields in frontmatter; legacy `<name>.json` files are migrated to `.md` on first read (json removed after successful write). Store files for built-in names left over from old seeding are ignored. Personality overlays live at `<store>/personas/<name>.personality.md` (plain text) and apply to any persona.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/persona_test.go` (keep existing tests; update any that assert seeding/JSON behavior — e.g. tests expecting `SeedPersonas` or `.json` files — to the new semantics):

```go
func TestBuiltinPersonasResolveWithoutSeeding(t *testing.T) {
	s := newTestStore(t) // use the file's existing store-constructor helper
	for _, name := range []string{"developer", "manager", "admin"} {
		p, err := s.GetPersona(name)
		if err != nil {
			t.Fatalf("GetPersona(%s): %v", name, err)
		}
		if p.Prompt == "" || p.Description == "" {
			t.Fatalf("built-in %s empty: %+v", name, p)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(s.Root, "personas")); len(entries) != 0 {
		t.Fatalf("built-ins must not be materialized in the store; found %d files", len(entries))
	}
}

func TestCustomPersonaMarkdownRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreatePersona("reviewer", "Review things.", "Reviews PRs.", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", "reviewer.md")); err != nil {
		t.Fatalf("custom persona must persist as markdown: %v", err)
	}
	p, err := s.GetPersona("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if p.Prompt != "Review things." || p.Description != "Reviews PRs." {
		t.Fatalf("round trip: %+v", p)
	}
	doc, err := s.PersonaDoc("reviewer")
	if err != nil || !strings.HasPrefix(doc, "---\n") {
		t.Fatalf("PersonaDoc: %q err=%v", doc, err)
	}
}

func TestLegacyJSONPersonaMigrates(t *testing.T) {
	s := newTestStore(t)
	old := core.Persona{Name: "legacy", Prompt: "Old prompt.", Description: "Old desc.",
		CreatedAt: core.Now(), UpdatedAt: core.Now(), CreatedBy: "a@b:c", UpdatedBy: "a@b:c"}
	if err := os.MkdirAll(filepath.Join(s.Root, "personas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(filepath.Join(s.Root, "personas", "legacy.json"), &old); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetPersona("legacy")
	if err != nil || p.Prompt != "Old prompt." {
		t.Fatalf("migrated read: %+v err=%v", p, err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", "legacy.md")); err != nil {
		t.Fatalf("json must convert to md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", "legacy.json")); !os.IsNotExist(err) {
		t.Fatal("json must be removed after migration")
	}
}

func TestBuiltinEditRefusedOverlayWorks(t *testing.T) {
	s := newTestStore(t)
	prompt := "x"
	if _, err := s.EditPersona("manager", &prompt, nil, "admin@cli:unset"); err == nil {
		t.Fatal("editing a built-in must fail; personality overlay is the customization path")
	}
	if err := s.SetPersonality("manager", "Dry wit.", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPersonality("manager")
	if err != nil || got != "Dry wit." {
		t.Fatalf("overlay: %q err=%v", got, err)
	}
	if err := s.ClearPersonality("manager"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPersonality("manager"); got != "" {
		t.Fatalf("cleared overlay still returns %q", got)
	}
	if err := s.SetPersonality("ghost", "x", "admin@cli:unset"); err == nil {
		t.Fatal("overlay for unknown persona must fail")
	}
}

func TestListPersonasMergesBuiltinsAndCustoms(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreatePersona("zed", "p", "d", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range s.ListPersonas() {
		names = append(names, p.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"developer", "manager", "admin", "zed"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("list %v missing %s", names, want)
		}
	}
}

func TestCreatePersonaRefusesBuiltinName(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreatePersona("manager", "p", "d", "admin@cli:unset"); err == nil {
		t.Fatal("shadowing a built-in must fail")
	}
}
```

(`newTestStore` stands for whatever helper the existing `persona_test.go` uses to construct a store — read the file and use its actual helper name.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'Persona|Builtin|Personality' 2>&1 | head -10`
Expected: FAIL (undefined methods, wrong persistence).

- [ ] **Step 3: Implement**

Rewrite `internal/store/persona.go`:

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"atm/internal/core"
	"atm/skills"
)

func (s *Store) personasDir() string { return filepath.Join(s.Root, "personas") }
func (s *Store) personaMDPath(name string) string {
	return filepath.Join(s.personasDir(), name+".md")
}
func (s *Store) personaJSONPath(name string) string {
	return filepath.Join(s.personasDir(), name+".json")
}
func (s *Store) personalityPath(name string) string {
	return filepath.Join(s.personasDir(), name+".personality.md")
}

// builtinPersona converts a skills built-in to the core persona shape.
// Built-ins have no audit trail: they ship with the binary.
func builtinPersona(spec skills.PersonaSpec) *core.Persona {
	return &core.Persona{
		Name:        spec.Name,
		Prompt:      spec.Body,
		Description: spec.Description,
		CreatedBy:   "builtin",
		UpdatedBy:   "builtin",
	}
}

// composePersonaDoc renders a custom persona as a skills-format markdown
// document with audit fields in frontmatter (the parser tolerates them).
func composePersonaDoc(p *core.Persona) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", p.Name)
	fmt.Fprintf(&b, "description: %s\n", sanitizeFrontmatterValue(p.Description))
	fmt.Fprintf(&b, "created_at: %s\n", p.CreatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "created_by: %s\n", p.CreatedBy)
	fmt.Fprintf(&b, "updated_at: %s\n", p.UpdatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "updated_by: %s\n", p.UpdatedBy)
	b.WriteString("---\n")
	b.WriteString(p.Prompt)
	if !strings.HasSuffix(p.Prompt, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// sanitizeFrontmatterValue keeps frontmatter one-line.
func sanitizeFrontmatterValue(v string) string {
	v = strings.ReplaceAll(v, "\n", " ")
	return strings.TrimSpace(v)
}

// parsePersonaDoc reads a stored custom-persona markdown file back into the
// core shape. The skills parser validates format; audit fields come from the
// tolerated extra frontmatter keys, re-read here.
func parsePersonaDoc(name string, src []byte) (*core.Persona, error) {
	spec, err := skills.ParsePersona(name, src)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", core.ErrUsage, err)
	}
	p := &core.Persona{Name: spec.Name, Prompt: spec.Body, Description: spec.Description}
	// Best-effort audit re-read: scan frontmatter lines only (up to the
	// closing --- delimiter).
	lines := strings.Split(string(src), "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			break
		}
		k, v, ok := strings.Cut(lines[i], ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "created_by":
			p.CreatedBy = v
		case "updated_by":
			p.UpdatedBy = v
		case "created_at":
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				p.CreatedAt = ts
			}
		case "updated_at":
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				p.UpdatedAt = ts
			}
		}
	}
	return p, nil
}

func (s *Store) CreatePersona(name, prompt, description, actor string) (*core.Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if _, ok := skills.Persona(name); ok {
		return nil, fmt.Errorf("%w: persona %q is built-in", core.ErrConflict, name)
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	var created *core.Persona
	err := s.WithLock("personas", func() error {
		if s.customExists(name) {
			return fmt.Errorf("%w: persona %q already exists", core.ErrConflict, name)
		}
		now := core.Now()
		p := &core.Persona{
			Name: name, Prompt: prompt, Description: description,
			CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor,
		}
		if p.Description == "" {
			p.Description = "Custom persona."
		}
		doc := composePersonaDoc(p)
		if _, err := parsePersonaDoc(name, []byte(doc)); err != nil {
			return err // format-enforced at the door
		}
		if err := os.MkdirAll(s.personasDir(), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(s.personaMDPath(name), []byte(doc), 0o644); err != nil {
			return err
		}
		created = p
		return nil
	})
	return created, err
}

func (s *Store) customExists(name string) bool {
	if _, err := os.Stat(s.personaMDPath(name)); err == nil {
		return true
	}
	if _, err := os.Stat(s.personaJSONPath(name)); err == nil {
		return true
	}
	return false
}

func (s *Store) GetPersona(name string) (*core.Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if spec, ok := skills.Persona(name); ok {
		return builtinPersona(spec), nil
	}
	if b, err := os.ReadFile(s.personaMDPath(name)); err == nil {
		return parsePersonaDoc(name, b)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Legacy JSON: migrate to markdown on first read.
	var legacy core.Persona
	if err := ReadJSON(s.personaJSONPath(name), &legacy); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: persona %q", core.ErrNotFound, name)
		}
		return nil, err
	}
	err := s.WithLock("personas", func() error {
		if err := os.WriteFile(s.personaMDPath(name), []byte(composePersonaDoc(&legacy)), 0o644); err != nil {
			return err
		}
		return os.Remove(s.personaJSONPath(name))
	})
	if err != nil {
		return nil, err
	}
	return &legacy, nil
}

func (s *Store) ListPersonas() []*core.Persona {
	var out []*core.Persona
	for _, spec := range skills.Personas() {
		out = append(out, builtinPersona(spec))
	}
	entries, err := os.ReadDir(s.personasDir())
	if err != nil {
		return out
	}
	seen := map[string]bool{}
	for _, p := range out {
		seen[p.Name] = true
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".md" && ext != ".json" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ext)
		if strings.HasSuffix(name, ".personality") || seen[name] {
			continue
		}
		if p, err := s.GetPersona(name); err == nil {
			seen[name] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) EditPersona(name string, prompt, description *string, actor string) (*core.Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if _, ok := skills.Persona(name); ok {
		return nil, fmt.Errorf("%w: persona %q is built-in; customize it via `atm persona personality`", core.ErrUsage, name)
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	var updated *core.Persona
	err := s.WithLock("personas", func() error {
		p, err := s.GetPersona(name)
		if err != nil {
			return err
		}
		if prompt != nil {
			p.Prompt = *prompt
		}
		if description != nil {
			p.Description = *description
		}
		p.UpdatedAt = core.Now()
		p.UpdatedBy = actor
		doc := composePersonaDoc(p)
		if _, err := parsePersonaDoc(name, []byte(doc)); err != nil {
			return err
		}
		if err := os.WriteFile(s.personaMDPath(name), []byte(doc), 0o644); err != nil {
			return err
		}
		updated = p
		return nil
	})
	return updated, err
}

func (s *Store) RemovePersona(name string) error {
	if err := core.ValidatePersonaName(name); err != nil {
		return err
	}
	if _, ok := skills.Persona(name); ok {
		return fmt.Errorf("%w: cannot remove built-in persona %q", core.ErrUsage, name)
	}
	return s.WithLock("personas", func() error {
		if _, err := s.GetPersona(name); err != nil {
			return err
		}
		_ = os.Remove(s.personaJSONPath(name))
		_ = os.Remove(s.personalityPath(name))
		return os.Remove(s.personaMDPath(name))
	})
}

// PersonaDoc returns the raw markdown document of a custom persona. Built-ins
// live in the binary; callers use the skills package for them.
func (s *Store) PersonaDoc(name string) (string, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return "", err
	}
	if _, ok := skills.Persona(name); ok {
		return "", fmt.Errorf("%w: persona %q is built-in", core.ErrUsage, name)
	}
	if _, err := s.GetPersona(name); err != nil { // triggers JSON migration
		return "", err
	}
	b, err := os.ReadFile(s.personaMDPath(name))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// personaExists reports whether name is a built-in or a stored custom.
func (s *Store) personaExists(name string) bool {
	if _, ok := skills.Persona(name); ok {
		return true
	}
	return s.customExists(name)
}

// GetPersonality returns the personality overlay text ("" when none is set).
func (s *Store) GetPersonality(name string) (string, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return "", err
	}
	if !s.personaExists(name) {
		return "", fmt.Errorf("%w: persona %q", core.ErrNotFound, name)
	}
	b, err := os.ReadFile(s.personalityPath(name))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (s *Store) SetPersonality(name, text, actor string) error {
	if err := core.ValidatePersonaName(name); err != nil {
		return err
	}
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if !s.personaExists(name) {
		return fmt.Errorf("%w: persona %q", core.ErrNotFound, name)
	}
	return s.WithLock("personas", func() error {
		if err := os.MkdirAll(s.personasDir(), 0o755); err != nil {
			return err
		}
		return os.WriteFile(s.personalityPath(name), []byte(strings.TrimSpace(text)+"\n"), 0o644)
	})
}

func (s *Store) ClearPersonality(name string) error {
	if err := core.ValidatePersonaName(name); err != nil {
		return err
	}
	if !s.personaExists(name) {
		return fmt.Errorf("%w: persona %q", core.ErrNotFound, name)
	}
	return s.WithLock("personas", func() error {
		err := os.Remove(s.personalityPath(name))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	})
}
```

In `internal/store/store.go`: delete `ensureBuiltinPersonas` and the `builtinsOnce`/`builtinsErr` fields (grep `builtins` across `internal/store/*.go` for the declaration and every call site — typically `validateActor`). Replace the persona-existence check inside `validateActor` (find it in `internal/store/actor_validate.go`) with `s.personaExists(persona)`.

In `internal/core/service.go`, extend `PersonaService`:

```go
type PersonaService interface {
	CreatePersona(name, prompt, description, actor string) (*Persona, error)
	GetPersona(name string) (*Persona, error)
	ListPersonas() []*Persona
	EditPersona(name string, prompt, description *string, actor string) (*Persona, error)
	RemovePersona(name string) error
	// PersonaDoc returns a custom persona's raw markdown document (usage
	// error for built-ins, which ship inside the binary).
	PersonaDoc(name string) (string, error)
	// Personality overlay: a user-set `## Personality` override applied over
	// the persona's own default at session render time. "" = none.
	GetPersonality(name string) (string, error)
	SetPersonality(name, text, actor string) error
	ClearPersonality(name string) error
}
```

Delete `internal/seed/persona.go` and remove the empty `internal/seed` directory; remove `"internal/seed"` from the `TestOnlyEventlogImportsEventsourceLib` dir list and add `"skills"`.

- [ ] **Step 4: Run the suite; fix fallout**

Run: `go build ./... && go test ./internal/store/ ./internal/core/ ./tests/arch/ 2>&1 | tail -20`
Expected: PASS after updating any store tests that asserted seeding (`SeedPersonas` gone), `.json` persistence for built-ins, or `EditPersona` on built-ins. TUI compiles unchanged (it only calls `CreatePersona`/`core.ValidatePersonaName`). Then `go test ./...` — fix any remaining consumer of `seed.Personas` (grep `internal/seed`).

- [ ] **Step 5: Commit**

```bash
git add internal/store/persona.go internal/store/store.go internal/store/actor_validate.go internal/store/persona_test.go internal/core/service.go tests/arch/imports_test.go
git rm -r internal/seed
git commit -m "feat(ATM-0772ea): store resolves built-in personas from skills; markdown customs; personality overlays"
```

---

### Task 5: `internal/session` — unified launcher and context template

**Files:**
- Create: `internal/session/launcher.go`, `internal/session/context.go`, `internal/session/context_v1.md`
- Test: `internal/session/context_test.go`, `internal/session/launcher_test.go` (port the relevant cases from `internal/developing/launcher_test.go` and `internal/manager/context_test.go` — read them first and carry over what still applies)

**Interfaces:**
- Consumes: `skills.PersonaSpec`.
- Produces:
  - `session.Launcher` interface: `Name() string`, `NotFoundHint() string`, `BuildArgv() []string`, `BuildArgvPrompt(contextPath string) []string`
  - `session.LauncherFor(name string) (Launcher, bool)`; `session.OllamaLauncher{Integration string}`
  - `session.ContextData{Code, Name, Actor string; Spec skills.PersonaSpec; Personality, Mode, Capability string}`; `session.RenderContext(ContextData) string`
  - `session.PromptMessage(contextPath string) string`

`internal/developing` and `internal/manager` are NOT deleted in this task (the old commands still compile against them); Task 6 deletes them.

- [ ] **Step 1: Write the failing tests**

`internal/session/context_test.go`:

```go
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
```

`internal/session/launcher_test.go`:

```go
package session

import (
	"strings"
	"testing"
)

func TestLauncherFor(t *testing.T) {
	for _, name := range []string{"opencode", "codex", "claude"} {
		l, ok := LauncherFor(name)
		if !ok || l.Name() != name {
			t.Fatalf("LauncherFor(%s) = %v %v", name, l, ok)
		}
		if got := l.BuildArgv(); len(got) == 0 || got[0] != name {
			t.Fatalf("BuildArgv = %v", got)
		}
	}
	if _, ok := LauncherFor("nope"); ok {
		t.Fatal("unknown launcher must be !ok")
	}
}

func TestBuildArgvPrompt(t *testing.T) {
	l, _ := LauncherFor("claude")
	argv := l.BuildArgvPrompt("/tmp/ctx.md")
	if argv[0] != "claude" || !strings.Contains(strings.Join(argv, " "), "/tmp/ctx.md") {
		t.Fatalf("argv = %v", argv)
	}
	oc, _ := LauncherFor("opencode")
	ocArgv := oc.BuildArgvPrompt("/tmp/ctx.md")
	if ocArgv[1] != "--prompt" {
		t.Fatalf("opencode uses --prompt: %v", ocArgv)
	}
	ol := OllamaLauncher{Integration: "opencode"}
	olArgv := ol.BuildArgvPrompt("/tmp/ctx.md")
	if olArgv[0] != "ollama" || olArgv[1] != "launch" || !strings.Contains(strings.Join(olArgv, " "), "--prompt") {
		t.Fatalf("ollama argv = %v", olArgv)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/ 2>&1 | head -3`
Expected: build failure — package doesn't exist.

- [ ] **Step 3: Implement**

`internal/session/launcher.go` (union of the two old launchers; `BuildArgvPrompt` generalizes `BuildArgvManage`):

```go
// Package session launches a host agent as a persona: it renders the unified
// context prompt and builds the host argv. It replaces the former
// internal/developing and internal/manager packages.
package session

type Launcher interface {
	Name() string
	NotFoundHint() string
	// BuildArgv launches the host bare (launch: hook personas — a session
	// plugin hook loads the context file from ATM_CONTEXT_FILE).
	BuildArgv() []string
	// BuildArgvPrompt launches the host with an initial message pointing at
	// the rendered context file (launch: prompt personas).
	BuildArgvPrompt(contextPath string) []string
}

const (
	promptMessagePrefix = "Read the session instructions in the file at "
	promptMessageSuffix = " and follow them exactly."
)

// PromptMessage is the initial message for launch:prompt personas.
func PromptMessage(contextPath string) string {
	return promptMessagePrefix + contextPath + promptMessageSuffix
}

type staticLauncher struct {
	name          string
	hint          string
	usePromptFlag bool
}

func (l staticLauncher) Name() string         { return l.name }
func (l staticLauncher) NotFoundHint() string { return l.hint }
func (l staticLauncher) BuildArgv() []string  { return []string{l.name} }

func (l staticLauncher) BuildArgvPrompt(contextPath string) []string {
	return append([]string{l.name}, msgArgv(l.usePromptFlag, PromptMessage(contextPath))...)
}

func msgArgv(usePromptFlag bool, msg string) []string {
	if usePromptFlag {
		return []string{"--prompt", msg}
	}
	return []string{msg}
}

func LauncherFor(name string) (Launcher, bool) {
	switch name {
	case "opencode":
		return staticLauncher{name: "opencode", hint: "https://opencode.ai", usePromptFlag: true}, true
	case "codex":
		return staticLauncher{name: "codex", hint: "https://developers.openai.com/codex"}, true
	case "claude":
		return staticLauncher{name: "claude", hint: "https://code.claude.com"}, true
	default:
		return nil, false
	}
}

// OllamaLauncher execs `ollama launch <integration> --`; extra args pass
// through after the separator. LauncherFor stays ok=false for "ollama"
// because the integration is not known at factory time.
type OllamaLauncher struct {
	Integration string
}

func (l OllamaLauncher) Name() string         { return "ollama" }
func (l OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv() []string {
	return []string{"ollama", "launch", l.Integration, "--"}
}

func (l OllamaLauncher) BuildArgvPrompt(contextPath string) []string {
	return append(l.BuildArgv(), msgArgv(l.Integration == "opencode", PromptMessage(contextPath))...)
}
```

`internal/session/context_v1.md`:

```markdown
# ATM session — <CODE>

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>`

<PERSONA_BLOCK>
<MODE_BLOCK>

## Orientation

ATM is the visible ledger for this work. Use it to record ideas, discussions, decisions, and progress as you go, and to find prior work and handoffs from earlier sessions. Start with the CLI landscape, read the conventions, then discover which capabilities this project has enabled and read each one's guide.

```
atm -h                                # general CLI landscape
atm conventions                       # what ATM is, the label substrate, the actor convention
atm capability list --project <CODE>  # which capabilities this project has enabled
atm capability <name> guide           # one capability's semantics, actions, and converged state
atm search --project <CODE> "..."     # find prior tasks, decisions, and handoffs before starting
```

Run `atm <cmd> --help` for exact flags. Stamp every ATM mutation with actor `<ACTOR>` — replace the `:unset` model segment with your actual model.

## Working Principles

- Respect the repository's existing process. ATM complements it; do not let it intrude or override project-specific prompts and workflows.
- Do the work and tell people. Journal frequently — ideas, decisions, and progress recorded now save a future agent from re-deriving them.
```

`internal/session/context.go`:

```go
package session

import (
	_ "embed"
	"fmt"
	"strings"

	"atm/skills"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code  string
	Name  string
	Actor string
	// Spec is the resolved persona (built-in from skills, or a custom persona
	// parsed into the same shape by the CLI).
	Spec skills.PersonaSpec
	// Personality overrides the spec's default personality section ("" keeps
	// the default).
	Personality string
	// Mode selects one declared mode; "" renders no mode block.
	Mode string
	// Capability scopes the session to one enabled capability ("" = all).
	Capability string
}

// RenderContext substitutes ContextData into the session template. Empty
// Code/Name/Actor leave their placeholders literal so a generic template can
// be produced (`atm session-context` with no --project).
func RenderContext(d ContextData) string {
	personality := d.Spec.Personality
	if d.Personality != "" {
		personality = d.Personality
	}
	var pb strings.Builder
	fmt.Fprintf(&pb, "## Persona: %s\n\n%s\n\n%s\n", d.Spec.Name, d.Spec.Description, d.Spec.CorePrompt)
	if personality != "" {
		fmt.Fprintf(&pb, "\n### Personality\n\n%s\n", personality)
	}
	pb.WriteString("\nYou are operating as this persona. Hold to its principles throughout the session, alongside repo instructions and the working routine below.\n")

	modeBlock := ""
	if d.Mode != "" {
		if m, ok := d.Spec.Mode(d.Mode); ok {
			var mb strings.Builder
			fmt.Fprintf(&mb, "## Mode: %s\n\n%s\n", m.Name, m.Instructions)
			if d.Capability != "" {
				fmt.Fprintf(&mb, "\nScope: limit this session to the `%s` capability.\n", d.Capability)
			}
			modeBlock = mb.String()
		}
	}

	pairs := []string{
		"<CODE>", d.Code,
		"<PROJECT_NAME>", d.Name,
		"<ACTOR>", d.Actor,
		"<PERSONA_BLOCK>", pb.String(),
		"<MODE_BLOCK>", modeBlock,
	}
	final := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		if val == "" && key != "<PERSONA_BLOCK>" && key != "<MODE_BLOCK>" {
			final = append(final, key, key) // keep placeholder literal
		} else {
			final = append(final, key, val)
		}
	}
	return strings.NewReplacer(final...).Replace(contextV1)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/session/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/
git commit -m "feat(ATM-0772ea): internal/session — unified launcher and persona-generic context template"
```

---

### Task 6: CLI — `atm --persona` dispatch, `session-context`, remove `dev`/`manage`

**Files:**
- Create: `internal/cli/session.go`
- Modify: `internal/cli/root.go` (root flags + dispatch; drop `newDevCmd`/`newManageCmd` mounts; mount `newSessionContextCmd` + `newManageContextCmd` alias)
- Modify: `internal/cli/agent_resolve.go` (one launcher mapping)
- Modify: `internal/cli/launcher_shared.go` (`contextCachePath`/`cacheKey` new signature; `emitLaunchHeader/Tail` role param now the persona name — signatures unchanged)
- Delete: `internal/cli/developing.go`, most of `internal/cli/manager.go` (keep the plugin subcommands — move `newManagerPluginCmd`/`managerPluginAgents` and the plugin status/install cmds into a new `internal/cli/plugins_manager.go`; note `newManagerPluginCmd` is currently unmounted dead code — keep it that way or delete it, but `atm init` uses `manager.InstallPlugin`, which Task 8 rehomes)
- Delete: `internal/developing/context.go`, `internal/developing/context_v1.md`, `internal/developing/launcher.go` and their tests; `internal/manager/context.go`, `internal/manager/context_v1.md`, `internal/manager/launcher.go` and their tests. **Keep** `internal/developing/plugins.go` + `plugin_assets` and `internal/manager/plugins.go` + `plugin_assets` (Task 8 touches them; `atm init` imports both packages for plugin install only).
- Modify: `internal/cli/developing_test.go` + `internal/cli/manager_test.go` → merge surviving coverage into a new `internal/cli/session_test.go`
- Modify: `internal/cli/testdata/golden/*` launch goldens
- Modify: `tests/arch/imports_test.go` (`TestOnlyEventlogImportsEventsourceLib` dir list: add `"internal/session"`)

**Interfaces:**
- Consumes: `session.LauncherFor/OllamaLauncher/RenderContext/PromptMessage`, `skills.Persona`, `skills.ParsePersona`, `core.Service.PersonaDoc/GetPersonality`.
- Produces (CLI surface):
  - `atm [--persona <name>] [--project <CODE>] [--mode <m>] [--capability <c>] [--agent <a>] [-- extra args...]` — persona empty/`admin` → TUI; else agent session.
  - hidden `atm session-context --persona <name> [--project <CODE>] [--actor <a>] [--mode <m>] [--capability <c>]`
  - hidden `atm manage-context [--project] [--actor]` → alias for `session-context --persona manager` (installed thin-pointer plugins call it).
- Cache key: `session-<persona>[-<mode>][-<capability>].md` under `projects/<CODE>/cache/`, or `<store>/cache/` when no project.
- Env: `ATM_ROLE` (= `"developing"` for launch:hook personas — back-compat with installed session-start hooks — else the persona name), `ATM_PROJECT`, `ATM_ACTOR`, `ATM_RUN_ID`, `ATM_TIMESTAMP`, `ATM_CONTEXT_FILE`, `ATM_AGENT`, `ATM_PERSONA`, plus `ATM_MODE`/`ATM_CAPABILITY` when set. `ATM_MANAGER_ACTION`/`ATM_MANAGER_CAPABILITY` are gone.

- [ ] **Step 1: Write the failing tests**

`internal/cli/session_test.go` — use the existing test harness style from `developing_test.go`/`manager_test.go` (read them first; they stub `runChildFn`, `lookPathFn`, and use a temp store). Core cases:

```go
package cli

import (
	"strings"
	"testing"
)

// helper: run atm with args against a seeded test state; reuse the file's
// existing harness (see developing_test.go's runCLI-style helper) — port it
// here as runSession(t, args...) returning stdout, stderr, captured argv/env.

func TestPersonaFlagDefaultsToTUI(t *testing.T) {
	// `atm` with no --persona launches the TUI as admin@tui:unset.
	// Port of the existing root TUI test; assert runTUI stub was invoked.
}

func TestPersonaAdminLaunchesTUI(t *testing.T) {
	// `atm --persona admin` also goes to the TUI.
}

func TestPersonaDeveloperLaunchesHookStyle(t *testing.T) {
	// `atm --persona developer --project ATM --agent claude`
	// captured argv == ["claude"] (bare — launch: hook), env contains
	// ATM_ROLE=developing, ATM_PERSONA=developer, ATM_CONTEXT_FILE=...session-developer.md
}

func TestPersonaManagerDefaultsModeAutopilot(t *testing.T) {
	// `atm --persona manager --project ATM --agent claude`
	// argv[0] == "claude", argv[1] contains "Read the session instructions";
	// env: ATM_ROLE=manager, ATM_MODE=autopilot; context path ends
	// session-manager-autopilot.md; rendered file contains "## Mode: autopilot".
}

func TestPersonaManagerModeBrief(t *testing.T) {
	// --mode brief renders "## Mode: brief" and not the autopilot section.
}

func TestModeValidation(t *testing.T) {
	// --persona manager --mode nope → usage error listing brief, autopilot, ask.
	// --persona developer --mode brief → usage error: developer declares no modes.
}

func TestCapabilityScopeValidation(t *testing.T) {
	// --persona manager --capability nope → unknown capability error listing registered.
	// (port validateManagerAction's test cases from manager_test.go)
}

func TestProjectRequiredUnlessOptional(t *testing.T) {
	// --persona manager (no --project) → usage error.
	// --persona concierge (no --project) succeeds once Task 7 adds the file;
	// until then use a custom persona with project_optional via store? Built-ins
	// only — so mark this concierge case as added in Task 7's steps.
}

func TestUnknownPersonaFails(t *testing.T) {
	// --persona ghost → not-found error.
}

func TestDevAndManageAreGone(t *testing.T) {
	// executeArgs with ["dev", "--project", "X"] exits non-zero with unknown
	// command; same for ["manage", ...].
}

func TestSessionContextRendersManager(t *testing.T) {
	// `atm session-context --persona manager --project ATM` prints a prompt
	// containing "## Persona: manager"; `atm manage-context --project ATM`
	// prints the same (alias).
}
```

Write these as real tests against the harness (each body follows the existing harness idiom — see `developer-codex-launch` golden usage in `developing_test.go`; where a golden file is compared, create new goldens `session-developer-claude-launch.json`, `session-manager-autopilot-launch.json` mirroring the old shapes with the new env/argv/cache-path values, and delete the old `developer-codex-launch.json` / `manage-codex-*` goldens).

- [ ] **Step 2: Run to verify failures**

Run: `go test ./internal/cli/ -run 'Session|Persona|Mode' 2>&1 | head -5`
Expected: FAIL / build errors.

- [ ] **Step 3: Implement `internal/cli/session.go`**

```go
package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"atm/internal/agent"
	"atm/internal/core"
	"atm/internal/session"
	"atm/skills"

	"github.com/spf13/cobra"
)

type sessionOpts struct {
	Persona     string
	Project     string
	Mode        string
	Capability  string
	Agent       string
	Integration string
	DefaultArgs []string
	ExtraArgs   []string
}

// sessionLauncherFor maps a catalog entry to the unified session launcher.
func sessionLauncherFor(e agent.Entry) (session.Launcher, bool) {
	if e.Launcher == "ollama" {
		return session.OllamaLauncher{Integration: e.Integration}, true
	}
	return session.LauncherFor(e.Launcher)
}

// resolvePersonaSpec resolves a persona name to its spec: built-ins from the
// skills package, customs parsed from their stored markdown document.
func resolvePersonaSpec(s core.Service, name string) (skills.PersonaSpec, error) {
	if spec, ok := skills.Persona(name); ok {
		return spec, nil
	}
	doc, err := s.PersonaDoc(name)
	if err != nil {
		return skills.PersonaSpec{}, err
	}
	spec, err := skills.ParsePersona(name, []byte(doc))
	if err != nil {
		return skills.PersonaSpec{}, fmt.Errorf("%w: stored persona %q: %v", ErrUsage, name, err)
	}
	return spec, nil
}

// resolveMode validates --mode against the persona's declared modes and
// applies the default.
func resolveMode(spec skills.PersonaSpec, flag string) (string, error) {
	if flag == "" {
		return spec.DefaultMode, nil
	}
	if len(spec.Modes) == 0 {
		return "", fmt.Errorf("%w: persona %q declares no modes", ErrUsage, spec.Name)
	}
	if _, ok := spec.Mode(flag); !ok {
		return "", fmt.Errorf("%w: unknown mode %q for persona %q (available: %s)",
			ErrUsage, flag, spec.Name, strings.Join(spec.ModeNames(), ", "))
	}
	return flag, nil
}

// validateCapabilityScope checks the optional --capability against the full
// registry (typo → registered list) then the enabled set.
func validateCapabilityScope(capabilityName string, enabled, registered []string) error {
	if capabilityName == "" {
		return nil
	}
	if !slices.Contains(registered, capabilityName) {
		return fmt.Errorf("%w: unknown capability %q (registered: %s)", ErrUsage, capabilityName, strings.Join(registered, ", "))
	}
	if !slices.Contains(enabled, capabilityName) {
		return fmt.Errorf("%w: capability %q is not enabled for project; run `atm project capability add --project <CODE> --name %s` first", ErrUsage, capabilityName, capabilityName)
	}
	return nil
}

func (st *cliState) launchSession(opts sessionOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	cfg, err := s.GetAgentsConfig()
	if err != nil {
		return err
	}
	e, defArgs, err := resolveEntry(opts.Agent, cfg)
	if err != nil {
		return err
	}
	l, ok := sessionLauncherFor(e)
	if !ok {
		return fmt.Errorf("%w: unknown agent %q", ErrUsage, e.Launcher)
	}
	opts.Integration = e.Integration
	opts.DefaultArgs = defArgs

	spec, err := resolvePersonaSpec(s, opts.Persona)
	if err != nil {
		return err
	}
	mode, err := resolveMode(spec, opts.Mode)
	if err != nil {
		return err
	}
	if err := validateCapabilityScope(opts.Capability, st.registry.Names(), st.fullRegistry.Names()); err != nil {
		return err
	}

	var code, projName string
	if opts.Project == "" {
		if !spec.ProjectOptional {
			return fmt.Errorf("%w: --project is required for persona %q", ErrUsage, spec.Name)
		}
	} else {
		p, err := ensureProjectForLaunch(s, opts.Project)
		if err != nil {
			return err
		}
		code, projName = p.Code, p.Name
	}

	personality, err := s.GetPersonality(spec.Name)
	if err != nil {
		return err
	}
	actor := spec.Name + "@" + l.Name() + ":unset"

	if _, err := st.lookPath("atm"); err != nil {
		return fmt.Errorf("%w: atm is not on PATH; the session prompt assumes `atm` resolves on PATH. Either add the directory containing the `atm` binary to PATH, or invoke atm from a shell where it resolves.", ErrUsage)
	}

	now := time.Now().UTC()
	runCode := code
	if runCode == "" {
		runCode = "atm"
	}
	runID := newRunID(runCode)
	timestamp := core.RFC3339UTC(now)
	contextPath := contextCachePath(s.StorePath(), code, spec.Name, mode, opts.Capability)

	rendered := session.RenderContext(session.ContextData{
		Code: code, Name: projName, Actor: actor,
		Spec: spec, Personality: personality, Mode: mode, Capability: opts.Capability,
	})
	if err := writeContextIfDiff(contextPath, []byte(rendered)); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	var base []string
	role := spec.Name
	if spec.Launch == "hook" {
		base = l.BuildArgv()
		role = "developing" // back-compat: installed session-start hooks gate on this
	} else {
		base = l.BuildArgvPrompt(contextPath)
	}
	envArgs := agentEnvArgs(e.Launcher, e.Integration)
	argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
	envValues := sessionEnvValues(code, actor, runID, contextPath, l.Name(), spec.Name, role, mode, opts.Capability, timestamp)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, spec.Name, code, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}

	exitCode, runErr := st.runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, spec.Name, code, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

func sessionEnvValues(project, actor, runID, contextPath, agentName, persona, role, mode, capability, timestamp string) map[string]string {
	m := map[string]string{
		"ATM_ROLE":         role,
		"ATM_PROJECT":      project,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_TIMESTAMP":    timestamp,
		"ATM_CONTEXT_FILE": contextPath,
		"ATM_AGENT":        agentName,
		"ATM_PERSONA":      persona,
	}
	if mode != "" {
		m["ATM_MODE"] = mode
	}
	if capability != "" {
		m["ATM_CAPABILITY"] = capability
	}
	return m
}

// newSessionContextCmd renders a persona's session prompt to stdout (hidden
// plumbing; thin-pointer subagent plugins call it at dispatch).
func newSessionContextCmd(st *cliState) *cobra.Command {
	var opts struct {
		Persona    string
		Project    string
		Actor      string
		Mode       string
		Capability string
	}
	cmd := &cobra.Command{
		Use:    "session-context",
		Short:  "Print a persona's rendered session prompt to stdout",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			spec, err := resolvePersonaSpec(s, opts.Persona)
			if err != nil {
				return err
			}
			mode, err := resolveMode(spec, opts.Mode)
			if err != nil {
				return err
			}
			personality, err := s.GetPersonality(spec.Name)
			if err != nil {
				return err
			}
			data := session.ContextData{Code: opts.Project, Actor: opts.Actor,
				Spec: spec, Personality: personality, Mode: mode, Capability: opts.Capability}
			if opts.Project != "" {
				data.Name = opts.Project // fallback when the project isn't in the store
				if p, err := s.GetProject(opts.Project); err == nil {
					data.Name = p.Name
				}
			}
			rendered := session.RenderContext(data)
			return st.emit(st.stdout(), map[string]any{"context": rendered}, func() {
				fmt.Fprint(st.stdout(), rendered)
			})
		},
	}
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name")
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project code (optional; when absent, placeholders are left for env-driven use)")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id (optional)")
	cmd.Flags().StringVar(&opts.Mode, "mode", "", "persona mode (default: the persona's default_mode)")
	cmd.Flags().StringVar(&opts.Capability, "capability", "", "scope to one capability")
	_ = cmd.MarkFlagRequired("persona")
	return cmd
}

// newManageContextCmd is the legacy alias installed thin-pointer manager
// plugins call; it renders the manager persona's prompt.
func newManageContextCmd(st *cliState) *cobra.Command {
	var opts struct {
		Project string
		Actor   string
	}
	cmd := &cobra.Command{
		Use:    "manage-context",
		Short:  "Print the ATM manager system prompt to stdout (alias of session-context --persona manager)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sub := newSessionContextCmd(st)
			argv := []string{"--persona", "manager"}
			if opts.Project != "" {
				argv = append(argv, "--project", opts.Project)
			}
			if opts.Actor != "" {
				argv = append(argv, "--actor", opts.Actor)
			}
			sub.SetArgs(argv)
			return sub.Execute()
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project code")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id")
	return cmd
}
```

(Delete the old `newManageContextCmd` in `manager.go` along with the launch code; if `sub.Execute()` on a constructed subcommand clashes with cobra flag parsing, call the render logic via a shared helper `renderSessionContext(st, persona, project, actor, mode, capability string) error` used by both commands instead — prefer the helper if the alias test fails.)

- [ ] **Step 4: Rewire root and shared launcher plumbing**

In `internal/cli/root.go` `newRootCmdWithState`:

```go
	var opts sessionOpts
	root := &cobra.Command{
		Use:           "atm",
		Short:         "Agent Tasks Management",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Persona == "" || opts.Persona == "admin" {
				if len(args) > 0 {
					return fmt.Errorf("%w: unknown command %q", ErrUsage, args[0])
				}
				return st.launchTUI()
			}
			opts.ExtraArgs = args
			return st.launchSession(opts)
		},
		PersistentPreRunE: ... // unchanged
	}
	root.Flags().StringVar(&opts.Persona, "persona", "", "launch as a persona: admin (default, opens the TUI) or an agent persona like developer, manager, concierge (see `atm persona list`)")
	root.Flags().StringVar(&opts.Project, "project", "", "ATM project the session works on")
	root.Flags().StringVar(&opts.Mode, "mode", "", "persona mode (validated against the persona's declared modes)")
	root.Flags().StringVar(&opts.Capability, "capability", "", "scope the session to one enabled capability")
	root.Flags().StringVar(&opts.Agent, "agent", "", "override the selected agent for this launch (see `atm agents list`)")
```

Mount changes: remove `root.AddCommand(newDevCmd(st))` and `root.AddCommand(newManageCmd(st))`; add `root.AddCommand(newSessionContextCmd(st))`; keep `root.AddCommand(newManageContextCmd(st))`.

In `internal/cli/launcher_shared.go`, replace `contextCachePath`/`cacheKey`:

```go
// contextCachePath returns the stable on-disk path for a rendered session
// prompt keyed on (persona, mode, capability). Repeated launches of the same
// tuple reuse the same file. With no project (project-optional personas), the
// file lives in the store-level cache dir.
func contextCachePath(storePath, code, persona, mode, capability string) string {
	key := cacheKey(persona, mode, capability)
	if code == "" {
		return filepath.Join(storePath, "cache", key+".md")
	}
	return filepath.Join(storePath, "projects", code, "cache", key+".md")
}

// cacheKey builds the filename stem: session-<persona>[-<mode>][-<capability>].
func cacheKey(persona, mode, capability string) string {
	parts := []string{"session", persona}
	if mode != "" {
		parts = append(parts, mode)
	}
	if capability != "" {
		parts = append(parts, capability)
	}
	for i, p := range parts {
		parts[i] = sanitizeCacheSegment(p)
	}
	return strings.Join(parts, "-")
}
```

In `internal/cli/agent_resolve.go`: delete `devLauncherFor`/`manageLauncherFor` and the `atm/internal/developing` + `atm/internal/manager` imports (`sessionLauncherFor` lives in session.go).

Delete `internal/cli/developing.go`; strip `internal/cli/manager.go` down to the plugin commands (or move them to `internal/cli/plugins_manager.go` and delete `manager.go`); delete the now-unused launch/context files in `internal/developing` and `internal/manager` (keep `plugins.go`, `plugins_test.go`, `plugin_install_test.go`, `plugin_assets/`). Update `internal/cli/launcher_shared_test.go` for the new `cacheKey` signature. Update `tests/arch/imports_test.go` dir list: remove `"internal/developing"`? No — the package still exists (plugins); keep it, and add `"internal/session"` and `"skills"` if not already added in Task 4.

- [ ] **Step 5: Run the full suite; fix fallout**

Run: `go build ./... && go test ./... 2>&1 | tail -30`
Expected: PASS after: updating `context_test.go`/`determinism_test.go`/golden files that referenced `dev`/`manage` paths, env names, or cache keys (grep `internal/cli/testdata` for `ATM_MANAGER_ACTION`, `dev-developer`, `manage-manager`); updating `harness_test.go` if it names removed commands. Regenerate goldens per the harness's documented update mechanism (check `harness_test.go` for an `-update`/env flag; otherwise hand-edit the JSON to the new argv/env/cache-path values asserted in Step 1's tests).

- [ ] **Step 6: Commit**

```bash
git add -u internal/cli internal/developing internal/manager tests/arch/imports_test.go
git add internal/cli/session.go internal/cli/session_test.go internal/cli/testdata/golden/
git commit -m "feat(ATM-0772ea): unified atm --persona launcher; remove atm dev / atm manage"
```

---

### Task 7: Concierge persona

**Files:**
- Create: `skills/persona/concierge.md`
- Test: extend `skills/skills_test.go` and `internal/cli/session_test.go`

**Interfaces:** consumes the Task 6 launch path (`project_optional`); produces the fourth built-in persona.

- [ ] **Step 1: Write the failing tests**

Append to `skills/skills_test.go`:

```go
func TestConciergePersonaShape(t *testing.T) {
	c, ok := Persona("concierge")
	if !ok {
		t.Fatal("concierge not found")
	}
	if !c.ProjectOptional {
		t.Fatal("concierge must be launchable without --project")
	}
	if c.Launch != "prompt" {
		t.Fatalf("concierge launches prompt-style, got %q", c.Launch)
	}
	if c.Personality == "" {
		t.Fatal("concierge ships a default personality (the customization showcase)")
	}
	for _, jargon := range []string{"label substrate"} {
		if strings.Contains(c.Body, jargon) {
			t.Fatalf("concierge speaks the user's language; found %q", jargon)
		}
	}
}
```

In `internal/cli/session_test.go`, complete `TestProjectRequiredUnlessOptional`: `atm --persona concierge --agent claude` (no `--project`) launches, argv carries the prompt message, and the context file lands under `<store>/cache/session-concierge.md`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./skills/ -run Concierge`
Expected: FAIL — concierge not found.

- [ ] **Step 3: Write `skills/persona/concierge.md`**

```markdown
---
name: concierge
description: Friendly onboarding guide — helps you set up ATM for your projects, no jargon required.
project_optional: true
---
# Persona: concierge

You are the ATM concierge: a warm, patient guide whose job is to get a person
comfortably set up with ATM — their environment, their first project, and the
way their work will be organized. You are the first face of ATM many people
meet. Your success is measured by their understanding and comfort, not by how
much you configure.

## Orient silently first

Before engaging, learn the terrain without narrating it:

1. `atm conventions` — what ATM is and how projects, tasks, labels, and
   actors fit together.
2. `atm capability list` (and `--project <CODE>` once a project exists) —
   which capabilities exist and which are enabled.
3. `atm capability <name> guide` for each — read its description, `Semantics`,
   and `Converge` sections so you know what each capability organizes and
   what a well-set-up project looks like.

This is your background knowledge. Do not recite it to the user.

## Speak the user's language

The cardinal rule: translate, never teach jargon.

- Ask about their world: what they are building, who works on it, how they
  track work today (issues? a notebook? nothing?), and what frustrates them
  about it.
- Map their answers to ATM concepts internally. When you propose something,
  express it in their words: "we can track which stage each piece of work is
  in" — never "enable workflow_ai for the stage namespace".
- Introduce an ATM term only after the user has seen the thing it names, and
  always alongside the plain description they already understand.
- One question at a time. Short messages. No walls of text.

## The onboarding flow

1. **Listen.** Learn their setup: projects, repositories, team, current
   tracking habits. Reflect it back briefly so they can correct you.
2. **Recommend.** Propose a concrete starting shape: a project (name and
   short code), which capabilities fit how they already work, and what views
   they will look at day-to-day. Justify each recommendation by the problem
   it solves for them, in their terms.
3. **Set up on confirmation.** Only after they agree: create the project,
   enable the chosen capabilities, and seed their vocabulary and boards. Show
   them what was created, briefly.
4. **Hand off.** Leave them the smallest set of things to remember: `atm` to
   look around, `atm --persona developer --project <CODE>` to work with an
   agent, `atm --persona manager --project <CODE>` for upkeep. Offer to stay
   and answer questions.

If no project exists yet, creating one is the expected outcome of your
session — never assume one exists.

## Personality

Warm, encouraging, and unhurried. Prefer plain words over precise words when
they conflict, and concrete examples over abstract descriptions. Celebrate
small progress. Never make the user feel behind.
```

- [ ] **Step 4: Run tests**

Run: `go test ./skills/ ./internal/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add skills/persona/concierge.md skills/skills_test.go internal/cli/session_test.go
git commit -m "feat(ATM-0772ea): concierge persona — plain-language onboarding, project-optional launch"
```

---### Task 8: Persona CLI — positional show, personality subcommand; plugin assets

**Files:**
- Modify: `internal/cli/persona.go`
- Modify: `internal/developing/plugin_assets/{claude,codex}/hooks/session-start` (gate on `ATM_CONTEXT_FILE` instead of `ATM_ROLE`), `internal/developing/plugin_assets/*/skills/atm-developing/SKILL.md` (wording: drop `ATM_ROLE=developing` mention), `internal/developing/plugin_assets/opencode/atm-developing.js` (same gate change — read it first), `internal/manager/plugin_assets/*/atm-manager.md` (call `session-context --persona manager`, keep `manage-context` mention as fallback)
- Test: `internal/cli/persona_test.go`, `internal/developing/plugins_test.go`, `internal/manager/plugins_test.go` (update assertions to new asset text)

**Interfaces (CLI surface):**
- `atm persona list` — built-ins + customs with descriptions (existing, now includes built-ins virtually).
- `atm persona show <name>` — positional arg (keep `--name` working); text output gains modes + personality status lines.
- `atm persona personality <name>` — print effective personality; `--set <text>` / `--file <path>` to customize; `--clear` to reset.
- `atm persona create/edit/remove` — customs only (store enforces; error messages point built-in editing at `personality`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/persona_test.go` (follow the file's existing harness idiom):

```go
func TestPersonaShowPositional(t *testing.T) {
	// `atm persona show manager` (no --name) prints name, description, and a
	// "modes: brief, autopilot, ask (default autopilot)" line.
}

func TestPersonaPersonalityRoundTrip(t *testing.T) {
	// `atm persona personality manager --set "Dry wit." --actor admin` then
	// `atm persona personality manager` prints "Dry wit."; `--clear` then
	// prints the built-in default (or empty for personas without one).
}

func TestPersonaEditBuiltinRefusedWithHint(t *testing.T) {
	// `atm persona edit --name manager --prompt x --actor admin` fails and the
	// error mentions `atm persona personality`.
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cli/ -run PersonaShow 2>&1 | head -3`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `internal/cli/persona.go`:
- `newPersonaShowCmd`: `Use: "show [name]"`, `Args: cobra.MaximumNArgs(1)`; resolve `name` from the positional arg when given, else `--name`; error when neither. Extend text output:

```go
			p, err := s.GetPersona(name)
			if err != nil {
				return err
			}
			spec, specErr := resolvePersonaSpec(s, name)
			overlay, _ := s.GetPersonality(name)
			return st.emit(st.stdout(), map[string]any{"persona": p, "modes": spec.ModeNames(), "default_mode": spec.DefaultMode, "personality_custom": overlay != ""}, func() {
				fmt.Fprintf(st.stdout(), "%s\t%s\n", p.Name, p.Description)
				if specErr == nil && len(spec.Modes) > 0 {
					fmt.Fprintf(st.stdout(), "modes: %s (default %s)\n", strings.Join(spec.ModeNames(), ", "), spec.DefaultMode)
				}
				if overlay != "" {
					fmt.Fprintln(st.stdout(), "personality: customized")
				}
				fmt.Fprintf(st.stdout(), "\n%s\n", p.Prompt)
			})
```

- Add `newPersonaPersonalityCmd(st)` mounted from `newPersonaCmd`:

```go
func newPersonaPersonalityCmd(st *cliState) *cobra.Command {
	var setText, file string
	var clear bool
	cmd := &cobra.Command{
		Use:   "personality <name>",
		Short: "Show or customize a persona's personality section",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			s, err := st.openStore()
			if err != nil {
				return err
			}
			mutating := clear || setText != "" || file != ""
			if setText != "" && file != "" {
				return fmt.Errorf("%w: --set and --file are mutually exclusive", ErrUsage)
			}
			if !mutating {
				spec, err := resolvePersonaSpec(s, name)
				if err != nil {
					return err
				}
				overlay, err := s.GetPersonality(name)
				if err != nil {
					return err
				}
				effective := spec.Personality
				custom := overlay != ""
				if custom {
					effective = overlay
				}
				return st.emit(st.stdout(), map[string]any{"persona": name, "personality": effective, "custom": custom}, func() {
					fmt.Fprintln(st.stdout(), effective)
				})
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			if clear {
				if err := s.ClearPersonality(name); err != nil {
					return err
				}
				return st.emit(st.stdout(), map[string]any{"persona": name, "cleared": true}, func() {
					fmt.Fprintf(st.stdout(), "cleared personality for %s\n", name)
				})
			}
			text := setText
			if file != "" {
				b, err := os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("read --file: %w", err)
				}
				text = string(b)
			}
			if err := s.SetPersonality(name, text, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": name, "personality": strings.TrimSpace(text)}, func() {
				fmt.Fprintf(st.stdout(), "set personality for %s\n", name)
			})
		},
	}
	cmd.Flags().StringVar(&setText, "set", "", "set the personality text")
	cmd.Flags().StringVar(&file, "file", "", "read the personality text from a file")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the customization (revert to the persona's default)")
	return cmd
}
```

- Plugin assets. `internal/developing/plugin_assets/claude/hooks/session-start` gate becomes:

```bash
if [ -z "${ATM_CONTEXT_FILE:-}" ] || [ -z "${ATM_PROJECT:-}" ]; then
  printf '{}\n'
  exit 0
fi
```

Mirror in the codex hook and the opencode `atm-developing.js` (read each first; change only the gate condition). In `SKILL.md` descriptions, replace `(ATM_ROLE=developing)` with `(ATM_CONTEXT_FILE set)`. In `internal/manager/plugin_assets/*/atm-manager.md`, replace the dispatch line:

```bash
"$ATM" session-context --persona manager --project "$ATM_PROJECT" --actor "$ATM_ACTOR" \
  || "$ATM" manage-context --project "$ATM_PROJECT" --actor "$ATM_ACTOR"
```

(the fallback keeps a new plugin working against an older binary).

- [ ] **Step 4: Run**

Run: `go test ./internal/cli/ ./internal/developing/ ./internal/manager/`
Expected: PASS after updating plugin-asset assertion tests to the new text.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/persona.go internal/cli/persona_test.go internal/developing/plugin_assets internal/manager/plugin_assets internal/developing/plugins_test.go internal/manager/plugins_test.go
git commit -m "feat(ATM-0772ea): persona show/personality commands; plugin assets gate on ATM_CONTEXT_FILE"
```

---

### Task 9: Docs, sweep, and changelog

**Files:**
- Modify: `README.md`, `AGENTS.md`, `CHANGELOG.md`, `internal/cli/conventions.go` (if it names Brief/Autopilot or dev/manage), `docs/architecture/logical-components.md` (package table: add `skills`, `internal/session`; drop `internal/seed`, note `developing`/`manager` reduced to plugin hosting)

- [ ] **Step 1: Sweep for stale references**

Run each and fix every production/doc hit (test fixtures were handled in their tasks):

```bash
grep -rn "atm dev\b\|atm manage\b" README.md AGENTS.md docs/ internal/ --include="*.md" --include="*.go" | grep -v superpowers/
grep -rn "Brief\|Autopilot" internal/ README.md AGENTS.md --include="*.md" --include="*.go" | grep -v plans/ | grep -v specs/
grep -rn "ATM_MANAGER_ACTION\|manage-context\|internal/seed\|internal/developing\|internal/manager" README.md AGENTS.md docs/architecture/
```

Replacement rules: `atm dev --project X` → `atm --persona developer --project X`; `atm manage --project X --action brief` → `atm --persona manager --project X --mode brief`; "Brief + Autopilot + reference" phrasing → "semantics, actions, and converged state"; persona docs mention `atm persona show <name>` and `atm persona personality <name>`.

- [ ] **Step 2: CHANGELOG entry**

Add under a new Unreleased heading (match the file's existing style):

```markdown
- **Breaking:** `atm dev` and `atm manage` are removed. Launch sessions with
  `atm --persona developer|manager|concierge --project <CODE>`; manager
  brief/autopilot are now `--mode brief|autopilot|ask` (default autopilot).
- Built-in personas (developer, manager, admin, new concierge) now ship inside
  the binary from the top-level `skills/` folder and are no longer seeded into
  the store; leftover seeded files are ignored. Customize a built-in with
  `atm persona personality <name> --set/--file/--clear`.
- Custom personas persist as markdown (`personas/<name>.md`); legacy JSON
  personas migrate automatically on first read.
- Capability guides are restructured: `## Semantics` / `## Actions` /
  `## Converge` replace the manager-specific `## Brief` / `## Autopilot`.
- New `concierge` persona: plain-language onboarding; launchable without
  `--project`.
- Env: `ATM_MODE`/`ATM_CAPABILITY` replace `ATM_MANAGER_ACTION`/
  `ATM_MANAGER_CAPABILITY`. `ATM_ROLE` still reads `developing` for developer
  sessions (installed session-start hooks keep working); reinstall plugins
  (`atm init`) to pick up the new context-file-gated hooks.
```

- [ ] **Step 3: Full suite + build**

Run: `go build ./... && go test ./...`
Expected: PASS, no skips introduced.

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md CHANGELOG.md docs/architecture/logical-components.md internal/cli/conventions.go
git commit -m "docs(ATM-0772ea): document skills/ folder, unified --persona launcher, concierge"
```

---

### Task 10: Ledger close-out

- [ ] **Step 1:** Record completion on ATM-0772ea: a progress comment naming the branch, the commits, and the doc/spec/plan locations; record the plan locator (`atm capability workflow_ai plan --task ATM-0772ea --kind file --ref docs/superpowers/plans/2026-07-22-persona-capability-skills.md`) and advance the stage per the workflow_ai ladder (`plan`, then `ready` once reviewed). Use the atm-manager subagent or the CLI directly.
- [ ] **Step 2:** Final review pass: `git log --oneline main..HEAD`, re-read the spec's Goals section, and verify each maps to landed code. Report any gaps as new ATM tasks rather than silent scope cuts.
