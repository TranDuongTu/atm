# ATM Developing Agent Launcher — Design Spec

**Status:** Approved
**Date:** 2026-07-05
**Parent specs:** `2026-07-02-tasks-management-v2-design.md`,
`2026-07-04-onboarding-v1-design.md`,
`2026-07-05-task-comments-v1-design.md`

## Driver

ATM already has an onboarding workflow: launch an agent, ask it to inspect a
repository, and have it seed context tasks into a project. That workflow is
bounded and run-and-forget.

Day-to-day software development needs a different role. A working agent should
use ATM as the visible work ledger during its normal interactive session:
feature work, design work, specs, implementation progress, discoveries,
commits, and decisions should be tied to the relevant task and recorded as task
comments. The agent should not drift into a private chat transcript that the
human cannot later inspect through ATM.

The UX constraint is strict: `atm developing <agent>` must not replace the
user's normal agent workflow. It must preserve the agent's normal harness,
skills, plugins, MCP servers, permissions, sandbox, and repo instructions
(`AGENTS.md`, `CLAUDE.md`, etc.). ATM should add a small, session-scoped
operating convention: when this session is launched through ATM, use the named
ATM project as the work ledger.

## Scope (v1)

- Add `atm developing opencode|codex|claude --project <CODE>`.
- Require `--project`, matching onboarding v1. No project inference in v1.
- Validate that the project exists before launching the agent.
- Write a small developing context file under `$ATM_HOME/developing/`.
- Launch the selected agent's normal interactive entrypoint.
- Set ATM session environment variables for the child process:
  - `ATM_ROLE=developing`
  - `ATM_PROJECT=<CODE>`
  - `ATM_BIN=<absolute path to atm>`
  - `ATM_CONTEXT_FILE=<path to rendered context file>`
  - `ATM_ACTOR=<actor>`
  - `ATM_RUN_ID=<run id>`
- Provide installable ATM bootstrap plugins for OpenCode, Codex, and Claude.
- Provide explicit plugin install/status commands. `atm developing` itself does
  not silently install or modify agent configuration.
- The plugin activates only when `ATM_ROLE=developing` and `ATM_PROJECT` are
  present. Outside that launcher context, it stays silent.
- The plugin injects concise context into the agent's normal context path
  without sending a visible first user message.

## Out of Scope (v1)

- Passing the user's actual work request through `atm developing`.
- Auto-creating projects or inferring projects from repos.
- Modifying repo-local agent configuration.
- Replacing or suppressing existing agent system prompts, repo instructions,
  skills, plugins, MCP servers, approval modes, or sandbox settings.
- Enforcing task/comment usage mechanically. The ledger behavior is advisory
  prompt context in v1.
- Building an ATM MCP server.
- Agent-specific deep automation such as commit hooks, automatic task
  detection from diffs, or background comment generation.
- Supporting agents beyond `opencode`, `codex`, and `claude`.

## Command Surface

```
atm developing opencode --project <CODE> [--actor <id>] [--dry-run]
atm developing codex    --project <CODE> [--actor <id>] [--dry-run]
atm developing claude   --project <CODE> [--actor <id>] [--dry-run]

atm developing plugin status  [opencode|codex|claude|all]
atm developing plugin install [opencode|codex|claude|all] [--dry-run]
```

`--actor` defaults to `<agent>-dev`, unless the global `--actor` /
`ATM_ACTOR` value is already set. The actor is not passed as a behavior
override to the agent. It is part of the ATM context so the agent can stamp
mutating ATM commands consistently.

`--dry-run` validates the project, renders the context file, prints the launch
environment and argv, and exits without starting the child process.

Plugin installation is explicit and user-scoped. The install command may write
to each agent's user-level plugin/config area, but it must not write repo-local
agent config. A regular `atm developing <agent>` launch may warn when the ATM
plugin is missing, but it must not install it automatically.

## Launcher Behavior

`atm developing <agent> --project FOO`:

1. Opens the store and verifies project `FOO` exists.
2. Resolves the absolute `atm` binary path with `os.Executable()`.
3. Builds a run id:
   `<CODE>-<YYYYMMDDHHMMSS>-<short-hex>`.
4. Renders `$ATM_HOME/developing/<run-id>.md`.
5. Prints a short header containing project, agent, run id, and context path.
6. Starts the selected agent as a child process with inherited stdin/stdout/
   stderr and the ATM environment variables listed above.
7. Waits for the child to exit.
8. Prints a short tail summary with the agent exit code.
9. Returns success only when the child exits successfully.

