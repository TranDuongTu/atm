# Feature Specification: Tasks Management System

**Feature Branch**: `001-tasks-management`

**Created**: 2026-07-02

**Status**: Draft

**Revision**: v2.0.0 — full rewrite. No backward compatibility with the prior v1.x
data model. The prior spec is removed; this is the initial Superpowers spec.

**Input**: A local-storage, CLI/API-first task system for AI agents and human
coordinators. Projects are identified by a short uppercase code (3-6 letters);
tasks are numbered per project (`ATM-0001`). Labels are the single dynamic
organizing substrate: global, hierarchical, project-prefixed
(`ATM:type:bug`, `ATM:status:done`, `ATM:owner:alice`). Status is a label axis,
not a dedicated field. Any namespace the user invents becomes a grouping axis
on demand in the TUI. A Bubble Tea TUI mirrors every CLI operation.

## Clarifications

### Session 2026-07-02

- Q: What does `atm project create` take? → A: Only `--code` (`^[A-Z]{3,6}$`,
  uppercase letters only, 3-6 chars) and `--name` (short display name). No
  type-axis, no labels, no repo-paths at create time. Labels and repos are added
  later via dedicated commands.
- Q: What happens to the prior `type_axis` concept? → A: Removed entirely. No
  designated axis. Convention-doc matching orders by matched-label-count desc
  then ID asc. Any namespace is discoverable as a grouping axis from the labels
  in use.
- Q: Are labels per-project or global? → A: Global registry, project-prefixed
  names. A label name is `<CODE>:<namespace>:<value>` (e.g. `ATM:type:bug`) or
  `<CODE>:<tag>` for free-form tags. All labels across all projects live in one
  `labels.json` file at `$ATM_HOME/labels.json`.
- Q: Is status a dedicated field? → A: No. `Task.Status` is removed. Status is
  expressed as a label on the `status` namespace of the task's project
  (`ATM:status:open`, `ATM:status:done`, ...). There is NO state machine: any
  status label may be set to any other freely.
- Q: Backward compatibility / migration? → A: None. Full rewrite. Existing
  `$ATM_HOME` data from v1.x is not migrated; the v2 store starts fresh.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Agent queries next task and context (Priority: P1)

An AI agent working in a repository runs a single command to ask the system
"what should I work on next?". The system returns the highest-priority,
unclaimed, non-blocked task in the current project, including its full context:
description, labels, links to related tasks, and pointers to any project-wide
best-practice / convention documents that apply based on the task's labels. The
agent can claim the task and later unclaim it if it abandons the work.

**Why this priority**: The core agent-facing value. Without it, agents have no
entry point into the task system. It is the single slice that makes the system
usable end-to-end for its primary user.

**Independent Test**: Seed a project with at least one task, run `next` /
`claim` / `show` commands, and verify the returned task is the expected one and
its context includes linked tasks and matching convention docs.

**Acceptance Scenarios**:

1. **Given** a project `ATM` exists with tasks `ATM-0001` (labeled
   `ATM:status:open`, unblocked) and `ATM-0002` (labeled `ATM:status:open`,
   blocked by `ATM-0001`), **When** an agent runs the "next task" command for
   `ATM`, **Then** the system returns `ATM-0001` (not `ATM-0002`) with its
   description, labels, and links.
2. **Given** `ATM-0001` is returned, **When** the agent runs "claim" for
   `ATM-0001` as `agent:claude-1`, **Then** `ATM-0001` is recorded as claimed by
   `agent:claude-1` with a timestamp, and a second "next task" command does not
   return `ATM-0001`.
3. **Given** a project convention doc task labeled `ATM:kind:convention` and
   `ATM:type:bug` exists, **When** the agent runs "show" for a task labeled
   `ATM:type:bug`, **Then** the response includes a pointer to that convention
   doc because the task's labels match.
4. **Given** `ATM-0001` is claimed by `agent:claude-1`, **When** `claude-1` runs
   "unclaim", **Then** `ATM-0001` returns to the claimable pool and reappears in
   "next task".

### User Story 2 - Human manages projects and labels (Priority: P2)

