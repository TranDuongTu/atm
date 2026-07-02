# ATM — Agent Tasks Management

A JIRA-like task management system, but purpose-built for **AI agents**.

- **TUI-friendly**: first-class terminal/CLI ergonomics, not GUI-first.
- **API-first**: every operation is exposed via a queryable API; the TUI and any other front-end are thin clients over it.
- **Agent-native**: tasks and workflow states are modeled for agentic software-development lifecycles (SDLCs), not just human ticketing.
- **Pure label-substrate**: v2 has no intrinsic workflow knowledge. Status, type, priority, ownership, and relationships are all labels. The system does not enforce a state machine, claim semantics, or review gates — those live in agent prompts and human habits.

## Status

v2 (pure label-substrate). The store, CLI, and TUI have been rewritten to remove all intrinsic workflow knowledge: there is no status field, no claim/unclaim, no review queue, no links entity, no guide, no actor entity. Labels are the single substrate. See `AGENTS.md` for how AI agents should work in this repo, and `docs/superpowers/specs/2026-07-02-tasks-management-v2-design.md` for the design.

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
location reproduces the same state and the same output.

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
`--actor <id>` (free-form; e.g. `claude`, `alice`, `agent:foo`) or read
`ATM_ACTOR`. Errors go to stderr with a non-zero exit code and a stable
`{"error":{"code":"...","message":"..."}}` envelope in JSON mode.

Global flags:

- `--store <path>`: store directory (overrides `ATM_HOME`).
- `--output json|text`: output format. Default `text`.
- `--actor <id>`: actor performing the operation (free-form). Env: `ATM_ACTOR`.
  Required for mutating commands; optional for read commands.
- `--quiet`: suppress non-essential stdout in text mode.

Exit codes: `0` success; `1` generic; `2` usage; `3` not-found; `4` conflict.

### Store / init

```
atm init [--actor <id>]            # idempotent; creates labels.json + actors.json + projects/
atm store path                     # print resolved store path
```

### Projects

A project owns a namespace prefix (`<CODE>:`) for its labels. Creation is
minimal — only `--code` and `--name`; labels are added later via `label add`.

```
atm project create    --code <CODE> --name <NAME> [--actor <id>]
atm project list
atm project show      --code <CODE>
atm project set-name  --code <CODE> --name <NAME> [--actor <id>]
atm project remove    --code <CODE> [--actor <id>]   # zero-task guard
```

`--code` must match `^[A-Z]{3,6}$` and be unique across the store.

### Labels

Labels are the single substrate. They are global, hierarchical, and
project-prefixed: `<CODE>:<namespace>:<value>` (two segments) or `<CODE>:<tag>`
(one segment). Namespaces are open — there is no whitelist and no type-axis.
`label add` is an upsert (auto-registers); `label remove` is a soft removal
that reports `retained_usage` if any task still carries the label.

```
atm label add     --name <L> [--description <DESC>] [--actor <id>]   # upsert; auto-registers
atm label remove  --name <L> [--actor <id>]                          # soft; reports retained_usage
atm label list    [--project <CODE>] [--namespace <NS>]              # namespace requires --project
atm label show    --name <L>
```

Label name regex: `^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$`.

### Tasks

```
atm task create          --project <CODE> --title <TITLE> [--description <DESC>] [--label <L>]... [--actor <id>]
atm task list            [--project <CODE>] [--label <L>]... [--facets] [--actor <id>]
atm task show            --id <ID> [--actor <id>]
atm task set-title       --id <ID> --title <TITLE> [--actor <id>]
atm task set-description --id <ID> --description <DESC> [--actor <id>]
atm task label add       --id <ID> --label <L> [--actor <id>]
atm task label remove    --id <ID> --label <L> [--actor <id>]
atm task remove          --id <ID> [--actor <id>]
```

`create` assigns the next id `<CODE>-<N>` (4-digit zero-padded up to 9999, then
natural width). `--label` takes the full project-prefixed name (e.g.
`ATM:type:bug`) and is repeatable. `list` AND-intersects label filters; exact
tokens return a flat list sorted by id (project-then-numeric). Wildcard
suffixes (e.g. `ATM:status:*`) combined with `--facets` drive faceted grouping
with multi-membership — each facet value becomes a group, and a task may appear
under multiple groups. There is no `--status` flag and no status field: status
is the `ATM:status:<state>` label axis.

