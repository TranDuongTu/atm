# TUI Contract: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-07-02 | **Revision**: v2.0.0

The TUI is a thin client over the same store operations the CLI exposes
(FR-002). Three tabs: Tasks, Projects, Dashboard. Every CLI op is reachable
from the TUI.

## Tab 1 - Tasks

List + detail. Filters: project, status, text. Detail (`show --with-context`)
shows links out/in, label-matched conventions (ranked by matched-count desc
then ID asc), timeline, and the project guide.

| Key | Action | Store op |
|-----|--------|----------|
| j/k, down/up | move cursor | - |
| g/G | top/bottom | - |
| Enter | open detail | `store.ShowWithContext` |
| `c` | create task | `store.CreateTask` |
| `s` | set status | `store.SetStatusLabel` |
| `l` | add label | `store.TaskLabelAdd` |
| `L` | remove label | `store.TaskLabelRemove` |
| `k` (detail) | add link | `store.LinkAdd` |
| `C` | claim | `store.Claim` |
| `U` | unclaim | `store.Unclaim` |
| `t` | add todo | `store.TodoAdd` |
| `f` | add followup | `store.FollowupAdd` |
| `d` | add discussion | `store.DiscussionAdd` |
| `x` | remove task | `store.RemoveTask` |
| **`G`** | **group by axis** (namespace picker) | `store.Namespaces` + `store.GroupTasksByNamespace` |
| Esc | back / close | - |

### Group-by-axis mode (FR-021)

Pressing `G` opens a picker listing all namespaces discovered from the current
project's labels (via `store.Namespaces(code)`), plus a "none (flat list)"
option. Selecting a namespace regroups the task list: tasks are bucketed under
each value of that namespace (via `store.GroupTasksByNamespace`). Render:

```
grouped by: owner
  ATM:owner:alice        (3)
    ATM-0001  Add claim command       open
    ATM-0004  Fix locking bug         open
    ATM-0007  Write docs              done
  ATM:owner:bob          (2)
    ATM-0002  ...
  (no owner label)       (1)
    ATM-0003  ...
```

- Group headers are collapsible (`Space` toggles the focused group). Default
  expanded.
- Existing filters (project, status, text) apply BEFORE grouping.
- Selecting "none" (or pressing `G` then Esc) restores the flat list.
- A header line shows `grouped by: <namespace>` when active.
- `Enter` on a task opens detail regardless of mode.

## Tab 2 - Projects

List + detail. Detail shows facts (code, name, repo paths, guide status) and
labels grouped by namespace (FR-020).

| Key | Action | Store op |
|-----|--------|----------|
| j/k, down/up | move cursor | - |
| Enter / `e` | open detail | `store.GetProject` |
| `a` | create project | `store.CreateProject` |
| `N` | set name | `store.SetProjectName` |
| `L` | add label | `store.LabelAdd` (global registry) |
| `l` | remove label | `store.LabelRemove` (soft; toast shows `retained_usage`) |
| `R` | add repo | `store.RepoAdd` |
| `r` | remove repo | `store.RepoRemove` |
| `x` | remove project (zero-task guard) | `store.ProjectRemove` |
| `S`/`s`/`X`/`M` | guide section add/remove/move | `store.GuideSection*` |
| `g`/`m`/`d` | guide ref add/move/remove | `store.GuideRef*` |
| `F` | guide set-freshness | `store.GuideSetFreshness` |
| Esc | back / close | - |

### Create form (FR-004)

Fields: `code` (required, `^[A-Z]{3,6}$`) and `name` (required). NO labels,
type-axis, or repos fields. Calls `store.CreateProject(code, name, actor)`.
Duplicate code errors with `4 conflict` (shown inline).

### Labels pane (FR-020)

Labels are rendered grouped by namespace:

```
labels:
  kind:
    ATM:kind:convention   Convention/best-practice doc
  owner:
    ATM:owner:alice       Owned by alice
  status:
    ATM:status:done       Completed
    ATM:status:open       Open / not started
    ATM:status:review     Awaiting review
  type:
    ATM:type:bug          Bug fix
    ATM:type:impl         Implementation task
  tags:
    ATM:refactor          Free-form tag
```

- Namespaces sorted alphabetically; labels within a namespace sorted by value.
- Unnamespaced labels (`<CODE>:<tag>`) grouped under a `tags:` heading.
- `L`/`l` operate on full label names; grouping is presentational.
- There is NO `T` set-type-axis action (removed).

## Tab 3 - Dashboard

Coordinator view: claimed tasks grouped by claimant, tasks with status label
`review`, open followups, and guide coverage/freshness. Mirrors
`atm review dashboard`.

| Key | Action |
|-----|--------|
| j/k | move cursor |
| Enter | open task detail |
| `a` | approve review (set status `done`) |
| `r` | reject review (set status `open` + comment) |