Unlike onboarding, developing does not inspect before/after task counts as a
success metric. A developing session can legitimately do only investigation,
review, planning, or discussion. Visibility is measured by whether the plugin
keeps the agent oriented toward ATM, not by net task creation.

## Plugin Bootstrap Model

The design deliberately mimics Superpowers' cross-agent pattern: use each
agent's native plugin/hook mechanism to add a small bootstrap context, instead
of sending a first user message or replacing the agent's system prompt.

The bootstrap context is minimal:

- This is an ATM developing session for project `<CODE>`.
- Use `<ATM_BIN>` for all ATM commands.
- Start work by finding the relevant task, or creating one if the work is a
  new feature/design/spec/chore.
- Record intentions, progress, discoveries, design links, commit references,
  and open questions as task comments.
- Prefer comments on the relevant task over private-only chat summaries.
- Follow repo instructions, existing skills, harness rules, and user
  directions first; ATM is a work ledger, not a replacement workflow.
- If instructions conflict, preserve the normal agent/repo instruction
  hierarchy and use ATM where compatible.

The rendered `$ATM_CONTEXT_FILE` can contain more detail, examples, and the
current project/task snapshot, but the injected bootstrap should remain short
and point to the context file only when more detail is needed.

## Plugin Install UX

`atm developing plugin status` checks whether the ATM developing bootstrap is
available to each supported agent and reports one of:

- `installed`: adapter is present and expected to load.
- `missing`: adapter is not installed.
- `unknown`: ATM cannot verify this agent's plugin state.

`atm developing plugin install <agent|all>` installs the adapter using the
least intrusive user-level mechanism available for that agent:

- OpenCode: install a global local plugin file under the user's OpenCode config
  directory, or update user OpenCode config to reference an npm/local package if
  that becomes the more maintainable packaging path.
- Claude: install a user-scoped plugin in Claude's skills/plugin directory or
  use Claude's plugin marketplace flow when ATM is distributed that way.
- Codex: install or register a user-scoped Codex plugin/marketplace entry.

The install command prints exactly what it changed and how to uninstall or
disable the adapter. It may ask the user to restart the target agent, but it
does not alter existing repo-local instructions or project config.

## Agent Adapters

### OpenCode

OpenCode supports local/global JS or TS plugins loaded from `.opencode/plugins`
or `~/.config/opencode/plugins`, plus npm package plugins configured in
OpenCode config. Its plugin API can hook events, alter shell environment, add
tools, and transform chat messages.

ATM's OpenCode adapter should be a JS plugin that:

- Checks `process.env.ATM_ROLE === "developing"`.
- Reads `ATM_PROJECT`, `ATM_BIN`, and `ATM_CONTEXT_FILE`.
- Prepends a concise ATM bootstrap block to the first user message through
  OpenCode's message transform hook, guarding against duplicate injection.
- Optionally injects the ATM environment into shell execution so spawned shell
  commands inherit the same context.

This mirrors Superpowers' OpenCode implementation, but with conditional
activation so regular OpenCode sessions are untouched.

### Claude Code

Claude Code plugins are directories with `.claude-plugin/plugin.json` and
optional `skills/`, `agents/`, `hooks/`, `.mcp.json`, and related components.
Claude plugin hooks can run on `SessionStart` and return additional context.

ATM's Claude adapter should be a plugin with:

