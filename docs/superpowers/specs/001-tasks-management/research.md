# Research: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-06-23 | **Spec revision**: v1.1.0

This document resolves the technical unknowns surfaced during planning for the ATM tasks management system. Each entry records the decision, rationale, and alternatives considered. Entries R1 and R4 were revised for spec v1.1.0 (machine-global storage and the added `Guide` entity); R9-R11 are new for v1.1.0.

## R1. Storage format, layout, and location

**Decision**: One file-per-record JSON under a machine-global store directory. The default location is `~/.config/atm` (XDG-style), overridable via the `ATM_HOME` env var or the `--store` global flag. Layout:

```
$ATM_HOME/                                  # default: ~/.config/atm
  projects/
    ATM.json                # project record (code, name, labels, counter, guide)
    ATM/
      tasks/
        ATM-0001.json       # one task record (todos/followups/discussions/history embedded)
        ATM-0002.json
      index.json            # optional, derived; not source of truth
  actors.json               # known actor ids (agents + humans), declared lazily on first use
```

**Rationale**: JSON is plain text, diffs cleanly, and is the lingua franca for agents and CLI tooling. One-file-per-task keeps writes localized (only the touched task file is rewritten), which makes concurrent edits and reviewable diffs straightforward. Embedding todos/followups/discussions/history inside the task record avoids a separate entity store for v1 and matches the YAGNI principle. The machine-global location (spec FR-001, design principle III) makes a project independent of any single repo: a project may span multiple repos, and copying `$ATM_HOME` to another machine reproduces the same state (detachability). SQLite was considered (below) and rejected for v1.

**Alternatives considered**:
- **Per-repo `.atm/` directory (the original v1.0 design)**: rejected because a project is NOT 1:1 with a repo and may span multiple repos; a per-repo store would force a project to be split across directories and break the single-counter invariant. The design principles (v1.1.0) explicitly mandates the machine-global location.
- **SQLite**: single-file DB with real query power and concurrent writes; rejected for v1 because it is binary (poor diff/review), overkill for the modest scale target (thousands of tasks), and violates the "text storage that version-controls well" design principles constraint. Can be revisited if scale forces it.
- **YAML**: more human-readable but indentation-sensitive and slower to parse; JSON's strictness is a feature for agent-produced data. YAML is available as an output *format* for humans without being the storage format.
- **One big JSON file per project**: simpler but every write rewrites the whole project; coarse and diff-noisy. File-per-task wins.
- **Directory-per-task**: over-engineered for v1; a single JSON file per task is enough.

## R2. Concurrency and atomic claim

**Decision**: Advisory file locking via `flock`-equivalent (on darwin/linux, `flock(2)` via `golang.org/x/sys/unix`; a Windows `LockFileEx` shim is out of scope for v1). Every store mutation takes an exclusive lock on the project's lock file (`$ATM_HOME/projects/<CODE>.lock`) for the duration of the read-modify-write. The "next + claim" sequence is a single locked operation: under the project lock, find the next claimable task, mark it claimed, write it, release. Two concurrent claimants therefore never both succeed on the same task; the second sees the updated store and is pointed to the next task or an empty result.

**Rationale**: The design principles demands deterministic, atomic claims (SC-002). File-level locking is the simplest mechanism that works for a local-trust, few-writer system. A single global lock would serialize all projects needlessly; a per-project lock is the right granularity.

**Alternatives considered**:
- **Optimistic concurrency (version stamps)**: agents would retry on conflict; adds complexity and non-determinism. Rejected.
- **Embedded DB transactions (SQLite)**: rejected with the storage choice.
- **No locking (last-write-wins)**: violates SC-002. Rejected.

## R3. IDs and counter management

**Decision**: Task ID format `<CODE>-<N>` where `N` is a decimal integer with *no* fixed width but rendered with at least 4 digits for sortability up to 9999, then natural width above (`ATM-10000`). The counter is stored in the project record and incremented under the project lock when a task is created. IDs are immutable and never reused, even if a task is deleted (deletion is a status, not removal, for v1).

