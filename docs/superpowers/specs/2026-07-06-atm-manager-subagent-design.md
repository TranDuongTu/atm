# ATM Manager Subagent — Design Spec

**Status:** Draft
**Date:** 2026-07-06
**Parent specs:** `2026-07-02-tasks-management-v2-design.md`,
`2026-07-04-onboarding-v1-design.md`,
`2026-07-05-atm-developing-agent-launcher-design.md`,
`2026-07-05-task-comments-v1-design.md`

## Driver

`atm developing` gives a working agent a visible work ledger: it injects a
small bootstrap context that tells the agent to find-or-create a task and
record progress as comments. That puts ledger hygiene entirely on the
developing agent. In practice this has two costs:

1. Every developing agent has to understand ATM's substrate — labels,
   status axis, comment conventions, project codes, the difference between
   a task and a chat note. The bootstrap prompt can only carry so much of
   that, and drift across sessions is real.
2. The human is the only force keeping the ledger organized. Stale tasks,
   inconsistent labels, missing priorities, and ambiguous titles accumulate
   because no one role owns the ledger's shape.

The manager role closes both. A manager is a fully autonomous ATM CLI
actor whose only job is to own the ledger: formalize raw "track" calls
from developing agents into proper tasks/comments/labels, surface
priorities, clean up staleness, and answer human steering questions when
the human opens an interactive manager session. Developing agents stop
being ledger authors; they become ledger reporters. They hand the manager
a freeform note and an advisory hint and continue working without
depending on the reply.

The UX constraint is the same as `atm developing`: the manager must not
replace the developing agent's normal harness, skills, plugins, MCP
servers, permissions, sandbox, or repo instructions. ATM adds a role
convention on top of the host's native subagent dispatch; it does not
fork the host.

## Scope (v1)

- Add an `atm manager` command tree parallel to `atm developing`.
- Two runtime modes for the manager role:
  1. **Subagent dispatch** — a developing agent asks the host to dispatch
     the `atm-manager` subagent with a freeform track message and an
     optional advisory hint. The manager formalizes the track call into
     ATM writes and returns a short confirmation. The developing agent
     does not branch on the reply.
  2. **Interactive human session** — `atm manager <host> --project <CODE>`
     launches a normal agent process with `ATM_ROLE=manager`, exactly
     parallel to `atm developing <host> --project <CODE>`. The human uses
     it to consult or steer the manager about project organization.
- Provide `atm manager render-context` as the single source of truth for
  the manager system prompt. Host adapters are thin wrappers that embed or
  invoke this prompt; they do not duplicate prompt content.
- Provide installable manager subagent definitions for OpenCode, Codex,
  and Claude, parallel to the developing bootstrap plugins. The manager
  subagent definition is what makes `@atm-manager` / Task dispatch
  available to a developing agent inside a host.
- Set ATM session environment variables for the interactive manager
  launcher, mirroring the developing launcher:
  - `ATM_ROLE=manager`
  - `ATM_PROJECT=<CODE>`
  - `ATM_BIN=<absolute path to atm>`
  - `ATM_CONTEXT_FILE=<path to rendered context file>`
  - `ATM_ACTOR=<actor>`
  - `ATM_RUN_ID=<run id>`
- The manager is fully autonomous in both modes: it never asks the caller
  back, never requests human clarification through a relay, and never
  blocks on a reply. In subagent mode it formalizes and returns. In
  interactive mode it answers the human directly in dialogue.

## Out of Scope (v1)

- A persistent background manager process or daemon. The manager runs
  either as a host-native subagent (synchronous, per-track) or as an
  interactive human-launched session. No inbox queue, no cron sweep, no
  long-running watcher in v1.
- A formal "track" wire protocol with typed fields. v1 uses a freeform
  message plus an optional advisory hint string. The manager interprets
  both.
- Making the developing agent wait on or branch on the manager's reply.
  The developing agent dispatches, notes the result, and continues. The
  reply is for the ledger, not for the developing agent's control flow.
- Auto-creating projects or inferring projects from repos. `--project`
  is required, matching developing v1.
