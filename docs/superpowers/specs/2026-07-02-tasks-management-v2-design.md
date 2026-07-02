# Tasks Management System v2 — Design Spec

**Status:** Approved (full rewrite; no backward compatibility with v1.x)
**Approach:** Wholesale delete-and-rebuild (Approach A).

## Driver

v1 coupled project creation to label/axis curation and special-cased a single
`type_axis` for one feature. Worse, v1 carried a growing set of *intrinsic
workflow knowledge*: a hardcoded status field with a state machine, a Claim
primitive, review/approve/reject flows, followups with their own status +
assignee, todos, discussions, convention matching keyed off a `kind:convention`
label, a per-project guide subsystem, and a managed Actor entity. Each was a
special case the system "knew about."

v2's vision is the opposite: **labels are the single dynamic organizing
substrate; the system has no intrinsic knowledge of any namespace.** Status,
owner, type, claim, convention-ness — all of it is gone as a first-class
concept. The system only knows about two entities (Project, Task) and one
substrate (Labels). Everything else a human or agent wants to express —
categorization, ownership, release tracking, "I'm working on this," a task's
role as a convention for another — is expressed by assigning labels and
filtering/grouping on them. Workflow lives outside the system, in agent prompts
and human habits, not in the store.

## What changes and why

### 1. Project creation is minimal

`atm project create` takes only `--code` (`^[A-Z]{3,6}$`, 3-6 uppercase letters)
and `--name`. No `--type-axis`, `--label`, `--repo-path` at create time.

v1's create surface coupled creation to label curation and created validation
ordering constraints (axis namespace must have >=1 label before designation).
Decoupling lets the human create the empty container first and curate labels as
the project's needs emerge.

The code regex tightens from `^[A-Z][A-Z0-9-]{1,15}$` (2-16 chars, digits/
hyphens) to `^[A-Z]{3,6}$` (3-6 uppercase letters only). Short, uniform,
human-friendly, unambiguous in agent logs and commit messages.

### 2. Project is a namespace owner, nothing more

The Project entity owns its `<CODE>:` label namespace — `<CODE>:*` labels can
only be created because the project exists — and holds the task ID counter
(`NextTaskN`). It carries no label set of its own (the project's labels are
derived from the global registry filtered by `<CODE>:` prefix), no guide, no
repo paths, no type axis. A project is the scope for per-project file locking
and ID generation; everything organizational is labels.

### 3. Labels are the single dynamic substrate

A single global registry at `$ATM_HOME/labels.json` holds all labels across all
projects. A label name is `<CODE>:<namespace>:<value>` (e.g. `ATM:type:bug`) or
`<CODE>:<tag>` for free-form tags. The namespace segment is optional and open —
any namespace the user invents is valid; there is no whitelist and no
pre-declared axis list. A namespace "exists" iff at least one label with that
prefix is in the registry.

Label-name regex: `^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$`. The project prefix
must match an existing project (rejects `XYZ:type:bug` if project `XYZ` does not
exist).

**Assigning a label creates/updates the registry entry.** When a task is
created or a label is added to a task, any label not already in `labels.json` is
auto-registered (description defaults to empty, preserved if already set). The
registry is a description store + namespace index, **not a gatekeeper** — there
is no "must exist before assignment" rule. `label add` is an upsert: it ensures
the label is present and sets/updates its description; it does not need to run
before a label can be used. Agents can self-organize by inventing labels
(`ATM:agent:claude`, `ATM:doc:architecture`) at assign time.

Soft removal is preserved: removing a label from the registry stops new
assignments but retains it on existing tasks; the removal response reports
`retained_usage`.

### 4. The system has no intrinsic workflow knowledge

v1's intrinsic workflow knowledge is removed in full. None of the following
survive as first-class concepts, commands, fields, or rules:

- **Status field + state machine.** `Task.Status`, `allowedTransitions`, the
  `blocked` status value, the "recognized status values" list, the auto
  `<CODE>:status:open` on new tasks — all gone. Status is just a namespace
  humans happen to use; the system treats `ATM:status:open` no differently than
  `ATM:type:bug`.
