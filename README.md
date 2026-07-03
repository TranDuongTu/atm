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
`--actor <id>` (free-form; e.g. `claude`, `alice`) or read
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
atm label seed    --project <CODE> [--actor <id>]                    # (re)apply the 17 default labels; idempotent
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
special-cases the documented namespaces. The system treats `ATM:context:agent`
identically to `ATM:type:bug`. See the "Conventions" section below for the
suggested seed namespaces.

## Conventions

v2's vision is that workflow lives outside the system — in agent prompts and
human habits, not in the store. But a fresh agent landing in a project needs a
deterministic entry point, and a fresh project needs its labels seeded.
Onboarding is the act of seeding index tasks and seed labels; the conventions
below tell humans and agents how to do that using only the label substrate.
No bootstrap command, no reserved labels, no repo-side files, no store-side
repo paths. The conventions are documented, not code-reserved: the system
treats `ATM:context:agent` identically to `ATM:type:bug`.

These conventions are **advisory** — `atm conventions` prints the same guide,
but nothing in the store validates or special-cases these namespaces.

### Suggested seed namespaces

A fresh project is auto-seeded with the 17 default labels below on
`atm project create` (and re-applied idempotently by
`atm label seed --project <CODE>` or the Labels tab `[S]` key). Templated
namespaces (`repo:<name>`, `doc:<name>`, `claimed-by:<agent>`, `blocks:<ID>`,
`related:<ID>`) are created on demand — they depend on project-specific values
and are NOT seeded as concrete labels.

| Namespace             | Examples                          | Purpose                                                          |
|-----------------------|-----------------------------------|------------------------------------------------------------------|
| `status:`             | open, todo, in-progress, done, blocked, review | workflow states — labels only, no state machine        |
| `type:`               | bug, feature, task, chore         | task categorization                                              |
| `priority:`           | high, medium, low                 | optional prioritization                                          |
| `context:documentation` | `ATM:context:documentation`     | the labeled task contains documentation about the project        |
| `context:repository`  | `ATM:context:repository`          | the labeled task contains a pointer to a code repository         |
| `context:agent`       | `ATM:context:agent`               | agent direction when navigating the project                     |
| `context:fixit`       | `ATM:context:fixit`               | something on this task should be reviewed, updated, or altered   |
| `repo:<name>`         | `ATM:repo:atm`                    | index task pointing at a repo — created on demand, not seeded   |
| `doc:<name>`          | `ATM:doc:architecture`            | index task pointing at a doc/resource — created on demand, not seeded |
| `claimed-by:<agent>`  | `ATM:claimed-by:claude`           | who's working on what — last-writer-wins, no conflict detection  |
| `blocks:<ID>`, `related:<ID>` | `ATM:blocks:ATM-0002`     | task relationships via labels — created on demand, not seeded   |

### Agent code-of-conduct (label hygiene)

Agents working in an ATM project follow these rules to keep the label substrate
legible for humans and other agents:

1. **Read before you write.** Run `atm label list --project <CODE>` and read
   every label's description before introducing any new label. The existing
   labels are the project's vocabulary; reuse them whenever one fits your
   intent.
2. **Default setup is the baseline.** The seeded labels (status, type,
   priority, context) cover the common cases. Prefer them. Do not reinvent
   `status:open` as `state:open` or `wf:open`.
3. **Invent only when nothing fits.** If no existing label captures your
   intent, you may create a new one — agents are free to self-organize. But
   before you do, ask yourself: would a human reviewing the Labels tab
   understand why this label exists?
4. **State the intention in the label description.** When you create a new
   label, also call
   `atm label add --name <CODE>:<ns>:<value> --description "<one sentence: why this label exists>"`.
   The description is the intention record. A label with no description is a
   flag for human review: "agent introduced this but didn't explain why."
5. **One label, one meaning.** Don't use the same label string to mean
   different things across tasks. If your intent diverges from an existing
   label's description, create a new label with a distinct name and a
   description that distinguishes it.
6. **Humans reconcile.** The Labels tab is the human's review surface. If you
   see labels that overlap, contradict, or lack descriptions, edit or remove
   them there. Agents follow the rules above; humans curate.

### First-time human sequence

1. `atm tui` (auto-inits the store)
2. Create the project (Add in the Projects tab). Project create auto-seeds the
   17 default labels with descriptions, so the Labels tab is populated from
   the start.
3. Create seed index tasks (`context:agent`, `context:repository`,
   `context:documentation`) and initial work tasks, labeling as you go. The
   human curates labels in the Labels tab.

### Agent first-contact sequence

1. `atm conventions` — read this guide, including the code-of-conduct.
2. `atm label list --project <CODE>` — read every label's description first to
   understand the project's vocabulary before exploring tasks. Labels are the
   project's language; knowing them makes every task query meaningful.
3. `atm task list --project <CODE> --label <CODE>:context:agent` — get agent
   directions for working in this project.
4. `atm task list --project <CODE> --label <CODE>:context:repository` /
   `:context:documentation` — discover repository pointers and documentation.
5. `atm task list --project <CODE> --label <CODE>:status:open` — get open work.

A fresh agent that does not yet know the project's namespaces runs the
label-list step first and follows the descriptions.

### Re-seeding defaults

`atm label seed --project <CODE>` or the Labels tab `[S]` key re-applies the
default set idempotently — existing descriptions are preserved, and any new
defaults introduced in a release are added.

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
