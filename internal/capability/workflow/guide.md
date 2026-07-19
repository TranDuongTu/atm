# Workflow capability — agent guide

Status transitions and planning priority for tasks: the paved road for the `status:*` and `priority:*` namespaces.

## What it means

Four mutating verbs — `atm capability workflow start` (in-progress), `open`, `block` (blocked), `complete` (done) — plus a read-only `atm capability workflow status` reporter and `atm capability workflow seed` to ensure the boards and the status/priority vocabularies. Each mutating verb swaps the task's `status:*` label (adds the target, removes any other), so exactly-one-status is an invariant the capability maintains. The store enforces nothing: raw `atm task label add/remove --label <CODE>:status:<value>` works; a human may hand-assign, rename, or delete any status or priority label. This is a paved road, not a fence.

## Vocabulary

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

## Brief

This capability's vocabulary is fixed — four status values and three priority values, all seeded by `atm capability workflow seed`, no more. Brief is not a setup interview; it is a walkthrough. Walk the human through the states and priorities and the boards they imply, and confirm the project will use them as-is:

- `status:open` — not started / under consideration. Lives on `<CODE>:open-tasks`.
- `status:in-progress` — someone is on it. Lives on `<CODE>:in-progress-tasks`.
- `status:blocked` — stuck pending something else. Still in-progress by intent; surface it so the human sees the blocker.
- `status:done` — stop. The four-state invariant is "exactly one `status:*` label per task"; the verbs (`start`/`open`/`block`/`complete`) maintain it for you.
- `priority:high` / `priority:medium` / `priority:low` — planning urgency. At most one per task; absent means default priority. The verbs do not touch priority — assign it by hand with `atm task label add --label <CODE>:priority:<value>`.

Also confirm the boards the capability seeds — `<CODE>:backlog` (no `status:*`), `<CODE>:open-tasks`, `<CODE>:in-progress-tasks`, `<CODE>:all-tasks` (every task; the TUI's default) — match how the human wants to view the project. If the human wants extra status or priority values (e.g. `status:review`, `priority:critical`), they may hand-add them via `atm label add`; the verbs only swap the four seeded statuses, so any extra state must be hand-assigned. Record the human's answers in the relevant label descriptions (`status:done`, `priority:high`, etc.) so the boards read them back.

## Autopilot

This capability's autopilot scope is the **status and priority hygiene** of every task. It is one of three scopes a manager runs; run it alongside the others, not in place of them. The three scopes:

- **Grooming.** What should be planned, what should be rejected, what needs a follow-up. Triage the `<CODE>:backlog` board: every untriaged task gets a status via `atm capability workflow start|open|block` (or hand-assignment), or is closed. A task that is not worth doing is rejected (label it `status:done` with a comment saying why, or remove it) — backlog is not a graveyard.
- **Planning.** What should be done next, in what order, at what priority. Walk `<CODE>:open-tasks` and `<CODE>:in-progress-tasks`; order them with `priority:high` / `priority:medium` / `priority:low` (seeded by this capability; assign by hand — the verbs do not touch priority). Confirm each `in-progress` task is still in progress; `complete` what's done, `block` what's stuck.
- **Retrospect.** Once a task is `status:done`, confirm its ledger is honest: the title matches what was actually done, comments record the decision and result, and any `context:` pointers it touched are stamped. Hygiene applies to done tasks too — a closed task with a stale title or a missing outcome is a future agent's trap.

Across all three: enforce exactly-one-`status:*` and at-most-one-`priority:*` on every task so the boards never present conflicting views. Keep the boards themselves alive — `atm capability workflow seed --project <CODE>` is idempotent; run it whenever a board is missing.

Do not invent new status or priority values on the human's behalf; ask first (Brief) or leave the task untriaged.