- **Claim / unclaim.** The `Task.Claim` field, the atomic compare-and-set
  primitive, and the claim/unclaim commands/keys are gone. If agents want to
  signal "I'm on this," they assign a label like `ATM:claimed-by:claude`; it is
  last-writer-wins with no conflict detection. Coordination lives outside the
  system.
- **`task next`.** Gone. There is no terminal-status rule, no
  blocked-by-terminal computation, no claimable/non-claimed filtering. The
  human/agent drives "what to work on next" by filtering labels (e.g.
  `task list --project ATM --label ATM:status:open`).
- **Review.** `review request/approve/reject/queue/followups/dashboard` are all
  gone. Review is just a label value (`ATM:status:review`) that humans filter
  on; approving/rejecting is a `task label add/remove`.
- **`type_axis`.** `Project.TypeAxis`, `SetTypeAxis`, the `set-type-axis`
  commands, and `typeAxisScore` are gone. All namespaces are equal.
- **Followups.** `Followup` and its status/assignee fields (which were
  themselves intrinsic workflow knowledge — owner + status) are gone.
- **Todos and Discussions.** Gone. The Task entity carries no conversational or
  checklist content; the history log (Section 5) is the only narrative record.
- **Convention matching.** `ShowWithContext`'s `matchingConventions` (which
  singled out `kind:convention` tasks) and the `typeAxisScore` ranking are
  gone. `task show` renders the task and its history; no links, no conventions,
  no guide, no timeline to assemble.
- **Links.** `Task.Links`, the `blocks`/`related-to`/`implements`/`documents`
  enum, the computed `blocked-by` reverse edge, and the `atm task link`
  commands are gone. If agents want task relationships, they express them as
  labels (`ATM:blocks:ATM-0002`, `ATM:related:ATM-0005`) — pure label substrate,
  no special edge type.
- **Project Guide.** The guide subsystem (sections, refs, freshness threshold,
  coverage/stale checks, all `project guide *` commands) is gone. If agents
  need always-read context, they keep it in a markdown file in the repo and
  read it via their own workflow.
- **Actor entity.** `actors.json`, the lazy-registration behavior, the
  `agent:X`/`human:X` format rule, the `actor list/show` commands, and
  `ValidateActorID` are gone. `--actor` is a free-form string (`claude`,
  `ttran`) stamped into history/created_by/updated_by. No registration step.

### 5. History is the only narrative record

The structural history log (machine-generated, like `git log`) survives on
every Task and Project. Each mutation appends a `HistoryEntry`:

```json
{"id":"h1","action":"created","actor":"claude","at":"2026-07-02T12:00:00Z","meta":{}}
{"id":"h2","action":"label-added","actor":"ttran","at":"2026-07-02T12:05:00Z","meta":{}}
```

It is not conversational — no human types "I changed the title"; the store
writes it automatically. With Discussions gone, History is the only narrative
record on a task. `actor` is a free-form string (Section 4). The `appendHistoryAt`
helper carries over from v1.

### 6. TUI group-by-namespace mode

The TUI Tasks tab gains a `G` key that opens a picker of namespaces discovered
from the current project's labels (`store.Namespaces(code)`). Selecting a
namespace regroups the task list under each value
(`store.GroupTasksByNamespace`). Existing label filters apply before grouping. A
sentinel group holds tasks with no label in the selected namespace. Selecting
"none" restores the flat list. Group headers are collapsible. `G` is uppercase
to distinguish from the existing `g` (top-of-list).

The TUI Projects tab labels pane renders the project's labels (global registry
filtered by `<CODE>:` prefix) grouped by namespace (sorted alphabetically;
unnamespaced tags under a `tags:` heading). Add/remove/describe actions operate
on full label names; grouping is presentational.

This realizes the "any namespace is a management axis on demand" vision without
dedicated per-axis screens or a pre-declared axis list. The user invents
namespaces by adding labels; the TUI discovers them.

