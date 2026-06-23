# Feature Specification: Tasks Management System

**Feature Branch**: `001-tasks-management`

**Created**: 2026-06-23

**Status**: Draft

**Input**: User description: "Local-storage, CLI/API first for agents to query next tasks to work on and relevant context, as well as tracking todo tasks, followup items, discussions and coordinate with human coordinator. CLI tool with TUI and commands (prefer Go bubbletea). Tasks are units organized into projects; task ID named by project code (e.g. ATM-0001). Humans manage projects and labels; tasks organized by labels and hierarchically via labels. Tasks can be linked together to help agents discover context and find project-wide best practices defined by humans (PR conventions, managing task types like epic/user-story/implementation-task/bug)."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Agent queries next task and context (Priority: P1)

An AI agent working in a repository runs a single command to ask the system "what should I work on next?". The system returns the highest-priority, unclaimed, non-blocked task in the current project, including its full context: description, labels, links to related tasks, and pointers to any project-wide best-practice / convention documents that apply based on the task's labels and type. The agent can claim the task (so other agents do not pick it up) and later unclaim it if it abandons the work.

**Why this priority**: This is the core agent-facing value of the whole product. Without it, agents have no entry point into the task system and cannot self-serve work. It is the single slice that makes the system usable end-to-end for its primary user (the agent).

**Independent Test**: Can be fully tested by seeding a project with at least one task, running `next` / `claim` / `show` commands, and verifying the returned task is the expected one and its context includes the linked tasks and matching convention docs.

**Acceptance Scenarios**:

1. **Given** a project `ATM` exists with tasks `ATM-0001` (open, unblocked, label `type:bug`) and `ATM-0002` (open, blocked by `ATM-0001`), **When** an agent runs the "next task" command for `ATM`, **Then** the system returns `ATM-0001` (not `ATM-0002`) with its description, labels, and links.
2. **Given** `ATM-0001` is returned, **When** the agent runs the "claim" command for `ATM-0001` as agent `claude-1`, **Then** `ATM-0001` is recorded as claimed by `claude-1` with a timestamp, and a second "next task" command does not return `ATM-0001`.
3. **Given** a project convention doc tagged with label `type:bug` exists, **When** the agent runs the "show" command for `ATM-0001`, **Then** the response includes a pointer to that convention doc because the task's labels match.
4. **Given** `ATM-0001` is claimed by `claude-1`, **When** `claude-1` runs the "unclaim" command, **Then** `ATM-0001` returns to the claimable pool and reappears in "next task".

---

### User Story 2 - Human manages projects and labels (Priority: P2)

A human coordinator creates a project with a short code (e.g. `ATM`), configures the labels available in that project (both free-form tags and structured labels that encode type/hierarchy such as `type:epic`, `type:user-story`, `type:impl`, `type:bug`, and hierarchy labels like `area:cli`, `area:tui`), and defines which labels carry special meaning (e.g. which label identifies the task "type" axis, which label defines the hierarchy/grouping axis). The human can later edit the project's label set and task-type taxonomy without migrating task data.

**Why this priority**: Projects and labels are the organizing substrate every other story depends on. Agents cannot be pointed at a project until a human has created it and declared its labels. This story must come immediately after the agent workflow to establish the management side.

**Independent Test**: Can be tested by creating a project via command, adding/renaming/removing labels, declaring a label as the "type" axis, and verifying tasks can be created with those labels and that querying by label returns the right set.

**Acceptance Scenarios**:

1. **Given** no project with code `ATM` exists, **When** a human runs the "create project" command with code `ATM` and a display name, **Then** the project is created and subsequent tasks are numbered `ATM-0001`, `ATM-0002`, etc.
2. **Given** project `ATM` exists, **When** the human adds labels `type:epic`, `type:user-story`, `type:impl`, `type:bug`, `area:cli`, `area:tui`, **Then** those labels are available for assignment to tasks in `ATM`.
3. **Given** the human declares label namespace `type` as the "task-type axis" for `ATM`, **When** an agent creates a task with label `type:bug`, **Then** the system treats it as a bug for type-aware behavior (filtering, convention matching).
4. **Given** tasks exist with label `area:cli`, **When** the human removes the `area:cli` label from the project's allowed set, **Then** existing tasks keep the label (soft removal) but new tasks cannot be assigned it; the system warns the human about retained usage.

