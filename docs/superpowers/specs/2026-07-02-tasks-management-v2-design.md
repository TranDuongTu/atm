# Tasks Management System v2 — Design Spec

**Status:** Proposed (full rewrite; no backward compatibility with v1.x)
**Driver:** v1 coupled project creation to label/axis curation and special-cased
a single `type_axis` for one feature. The user wants creation to be deadly
simple and labels to be the single dynamic organizing substrate — global,
hierarchical, project-prefixed, with any namespace becoming a management axis
on demand.

## What changes and why

### 1. Project creation is minimal

`atm project create` takes only `--code` (`^[A-Z]{3,6}$`, 3-6 uppercase letters)
and `--name`. No `--type-axis`, `--label`, `--repo-path` at create time. Labels
and repos are added later via dedicated commands.

v1's create surface coupled creation to label curation and created validation
ordering constraints (axis namespace must have >=1 label before designation).
Decoupling lets the human create the empty container first and curate labels as
the project's needs emerge.

The code regex tightens from `^[A-Z][A-Z0-9-]{1,15}$` (2-16 chars, digits/
hyphens) to `^[A-Z]{3,6}$` (3-6 uppercase letters only). Short, uniform,
human-friendly, unambiguous in agent logs and commit messages.

### 2. type_axis removed entirely

The v1 `Project.TypeAxis` field, `SetTypeAxis`, the `set-type-axis` CLI/TUI
commands, and `typeAxisScore` are all removed. There is no designated axis.
Convention-doc matching (`ShowWithContext`) orders by matched-label-count desc
then ID asc.

The type-axis was a single special-cased namespace used by exactly one feature
(convention-match priority). It added validation coupling and conceptual
surface for marginal benefit. Removing it makes ALL namespaces equal — any
namespace becomes a grouping axis on demand, which is a richer model than
designating one axis up front.

### 3. Labels are global, hierarchical, project-prefixed

A single global registry at `$ATM_HOME/labels.json` holds all labels across all
projects. A label name is `<CODE>:<namespace>:<value>` (e.g. `ATM:type:bug`) or
`<CODE>:<tag>` for free-form tags. The namespace segment is optional and open —
any namespace the user invents is valid; there is no whitelist and no
pre-declared axis list. A namespace "exists" iff at least one label with that
prefix is in the registry.

This is the core of the user's vision: labels are the dynamic substrate.
Categorization (`type`), ownership (`owner`), release tracking (`release`),
documentation grouping (`doc`), and any future concern are all expressed
through one mechanism. Any namespace becomes a management axis on demand, with
no dedicated per-axis screens. Agents can both query by label metadata and
create labels (`agent:claude`, `doc:architecture`) to self-organize.

Soft removal is preserved: removing a label from the registry stops new
assignments but retains it on existing tasks; the removal response reports
`retained_usage`.

### 4. Status is a label, not a field; no state machine

`Task.Status` (the v1 dedicated string field) is removed. Status is expressed
as a label on the project's `status` namespace (`<CODE>:status:<state>`). The
system reads the status label wherever v1 read `Task.Status`: next-task
eligibility, blocking, claim, review. New tasks are auto-labeled
`<CODE>:status:open`.

There is NO state machine — any status label may replace any other freely
(FR-005). v1's `allowedTransitions` table is removed. v1's `blocked` status
value is also removed: "blocked" is a derived state from an active
`blocked-by` link (a `blocks` edge from a task whose status label is not
terminal), not a status value.

Recognized status values: `open`, `in-progress`, `done`, `cancelled`, `review`.
Terminal (exclude from next-task, stop active blocks): `done`, `cancelled`.
A task with zero status labels is treated as `open` (with a warning on `show`);
a task with multiple is disambiguated by lexicographic order (with a warning).

Treating status as just another label axis unifies the model: status is
categorization, like type or owner. The state machine prevented some
transitions (e.g. done->blocked) but added validation surface and friction the
user explicitly chose to drop.

### 5. TUI group-by-axis mode

The TUI Tasks tab gains a `G` key that opens a picker of namespaces discovered
from the current project's labels (`store.Namespaces(code)`). Selecting a
namespace regroups the task list under each value
(`store.GroupTasksByNamespace`). Existing filters apply before grouping. A
sentinel group holds tasks with no label in the selected namespace. Selecting
"none" restores the flat list. Group headers are collapsible.

The TUI Projects tab labels pane renders labels grouped by namespace (sorted
alphabetically; unnamespaced tags under a `tags:` heading). Add/remove/
describe actions operate on full label names; grouping is presentational. The
v1 `[T] set type-axis` action is removed.

This realizes the "any namespace is a management axis on demand" vision without
dedicated per-axis screens or a pre-declared axis list. The user invents
namespaces by adding labels; the TUI discovers them.

## CLI surface (summary)

- `project create --code <CODE> --name <NAME>` — minimal.
- `label add/remove/list/show` — global registry; `list` takes optional
  `--project` and `--namespace` filters.
- `task create/list/show/set-status/label/link/claim/unclaim/remove` —
  `list` takes optional `--group-by <NS>` mirroring the TUI `G` mode.
  `set-status` replaces the `<CODE>:status:*` label(s); no state machine.
- `next --project <CODE> [--claim]` — terminal statuses `done`/`cancelled`.
- `review request/approve/reject/queue/followups/dashboard` — `request` sets
  status label `review`; `approve` sets `done`; `reject` sets `open` + comment
  as discussion.
- Entry commands (`todo`, `followup`, `discussion`, `timeline`) unchanged.
- Project guide commands unchanged.

## Data model (summary)

- `$ATM_HOME/labels.json` — global registry: `{labels: [{name, description}]}`.
  Name = `<CODE>:<namespace>:<value>` or `<CODE>:<tag>`.
- `$ATM_HOME/actors.json` — unchanged.
- `$ATM_HOME/projects/<CODE>.json` — code, name, next_task_n, guide,
  guide_freshness_threshold, repo_paths, history, timestamps. NO label set,
  NO type_axis.
- `$ATM_HOME/projects/<CODE>/tasks/<ID>.json` — id, project_code, title,
  description, labels (full names), links, claim, todos, followups,
  discussions, history, timestamps. NO status field.

New store helpers: `Namespaces(code) []string` (distinct namespaces among
`<CODE>:`-prefixed labels), `GroupTasksByNamespace(code, ns) map[string][]*Task`.

## No backward compatibility

v1.x data is not migrated. The v2 store starts fresh. Existing code that reads
`Task.Status` or `Project.TypeAxis` is rewritten, not shimmed. This is a full
rewrite of the store, CLI, and TUI layers.

## Out of scope

- A future spec may add agent-side label-creation behavior and richer
  cross-project label queries. This spec defines the registry and the
  group-by-axis TUI mode; it does not build per-axis customization or dedicated
  management screens beyond the generic group-by-namespace view.