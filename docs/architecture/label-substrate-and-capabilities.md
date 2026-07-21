# The label substrate and capability commands

This document describes what ATM is made of, and how it is extended. It is the architectural reference behind `atm conventions` (which is the agent-facing summary) and behind every design spec in `docs/superpowers/specs/`. It states both the architecture as built and the committed extensibility direction (the capability extension points initiative, ATM-4dd440); anything not yet implemented is marked *(planned)*.

Four ideas carry the whole system:

1. **The label substrate** — there is one entity (the task) and one shared mechanism (the label). The substrate records truth and filters it; everything else is interpretation.
2. **Capability commands** — new behaviour is added as a CLI command that owns a slice of that substrate, not as a new entity or a new enforced field.
3. **Capability semantics** — a capability explains itself. Agents do not learn a project from one hand-written master document; they read the substrate core plus an enumeration of the project's enabled capabilities, and consult each capability's own guide for its semantics and operating mode.
4. **Capability independence** — capabilities are independent views over the same tasks. They meet only in the substrate: shared meaning lives in labels, private state is private absolutely, and no contract connects one capability to another.

## Part 1: the label substrate

### One entity, two functions

A project holds tasks. A task carries free-form text (title, description), a set of labels, an append-mostly comment thread, and *(planned)* a capability-keyed metadata column. The substrate does exactly two things with all of it: **recording** — tasks and labels state the truth — and **filtering** — boards select over labels. No more. Everything that looks like workflow, knowledge management, or process is a capability's interpretation of recorded labels, not a substrate feature.

There is **no status field, no claim entity, no review queue, no links table, no state machine, no priority column, no assignee**. Status, type, priority, ownership, and relationships are all expressed as labels and interpreted by whoever reads them. Workflow lives in capabilities (`internal/capability/workflow`), not in the store; the store only keeps the substrate legible. A capability is a paved road, not a fence — a project can replace it.

This is a deliberate refusal. Every field a task store adds is a decision imposed on every future workflow that uses it. A `status` enum forces every project to agree on what the states are. A `links` table forces every project to agree on what a relationship is. ATM declines to decide, and the cost of that refusal is paid once, in the reader: **an agent must read the labels to understand a task.** The benefit is paid forever: a project can invent whatever vocabulary it needs, and nothing in the store objects.

Comments are today a core entity with their own event vocabulary; the long-term direction is that the thread leaves the core and becomes a capability's territory, leaving the shared data model at text + labels + opaque capability state. Design nothing new against comments as a machine surface (see obligation 3) — the thread is for humans and agents writing prose, not for formats.

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

### The metadata column *(planned)*

The substrate's answer to capability-private state is one uniform mechanism, the same move that labels made for shared state: a per-task map keyed by capability name, holding one opaque payload per (task, capability). The store holds it, event-logs it (`task.meta.set`), syncs it, and never interprets it. Only the owning capability's verbs read or write its own key — enforced at the API layer, not by the store validating anything. Payloads are replace-only until a real consumer proves the need for more, and they hold pointers to big content, never the content itself.

Opaque is not invisible. `atm task show` lists which capabilities hold metadata on a task — presence, size, last writer, timestamp — without interpreting content, and a disabled or uninstalled capability's payload renders as present-but-uninterpretable rather than being hidden or dropped. Metadata dies with its task; disabling a capability retains its payloads, because enablement is a fence on the tooling surface, never on data.

The litmus test that keeps the two mechanisms honest: **if a board or any other capability could ever need to see it, it is a label; if only the owning capability needs it, it is metadata.** Boards never query into payloads — anything filterable must be projected into labels by the capability's verbs. Format and versioning inside a payload are the owning capability's problem, with one convention: embed a format version, and read your own old formats (degrade-never-reject applied to yourself).

### Advisory, always

**Nothing in the store validates or special-cases any namespace.** `status:` is not privileged. `context:` is not privileged. `atm conventions` says so in its own last line, and it means it. The same holds for the metadata column: the store never inspects a payload, and a raw store accessor for recovery is the moral equivalent of hand-assigning a label.