A human coordinator creates a project with a short 3-6 letter code and a
display name. Creation is deliberately minimal: no labels, no axes, no repos up
front. After creation the human curates the global label registry — adding
labels with project-prefixed hierarchical names like `ATM:type:bug`,
`ATM:owner:alice`, `ATM:release:v2`, `ATM:doc:architecture`. Any namespace the
human invents becomes a usable grouping axis; there is no pre-declared axis
list. Labels can be removed (soft removal: existing tasks retain them, new
assignments reject them).

**Why this priority**: Projects and labels are the organizing substrate every
other story depends on. Agents cannot be pointed at a project until a human has
created it; labels cannot be assigned until they exist in the registry.

**Independent Test**: Create a project via command, add/remove labels in the
global registry, verify tasks can be created with those labels and that
querying by label returns the right set.

**Acceptance Scenarios**:

1. **Given** no project with code `ATM` exists, **When** a human runs
   `atm project create --code ATM --name "Agent Tasks Management"`, **Then**
   the project is created with an empty label set and subsequent tasks are
   numbered `ATM-0001`, `ATM-0002`, etc.
2. **Given** project `ATM` exists, **When** the human adds labels
   `ATM:type:epic`, `ATM:type:impl`, `ATM:type:bug`, `ATM:owner:alice`,
   `ATM:doc:architecture`, **Then** those labels are available for assignment
   to tasks in `ATM` and appear grouped by namespace in the TUI.
3. **Given** tasks exist with label `ATM:area:cli`, **When** the human removes
   `ATM:area:cli` from the registry, **Then** existing tasks keep the label
   (soft removal) but new tasks cannot be assigned it; the system reports
   retained usage.
4. **Given** a project code `ATM` is reused, **When** the human tries to create
   a second project with code `ATM`, **Then** creation is rejected (codes are
   unique and immutable).

### User Story 3 - Create tasks and organize them via labels and links (Priority: P3)

An agent or human creates a task in a project, assigns it labels (including a
status label on the `status` namespace), and links it to other tasks to express
dependencies and context relationships. The task ID is auto-assigned per
project as `<CODE>-<NNNN>`. Tasks can be grouped into a hierarchy purely
through labels, and explicit links connect tasks across the hierarchy for
context discovery.

**Why this priority**: The authoring side that feeds User Story 1's "next task"
and context retrieval. Without tasks there is nothing to query; without links
the agent cannot discover related context or best practices.

**Independent Test**: Create several tasks, label and link them, then query by
label and walk links to confirm the graph is correct.

**Acceptance Scenarios**:

1. **Given** project `ATM` exists, **When** a user creates a task titled "Add
   claim command", **Then** the task is assigned id `ATM-0001` and the default
   status label `ATM:status:open` is applied automatically.
2. **Given** tasks `ATM-0001` and `ATM-0002` exist, **When** the user adds a
   `blocks` link from `ATM-0001` to `ATM-0002`, **Then** `ATM-0002` is treated
   as blocked by `ATM-0001` and excluded from "next task" results until
   `ATM-0001`'s status label is a terminal state (`ATM:status:done` or
   `ATM:status:cancelled`).
3. **Given** an epic `ATM-0010` with label `ATM:type:epic` exists, **When** the
   user creates `ATM-0011` with label `ATM:type:impl` and an `implements` link
   to `ATM-0010`, **Then** querying the epic's implementation tasks returns
   `ATM-0011`.
4. **Given** a convention doc task `ATM-0005` with label `ATM:kind:convention`
   exists, **When** the user creates `ATM-0012` with `ATM:type:bug` and a
   `documents` link from `ATM-0005` to `ATM-0012`'s type, **Then** the
   convention is discoverable from `ATM-0012` via label match (per US1) and via
   the link.

### User Story 4 - Track todos, followups, and discussions on a task (Priority: P4)

On any task, agents and humans can append structured sub-entries: todo items,
followup items, and discussion entries. Each entry records who (agent id or
human) created it and when. The human coordinator can mark followups resolved.

**Why this priority**: The coordination loop that lets agents and humans
collaborate over time on the same task without losing context.

**Independent Test**: Create a task, add todos/followups/discussions from both
an agent and a human actor, resolve a followup, and query the task's timeline.

