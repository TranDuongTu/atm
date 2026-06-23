# Data Model: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-06-23 | **Spec revision**: v1.1.0

All on-disk records are JSON under the machine-global store (`$ATM_HOME`, default `~/.config/atm`). Dates/timestamps are RFC 3339 UTC (`Z`). Unknown/optional fields are omitted (not `null`) when absent, except where a field is explicitly nullable (e.g. `due`, `resolved_at`). Object keys are serialized in sorted order for deterministic output (R8).

## Entities

### Project

Root entity owning tasks, labels, the task counter, and the project guide.

```json
{
  "code": "ATM",
  "name": "Agent Tasks Management",
  "type_axis": "type",
  "labels": [
    {"name": "type:epic", "description": "Large body of work"},
    {"name": "type:user-story", "description": "User-facing story"},
    {"name": "type:impl", "description": "Implementation task"},
    {"name": "type:bug", "description": "Bug fix"},
    {"name": "area:cli", "description": "CLI surface"},
    {"name": "area:tui", "description": "TUI surface"},
    {"name": "kind:convention", "description": "Convention/best-practice doc"}
  ],
  "next_task_n": 13,
  "guide": {
    "sections": [
      {"name": "conventions", "refs": [
        {"kind": "task", "target": "ATM-0005"},
        {"kind": "file", "target": "/abs/path/to/CONVENTIONS.md"}
      ]},
      {"name": "testing", "refs": [
        {"kind": "task", "target": "ATM-0012"}
      ]}
    ],
    "updated_at": "2026-06-23T12:00:00Z",
    "updated_by": "human:alice"
  },
  "guide_freshness_threshold": "720h",
  "repo_paths": ["/Users/me/projects/scyllas/atm"],
  "created_at": "2026-06-23T10:00:00Z",
  "created_by": "human:alice",
  "updated_at": "2026-06-23T10:05:00Z"
}
```

Fields:
- `code` (string, required, unique, immutable, `^[A-Z][A-Z0-9-]{1,15}$`): short code used in task IDs.
- `name` (string, required): human display name.
- `type_axis` (string, optional): namespace designated as the task-type axis (e.g. `type`). Omitted until declared.
- `labels` (array of Label, required, may be empty): allowed label set for the project.
- `next_task_n` (int, required, >= 1): next integer to assign as a task number; incremented under the project lock on task creation.
- `guide` (Guide, optional): the project's always-read agent-context harness (FR-016). Omitted until first edit; an empty/missing guide is treated as no guide.
- `guide_freshness_threshold` (duration string, optional, e.g. `"720h"`): if set, guide refs whose referenced task's `updated_at` is older than `now - threshold` are flagged stale on the dashboard (FR-018). If omitted, freshness is reported as `unknown` (not stale).
- `repo_paths` (array of strings, optional, may be empty): repo filesystem paths associated with the project (a project may span multiple repos or none; FR-001). Informational only; ATM does not read/write these paths.
- `created_at`, `updated_at` (RFC 3339, required).
- `created_by` (Actor id, required).

Validation:
- `code` is unique across all projects and immutable after creation.
- A label may be removed from `labels` (soft removal); existing tasks retain it but new assignments reject it.
- Setting `type_axis` requires the namespace to have at least one label in `labels`.
- `guide_freshness_threshold`, if present, must parse as a Go `time.Duration` string and be positive.

### Task

The unit of work. One JSON file per task at `$ATM_HOME/projects/<CODE>/tasks/<ID>.json`.

```json
{
  "id": "ATM-0001",
  "project_code": "ATM",
  "title": "Add claim command",
  "description": "Implement `atm task claim` with atomic locking.",
  "status": "open",
  "labels": ["type:impl", "area:cli"],
  "links": [
    {"type": "blocks", "target": "ATM-0002"},
    {"type": "implements", "target": "ATM-0010"}
  ],
  "claim": {"actor": "agent:claude-1", "at": "2026-06-23T11:00:00Z"},
  "todos": [
    {"id": "t1", "text": "Write tests for claim", "done": false, "author": "agent:claude-1", "at": "2026-06-23T11:01:00Z"}
  ],
  "followups": [
    {"id": "f1", "text": "Decide on storage format", "assignee": "human:alice", "status": "open", "due": null, "author": "human:alice", "at": "2026-06-23T11:02:00Z", "resolved_at": null, "resolved_by": null}
  ],
  "discussions": [
    {"id": "d1", "text": "Use file-level locking.", "author": "human:alice", "at": "2026-06-23T11:03:00Z"}
  ],
  "history": [
    {"id": "h1", "action": "created", "actor": "human:alice", "at": "2026-06-23T10:30:00Z", "meta": {}},
    {"id": "h2", "action": "claimed", "actor": "agent:claude-1", "at": "2026-06-23T11:00:00Z", "meta": {}}
  ],
  "created_at": "2026-06-23T10:30:00Z",
  "updated_at": "2026-06-23T11:00:00Z"
}
```

