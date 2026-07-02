# Research Decisions: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-07-02 | **Revision**: v2.0.0

## R1. Storage location and detachability

**Decision**: All data lives under `$ATM_HOME` (default `~/.config/atm`), one
JSON file per project and per task, plus a global `labels.json` and
`actors.json`. Detachability is achieved by copying the directory. Per-project
file-level locking for atomic mutations.

**Rationale**: Plain text version-controls cleanly; no DB dependency; a project
is NOT 1:1 with a repo (a project may span multiple repos or none).

## R2. Task IDs

**Decision**: `<CODE>-<NNNN>` with a per-project sequential counter that widens
past four digits (`ATM-10000`). The code matches `^[A-Z]{3,6}$` (3-6 uppercase
ASCII letters only — no digits, no hyphens).

**Rationale**: Short, memorable, deterministic, and unambiguous in agent logs
and commit messages. The strict 3-6 letter rule keeps codes uniform and human-
friendly.

## R3. Project creation is minimal

**Decision**: `atm project create` takes ONLY `--code` and `--name` (plus the
internal `--actor`). No labels, no type-axis, no repo-paths at create time.
Labels and repos are added later via dedicated commands.

**Rationale**: Creation should be deadly simple. The prior v1 create surface
(--type-axis, --label, --repo-path) coupled creation to label/axis curation and
created validation ordering constraints (axis namespace must have >=1 label).
Decoupling lets the human create the empty container first and curate labels
as the project's needs emerge.

## R4. type_axis removed entirely

**Decision**: The v1 `Project.TypeAxis` field, `SetTypeAxis`, the `set-type-axis`
CLI/TUI commands, and `typeAxisScore` are all removed. There is no designated
axis. Convention-doc matching orders by matched-label-count desc then ID asc.

**Rationale**: The type-axis was a single special-cased namespace used by
exactly one feature (convention-match priority). It added validation coupling
and conceptual surface for marginal benefit. Removing it makes ALL namespaces
equal — any namespace becomes a grouping axis on demand (FR-021), which is a
richer model than designating one axis up front.

## R5. Labels are global, hierarchical, project-prefixed

**Decision**: A single global registry at `$ATM_HOME/labels.json` holds all
labels. A label name is `<CODE>:<namespace>:<value>` (e.g. `ATM:type:bug`) or
`<CODE>:<tag>` for free-form tags. The namespace segment is optional and open —
any namespace the user invents is valid; there is no whitelist and no
pre-declared axis list. A namespace "exists" iff at least one label with that
prefix is in the registry.

**Rationale**: The user wants labels to be the dynamic substrate of the system:
categorization (type), ownership (owner), release tracking (release),
documentation grouping (doc), and any future concern — all expressed through
one mechanism. A global registry with project-prefixed hierarchical names lets
any namespace become a management axis on demand (US6), without pre-declaring
axes or building dedicated per-axis screens. Agents can both query by label
metadata and create labels (`agent:claude`, `doc:architecture`) to self-organize.

## R6. Status is a label, not a field; no state machine

**Decision**: `Task.Status` (the v1 dedicated string field) is removed. Status
is expressed as a label on the project's `status` namespace
(`<CODE>:status:<state>`). The system reads the status label wherever v1 read
`Task.Status` (next-task eligibility, blocking, claim, review). There is NO
state machine — any status label may replace any other freely (FR-005).

**Rationale**: Treating status as just another label axis unifies the model:
status is categorization, like type or owner. The v1 state machine
(`allowedTransitions`) prevented some transitions (e.g. done->blocked) but added
validation surface and friction. The user explicitly chose free transitions.
The recognized status values (`open`, `in-progress`, `done`, `cancelled`,
`review`) are conventional, not registry-enforced, but the system reads them
for coordination logic. A task with zero status labels is treated as `open`
(with a warning); a task with multiple is disambiguated by lexicographic order
(with a warning).

## R7. Links

**Decision**: Typed directed edges stored as an array on the task record.
Supported types for v2: `blocks` (A blocks B means B cannot start until A is
done; B carries an implied `blocked-by` reverse edge, computed not stored),
`related-to` (symmetric, stored on one side and traversed both ways),
`implements` (A implements B; B is the parent, e.g. an epic), `documents` (A
documents B or A documents a label/area). The "next task" query excludes any
task with an unmet `blocked-by` (a `blocks` edge from a task whose status label
is not terminal — `done`/`cancelled`).

**Rationale**: Unchanged from v1 semantics; the only adaptation is reading
status from the status label instead of the removed `Status` field.

## R8. Deterministic output

**Decision**: All command output is deterministic for a given store and
arguments. JSON object keys are serialized in sorted order; list orderings are
stable (by ID, by namespace, etc.). Snapshot/golden tests rely on this.

**Rationale**: Agents and snapshot tests must be reproducible (SC-002a).

## R9. Project guide

**Decision**: Each project has an optional `guide`: an ordered list of
references to convention docs (tasks) or external file paths, grouped by named
section. The guide is the always-read harness agents receive in `next`/`show`
context. The dashboard surfaces guide coverage (empty sections) and freshness
(referenced docs whose `updated_at` is older than a project-configured
threshold).

**Rationale**: Unchanged from v1 (FR-016/017/018).

## R10. Convention matching

**Decision**: `ShowWithContext` collects convention-doc tasks (labeled
`<CODE>:kind:convention`) whose labels intersect the task's labels, ordered by
matched-label-count desc then ID asc. No type-axis priority.

**Rationale**: With type_axis removed (R4), pure label-intersection ranking is
the natural fallback. More shared labels = more relevant convention doc.

## R11. TUI group-by-axis mode

**Decision**: The TUI Tasks tab provides a `G` key that opens a picker of
namespaces discovered from the project's labels (`store.Namespaces(code)`).
Selecting a namespace regroups the task list under each value
(`store.GroupTasksByNamespace`). Existing filters apply before grouping. A
sentinel group holds tasks with no label in the selected namespace. Selecting
"none" restores the flat list.

**Rationale**: Realizes the "any namespace is a management axis on demand"
vision (US6/FR-021) without dedicated per-axis screens or a pre-declared axis
list. The user invents namespaces by adding labels; the TUI discovers them.