## CLI surface

Global flags (carried from v1): `--store <path>` (overrides `ATM_HOME`),
`--output json|text` (default text; JSON is deterministic — sorted keys, stable
whitespace, RFC 3339 UTC timestamps), `--actor <id>` (free-form string; required
on mutating commands, optional on reads), `--quiet`. Exit codes: 0 success; 1
generic; 2 usage; 3 not-found; 4 conflict. Errors go to stderr with a stable
`{"error":{"code":"...","message":"..."}}` envelope in JSON mode.

```
# Store
atm init [--store <path>]                       # idempotent; creates projects/ + touches labels.json
atm store path                                 # print resolved store path

# Projects
atm project create  --code <CODE> --name <NAME> [--actor <id>]
atm project list
atm project show    --code <CODE>
atm project set-name --code <CODE> --name <NAME> [--actor <id>]
atm project remove  --code <CODE> [--actor <id>]     # zero-task guard

# Labels (global registry)
atm label add    --name <NAME> [--description <DESC>] [--actor <id>]   # upsert; auto-registers
atm label remove --name <NAME> [--actor <id>]                          # soft; reports retained_usage
atm label list   [--project <CODE>] [--namespace <NS>]                 # filtered views
atm label show   --name <NAME>

# Tasks
atm task create  --project <CODE> --title <TITLE> [--description <DESC>] [--label <L>]... [--actor <id>]
atm task list    [--project <CODE>] [--label <L>]... [--group-by <NS>]
atm task show    --id <ID>
atm task set-title       --id <ID> --title <TITLE> [--actor <id>]
atm task set-description --id <ID> --description <DESC> [--actor <id>]
atm task label add    --id <ID> --label <L> [--actor <id>]
atm task label remove --id <ID> --label <L> [--actor <id>]
atm task remove       --id <ID> [--actor <id>]
```

`task list --group-by <NS>` mirrors the TUI `G` mode: when set, output is
grouped by the namespace's values (sentinel group for tasks missing that
namespace). JSON shape: `{"groups":[{"value":"bug","tasks":[...]},{"value":"","tasks":[...]}]}`.
Without `--group-by`, flat list sorted by project-then-numeric ID.

`task create` assigns the next id `<CODE>-<N>` (4-digit zero-padded up to 9999,
then natural width). It auto-registers any supplied labels in `labels.json`. It
does **not** auto-assign any status label (no intrinsic knowledge).

`atm tui [--store <path>] [--actor <id>]` is the TUI entrypoint.

**Removed command groups** (deleted entirely): `task set-status`, `task next`,
`task claim`, `task unclaim`, `task link`, `task todo`, `task followup`,
`task discussion`, `task timeline`, `review`, `project set-type-axis`,
`project repo`, `project guide`, `actor`.

## Data model

Store root (machine-global; resolution: `--store` flag -> `ATM_HOME` ->
`~/.config/atm`; no DB; detachable by directory copy):

```
$ATM_HOME/
  labels.json            # global registry: {"labels":[{"name","description"}]}
  projects/
    <CODE>.json          # Project entity
    <CODE>/
      tasks/
        <CODE>-<NNNN>.json   # one Task file per task
    <CODE>.lock          # per-project file lock (unchanged from v1)
```

`actors.json` is gone. No guide files. No followup/todo/discussion sub-files.

**Project** (`projects/<CODE>.json`):

```go
type Project struct {
    Code         string         `json:"code"`           // ^[A-Z]{3,6}$
    Name         string         `json:"name"`
    NextTaskN    int            `json:"next_task_n"`    // ID counter
    History      []HistoryEntry `json:"history,omitempty"`
    NextHistoryN int            `json:"next_history_n,omitempty"`
    CreatedAt    time.Time      `json:"created_at"`
    CreatedBy    string        `json:"created_by"`       // free-form actor string
    UpdatedAt    time.Time      `json:"updated_at"`
    UpdatedBy    string        `json:"updated_by"`
}
```

