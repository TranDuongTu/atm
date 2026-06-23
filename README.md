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

## Store resolution

The store is a machine-global directory of plain JSON files (no database). A
project is **not** 1:1 with a repo; one store may hold many projects spanning
many repos. The store is detachable: copying the directory wholesale to another
location reproduces the same state and the same output (SC-004/FR-001).

Resolution order:

1. `--store <path>` flag
2. `ATM_HOME` env var
3. `~/.config/atm` default

There is **no** walk-up-from-CWD search; the store is machine-global.

## CLI

The `atm` binary is the stable, versioned out-of-process API for agents, and
the surface the TUI wraps. Output format is selected globally with
`--output json|text` (default `text`); JSON output is deterministic (sorted
keys, stable whitespace, RFC 3339 UTC timestamps). All mutating commands accept
`--actor <id>` (or read `ATM_ACTOR`). Errors go to stderr with a non-zero exit
code and a stable `{"error":{"code":"...","message":"..."}}` envelope in JSON
mode. See `specs/001-tasks-management/contracts/cli.md` for the full contract.

Global flags:

- `--store <path>`: store directory (overrides `ATM_HOME`).
- `--output json|text`: output format. Default `text`.
- `--actor <id>`: actor performing the operation (e.g. `agent:claude-1`,
  `human:alice`). Env: `ATM_ACTOR`. Required for mutating commands; optional
  for read commands.
- `--quiet`: suppress non-essential stdout in text mode.

Exit codes: `0` success; `1` generic; `2` usage; `3` not-found; `4` conflict.

### Store / init

```
atm init [--store <path>] [--actor <id>]   # idempotent; creates actors.json + projects/
atm store path                             # print resolved store path
```

### Projects

```
atm project create --code <CODE> --name <NAME> [--type-axis <NS>] [--label <L>]... [--repo-path <PATH>]... [--actor <id>]
atm project list
atm project show --code <CODE>
atm project set-name --code <CODE> --name <NAME> [--actor <id>]
atm project set-type-axis --code <CODE> --namespace <NS> [--actor <id>]
atm project repo add    --code <CODE> --path <PATH> [--actor <id>]
atm project repo remove --code <CODE> --path <PATH> [--actor <id>]
atm project label add    --code <CODE> --label <L> [--description <DESC>] [--actor <id>]
atm project label remove --code <CODE> --label <L> [--actor <id>]   # soft removal; reports retained_usage
atm project label list   --code <CODE>
```

`--code` must match `^[A-Z][A-Z0-9-]{1,15}$` and be unique.

### Project guide

The guide is the always-read agent-context harness for a project (FR-016/017/018).

```
atm project guide show            --code <CODE>
atm project guide section add     --code <CODE> --name <NAME> [--actor <id>]
atm project guide section rename  --code <CODE> --name <NAME> --new-name <NEW> [--actor <id>]
atm project guide section remove  --code <CODE> --name <NAME> [--actor <id>]
atm project guide section move    --code <CODE> --name <NAME> --before <OTHER|>   # --before "" = move to end
atm project guide ref add    --code <CODE> --section <NAME> --kind task|file --target <T> [--actor <id>]
atm project guide ref remove --code <CODE> --section <NAME> --kind task|file --target <T> [--actor <id>]
atm project guide ref move    --code <CODE> --section <NAME> --kind task|file --target <T> --before <OTHER_REF_TARGET|>
atm project guide set-freshness --code <CODE> --threshold <DURATION|unset> [--actor <id>]
atm project guide status        --code <CODE>     # coverage (empty sections) + freshness (stale/missing refs)
```

All guide edits set `guide.updated_at`/`updated_by` and append a `guide-updated`
entry to the project history.

### Tasks

```
atm task create --project <CODE> --title <TITLE> [--description <DESC>] [--label <L>]... [--actor <id>]
atm task show --id <ID> [--with-context]
atm task list [--project <CODE>] [--label <L>]... [--status <S>] [--assignee <ACTOR>] [--claimant <ACTOR>]
atm task set-status      --id <ID> --status <S> [--actor <id>]
atm task set-title       --id <ID> --title <TITLE> [--actor <id>]
atm task set-description --id <ID> --description <DESC> [--actor <id>]
atm task label add    --id <ID> --label <L> [--actor <id>]
atm task label remove --id <ID> --label <L> [--actor <id>]
```

