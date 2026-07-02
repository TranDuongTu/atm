# Data Model: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-07-02 | **Spec revision**: v2.0.0

Full rewrite. No backward compatibility with v1.x. All on-disk records are JSON
under the machine-global store (`$ATM_HOME`, default `~/.config/atm`). Dates/
timestamps are RFC 3339 UTC (`Z`). Unknown/optional fields are omitted (not
`null`) when absent, except where a field is explicitly nullable (e.g. `due`,
`resolved_at`). Object keys are serialized in sorted order for deterministic
output.

## Store layout

```
$ATM_HOME/
  labels.json          # global label registry (all projects)
  actors.json          # global actor registry
  projects/
    <CODE>.json        # project record
    <CODE>/
      tasks/
        <ID>.json      # one file per task
    <CODE>.lock        # per-project lock file (file-level locking)
```

## Entities

### Project

Root entity owning tasks, the task counter, and the project guide. Does NOT own
a label set (labels are global); does NOT carry a type-axis field.

```json
{
  "code": "ATM",
  "name": "Agent Tasks Management",
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
    "updated_at": "2026-07-02T12:00:00Z",
    "updated_by": "human:alice"
  },
  "guide_freshness_threshold": "720h",
  "repo_paths": ["/Users/me/projects/scyllas/atm"],
  "created_at": "2026-07-02T10:00:00Z",
  "created_by": "human:alice",
  "updated_at": "2026-07-02T10:05:00Z"
}
```

Fields:
- `code` (string, required, unique, immutable, `^[A-Z]{3,6}$`): short code used
  in task IDs and as the prefix for the project's labels.
- `name` (string, required): human display name.
- `next_task_n` (int, required, >= 1): next integer to assign as a task number;
  incremented under the project lock on task creation.
- `guide` (Guide, optional): the project's always-read agent-context harness
  (FR-016). Omitted until first edit; an empty/missing guide is treated as no
  guide.
- `guide_freshness_threshold` (duration string, optional, e.g. `"720h"`): if
  set, guide refs whose referenced task's `updated_at` is older than
  `now - threshold` are flagged stale on the dashboard (FR-018). If omitted,
  freshness is reported as `unknown` (not stale).
- `repo_paths` (array of strings, optional, may be empty): repo filesystem paths
  associated with the project (a project may span multiple repos or none;
  FR-001). Informational only; ATM does not read/write these paths.
- `created_at`, `updated_at` (RFC 3339, required).
- `created_by` (Actor id, required).

Validation:
- `code` is unique across all projects and immutable after creation.
- `guide_freshness_threshold`, if present, must parse as a Go `time.Duration`
  string and be positive.

### Label (global registry)

A single global registry at `$ATM_HOME/labels.json` holds all labels across all
projects. A label name is hierarchical and project-prefixed:
`<CODE>:<namespace>:<value>` (e.g. `ATM:type:bug`) or `<CODE>:<tag>` for
free-form tags (e.g. `ATM:refactor`). The namespace segment is optional and
open — any namespace the user invents is valid; there is no whitelist and no
pre-declared axis list.

```json
{
  "labels": [
    {"name": "ATM:kind:convention", "description": "Convention/best-practice doc"},
    {"name": "ATM:owner:alice", "description": "Owned by alice"},
    {"name": "ATM:status:cancelled", "description": "Cancelled"},
    {"name": "ATM:status:done", "description": "Completed"},
    {"name": "ATM:status:in-progress", "description": "Work in progress"},
    {"name": "ATM:status:open", "description": "Open / not started"},
    {"name": "ATM:status:review", "description": "Awaiting review"},
    {"name": "ATM:type:bug", "description": "Bug fix"},
    {"name": "ATM:type:epic", "description": "Large body of work"},
    {"name": "ATM:type:impl", "description": "Implementation task"},
    {"name": "ATM:doc:architecture", "description": "Architecture notes"}
  ]
}
```

Fields:
- `name` (string, required, unique): `<CODE>:<namespace>:<value>` or
  `<CODE>:<tag>`. The `<CODE>` prefix MUST match an existing project's code.
- `description` (string, optional): human-readable description.

Validation:
- Label names are unique within the global registry.
- The `<CODE>` prefix MUST match `^[A-Z]{3,6}$` and reference an existing
  project at add time.
- A label may be removed from the registry (soft removal); existing tasks retain
  it but new assignments reject it. The removal response reports
  `retained_usage` (count of tasks still carrying the label).
- There is no namespace whitelist. Any namespace token (the segment between the
  first and second `:`) is valid. A namespace "exists" iff at least one label
  with that prefix is in the registry.

Status labels: the `status` namespace is conventional (not enforced as special
at the registry level) but the system reads it for next-task eligibility,
blocking, claim, and review logic (FR-005/FR-006/FR-007/FR-010). The set of
recognized status values is: `open`, `in-progress`, `done`, `cancelled`,
`review`. (Free transitions are allowed; there is no state machine — FR-005.)

Note: v1's `blocked` status value is REMOVED in v2. A task is "blocked" by
virtue of having an active `blocked-by` link (a `blocks` edge from a task whose
status label is not terminal), not by carrying a `blocked` status label.
`blocked` is a derived state, not a status value.

### Task

The unit of work. One JSON file per task at
`$ATM_HOME/projects/<CODE>/tasks/<ID>.json`.

