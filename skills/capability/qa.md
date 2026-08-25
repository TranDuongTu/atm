---
name: qa
description: "QA flow capability: verifies finished development work through born-claimed test scaffolds, certifying originals only."
brief: QA flow capability over finished development work; read its guide before absorbing, scaffolding, or passing verification.
labels: [qa:*, qa-out:*]
boards: [qa-inbox, qa-pipeline, qa-out-board]
---
# qa capability — agent guide

Verification of finished development work. qa absorbs the ORIGINAL task, spawns test scaffolds beneath it, and certifies the original once the scaffolds have all passed.

qa is a FLOW capability. It never reads another capability's metadata and never calls another capability's verbs; it reaches upstream only through project wiring, and downstream only by stamping its own finish socket.

## Semantics

### The three lanes

- `<CODE>:qa-inbox` — finished work qa has not decided about. Its expression is PROJECT WIRING, typically the upstream finish socket narrowed to the types qa verifies: `scrum-stage:done AND (scrum:task OR scrum:bug OR scrum:story)`. Epics and design tasks never arrive, because that expression excludes them. A non-empty inbox is the signal to dispatch the manager.
- `<CODE>:qa-pipeline` — originals under verification and their scaffolds (`qa:* AND NOT qa-out:*`). Certified originals stay visible.
- `<CODE>:qa-out-board` — settled evictions (`qa-out:*`), permanent until someone calls `release`.

### The two axes

`qa:*` is the CLAIM axis, and it doubles as the state: `testing`, then `done`. A certified original stays claimed, which is why it stays in the pipeline lane instead of vanishing.

`qa-out:*` is the EVICT axis; its value is the reason: `failed`, `not-relevant`, `covered-by`.

### Originals and scaffolds

An ORIGINAL is work qa absorbed from its inbox. A SCAFFOLD is a task qa created beneath an original to hold one verification effort — a staging run, a dev environment pass. Scaffolds are born claimed, so they never appear in anyone's inbox, and they are marked by a `part_of` pointer in `Meta["qa"]`. Scaffolds do not nest.

**qa:done is only ever stamped on absorbed originals, never scaffolds.** This is the guarantee everything downstream leans on. A scaffold that passes simply gives up its claim and keeps its `part_of` for history. `pass` on an original is refused while any scaffold still holds a claim, and the refusal names them. Release selection gets originals-only for free because of this rule — nothing has to filter scaffolds out.

### The sockets

- FINISH: `<CODE>:qa:done` — this original is verified.
- EVICT: `<CODE>:qa-out:*` — qa considered the work and declined it. A `failed` eviction is a verdict, not a shrug: it is the backward-flow signal the manager routes.

Both are stored labels, never metadata: intake expressions are evaluated by the store's resolver, which never reads metadata.

### State

`Meta["qa"]` holds the scaffold topology and nothing else:

- `part_of` — on a scaffold: the original it verifies. Its presence is what MAKES a task a scaffold.
- `scaffolds` — on an original: its scaffold roster. This is the list `pass` checks, so it is stored rather than derived.
- `covered_by` — the task whose verification covered this one; required by the `covered-by` eviction.

### Story convergence

The default is the INTEGRATION PASS: when a story converges, qa absorbs the story too, as a distinct certification level. Its children certify the units; the story certifies that they work together. The lighter mode — verifying children only and evicting the story as `covered-by` — is a deliberate, recorded choice, not the default.

### Backward flow

Nothing automatic points backward. When verification fails, the manager either creates a follow-up task into the pool, or runs the RE-REVIEW PAIR: the upstream capability's `reopen` plus this capability's `release`. Two audited, order-independent verbs — never composed, because no capability may un-stamp a sibling.

## Actions

- `atm capability qa absorb --task <ID>` — claim an inbox task as an original under verification. Refuses a scaffold (they are born claimed) and refuses a task qa evicted (release it first).
- `atm capability qa scaffold --task <ORIGINAL> --title "..."` — create a test scaffold born into the pipeline beneath an original, recorded at both ends.
- `atm capability qa pass --task <ID>` — record successful verification. On a scaffold: gives up the claim, keeps `part_of`. On an original: stamps the finish socket, refused while any scaffold is still under test.
- `atm capability qa evict --task <ID> [--reason failed|not-relevant|covered-by] [--covered-by <ID>]` — settle work out of qa.
- `atm capability qa release --task <ID> --reason "..."` — withdraw qa's perspective entirely; the task returns to the pool.
- `atm capability qa report --project <CODE>` — lane rosters plus findings (read-only).
- `atm capability qa seed --project <CODE>` — ensure the vocabulary and lane boards exist.

## Converge

A converged project reads like this:

- The Inbox is empty, or every row in it carries a comment saying why it is deferred.
- Every original whose scaffolds have all passed is itself stamped `qa:done` — until it is, downstream sees nothing.
- Every scaffold's `part_of` points at a live original, and every roster entry names a task that exists.
- No scaffold carries the finish socket.
- Every eviction carries a reason, and `covered-by` evictions carry `covered_by`.
- No `report` finding is left standing: each is resolved or answered with a recorded decision.

## Duty: manager

### Triage
List the inbox. Absorb finished dev work with `atm capability qa absorb --task <ID>`,
then create its test scaffolds (`qa scaffold --task <ID> --title "test: staging — ..."`)
per the project's test surface. Prefer the highest converged unit: when a story arrives
converged, absorb it as the INTEGRATION pass (children were unit certification); in the
lighter mode, evict the story `covered-by` its children instead. Evict non-testable work
(`not-relevant`).

### Advance
Run `atm capability qa report --project <CODE>`. Originals whose scaffolds all passed →
`qa pass --task <ORIGINAL>`. Stalled scaffolds → dispatch a tester/developer persona.
Never stamp `qa pass` on an original with live scaffolds — the verb refuses; fix the
scaffolds instead.

### Route
A fresh `qa-out:failed` eviction means certified-done work is NOT done. Run the backward
pair: `atm capability scrum reopen --task <ID> --reason "<what failed>"` then
`atm capability qa release --task <ID> --reason "re-spiral after failed verification"`.
If the failure is new scope rather than broken work, create a follow-up task in the pool
instead and leave the eviction settled.