- Modifying repo-local agent configuration. Manager plugin install writes
  only to user-level agent config areas.
- Replacing or suppressing existing agent system prompts, repo
  instructions, skills, plugins, MCP servers, approval modes, or sandbox
  settings.
- An ATM MCP server. The manager uses the `atm` CLI as its only ATM
  substrate, same as developing.
- Supporting hosts beyond `opencode`, `codex`, and `claude`.

## Command Surface

```
atm manager opencode --project <CODE> [--actor <id>] [--dry-run]
atm manager codex    --project <CODE> [--actor <id>] [--dry-run]
atm manager claude   --project <CODE> [--actor <id>] [--dry-run]

atm manager render-context [--project <CODE>] [--actor <id>] [--track <msg>] [--hint <hint>]

atm manager plugin status  [opencode|codex|claude|all]
atm manager plugin install [opencode|codex|claude|all] [--dry-run]
```

`--actor` defaults to `<host>-manager`, unless the global `--actor` /
`ATM_ACTOR` value is already set. The actor stamps mutating ATM commands
the manager issues, so ledger history distinguishes manager-written
entries from developing-agent-written ones.

`--dry-run` on the launcher validates the project, renders the context
file, prints the launch environment and argv, and exits without starting
the child process — identical semantics to `atm developing <host>
--dry-run`.

`atm manager render-context` prints the manager system prompt to stdout.
With `--track` it appends a "Track request" section containing the
caller's freeform message and advisory hint, so a host adapter can pipe
the result into a subagent dispatch. Without `--track` it prints the bare
manager role prompt used to seed interactive sessions and subagent
definitions. The output is plain markdown; host adapters decide whether to
embed it at install time or invoke the command at dispatch time.

Plugin installation is explicit and user-scoped, mirroring developing:

- `atm manager plugin status` reports `installed` / `partial` / `missing`
  per host. `partial` means the `atm-manager` subagent definition is present
  but the developing bootstrap plugin is not installed, so the developing
  agent does not know to dispatch `atm-manager`.
- `atm manager plugin install <host|all>` writes the manager subagent
  definition to the host's user-level agents directory and prints exactly
  what changed and how to uninstall. It does not write repo-local config.

## Two Runtime Modes

### Subagent dispatch (developing → manager)

When a developing agent wants to record work, it asks its host to
dispatch the `atm-manager` subagent. The dispatch carries:

- A freeform **track message**: what the developing agent just did, is
  about to do, decided, blocked on, or noticed. Natural language, no
  schema.
- An optional **advisory hint**: a short string the manager may use to
  pick the right ledger action. v1 recognizes a small open set: `feature`,
  `bug`, `design`, `spec`, `chore`, `investigation`, `decision`, `progress`,
  `blocker`, `handoff`, `question`. Unknown hints are ignored; the manager
  falls back to interpreting the freeform message alone.

The host's native subagent dispatch is synchronous by construction. The
developing agent waits briefly for the manager to finish formalizing the
track call, notes the result, and continues. It never branches on the
reply, never relays follow-ups, never re-dispatches. "Doesn't wait" in
the brainstormed decision is interpreted as "doesn't *depend* on the
reply", not as literally non-blocking. Options that would have required
shifting to a shell-out or inbox model were rejected to preserve
host-native subagent dispatch.

The manager, on receiving the dispatch:

1. Reads `ATM_PROJECT`, `ATM_BIN`, `ATM_ACTOR` from its environment
   (inherited from the developing session that spawned it).
2. Inspects the current ledger state for the project: open tasks, recent
   comments, labels.
3. Decides the formal ledger action: create a new task, append a comment
   to an existing task, adjust labels (e.g. add a priority or status
   transition), or split one track call into multiple writes.
4. Executes the writes through the `atm` CLI using `ATM_ACTOR` (or
   `<host>-manager` if unset) as the actor.
5. Returns a concise confirmation: which task(s) were touched, what
   action was taken, and the task ID(s). No prose beyond what the caller
   needs to know the ledger is updated.

