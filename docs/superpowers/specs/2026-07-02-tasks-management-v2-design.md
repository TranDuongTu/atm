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

### 6. Filter-driven faceting (no intrinsic workflow knowledge)

The TUI Tasks tab has **no grouping toggle and no namespace picker.** The
filter syntax doubles as the view-mode selector, and grouping is automatic
whenever the filter contains wildcard tokens. This realizes the "any namespace
is a management axis on demand" vision without dedicated per-axis screens, a
pre-declared axis list, or a modal — the user invents namespaces by adding
labels, and the TUI facets them on demand via the filter line.

**Filter tokens** (space-separated, AND-joined across tokens):
- **Exact** (`ATM`, `ATM:status:open`, `ATM:type:bug`) — restrict the visible
  set. `ATM` matches the task's `ProjectCode`; a full label name matches tasks
  carrying that label.
- **Wildcard** (suffix-only at a namespace boundary: `ATM:*` for whole-project
  faceting, `ATM:status:*` for one namespace) — does **not** restrict; it
  declares a facet dimension. Multiple wildcards declare multiple facets. No
  infix/prefix wildcards.

**View mode is implicit in the filter:**
- **No wildcard** in filter → flat paged list, sorted per the current sort mode.
- **≥1 wildcard** → grouped view. Groups are the concrete labels matched by
  each wildcard that appear on ≥1 in-scope task. A task appears in **every**
  group whose key it carries (multi-membership preserved — a task with both
  `ATM:status:open` and `ATM:status:done` shows in both, surfacing
  inconsistencies for cleanup). A single shared `(no matching labels)` bucket
  holds in-scope tasks that match no wildcard (covers unlabeled tasks and
  tasks missing every faceted namespace — the view for finding and correcting
  under-labeled tasks).

The TUI Projects tab labels pane renders the project's labels (global registry
filtered by `<CODE>:` prefix) grouped by namespace (sorted alphabetically;
unnamespaced tags under a `tags:` heading), each label shown with its current
task usage count (`name (N tasks)`) to support the human's label-reconciliation
workflow. Add/remove/describe actions operate on full label names; grouping is
presentational.

The TUI and CLI are both first-class management surfaces; the typical actor on
the CLI is an AI agent, the typical actor on the TUI is a human consulting and
steering — but both surfaces carry full management capability. History is an
**immutable system invariant**: every mutation appends a `HistoryEntry` and no
command exists to edit or delete history; this holds for human and agent
mutations alike.

### 7. Onboarding & conventions (advisory, not enforced)

v2's vision is that workflow lives outside the system — in agent prompts and
human habits, not in the store. But a fresh agent landing in a project needs a
deterministic entry point, and a fresh project needs its labels seeded.
**Onboarding is the act of seeding index tasks and seed labels; the conventions
below tell humans and agents how to do that using only the label substrate.**
No bootstrap command, no reserved labels, no repo-side files, no store-side
repo paths. The conventions are **documented, not code-reserved**: the system
treats `ATM:context:start-here` identically to `ATM:type:bug`.

The `atm conventions` command (and the TUI Help tab) prints the conventions as
the agent's first-contact reference. It documents:

**Suggested seed namespaces** a fresh project should populate:

| Namespace | Examples | Purpose |
|-----------|----------|---------|
| `status:` | `open`, `todo`, `in-progress`, `done`, `blocked`, `review` | workflow states — labels only, no state machine |
| `type:` | `bug`, `feature`, `task`, `chore` | task categorization |
| `priority:` | `high`, `medium`, `low` | optional prioritization |
| `repo:<name>` | `ATM:repo:atm` | **index task** whose description says where to find the repo and what it means — this is the repo→project binding, expressed in the label substrate |
| `doc:<name>` | `ATM:doc:architecture` | index task pointing at a doc/resource and how to use it |
| `context:always-read` | `ATM:context:always-read` | pointer to the always-read context markdown (replaces the deleted v1 Project Guide) |
| `context:start-here` | `ATM:context:start-here` | **the single entry-point task** a fresh agent queries first; its description is the "read this first" pointer (to a context.md, a steering note, or a list of where to look) |
| `claimed-by:<agent>` | `ATM:claimed-by:claude` | who's working on what — replaces v1 Claim; last-writer-wins, no conflict detection |
| `blocks:<ID>`, `related:<ID>` | `ATM:blocks:ATM-0002` | task relationships via labels (replaces v1 Links) |