```json
{
  "id": "ATM-0001",
  "project_code": "ATM",
  "title": "Add claim command",
  "description": "Implement `atm task claim` with atomic locking.",
  "labels": ["ATM:status:open", "ATM:type:impl", "ATM:area:cli"],
  "links": [
    {"type": "blocks", "target": "ATM-0002"},
    {"type": "implements", "target": "ATM-0010"}
  ],
  "claim": {"actor": "agent:claude-1", "at": "2026-07-02T11:00:00Z"},
  "todos": [
    {"id": "t1", "text": "Write tests for claim", "done": false, "author": "agent:claude-1", "at": "2026-07-02T11:01:00Z"}
  ],
  "followups": [
    {"id": "f1", "text": "Decide on storage format", "assignee": "human:alice", "status": "open", "due": null, "author": "human:alice", "at": "2026-07-02T11:02:00Z", "resolved_at": null, "resolved_by": null}
  ],
  "discussions": [
    {"id": "d1", "text": "Use file-level locking.", "author": "human:alice", "at": "2026-07-02T11:03:00Z"}
  ],
  "history": [
    {"id": "h1", "action": "created", "actor": "human:alice", "at": "2026-07-02T10:30:00Z", "meta": {}},
    {"id": "h2", "action": "claimed", "actor": "agent:claude-1", "at": "2026-07-02T11:00:00Z", "meta": {}}
  ],
  "created_at": "2026-07-02T10:30:00Z",
  "updated_at": "2026-07-02T11:00:00Z"
}
```

Fields:
- `id` (string, required, unique): `<CODE>-<NNNN>` with a per-project sequential
  counter that widens as needed (minimum 4 digits).
- `project_code` (string, required): the owning project's code.
- `title` (string, required).
- `description` (string, optional).
- `labels` (array of string, required, may be empty): full label names from the
  global registry. New tasks are auto-labeled `<CODE>:status:open` at creation.
  There is NO dedicated `status` field — status is read from the
  `<CODE>:status:<value>` label.
- `links` (array of Link, required, may be empty).
- `claim` (Claim, optional): present iff the task is currently claimed.
- `todos` (array of Todo, required, may be empty).
- `followups` (array of Followup, required, may be empty).
- `discussions` (array of DiscussionEntry, required, may be empty).
- `history` (array of HistoryEntry, required): append-only.
- `created_at`, `updated_at` (RFC 3339, required).

Validation:
- `labels` MUST reference names present in the global registry at assignment
  time (existing tasks retain removed labels — soft removal).
- A task SHOULD carry exactly one `<CODE>:status:*` label. If it carries zero,
  it is treated as `open` for next-task eligibility with a warning surfaced on
  `show`. If it carries more than one, the lexicographically smallest wins for
  status-based logic, with a warning surfaced on `show`.
- Status transitions are unconstrained (no state machine — FR-005). Any status
  label may replace any other freely.

### Link, Claim, Todo, Followup, DiscussionEntry, HistoryEntry, Actor

Unchanged from v1 semantics (see spec.md Key Entities). Followup `status` is
the followup's own open/resolved state, unrelated to the task's status label.

### Guide

Unchanged from v1: ordered list of references grouped by named section. Edited
by the human coordinator. Surfaced on the dashboard for coverage/freshness.

## Status-as-label semantics

The system reads the task's `<CODE>:status:*` label wherever v1 read
`Task.Status`:

- **Next task eligibility**: a task is eligible iff its status label is not
  `done` and not `cancelled` (terminal states), it is unclaimed, it is not a
  convention doc (`<CODE>:kind:convention`), and its blocked-by count is zero.
- **Blocking**: a `blocks` link from a task is considered "active" (blocks the
  target) iff the blocking task's status label is not terminal (`done`/
  `cancelled`).
- **Claim**: claiming is allowed iff the task's status label is not terminal.
- **Review**: the review queue is tasks whose status label is `review`.
  Approving sets the status label to `done`; rejecting sets it to `open` and
  records the rejection comment as a discussion entry.

There is no `allowedTransitions` table. `SetStatusLabel(code, id, newStatus,
actor)` replaces the prior `SetStatus` and simply removes any existing
`<CODE>:status:*` label(s) from the task and adds `<CODE>:status:<newStatus>`,
validating that `<CODE>:status:<newStatus>` exists in the global registry.

## Namespace discovery and grouping

- `Store.Namespaces(code string) []string`: returns the distinct namespaces
  (the segment between the first and second `:`) found among labels in the
  global registry whose prefix is `<CODE>:`, sorted alphabetically. Labels with
  no namespace (`<CODE>:<tag>`) are excluded. Used by the TUI Projects tab
  grouping and the Tasks tab `G` picker.
- `Store.GroupTasksByNamespace(code, namespace string) map[string][]*Task`:
  groups the project's tasks under each full label name of the form
  `<CODE>:<namespace>:<value>` that appears on at least one task. Tasks with no
  matching label go into the sentinel key `""`. Used by the TUI Tasks tab
  "group by axis" mode.

## Convention matching

`ShowWithContext` collects convention-doc tasks (those labeled
`<CODE>:kind:convention`) whose labels intersect the task's labels, ordered by
matched-label-count desc then ID asc. There is no type-axis priority. Pure
label-intersection ranking.