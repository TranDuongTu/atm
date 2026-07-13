# The label substrate and capability commands

This document describes what ATM is made of, and how it is extended. It is the architectural reference behind `atm conventions` (which is the agent-facing summary) and behind every design spec in `docs/superpowers/specs/`.

Two ideas carry the whole system:

1. **The label substrate** — there is one entity (the task) and one uniform mechanism (the label). Everything else is interpretation.
2. **Capability commands** — new behaviour is added as a CLI command that owns a slice of that substrate, not as a new entity or a new enforced field.

## Part 1: the label substrate

### One entity, one mechanism

A project holds tasks. A task carries free-form text (title, description), an append-mostly comment thread, and a set of labels. That is the entire data model.

There is **no status field, no claim entity, no review queue, no links table, no state machine, no priority column, no assignee**. Status, type, priority, ownership, and relationships are all expressed as labels and interpreted by whoever reads them. Workflow lives outside the store — in agent prompts and human habits. The store only keeps the substrate legible.

This is a deliberate refusal. Every field a task store adds is a decision imposed on every future workflow that uses it. A `status` enum forces every project to agree on what the states are. A `links` table forces every project to agree on what a relationship is. ATM declines to decide, and the cost of that refusal is paid once, in the reader: **an agent must read the labels to understand a task.** The benefit is paid forever: a project can invent whatever vocabulary it needs, and nothing in the store objects.

### Three kinds of label

| Kind | Example | Asserted or computed |
|---|---|---|
| **stored** | `ATM:status:open` | Tasks assert it directly. |
| **namespace** | `ATM:status:*` | Emergent — any label sharing the prefix. Describable, queryable, never assigned. |
| **board** | `ATM:next-sprint` | **Computed.** Carries an expression over other labels. Membership is derived, never asserted. Cannot be assigned to a task. |

Boards are the substrate's one piece of real machinery. A board is a label with an expression (`status:open AND sprint:next`), evaluated at query time, and boards may reference other boards. `NOT status:*` means "carries no status label" — that is, untriaged.

The consequence matters more than it first appears: **a saved query is not a second-class citizen.** Anywhere a label may be used — `--label`, faceting, another board's expression — a board may be used. This is why "return only current knowledge" needs no code (see `context-current` in the context-map design): it is a board, and boards are already a first-class part of the query surface.

### The description is the intention record

Every label carries a description. That description is not documentation *about* the vocabulary; it *is* the vocabulary's definition, stored where every agent can read it. An agent's first act in an unfamiliar project is `atm label list --project <CODE>` — reading the descriptions is how it learns what the project means by a word before using it.

A label without a description is therefore a defect, and it surfaces as one: the Boards pane shows a warning. Not an error, not a rejection — a visible signal that *an agent introduced this but did not explain why*, for a human to reconcile.

### Advisory, always

**Nothing in the store validates or special-cases any namespace.** `status:` is not privileged. `context:` is not privileged. `atm conventions` says so in its own last line, and it means it.

This is the invariant that everything else must respect. It has a specific practical consequence: **degrade, never reject.** When a tool encounters data it cannot interpret — a context task with no provenance, a label with no description, a namespace it has never seen — it reports the gap and carries on. It does not fail, and it does not "fix" the data on the human's behalf.

## Part 2: capability commands

### The problem with growing a substrate

Sooner or later a subsystem needs structure the substrate does not have. The context map needs to record *what a pointer was derived from and when*, so drift can be detected. The obvious moves are both wrong:

- **Add a typed field** (`sources` on the task) → the store now special-cases context tasks. Every consumer of the stable store API pays for a field only one namespace uses, and the "no privileged namespaces" invariant dies.
- **Let agents hand-write the convention** (a JSON blob in the description, label names typed into prompts) → the format becomes an implicit contract between whoever writes it and whoever reads it. Prompts hardcode label strings. The strings drift from the store. This repo has already hit that bug (ATM-0114: tests asserting prompt fragments that a template rewrite had made stale).

### The pattern

A **capability command** is a CLI subsystem that owns a slice of the label substrate. It has three obligations:

**1. It ensures its own vocabulary.** Before using a label or a board, it creates it — idempotently, with a description. It never assumes `atm label seed` ran. It never assumes the project's labels have a particular shape. It works in a project whose human curated the vocabulary differently, and in one created five minutes ago.

*Consequence: capabilities are self-bootstrapping. There is no seeding dependency, and no "this feature only works in a properly-configured project."*

**2. It exposes intent-level verbs.** The caller says what it means — `supersede this pointer, because its subject died` — not which labels to apply. Label names, expressions, and formats never appear in a prompt, a skill, or an agent's reasoning.

*Consequence: prompts stop hardcoding vocabulary, so vocabulary can change without touching prompts. The manager reasons about drift; it has never heard of `knowledge:superseded`.*

**3. It owns its data formats.** Anything machine-written and machine-read is written and read exclusively by that capability. Nothing else parses it.

*Consequence: the format is private and can change freely. It also means the format belongs in a comment or a description — somewhere the substrate already stores free text — rather than in a new field.*

### What a capability may not do

- **It may not enforce.** The store still validates nothing. A capability's labels can be hand-assigned, renamed, or deleted by a human, and nothing breaks. The capability reports what it can prove and stays quiet about the rest. **A capability is a paved road, not a fence.**
- **It may not judge.** Read-only reporters (`check`) report; deciders (agents, humans) decide. A tool that automatically marked knowledge stale because a file changed would be wrong most of the time — a helper function added to a package does not invalidate "this package is the stable in-process API." Machines say *where to look*; models say *what it means*.
- **It may not grow integrations.** ATM speaks no third-party API and holds no credentials. Where a source cannot be witnessed locally (a Jira ticket, a Notion page), the capability records what it can and reports *age* instead of *change* — a weaker signal, but an honest one. The agent, which already has tools, does the verifying.

### Reader/writer split

Every capability separates the two:

| Role | Mutates | Example |
|---|---|---|
| **Recorder** | Yes | `atm context add / stamp / retarget / supersede` |
| **Reporter** | **Never** | `atm context check` |
| **Decider** | Via recorders | The manager prompt |

The reporter's purity is testable and should be tested: the store is byte-identical before and after it runs.

### First instance: `atm context`

The context map is the pattern's first realisation. It owns `context:*` (pointer kinds), `knowledge:superseded` (lifecycle), `comment:provenance` (its private format), and the `context-current` board (`context:* AND NOT knowledge:superseded`). It exposes five verbs, of which exactly one is read-only. It witnesses git and local files provably, URLs opportunistically, and external systems by age alone.

See `docs/superpowers/specs/2026-07-13-context-map-refresh-design.md` for the full design.

### Adding the next one

When a subsystem needs structure the substrate lacks, ask in order:

1. **Can a board express it?** A grouping, a filter, a "current view" — these are saved queries, not features. Write the expression; write no code.
2. **Can existing labels express it?** Read every label's description first. The vocabulary is usually larger than it looks.
3. **Does it need a private machine-readable format?** Then it is a capability. Put the format in a comment kind that only that capability parses, ensure the vocabulary on first use, and expose verbs — not labels — to the callers.
4. **Does it truly need a new store field?** Almost certainly not. If the answer is genuinely yes, it belongs to the substrate itself, applies to *every* task, and needs its own design discussion — because it is a decision imposed on every project ATM will ever hold.
