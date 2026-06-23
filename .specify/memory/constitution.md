# ATM Constitution

## Core Principles

### I. API-First

Every operation MUST be exposed via a queryable API that agents and humans can call programmatically. The TUI and any other front-end are thin clients over this API. Local storage is the source of truth for task data. Text in/out protocol: args/stdin -> stdout (machine formats like JSON plus human formats); errors -> stderr. Every feature must be reachable both via a command and via the API surface.

### II. Agent-Native

Tasks, workflows, and states are modeled for agentic software-development lifecycles, not just human ticketing. Agents MUST be able to: discover the next task to work on, retrieve relevant context (linked tasks, project conventions, best-practice guides), post todos/followups, and coordinate with a human coordinator. The data model MUST treat agents as first-class actors (not anonymous "system" users) and record who (agent id or human) performed each action.

### III. Local-First & Offline-Capable

All task data is stored locally in the workspace. No network dependency is required for core operation. Synchronization with remote systems (if any) is a non-goal for v1 and must not leak into the core API. The storage format MUST be text (machine-readable) so it diffs, merges, and version-controls cleanly.

### IV. Stability & Versioning

The API surface MUST stay stable and versioned; the TUI consumes it. Breaking changes require a version bump and a migration path. Internal implementation details are not part of the API and may change freely. Conventions (PR workflow, task types, labels) are project-configured data, not hard-coded behavior.

### V. Simplicity (YAGNI)

Start with the minimal model that supports the agent workflow: Projects, Tasks, Labels, Links, Discussions, and a human-coordinator loop. Defer everything else (boards, time tracking, sprints, remote sync) until a concrete need is proven. Complexity must be justified against a simpler alternative.

## Constraints

- **No emojis** in code, specs, commits, or stored data.
- **Text storage**: on-disk format is plain text that version-controls well (JSON or YAML are both acceptable; pick one per store and stay consistent).
- **Single binary**: the CLI/TUI ships as one binary; subcommands are the primary interaction mode.
- **Deterministic output**: the same command with the same store produces the same output (for agent reproducibility and snapshot testing).

## Development Workflow

1. Pick a task (this repo dogfoods itself).
2. Find or author the corresponding spec via Spec Kit.
3. Implement against the spec.
4. Verify: run lint/typecheck/tests before declaring done.
5. Update the spec only if requirements genuinely changed; never to match code.

## Governance

The constitution supersedes ad-hoc practice. Amendments require documentation, approval, and a migration plan. All PRs/reviews MUST verify compliance with these principles. Complexity beyond the minimal model must be justified against the YAGNI principle.

**Version**: 1.0.0 | **Ratified**: 2026-06-23 | **Last Amended**: 2026-06-23