### Conventions

```
atm conventions          # print the onboarding guide + suggested seed namespaces
```

Conventions are **advisory only** — nothing in the store validates or
special-cases the documented namespaces. The system treats `ATM:context:start-here`
identically to `ATM:type:bug`. See the "Conventions" section below for the
suggested seed namespaces.

## Conventions

v2's vision is that workflow lives outside the system — in agent prompts and
human habits, not in the store. A fresh agent landing in a project needs a
deterministic entry point, and a fresh project needs its labels seeded.
Onboarding is the act of seeding index tasks and seed labels; the conventions
below tell humans and agents how to do that using only the label substrate.

These conventions are **advisory** — `atm conventions` prints the same guide,
but nothing in the store validates or special-cases these namespaces.

### Suggested seed namespaces

A fresh project should populate these namespaces (via labels on seed index
tasks and work tasks):

| Namespace             | Examples                          | Purpose                                                          |
|-----------------------|-----------------------------------|------------------------------------------------------------------|
| `status:`             | open, todo, in-progress, done, blocked, review | workflow states — labels only, no state machine        |
| `type:`               | bug, feature, task, chore         | task categorization                                              |
| `priority:`           | high, medium, low                 | optional prioritization                                          |
| `repo:<name>`         | `ATM:repo:atm`                    | index task whose description says where to find the repo and what it means — the repo→project binding, expressed in the label substrate |
| `doc:<name>`          | `ATM:doc:architecture`            | index task pointing at a doc/resource and how to use it          |
| `context:always-read` | `ATM:context:always-read`         | pointer to the always-read context markdown (replaces the deleted v1 Project Guide) |
| `context:start-here`  | `ATM:context:start-here`          | the single entry-point task a fresh agent queries first; its description is the "read this first" pointer |
| `claimed-by:<agent>`  | `ATM:claimed-by:claude`           | who's working on what — last-writer-wins, no conflict detection  |
| `blocks:<ID>`         | `ATM:blocks:ATM-0002`             | task relationships via labels (replaces v1 Links)               |
| `related:<ID>`        | `ATM:related:ATM-0003`            | non-blocking task relationships via labels                       |

### First-time human sequence

1. `atm tui` (auto-inits the store)
2. Create the project (Add in the Projects tab)
3. Create a few seed index tasks (`start-here`, `repo:<name>`, `doc:<name>`,
   `context:always-read`) and initial work tasks, labeling as you go. The act
   of seeding these tasks populates the `status`/`type`/`repo`/`doc`/`context`
   namespaces organically — there is no separate bootstrap step.

### Agent first-contact sequence

1. `atm conventions` — read the guide.
2. `atm task list --project <CODE> --label <CODE>:context:start-here` — get the
   entry-point pointer and follow it.
3. `atm task list --project <CODE> --label <CODE>:repo:*` / `:doc:*` /
   `:context:*` — discover index tasks for repos, docs, and always-read context.
4. `atm task list --project <CODE> --label <CODE>:status:open` — get open work.

A fresh agent that does not yet know the project's namespaces runs the
`start-here` query first (one deterministic label) and follows whatever the
`start-here` task's description points at.

## TUI

`atm tui` is a first-class management surface that mirrors every CLI op. The
typical TUI actor is a human consulting/steering; the typical CLI actor is an
AI agent. See
`docs/superpowers/specs/2026-07-02-tasks-management-v2-tui-mockups-design.md`
for screens/keymaps.

```
atm tui [--store <path>] [--actor <id>]
```

The Tasks tab has no grouping toggle — filter wildcards (e.g.
`ATM:status:*`) drive faceted grouping with multi-membership; exact filter
tokens give a flat paged list.

## Dogfooding

ATM dogfoods itself: the ATM project and its follow-on tasks are tracked in the
machine-global store. The bootstrap is idempotent and opt-in (it is **not** run
by `make verify`):

```sh
make dogfood                            # builds, then seeds the default store
ATM_HOME=/tmp/atm-dogfood make dogfood  # bootstrap against a throwaway store
```

See `scripts/dogfood.sh` for the exact commands.

## Architecture

The implementation lives under `internal/store` (the stable in-process API),
`internal/cli` (the stable out-of-process API), and `internal/tui` (a thin
Bubble Tea client). The binary entrypoint is `cmd/atm`. The v2 design spec and
TUI mockups live under `docs/superpowers/specs/`.