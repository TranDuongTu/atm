# CLI Contract: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-07-02 | **Revision**: v2.0.0

Every command accepts `--store <path>` (optional; defaults to `$ATM_HOME`) and
`--actor <id>` (required for mutating commands). Output is JSON when `--json`
is passed (or when stdout is not a TTY for query commands); otherwise
human-readable. All output is deterministic for a given store and arguments
(SC-002a).

## Project

```
atm project create --code <CODE> --name <NAME> [--actor <id>]
atm project list
atm project show --code <CODE>
atm project set-name --code <CODE> --name <NAME> [--actor <id>]
atm project remove --code <CODE> [--actor <id>]
atm project repo add --code <CODE> --path <PATH> [--actor <id>]
atm project repo remove --code <CODE> --path <PATH> [--actor <id>]
```

- `create`: creates a project. `--code` must match `^[A-Z]{3,6}$` (3-6 uppercase
  ASCII letters) and be unique. `--name` is the display name. NO labels,
  type-axis, or repo-paths are accepted at create time (FR-004). The project is
  created with an empty label set and empty repo paths.
- `list`: lists projects (code, name).
- `show`: shows a project's facts (code, name, repo paths, guide status) and
  its labels grouped by namespace.
- `set-name`: renames a project.
- `remove`: removes a project (guard: zero tasks).
- `repo add`/`repo remove`: associate/disassociate a repo path (informational).

## Label (global registry)

```
atm label add --name <NAME> [--description <DESC>] [--actor <id>]
atm label remove --name <NAME> [--actor <id>]
atm label list [--project <CODE>] [--namespace <NS>]
atm label show --name <NAME>
```

- `add`: adds a label to the global registry. `--name` is the full hierarchical
  name `<CODE>:<namespace>:<value>` or `<CODE>:<tag>` (FR-019). The `<CODE>`
  prefix must match an existing project. Any namespace is valid; there is no
  whitelist. If the label already exists, `--description` updates it.
- `remove`: soft-removes a label from the registry. Existing tasks retain it;
  new assignments reject it. Response includes `retained_usage` (count of tasks
  still carrying the label).
- `list`: lists labels. `--project <CODE>` filters to labels prefixed with
  `<CODE>:`. `--namespace <NS>` further filters to labels whose namespace
  segment equals `<NS>`. Output is grouped by namespace in human mode.
- `show`: shows a label's facts and the tasks carrying it.

## Task

```
atm task create --project <CODE> --title <TITLE> [--description <DESC>] [--label <L>]... [--actor <id>]
atm task list [--project <CODE>] [--label <L>]... [--status <S>] [--assignee <A>] [--claimant <C>] [--group-by <NS>]
atm task show --id <ID> [--with-context]
atm task set-title --id <ID> --title <TITLE> [--actor <id>]
atm task set-description --id <ID> --description <DESC> [--actor <id>]
atm task set-status --id <ID> --status <S> [--actor <id>]
atm task label add --id <ID> --label <L> [--actor <id>]
atm task label remove --id <ID> --label <L> [--actor <id>]
atm task link add --id <ID> --type <T> --target <ID2> [--actor <id>]
atm task link remove --id <ID> --type <T> --target <ID2> [--actor <id>]
atm task claim --id <ID> [--actor <id>]
atm task unclaim --id <ID> [--actor <id>]
atm task remove --id <ID> [--actor <id>]
```

- `create`: creates a task. Auto-assigns `<CODE>-<NNNN>`. Auto-applies
  `<CODE>:status:open` unless the user supplies a `<CODE>:status:*` label
  explicitly (in which case the supplied one wins and `open` is not added).
  `--label` may be repeated; each must exist in the global registry.
- `list`: lists tasks. `--label` is AND-intersection. `--status <S>` filters by
  the `<CODE>:status:<S>` label. `--group-by <NS>` regroups output under each
  value of namespace `<NS>` (mirrors the TUI `G` mode; FR-021). Without
  `--group-by`, output is a flat sorted list.