No `TypeAxis`, no `Labels`, no `Guide`, no `RepoPaths`, no
`GuideFreshnessThreshold`.

**Task** (`projects/<CODE>/tasks/<ID>.json`):

```go
type Task struct {
    ID          string         `json:"id"`            // <CODE>-<NNNN>
    ProjectCode string         `json:"project_code"`
    Title       string         `json:"title"`
    Description string         `json:"description,omitempty"`
    Labels      []string       `json:"labels"`        // full names, e.g. "ATM:type:bug"
    History     []HistoryEntry `json:"history"`
    CreatedAt   time.Time      `json:"created_at"`
    CreatedBy   string        `json:"created_by"`
    UpdatedAt   time.Time      `json:"updated_at"`
    UpdatedBy   string        `json:"updated_by"`
}
```

No `Status`, no `Claim`, no `Links`, no `Todos`, no `Followups`, no
`Discussions`.

**Label** (in `labels.json`):

```go
type Label struct {
    Name        string `json:"name"`        // <CODE>:<namespace>:<value> | <CODE>:<tag>
    Description string `json:"description,omitempty"`
}
```

**HistoryEntry** (unchanged shape from v1; lives on Project + Task):

```go
type HistoryEntry struct {
    ID     string         `json:"id"`        // h1, h2, ...
    Action string         `json:"action"`    // created, title-changed, label-added, ...
    Actor  string         `json:"actor"`     // free-form, e.g. "claude", "ttran"
    At     time.Time      `json:"at"`
    Meta   map[string]any `json:"meta,omitempty"`
}
```

## Store API surface

Carryover from v1 `store.go`: `Open`, `Init`, `ResolveStorePath`, `WithLock`
(per-project file lock), `ReadJSON`/`WriteJSON`, `ParseTaskID`/`RenderTaskID`/
`SortTaskIDs`, `RFC3339UTC`/`Now`, the error sentinels
(`ErrNotFound`/`ErrConflict`/`ErrUsage`). `ValidateProjectCode` regex moves to
`^[A-Z]{3,6}$`. `ValidateActorID` is removed (actor is free-form). `ValidateLabelName`
moves to the new label regex. `lock.go` and `json.go` carry over unchanged.

**Project ops** (`project.go`):
- `CreateProject(code, name, actor) (*Project, error)` — minimal; validates code
  regex, conflict-checks, writes `projects/<CODE>.json` + creates
  `projects/<CODE>/tasks/` dir.
- `GetProject(code) (*Project, error)`
- `ListProjects() []*Project`
- `SetProjectName(code, name, actor) error`
- `RemoveProject(code, actor) error` — zero-task guard (refuses if any task
  files exist); deletes the project file + tasks dir.

**Label ops** (`label.go` — new file):
- `LabelAdd(name, description, actor) error` — validates name regex, enforces
  `<CODE>:` prefix matches an existing project, upserts into `labels.json`
  (creates entry if absent; updates description if present and non-empty). This
  is also the auto-registration path called lazily by `TaskLabelAdd`/`CreateTask`.
- `LabelRemove(name, actor) (*LabelRemoveResult, error)` — soft removal: drops
  the entry from `labels.json`, counts tasks still carrying it ->
  `retained_usage`. Existing tasks keep the label string; new assignments are
  refused.
- `LabelList(project, namespace string) []Label` — optional filters: `project`
  filters by `<CODE>:` prefix; `namespace` filters by `<CODE>:<namespace>:` prefix.
  Empty filters -> all.
- `LabelShow(name) (Label, error)` — returns the label + description, or
  `ErrNotFound`.
- `Namespaces(code string) []string` — distinct namespaces among `<CODE>:`-prefixed
  labels in the registry, sorted. Drives the TUI `G` picker.

**Task ops** (`task.go`):
- `CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error)`
  — validates title non-empty; validates each label regex + project-prefix
  match; auto-registers any not present in the registry; assigns next
  `<CODE>-<NNNN>`; writes task file + bumps `Project.NextTaskN`. No auto status
  label.
