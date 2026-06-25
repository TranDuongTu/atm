# CLI Contract: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-06-23 | **Spec revision**: v1.1.0

The CLI (`atm`) is the stable, versioned API surface for agents (out-of-process) and the front-end the TUI wraps. Every operation is a subcommand. Output format is selected globally with `--output json|text` (default `text`). JSON output is deterministic: object keys sorted, stable whitespace, RFC 3339 UTC timestamps. All mutating commands accept `--actor <id>` (or read `ATM_ACTOR`). Errors go to stderr with a non-zero exit code and a stable JSON `{ "error": { "code": "...", "message": "..." } }` shape in JSON mode.

Global flags:
- `--store <path>`: path to the store directory (overrides `ATM_HOME`). Default: `~/.config/atm`.
- `--output json|text`: output format. Default `text`.
- `--actor <id>`: actor performing the operation (e.g. `agent:claude-1`, `human:alice`). Env: `ATM_ACTOR`. Required for mutating commands; optional for read commands.
- `--quiet`: suppress non-essential stdout in text mode.

Store resolution: `--store` flag > `ATM_HOME` env var > `~/.config/atm` default. There is no walk-up-from-CWD search (the store is machine-global; a project is not 1:1 with a repo).

Exit codes: `0` success; `1` generic error; `2` usage error; `3` not-found (e.g. task/project missing); `4` conflict (e.g. already claimed, invalid status transition, duplicate project code).

## Command reference

### Store / init

```
atm init [--store <path>] [--actor <id>]
```
Creates an empty store at the resolved path (default `~/.config/atm`). Idempotent: re-running on an existing store is a no-op. Initializes `actors.json` and `projects/`.

```
atm store path
```
Prints the resolved store path. (Read-only; useful for agents to confirm location.)

### Projects

```
atm project create --code <CODE> --name <NAME> [--type-axis <NS>] [--label <L>]... [--repo-path <PATH>]... [--actor <id>]
atm project list
atm project show --code <CODE>
atm project set-type-axis --code <CODE> --namespace <NS> [--actor <id>]
atm project set-name --code <CODE> --name <NAME> [--actor <id>]
atm project repo add --code <CODE> --path <PATH> [--actor <id>]
atm project repo remove --code <CODE> --path <PATH> [--actor <id>]
atm project label add --code <CODE> --label <L> [--description <DESC>] [--actor <id>]
atm project label remove --code <CODE> --label <L> [--actor <id>]
atm project label list --code <CODE>
```
- `create`: creates a project. `--code` must match `^[A-Z][A-Z0-9-]{1,15}$` and be unique. `--label` may be repeated to seed the label set. `--type-axis` optionally declares the type namespace up front. `--repo-path` may be repeated to associate repo paths (informational; a project may span multiple repos or none).
- `label remove`: soft removal. Warns (in text mode) or includes `retained_usage` (in JSON mode) when existing tasks still use the label.

### Project guide *(new in v1.1.0)*

