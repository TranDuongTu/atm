# Workflow capability — agent guide

Status transitions for tasks: the paved road for the `status:*` namespace.

## What it means

Status transitions are exposed as four mutating verbs — `atm workflow start` (in-progress), `atm workflow open`, `atm workflow block` (blocked), `atm workflow complete` (done) — plus a read-only `atm workflow status` reporter and `atm workflow seed` to ensure the boards. Each mutating verb swaps the task's `status:*` label (adds the target, then removes any other), so exactly-one-status is an invariant the capability maintains. The store still enforces nothing: raw `atm task label add/remove --label <CODE>:status:<value>` works and a human may hand-assign, rename, or delete any status label. This capability is a paved road, not a fence — a project can replace it with a different transition model.

## Vocabulary

`status:open` means not done; `status:in-progress` means someone is on it; `status:blocked` means stuck; `status:done` means stop.

Boards ensured on project create / label seed / TUI use:

- `<CODE>:backlog` (`NOT status:*`) — untriaged jottings.
- `<CODE>:open-tasks` (`status:open`) — active work.
- `<CODE>:in-progress-tasks` (`status:in-progress`).
- `<CODE>:all-tasks` — every task; the default board a UI selects.

In an older project where a board is absent, the expression fallback applies (`--label <CODE>:status:open` etc.).

## How to use it

Prefer the verbs over raw `task label add/remove --label status:*`. Check where a task stands with `atm workflow status --task <ID>`; claim work with `atm workflow start --task <ID>`; finish with `atm workflow complete --task <ID>`. Review untriaged jottings via `atm task list --project <CODE> --label <CODE>:backlog`.

## Manager duty

None. This capability contributes no dedicated manager action: status hygiene (untriaged tasks, stale in-progress work) falls under the manager's core curate role.
