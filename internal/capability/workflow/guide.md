# Workflow capability — agent guide

Status transitions for tasks: the paved road for the `status:*` namespace.

## What it means

Four mutating verbs — `atm capability workflow start` (in-progress), `open`, `block` (blocked), `complete` (done) — plus a read-only `atm capability workflow status` reporter and `atm capability workflow seed` to ensure the boards. Each mutating verb swaps the task's `status:*` label (adds the target, removes any other), so exactly-one-status is an invariant the capability maintains. The store enforces nothing: raw `atm task label add/remove --label <CODE>:status:<value>` works; a human may hand-assign, rename, or delete any status label. This is a paved road, not a fence.

## Vocabulary

- `status:open` — not done.
- `status:in-progress` — someone is on it.
- `status:blocked` — stuck.
- `status:done` — stop.

Boards (declared by this capability, seeded by `atm capability workflow seed` / project create):
- `<CODE>:backlog` (`NOT status:*`) — untriaged jottings.
- `<CODE>:open-tasks` (`status:open`).
- `<CODE>:in-progress-tasks` (`status:in-progress`).
- `<CODE>:all-tasks` (`*`) — every task; the TUI's default-selected board.

## Brief

Interview the human to set up this project's status model. Ask:
- "Do you use these four status values, or do you want different ones (e.g. `status:review`, `status:wip`)?" — record the answer by creating any extra `status:<value>` labels with descriptions via `atm label add`.
- "What does 'done' mean for this project — merged, shipped, closed?" — write the answer into the `status:done` label's description.
- "Is there a board you want beyond backlog/open-tasks/in-progress-tasks/all-tasks?" — create it with `atm label add --expr`.

Leave the human's answers in the label descriptions; the boards read them.

## Autopilot

Keep status hygiene:
1. Run `atm task list --project <CODE> --label <CODE>:backlog` — triage untriaged tasks: assign a status with `atm capability workflow start|open|block` (or hand-assign the label).
2. Run `atm task list --project <CODE> --label <CODE>:in-progress-tasks` — confirm each is still in progress; `complete` what's done, `block` what's stuck.
3. Ensure boards exist: `atm capability workflow seed --project <CODE>` (idempotent).

Do not invent new status values on the human's behalf; ask first (Brief) or leave untriaged.