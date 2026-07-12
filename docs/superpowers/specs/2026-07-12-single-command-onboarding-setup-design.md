# Single-Command Onboarding Setup - Design Spec

**Status:** Draft for user review.
**Task:** ATM-0101.
**Date:** 2026-07-12.

## Driver

The README's 30-second start describes onboarding as two different acts:
first-time user setup, then repository/project onboarding. The current command
surface still exposes the first act as several steps: `atm init` installs
plugins, `atm agents list` shows readiness, `atm agents select` picks the
default agent, and `atm agents args` records default host-agent arguments.

That is too much ceremony for a first run. A user runs one interactive
setup command, chooses supported agents, installs the needed ATM plugins,
selects the default launch agent, optionally records default args, and then
runs a normal manager onboarding session:

```sh
atm init
atm manage --project ATM --onboarding
```

After that, `atm` opens the dashboard for the onboarded project, and
`atm dev --project <CODE>` / `atm manage --project <CODE> --<action>` are the
normal working surfaces.

## Goals

- Make `atm init` the single interactive first-run setup command.
- Preserve existing scripted setup with repeatable `--agent` and non-interactive
  behavior.
- Let interactive setup install supported native plugins, choose the default
  launch agent, and set default passthrough args in one flow.
- Keep `atm agents list|select|args` as advanced/manual maintenance commands.
- Update README and conventions so users see one setup command before
  `--onboarding`.

## Non-goals

- No new store entities. Continue using `<ATM_HOME>/agents.json`.
- No custom agent registration.
- No plugin install for `ollama`; ollama entries still use the integration
  host's plugin.
- No removal of `atm agents list|select|args`.
- No change to task, label, persona, manager, or launcher data models.

## Command Surface

### Interactive `atm init`

In text output mode, with TTY stdin, and with no explicit `--agent`, `atm init`
runs a guided setup:

1. Initialize the store idempotently.
2. Prompt for native agent plugins to install:

   ```text
   Choose agent integrations to install (multiple allowed):
     1) opencode
     2) codex
     3) claude
   Agents [comma-separated numbers/names, all, or Enter to skip]:
   ```

3. Install developing and manager plugins for the selected native hosts.
4. Prompt for the default launch agent.
5. Prompt for optional default args for that selected agent.
6. Print a setup summary including store path, plugin writes, selected agent,
   configured args, and the next command:

   ```text
   Next: atm manage --project <CODE> --onboarding
   ```

The default-agent prompt lists catalog entries that are viable after the setup
step:

- Native entries for installed native plugins.
- `ollama:<integration>` entries when the integration plugin is installed.
- Existing selected agent, if any, even if no new plugin was installed.

Readiness is still computed live by `agent.Status`. If an entry is not fully
ready because a binary is missing, setup can still select it but prints the same
kind of warning as `atm agents select`.

Blank input at the default-agent prompt keeps the existing selected agent. If no
agent is currently selected and no viable entries exist, setup skips selection
and prints the existing no-agent guidance.

The default-args prompt accepts a single line and parses it with shell-like
quoting, so a user can enter:

```text
--yolo --profile "work laptop"
```

Blank input stores no args for a newly selected agent and preserves existing
args when keeping an existing selection.

### Explicit `atm init --agent`

Scripted setup remains non-interactive. `--agent` keeps its current meaning:
install plugins for one or more native hosts (`opencode`, `codex`, `claude`,
or `all`).

After a successful explicit install, `atm init --agent ...` keeps the current
behavior: if no agent is selected, select the first installed native agent in
stable order. It does not prompt for a different default or args.

### Non-interactive / JSON setup

With non-TTY stdin or `--output json`, `atm init` never prompts. With no
`--agent`, it initializes the store only and leaves selection empty. This keeps
CI and scripts deterministic.

JSON output adds optional setup fields when they are available:

```json
{
  "store": "...",
  "installed": [],
  "selected": "codex",
  "args": ["--yolo"]
}
```

Fields omitted today remain omitted when empty, preserving the existing
small JSON shape for basic init.

## Persistence

The setup flow writes only existing config:

- `Store.SetSelectedAgent(name, "admin@cli:unset")`
- `Store.SetAgentArgs(name, args, "admin@cli:unset")`

Interactive setup overwrites the selected agent when the user explicitly
chooses a new one. It only overwrites args when the user enters an args line for
the selected agent.

`--dry-run` keeps its current plugin behavior and must not mutate `agents.json`.
In interactive mode, dry-run still shows the selection and args prompts as a
preview, but the final summary says those config writes were not persisted.

## README and Conventions

The README's 30-second start becomes:

```sh
atm init                       # guided setup: store, plugins, default agent, args
atm manage --project ATM --onboarding
```

The README no longer puts `atm agents list` in the main onboarding path.
It can mention `atm agents list|select|args` later as maintenance commands for
changing agents after setup.

`atm conventions` describes first-run setup as `atm init`, then daily
work as:

```sh
atm
atm dev --project <CODE>
atm manage --project <CODE> --planning
```

It still documents `--agent <name>`, `ATM_AGENT`, and `atm agents args`
as override/maintenance tools.

## Implementation Notes

- Keep the current `promptInitAgents` parser for plugin selection.
- Add a small interactive setup result type so `newInitCmd` can distinguish
  selected plugin hosts, chosen default agent, chosen args, and whether each was
  explicitly provided.
- Keep install expansion stable: `all` expands to `opencode`, `codex`,
  `claude`; duplicates are collapsed.
- Build the default-agent prompt from `agent.Catalog()` and the installed or
  already configured plugins rather than hardcoding display strings in multiple
  places.
- Use a shell-like splitter for args instead of `strings.Fields`, so quoted
  args survive. If the standard library is not enough, add a tiny local parser
  with focused tests rather than adding a dependency.

## Testing

Tests cover:

- Interactive `atm init` installs selected plugins, prompts for default agent,
  persists `selected`, prompts for args, and persists args.
- Interactive setup can select an `ollama:<integration>` entry after installing
  the integration plugin.
- Blank default-agent input preserves an existing selection.
- Blank args input preserves existing args when keeping an existing selection.
- `--dry-run` does not mutate `agents.json`.
- Explicit `atm init --agent codex` remains non-interactive and still selects
  the first installed agent only when no selected agent exists.
- Non-interactive `atm init` with no `--agent` still does not prompt and does
  not select an agent.
- JSON output includes selected/args when setup writes them.
- README and conventions text no longer require `atm agents list/select/args`
  in the primary first-run path.

Full verification remains `make verify`.
