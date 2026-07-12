# Init Plugin and Project Simplification - Design Spec

**Status:** Approved for implementation.
**Task:** ATM-0097.
**Date:** 2026-07-12.

## Driver

ATM currently splits first-time setup across separate command surfaces:
`atm init` initializes the store, while manager/developer plugin installation
lives under launcher-specific plugin commands. Developer and manager launchers
also require the project to exist before a session can start. That makes the
first useful path too manual: initialize, create project, install developing
plugin, install manager subagent, then launch.

The user-facing path should be smaller:

1. Run `atm init` to initialize the store and install ATM agent integration for
   one or more selected agents.
2. Run `atm <agent> --project <CODE>` or
   `atm manage <agent> --project <CODE> --<action>`.
3. If the project does not exist yet, the launcher creates it.

## Goals

- Make `atm init` the only user-facing plugin setup command.
- Let `atm init` install both developer and manager plugins for multiple agents
  in one run.
- Hard-remove the user-facing `atm manage plugin ...` command tree.
- Auto-create a missing project from developer and manager launchers.
- Preserve idempotency: existing stores, existing plugins, and existing
  projects remain valid and are not recreated.

## Non-goals

- No support for `ollama` plugin installation. Ollama is an integration launcher,
  not a host with ATM plugin assets.
- No change to the task/label/store data model.
- No migration or compatibility alias for removed plugin commands.

## Command Surface

`atm init` keeps its existing store initialization behavior and guides
interactive users through plugin setup. In text mode with TTY stdin and no
explicit `--agent`, it prints an `ATM setup` section, lists supported agents,
and accepts one or more comma-separated numbers/names or `all`. Pressing Enter
skips plugin installation after store initialization.

For scripts and non-interactive callers, `atm init` also supports explicit
repeatable flags:

```
atm init --agent codex
atm init --agent codex --agent claude
atm init --agent all
atm init --dry-run --agent opencode
```

Supported agent values are `codex`, `claude`, `opencode`, and `all`.
`--agent` is repeatable. `all` expands to all supported plugin hosts. Duplicate
agents are collapsed in stable order: `opencode`, `codex`, `claude`.

With no `--agent` and non-TTY stdin, `atm init` initializes the store only. This
prevents CI/scripts from hanging on a prompt. JSON output also stays
non-interactive.

Text output prints the initialized store path followed by one line per installed
plugin role and agent. JSON output includes the store path and an `installed`
array with `role`, `agent`, `path`, `files`, and `dry_run`.

`--dry-run` reports the files that would be written and does not mutate user
plugin configuration. Store initialization remains real and idempotent, matching
the command's primary purpose.

## Removed Command Surface

`atm manage plugin status` and `atm manage plugin install` are removed from
Cobra registration. The underlying package functions may remain because `atm
init` uses them and tests cover their behavior directly.

## Launcher Project Creation

Developer and manager launchers no longer error when `--project <CODE>` is
missing from the store. They call a shared helper that:

1. Opens the store.
2. Returns the existing project if present.
3. If not present, creates the project with code `<CODE>` and name `<CODE>`.
4. Uses the same actor floor that normal direct CLI project creation uses:
   `admin@cli:unset`.

Creation uses the normal store project creation path so seed labels and personas
remain consistent with explicit `atm project create`.

The `--project` flag remains required. ATM still needs a project code to scope
the session; it simply stops requiring a separate pre-create step.

## Testing

Tests cover:

- `atm init --agent codex --agent claude` installs both developer and manager
  plugin assets for both selected agents.
- `atm init` in text-mode TTY prompts for one or more agents and installs both
  developer and manager plugin assets for the selected agents.
- `atm init` in non-interactive text mode without `--agent` does not prompt or
  install plugins.
- `atm init --agent all --dry-run` reports all plugin writes without creating
  plugin files.
- `atm manage plugin ...` fails because the command tree is removed.
- `atm codex --project FOO` creates project `FOO` when missing and launches.
- `atm manage codex --project FOO --planning` creates project `FOO` when missing
  and launches.

Full verification remains `make verify`.