- `GetTask(id) (*Task, error)`
- `SetTitle(id, title, actor) error`
- `SetDescription(id, description, actor) error`
- `TaskLabelAdd(id, label, actor) error` — validates label regex + project-prefix
  match; auto-registers in `labels.json` if absent; appends to task (dedup,
  sorted).
- `TaskLabelRemove(id, label, actor) error` — removes from task; does NOT touch
  the registry.
- `RemoveTask(id, actor) error` — deletes the task file.

**Query ops** (`query.go`):

```go
type QueryFilters struct {
    Project string
    Labels  []string   // AND-intersect; full label names
}
```

- `ListTasks(filters QueryFilters) []*Task` — drops `Status`/`Assignee`/
  `Claimant` filters (all gone). Sorts by project-then-numeric ID.
- `GroupTasksByNamespace(code, ns string) map[string][]*Task` — groups a
  project's tasks by the value they carry in namespace `ns`. Tasks with no label
  in that namespace land in the sentinel key `""`. Used by `task list --group-by`
  and the TUI `G` mode.

**Concurrency**: `labels.json` is global; mutations take the relevant project's
lock (the `<CODE>:` prefix ties a label to a project). Reads are lock-free
(best-effort). Two projects editing the same global file concurrently is a
tolerated race, same as v1's `actors.json` approach.

**What's gone from the store API:** `SetStatus`, `Claim`/`Unclaim`/`Next`,
`RequestReview`/`ApproveReview`/`RejectReview`/`ReviewQueue`/`OpenFollowups`/
`Dashboard`, `TodoAdd`/`TodoToggle`, `FollowupAdd`/`FollowupResolve`,
`DiscussionAdd`, `TimelineList`, `ShowWithContext` (+ `matchingConventions`),
`SetTypeAxis`, all `Guide*` methods, `RepoAdd`/`RepoRemove`, the entire
`actor.go` (`Register`/`ListActors`/`ShowActor`), `ValidateActorID`,
`allowedTransitions`, `typeAxisScore`, and all Link methods (`LinkAdd`/
`LinkRemove`/`LinkList`).

## TUI surface

The TUI keeps the existing Bubble Tea shell (`app.go`, `keymap.go`, `styles.go`,
`form.go`, `help.go`, `components/`) and the three-tab structure: **Projects**,
**Tasks**, **Help**. (The v1 Dashboard tab is gone — it was review+followups+
guide.)

**Removed from TUI**: `dashboard.go`, `actors.go`, `guide.go`; all claim keys
(`[c]`, `[u]`), the status overlay (`[s]`, `showStatusOverlay`,
`allowedTransitionsPub`, `allStatuses`, `statusKey`), review key (`[v]`);
todo/followup/discussion/link keys (`[t]`, `[o]`, `[O]`, `[d]`, `[L]`,
Space-toggle); `[T] set type-axis`, `[R]/[r] repo`, all guide section/ref keys
(`[S]/[s]/[X]/[M]/[g]/[m]/[d]/[F]`); the "MATCHING CONVENTIONS", "TIMELINE", and
"LINKS" sections of the task detail render.

**Projects tab** (`projects.go`, rewritten):
- List columns: `CODE  NAME  TASKS  LABELS  UPDATED` (drops GUIDE). Keys:
  `[a]dd`, `Enter/[e]` detail, `[x] remove` (zero-task guard).
- Detail view is a single pane (no multi-pane right column — repos/guide/
  advanced are gone). Renders project facts + labels grouped by namespace
  (unnamespaced tags under `tags:`). Keys: `[N]` set name, `[L]` add label
  (form: name + description), `[l]` remove label (form: name; toast shows
  `retained_usage`), `[x]` remove project.

**Tasks tab** (`tasks.go`, rewritten):
- List columns: `ID  TITLE  LABELS` (drops STATUS and CLAIMANT). Filter line
  keeps `project:` and adds `label:`. Keys in list mode: `j/k/g/G` nav, `Enter`
  open detail, `[a]dd` new task, `/` filter. `[n]ext`/`[c]laim`/`[u]nclaim` gone.
