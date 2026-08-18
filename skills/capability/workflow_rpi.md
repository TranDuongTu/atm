---
name: workflow_rpi
description: Manager-oriented RPI workflow: backlog, product roadmap, build pipeline, reject, with private task links and convergence reports.
brief: Manager-oriented RPI workflow over backlog/product/pipeline/reject; read its guide before triaging or linking RPI work.
labels: [rpi:*, rpi-product:*, rpi-dev:*, rpi-reject:*]
boards: [rpi-backlog, rpi-product, rpi-pipeline, rpi-reject]
---
# workflow_rpi capability — agent guide

A manager-oriented perspective over EVERY task in the project. Other capabilities require a task to be stamped onto their road before their boards can see it; `workflow_rpi` inverts that — a task is in RPI backlog until RPI decides otherwise, so nothing created by another capability, imported, or jotted down can hide from the manager's view.

Independent of `workflow` (`status:*`) and `workflow_ai` (`stage:*`): the label namespaces are disjoint, and this capability never reads another capability's metadata nor calls its verbs. Disagreement is allowed and meaningful — a `status:done` task may sit in `rpi:product` as roadmap context, and a `stage:planned` task is not in `rpi:pipeline` until the manager accepts it.

## Semantics

Four exclusive task sets, three of them stored:

- `rpi-backlog` — intake. Computed as `NOT rpi:*`; never a stored label. Absence is the state.
- `rpi:product` — product roadmap item under consideration.
- `rpi:pipeline` — buildable work, linked to a product task.
- `rpi:reject` — considered and rejected from the RPI perspective.

Four boards, one per set (seeded by `atm capability workflow_rpi seed` / project create):

- `<CODE>:rpi-backlog` (`NOT rpi:*`) — what RPI has not decided about yet.
- `<CODE>:rpi-product` (`rpi:product`) — the roadmap.
- `<CODE>:rpi-pipeline` (`rpi:pipeline`) — what is being built.
- `<CODE>:rpi-reject` (`rpi:reject`) — what was considered and declined.

No board filters on metadata. Anything that needs links, missing parents, dependency blockers, or malformed payloads read is reporter output, not a board expression.

`rpi:*` is the exclusive lane axis: the recorders maintain at most one lane label per task. `rpi-product:*`, `rpi-dev:*`, and `rpi-reject:*` are lane-local axes — meaningful only inside their lane, and converged away by the recorders when a task changes lane.

- product statuses: `unclarified`, `clarified`.
- dev statuses: `clarified`, `brainstormed`, `planned`, `implementing`, `review`, `done`.
- reject reasons: `duplicate`, `out-of-scope`, `not-worth-it`, `covered-by`.

There is no `blocked` dev status. Blocking is DERIVED from unresolved `depends_on` links, because a task can be planned and blocked at the same time.

Task topology lives in `Task.Meta["workflow_rpi"]` and nowhere else — there is no ATM-wide link model, and target task IDs never appear in labels:

```json
{"v": 1, "product_of": "ATM-abc123", "depends_on": ["ATM-def456"], "relates_to": ["ATM-aaa111"], "covered_by": "ATM-bbb222"}
```

- `product_of` — the pipeline task's roadmap parent. Required for pipeline entry, at most one.
- `depends_on` — outbound execution dependencies; a dependency that is not `rpi-dev:done` blocks.
- `relates_to` — generic association, no workflow semantics.
- `covered_by` — the task that covers this one; required for the `covered-by` reject reason.

Only OUTBOUND facts are stored. Inbound views — a product's pipeline children, a task's dependents — are derived by scanning the project. Unknown payload fields survive every write; a payload this capability cannot parse makes its mutating verbs fail with hand-repair guidance rather than overwrite unreadable state, while reporters and the task annotation degrade to at-risk.

The store enforces none of this. Raw `atm task label add/remove` works. This is a paved road, not a fence.

## Actions

- `atm capability workflow_rpi product --task <ID> [--status unclarified|clarified]` moves a task to the product roadmap.
- `atm capability workflow_rpi pipeline --task <ID> --product <PRODUCT-ID> [--status clarified|brainstormed|planned|implementing|review|done]` moves a task to the build pipeline and records `product_of`. It REFUSES a parent that is not already in the product lane — manager intent stays explicit.
- `atm capability workflow_rpi reject --task <ID> [--reason duplicate|out-of-scope|not-worth-it|covered-by] [--covered-by <ID>]` records considered-and-rejected work.
- `atm capability workflow_rpi release --task <ID> --reason "..."` clears RPI labels and metadata so the task returns to backlog, and records the reason as a comment. Labels owned by other capabilities are untouched.
- `atm capability workflow_rpi status --task <ID> --product-status clarified` updates product status.
- `atm capability workflow_rpi status --task <ID> --dev-status planned` updates pipeline status.
- `atm capability workflow_rpi link --task <ID> --depends-on <ID>` records an execution dependency.
- `atm capability workflow_rpi link --task <ID> --relates-to <ID>` records a generic relation.
- `atm capability workflow_rpi unlink --task <ID> --depends-on|--relates-to <ID>` removes those links.
- `atm capability workflow_rpi links --task <ID>` shows outbound and derived inbound links.
- `atm capability workflow_rpi report --project <CODE>` reports lane rosters, the backlog count, link summaries, and at-risk findings.
- `atm capability workflow_rpi seed --project <CODE>` ensures vocabulary and boards.

Link verbs validate same-project, no self-link, and target existence; a direct two-node dependency cycle is refused. Deeper cycles are reporter findings, not recorder errors.

## Converge

A converged project under this capability looks like:

- **The backlog is triaged, not neglected.** Every task on `rpi-backlog` has either been moved to product, pipeline, or reject, or is intentionally still untriaged — with the intent recorded as a comment, so absence reads as a decision.
- **Product items are decision-ready.** Each `rpi:product` task is clarified enough for roadmap discussion and decomposition, or is explicitly `rpi-product:unclarified` and queued for that clarification.
- **The pipeline is buildable and parented.** Every `rpi:pipeline` task carries `product_of` pointing at a live product task, and a dev status that matches reality. Orphan pipeline tasks are report findings, and the manager either links or releases them.
- **Blocking is derived, not asserted.** Dependencies are recorded with `depends_on`; blocked work is read off the report rather than stamped as a label.
- **Rejections carry their reason.** Every `rpi:reject` task has a reject reason, a `covered_by` pointer when the reason is `duplicate` or `covered-by`, and a comment whenever the judgment is not obvious from labels and links.
- **No at-risk findings are left standing.** `report` lists missing link targets, unparseable payloads, stale lane-local labels, vocabulary drift, and dependency cycles; each is resolved or explicitly accepted.

The manager persona is the decider. Reporters identify gaps and never mutate; the manager records decisions through the RPI verbs and leaves comments for judgment that labels and links cannot carry.