---

### User Story 3 - Create tasks and organize them via labels and links (Priority: P3)

An agent or human creates a task in a project, assigns it labels (including a type label for hierarchy), and links it to other tasks to express dependencies ("blocked-by", "blocks") and context relationships ("related-to", "documents", "implements"). The task ID is auto-assigned per project as `<CODE>-<NNNN>`. Tasks can be grouped into a hierarchy purely through labels (e.g. all tasks with `area:cli` form a group; an `epic` is any task with `type:epic`), and explicit links connect tasks across the hierarchy for context discovery.

**Why this priority**: This is the authoring side that feeds User Story 1's "next task" and context retrieval. Without tasks there is nothing to query; without links the agent cannot discover related context or best practices.

**Independent Test**: Can be tested by creating several tasks, labeling and linking them, then querying by label and walking links to confirm the graph is correct.

**Acceptance Scenarios**:

1. **Given** project `ATM` exists, **When** a user creates a task titled "Add claim command", **Then** the task is assigned id `ATM-0001` (next available in `ATM`) and default status `open`.
2. **Given** tasks `ATM-0001` and `ATM-0002` exist, **When** the user adds a `blocks` link from `ATM-0001` to `ATM-0002`, **Then** `ATM-0002` is treated as blocked by `ATM-0001` and excluded from "next task" results until `ATM-0001` is done.
3. **Given** an epic `ATM-0010` with label `type:epic` exists, **When** the user creates `ATM-0011` with label `type:impl` and a `implements` link to `ATM-0010`, **Then** querying the epic's implementation tasks returns `ATM-0011`.
4. **Given** a convention doc task `ATM-0005` with label `kind:convention` exists, **When** the user creates `ATM-0012` with `type:bug` and a `documents` link from `ATM-0005` to `ATM-0012`'s type, **Then** the convention is discoverable from `ATM-0012` via label match (per US1) and via the link.

---

### User Story 4 - Track todos, followups, and discussions on a task (Priority: P4)

On any task, agents and humans can append structured sub-entries: todo items (checklist items tied to the task), followup items (action items needing later attention, assignable to an actor), and discussion entries (threaded or flat comments recording decisions and questions). Each entry records who (agent id or human) created it and when. The human coordinator can mark followups resolved and close discussions.

**Why this priority**: This is the coordination loop that lets agents and humans collaborate over time on the same task without losing context. It builds on the prior stories (tasks must exist first) and is what makes the system a coordinator rather than a static registry.

**Independent Test**: Can be tested by creating a task, adding todos/followups/discussions from both an agent and a human actor, resolving a followup, and querying the task's timeline.

**Acceptance Scenarios**:

1. **Given** task `ATM-0001` exists, **When** an agent adds a todo "Write tests for claim", **Then** the todo appears on the task with author = `<agent-id>` and a timestamp.
2. **Given** task `ATM-0001` exists, **When** a human adds a followup "Decide on storage format" assigned to themselves, **Then** the followup is recorded with author = `<human-id>`, assignee = `<human-id>`, status `open`, and a due timestamp (optional).
3. **Given** an open followup exists on `ATM-0001`, **When** the human resolves it, **Then** its status becomes `resolved` with timestamp and resolver recorded.
4. **Given** task `ATM-0001` has discussions, **When** a user queries the task's full timeline, **Then** all todos, followups, and discussions are returned in chronological order with authors and timestamps.

---

### User Story 5 - Human coordinator reviews agent activity and steers work (Priority: P5)