```
atm project guide show --code <CODE>
atm project guide section add    --code <CODE> --name <NAME> [--actor <id>]
atm project guide section rename --code <CODE> --name <NAME> --new-name <NEW> [--actor <id>]
atm project guide section remove --code <CODE> --name <NAME> [--actor <id>]
atm project guide section move    --code <CODE> --name <NAME> --before <OTHER|>   # reorder; --before "" = move to end
atm project guide ref add    --code <CODE> --section <NAME> --kind task|file --target <T> [--actor <id>]
atm project guide ref remove --code <CODE> --section <NAME> --kind task|file --target <T> [--actor <id>]
atm project guide ref move    --code <CODE> --section <NAME> --kind task|file --target <T> --before <OTHER_REF_TARGET|>  # reorder within section
atm project guide set-freshness --code <CODE> --threshold <DURATION|unset> [--actor <id>]
atm project guide status --code <CODE>     # coverage (empty sections) + freshness (stale/missing refs)
```
- `guide show`: returns the whole guide (sections + refs + `updated_at`/`updated_by`) or `{ "guide": null }` if none.
- `section add`: appends a section with the given name (names unique within the guide). `section rename` changes a name; `section remove` drops the section and its refs; `section move` reorders sections.
- `ref add`: appends a ref to a section. `kind task` validates `target` is an existing task id in the same project; `kind file` accepts an absolute path (no existence check at add time; existence is checked on demand by `guide status`). `ref remove` drops a single ref by `(kind,target)`. `ref move` reorders refs within a section.
- `set-freshness`: sets `guide_freshness_threshold` (a Go `time.Duration` string, e.g. `720h`) or unsets it with `unset`.
- `guide status`: returns coverage (sections with zero refs) and, for each `kind:task` ref, a freshness state (`fresh`, `stale`, `missing`, or `unknown` when the threshold is unset); for `kind:file` refs, an existence state (`present`/`missing`). A `kind:task` ref whose task is deleted is `missing` (the ref is preserved, not auto-removed).
- All guide edits set `guide.updated_at`/`updated_by` and append a `guide-updated` entry to the project history.

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
- `show --with-context`: includes the project guide (FR-017), linked tasks (both directions), matching convention docs (label intersection, weighted by type axis), and the todo/followup/discussion timeline.
- `list`: filter by project, labels (AND-intersection), status, assignee (followup assignee), claimant. Sorted by id (project-then-numeric).

### Agent workflow (claim / next)

```
atm task next --project <CODE> [--actor <id>] [--claim]
atm task claim --id <ID> [--actor <id>]
atm task unclaim --id <ID> [--actor <id>]
```
- `next`: returns the highest-priority claimable, non-blocked, non-claimed, non-done task in `<CODE>`. Priority for v1 = a stable ordering: blocked-by count ascending, then created_at ascending (oldest first). `--claim` atomically claims the returned task under the project lock. Returns an empty result (not an error) if none claimable. The response always includes the project guide (FR-017) so the agent receives the always-read harness in the same call.
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
atm review dashboard [--project <CODE>]                 # queue + open followups + guide status (FR-010/FR-018)
```
- `dashboard`: the coordinator's single view: review queue (grouped by claimant), open followups, and guide coverage/freshness for the project. The TUI coordinator view renders this.

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
    "guide": { "sections": [ {"name": "conventions", "refs": [...]} ], "updated_at": "...", "updated_by": "..." },
    "links_out": [...], "links_in": [...],
    "conventions": [ { "id": "ATM-0005", "title": "PR conventions", "matched_labels": ["type:bug"] } ],
    "timeline": [ { "kind": "history", "id": "h1", ... }, { "kind": "todo", "id": "t1", ... } ]
  }
}
```
`task next`:
```json
{ "task": { "id": "ATM-0001", "title": "...", "status": "open", "labels": [...], "blocked_by": [] },
  "guide": { "sections": [...] } }
```
or `{ "task": null, "guide": {...} }` when none claimable (the guide is still returned so an agent idling still sees the harness).
`review queue`:
```json
{ "groups": [ { "claimant": "agent:claude-1", "tasks": [ { "id": "ATM-0007", "title": "..." } ] } ] }
```
`project guide status`:
```json
{
  "coverage": { "empty_sections": ["testing"], "total_sections": 3, "total_refs": 5 },
  "freshness": [
    { "section": "conventions", "kind": "task", "target": "ATM-0005", "state": "stale", "updated_at": "2026-05-01T00:00:00Z" },
    { "section": "conventions", "kind": "file", "target": "/abs/path", "state": "missing" }
  ]
}
```
`review dashboard`:
```json
{
  "project": "ATM",
  "review_queue": { "groups": [...] },
  "open_followups": [ { "id": "ATM-0002", "followup": "f1", "text": "...", "assignee": "human:alice" } ],
  "guide_status": { "coverage": {...}, "freshness": [...] }
}
```

## Determinism guarantees

- All array outputs are sorted by a documented stable key (task id; timeline by timestamp then by entry id; review queue by claimant then task id; guide sections/refs by their stored order — the guide is explicitly ordered, not sorted, so its order is part of the data).
- All JSON object keys are emitted in sorted order.
- The same store + arguments produce byte-identical output (snapshot-testable).