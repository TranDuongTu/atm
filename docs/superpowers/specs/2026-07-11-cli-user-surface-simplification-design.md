# CLI User Surface Simplification — Design Spec

**Status:** Approved for spec draft after ATM-0084 brainstorming.
**Task:** ATM-0084.
**Date:** 2026-07-11.

## Driver

ATM's current CLI exposes the store/API surface, launcher internals, and human
entrypoints at the same level. `atm --help` reads like an internal command map:
`tui`, `developing`, `manager`, `task`, `label`, `store`, `persona`, `activity`,
`index`, `embed`, `search`, `vocabulary`, `inquiry`, and more all compete for
attention.

The user-facing workflow should be smaller and opinionated:

1. Run `atm` to open the TUI.
2. Run `atm <agent> --project <CODE>` to start a developer session.
3. Run `atm manage <agent> --project <CODE> --<action>` to start a manager
   session in a named management mode.

This is an intentional breaking cleanup. There is no alias-first migration for
the old user-facing launcher commands.

## Goals

- Make `atm` with no arguments open the TUI everywhere.
- Replace `atm developing <agent>` with top-level developer launch commands:
  `atm codex`, `atm claude`, `atm opencode`, `atm ollama`.
- Replace `atm manager` with `atm manage`.
- Remove user-facing `--actor` and `--dry-run` from launcher commands.
- Keep `--persona` as the only user-facing identity override on launchers.
- Make manager sessions explicit by requiring exactly one action flag.
- Rename manager actions and prompt language to user-facing terms:
  `tracking request` -> `tracking`, `inquiry` -> `asking`, and
  `vocabulary` -> `glossary`.
- Rewrite README to focus on user actions first, then the reason ATM exists.

## Non-goals

- No removal of the lower-level task/label/store/search/index/persona commands
  from the binary in this change. They remain the stable API/advanced surface.
- No change to the store data model.
- No change to the TUI's internal actor behavior beyond the existing `admin`
  persona default.
- No alias/deprecation period for the old launcher commands.
- No broad documentation of every API command in README.

## User-facing command surface

### TUI

```
atm
```

No arguments launches the Bubble Tea TUI. This hard-replaces the user-facing
`atm tui` entrypoint.

The direct TUI/human default persona remains `admin`, producing the same actor
floor already established by actor convention enforcement:
`admin@tui:unset` for TUI writes and `admin@cli:unset` for direct non-launcher
CLI writes.

### Developer sessions

```
atm codex    --project <CODE> [--persona <NAME>] [-- <agent args...>]
atm claude   --project <CODE> [--persona <NAME>] [-- <agent args...>]
atm opencode --project <CODE> [--persona <NAME>] [-- <agent args...>]
atm ollama   --project <CODE> --integration <HOST> [--persona <NAME>] [-- <agent args...>]
```

These hard-replace:

```
atm developing codex
atm developing claude
atm developing opencode
atm developing ollama
```

Developer launch semantics remain the same as the current developing launcher:
ATM renders the developing context, sets ATM environment variables, and launches
the requested host agent. Extra host-agent arguments still pass through after
`--` unchanged.

The default persona is `developer`. The default actor floor is
`developer@<agent>:unset`, with the running agent still instructed by context to
replace `:unset` with its real model when stamping ATM mutations. If
`--persona <NAME>` is supplied, that persona replaces `developer` and must be a
registered persona.

The launcher no longer exposes `--actor` or `--dry-run`.

### Manager sessions

```
atm manage codex    --project <CODE> --planning   [--persona <NAME>] [-- <agent args...>]
atm manage codex    --project <CODE> --grooming   [--persona <NAME>] [-- <agent args...>]
atm manage codex    --project <CODE> --tracking   [--persona <NAME>] [-- <agent args...>]
atm manage codex    --project <CODE> --asking     [--persona <NAME>] [-- <agent args...>]
atm manage codex    --project <CODE> --glossary   [--persona <NAME>] [-- <agent args...>]
atm manage codex    --project <CODE> --onboarding [--persona <NAME>] [-- <agent args...>]

atm manage claude   --project <CODE> --<action> [--persona <NAME>] [-- <agent args...>]
atm manage opencode --project <CODE> --<action> [--persona <NAME>] [-- <agent args...>]
atm manage ollama   --project <CODE> --integration <HOST> --<action> [--persona <NAME>] [-- <agent args...>]
```

`<action>` is exactly one of:

- `--planning`
- `--grooming`
- `--tracking`
- `--asking`
- `--glossary`
- `--onboarding`

The command errors if zero action flags or more than one action flag are
provided. This keeps manager sessions intentional and prevents a vague
"manager, do whatever" mode from becoming the default.

The default persona is `manager`. The default actor floor is
`manager@<agent>:unset`; for `ollama`, the agent segment is `ollama` and the
integration stays separate launch configuration. If `--persona <NAME>` is
supplied, that persona replaces `manager` and must be registered.

The launcher no longer exposes `--actor` or `--dry-run`.