Fields:
- `id` (string, required, immutable): `<CODE>-<N>`, N is `next_task_n` at creation, rendered with at least 4 digits up to 9999 then natural width.
- `project_code` (string, required, immutable): the owning project's code.
- `title` (string, required, non-empty).
- `description` (string, optional, default `""`): may be markdown.
- `status` (enum, required): `open` | `in-progress` | `blocked` | `review` | `done` | `cancelled`. Default `open` on creation.
- `labels` (array of strings, default `[]`): each must be in the project's `labels` set at assignment time (soft-removal allows existing labels to persist).
- `links` (array of Link, default `[]`).
- `claim` (object, optional): present only while claimed. `{"actor": <Actor id>, "at": <RFC 3339>}`. Removed on unclaim.
- `todos` (array of Todo, default `[]`).
- `followups` (array of Followup, default `[]`).
- `discussions` (array of DiscussionEntry, default `[]`).
- `history` (array of HistoryEntry, required, append-only): every mutation appends one entry.
- `created_at`, `updated_at` (RFC 3339, required).

Status transitions (allowed, anything not listed is rejected with an error):
- `open` -> `in-progress`, `blocked`, `cancelled`, `review`
- `in-progress` -> `review`, `done`, `open`
- `blocked` -> `open`, `in-progress`, `cancelled`
- `review` -> `done` (approve), `in-progress` (reject), `open` (reject)
- `done` -> `open` (reopen only)
- `cancelled` -> `open` (reopen only)

### Label

A project-scoped tag. Either a free-form string or `namespace:value` (colon-separated, namespace and value are `[a-z0-9][a-z0-9-]*`). Stored inside the project's `labels` array as `{"name": <label>, "description": <string, optional>}`. The `description` is optional human-readable text.

### Link

A typed directed edge from the current task to a `target` task.

```json
{"type": "blocks", "target": "ATM-0002"}
```

- `type` (enum): `blocks` | `related-to` | `implements` | `documents`.
- `target` (string, required): another task id (same project for v1; cross-project links are a non-goal for v1).

Semantics:
- `blocks`: this task blocks `target`. `target` is excluded from "next task" while this task is not `done`. Implied reverse edge `blocked-by` is computed at query time, not stored.
- `related-to`: symmetric; traversed both ways.
- `implements`: this task implements `target` (e.g. `target` is an epic).
- `documents`: this task documents `target` (e.g. this task is a convention doc for `target`).

A `blocks`/`implements`/`documents` edge where `target` does not exist is preserved but reported as a warning (stale link edge case). `related-to` is deduplicated: if A->B `related-to` exists, adding B->A `related-to` is a no-op (already related).

### Actor

Identified by a string `<kind>:<id>` where kind is `agent` or `human`. Stored lazily in `$ATM_HOME/actors.json`:

```json
{
  "actors": [
    {"id": "human:alice", "kind": "human", "name": "Alice", "first_seen": "2026-06-23T10:00:00Z"},
    {"id": "agent:claude-1", "kind": "agent", "name": "Claude 1", "first_seen": "2026-06-23T11:00:00Z"}
  ]
}
```

The `name` is optional and only set if provided via `--actor-name`. The actor registry is informational provenance; it is not consulted for authn (local-trust).

### Todo

```json
{"id": "t1", "text": "Write tests for claim", "done": false, "author": "agent:claude-1", "at": "2026-06-23T11:01:00Z"}
```

- `id` (string): unique within the task; `<prefix><n>` where prefix is `t` and n is a per-task counter.
- `text` (string, required, non-empty).
- `done` (bool, default false).
- `author` (Actor id, required), `at` (RFC 3339, required).

### Followup

```json
{"id": "f1", "text": "Decide on storage format", "assignee": "human:alice", "status": "open", "due": "2026-06-30T00:00:00Z", "author": "human:alice", "at": "2026-06-23T11:02:00Z", "resolved_at": null, "resolved_by": null}
```

