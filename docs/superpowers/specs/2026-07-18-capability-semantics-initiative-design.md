# Capability-driven semantics — initiative design

Date: 2026-07-18
Status: approved design (initiative roadmap + phase 1 detail)

## Problem

ATM's core is a neutral, business-agnostic label substrate: task records plus
described labels, with all meaning carried by interpretation. Agents working
with ATM learn that meaning from two hand-maintained prose surfaces:

- `atm conventions` (`internal/cli/conventions.go`) — a single static document
  that hand-writes the semantics of every capability (workflow verbs, the
  context map) alongside the substrate core.
- The manager prompt (`internal/manager/context_v1.md`) — a fixed template
  whose "Your Roles" section hardcodes three actions (`curate`, `recall`,
  `mapping`) and embeds contextmap's entire mapping procedure.

Both surfaces are prose *about* capabilities living *outside* the capability —
the same drift class as label names hardcoded in prompts (the ATM-0114 bug).
And capability registration is compile-time and all-or-nothing: every project
gets every capability, and no project can choose otherwise.

## Goal

Agents stop relying on the bare-label structure plus one static conventions
document, and instead consult **capabilities** — each of which encapsulates
its own semantics and operational mode. The narrative:

1. A user creates a project choosing the capabilities it actually needs.
2. `atm conventions` presents the substrate core plus an enumeration of the
   project's enabled capabilities; agents consult each capability's guide for
   its semantics (progressive disclosure, the same shape as agent skills).
3. The atm-manager's scope is composed from an irreducible substrate core plus
   the actions the enabled capabilities contribute.

## Decisions (settled in brainstorming, 2026-07-18)

| Question | Decision |
|---|---|
| Manager core vs capability actions | **Substrate core + capability actions.** Curate and recall remain the manager's irreducible duty (the substrate always exists); enabled capabilities contribute additional actions (contextmap → `mapping`). |
| Enablement semantics | **Hard gate.** A capability a project has not enabled has its commands **not mounted** for that project. The gate is on the tooling surface only — the store still validates nothing, and hand-assigned labels remain legal. |
| Where enablement lives | **Per-project, in the store.** Enable/disable are project-level events in the v2 event log — audited, ledger-visible, travels with the store. |
| Consultation mechanism | **Enumerate + per-capability guide verb.** Conventions shrinks to substrate core + enabled-capability enumeration; each capability owns a `guide` subcommand with its full semantics. |
| Phasing | **Three phases**, each its own spec → plan → implementation: describe → enable → manage. |

## Doctrine changes (architecture)

Recorded in `docs/architecture/label-substrate-and-capabilities.md`:

1. **A fourth capability obligation: it explains itself.** A capability
   carries its own agent-facing semantics — what its vocabulary means, how to
   operate its verbs, and what its manager duty is — retrievable at runtime
   via its `guide` verb. Prose about a capability outside the capability is a
   defect.
2. **Enablement is a fence on the tooling surface, not on the substrate.**
   "A capability is a paved road, not a fence" still holds for the store: no
   validation, no privileged namespaces, hand-editing stays legal. What
   becomes project-scoped is *which paved roads are built*: a project that
   did not enable a capability does not get its commands, its vocabulary
   seeding, its boards, or its manager action. "Advisory, always" continues
   to describe the store; capability availability becomes a per-project
   composition decision recorded in the ledger.

## Phase 1 — capabilities describe themselves (this phase's scope)

No gating, no store changes. Pure semantic refactor: move capability prose
into the capability packages and compose the agent-facing surfaces from the
registry.

### Interface

`internal/capability.Capability` grows two methods:

- `Summary() string` — one line, used wherever capabilities are enumerated
  (conventions, help, later the manager action list).
- `Guide() string` — the full agent-facing semantics: what the capability
  means, its vocabulary's intent, how to use its verbs, its operating
  procedure, and a **manager section** describing the capability's manager
  duty. Guide text lives as an embedded `.md` file in the capability package
  (same mechanism as `context_v1.md`).