The manager is autonomous: if the track message is ambiguous, the
manager picks the most reasonable interpretation and writes that. It does
not ask the developing agent to clarify. If the developing agent later
disagrees, it can dispatch a correction track call.

### Interactive human session (human → manager)

`atm manager <host> --project <CODE>` launches a normal agent process
with `ATM_ROLE=manager` and the rendered manager context file. The human
is the primary actor. Use cases:

- "What's stale in this project?" — the manager queries open tasks with
  old `updated_at`, surfaces them, and proposes cleanup actions.
- "What should I work on next?" — the manager ranks open tasks by
  priority/status labels and recent activity.
- "Reconcile labels on ATM-0024." — the manager adjusts labels on a task
  per the human's steering.
- "Summarize the last session's ledger activity." — the manager reads
  recent comments and produces a digest.

This mode replaces the brainstormed-but-rejected "human passthrough
relay" flow. The human does not ask the developing agent to ask the
manager; the human opens the manager directly.

## Manager Prompt

The manager system prompt is embedded in the `atm` binary as
`internal/manager/context_v1.md` and rendered by
`atm manager render-context`. It encodes:

- **Role**: You are the ATM ledger owner for project `<CODE>`. You own
  the ledger's shape: consistent labels, sensible titles, accurate
  status, prioritized work, and clean comments.
- **Authority**: You are a full ATM CLI actor via `<ATM_BIN>`. You create
  tasks, add comments, adjust labels, and transition status. You stamp
  all writes with actor `<ACTOR>`.
- **Autonomy**: You never ask the caller back. In subagent mode you
  interpret, formalize, write, and return a short confirmation. In
  interactive mode you answer the human directly. Ambiguity is resolved
  by your best judgment; corrections come as new track calls.
- **Track pipeline**: A track request is a freeform message plus an
  optional advisory hint. Decide the formal action: new task, comment on
  existing task, label adjustment, or a small set of writes. Prefer
  appending to an existing open task over creating a new one when the
  track message clearly extends prior work. Prefer one task per
  conceptual unit of work.
- **Ledger hygiene**: Use the project's label conventions consistently.
  Status is a label axis (`<CODE>:status:<state>`), not a field. Keep
  titles concise and accurate. Comments record intent, progress,
  decisions, files changed, test results, blockers, commit SHAs, and
  handoff notes — not chat.
- **Interactive mode**: When launched as `atm manager <host>`, the human
  is consulting you about project organization. Answer in dialogue. Do
  not start writing code or modifying repo files; you only touch the
  ATM ledger.
- **Code of conduct**: Follow repo instructions, existing skills,
  harness rules, tool permissions, and user directions first. ATM is the
  ledger you own; it is not a workflow that overrides the host's normal
  rules. If instructions conflict, preserve the normal agent/repo
  instruction hierarchy and use ATM where compatible.