- `G` group-by-namespace mode: overlay picker lists
  `store.Namespaces(code)` + a "none (flat)" option. Selecting a namespace
  regroups the list under each value; existing label filters apply before
  grouping; sentinel group for tasks missing the namespace; group headers
  collapsible; "none" restores flat.
- Detail view (simplified): task facts + `HISTORY` section (machine-generated
  log). Keys: `[e]` edit title, `[b]` add label, `[B]` remove label, `[x]`
  remove task (confirm overlay). No `[s]`/`[c]`/`[u]`/`[L]`/`[t]`/`[o]`/`[O]`/
  `[d]`/`[v]`/Space.

**Help tab** (`help.go`, rewritten): CLI/TUI parity table + global keymap,
shrunk to the v2 surface.

`atm tui [--store <path>] [--actor <id>]` keeps `--store`, `--actor`. The
`requireActor()` guard still gates mutations; actor is free-form (no
`agent:`/`human:` enforcement).

## Testing, verification & rollout

**Testing approach** — table-driven unit tests per store file (mirroring v1's
`store_test.go`/`project_test.go`/`task_test.go`/`query_test.go`/`label_test.go`
new/`lock_test.go`+`json_test.go` carried over). CLI tests mirror
`project_test.go`/`task_test.go`/`entry_test.go` using the `testdata/` golden
pattern. TUI tests mirror `app_test.go` (model updates + view snapshots). No new
integration framework.

**Store test invariants**: project create validates `^[A-Z]{3,6}$` + refuses
duplicates; label auto-registration on `CreateTask`/`TaskLabelAdd`;
`LabelRemove` soft-removes (drops entry, counts retained usage, refuses new
assignment); `LabelList` filters; `Namespaces` returns distinct sorted
namespaces; `ListTasks` AND-intersects labels; `GroupTasksByNamespace` buckets
by value with sentinel; `RemoveProject` zero-task guard; label regex enforces
project-prefix match; determinism (sorted JSON, stable ordering).

**CLI tests**: each command has text-mode and json-mode cases via the existing
`emit`/golden pattern; `--group-by` output shape (groups array) gets a golden
file; error exit codes (2/3/4) covered.

**TUI tests**: tab switching; project create form; task create form; label add
form; the `G` overlay picker (open, select namespace, regroup, select "none"
restores flat); remove-task confirm overlay; view snapshot assertions on the
simplified detail render.

**Verification gate**: `make verify` (runs `make build && make test`) remains
the gate per AGENTS.md. No new make targets. `.golangci.yml` carries over
unchanged.

**Rollout** (Approach A — wholesale delete-and-rebuild):
1. One commit deletes the v1 store/cli/tui files (keeping `store.go`, `json.go`,
   `lock.go`) + their tests, plus `dashboard.go`, `guide.go`, `actors.go`,
   `claim.go`, `review.go`, `context.go`, `entry.go` (followups/todos/
   discussions/timeline), `link.go`, `actor.go`.
2. Subsequent commits rebuild layer by layer: store (project, label, task,
   query) -> store tests -> cli (project, label, task, output, root, tui) ->
   cli tests -> tui (projects, tasks, help, app) -> tui tests -> README +
   dogfood script rewrite.
3. `make verify` must be green before each layer's commit is considered done;
   the tree may not build between the delete commit and the store rebuild
   commit — that is expected and acceptable per Approach A.

**Migration**: none. v1.x data is not migrated. The v2 store starts fresh; `atm
init` creates `projects/` and touches `labels.json`. Existing v1 users delete
their `$ATM_HOME` or point `--store` at a new empty dir.

## Out of scope

- Agent-side label-creation heuristics, richer cross-project label queries.
- A future "workflow declaration" layer (per-project terminal labels / default
  new-task labels) — explicitly deferred; v2 ships without it. If users want
  workflow, they drive it via label filters in their agent prompts/habits for
  now.
- Migration tooling from v1.