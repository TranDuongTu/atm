# workflow_ai capability — agent guide

The AI-native task cycle: brainstorm → clarify → plan → ready → implement → done, over the `stage:*` namespace, with task links and plan tracking in this capability's metadata key. Fully independent of the `workflow` capability (`status:*`): disjoint namespaces, no interplay — a task may carry both views.

## What it means

Stage verbs — `atm capability workflow_ai brainstorm`, `atm capability workflow_ai clarify`, `atm capability workflow_ai plan --kind file|commit|ephemeral --ref <ref>`, `atm capability workflow_ai ready`, `atm capability workflow_ai done` — climb the ladder one rung at a time; each swaps the task's `stage:*` label (adds the target, removes any other), so exactly-one-stage is an invariant the capability maintains. "New" is the absence of any stage label, not a label. `atm capability workflow_ai demote --task X --reason "..."` resets any stage back to new, clears the plan record, and logs the reason as a comment. The store enforces nothing: raw `atm task label add/remove` works. This is a paved road, not a fence.

There is no `implement` verb: implementation is your dev session. The gate is doctrine — **never implement a task whose stage is not `stage:implementable`**; check the stage first and refuse otherwise.

## Vocabulary

Stages (exactly one per task; absence = new):
- `stage:brainstormed` — the idea has been explored.
- `stage:clarified` — scope and success criteria settled.
- `stage:planned` — a plan locator is recorded in this capability's metadata.
- `stage:implementable` — planned AND sized for one implementation session; cleared for implementation.
- `stage:done` — completed the cycle.

Marker:
- `wfai:revision` — this task is a revision follow-up of a bigger planned task; the link itself (`revision_of`) lives in the metadata payload, the marker only makes it board-visible.

Boards (seeded by `atm capability workflow_ai seed` / project create):
- `<CODE>:new-tasks` (`NOT stage:*`) — the intake queue, not yet brainstormed.
- `<CODE>:brainstormed-tasks` (brainstormed or clarified) — in refinement.
- `<CODE>:planned-tasks` (planned or implementable) — has a recorded plan.
- `<CODE>:revisions` (revision marker, not done) — follow-ups still needing refinement.
- `<CODE>:done-tasks` (`stage:done`).

## Links and plans

- `atm capability workflow_ai link --task X --revision-of Y` — X is a refinement follow-up of Y (one parent max). `--relates-to Y` — generic association. `unlink` reverses either. `atm capability workflow_ai links --task X` shows both directions.
- `plan` records WHERE the plan lives: `--kind file --ref docs/...` (repo-relative), `--kind commit --ref <rev>`, or `--kind ephemeral --ref "session ..."` for a plan that lives in a conversation. Record ephemeral plans honestly — they are unverifiable and always at-risk.
- `atm capability workflow_ai report --project <CODE>` — the staleness check: verifies every planned/implementable task's plan (file existence, commit resolvability from the current directory) and lists what is at risk. The reporter never demotes; **you** decide, then `demote --reason`.

## Sizing doctrine

A task is sized to one plan a framework like superpowers can execute in a single session. When a planned task is bigger than that, split it: create follow-up tasks linked with `--revision-of`, each entering the cycle at its own stage. The revisions board is the refinement queue.

## Brief

Walk the human through the ladder and confirm the project will use it as-is: the five stages, absence-as-new, the five boards, the plan-locator kinds, and the two link types (`revision_of`, `relates_to`). The vocabulary is fixed; extra stages are not part of the paved road. Confirm where plans normally live (committed plan docs vs ephemeral sessions) and record that preference in the `stage:planned` label description.

## Autopilot

The mechanical loop, run at session start:
1. `report --project <CODE>`; for each unlocatable or unrecoverable plan, ask the decider (or decide, if you are the decider) and `demote --reason` — replanning is cheaper than implementing against a ghost.
2. Advance tasks whose next rung's evidence exists: brainstormed notes → `brainstorm`; settled scope → `clarify`; a written plan → `plan`; reviewed and sized → `ready`.
3. Never skip rungs; never implement below `stage:implementable`; split oversized planned tasks into `--revision-of` follow-ups.