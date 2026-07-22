---
name: workflow
description: Status transitions and planning priority for tasks: the paved road for the status:* and priority:* namespaces.
labels: [status:*, priority:*]
boards: [backlog, open-tasks, in-progress-tasks, all-tasks]
---
# Workflow capability — agent guide

Status transitions and planning priority for tasks: the paved road for the `status:*` and `priority:*` namespaces.

## Semantics

Each mutating verb swaps the task's `status:*` label (adds the target, removes any other), so exactly-one-status is an invariant the capability maintains. The store enforces nothing: raw `atm task label add/remove --label <CODE>:status:<value>` works; a human may hand-assign, rename, or delete any status or priority label. This is a paved road, not a fence.

Status (lifecycle — exactly one per task):
- `status:open` — not done.
- `status:in-progress` — someone is on it.
- `status:blocked` — stuck.
- `status:done` — stop.

Priority (planning — at most one per task, absent means default):
- `priority:high` — do this first.
- `priority:medium` — do after high-priority work.
- `priority:low` — do when no higher-priority work remains.

Boards (declared by this capability, seeded by `atm capability workflow seed` / project create):
- `<CODE>:backlog` (`NOT status:*`) — untriaged jottings.
- `<CODE>:open-tasks` (`status:open`).
- `<CODE>:in-progress-tasks` (`status:in-progress`).
- `<CODE>:all-tasks` (`*`) — every task; the TUI's default-selected board.

The vocabulary is fixed — four status values and three priority values, no more. If the humans want extra values (e.g. `status:review`, `priority:critical`), they may hand-add them via `atm label add`; the verbs only swap the four seeded statuses, so any extra state must be hand-assigned, and the decision belongs in the affected label's description.

## Actions

- `atm capability workflow start --task <ID>` — swap to `status:in-progress`.
- `atm capability workflow open --task <ID>` — swap to `status:open`.
- `atm capability workflow block --task <ID>` — swap to `status:blocked`.
- `atm capability workflow complete --task <ID>` — swap to `status:done`.
- `atm capability workflow status --project <CODE>` — read-only status report.
- `atm capability workflow seed --project <CODE>` — idempotently ensure the status/priority vocabulary and the boards.

Priority is never touched by the verbs — assign it by hand with `atm task label add --label <CODE>:priority:<value>`.

## Converge

A converged project under this capability looks like:

- **Invariants hold.** Every task carries exactly one `status:*`; at most one `priority:*`. The boards never present conflicting views.
- **The backlog is triaged, not a graveyard.** Every task on `<CODE>:backlog` either gets a status or is rejected — a task not worth doing is labeled `status:done` with a comment saying why, or removed.
- **The in-progress list is honest.** Each `status:in-progress` task is actually being worked; finished work is completed, stuck work is blocked so the blocker is visible.
- **Open work is ordered.** `<CODE>:open-tasks` and `<CODE>:in-progress-tasks` carry `priority:*` labels that reflect the current plan.
- **Done tasks have honest ledgers.** The title matches what was actually done, comments record the decision and result, and any `context:` pointers touched are stamped. A closed task with a stale title or missing outcome is a future agent's trap.
- **The boards stay alive.** `atm capability workflow seed --project <CODE>` is idempotent; a missing board means it should be run.
- **The vocabulary stays deliberate.** New status/priority values appear only by explicit human decision, recorded in the label's description — never invented on the human's behalf.