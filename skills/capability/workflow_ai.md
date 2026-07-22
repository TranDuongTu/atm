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