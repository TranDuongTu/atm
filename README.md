# ATM — Agent Tasks Management

A JIRA-like task management system, but purpose-built for **AI agents**.

- **TUI-friendly**: first-class terminal/CLI ergonomics, not GUI-first.
- **API-first**: every operation is exposed via a queryable API; the TUI and any other front-end are thin clients over it.
- **Agent-native**: tasks, workflows, and states are modeled for agentic software-development lifecycles (SDLCs), not just human ticketing.

## Status

Bootstrapping. See `AGENTS.md` for how AI agents should work in this repo.

## Build & verify

```sh
make build
make test
```

## Verify

Run the full verification step used by the AGENTS.md workflow:

```sh
make verify
```

This runs `make build && make test`.

The current implementation lives under `internal/store` (the stable in-process
API), `internal/cli` (the stable out-of-process API), and `internal/tui` (a
thin Bubble Tea client). The binary entrypoint is `cmd/atm`. The first feature
spec, plan, and task list live under `specs/001-tasks-management/`.