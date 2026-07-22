# Persona–capability decoupling: `skills/` prompt folder, unified launcher, concierge persona

- **Task:** ATM-0772ea
- **Date:** 2026-07-22
- **Status:** Approved design, pre-implementation

## Problem

Persona and capability prompting are coupled in three ways:

1. Capability guides carry manager-persona procedure: every guide must contain
   `## Brief` and `## Autopilot` sections (mandated by the `Capability.Guide()`
   contract in `internal/capability/capability.go`), and
   `internal/manager/context.go` hard-codes those section names in its action
   block. Capabilities cannot be written without knowing about the manager.
2. Launchers are per-persona commands (`atm dev`, `atm manage`) with divergent
   flag surfaces, templates (`internal/developing/context_v1.md`,
   `internal/manager/context_v1.md`), and plugin assets.
3. Prompt sources are scattered: persona prompts as Go string literals in
   `internal/seed/persona.go`, capability guides as per-package `go:embed`
   files. There is no single place to read or evolve the prompt surface, and
   no enforced format.

Separately, first-time onboarding has no owner: the manager's `brief` action
partially covers setup but speaks ATM jargon and assumes an existing project.

## Goals

- Capabilities are leaves: they define semantics (how data is organized),
  actions (verbs), and convergence (what healthy data looks like) — with no
  persona-specific content.
- Personas are functions: capability-agnostic prompts that discover
  capabilities at runtime and operate over them, optionally in declared modes.
- The CLI is the invocation: one launcher, `atm --persona`, that validates
  modes/capabilities and assembles the session prompt.
- A new **concierge** persona owns first-time onboarding in the user's own
  language.
- All built-in prompts live in a top-level `skills/` folder with a
  parser-enforced format.

## Decisions of record

- New persona name: **concierge** (over buddy/coach/guide — role-noun family,
  no collision with the `guide` CLI vocabulary).
- `atm dev` and `atm manage` are **removed outright** — no deprecated aliases.
- Mode selection flag is **`--mode`** (not `--action`, which now collides with
  the capability-format term "actions").
- `skills/` is **embedded at build time** (single-binary shipping preserved);
  runtime-loaded prompt directories were rejected for version-skew risk.

## Design

### 1. `skills/` layout and file contracts

```
skills/
  skills.go              # package skills: //go:embed + parser + typed accessors
  persona/
    developer.md
    manager.md
    admin.md
    concierge.md
  capability/
    workflow.md
    workflow_ai.md
    contextmap.md
```

**Persona file** — YAML frontmatter + markdown body:

```markdown
---
name: manager
description: Keeps the ledger accurate and legible…   # shown by CLI, later TUI
modes:                                                # optional
  brief: Interview the human to set up capability data.
  autopilot: Autonomously converge the ledger.
  ask: Read-only standby.
---
# Persona: manager
<core prompt — capability-agnostic; discovers capabilities at runtime
 via `atm capability list` + `atm capability <name> guide`>

## Mode: brief
…full instructions for brief mode…

## Mode: autopilot
…

## Personality        # optional — the customizable section
```

Parser rules (personas):

- `name` required and must match the filename stem.
- `description` required.
- Every frontmatter mode has a matching `## Mode: <name>` body section, and
  vice versa.
- `## Personality` optional.

At render time only the *selected* mode's section is included in the session
prompt.

**Capability file** — same shape, different contract:

```markdown
---
name: workflow_ai
description: <1–2 lines an agent skims to judge relevance before reading fully>
labels: [stage:*, wfai:*]
boards: [brainstormed-tasks, …]
---
# Capability: workflow_ai
## Semantics     # what it means, how its data is organized
## Actions       # exposed verbs (CLI subcommands) and when to use them
## Converge      # persona-agnostic: what a healthy/converged data state looks like
```

Parser rules (capabilities): frontmatter `name`, `description`, `labels`,
`boards` required; body must contain `## Semantics`, `## Actions`, and
`## Converge`. `## Brief` / `## Autopilot` are gone; the `Capability.Guide()`
interface contract drops that mandate.

**Enforcement** lives in the `skills` package parser with unit tests.
`atm persona create/edit` and the capability registry refuse files that fail
parsing, with errors naming the missing field or section.

**Store handling:** built-ins are no longer seeded into the user store
(`SeedPersonas` is deleted; built-ins resolve from the binary). The store
keeps only user-created personas — same markdown format, one `.md` per
persona, validated by the same parser — plus per-persona *personality
overlays* for customizing built-ins. Existing store JSON personas are
converted lazily on first load.

### 2. CLI surface and unified launcher

```
atm                                      # unchanged: TUI as admin@tui
atm --persona admin                      # explicit form of the same thing → TUI
atm --persona developer --project ATM    # agent session (replaces `atm dev`)
atm --persona manager --project ATM --mode autopilot [--capability workflow_ai]
atm --persona concierge                  # onboarding; --project optional
```

