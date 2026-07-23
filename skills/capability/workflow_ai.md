---
name: workflow_ai
description: AI-native task cycle (queue→brainstorm→clarify→plan→done) with spec/plan tracking, links, and action-oriented stage boards.
labels: [stage:*, wfai:*]
boards: [to-brainstorm, to-clarify, to-plan, to-implement, revisions, done-tasks]
---
# workflow_ai capability — agent guide

The AI-native task cycle: queue → brainstorm → clarify → plan → done, over the `stage:*` namespace, with spec/plan locators, task links, and demotion breadcrumbs in this capability's metadata key. Independent of the `workflow` capability (`status:*`): the label namespaces are disjoint and neither capability's verbs touch the other's labels — a task may carry both views. Disjointness is about labels, not evidence: another capability's state is admissible evidence for staging (a `status:done` task with completed work behind it is evidence for `stage:done`).

## Semantics

Stage verbs climb the ladder one rung at a time; each swaps the task's `stage:*` label (adds the target, removes any other), so exactly-one-stage is an invariant the capability maintains. `queued` is a real stored label — the explicit entry stamp into the cycle. A task with no `stage:*` label is simply not in the workflow_ai cycle (it may be a context pointer belonging to `contextmap`, a naked jotting, or a deliberate deferral). The store enforces nothing: raw `atm task label add/remove` works. This is a paved road, not a fence.

Adoption: when this capability is enabled over an existing backlog, legacy tasks are stamped directly to the stage their existing evidence supports — completed work is its own evidence, so a finished task is stamped `stage:done` outright. Raw `atm task label add` is the sanctioned path for backfill; the one-rung climb governs live refinement, not adoption. Until the backlog is backfilled, the boards misread history as intake.

There is no `implement` verb: implementation is a dev session. The gate is doctrine — **never implement a task whose stage is not `stage:planned`**; check the stage first and refuse otherwise.

Stages (exactly one per task; absence = not in the cycle):
- `stage:queued` — entry; in the cycle, not yet brainstormed.
- `stage:brainstormed` — the idea has been explored; ready to clarify.
- `stage:clarified` — scope and success criteria settled; a spec locator is recorded in this capability's metadata.
- `stage:planned` — a plan locator is recorded; cleared for implementation.
- `stage:done` — completed the cycle.

Markers:
- `wfai:revision` — this task is a revision follow-up of a bigger planned task; the link itself (`revision_of`) lives in the metadata payload, the marker only makes it board-visible.
- `wfai:framework` — a stored label (not stamped on tasks) carrying the project's framework conventions in its description; written during setup, read at session start. See Converge.

Boards (seeded by `atm capability workflow_ai seed` / project create):
- `<CODE>:to-brainstorm` (`stage:queued`) — what to brainstorm next.
- `<CODE>:to-clarify` (`stage:brainstormed`) — what to clarify (write a spec for) next.
- `<CODE>:to-plan` (`stage:clarified`) — what to plan next.
- `<CODE>:to-implement` (`stage:planned`) — what can be implemented now.
- `<CODE>:revisions` (revision marker, not done) — follow-ups still needing refinement.
- `<CODE>:done-tasks` (`stage:done`).

Sizing doctrine: a task is sized to one plan a framework like superpowers can execute in a single session. When a planned task is bigger than that, split it: create follow-up tasks linked with `--revision-of`, each entering the cycle at its own stage. The revisions board is the refinement queue. The manager walks `revision_of` links to stay aware of follow-ups sharing a parent's plan.

## Actions

- `atm capability workflow_ai queue --task <ID>` — stamp the entry label (new → queued).
- `atm capability workflow_ai brainstorm --task <ID>` — mark the idea brainstormed (queued → brainstormed).
- `atm capability workflow_ai clarify --task <ID> --kind file|commit|ephemeral --ref <ref>` — record WHERE the spec lives: `--kind file --ref docs/superpowers/specs/...` (repo-relative), `--kind commit --ref <rev>`, or `--kind ephemeral --ref "session ..."` for a spec that lives in a conversation. Record ephemeral specs honestly — they are unverifiable and always at-risk.
- `atm capability workflow_ai plan --task <ID> --kind file|commit|ephemeral --ref <ref>` — record WHERE the plan lives (same kind/ref shape as clarify; clarified → planned, or updates the locator from planned).
- `atm capability workflow_ai done --task <ID>` — close the cycle (planned → done).
- `atm capability workflow_ai demote --task <ID> --reason "..."` — reset any stage back to queued, clear the spec+plan records, log the reason as a comment.
- `atm capability workflow_ai link --task X --revision-of Y` — X is a refinement follow-up of Y (one parent max). `--relates-to Y` — generic association. `unlink` reverses either. `links --task X` shows both directions.
- `atm capability workflow_ai report --project <CODE>` — the staleness check: verifies every clarified/planned task's spec and plan (file existence, commit resolvability from the current directory) and lists what is at risk. The reporter never demotes; the operator decides, then `demote --reason`.
- `atm capability workflow_ai seed --project <CODE>` — idempotently ensure vocabulary and boards.

## Converge

A converged project under this capability looks like:

- **The framework conventions are recorded and current.** The `<CODE>:wfai:framework` label's description says which framework the project uses (superpowers, speckit, grillme, or none), where specs and plans normally live (committed docs vs ephemeral sessions), sizing expectations, and any customizations. Agents read it at session start (`atm label show <CODE>:wfai:framework`) and bend accordingly; when practice drifts from what it says, it gets updated — convention changes are confirmed with the decider before the label is rewritten. Where plans normally live is also recorded in the `stage:planned` label description. Specific one-off answers stay as task comments; only conventions live in `wfai:framework`.
- **Coverage is total.** Every workflow task carries a stage validated against its evidence; absence means the task is not in the workflow_ai cycle (a context pointer, or un-triaged intake) — never "this task predates the capability". On adoption, backfilling stages across the existing backlog (see Semantics) is the first convergence job. Context pointers (`context:*`) are `contextmap`'s domain — never queue them into workflow_ai.
- **Every stage is evidenced.** A `stage:brainstormed` task has exploration notes; `stage:clarified` has a locatable spec (`report` verifies); `stage:planned` has a locatable plan (`report` verifies). Tasks whose evidence has decayed are demoted with a reason — replanning is cheaper than implementing against a ghost.
- **Staging is recognition at bounded depth.** Whoever stages a task reads its title, labels, and latest comments; a full read only when promoting a rung. When in doubt, keep the lower rung — under-staging is recoverable, over-staging misleads. Staging recognizes evidence that exists; creating it (exploration notes, settled scope, specs, plans) is developing-session work — a curator stamps and demotes but does not invent evidence to enable a climb.
- **The intake queue is triaged.** Tasks on `<CODE>:to-brainstorm` worth pursuing are brainstormed; the rest are deferred deliberately, with the deferral recorded as a task comment so absence reads as a decision, not neglect. Ambiguity goes to the decider — or is decided, recorded as a task comment, and flagged for the next decider review. Never let ambiguity stall silently.
- **No skipped rungs, no premature implementation.** Tasks advance one rung at a time and only when the next rung's artifact exists; nothing below `stage:planned` is implemented.
- **Links hold.** Every `revision_of`/`relates_to` link points at a live task and a relationship that still holds; stale links are unlinked. Oversized planned tasks are split into `--revision-of` follow-ups.
- **The vocabulary is fixed.** Five stages, queued-as-entry, six boards, two link types. Extra stages are not part of the paved road.