**Acceptance Scenarios**:

1. **Given** task `ATM-0001` exists, **When** an agent adds a todo "Write tests
   for claim", **Then** the todo appears on the task with author = `<agent-id>`
   and a timestamp.
2. **Given** task `ATM-0001` exists, **When** a human adds a followup "Decide on
   storage format" assigned to themselves, **Then** the followup is recorded
   with author = `<human-id>`, assignee = `<human-id>`, status `open`, and an
   optional due timestamp.
3. **Given** an open followup exists on `ATM-0001`, **When** the human resolves
   it, **Then** its status becomes `resolved` with timestamp and resolver
   recorded.
4. **Given** task `ATM-0001` has discussions, **When** a user queries the task's
   full timeline, **Then** all todos, followups, and discussions are returned in
   chronological order with authors and timestamps.

### User Story 5 - Human coordinator reviews agent activity and steers work (Priority: P5)

The human coordinator views a dashboard of agent activity: which tasks are
claimed by which agents, which tasks have open followups needing human input,
and which tasks are labeled `ATM:status:review`. The human can then act:
reassign a task, respond to a followup, or approve/reject a task in review.
The TUI provides this view; the underlying data is the same the API exposes, so
an agent could also query it.

**Why this priority**: Closes the loop between agents and humans. Lower priority
than the agent self-serve path and the authoring/coordination primitives, but
it is what makes the system a *coordinator* rather than just a registry.

**Independent Test**: Have an agent claim tasks and set one to review status,
then a human runs the coordinator view and acts on the review request.

**Acceptance Scenarios**:

1. **Given** two agents have claimed two tasks and one task is labeled
   `ATM:status:review`, **When** the human opens the coordinator TUI view,
   **Then** they see the claimed tasks grouped by agent and a section listing
   tasks awaiting review.
2. **Given** a task is in review, **When** the human approves it, **Then** the
   task's status label becomes `ATM:status:done` and the requesting agent's work
   item is cleared.
3. **Given** a task is in review, **When** the human rejects it with a comment,
   **Then** the task's status label becomes `ATM:status:open`, the comment is
   recorded as a discussion entry by the human, and the agent sees the rejection
   on next query.

### User Story 6 - Manage tasks by any label axis (Priority: P4)

In the TUI Tasks tab, the user can group the task list by any namespace
discovered from the project's labels. Pressing `G` opens a picker listing all
namespaces in use (e.g. `type`, `owner`, `release`, `doc`, `status`, `area`);
selecting one regroups tasks under each value of that namespace. This is the
realization of "labels are the dynamic substrate": any namespace the user
invents becomes a management view on demand, with no pre-declared axis list and
no dedicated per-axis screens.

**Why this priority**: Makes the label-centric model tangible in the UI. Without
it, labels are just metadata; with it, any namespace the user invents becomes a
lens over the work.

**Independent Test**: Create tasks with labels across several namespaces, open
the Tasks tab, press `G`, select each namespace in turn, and verify the task
list regroups correctly.

**Acceptance Scenarios**:

1. **Given** tasks exist with labels `ATM:owner:alice`, `ATM:owner:bob`,
   `ATM:type:bug`, `ATM:type:impl`, **When** the user presses `G` in the Tasks
   tab and selects the `owner` namespace, **Then** tasks are displayed grouped
   under `ATM:owner:alice` and `ATM:owner:bob` headers, with counts.
2. **Given** the task list is grouped by `owner`, **When** the user presses `G`
   again and selects "none", **Then** the flat task list is restored.
3. **Given** tasks exist with no label in the selected namespace, **When** the
   user groups by that namespace, **Then** those tasks appear under a "(no
   <namespace> label)" sentinel group.

### Edge Cases

- Two agents run "next task" simultaneously and both attempt to claim the same
  task: the claim MUST be atomic; the second claim fails with a clear "already
  claimed" error and the agent is pointed back to "next task".
- A task is blocked by a task that no longer exists (deleted): the stale link is
  ignored for blocking purposes but preserved and surfaced as a warning so the
  human can clean it up.
- A project code collides with an existing project's code: project creation is
  rejected with a clear error; project codes are unique and immutable once set.