The registry mounts a uniform `guide` subcommand on every capability's
command tree (`atm workflow guide`, `atm context guide`). The subcommand is
added by the registry in `Commands(env)`, not by each capability, so its
shape (name, help text, plain-text output to stdout) is identical across
capabilities and cannot be forgotten.

### Conventions becomes core + enumeration

`atm conventions` renders:

1. The substrate core: what ATM is, labels/boards, tasks, comments, search,
   actor identity, first-contact sequence, code-of-conduct, human sequence,
   store-format notes. (Unchanged in meaning; trimmed of capability prose.)
2. A **Capabilities** section enumerating registered capabilities:
   name, `Summary()`, and the consult instruction
   (`atm <cap> guide`). Rendered from the registry, not hand-written.

The hand-written workflow and context-map sections ("Workflow verbs", "The
context map", the workflow/context steps inside the first-contact sequence)
are deleted from the static text; first-contact instead says: read the
Capabilities section and consult each enabled capability's guide before
acting. In phase 1 "enabled" = "registered" (all built-ins); phase 2 makes
the enumeration project-aware.

### Manager prompt de-hardcoded

`context_v1.md` keeps Curate and Recall as written (irreducible core). The
Mapping role entry shrinks to: contextmap's manager duty — consult
`atm context guide` for the procedure. The three-step mapping procedure
moves verbatim into contextmap's guide (manager section). CLI flags stay
`--curate/--recall/--mapping` in phase 1.

### Tests

- Conventions output contains each registered capability's name and summary,
  and the consult instruction — asserted via the registry, not string
  literals per capability.
- No capability-owned procedure or vocabulary prose remains in
  `internal/cli/conventions.go` or `context_v1.md` (arch tests may pin the
  absence of known-moved fragments, e.g. `context stamp` in the manager
  template).
- `guide` subcommand: exists for every registered capability, prints the
  embedded text, is read-only (store byte-identical before/after).
- Guide content: workflow's guide names its four verbs and the
  exactly-one-status invariant; contextmap's guide contains the mapping
  procedure and the manager section.

## Phase 2 — per-project enablement, hard gate (outline; own spec later)

- New project-level events in the v2 log: capability enabled / disabled.
  The project record exposes the enabled set.
- UX: `atm project create --capabilities …`; `atm project capability
  add|remove|list`; `atm init` selects the default set for the projects it
  creates; minimal TUI surface (view + toggle in the Projects pane).
- Hard gate: the composition root resolves the target project **before**
  building the cobra tree (prescan `--project` / `ATM_PROJECT`; task-ID
  prefixes imply a project — the spec pins the resolution order) and mounts
  only enabled capabilities' commands. No project context → mount all
  (store-wide operation). `EnsureVocabulary`, `DefaultBoard`, the
  conventions enumeration, and TUI per-project features filter by the
  enabled set.
- Migration: projects predating the feature read as **all built-in
  capabilities enabled** — inferred at read time, no migration event (the
  persona-inference precedent).

## Phase 3 — capability-driven manager scope (outline; own spec later)

- `Capability` grows `ManagerActions() []ActionSpec` (name + summary);
  contextmap contributes `mapping`.
- `atm manage` keeps `--curate`/`--recall`; capability actions are selected
  with `--action <name>`, validated against the project's enabled set
  (`--mapping` becomes a deprecated alias).
- The rendered manager context composes: persona + principles + core roles +
  the enabled capabilities' action list; the selected action's block points
  at the capability's guide. `context_v1.md`'s fixed Roles section is
  replaced by this composition.

## What this buys

Adding capability N becomes one package implementing the interface. Its
vocabulary, verbs, semantics, guide, and manager duty arrive by
registration — no edits to conventions prose, the manager template, or CLI
flag lists. A project's `atm conventions` and manager scope reflect exactly
what that project chose.

## Out of scope

- Third-party / plugin-loaded capabilities (registration stays compile-time;
  only *enablement* becomes per-project).
- Any store-level validation of capability vocabularies.
- Changes to the developing-session context beyond what conventions
  composition already delivers.
