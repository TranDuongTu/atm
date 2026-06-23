# CLI Contract: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-06-23

The CLI (`atm`) is the stable, versioned API surface for agents (out-of-process) and the front-end the TUI wraps. Every operation is a subcommand. Output format is selected globally with `--output json|text` (default `text`). JSON output is deterministic: object keys sorted, stable whitespace, RFC 3339 UTC timestamps. All mutating commands accept `--actor <id>` (or read `ATM_ACTOR`). Errors go to stderr with a non-zero exit code and a stable JSON `{ "error": { "code": "...", "message": "..." } }` shape in JSON mode.

Global flags:
- `--store <path>`: path to the `.atm` store directory. Default: nearest `.atm/` walking up from CWD, else `.atm` in CWD.
- `--output json|text`: output format. Default `text`.
- `--actor <id>`: actor performing the operation (e.g. `agent:claude-1`, `human:alice`). Env: `ATM_ACTOR`. Required for mutating commands; optional for read commands.
- `--quiet`: suppress non-essential stdout in text mode.

Exit codes: `0` success; `1` generic error; `2` usage error; `3` not-found (e.g. task/project missing); `4` conflict (e.g. already claimed, invalid status transition).

## Command reference

### Store / init

```
atm init [--store <path>] [--actor <id>]
```
Creates an empty `.atm` store at `<path>` (default `.atm` in CWD). Idempotent: re-running on an existing store is a no-op. Initializes `actors.json` and `projects/`.

```
atm store path
```
Prints the resolved store path. (Read-only; useful for agents to confirm location.)

### Projects

```
atm project create --code <CODE> --name <NAME> [--type-axis <NS>] [--label <L>]... [--actor <id>]
atm project list
atm project show --code <CODE>
atm project set-type-axis --code <CODE> --namespace <NS> [--actor <id>]
atm project label add --code <CODE> --label <L> [--description <DESC>] [--actor <id>]
atm project label remove --code <CODE> --label <L> [--actor <id>]
atm project label list --code <CODE>
```
- `create`: creates a project. `--code` must match `^[A-Z][A-Z0-9-]{1,15}$` and be unique. `--label` may be repeated to seed the label set. `--type-axis` optionally declares the type namespace up front.
- `label remove`: soft removal. Warns (in text mode) or includes `retained_usage` (in JSON mode) when existing tasks still use the label.

### Tasks

```
atm task create --project <CODE> --title <TITLE> [--description <DESC>] [--label <L>]... [--actor <id>]
atm task show --id <ID> [--with-context]
atm task list [--project <CODE>] [--label <L>]... [--status <S>] [--assignee <ACTOR>] [--claimant <ACTOR>]
atm task set-status --id <ID> --status <S> [--actor <id>]
atm task set-title --id <ID> --title <TITLE> [--actor <id>]
atm task set-description --id <ID> --description <DESC> [--actor <id>]
atm task label add --id <ID> --label <L> [--actor <id>]
atm task label remove --id <ID> --label <L> [--actor <id>]
```
- `create`: assigns the next id `<CODE>-<N>`; status defaults to `open`.
- `show --with-context`: includes linked tasks (both directions), matching convention docs (label intersection, weighted by type axis), and the todo/followup/discussion timeline.
- `list`: filter by project, labels (AND-intersection), status, assignee (followup assignee), claimant. Sorted by id (project-then-numeric).

### Agent workflow (claim / next)

```
atm task next --project <CODE> [--actor <id>] [--claim]
atm task claim --id <ID> [--actor <id>]
atm task unclaim --id <ID> [--actor <id>]
```
- `next`: returns the highest-priority claimable, non-blocked, non-claimed, non-done task in `<CODE>`. Priority for v1 = a stable ordering: blocked-by count ascending, then created_at ascending (oldest first). `--claim` atomically claims the returned task under the project lock. Returns an empty result (not an error) if none claimable.
- `claim`/`unclaim`: set/clear the `claim` field. `claim` is atomic and errors with `4 conflict` if already claimed by another actor. An actor may claim multiple tasks.

### Links

```
atm task link add --id <ID> --type <T> --target <ID> [--actor <id>]
atm task link remove --id <ID> --type <T> --target <ID> [--actor <id>]
atm task link list --id <ID>
```
- `link add`/`remove`: managed typed edges. `related-to` deduplicates symmetrically. `blocks` implies a computed `blocked-by` reverse edge (not stored). Stale targets (deleted task) are preserved with a warning.
- `link list`: returns both stored edges and computed reverse edges, tagged with `direction: out|in`.

### Todos / Followups / Discussions

```
atm task todo add --id <ID> --text <TEXT> [--actor <id>]
atm task todo toggle --id <ID> --todo <TODO_ID> [--actor <id>]
atm task followup add --id <ID> --text <TEXT> [--assignee <ACTOR>] [--due <RFC3339>] [--actor <id>]
atm task followup resolve --id <ID> --followup <F_ID> [--actor <id>]
atm task discussion add --id <ID> --text <TEXT> [--actor <id>]
atm task timeline --id <ID>
```
- `timeline`: returns todos, followups, discussions, and history merged and sorted by timestamp ascending. Each entry carries a `kind` discriminator (`todo|followup|discussion|history`).

### Review (human coordinator)

```
atm review request --id <ID> [--actor <id>]            # sets status -> review
atm review approve --id <ID> [--comment <TEXT>] [--actor <id>]   # status -> done
atm review reject  --id <ID> [--comment <TEXT>] [--actor <id>]   # status -> in-progress (or open), comment recorded as discussion
atm review queue [--project <CODE>]                     # tasks with status review, grouped by claimant
atm review followups [--project <CODE>]                 # open followups, optionally filtered
```

### Actors

```
atm actor list
atm actor show --id <ACTOR>
```
Actors are registered lazily; there is no `actor create`. `show` returns the actor's first-seen and (optionally) a summary of claimed tasks / open followups.

## Output shapes (JSON mode, abbreviated)

`task show`:
```json
{
  "task": { "id": "ATM-0001", "project_code": "ATM", "title": "...", "status": "open",
            "labels": ["type:impl"], "links": [...], "claim": null,
            "todos": [...], "followups": [...], "discussions": [...], "history": [...],
            "created_at": "...", "updated_at": "..." }
}
```
`task show --with-context` adds:
```json
{
  "context": {
    "links_out": [...], "links_in": [...],
    "conventions": [ { "id": "ATM-0005", "title": "PR conventions", "matched_labels": ["type:bug"] } ],
    "timeline": [ { "kind": "history", "id": "h1", ... }, { "kind": "todo", "id": "t1", ... } ]
  }
}
```
`task next`:
```json
{ "task": { "id": "ATM-0001", "title": "...", "status": "open", "labels": [...], "blocked_by": [] } }
```
or `{ "task": null }` when none claimable.
`review queue`:
```json
{ "groups": [ { "claimant": "agent:claude-1", "tasks": [ { "id": "ATM-0007", "title": "..." } ] } ] }
```

## Determinism guarantees

- All array outputs are sorted by a documented stable key (task id; timeline by timestamp then by entry id; review queue by claimant then task id).
- All JSON object keys are emitted in sorted order.
- The same store + arguments produce byte-identical output (snapshot-testable).