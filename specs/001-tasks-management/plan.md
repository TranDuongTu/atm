# Implementation Plan: Tasks Management System

**Branch**: `001-tasks-management` | **Date**: 2026-06-23 | **Spec**: specs/001-tasks-management/spec.md

**Input**: Feature specification from `specs/001-tasks-management/spec.md`

## Summary

ATM is a local-first, agent-native tasks management CLI/TUI. The primary requirement is an agent-facing API (the CLI plus a Go `store` package) to discover the next task, claim it, and retrieve context (linked tasks, matching convention docs, todo/followup/discussion timeline). The technical approach (see `research.md`) is a single Go binary with three layers: a `store` package owning one-file-per-task JSON storage under `.atm/` with per-project file locking; a `cli` (cobra) exposing every operation as a subcommand with deterministic JSON/text output; and a Bubble Tea TUI as a thin client over `store`. Projects own a label set and a task counter; tasks are numbered `<CODE>-<N>`; typed links model blocking and context relationships; todos/followups/discussions/history are embedded in the task record with full actor provenance.

## Technical Context

**Language/Version**: Go 1.22+ (chosen per user preference for Bubble Tea; recorded as an assumption in the spec).

**Primary Dependencies**:
- `github.com/spf13/cobra` - CLI subcommand framework.
- `github.com/charmbracelet/bubbletea` + `lipgloss` - TUI.
- `github.com/google/uuid` or an in-house counter - ids for embedded entries (a per-task counter is simpler and chosen for determinism; uuid rejected for non-determinism of string form).
- `golang.org/x/sys/unix` - `flock` for per-project locking (darwin/linux); a Windows shim is out of scope for v1 (darwin/linux only for now, documented).
- Standard library `encoding/json` with sorted keys for deterministic output.

**Storage**: Local text files under `.atm/`. One file-per-task JSON; one project record JSON; one actors JSON. No database. See `research.md` R1 and `data-model.md`.

**Testing**: Go `testing` (unit) + a thin golden/snapshot helper for deterministic CLI output (compare against committed `testdata/*.json`). Integration tests run the compiled binary against a temp store. No external test framework.

**Target Platform**: macOS and Linux developer workstations (darwin/linux). Windows is not a v1 target (the locking primitive differs); documented as an assumption.

**Project Type**: CLI tool with an embedded TUI (single binary).

**Performance Goals**: `task next` and `task show --with-context` under 200ms on a project with 10,000 tasks (SC-001). TUI coordinator view under 1s on 1,000 tasks (SC-005). All operations on local files; no network.

**Constraints**: Offline-capable (no network dependency). Deterministic output (SC-002a). Concurrent agent claims must be atomic (SC-002). No emojis in code/data/commits. Plain-text store that version-controls cleanly.

**Scale/Scope**: Thousands of tasks per project, tens of projects, tens of actors. Modest scale by design (YAGNI).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. API-First | Pass | Every operation is a CLI subcommand with JSON/text output; the `store` package is the in-process API; the TUI is a thin client over it. No HTTP server in v1 (research R5). |
| II. Agent-Native | Pass | Agents are first-class actors (`agent:<id>`); `next`/`claim`/`show --with-context` are the agent entry points; provenance recorded on every mutation. |
| III. Local-First & Offline | Pass | All data under `.atm/`; no network dependency; text format diffs cleanly. |
| IV. Stability & Versioning | Pass | The CLI surface (`contracts/cli.md`) is the versioned API; `store` package is the versioned in-process API; internals may change. |
| V. Simplicity (YAGNI) | Pass | Minimal model: Project, Task, Label, Link, Actor, Todo, Followup, DiscussionEntry, HistoryEntry. Boards/sprints/time-tracking/remote-sync explicitly deferred. |

No violations. No complexity tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/001-tasks-management/
├── plan.md              # this file
├── research.md          # Phase 0: technical decisions and rationale
├── data-model.md        # Phase 1: entities, fields, invariants
├── quickstart.md        # Phase 1: runnable validation scenarios
├── contracts/
│   └── cli.md           # Phase 1: CLI command schema (the versioned API)
└── tasks.md             # Phase 2 (/speckit.tasks output, created next)
```

### Source Code (repository root)

```text
cmd/
└── atm/
    └── main.go              # binary entrypoint: wires cobra root + TUI subcommand

internal/
├── store/                    # the in-process API (versioned, stable)
│   ├── store.go             # Store type, open/close, path resolution
│   ├── lock.go              # per-project file locking
│   ├── project.go           # Project CRUD + label set + type axis
│   ├── task.go             # Task CRUD, status transitions, id assignment
│   ├── link.go             # typed links + reverse-edge computation
│   ├── claim.go            # atomic next+claim under project lock
│   ├── context.go          # show-with-context: links + conventions + timeline
│   ├── entry.go            # todo/followup/discussion/timeline
│   ├── review.go           # review request/approve/reject + queue
│   ├── actor.go            # lazy actor registration
│   ├── history.go         # append-only history
│   └── query.go           # list/filter (label intersection, status, assignee, claimant)
├── cli/                     # the out-of-process API (versioned, stable)
│   ├── root.go             # cobra root + global flags (--store/--output/--actor/--quiet)
│   ├── output.go           # json/text renderers (deterministic)
│   ├── project.go          # project + project label subcommands
│   ├── task.go            # task subcommands (create/show/list/set-*/label/link)
│   ├── workflow.go         # next/claim/unclaim
│   ├── entry.go           # todo/followup/discussion/timeline
│   ├── review.go          # review subcommands
│   ├── actor.go           # actor subcommands
│   └── errors.go         # stable error codes/exit codes
└── tui/                    # thin client over store (Bubble Tea)
    ├── app.go             # root model + alt-screen setup
    ├── coordinator.go      # review queue + agent activity view
    └── components/        # list/detail/input widgets

testdata/
├── golden/                # committed expected JSON outputs (determinism)
└── stores/               # fixture stores for integration tests

go.mod
go.sum
README.md
AGENTS.md
```

**Structure Decision**: Single Go module, `cmd/atm` for the binary, `internal/store` for the stable in-process API, `internal/cli` for the stable out-of-process API, `internal/tui` for the thin TUI client. The `internal/` layout keeps the implementation details out of the public import path, reinforcing that the *CLI* and the *store package's exported types* are the API, not the wiring. Tests live alongside packages plus a `testdata/` for golden fixtures.

## Complexity Tracking

> None. Constitution Check passed with no violations.