This is the invariant that everything else must respect. It has a specific practical consequence: **degrade, never reject.** When a tool encounters data it cannot interpret — a context task with no provenance, a label with no description, a namespace it has never seen, a payload whose owner is not installed — it reports the gap and carries on. It does not fail, and it does not "fix" the data on the human's behalf.

## Part 2: capability commands

### The problem with growing a substrate

Sooner or later a subsystem needs structure the substrate does not have. The context map needs to record *what a pointer was derived from and when*, so drift can be detected. The obvious moves are both wrong:

- **Add a typed field** (`sources` on the task) → the store now special-cases context tasks. Every consumer of the stable store API pays for a field only one namespace uses, and the "no privileged namespaces" invariant dies.
- **Let agents hand-write the convention** (a JSON blob in the description, label names typed into prompts) → the format becomes an implicit contract between whoever writes it and whoever reads it. Prompts hardcode label strings. The strings drift from the store. This repo has already hit that bug (ATM-0114: tests asserting prompt fragments that a template rewrite had made stale).

A third move looked right and proved wrong at larger scale: **put the machine format in a comment kind that only the capability parses.** It respects encapsulation (contextmap's `comment:provenance` is fully private to its package), but it plants one capability's machine state in another surface's territory — a label spanning two concerns — and it makes the comment thread a machine surface when the thread exists for prose. The metadata column *(planned)* replaces this pattern; `comment:provenance` is its named migration target.

### The pattern

A **capability command** is a CLI subsystem that owns a slice of the label substrate. It has four obligations:

**1. It ensures its own vocabulary via a single seam.** `EnsureVocabulary(svc, code, actor)` is the capability's one self-setup call: idempotently, with a description, it seeds every label and board the capability owns and returns the **boards** it owns (labels with `Expr`). This seam is invoked at project create, `atm project capability add`, TUI project select, and the TUI Boards [S] key; recorders also call it defensively so verbs work in an unseeded project, and `atm capability workflow seed` exposes the same seam as a manual trigger — there is one seeding path per capability, not zero seeding commands. It never assumes the project's labels have a particular shape; it works in a project whose human curated the vocabulary differently, and in one created five minutes ago. A human's curated description is never overwritten.

*Consequence: capabilities are self-bootstrapping. There is one seeding path per capability, no seeding dependency, and no "this feature only works in a properly-configured project." A new board added in a new capability version appears on the next `EnsureVocabulary` run — no migration step.*

**2. It exposes intent-level verbs.** The caller says what it means — `supersede this pointer, because its subject died` — not which labels to apply. Label names, expressions, and formats never appear in a prompt, a skill, or an agent's reasoning.

*Consequence: prompts stop hardcoding vocabulary, so vocabulary can change without touching prompts. The manager reasons about drift; it has never heard of `knowledge:superseded`.*

**3. It owns its private state, in its own metadata key.** Anything machine-written and machine-read belongs in the capability's slot of the task metadata column *(planned)*: written and read exclusively by that capability, invisible to boards, addressable by no other capability. Until the column ships, `comment:provenance` stands as the legacy of the earlier comment-format pattern, encapsulated but slated for migration; no new capability may adopt that pattern. Free text — descriptions, comments — is for humans and agents, never a parse target.

*Consequence: the format is private and can change freely, and the substrate's shared surfaces stay prose-only. Whatever must be filterable is projected into labels; whatever is working state stays in the payload.*

**4. It explains itself and mounts under its `Name()`.** A capability carries its own agent-facing semantics — what its vocabulary means, how its verbs are used, its operating procedure, and its manager duty — retrievable at runtime as a `guide` verb (`atm capability <Name()> guide`) and a one-line summary for enumeration. Its cobra command tree mounts under `atm capability <Name()>` (e.g. `atm capability workflow start`, `atm capability contextmap add`); a disabled capability's tree is unmounted (cobra "unknown command"), a hard gate on the tooling surface only, engaged when the invocation resolves a project from `--project`/`--task`/`ATM_PROJECT` — an unscoped invocation mounts the full registry, because degrading open beats guessing. `atm conventions` is a minimal substrate primer that points at `atm capability list` and `atm capability <name> guide` for discovery; it does not enumerate capabilities, and it does not restate them. Prose about a capability living outside the capability is a defect — the same drift class as label names hardcoded in prompts.

*Consequence: a capability's semantics and command surface have exactly one source. Adding a capability is one package implementing the interface; conventions, the manager scope, and agent behaviour follow by registration, with no prose sites to keep in sync.*

### Capability independence

Capabilities are parallel interpretations of the same tasks, and the doctrine has three clauses:

- **The substrate is the only meeting point.** A capability reads tasks and labels; it never reads another capability's metadata payload, never parses another capability's format, and never invokes another capability's verbs. If two capabilities need to agree on something, that something is a label, defined by its description like any other shared word.
- **No label spans two capabilities.** A label owned by one capability but written into another's territory is the anti-pattern; `comment:provenance` — contextmap's machine format planted in the comment system's namespace — is the living instance, and retiring it is on the initiative roadmap. Each namespace has exactly one owner, or it is project-curated and owned by no capability.
- **Coexisting views are allowed to disagree.** Two lifecycle capabilities enabled in one project — `workflow`'s `status:*` and a planned `workflow_ai`'s `stage:*` — operate on disjoint namespaces with no interplay contract: no verb composition, no mapping, no cross-checking. A task that is `status:done` and `stage:brainstormed` is two answers to two different questions, not incoherence to repair. Each capability maintains only its own invariant.

*Consequence: enabling a capability can never break another, third-party capabilities compose without coordination, and the failure mode of a bad capability is contained to its own vocabulary and its own payload.*

### What a capability may not do

- **It may not enforce.** The store still validates nothing. A capability's labels can be hand-assigned, renamed, or deleted by a human, and nothing breaks. The capability reports what it can prove and stays quiet about the rest. **A capability is a paved road, not a fence.** Its verbs may refuse an ill-formed transition on their own road — that is what intent-level verbs are — but the refusal gates the verb, never the store. Enablement (below) does not weaken this: it decides *which paved roads get built* for a project — never what the store accepts.
- **It may not judge.** Read-only reporters (`check`) report; deciders (agents, humans) decide. A tool that automatically marked knowledge stale because a file changed would be wrong most of the time — a helper function added to a package does not invalidate "this package is the stable in-process API." Machines say *where to look*; models say *what it means*.
- **It may not reach into a sibling.** Another capability's namespace, payload, and verbs are off limits (see capability independence) — coupling two capabilities through any of them recreates, one level up, exactly the implicit contracts the substrate refused.
- **It may not grow integrations.** ATM speaks no third-party API and holds no credentials. Where a source cannot be witnessed locally (a Jira ticket, a Notion page), the capability records what it can and reports *age* instead of *change* — a weaker signal, but an honest one. The agent, which already has tools, does the verifying.

### Reader/writer split

Every capability separates the two:

| Role | Mutates | Example |
|---|---|---|
| **Recorder** | Yes | `atm capability contextmap add / stamp / retarget / supersede` |
| **Reporter** | **Never** | `atm capability contextmap check` |
| **Decider** | Via recorders | The manager prompt |

The reporter's purity is testable and should be tested: the store is byte-identical before and after it runs.

### Enablement: which paved roads get built

Capabilities are registered at compile time — one site, `cmd/atm/main.go` — but **chosen per project**. A project's enabled set is a project-level fact in its event log — selected at project create, editable later, audited like every other mutation. A capability a project has not enabled is absent from that project's tooling surface: its commands are not mounted (`atm capability <Name()>` returns "unknown command" in a project-scoped invocation), its vocabulary is not seeded, its boards are not ensured, and its manager action scope is not offered.

This is a fence on the **tooling surface**, not on the substrate. "Advisory, always" continues to describe the store: no validation, no privileged namespaces, and a human hand-assigning a disabled capability's labels breaks nothing. Disabling a capability likewise retains its labels and its metadata payloads. What a project chooses is which interpretations it is offered, and that choice is itself part of the ledger.

### The composed semantic surface

Because capabilities explain themselves (obligation 4) and are chosen per project (enablement), the agent-facing surfaces are **composed, not written**:

- `atm conventions` = a minimal substrate primer (what ATM is, the substrate commands, advisory-only rule) + a one-line pointer at `atm capability list` and `atm capability <name> guide` for discovery. It does **not** enumerate capabilities; it teaches the substrate, and the capabilities teach themselves.
- The manager scope = an irreducible substrate core (curate — keep the ledger legible; recall — grounded synthesis) + three semantic-agnostic actions — `brief`, `autopilot`, `ask` — scoped by an optional `--capability <name>`. Each capability's guide carries `## Brief` and `## Autopilot` sections; the manager prompt does not inline guide text — it instructs the agent to run `atm capability <name> guide` and follow the relevant section at runtime. `ManagerActions()` is not a capability concern; the procedure lives in the guide, not in the prompt.

### Views live with the owner

The UI picks the default board, not the capability. `DefaultBoard` is not a capability concern: the capability declares its boards via `EnsureVocabulary`'s return, and the store is the source of truth for what boards exist at render time. The TUI's `selectDefault` selects `<CODE>:all-tasks` if present in the ring, else the first row — so a project that disabled `workflow` (and thus has no `all-tasks`) falls back to whatever boards the enabled capabilities seeded.

*(planned)* The same ownership rule extends to rendering capability state: a capability interface method (`Annotate(task) → short cell`) feeds a contextual column in the TUI tasks table — workflow_ai showing planned-vs-needs-clarification, contextmap showing staleness — with the substrate handing over the payload and the owner deciding what it means. Today the tasks table's columns are hardcoded and no such seam exists; building it is part of the metadata column work.

An agent's consultation sequence mirrors the composed surface: read the substrate primer, run `atm capability list`, then consult each enabled capability's `atm capability <name> guide` before operating in its territory — progressive disclosure, the same shape as agent skills.

See `docs/superpowers/specs/2026-07-18-capability-namespace-manager-actions-v2-design.md` for the v2 doctrine (capability namespace, manager action model) and `2026-07-18-capability-semantics-initiative-design.md` for the original initiative roadmap (describe → enable → manage).

### First instance: `atm capability contextmap`

The context map is the pattern's first realisation. It owns `context:*` (pointer kinds), `knowledge:superseded` (lifecycle), `comment:provenance` (its private format — the legacy comment-format pattern, migration to its metadata key pending), and the `context-current` board (`context:* AND NOT knowledge:superseded`). It exposes five verbs (`add`, `stamp`, `retarget`, `supersede`, `check`), mounted under `atm capability contextmap`, of which exactly one (`check`) is read-only. It witnesses git and local files provably, URLs opportunistically, and external systems by age alone.

See `docs/superpowers/specs/2026-07-13-context-map-refresh-design.md` for the full design.

### Second instance: `atm capability workflow`

The workflow capability owns the paved road for status and priority. Its `EnsureVocabulary` seeds the `status:*` namespace (four values: `open`, `in-progress`, `blocked`, `done`) and the `priority:*` namespace (three values: `high`, `medium`, `low`) — priority is a planning concern, and workflow is the planning/status capability, so it owns the vocabulary alongside status. It also seeds four boards (`backlog`, `open-tasks`, `in-progress-tasks`, `all-tasks`). Its mutating verbs (`start`/`open`/`block`/`complete`) swap the `status:*` label and maintain the exactly-one-status invariant; they do not touch priority (assign by hand). `comment:*` is not seeded by any capability — agents invent comment kinds on demand.

### Third instance *(planned)*: `atm capability workflow_ai`

A lifecycle capability tailored to the brainstorm → plan → implement AI cycle: a `stage:*` namespace (`brainstormed`, `clarified`, `planned`, `implementable`), boards over it (`new-tasks`, `brainstormed-tasks`, `planned-tasks`, `revisions`, `done-tasks`), link-management and demote verbs, and plan tracking in its metadata key with a staleness `check` — the reporter reports an unlocatable plan, a decider demotes. It coexists with `workflow` in the same project as an independent view (see capability independence): users of workflow_ai see a stage vocabulary, never the substrate, and the substrate never learns what a "stage" is. It is the initiative's proof that a capability can impose a whole tailored vocabulary without contaminating the shared surface.

## Part 3: the extension surface

Capabilities are the primary extension point, but not the only seam. The honest map:

| Seam | What it extends | Mechanism | Storage |
|---|---|---|---|
| **Capability** | Vocabulary, boards, verbs, guide, metadata *(planned)*, TUI cell *(planned)* | Go package implementing `capability.Capability`, registered in `cmd/atm/main.go`, enabled per project | Substrate (event-sourced) |
| **Persona** | Who the working agent is (developer, manager, admin, custom) | Persona registry: seeded built-ins + user-defined, prompt rendered into dev/manage context | JSON side-files under `<store>/personas/` |
| **Agent host** | Which agent binary `atm dev` / `atm manage` launches | Catalog + per-host launcher (`claude`, `codex`, `opencode`, ollama) | Agents config side-file |
| **Host plugins** | Skills/hooks installed into the selected agent | `atm init` installs embedded per-host plugin assets | Agent-side |
| **Embed endpoint** | Semantic search embedding | Per-project endpoint config — the one model-touching boundary | Project config |
| **Project vocabulary** | Search-term weighting (`atm vocabulary` — unrelated to capability label vocabulary) | Term list biasing semantic search | Project side-file |

Two honesty notes on the map. First, only the substrate (projects, tasks, labels, comments, enablement) is event-sourced; personas, pins, vocabulary, agents, and embedding config are side-files outside both the event log and the label substrate — they are extension seams, but not ledger facts. Second, the two prompt-facing seams compose rather than couple: personas define *who* is working, capabilities define *what the project's words mean*, and the only bridge is the manager action block telling the agent to go read the relevant guides.

### Toward third-party capabilities

Everything in Part 2 is written so that a capability is one self-contained package: it seeds its own vocabulary, mounts under its own name, explains itself, and — once the metadata column ships — keeps its state under its own key. What is missing for an external developer is distribution: registration is compile-time, and capability identity is the bare `Name()` string, which the metadata key and the enablement set both make load-bearing. The packaging design (in-process plugin vs. subprocess vs. separate binary mounting under `atm capability <name>`, stable capability identity vs. display name, upgrade and data-retention story) is an open follow-up of the initiative — deliberately after the metadata column and workflow_ai have proven the interface worth freezing.

### Adding the next one

When a subsystem needs structure the substrate lacks, ask in order:

1. **Can a board express it?** A grouping, a filter, a "current view" — these are saved queries, not features. Write the expression; write no code.
2. **Can existing labels express it?** Read every label's description first. The vocabulary is usually larger than it looks.
3. **Does it need private machine-readable state?** Then it is a capability. Keep the state in the capability's own metadata key *(planned; the comment-format pattern is legacy and closed to new uses)*, project anything filterable into labels, ensure the vocabulary on first use, expose verbs — not labels — to the callers, and write its guide: the capability explains itself, or its semantics will end up hand-written somewhere they will drift.
4. **Does it truly need a new store field?** Almost certainly not — capability state has a home now, and the bar for the shared model is higher still. If the answer is genuinely yes, it belongs to the substrate itself, applies to *every* task, and needs its own design discussion — because it is a decision imposed on every project ATM will ever hold. (A new task field touches the action vocabulary, the changeset, the fold, the core struct, the cache schema, and the golden logs — the friction is the point.)