`create` assigns the next id `<CODE>-<N>` (4-digit zero-padded up to 9999, then
natural width); status defaults to `open`. `show --with-context` includes the
project guide, linked tasks (both directions), matching convention docs (label
intersection, weighted by the type axis), and the
todo/followup/discussion timeline. `list` filters AND-intersect labels and
sorts by id (project-then-numeric).

### Agent workflow (claim / next)

```
atm task next    --project <CODE> [--actor <id>] [--claim]   # highest-priority claimable, non-blocked, non-claimed, non-done task
atm task claim   --id <ID> [--actor <id>]                    # atomic; conflict if already claimed by another actor
atm task unclaim --id <ID> [--actor <id>]
```

`next` always includes the project guide so the agent receives the always-read
harness in the same call. Priority for v1 = blocked-by count ascending, then
`created_at` ascending (oldest first). Returns an empty result (not an error)
if none claimable.

### Links

```
atm task link add    --id <ID> --type <T> --target <ID> [--actor <id>]
atm task link remove --id <ID> --type <T> --target <ID> [--actor <id>]
atm task link list   --id <ID>
```

`type` enum: `blocks` / `related-to` / `implements` / `documents`.
`related-to` deduplicates symmetrically. `blocks` implies a computed
`blocked-by` reverse edge (not stored). Stale targets (deleted task) are
preserved with a warning. `link list` returns both stored edges and computed
reverse edges, tagged with `direction: out|in`.

### Todos / Followups / Discussions

```
atm task todo add       --id <ID> --text <TEXT> [--actor <id>]
atm task todo toggle    --id <ID> --todo <TODO_ID> [--actor <id>]
atm task followup add    --id <ID> --text <TEXT> [--assignee <ACTOR>] [--due <RFC3339>] [--actor <id>]
atm task followup resolve --id <ID> --followup <F_ID> [--actor <id>]
atm task discussion add --id <ID> --text <TEXT> [--actor <id>]
atm task timeline       --id <ID>
```

`timeline` merges todos, followups, discussions, and history sorted by
timestamp ascending then entry id; each entry carries a `kind` discriminator
(`todo|followup|discussion|history`).

### Review (human coordinator)

```
atm review request    --id <ID> [--actor <id>]            # status -> review
atm review approve    --id <ID> [--comment <TEXT>] [--actor <id>]   # status -> done
atm review reject     --id <ID> [--comment <TEXT>] [--actor <id>]   # status -> in-progress (or open); comment recorded as discussion
atm review queue      [--project <CODE>]                  # tasks with status review, grouped by claimant
atm review followups  [--project <CODE>]                  # open followups, optionally filtered
atm review dashboard  [--project <CODE>]                  # queue + open followups + guide status (FR-010/FR-018)
```

### Actors

```
atm actor list
atm actor show --id <ACTOR>
```

Actors are registered lazily on first mutation; there is no `actor create`.
`show` returns first-seen and a summary of claimed tasks / open followups.

## TUI

`atm tui` is a first-class management surface that mirrors every CLI op (FR-002).
See `specs/001-tasks-management/tui-mockups.md` for screens/keymaps and
`specs/001-tasks-management/contracts/tui.md` for the CLI/TUI parity matrix.

```
atm tui [--store <path>] [--actor <id>]
```

## Dogfooding

ATM dogfoods itself: the ATM project and its follow-on tasks are tracked in the
machine-global store. The bootstrap is idempotent and opt-in (it is **not** run
by `make verify`):

```sh
make dogfood                    # uses the built bin/atm against the default store
ATM_HOME=/tmp/atm-dogfood make dogfood   # bootstrap against a throwaway store
```

See `scripts/dogfood.sh` for the exact commands.

## Architecture

The current implementation lives under `internal/store` (the stable in-process
API), `internal/cli` (the stable out-of-process API), and `internal/tui` (a
thin Bubble Tea client). The binary entrypoint is `cmd/atm`. The first feature
spec, plan, and task list live under `specs/001-tasks-management/`.