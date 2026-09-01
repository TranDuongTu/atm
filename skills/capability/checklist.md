---
name: checklist
description: Checklists — free-standing, name-keyed standing operating procedures; the project's configured process that personas read at session start and follow.
brief: List your checklists (`atm checklist list`) and read the ones matching this session's purpose before starting work. They are this project's operating procedure for your persona; they override nothing but must not be skipped.
labels: [checklist, checklist:*]
boards: [checklists]
---
# checklist capability — agent guide

A checklist is a free-standing, name-keyed standing operating procedure: the concrete, user-configured process that generic persona prompts and capability guides cannot know. Where a persona prompt describes the shape of your session and a capability guide describes one capability's data model, a checklist is this project's own process for a specific job — what happens, in what order, against which surfaces. A checklist's `suits` list names the personas it is a default fit for — a hint, not ownership: any persona can run any checklist. Personas read the checklists matching their session's purpose at session start and follow them.

Checklists commonly name channels by handle. Resolve a handle with `atm channel show --project <CODE> --name <handle>` and do the I/O yourself with your own tools; a checklist names the surface, it does not reach it for you.

A checklist is a briefing, not a tracker. It has no done or pending state — compliance shows in the ledger, in the work you journal while following the steps. Read it, follow it, and leave the ledger evidence; do not look for a checklist status.

Any persona uses checklists the same way: run `atm checklist list --project <CODE>` and pick the record whose purpose matches this session's work, then `atm checklist show --project <CODE> --name <name>` and follow the steps. A persona with no suited checklists is a normal state, not an error — work without one and propose adding one if a repeatable process is missing.

Author and repair checklists by interview: ask which external surfaces the work touches, what must happen before, during, and after, and what must never be inlined into a step — credentials, secrets, and session-specific trivia stay in the work itself, never in a checklist. Prefer a few named checklists with crisp purposes over many overlapping ones: the right record should be findable by purpose at a glance.

## Semantics

A checklist record is a task labelled `checklist` (bare); its title is the name, its description a fixed one-line pointer, and its payload `{v: 2, name, purpose, steps, suits, requires, origin}`. The name is unique per project. Steps are a recursive tree — every node is `{text, children}`, rendered everywhere with nested numbering (`1.`, `1.1`, `1.2.1`). `suits` lists the personas the checklist is a default fit for; `requires` declares the capabilities and channel handles it needs (unmet requirements warn, never block); `origin` is reset provenance — `user`, `shipped:atm`, or `shipped:<capability>`. Checklist records are tasks only as plumbing: manage them exclusively through `atm checklist`, never through raw task verbs. The `checklists` board sees both generations of record so queries can find them.

Legacy v1 records (label `checklist:<persona>`, title `<persona>/<name>`, flat string steps) remain fully readable — the persona reads as `suits: [persona]`, origin as `user` — and move to the v2 shape and the bare label the first time they are set. Reads never rewrite anything. Two legacy records could share a name under different personas; by-name verbs then fail naming both task IDs, and `remove --task <ID>` disambiguates.

## Actions

- `atm checklist list --project <CODE> [--persona <persona>] [--all]` — names, suits, and purposes; the selection surface. `--persona` (or `ATM_PERSONA`) filters to records whose suits name the persona; `--all` lists everything.
- `atm checklist show --project <CODE> --name <name>` — purpose, suits, requires, origin, and the full step tree.
- `atm checklist add --project <CODE> --name <name> --purpose "..." [--step "..."]... [--steps-file <f|->] [--suits <persona>]... [--requires-capability <cap>]... [--requires-channel <handle>]...` — author a record. `--step` adds flat top-level steps; `--steps-file` reads a markdown nested list (indentation is depth). Origin is always `user` from the CLI.
- `atm checklist set --project <CODE> --name <name> --file <f|->` — replace the record wholesale from a checklist document (the seed format: frontmatter `name`/`purpose`/`suits`/`requires_capabilities`/`requires_channels`, a nested-list step body). Checklists are authored elsewhere and imported; there is no per-field edit. The document's `name` must match `--name`, and its `origin` is ignored — provenance stays with the record.
- `atm checklist remove --project <CODE> --name <name> [--task <ID>]` — remove the record; `--task` disambiguates a legacy same-name collision.
- `atm capability checklist seed --project <CODE>` — ensure vocabulary, board, and the shipped starter checklists. Existing names are never overwritten, so your edits survive re-seeding; a seed you deleted returns on the next run — delete it again or edit it into usefulness.

## Converge

Every dispatched persona has the checklists its work needs; steps are concrete and imperative, and every named channel handle resolves via `atm channel show`. The checklist seed has been run for the project. Checklists whose `suits` name personas that no longer exist are stale hints — surface them with `atm checklist list --all` and re-suit or clear them. To author or repair a checklist, interview the user and read the steps back before writing. The shipped `empty-project` checklist walks the full setup for a fresh project.