**First-time human sequence:** `atm tui` (auto-inits the store) → create the
project (`[a]dd` in the Projects tab) → create a few seed index tasks
(`start-here`, `repo:<name>`, `doc:<name>`, `context:always-read`) and initial
work tasks, labeling as you go. The act of seeding these tasks populates the
`status`/`type`/`repo`/`doc`/`context` namespaces organically — there is no
separate bootstrap step.

**Agent first-contact sequence:**
1. `atm conventions` — read the guide.
2. `task list --project <CODE> --label <CODE>:context:start-here` — get the
   entry-point pointer and follow it.
3. `task list --project <CODE> --label <CODE>:repo:*` / `:doc:*` / `:context:*`
   — discover index tasks for repos, docs, and always-read context.
4. `task list --project <CODE> --label <CODE>:status:open` — get open work.

A fresh agent that does not yet know the project's namespaces runs the
`start-here` query first (one deterministic label) and follows whatever the
start-here task's description points at.

**Plugins/skills:** ATM ships only the doc + the `conventions` command. Plugins
or agent skills may wrap the first-contact sequence; ATM itself has no plugin
mechanism in v2.

This single convention folds three earlier-identified onboarding gaps into one
documentation answer:
- **Repo→project binding** — `repo:<name>` index tasks in the label substrate.
- **Always-read context anchor** — `context:always-read` index task pointing at
  the markdown; `context:start-here` as the deterministic entry point.
- **Cold-start label vacuum** — onboarding *is* seeding the index and work
  tasks, which populates the namespaces. No empty-project problem.


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
atm conventions [--output json|text]           # print onboarding guide + suggested label namespaces

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
atm task list    [--project <CODE>] [--label <L>]... [--facets]
atm task show    --id <ID>
atm task set-title       --id <ID> --title <TITLE> [--actor <id>]
atm task set-description --id <ID> --description <DESC> [--actor <id>]
atm task label add    --id <ID> --label <L> [--actor <id>]
atm task label remove --id <ID> --label <L> [--actor <id>]
atm task remove       --id <ID> [--actor <id>]
```

`task list --facets` mirrors the TUI grouped view: `--label` accepts full label
names **and suffix-only wildcards** (`ATM:status:*`, `ATM:*`). Non-wildcard
`--label` tokens restrict the set; wildcard tokens facet it. With `--facets`,
output is grouped by every concrete label matched by any wildcard `--label`
token (multi-membership; tasks matching no wildcard are returned in a separate
`others` array). JSON shape:
`{"groups":[{"label":"ATM:status:open","tasks":[...]}],"others":[...]}`.
Without `--facets`, flat list sorted by project-then-numeric ID.

`task create` assigns the next id `<CODE>-<N>` (4-digit zero-padded up to 9999,
then natural width). It auto-registers any supplied labels in `labels.json`. It
does **not** auto-assign any status label (no intrinsic knowledge).

`atm tui [--store <path>] [--actor <id>]` is the TUI entrypoint. If the
resolved store directory is absent on launch, `tui` auto-initializes it
(equivalent to `atm init`: creates `projects/` + touches `labels.json`) and
continues, so a first-time human can start with one command. If the store
exists but is empty of projects, `tui` launches normally (the Projects tab
shows the empty state). `atm init` remains for explicit/scripted use.

`atm conventions` is a read-only command that prints the onboarding guide and
suggested label-namespace conventions as text (and JSON via `--output json`).
It is the agent's first ATM call on contact with a project. Stable, versioned
with the binary; also rendered in the TUI Help tab. See **Onboarding &
conventions** below for what it documents. Conventions are advisory only —
nothing in the store validates or special-cases the documented namespaces.

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
  labels in the registry, sorted. Used by the TUI Projects tab labels pane and
  available for advisory display; the Tasks tab faceting is wildcard-driven and
  does not require this.

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
    Labels  []string   // AND-intersect; full label names; may include
                       // suffix-only wildcards (e.g. "ATM:status:*", "ATM:*")
                       // which declare facets and do NOT restrict the set.
}
```