- `id` (string): unique within the task; `f<n>`.
- `text` (string, required).
- `assignee` (Actor id, optional): defaults to the author if omitted.
- `status` (enum): `open` | `resolved`. Default `open`.
- `due` (RFC 3339, nullable): omitted when null.
- `author` (Actor id, required), `at` (RFC 3339, required).
- `resolved_at` (RFC 3339, nullable), `resolved_by` (Actor id, nullable): set when status -> `resolved`.

### DiscussionEntry

```json
{"id": "d1", "text": "Use file-level locking.", "author": "human:alice", "at": "2026-06-23T11:03:00Z"}
```

- `id` (string): `d<n>`.
- `text` (string, required): may be markdown.
- `author` (Actor id, required), `at` (RFC 3339, required).
- Flat (no threading) for v1.

### HistoryEntry

Append-only record of a task mutation.

```json
{"id": "h1", "action": "created", "actor": "human:alice", "at": "2026-06-23T10:30:00Z", "meta": {}}
```

- `id` (string): `h<n>`.
- `action` (string): one of `created`, `status-changed`, `label-added`, `label-removed`, `link-added`, `link-removed`, `claimed`, `unclaimed`, `todo-added`, `todo-toggled`, `followup-added`, `followup-resolved`, `discussion-added`, `review-requested`, `approved`, `rejected`, `title-changed`, `description-changed`, `guide-updated`.
- `actor` (Actor id, required), `at` (RFC 3339, required).
- `meta` (object, optional): free-form structured detail (e.g. `{"from": "open", "to": "in-progress"}` for status changes).

### Guide *(new in v1.1.0)*

A project-level entity: the always-read agent-context harness (FR-016). Stored as the `guide` field on the Project record, so it is read/written under the project lock.

```json
{
  "sections": [
    {"name": "conventions", "refs": [
      {"kind": "task", "target": "ATM-0005"},
      {"kind": "file", "target": "/abs/path/to/CONVENTIONS.md"}
    ]},
    {"name": "testing", "refs": [
      {"kind": "task", "target": "ATM-0012"}
    ]}
  ],
  "updated_at": "2026-06-23T12:00:00Z",
  "updated_by": "human:alice"
}
```

Fields:
- `sections` (array of GuideSection, required, may be empty): ordered list of named sections.
- `updated_at` (RFC 3339, required): last edit timestamp.
- `updated_by` (Actor id, required): last editor.

GuideSection:
- `name` (string, required, non-empty): section name (e.g. `conventions`, `testing`).
- `refs` (array of GuideRef, required, may be empty): ordered references in the section.

GuideRef:
- `kind` (enum, required): `task` (a convention-doc task in the same project) or `file` (an external file path).
- `target` (string, required): a task id (for `kind:task`) or an absolute filesystem path (for `kind:file`). Relative paths are not normalized; store absolute paths.

Validation / invariants:
- Section names are unique within a guide (case-sensitive).
- For `kind:task`, `target` must be a valid task id in the same project; a ref whose target task is deleted/missing is preserved but flagged `stale|missing` on the dashboard (FR-018) — the ref is NOT auto-removed (the human cleans it up, matching the stale-link edge case in the spec).
- For `kind:file`, existence is checked on demand for the dashboard; a missing file is flagged but the ref is preserved.
- Editing the guide (add/remove/reorder refs, add/rename/remove sections) sets `updated_at`/`updated_by` and appends a `guide-updated` history entry to the project record (project-level history, not a task history entry).

## Relationships

- Project 1..N Task. A task belongs to exactly one project.
- Project 1..1 Guide (optional; stored on the project record).
- Task N..N Label (via the project's label set; stored as string array on the task).
- Task N..N Task (via Link; self-referential, same project only for v1).
- Task 1..N Todo/Followup/DiscussionEntry/HistoryEntry (embedded in the task record).

## Store invariants (enforced under the project lock)

1. `task.id` equals `<project_code>-<rendered next_task_n at creation>`; `next_task_n` increments on creation.
2. A label assigned to a task must be in the project's `labels` set *at assignment time*; removed labels persist on existing tasks but cannot be newly assigned.
3. A `blocks` edge from a non-done task excludes the `target` from "next task".
4. A task may have at most one claim; an actor may claim multiple tasks.
5. Status transitions must follow the allowed matrix; violations error out.
6. History (task and project) is append-only; no entry is ever removed or mutated.
7. IDs (task, todo, followup, discussion, history) are never reused, even after the owning entity is removed.
8. Guide section names are unique within the guide; guide refs are preserved even when their target is deleted/missing (flagged, not removed).