- `.claude-plugin/plugin.json`
- `hooks/hooks.json` registering a `SessionStart` hook.
- A bundled script that checks `ATM_ROLE` / `ATM_PROJECT` and returns:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "<minimal ATM developing bootstrap>"
  }
}
```

If the ATM env vars are absent, the script returns no additional context. This
gives Claude the ATM ledger reminder before the user's first real prompt
without displaying a synthetic first user message.

### Codex

Codex plugins are directories with `.codex-plugin/plugin.json` and can bundle
skills, MCP servers, apps, and lifecycle hooks. Codex also supports
`SessionStart` hooks, with trust review for non-managed command hooks.

ATM's Codex adapter should start with the same plugin shape:

- `.codex-plugin/plugin.json`
- A small `skills/atm-developing/SKILL.md` describing the ledger behavior.
- A bundled `SessionStart` hook that checks `ATM_ROLE=developing` and returns
  additional context when supported by the current Codex hook output contract.

Because Codex hook behavior is stricter around trust review and the current
examples in the local marketplace mostly demonstrate skill/MCP packaging, the
implementation plan must prototype this adapter first. If the hook path cannot
quietly inject context in the current Codex version, v1 falls back to a
Codex-specific installed skill plus `atm developing codex` printing a clear
warning that automatic bootstrap is unavailable until the hook is trusted or
supported.

## Context File

The context file is rendered per run and stored under:

```
$ATM_HOME/developing/<run-id>.md
```

It includes:

- Project code and name.
- Run id and timestamp.
- Absolute ATM binary path.
- Actor.
- A compact summary of the agent code of conduct.
- A small command cheat sheet:
  - `atm conventions`
  - `atm label list --project <CODE>`
  - `atm task list --project <CODE> --output json`
  - `atm task show --id <ID> --output json`
  - `atm task create ... --actor <ACTOR>`
  - `atm task comment add --task <ID> --body ... --actor <ACTOR>`
  - `atm task comment list --task <ID>`
- Comment guidance: use comments for progress, decisions, implementation
  notes, test results, commit SHAs, open questions, and handoff notes.

The context file is advisory and session-local. It is not the source of truth;
ATM tasks/comments are.

## Developing Code of Conduct

The plugin bootstrap and context file encode these rules of thumb:

1. A feature, design, spec, bug, chore, or meaningful investigation should have
   a task.
2. Before starting work, search the project tasks for a relevant existing task.
3. If none exists, create one with a concise title and labels that match the
   project's label descriptions.
4. Record intent before substantial work when practical.
5. Add progress comments at meaningful checkpoints: plan chosen, files changed,
   test results, blockers, review findings, commits, and handoff.
6. Do not duplicate task state into private chat only. Important working
   memory belongs on the task.
7. Use ATM as a ledger, not as an authority that overrides repo instructions or
   the user's direct request.

## Internal Architecture

The implementation should reuse onboarding's launcher shape without merging
the command concepts:

- Shared run id generation.
- Shared child process execution with inherited stdio.
- Shared prompt/context-file rendering helpers where appropriate.
- Separate packages or types for onboarding vs developing role data.
- Separate embedded prompt/context templates.

The external command remains `atm developing`, not
`atm onboarding --role developing`, because the user mental models differ:
onboarding creates context; developing uses ATM as the daily work ledger.

## Error Handling

- Missing project: return `ErrNotFound` and print the exact
  `atm project create --code <CODE> --name "..."` hint.
- Unknown agent subcommand: Cobra usage error.
- Missing agent binary: return an error with the agent's install hint.
- Context render/write failure: return the underlying filesystem error.
- Child non-zero exit: print the tail summary, then return non-zero.
- Plugin not installed: the launcher should not fail in v1. It should print a
  warning that the selected agent may not receive automatic ATM bootstrap until
  the ATM plugin is installed/enabled.

## Testing

Unit and golden tests:

- `atm developing <agent> --project MISSING` maps to not-found.
- Dry-run for each agent renders deterministic JSON/text envelope fields with
  dynamic paths normalized in tests.
- Context file rendering includes project, actor, ATM binary, task snapshot,
  and command cheat sheet.
- Launcher argv preserves each agent's normal interactive entrypoint.
- Child environment includes the ATM variables and preserves existing
  environment values.
- Plugin bootstrap scripts stay silent without `ATM_ROLE=developing`.
- Plugin bootstrap scripts emit concise context with ATM env vars set.
- Duplicate-injection guard for the OpenCode message transform.

Manual smoke tests:

- Install/enable the ATM plugin for each supported agent.
- Run `atm developing <agent> --project ATM --dry-run`.
- Run `atm developing <agent> --project ATM`.
- Confirm the agent opens normally, no visible first user prompt is sent, and
  the agent has ATM context available before the first user work request.

Repository gate:

- `make verify`.

## Research Notes

- OpenCode plugin docs: https://opencode.ai/docs/plugins/
- OpenCode config directory docs: https://opencode.ai/docs/config/
- Claude plugin docs: https://code.claude.com/docs/en/plugins
- Claude hook docs: https://code.claude.com/docs/en/hooks
- Codex plugin docs from the official Codex manual:
  `/codex/plugins`, `/codex/plugins/build`, `/codex/hooks`.
- Superpowers provides the cross-agent precedent: OpenCode uses a message
  transform, Claude uses a `SessionStart` hook with additional context, and
  Codex packages skills/plugins through `.codex-plugin/plugin.json`.

## Open Questions

- Codex automatic bootstrap needs a prototype against the current local Codex
  hook output behavior before the implementation plan promises parity with
  OpenCode and Claude.