**Rationale**: The user spec called for `ATM-0001`-style IDs. Fixed 4-digit width is impossible to keep across all scales; zero-padding to a minimum of 4 balances human ergonomics and unlimited growth (the spec's edge case explicitly calls for widening). Keeping deleted-task IDs reserved preserves referential integrity for links and history.

**Alternatives considered**:
- **UUIDs**: opaque, not human-friendly, breaks the user's explicit `<CODE>-<NNNN>` request. Rejected.
- **Fixed 4-digit forever**: overflows at 9999; the spec's edge case explicitly calls for widening. Rejected.
- **Reuse IDs of deleted tasks**: breaks link integrity. Rejected.

## R4. Actor identity

**Decision**: Actors are identified by a string `<kind>:<id>` where kind is `agent` or `human` (e.g. `agent:claude-1`, `human:alice`). The actor is passed to every command via a global `--actor` flag (or `ATM_ACTOR` env var). Actors are registered lazily: the first time an actor id appears in a mutation, it is appended to `$ATM_HOME/actors.json` with a first-seen timestamp. No password, no authn in v1 (local-trust assumption).

**Rationale**: FR-012 requires agents to be first-class actors with stable identifiers. The `<kind>:<id>` namespace is self-describing, sorts/queries easily, and avoids collisions between agent and human name spaces. Lazy registration keeps the CLI ergonomic (no "create actor" step) while still recording provenance.

**Alternatives considered**:
- **Numeric actor IDs**: loses the kind signal and requires a lookup table; worse for agents reading logs. Rejected.
- **Full authn (tokens/keys)**: explicitly out of scope per the assumptions (local-trust). Rejected for v1.
- **Config-file-only actor (no flag)**: inflexible when one human runs several agents with different ids from the same shell. Rejected.

## R5. TUI framework and API layering

**Decision**: Go + Bubble Tea (`github.com/charmbracelet/bubbletea`) for the TUI, as the user requested. The architecture is a single binary with three layers: (1) a `store` Go package that owns the on-disk format, locking, and all read/write logic; (2) a `cli` layer (using `cobra`) that exposes every operation as a subcommand with JSON and human output; (3) a `tui` layer (Bubble Tea) that calls into the `store` package (or a thin `app` service layer above it) to render interactive views. There is no separate HTTP API server in v1: the "API" the spec refers to is the `store` Go package plus the CLI surface. Agents integrate by shelling out to the CLI (JSON mode) — which is how agents naturally consume tools.

**Rationale**: A local-first, agent-native tool's most natural API is the CLI itself (stdin/stdout JSON). Running a long-lived HTTP server per workspace would fight the local-first principle and complicate agent usage. The `store` package is the stable, versioned API for in-process clients (the TUI); the CLI is the stable API for out-of-process clients (agents). This satisfies the Superpowers workflow's "API-first, every operation reachable via a command" without dragging in a server runtime.

**Alternatives considered**:
- **HTTP/JSON API server (localhost)**: more conventional "API-first", but adds lifecycle complexity (port management, startup, shutdown) for little agent benefit over shelling out. Rejected for v1; can be layered on later behind the same `store` package if needed.
- **gRPC**: even heavier; rejected.
- **TUI-only (no CLI)**: violates design principles (every operation must be a command) and blocks agent use. Rejected.
- **Non-Bubble-Tea TUI (tview, gocui)**: user explicitly preferred Bubble Tea. Rejected.

## R6. Links and context discovery

**Decision**: Links are typed directed edges stored as an array on the task record. Supported types for v1: `blocks` (A `blocks` B means B cannot start until A is done; B carries an implied `blocked-by` reverse edge, computed not stored), `related-to` (symmetric, stored on one side and traversed both ways), `implements` (A `implements` B; B is the parent, e.g. an epic), `documents` (A `documents` B or A `documents` label/area). The "next task" query excludes any task with an unmet `blocked-by` (i.e. a `blocks` edge from a non-done task). Context discovery for a task collects: (a) all linked tasks (both directions), (b) all convention-doc tasks whose labels intersect the task's labels (especially the type-axis label), (c) the project guide references (FR-016/017 — see R9), and (d) the task's full todo/followup/discussion timeline.

**Rationale**: The user asked for links that help agents discover context and find best-practices. Storing edges on the task record keeps everything in one file and avoids a separate graph store. Convention matching via label intersection makes best-practices data-driven (a convention doc is just a task tagged `kind:convention` with labels matching the work it applies to), satisfying FR-015.

**Alternatives considered**:
- **Separate graph store**: more query power, more complexity. Rejected for v1.
- **Storing only reverse edges**: awkward for authoring ("create B, add a blocked-by A"). Storing the forward `blocks` edge on A is more natural. Chosen.
- **Free-form link types**: flexible but loses type-aware behavior (blocking). Typed set for v1, extensible later via project config.

## R7. Labels and the task-type axis

**Decision**: A label is either a free-form tag (`refactor`) or a namespaced `namespace:value` pair (`type:bug`, `area:cli`). Each project declares an allowed label set; one namespace may be designated the `type` axis (so `type:epic`, `type:user-story`, `type:impl`, `type:bug` are all values of the type axis). Labels are soft-removed: removing from the allowed set stops new assignments but retains the label on existing tasks (with a warning surfacing retained usage). Convention matching and type-aware behavior key off the type axis.

**Rationale**: The user asked for label-driven organization and hierarchy. A namespaced scheme with one designated type axis gives both free-form grouping and structured type semantics without a separate "type" field on the task — keeping the model minimal (YAGNI) while satisfying FR-004/FR-015.

**Alternatives considered**:
- **Separate `type` field on Task + separate `labels`**: more rigid, two sources of truth for "what kind of task is this". Rejected.
- **Fully free-form labels with no axis**: loses type-aware behavior (convention matching, the epic/impl hierarchy). Rejected.
- **Hard-coded task types**: violates FR-015. Rejected.

## R8. Determinism and output stability

**Decision**: All list/query output is sorted by a stable key (task ID lexicographically by project then numeric value; ties broken by created timestamp) and rendered deterministically (map keys sorted before serialization). `--output json` produces stable, pretty-printed JSON with sorted object keys. This makes snapshot testing feasible and agent runs reproducible (SC-002a).

**Rationale**: design principle IV and SC-002a. Go's default map iteration order is randomized, so explicit sorting is required everywhere maps are serialized.

**Alternatives considered**:
- **Insertion-order output**: requires an order field on every entity and complicates queries that filter then sort. Rejected; a stable sort key is simpler and good enough.
- **Non-determinism with a "sort" flag**: violates SC-002a by default. Rejected.

## R9. The project Guide (the agent-context harness) *(new in v1.1.0)*

**Decision**: Add a `Guide` entity at the project level. A guide is an ordered list of references, grouped by named section. Each reference points either to a convention-doc *task* (by task id, same project) or to an *external file path* (absolute, or relative to `$ATM_HOME`). Sections have string names (e.g. `conventions`, `work-conduct`, `communication`, `testing`). The guide is stored inside the project record (`guide` field) so it is read/written under the same project lock as the rest of the project.

```
"guide": {
  "sections": [
    { "name": "conventions", "refs": [
      { "kind": "task", "target": "ATM-0005" },
      { "kind": "file", "target": "/abs/path/to/CONVENTIONS.md" }
    ]},
    { "name": "testing", "refs": [
      { "kind": "task", "target": "ATM-0012" }
    ]}
  ],
  "updated_at": "2026-06-23T12:00:00Z",
  "updated_by": "human:alice"
}
```

`next`/`show --with-context` always include the guide in the returned context (FR-017), alongside the per-task label-matched convention docs (FR-008). The human coordinator edits the guide (add/remove/reorder refs, name sections) via CLI (FR-018). The coordinator dashboard surfaces guide coverage (which sections are empty) and freshness (which referenced convention docs are stale — `updated_at` beyond a project-configured threshold — or missing/deleted).

**Rationale**: The clarifications session (2026-06-23) decided the agent-context harness is a project-level `guide` entity: an ordered list of references grouped by section. Modeling it on the project record keeps it co-located with the project it belongs to and serialized under the project lock, with no new top-level entity store (YAGNI). Two reference kinds cover the two cases in the clarification: convention-doc *tasks* (discoverable, label-matchable, versioned in the task system) and *external file paths* (e.g. a repo's AGENTS.md or a testing guide that lives outside ATM). Storing the guide on the project means `next`/`show` can return it with one read after loading the project.

**Alternatives considered**:
- **A separate top-level `guides/` store**: more moving parts, another lock surface, no benefit over embedding on the project. Rejected.
- **Guide as a task (`type:guide`)**: blurs the task/guide boundary; a guide is project-scoped config, not a unit of work; it would also make `show` context retrieval recursive and awkward. Rejected.
- **Free-form markdown blob on the project**: loses the ordered, sectioned, reference-typed structure the clarification asked for, and makes coverage/freshness checks (FR-018) impossible to compute. Rejected.
- **Guide refs as raw strings (no `kind`)**: loses the ability to distinguish a task ref (resolvable, freshness-checkable) from a file path (only existence-checkable). The typed `kind` makes the dashboard's stale/missing checks well-defined. Chosen.

## R10. Store path resolution (machine-global default) *(new in v1.1.0)*

**Decision**: Store resolution order: (1) `--store <path>` global flag if set; (2) `ATM_HOME` env var if set; (3) `~/.config/atm` default. There is NO walk-up-from-CWD search: the store is machine-global, not per-repo, so CWD is irrelevant. `atm init` creates/verifies an empty store at the resolved path and is idempotent. All commands resolve the store the same way before running.

**Rationale**: Design principles v1.1.0 principle III mandates the machine-global location and that a project is not 1:1 with a repo. A walk-up search would silently pick a per-repo store and re-introduce the very coupling the amendment removed. A single, explicit resolution rule (flag > env > default) is simpler, deterministic, and detachable.

**Alternatives considered**:
- **Walk-up `.atm/` search then default**: rejected for v1.1.0 — it reintroduces per-repo coupling and makes the active store depend on CWD, which hurts reproducibility (SC-002a) and detachability.
- **Config-file-only store path**: less ergonomic for one-off commands and tests; the flag/env default covers the common cases. Rejected as the sole mechanism (kept as a possible future addition behind the env var).

## R11. Guide freshness and coverage on the dashboard *(new in v1.1.0)*

**Decision**: The coordinator dashboard (`review queue` + open followups, extended for the guide) computes two guide metrics per project: **coverage** (which sections have zero refs) and **freshness** (for each `kind:task` ref, compare the referenced task's `updated_at` to `now - project.guide_freshness_threshold`; if older, or if the task is deleted/missing, flag it stale/missing). `guide_freshness_threshold` is an optional duration field on the project (e.g. `"720h"` = 30 days); if unset, freshness is reported as `unknown` rather than stale. The dashboard is a read-only view composed from existing store reads; no new mutation path.

**Rationale**: FR-018 requires the human coordinator to see guide coverage and freshness. Reusing the task's `updated_at` (already recorded on every mutation) avoids a separate "guide doc last reviewed" field and keeps the model minimal. An unset threshold yielding `unknown` (not `stale`) avoids false alarms when a project hasn't configured freshness expectations.

**Alternatives considered**:
- **Per-ref `last_reviewed_at` timestamps**: more accurate but adds a new field per ref and a new mutation (mark reviewed) — YAGNI for v1. The task's `updated_at` is a good enough proxy.
- **Hard-coded 30-day threshold**: violates "conventions are data, not hard-coded" (FR-015 analog). Rejected; the threshold is project-configured.
- **No freshness tracking**: fails FR-018. Rejected.