- `--persona` resolves built-ins (from `skills/`) then store customs. Admin
  (or omitted) → TUI; anything else → agent session.
- `--mode` validated against the persona's declared modes. A persona with no
  modes rejects the flag; an invalid value errors listing the declared modes.
- `--capability` remains an optional scope, validated against the project's
  enabled capabilities and exposed to the prompt as a variable; any persona
  may use or ignore it.
- `--agent` (opencode/codex/claude/ollama) unchanged.
- Concierge is the one persona where `--project` is optional, since creating
  the project may be the session's outcome.

**Persona query/customization** (existing singular `atm persona` noun kept,
consistent with `atm capability`):

```
atm persona list                       # built-ins + customs, with descriptions
atm persona show <name>                # name, description, modes, personality status
atm persona <name> personality         # print effective personality
atm persona <name> personality --edit  # customize (overlay for built-ins)
atm persona <name> personality --clear
atm persona create/edit/remove         # custom personas only, format-validated
```

**Launcher internals:** `internal/developing` and `internal/manager` merge
into one `internal/session` package — a single `context_v1.md` template with
`<PERSONA_BLOCK>`, `<MODE_BLOCK>` (empty when no mode), and the orientation
block. Cache key: `session-<persona>[-<mode>][-<capability>].md`. Env:
`ATM_PERSONA`, `ATM_MODE`, `ATM_CAPABILITY` plus existing
`ATM_PROJECT`/`ATM_ACTOR`/`ATM_CONTEXT_FILE`. `ATM_ROLE` is replaced by the
persona name; plugin session-start hooks gate on `ATM_CONTEXT_FILE` being set
rather than on specific roles.

**Re-render:** hidden `atm manage-context` becomes generic hidden
`atm session-context --persona <name> --project <X> [--mode …]`, keeping the
manager subagent plugin (and future persona plugins) a thin pointer.

### 3. Content migration

Each capability guide's `## Brief`/`## Autopilot` content splits:

- Capability-specific knowledge (setup questions that matter, what a
  maintained ledger looks like, e.g. workflow_ai's stage-ladder hygiene)
  is rewritten persona-agnostically into that capability's `## Converge`
  (target state + convergence rules) and `## Actions` (verbs to get there).
- Procedure (interview vs. act autonomously) moves into the manager persona's
  `## Mode: brief` / `## Mode: autopilot`, written generically: for each
  enabled capability (or the `--capability` scope), read its guide; in brief,
  interview the human and configure toward its Converge state; in autopilot,
  apply its Actions to converge without asking. The manager prompt names no
  capability.

`internal/manager/context.go`'s action block (hard-coded "Brief"/"Autopilot"
section names) is deleted; the generic `<MODE_BLOCK>` replaces it.

### 4. Concierge persona

- **Description:** "Your setup buddy — helps you get ATM working for your
  projects, no jargon required."
- **Core prompt flow:**
  1. Silently orient: read `atm conventions`, enumerate capabilities, skim
     each guide's description/Semantics/Converge before speaking.
  2. Engage the user about *their* world — what they build, how they track
     work today, team habits — in plain language, never assuming ATM
     vocabulary.
  3. Map what it heard to ATM patterns and recommend concretely (create the
     project, enable fitting capabilities, seed labels/boards), explaining
     each in the user's own terms; execute on confirmation.
  4. Hand off: point the user at the next persona (`developer` day-to-day,
     `manager` upkeep).
- **Rule:** translate, don't teach jargon — "we can track which stage each
  piece of work is in," not "enable workflow_ai for the stage: substrate."
- No modes initially. Ships with a warm default `## Personality` section as
  the showcase for customization.
- Scope shift: concierge owns first-time environment/project onboarding;
  manager `brief` narrows to capability-data setup/adjustment on existing
  projects.

## Testing

- Parser unit tests: missing fields, mode/section mismatch, duplicate names,
  personality overlay application.
- Registry↔skills consistency: every registered capability has a matching
  skills file; all persona files parse.
- CLI tests: `--mode` validation, persona dispatch (admin→TUI vs session),
  removed-command absence.
- Launcher tests: argv/env assembly, cache-key shape (existing dev/manage
  launcher tests ported to `internal/session`).
- `tests/arch` rules and docs (README, AGENTS.md, guide vocabulary sections)
  updated in the same change.

## Implementation stages (one branch)

1. `skills` package + prompt files moved verbatim.
2. Parser + format rewrite of guides and personas to the new contracts.
3. Unified launcher/CLI; remove `atm dev` / `atm manage`.
4. Concierge persona.
5. Docs + ledger updates.