The human coordinator views a dashboard of agent activity: which tasks are claimed by which agents, which tasks have open followups needing human input, and which tasks an agent has marked as needing review. The human can then act: reassign a task, respond to a followup, or approve/reject a task marked for review. The TUI provides this view; the underlying data is the same the API exposes, so an agent could also query it.

**Why this priority**: This closes the loop between agents and humans. It is lower priority than the agent self-serve path and the authoring/coordination primitives, but it is what makes the system a *coordinator* rather than just a registry. It depends on US1-US4 being in place.

**Independent Test**: Can be tested by having an agent claim tasks and request review on one, then a human running the coordinator view and acting on the review request.

**Acceptance Scenarios**:

1. **Given** two agents have claimed two tasks and one has requested review on a third, **When** the human opens the coordinator TUI view, **Then** they see the claimed tasks grouped by agent and a section listing tasks awaiting review.
2. **Given** a task is awaiting review, **When** the human approves it, **Then** the task moves to `done` and the requesting agent's work item is cleared.
3. **Given** a task is awaiting review, **When** the human rejects it with a comment, **Then** the task returns to `open` (or `in-progress`), the comment is recorded as a discussion entry by the human, and the agent sees the rejection on next query.

---

### Edge Cases

- What happens when two agents run "next task" simultaneously and both attempt to claim the same task? The claim MUST be atomic; the second claim fails with a clear "already claimed" error and the agent is pointed back to "next task".
- What happens when a task is blocked by a task that no longer exists (deleted)? The stale link is ignored for blocking purposes but preserved and surfaced as a warning so the human can clean it up.
- What happens when a project code collides with an existing project's code? Project creation is rejected with a clear error; project codes are unique and immutable once set.
- What happens when a task references a label that was removed from the project's allowed set? The label is retained on existing tasks (soft removal) but rejected on new assignments; queries by that label still return the legacy tasks.
- What happens when the task counter would overflow four digits (`ATM-9999`)? The counter widens to five digits automatically (`ATM-10000`); the ID format is `<CODE>-<N+>` where N is at least 4 digits, not fixed-width.
- What happens when an agent queries "next task" and no claimable task exists? The system returns a clear empty result (not an error) so the agent can idle gracefully.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST store all data locally in the workspace as plain text that version-controls cleanly; no external database or network service is required for v1.
- **FR-002**: System MUST expose every operation as a CLI command with machine-readable output (JSON) and human-readable output; the TUI is a thin client over the same operations.
- **FR-003**: System MUST support multiple projects identified by a unique short code (e.g. `ATM`); task IDs are `<CODE>-<NNNN>` with a per-project sequential counter that widens as needed.
- **FR-004**: Humans MUST be able to create, list, and edit projects and their label sets, including declaring which label namespace is the task-type axis.
- **FR-005**: Users (agents and humans) MUST be able to create tasks, assign labels, and set a status (`open`, `in-progress`, `done`, `blocked`, `review`, `cancelled` at minimum).
- **FR-006**: System MUST support typed links between tasks: at minimum `blocks`/`blocked-by`, `related-to`, `implements`, `documents`. A task that is `blocked-by` an undone task is excluded from "next task".
- **FR-007**: Agents MUST be able to query the next claimable, non-blocked task for a project and to claim/unclaim tasks; claim is atomic and records the claiming actor and timestamp.
- **FR-008**: `show`/context retrieval MUST surface, alongside the task, the convention/best-practice docs whose labels match the task's labels (especially the task-type label).
- **FR-009**: Any task MUST be able to carry todos (checklist items), followups (assignable action items with status `open`/`resolved`), and discussion entries; each entry records author (agent id or human id) and timestamp.
- **FR-010**: The human coordinator MUST be able to view tasks grouped by claimant, tasks with open followups, and tasks awaiting review, and act on review requests (approve/reject with comment).
- **FR-011**: Every mutating operation MUST record the actor (agent id or human id) and a timestamp in an append-only history per task.
- **FR-012**: System MUST treat agents as first-class actors with stable identifiers; no action is attributed to an anonymous "system" actor.
- **FR-013**: System MUST allow querying tasks by label (single and multi-label intersection) and by link traversal (e.g. "all tasks this task implements" / "all tasks blocking this task").
- **FR-014**: System MUST keep deterministic command output for a given store so that agent runs and snapshot tests are reproducible.
- **FR-015**: Project conventions and task-type taxonomy MUST be data (project-configured labels and convention docs), not hard-coded behavior; adding a new task type requires no code change.