The rendered context file for interactive sessions also includes a
command cheat sheet (mirroring developing's): `atm conventions`, `atm
label list`, `atm task list/show/create/comment`, with the manager actor
pre-filled.

## Host Adapters

Each host gets a thin adapter whose only job is to make the
`atm-manager` subagent available to a developing agent running inside
that host. The adapter does not contain prompt logic; it references the
rendered manager context.

### OpenCode

OpenCode supports markdown subagent definitions in
`~/.config/opencode/agents/` or `.opencode/agents/`. The file name
becomes the agent name. Frontmatter carries `description`, `mode:
subagent`, `permission`, and optional `model`; the body is the system
prompt.

ATM's OpenCode adapter installs `~/.config/opencode/agents/atm-manager.md`
with:

- `description`: "ATM ledger owner for project dispatches. Invoke when
  the developing agent asks to track work, formalize progress, or
  organize the project ledger."
- `mode: subagent`
- `permission`: `bash: allow` (the manager needs to invoke the `atm` CLI),
  `edit: deny`, `write: deny` (the manager does not touch repo files).
- Body: the rendered manager prompt from `atm manager render-context
  --project <CODE>` for the project the install is scoped to, or a
  generic version that reads `ATM_PROJECT` from env at dispatch time.

Because a subagent definition is static markdown, v1 prefers
**env-driven** bodies: the body tells the manager to read
`ATM_PROJECT`, `ATM_BIN`, `ATM_ACTOR` from its environment and only act
when `ATM_ROLE=manager` is present. This lets one installed
`atm-manager.md` serve any project, matching how the developing plugin
stays silent outside a developing session. `atm manager render-context`
without `--project` prints this generic body; with `--project` it prints a
project-pinned body for hosts that require a concrete project at install
time.

The developing agent's bootstrap context (installed by
`atm developing plugin install`) gains a short paragraph: "To track work,
dispatch the `atm-manager` subagent with a freeform message and an
optional advisory hint (`feature`, `bug`, `design`, `chore`,
`investigation`, `decision`, `progress`, `blocker`, `handoff`,
`question`). Note the reply and continue. Do not branch on it."

### Claude Code

Claude Code supports agent definitions under `~/.claude/agents/` as
markdown with frontmatter (`name`, `description`, `tools`, optional
`model`). The body is the system prompt.

ATM's Claude adapter installs `~/.claude/agents/atm-manager.md` with the
same env-driven body as OpenCode and a `tools` field permitting bash (for
`atm` CLI) and read tools. The developing Claude plugin's SessionStart
hook gains the same "dispatch `atm-manager` to track work" paragraph.

### Codex

Codex supports subagent/skill packaging through `.codex-plugin/plugin.json`
and bundled agent/skill definitions. ATM's Codex adapter installs an
`atm-manager` agent definition in the user-scoped Codex agents area with
the same env-driven body. As with developing v1, if the current Codex
version cannot quietly register a dispatchable subagent, v1 falls back to
documenting the dispatch contract in the developing bootstrap and
printing a clear warning from `atm manager plugin status codex`.

## Plugin Install UX

Identical shape to `atm developing plugin`, with `installed` / `partial`
/ `missing` states:

- `installed`: the `atm-manager` subagent definition is present in the
  host's user-level agents area and the developing bootstrap plugin is
  installed, so the developing agent knows to dispatch `atm-manager`.
- `partial`: the subagent definition exists but the developing bootstrap
  plugin is absent.
- `missing`: no subagent definition found.

`atm manager plugin install <host|all>` writes the subagent definition
and prints what changed and how to uninstall. It may ask the user to
restart the target host. It does not alter repo-local config and does not
install the developing plugin; the developing plugin is installed
separately by `atm developing plugin install`.

## Internal Architecture

The implementation mirrors `internal/developing/` without merging the
packages, exactly as developing mirrors onboarding:

```
internal/manager/
  context.go        // embeds context_v1.md, RenderContext(data)
  context_v1.md     // manager system prompt template
  launcher.go       // LauncherFor(host) for interactive sessions
  plugins.go        // PluginStatus(host), InstallPlugin(host, dryRun)
  plugin_assets/    // per-host subagent definition templates
    opencode/
    codex/
    claude/
```

`internal/cli/manager.go` adds the `atm manager` command tree, parallel
to `internal/cli/developing.go`. Shared helpers (run id generation,
child process execution, env assembly, header/tail emission) are
extracted from `internal/cli/developing.go` into a small shared helper
package or shared file so both launchers use the same code path without
one depending on the other. The developing and manager packages remain
independent at the package level; only the CLI glue shares helpers.

The `atm manager render-context` subcommand lives in
`internal/cli/manager.go` and calls `manager.RenderContext` with the
resolved project (or a generic env-driven template when `--project` is
absent) plus optional `--track` / `--hint` payload appended as a final
"Track request" section.

## Error Handling

- Missing project on the interactive launcher: return `ErrNotFound` and
  print the `atm project create --code <CODE> --name "..."` hint, same as
  developing.
- Unknown host subcommand: Cobra usage error.
- Missing host binary on the interactive launcher: return an error with
  the host's install hint, same as developing.
- Context render/write failure: return the underlying filesystem error.
- Child non-zero exit: print the tail summary, then return non-zero.
- Manager plugin not installed: the interactive launcher prints a warning
  that subagent dispatch may be unavailable until `atm manager plugin
  install` is run. It does not fail the launch.
- Subagent dispatch failure (developing side): the developing agent
  notes the failure in its own context and continues. It does not retry
  or block. Ledger hygiene is best-effort in v1.
- `atm manager render-context` with a missing project prints a generic
  env-driven body and exits 0; it does not require `--project` so host
  adapters can install a project-agnostic subagent definition.

## Testing

Unit and golden tests:

- `atm manager <host> --project MISSING` maps to not-found with the
  project-create hint.
- Dry-run for each host renders deterministic JSON/text envelope fields
  with dynamic paths normalized, mirroring developing's dry-run tests.
- Context file rendering includes project, actor, ATM binary, manager
  code of conduct, and command cheat sheet.
- `atm manager render-context --project ATM` produces a deterministic
  prompt body with the project code substituted.
- `atm manager render-context` without `--project` produces the
  env-driven generic body.
- `atm manager render-context --track "..." --hint progress` appends a
  Track request section with both fields.
- Launcher argv preserves each host's normal interactive entrypoint.
- Child environment includes the ATM variables and preserves existing
  environment values.
- Manager subagent definition templates stay silent without
  `ATM_ROLE=manager`.
- Manager subagent definition templates emit the manager role context
  with ATM env vars set.
- `PluginStatus` reports `installed` / `partial` / `missing` correctly
  per host, including the partial case where the developing plugin is
  absent.
- `InstallPlugin` writes the expected files to the user-level agents
  directory and is idempotent.

Manual smoke tests:

- `atm manager plugin install all`.
- `atm manager <host> --project ATM --dry-run`.
- `atm manager <host> --project ATM` — confirm the host opens normally
  with manager context and the human can ask organizational questions.
- From a developing session in the same host, dispatch `@atm-manager`
  (or equivalent) with a freeform track message and confirm the manager
  creates/comments on the right task and returns a short confirmation.
- Confirm the developing agent continues without branching on the reply.

Repository gate:

- `make verify`.

## Research Notes

- OpenCode agent docs: https://opencode.ai/docs/agents/ — markdown
  subagent definitions under `~/.config/opencode/agents/` or
  `.opencode/agents/`, with frontmatter (`description`, `mode`,
  `permission`, `model`) and a body that is the system prompt. `mode:
  subagent` makes the agent invokable via the Task tool or `@` mention.
- OpenCode plugin docs: https://opencode.ai/docs/plugins/ — used by the
  developing bootstrap; the manager reuses the same plugin to add the
  "dispatch atm-manager" instruction to the developing bootstrap.
- Claude Code plugin docs: https://code.claude.com/docs/en/plugins —
  agent definitions under `~/.claude/agents/`.
- Claude Code hook docs: https://code.claude.com/docs/en/hooks —
  SessionStart hook used by the developing bootstrap.
- Codex plugin docs: `/codex/plugins`, `/codex/plugins/build`,
  `/codex/hooks`.
- Superpowers provides the cross-agent precedent for env-conditional
  bootstrap and subagent dispatch.

## Open Questions

- Whether the OpenCode `atm-manager.md` subagent body should be
  project-pinned at install time (`atm manager render-context --project
  ATM`) or env-driven (read `ATM_PROJECT` at dispatch time). v1 leans
  env-driven so one install serves all projects; the implementation plan
  must confirm the host's subagent dispatch actually inherits the
  parent's ATM env vars.
- Codex subagent registration parity: whether the current Codex version
  can register a dispatchable `atm-manager` subagent the same way
  OpenCode and Claude can, or whether v1 falls back to documenting the
  dispatch contract in the developing bootstrap. Needs a prototype
  against the local Codex, same as the developing v1 Codex adapter.
- Whether the developing bootstrap should auto-dispatch `atm-manager`
  for the first trackable event of a session (e.g. "I'm starting work on
  X") or always leave dispatch to the developing agent's judgment. v1
  leaves it to the agent's judgment to avoid accidental ledger noise.