## Manager prompt changes

`internal/manager/context_v1.md` keeps the same responsibilities but renames
them to match the new command flags:

- **Planning** — review the open backlog and keep statuses honest: what is
  ready, what needs information, what is blocked, and what is in flight.
- **Grooming** — prioritize the backlog so the most important work surfaces
  first.
- **Tracking** — a developing agent hands over progress, decisions, questions,
  or friction; find the task it extends and curate it with the right comment,
  split, label, or task update.
- **Asking** — answer project questions by recalling ledger knowledge grounded
  in cited task/comment IDs.
- **Glossary** — maintain the project's shared language: recurring domain
  terms, short definitions, and naming consistency across tasks, comments,
  labels, and docs.
- **Onboarding** — when first introduced to a repo/project, learn it and
  organize it into a substrate a later agent can pick up.

The previous prompt labels are removed:

- `Tracking request`
- `Inquiry`
- `Vocabulary`

## Existing command removal

The following user-facing command trees are removed from Cobra registration:

- `atm tui`
- `atm developing`
- `atm manager`

`atm manage` is the new manager tree. `atm <agent>` is the new developer tree.

The lower-level commands remain:

- `task`
- `label`
- `project`
- `store`
- `search`
- `index`
- `embed`
- `persona`
- `activity`
- `vocabulary`
- `inquiry`
- `conventions`
- `version`
- `completion`
- `help`

They are no longer presented as the README's primary workflow. They remain
discoverable through `atm help` / `atm <cmd> --help` for scripts, agents, and
advanced users.

## README rewrite

The README becomes minimal and user-action focused.

### Order

1. Product name and one-sentence summary.
2. User actions:
   - `atm`
   - `atm <agent> --project <CODE>`
   - `atm manage <agent> --project <CODE> --<action>`
3. Manager action list.
4. Why ATM exists.
5. Install/build/verify basics.
6. Advanced/API note pointing to `atm help` and `atm conventions`.

### User actions section

The first command block should be:

```
atm
atm codex --project ATM
atm manage codex --project ATM --planning
atm manage codex --project ATM --grooming
atm manage codex --project ATM --tracking
atm manage codex --project ATM --asking
atm manage codex --project ATM --glossary
atm manage codex --project ATM --onboarding
```

The README can mention `claude`, `opencode`, and `ollama --integration <HOST>`
as equivalent launch hosts, but it should not enumerate every low-level API
command.

### Why ATM exists

This section comes after actions. It should preserve the user's reasons in a
polished form:

- I work across multiple projects at once, and some projects span multiple
  repositories.
- I use multiple coding agents and switch between them regularly to manage
  cost, context, and token usage.
- I need to resume or hand off work across agents with minimal guidance.
- I switch machines frequently, so I need a centralized, immutable, append-only
  ledger that can be shared.
- I do not want a traditional Jira-style ticket system built around human
  browsing workflows. I want to ask my agents and have them work from the
  ledger.

### Removed README content

The README should not include a long front-page command reference for:

- task CRUD
- label CRUD
- store internals
- persona CRUD
- activity aggregation
- vocabulary write internals
- index/embed/search internals
- actor alias/migration

Those details belong in `atm help`, `atm conventions`, specs, or future
advanced docs.

## Error handling

- `atm` with no args launches the TUI. If the TUI fails to initialize, it
  returns the same errors as the current `atm tui`.
- `atm <unknown>` continues to report Cobra unknown-command usage unless it is a
  known low-level command.
- `atm <agent>` requires `--project`; missing project is a usage error.
- `atm ollama` requires `--integration`.
- `atm manage <agent>` requires `--project` and exactly one manager action flag.
- `atm manage ollama` requires `--integration`.
- `--actor` and `--dry-run` on new launch commands are unknown flags.

## Testing

- Root command with no args launches the TUI path. Use a test seam rather than
  starting a real terminal program.
- `atm codex|claude|opencode --project FOO` produces the same launch argv/env as
  the old `atm developing <agent> --project FOO` path, minus removed flags.
- `atm ollama --project FOO --integration codex` preserves ollama behavior.
- `atm manage <agent> --project FOO --planning|...` renders manager context with
  an action signal for the selected mode.
- `atm manage <agent> --project FOO` without an action errors.
- `atm manage <agent> --project FOO --planning --grooming` errors.
- `atm manage ollama --project FOO --onboarding` without `--integration` errors.
- `atm tui`, `atm developing`, and `atm manager` are no longer registered.
- README examples use only the new user-facing commands.
- Prompt tests/goldens are updated for `Tracking`, `Asking`, and `Glossary`.

## Rollout

This is a breaking CLI cleanup. Release notes should call out:

- `atm` now opens the TUI.
- Use `atm <agent> --project <CODE>` instead of `atm developing <agent>`.
- Use `atm manage <agent> --project <CODE> --<action>` instead of
  `atm manager <agent>`.
- `--actor` and `--dry-run` are no longer launch-command flags.
- Manager language now uses tracking, asking, and glossary.