- `ListTasks(filters QueryFilters) []*Task` — applies only the *restricting*
  tokens (exact labels + project); wildcard tokens are ignored for scoping.
  Sorts by project-then-numeric ID (the store's canonical order; the TUI/CLI
  sort mode is applied by the caller on the returned slice).
- `GroupTasks(filters QueryFilters) (groups []LabelGroup, others []*Task)` —
  groups the in-scope set (restricted by exact tokens) by every concrete label
  matched by any wildcard token that appears on ≥1 in-scope task. A task lands
  in every group whose key it carries (multi-membership). `others` holds
  in-scope tasks matching no wildcard. Empty wildcard matches → `others` is the
  whole in-scope set. Used by `task list --facets` and the TUI grouped view.
  Replaces v1's `GroupTasksByNamespace`.

```go
type LabelGroup struct {
    Label string   // concrete label name, e.g. "ATM:status:open"
    Tasks []*Task
}
```

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
`allowedTransitions`, `typeAxisScore`, all Link methods (`LinkAdd`/
`LinkRemove`/`LinkList`), and `GroupTasksByNamespace` (replaced by
`GroupTasks`).

## TUI surface

The TUI keeps the existing Bubble Tea shell (`app.go`, `keymap.go`, `styles.go`,
`form.go`, `help.go`, `components/`) and the three-tab structure: **Projects**,
**Tasks**, **Help**. (The v1 Dashboard tab is gone — it was review+followups+
guide.) The TUI and CLI are both first-class management surfaces; the typical
actor on the CLI is an AI agent, the typical actor on the TUI is a human
consulting and steering — but both surfaces carry full management capability.
History is an **immutable system invariant**: every mutation appends a
`HistoryEntry` and no command exists to edit or delete history; this holds for
human and agent mutations alike.

**Removed from TUI**: `dashboard.go`, `actors.go`, `guide.go`; all claim keys
(`[c]`, `[u]`), the status overlay (`[s]`, `showStatusOverlay`,
`allowedTransitionsPub`, `allStatuses`, `statusKey`), review key (`[v]`);
todo/followup/discussion/link keys (`[t]`, `[o]`, `[O]`, `[d]`, `[L]`,
Space-toggle); `[T] set type-axis`, `[R]/[r] repo`, all guide section/ref keys
(`[S]/[s]/[X]/[M]/[g]/[m]/[d]/[F]`); the "MATCHING CONVENTIONS", "TIMELINE", and
"LINKS" sections of the task detail render; the `G` group-by-namespace picker
(grouping is now automatic from filter wildcards — see Section 6).

**Projects tab** (`projects.go`, rewritten):
- List columns: `CODE  NAME  TASKS  LABELS  UPDATED` (drops GUIDE). Keys:
  `[a]dd`, `Enter/[e]` detail, `[x] remove` (zero-task guard).
- Detail view is a single pane (no multi-pane right column — repos/guide/
  advanced are gone). Renders project facts + labels grouped by namespace
  (unnamespaced tags under `tags:`), **each label shown with its current task
  usage count** (`name (N tasks)`) to support the human's label-reconciliation
  workflow. Keys: `[N]` set name, `[L]` add label (form: name + description),
  `[l]` remove label (form: name; toast shows `retained_usage`), `[x]` remove
  project.

**Tasks tab** (`tasks.go`, rewritten):
- A persistent one-line header over the list always shows the current view
  state: `PROJECT: <code>  FILTER: <tokens>  SORT: <mode>`. The filter syntax
  doubles as the view-mode selector (see Section 6); there is no separate
  grouping toggle or picker.