- `show`: shows a task. `--with-context` adds `links_out`/`links_in`,
  `conventions` (label-matched, ranked by matched-count desc then ID asc),
  `timeline`, and `guide` (project's guide or `null`).
- `set-status`: replaces the task's `<CODE>:status:*` label(s) with
  `<CODE>:status:<S>`. No state machine (FR-005); any status may replace any
  other. Validates `<CODE>:status:<S>` exists in the global registry.
- `label add`/`label remove`: add/remove a label on a task. Add validates the
  label exists in the global registry. Remove retains the label on the task
  record but removes it from the task's `labels` array.
- `link add`/`link remove`: add/remove a typed link. Types: `blocks`,
  `related-to`, `implements`, `documents`.
- `claim`/`unclaim`: atomic claim/unclaim; records actor and timestamp.
- `remove`: deletes a task (with cleanup of inbound links from other tasks).

## Entry (todos, followups, discussions)

```
atm todo add --id <ID> --text <TEXT> [--actor <id>]
atm todo toggle --id <ID> --todo <TODO_ID> [--actor <id>]
atm followup add --id <ID> --text <TEXT> [--assignee <A>] [--due <RFC3339>] [--actor <id>]
atm followup resolve --id <ID> --followup <FID> [--actor <id>]
atm discussion add --id <ID> --text <TEXT> [--actor <id>]
atm timeline --id <ID>
```

- `timeline`: returns all todos, followups, and discussions for a task in
  chronological order with authors and timestamps.

## Next / context

```
atm next --project <CODE> [--claim] [--actor <id>]
```

- Returns the highest-priority, unclaimed, non-blocked, non-terminal task in the
  project, plus the project guide. `--claim` atomically claims it for `--actor`.
  Terminal status labels: `done`, `cancelled`. Non-terminal: all others.

## Review

```
atm review request --id <ID> [--actor <id>]
atm review approve --id <ID> [--comment <TEXT>] [--actor <id>]
atm review reject --id <ID> --comment <TEXT> [--actor <id>]
atm review queue [--project <CODE>]
atm review followups [--project <CODE>]
atm review dashboard [--project <CODE>]
```

- `request`: sets the task's status label to `<CODE>:status:review` (replaces
  any existing status label).
- `approve`: sets the status label to `<CODE>:status:done`.
- `reject`: sets the status label to `<CODE>:status:open` and records
  `--comment` as a discussion entry by `--actor`.
- `queue`: tasks whose status label is `review`.
- `followups`: tasks with open followups.
- `dashboard`: combined coordinator view (claimed grouped by claimant, review
  queue, open followups, guide coverage/freshness).

## Project guide

```
atm project guide show --code <CODE>
atm project guide section add --code <CODE> --name <NAME> [--actor <id>]
atm project guide section rename --code <CODE> --name <OLD> --to <NEW> [--actor <id>]
atm project guide section remove --code <CODE> --name <NAME> [--actor <id>]
atm project guide section move --code <CODE> --name <NAME> --to <INDEX> [--actor <id>]
atm project guide ref add --code <CODE> --section <NAME> --kind <K> --target <T> [--actor <id>]
atm project guide ref remove --code <CODE> --section <NAME> --target <T> [--actor <id>]
atm project guide ref move --code <CODE> --section <NAME> --target <T> --to <INDEX> [--actor <id>]
atm project guide set-freshness --code <CODE> --threshold <DUR> [--actor <id>]
atm project guide status --code <CODE>
```

- `ref --kind` is `task` (target = task id) or `file` (target = absolute path).

## Actor

```
atm actor register --id <ID> [--name <NAME>]
atm actor list
```

- Actors are registered lazily on first use of `--actor <ID>`; explicit
  `register` sets the display name.

## TUI

```
atm tui
```

- Opens the Bubble Tea TUI. Tabs: Tasks, Projects, Dashboard. Every CLI op is
  mirrored (FR-002). See contracts/tui.md.