- A task references a label that was removed from the registry: the label is
  retained on existing tasks (soft removal) but rejected on new assignments;
  queries by that label still return the legacy tasks.
- The task counter would overflow four digits (`ATM-9999`): the counter widens
  to five digits automatically (`ATM-10000`); the ID format is `<CODE>-<N+>`
  where N is at least 4 digits, not fixed-width.
- An agent queries "next task" and no claimable task exists: the system returns
  a clear empty result (not an error) so the agent can idle gracefully.
- A task has no status label at all (corrupted/manual edit): it is treated as
  `ATM:status:open` for next-task eligibility (non-terminal), but a warning is
  surfaced on `show` so the human can repair it.
- A status label and another status label are both present on one task
  (corrupted/manual edit): the lexicographically smallest one wins for
  status-based logic; a warning is surfaced on `show`.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST store all task/project data locally in the
  machine-global `$ATM_HOME` directory (default `~/.config/atm`) as plain text
  that version-controls cleanly; no external database or network service is
  required. The store MUST be detachable: copying the global directory to
  another machine and re-running `atm` MUST reproduce the same state. A project
  is NOT 1:1 with a repo; a project MAY reference one or more repository paths
  (or none), and ATM's domain is limited to projects/tasks — it MUST NOT store
  user-specific artifacts.
- **FR-002**: System MUST expose every operation as a CLI command with
  machine-readable output (JSON) and human-readable output; the TUI is a thin
  client over the same operations.
- **FR-003**: System MUST support multiple projects identified by a unique short
  code matching `^[A-Z]{3,6}$` (3-6 uppercase ASCII letters); task IDs are
  `<CODE>-<NNNN>` with a per-project sequential counter that widens as needed.
- **FR-004**: `atm project create` MUST take only `--code` and `--name` (plus
  the internal `--actor`). It MUST NOT accept labels, axes, or repo paths at
  creation time. Labels and repos are added later via dedicated commands.
- **FR-005**: Users (agents and humans) MUST be able to create tasks and assign
  labels. Task status is expressed as a label on the project's `status`
  namespace (`<CODE>:status:<state>`); there is no dedicated `Status` field and
  no state machine — any status label may replace any other freely. New tasks
  are auto-labeled `<CODE>:status:open`. The recognized status values are:
  `open`, `in-progress`, `done`, `cancelled`, `review`. (v1's `blocked` status
  is removed; "blocked" is a derived state from an active `blocked-by` link, not
  a status value.)
- **FR-006**: System MUST support typed links between tasks: at minimum
  `blocks`/`blocked-by`, `related-to`, `implements`, `documents`. A task that is
  blocked by an undone task (one whose status label is not a terminal state —
  `done` or `cancelled`) is excluded from "next task".
- **FR-007**: Agents MUST be able to query the next claimable, non-blocked task
  for a project and to claim/unclaim tasks; claim is atomic and records the
  claiming actor and timestamp.
- **FR-008**: `show`/context retrieval MUST surface, alongside the task, the
  convention/best-practice docs whose labels intersect the task's labels,
  ordered by matched-label-count desc then ID asc. There is no designated
  type-axis; all namespaces are treated equally for convention matching.
- **FR-009**: Any task MUST be able to carry todos, followups, and discussion
  entries; each entry records author and timestamp.
- **FR-010**: The human coordinator MUST be able to view tasks grouped by
  claimant, tasks with open followups, tasks whose status label is `review`, and
  the project guide coverage/freshness, and act on review requests and guide
  edits (FR-018).
- **FR-011**: Every mutating operation MUST record the actor and a timestamp in
  an append-only history per task.
- **FR-012**: System MUST treat agents as first-class actors with stable
  identifiers; no action is attributed to an anonymous "system" actor.
- **FR-013**: System MUST allow querying tasks by label (single and multi-label
  intersection) and by link traversal.
- **FR-014**: System MUST keep deterministic command output for a given store so
  that agent runs and snapshot tests are reproducible.
- **FR-015**: Project conventions and task-type taxonomy MUST be data
  (project-configured labels and convention docs), not hard-coded behavior;
  adding a new task type requires no code change.