- **Filter tokens** (space-separated, AND-joined):
  - **Exact** (`ATM`, `ATM:status:open`, `ATM:type:bug`) — restrict the visible
    set. `ATM` matches the task's `ProjectCode`; a full label name matches
    tasks carrying that label.
  - **Wildcard** (suffix-only at a namespace boundary: `ATM:*` for whole-
    project faceting, `ATM:status:*` for one namespace) — does **not**
    restrict; it declares a facet dimension. Multiple wildcards declare
    multiple facets. No infix/prefix wildcards.
- **View mode is implicit in the filter:**
  - **No wildcard** in filter → flat paged list, sorted per the current sort
    mode.
  - **≥1 wildcard** → grouped view. Groups are the concrete labels matched by
    each wildcard that appear on ≥1 in-scope task. A task appears in **every**
    group whose key it carries (multi-membership preserved — a task with both
    `ATM:status:open` and `ATM:status:done` shows in both, surfacing
    inconsistencies for cleanup). A single shared `(no matching labels)`
    bucket holds in-scope tasks that match no wildcard (covers unlabeled tasks
    and tasks missing every faceted namespace — the view for finding and
    correcting under-labeled tasks).
- **Sort** (`s` cycles): `updated-desc` (default — supports the human's
  "browse recent agent activity" consult mode) → `updated-asc` → `id-asc`.
- List columns: `ID  TITLE  LABELS  UPDATED` (drops STATUS and CLAIMANT).
  LABELS shows full label names; UPDATED is a relative timestamp.
- Keys in list mode: `j/k/g` nav (`g` top-of-list), `Enter` open detail,
  `[a]dd` new task, `/` edit FILTER inline, `s` cycle sort, group headers
  collapsible. `[n]ext`/`[c]laim`/`[u]nclaim` gone; `G` group-by-namespace
  picker gone (grouping is automatic from filter wildcards).
- Empty states: no tasks → `no tasks`; wildcard filter yielding no concrete
  labels to group → `(no matching labels)` only, header note `no labels match
  wildcard — add labels to tasks`.

**Detail view** (simplified): task facts + `HISTORY` section (machine-generated,
immutable log). Keys: `[e]` edit title, `[b]` add label, `[B]` remove label,
`[x]` remove task (confirm overlay). No `[s]`/`[c]`/`[u]`/`[L]`/`[t]`/`[o]`/
`[O]`/`[d]`/`[v]`/Space.

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
namespaces; `ListTasks` AND-intersects exact labels and ignores wildcard tokens
for scoping; `GroupTasks` buckets by concrete labels matched by wildcards with
multi-membership, `others` holds tasks matching no wildcard; `RemoveProject`
zero-task guard; label regex enforces project-prefix match; determinism (sorted
JSON, stable ordering).

**CLI tests**: each command has text-mode and json-mode cases via the existing
`emit`/golden pattern; `--facets` output shape (`groups` array with `label`
keys + separate `others` array) gets a golden file; wildcard `--label` tokens
accepted (`ATM:status:*`, `ATM:*`); error exit codes (2/3/4) covered.

**TUI tests**: tab switching; project create form; task create form; label add
form; filter-driven grouping (enter `ATM:status:*` → grouped view with
multi-membership; enter `ATM` exact → flat list; `(no matching labels)` bucket
for unlabeled tasks); sort cycling (`s` → updated-desc/updated-asc/id-asc);
remove-task confirm overlay; view snapshot assertions on the simplified detail
render.

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
  new-task labels) — explicitly deferred; v2 ships without it. The Onboarding &
  conventions section (Section 7) documents *advisory* seed namespaces instead;
  no mechanism enforces them. If users want workflow, they drive it via label
  filters in their agent prompts/habits for now.
- Task comments / discussions as a narrative *human+agent* channel (separate
  from the immutable machine-generated History). Deferred; the current Task
  shape and append-only History model are compatible with a future
  `DiscussionAdd`-style append, so no data-model change would be required.
- Migration tooling from v1.