### Key Entities *(include if feature involves data)*

- **Project**: Identified by a unique short code and a display name; owns a label set and a task counter. Tasks live inside a project and are numbered by that project's counter.
- **Task**: The unit of work; has id `<CODE>-<NNNN>`, title, description, status, labels, links, and an append-only history. Lives in exactly one project.
- **Label**: A namespaced tag (`namespace:value`, e.g. `type:bug`, `area:cli`) belonging to a project's allowed set; one namespace per project is designated the task-type axis. Free-form tags (no namespace) are also allowed.
- **Link**: A typed directed relationship between two tasks (`blocks`, `blocked-by`, `related-to`, `implements`, `documents`). Traversable in both directions for context discovery.
- **Actor**: An agent or human with a stable identifier; recorded as the author of every entry and mutation.
- **Todo**: A checklist item belonging to a task with a done/not-done state and an author/timestamp.
- **Followup**: An assignable action item on a task with status `open`/`resolved`, optional assignee, optional due, and an author/timestamp; the human-coordination primitive.
- **Discussion entry**: A comment on a task recording a decision/question, with author and timestamp; flat (no threading) for v1.
- **History entry**: An append-only record of a task mutation (status change, label add/remove, link add/remove, claim, etc.) with actor and timestamp.
- **Convention doc**: A task (or linked document) tagged as a convention/best-practice; discoverable from other tasks via label match and explicit links.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An agent can run a single command to retrieve a claimable, non-blocked task with full context in under 200ms on a project with 10,000 tasks.
- **SC-002**: Two agents running "next task" concurrently never claim the same task; the second receives a clear "already claimed" response.
- **SC-002a**: All commands produce byte-identical output for the same store and arguments (reproducible for agents and snapshot tests).
- **SC-003**: A human can create a project, configure its labels, and define a task-type taxonomy in under 2 minutes using only CLI commands.
- **SC-004**: All task data is stored as plain text in the workspace and produces clean, reviewable diffs under version control.
- **SC-005**: The TUI coordinator view renders the current agent activity, open followups, and review queue in under 1 second on a project with 1,000 tasks.
- **SC-006**: Adding a new task type (e.g. `type:spike`) requires zero code changes — only project label configuration and (optionally) a convention doc.
- **SC-007**: Every mutating command records actor and timestamp so the full history of any task is reconstructable from the store.

## Assumptions

- The primary users are AI agents and a small number of human coordinators, not a large human team; scale targets are modest (thousands of tasks per project, tens of projects).
- All access is local-trust: there is no multi-tenant isolation or authn/authz in v1. Actor identity is declared by the caller (e.g. `--actor agent:claude-1` or `--actor human:alice`) and trusted. Hardening access control is a later concern.
- The store lives in a single workspace directory (e.g. `.atm/` under the repo); concurrent writers are expected to be few and are serialized by file locking at the store level.
- The TUI is built with Go and Bubble Tea as stated in the user description; the API is the Go package the TUI calls, also exposed via the CLI.
- Convention/best-practice docs are modeled as tasks (or links to external files) rather than a separate top-level entity, to keep the model minimal.
- Time tracking, boards, sprints, and remote sync (Jira/GitHub Issues) are explicitly out of scope for v1.
- Go is the implementation language per the user's preference for Bubble Tea; the spec remains technology-agnostic but assumes a single-binary CLI/TUI.