- **FR-016**: Each project MAY declare a `guide`: an ordered list of references
  to convention docs (tasks) or external file paths, grouped by named section.
  The guide is the set of project-wide information agents MUST read when working
  on any task in the project.
- **FR-017**: `next`/`show`/context retrieval MUST include the project's guide
  references in the returned context.
- **FR-018**: The human coordinator MUST be able to edit a project's guide and
  view guide coverage and freshness on the coordinator dashboard.
- **FR-019**: Labels MUST live in a single global registry at
  `$ATM_HOME/labels.json`. A label name is `<CODE>:<namespace>:<value>` (e.g.
  `ATM:type:bug`) or `<CODE>:<tag>` for free-form tags. The namespace segment is
  optional and open — any namespace the user invents is valid; there is no
  whitelist and no pre-declared axis list. A namespace "exists" iff at least one
  label with that prefix is in the registry.
- **FR-020**: The TUI Projects tab MUST render the project's labels grouped by
  namespace (sorted alphabetically; labels within a namespace sorted by value;
  unnamespaced tags under a `tags:` heading). Add/remove/describe actions
  operate on full label names; grouping is presentational.
- **FR-021**: The TUI Tasks tab MUST provide a "group by axis" mode: pressing
  `G` opens a picker of namespaces discovered from the project's labels;
  selecting one regroups the task list under each value of that namespace, with
  a sentinel group for tasks having no label in that namespace. Selecting
  "none" restores the flat list. Existing filters apply before grouping.

### Key Entities *(include if feature involves data)*

- **Project**: Identified by a unique short code (`^[A-Z]{3,6}$`) and a display
  name; owns a task counter and an optional guide. Does NOT own a label set
  (labels are global); does NOT carry a type-axis field.
- **Task**: The unit of work; has id `<CODE>-<NNNN>`, title, description,
  labels, links, claim, todos, followups, discussions, and append-only history.
  Has NO dedicated `Status` field — status is a label on the project's `status`
  namespace.
- **Label**: A global, hierarchical, project-prefixed tag
  (`<CODE>:<namespace>:<value>` or `<CODE>:<tag>`) in the global registry. Any
  namespace is valid; namespaces are discovered from labels in use, not
  pre-declared.
- **Link**: A typed directed relationship between two tasks (`blocks`,
  `blocked-by`, `related-to`, `implements`, `documents`).
- **Actor**: An agent or human with a stable identifier; recorded as the author
  of every entry and mutation.
- **Todo**: A checklist item belonging to a task with a done/not-done state and
  an author/timestamp.
- **Followup**: An assignable action item on a task with status `open`/
  `resolved`, optional assignee, optional due, and an author/timestamp.
- **Discussion entry**: A comment on a task recording a decision/question, with
  author and timestamp; flat (no threading) for v2.
- **History entry**: An append-only record of a task mutation with actor and
  timestamp.
- **Convention doc**: A task (or linked document) tagged with a
  `<CODE>:kind:convention` label; discoverable from other tasks via label match
  and explicit links.
- **Guide**: A project-level entity; an ordered list of references to convention
  docs (tasks) or external file paths, grouped by named section. Edited by the
  human coordinator; surfaced on the dashboard for coverage/freshness followup.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An agent can run a single command to retrieve a claimable,
  non-blocked task with full context in under 200ms on a project with 10,000
  tasks.
- **SC-002**: Two agents running "next task" concurrently never claim the same
  task; the second receives a clear "already claimed" response.
- **SC-002a**: All commands produce byte-identical output for the same store and
  arguments (reproducible for agents and snapshot tests).
- **SC-003**: A human can create a project and begin curating labels in under 1
  minute using only `atm project create` and `atm label add`.
- **SC-004**: All task data is stored as plain text in the machine-global
  `~/.config/atm` directory and can be exported/copied wholesale to another
  machine to reproduce the same state (detachability); the on-disk format
  produces clean, reviewable diffs when exported.
- **SC-005**: The TUI coordinator view renders the current agent activity, open
  followups, and review queue in under 1 second on a project with 1,000 tasks.
- **SC-006**: The TUI "group by axis" mode regroups 1,000 tasks under any
  namespace in under 100ms.