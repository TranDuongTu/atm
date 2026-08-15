---
name: checklist
description: Checklists — named, per-persona standing operating procedures; the project's configured process that personas read at session start and follow.
brief: List your checklists (`atm checklist list`) and read the ones matching this session's purpose before starting work. They are this project's operating procedure for your persona; they override nothing but must not be skipped.
labels: [checklist:*]
boards: [checklists]
---
# checklist capability — agent guide

A checklist is a named, per-persona standing operating procedure: the concrete, user-configured process that generic persona prompts and capability guides cannot know. Where a persona prompt describes the shape of your session and a capability guide describes one capability's data model, a checklist is this project's own process for a specific job — who does it, in what order, against which surfaces. Personas read the checklists matching their session's purpose at session start and follow them.

Checklists commonly name channels by handle. Resolve a handle with `atm channel show --project <CODE> --name <handle>` and do the I/O yourself with your own tools; a checklist names the surface, it does not reach it for you.

A checklist is a briefing, not a tracker. It has no done or pending state — compliance shows in the ledger, in the work you journal while following the steps. Read it, follow it, and leave the ledger evidence; do not look for a checklist status.

Any persona uses checklists the same way: run `atm checklist list --project <CODE>` and pick the record whose purpose matches this session's work, then `atm checklist show --project <CODE> --persona <persona> --name <name>` and follow the steps. A persona with no checklists is a normal state, not an error — work without one and propose adding one if a repeatable process is missing.

The concierge authors and repairs checklists by interview: ask which external surfaces the work touches, what must happen before, during, and after, and what must never be inlined into a step — credentials, secrets, and session-specific trivia stay in the work itself, never in a checklist. Prefer a few named checklists with crisp purposes over many overlapping ones: a persona should find the right record by purpose at a glance.

## Semantics

A checklist record is a task labelled `checklist:<persona>`; its title is `<persona>/<name>`, its description a fixed one-line pointer, and its payload `{v, persona, name, purpose, steps}`. (persona, name) is unique per project. Checklist records are tasks only as plumbing: manage them exclusively through `atm checklist`, never through raw task verbs. The `checklists` board exists so queries can see them. Per-persona value labels (`<code>:checklist:<persona>`) are created lazily on first add — personas are user-creatable, so the store seeds them on demand, not ahead of time.

## Actions

- `atm checklist list --project <CODE> [--persona <persona>] [--all]` — names and purposes; the selection surface.
- `atm checklist show --project <CODE> --persona <persona> --name <name>` — the full ordered steps.
- `atm checklist add --project <CODE> --persona <persona> --name <name> --purpose "..." --step "..." [--step "..."]` — author a checklist record.
- `atm checklist edit --project <CODE> --persona <persona> --name <name> [--purpose "..."] [--step "..."]` — replace the purpose and/or the steps.
- `atm checklist remove --project <CODE> --persona <persona> --name <name>` — remove the record.
- `atm capability checklist seed --project <CODE>` — ensure vocabulary, board, and the shipped starter checklists. Existing names are never overwritten, so your edits survive re-seeding; a seed you deleted returns on the next run — delete it again or edit it into usefulness.

## Converge

Every dispatched persona has the checklists its work needs; steps are concrete and imperative, and every named channel handle resolves via `atm channel show`. The checklist seed has been run for the project. Checklists for personas that no longer exist are orphans — surface them with `atm checklist list --all` and propose removing or reassigning them. To author or repair a checklist, interview the user and read the steps back before writing. The shipped `concierge/empty-project` checklist walks the full setup for a fresh project.
