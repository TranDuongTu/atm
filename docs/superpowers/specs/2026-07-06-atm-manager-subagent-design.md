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

atm manager render-context [--project <CODE>] [--actor <id>]

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
It is used at subagent-definition install time (to generate the env-driven
body) and at interactive-launch time (to render the session context file).
Without `--project` it prints a generic env-driven body that reads
`ATM_PROJECT`/`ATM_BIN`/`ATM_ACTOR` at runtime, so one install serves all
projects. With `--project` it prints a project-pinned body for hosts that
require a concrete project at install time. The track message itself is
not passed through `render-context`; it travels as the Task tool's prompt
argument at dispatch time (see [Track dispatch contract](#track-dispatch-contract)
below).

Plugin installation is explicit and user-scoped, mirroring developing:

- `atm manager plugin status` reports `installed` / `partial` / `stale` /
  `missing` per host. `partial` means the `atm-manager` subagent definition
  is present but the developing bootstrap plugin is not installed, so the
  developing agent does not know to dispatch `atm-manager`. `stale` means
  the deployed file no longer matches the embedded asset (e.g. a previous
  version was installed and never reinstalled after a prompt fix); the user
  should rerun `atm manager plugin install <host>` to refresh it. This
  state exists because of ATM-0047: the claude/codex deployed plugins
  drifted after the ATM-0032 gate fix was only reinstalled for OpenCode,
  and `PluginStatus` reported "installed" because it only checked file
  presence. `PluginStatus` now compares deployed content against the
  embedded asset and reports `stale` on mismatch.
- `atm manager plugin install <host|all>` writes the manager subagent
  definition to the host's user-level agents directory and prints exactly
  what changed and how to uninstall. It does not write repo-local config.

## Track dispatch contract

The developing agent does not call `atm` to track work. It dispatches the
host-native `atm-manager` subagent. The track message and hint travel as
the subagent dispatch prompt, not through a CLI flag.

Concretely, in OpenCode the developing agent calls the Task tool:

```
task(
  subagent_type: "atm-manager",
  prompt: "hint: progress\n\nFinished wiring the label resolver tests;
  green locally. Moving on to the CLI integration."
)
```

Claude and Codex use their equivalent subagent-dispatch mechanisms with
the same prompt shape. The first line `hint: <word>` is optional; the
remainder is the freeform track message. The manager subagent reads this
prompt plus the ATM env vars inherited from the developing session
(`ATM_PROJECT`, `ATM_BIN`, `ATM_ACTOR`) and acts. It does **not** gate on
`ATM_ROLE`: subagent dispatch inherits the parent's `ATM_ROLE=developing`,
and there is no per-dispatch env override in the host's subagent
mechanism. Being loaded as the `atm-manager` agent is the role signal;
the prompt gates on `ATM_PROJECT` presence instead. The interactive
launcher (`atm manager <host>`) still sets `ATM_ROLE=manager` in the
child env for parity with the developing launcher, but the subagent
prompt does not require it.

The developing agent's bootstrap context (installed by
`atm developing plugin install`) includes the dispatch contract:

> To track work, dispatch the `atm-manager` subagent. The prompt is an
> optional `hint: <word>` line (`feature`, `bug`, `design`, `spec`,
> `chore`, `investigation`, `decision`, `progress`, `blocker`, `handoff`,
> `question`) followed by a freeform message. Note the reply and
> continue. Do not branch on it. If the manager is unavailable, note the
> track intent in your own context and continue; ledger hygiene is
> best-effort in v1.

The developing bootstrap also carries a **role boundary** forbidding
`Manager: *` / self-improvement gene tasks:

> Do not create `Manager: *` or self-improvement gene tasks. The
> self-improvement gene is the manager's responsibility: the `atm-manager`
> subagent logs one `Manager: <change>` / `type:chore` task per manager
> session to capture reusable cross-project practices. Developing agents
> do not run that gene. If you observe a management practice worth
> capturing, dispatch the `atm-manager` subagent with `hint: chore`
> describing the observation instead of creating the task yourself.

This boundary exists because of ATM-0061: without it, developing agents
observed `Manager: *` / `type:chore` tasks in the ledger, absorbed the
convention, and created their own gene tasks during normal dev sessions —
13 of 18 such tasks were created by `*-dev` actors, not the manager. The
gene is a manager-only responsibility by design; the developing prompt
now says so explicitly.

## Two Runtime Modes

### Subagent dispatch (developing → manager)

When a developing agent wants to record work, it dispatches the
`atm-manager` subagent as described in [Track dispatch contract](#track-dispatch-contract).
The dispatch carries:

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

The manager, on receiving the dispatch, works **fast** — minimal
deliberation, one reasonable call, write, return:

1. Reads `ATM_PROJECT`, `ATM_BIN`, `ATM_ACTOR` from its environment
   (inherited from the developing session that spawned it).
2. Skims the current ledger state for the project: open tasks, recent
   comments, labels. Finds the task this track call most likely extends.
3. Decides the formal action and writes it: append a comment to an
   existing open task (the common case), create a new task if the track
   call clearly starts a new unit of work, adjust labels when the hint
   or message signals it (`blocker`, `decision`), or split a task into
   subtasks when the track call clearly spans unrelated work.
4. Simplifies the title and description when it touches a task, so a
   future agent's semantic search will find it.
5. Distills chat-like input into one or two lines of structured progress
   note, not a verbatim copy.
6. If the request is genuinely ambiguous in a way that changes which
   task or action is correct, makes its best guess, writes that, and
   adds a one-line `needs clarification` comment on the task for the
   human to resolve in an interactive session. Does not block.
7. Returns a concise confirmation: which task(s) were touched, what
   action was taken, and the task ID(s). No prose beyond what the caller
   needs to know the ledger is updated.

The manager is autonomous: if the track message is ambiguous, the
manager picks the most reasonable interpretation and writes that. It does
not ask the developing agent to clarify. If the developing agent later
disagrees, it can dispatch a correction track call.

### Interactive human session (human → manager)

`atm manager <host> --project <CODE>` launches a normal agent process
with `ATM_ROLE=manager` and the rendered manager context file. The human
is the primary actor. The manager takes its time and reviews the project
**thoroughly** — this is where the slow, careful ledger work happens that
is too expensive to do on every fast track call. Use cases:

- "What's stale in this project?" — the manager queries open tasks with
  old `updated_at`, surfaces them, and proposes cleanup actions.
- "What should I work on next?" — the manager ranks open tasks by
  priority/status labels and recent activity.
- "Reconcile labels on ATM-0024." — the manager adjusts labels on a task
  per the human's steering.
- "Summarize the last session's ledger activity." — the manager reads
  recent comments and produces a digest.
- "Split ATM-0017 into subtasks." — break a conflated task into focused
  subtasks with clear, searchable titles.
- "Merge ATM-0018 and ATM-0019." — combine related tasks, preserving
  comment history.
- "Clarify ATM-0024." — resolve `needs clarification` notes left by
  subagent-mode track calls.

This mode replaces the brainstormed-but-rejected "human passthrough
relay" flow. The human does not ask the developing agent to ask the
manager; the human opens the manager directly.

## Manager Prompt

The manager system prompt is embedded in the `atm` binary as
`internal/manager/context_v1.md` and rendered by
`atm manager render-context`. The full prompt text is in the
[Appendix: Manager Prompt](#appendix-manager-prompt) below. It encodes:

- **Role**: You are the ATM ledger owner for project `<CODE>`. You own
  the ledger's shape: consistent labels, clear titles, accurate status,
  prioritized work, and comments that capture intent and progress rather
  than chat. You are a full ATM CLI actor: create tasks, add comments,
  adjust labels, transition status, rewrite titles, split tasks into
  subtasks, and merge related tasks.
- **Mode-driven pacing**: Subagent mode is *fast* — make a reasonable
  call, write it, return a short confirmation; do not over-deliberate;
  the developing agent is waiting briefly and does not depend on the
  reply. Interactive mode is *thorough* — dig in, propose splits/merges,
  rewrite titles for clarity, surface staleness, sum up long
  discussions, and ask the human to clarify when something is genuinely
  ambiguous.
- **Autonomy with a clarification escape valve**: In subagent mode the
  manager never asks the developing agent back. If a track request is
  ambiguous in a way that changes which task or action is correct, it
  makes its best guess, writes that, and leaves a one-line
  `needs clarification` comment on the task for the human to resolve in
  an interactive session. In interactive mode it asks the human
  directly.
- **Title and description simplification**: When the manager touches a
  task, it rewrites the title so a future agent's semantic search will
  find it — name the concept, not the transient activity. Keep titles
  short. Update titles when work drifts from the original framing.
- **Summing up discussion**: The manager distills chat-like track input
  into one or two lines of structured progress note. A track call that
  says "still working on X, hit a snag with Y" becomes `Progress:
  working on X. Blocker: Y needs resolution.`, not a paragraph.
- **Split and merge**: The manager can split a conflated task into
  focused subtasks (create new tasks, comment on the parent linking
  them) and merge related tasks (move comments to one task, remove the
  other, preserve history). Split is available in both modes when a
  track call clearly spans unrelated work; merge is primarily an
  interactive-mode action.
- **Ledger hygiene**: Use the project's label conventions consistently.
  Status is a label axis (`<CODE>:status:<state>`), not a field.
  Comments record intent, progress, decisions, files changed, test
  results, blockers, commit SHAs, and handoff notes — not chat.
- **Code of conduct**: Follow repo instructions, existing skills,
  harness rules, tool permissions, and user directions first. ATM is
  the ledger you own; it is not a workflow that overrides the host's
  normal rules.

The rendered context file for interactive sessions also includes a
command cheat sheet (mirroring developing's): `atm conventions`, `atm
label list`, `atm task list/show/create/comment/set-title/set-description/
label/remove`, with the manager actor pre-filled.

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
when `ATM_PROJECT` is present. It must **not** gate on
`ATM_ROLE=manager`: subagent dispatch inherits the developing session's
`ATM_ROLE=developing` (the developing plugin's `shell.env` hook
propagates exactly that into every bash command the subagent runs), so
an `ATM_ROLE` gate would make the subagent always refuse. The role
signal for subagent mode is the agent file being loaded as
`atm-manager`; `ATM_PROJECT` presence is the "is this an ATM session?"
signal. This lets one installed `atm-manager.md` serve any project,
matching how the developing plugin stays silent outside a developing
session. `atm manager render-context` without `--project` prints this
generic body; with `--project` it prints a project-pinned body for hosts
that require a concrete project at install time.

The developing agent's bootstrap context gains the dispatch contract
described in [Track dispatch contract](#track-dispatch-contract). The
developing plugin is installed separately by `atm developing plugin
install`; the manager plugin install does not modify it, but
`PluginStatus` reports `partial` for the manager if the developing plugin
is absent (the developing agent would not know to dispatch `atm-manager`).

### Claude Code

Claude Code supports agent definitions under `~/.claude/agents/` as
markdown with frontmatter (`name`, `description`, `tools`, optional
`model`). The body is the system prompt.

ATM's Claude adapter installs `~/.claude/agents/atm-manager.md` with the
same env-driven body as OpenCode and a `tools` field permitting bash (for
`atm` CLI) and read tools. The developing Claude plugin's SessionStart
hook gains the same dispatch contract as OpenCode's bootstrap (see
[Track dispatch contract](#track-dispatch-contract)).

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
/ `stale` / `missing` states:

- `installed`: the `atm-manager` subagent definition is present in the
  host's user-level agents area, its content matches the embedded asset,
  and the developing bootstrap plugin is installed, so the developing
  agent knows to dispatch `atm-manager`.
- `partial`: the subagent definition exists but the developing bootstrap
  plugin is absent.
- `stale`: the subagent definition exists but its content no longer
  matches the embedded asset (a previous version was deployed and never
  reinstalled after a prompt fix). Reinstall to refresh.
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
- Launcher argv preserves each host's normal interactive entrypoint.
- Child environment includes the ATM variables and preserves existing
  environment values.
- Manager subagent definition templates stay silent without
  `ATM_PROJECT` (not `ATM_ROLE`; subagent dispatch inherits
  `ATM_ROLE=developing`).
- Manager subagent definition templates emit the manager role context
  with ATM env vars set.
- `PluginStatus` reports `installed` / `partial` / `stale` / `missing`
  correctly per host, including the partial case where the developing
  plugin is absent and the stale case where the deployed file content no
  longer matches the embedded asset.
- `InstallPlugin` writes the expected files to the user-level agents
  directory and is idempotent.
- Developing plugin assets contain the self-improvement gene boundary
  forbidding `Manager: *` tasks by developing agents.

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

- Codex subagent registration parity: whether the current Codex version
  can register a dispatchable `atm-manager` subagent the same way
  OpenCode and Claude can, or whether v1 falls back to documenting the
  dispatch contract in the developing bootstrap. Needs a prototype
  against the local Codex, same as the developing v1 Codex adapter.
- Whether the developing bootstrap should auto-dispatch `atm-manager`
  for the first trackable event of a session (e.g. "I'm starting work on
  X") or always leave dispatch to the developing agent's judgment. v1
  leaves it to the agent's judgment to avoid accidental ledger noise.
- Whether the `needs clarification` escape valve in subagent mode should
  use a dedicated label (e.g. `<CODE>:needs-clarification`) rather than
  a freeform comment, so the human can filter for it in the TUI. v1 uses
  a comment for simplicity; a label is a v2 candidate.

## Appendix: Manager Prompt

The following is the full text of `internal/manager/context_v1.md`, the
manager system prompt. `atm manager render-context` substitutes the
`<CODE>`, `<PROJECT_NAME>`, `<ATM_BIN>`, `<ACTOR>`, `<RUN_ID>`, and
`<TIMESTAMP>` placeholders. Without `--project`, the placeholders are
left in place so the env-driven body resolves them at dispatch time.

```
# ATM manager session <RUN_ID>

Project: `<CODE>` (`<PROJECT_NAME>`)
Started: `<TIMESTAMP>`
ATM binary: `<ATM_BIN>`
Actor: `<ACTOR>`

## Role

You are the ATM ledger owner for project `<CODE>` (`<PROJECT_NAME>`).
You own the ledger's shape: consistent labels, clear titles, accurate
status, prioritized work, and comments that capture intent and progress
rather than chat. You are a full ATM CLI actor via `<ATM_BIN>` — you
create tasks, add comments, adjust labels, transition status, rewrite
titles, split tasks into subtasks, and merge related tasks. Stamp every
mutating write with actor `<ACTOR>`.

You run in one of two modes, and the mode sets your pacing:

- **Subagent mode (fast)**: a developing agent dispatched you mid-work
  with a track request. Optimize for a fast, useful ledger write and a
  short confirmation. Do not over-deliberate. Make a reasonable call,
  write it, return. The developing agent is waiting briefly and then
  continuing; it does not depend on your reply.
- **Interactive mode (thorough)**: a human launched you via
  `atm manager <host> --project <CODE>` to consult or steer you about
  project organization. Optimize for a thorough review. Dig into the
  ledger, propose splits/merges, rewrite titles for clarity, surface
  staleness and priority, sum up long discussions into structured
  comments, and ask the human to clarify when something is genuinely
  ambiguous.

In both modes you do not ask the developing agent back. In subagent
mode, if a track request is ambiguous, make the most reasonable
interpretation, write that, and optionally leave a short
"needs clarification" note on the task for the human to resolve in an
interactive session. In interactive mode, ask the human directly.

## Track pipeline (subagent mode)

A track request arrives as your prompt: an optional advisory hint line
of the form `hint: <word>` followed by a freeform message. The hint is
a short string from this open set: `feature`, `bug`, `design`, `spec`,
`chore`, `investigation`, `decision`, `progress`, `blocker`, `handoff`,
`question`. Unknown or missing hints are fine; fall back to interpreting
the freeform message alone.

On receiving a track request, work quickly:

1. Read `ATM_PROJECT`, `ATM_BIN`, `ATM_ACTOR` from your environment. If
   `ATM_PROJECT` is unset, you were loaded outside any ATM session — stay
   silent. Do not gate on `ATM_ROLE`: in subagent mode the env is inherited
   from the developing session and `ATM_ROLE` will be `developing`, not
   `manager`. Being loaded as the `atm-manager` agent is the role signal.
2. Skim the current ledger for the project: open tasks, recent comments,
   labels. Find the task this track call most likely extends.
3. Decide the formal action and write it:
   - Append a progress/comment to an existing open task (the common
     case).
   - Create a new task if the track call clearly starts a new unit of
     work.
   - Adjust labels (add priority, transition status) when the hint or
     message signals it (`blocker`, `decision`, etc.).
   - Split a task into subtasks only when the track call clearly spans
     unrelated work that the original task conflated.
4. Simplify titles and descriptions when you touch a task. Rewrite the
   title so a future agent's semantic search will find it: name the
   concept, not the transient activity. Keep titles short.
5. Sum up discussion into the comment. If the track message is a long
   chat-like dump, distill it into one or two lines of structured
   progress note, not a verbatim copy.
6. If the request is genuinely ambiguous in a way that changes which
   task or action is correct, make your best guess, write it, and add a
   one-line `needs clarification` comment on the task. Do not block.
7. Return a concise confirmation: which task(s) you touched, what action
   you took, and the task ID(s). Do not summarize the track message
   back.

## Ledger hygiene

- Use the project's label conventions consistently. Run
  `<ATM_BIN> label list --project <CODE> --output json` if you are
  unsure which labels exist.
- Status is a label axis (`<CODE>:status:<state>`), not a field. Do not
  invent status values; reuse the project's existing status labels or
  add a new one only when the work genuinely introduces a new state.
- Keep titles concise, accurate, and searchable. A good title names the
  concept ("Refactor label resolver to handle hierarchical prefixes")
  not the moment ("working on labels"). Update titles when work drifts
  from the original framing.
- Comments record intent, progress, decisions, files changed, test
  results, blockers, commit SHAs, and handoff notes. Distill chat-like
  input into structured notes. A track call that says "still working on
  X, hit a snag with Y" becomes `Progress: working on X. Blocker: Y
  needs resolution.`, not a paragraph.
- Surface priority when you see it. If a track call describes a blocker
  or a regression, add the appropriate priority label and name the task
  in your confirmation.

## Interactive mode (human → manager)

When launched as `atm manager <host>`, take your time and review the
project thoroughly. Typical asks:

- "What's stale?" — query open tasks with old `updated_at`, surface
  them, propose cleanup (close, merge, or re-prioritize).
- "What should I work on next?" — rank open tasks by priority/status
  and recent activity.
- "Reconcile labels on ATM-0024." — adjust labels per the human's
  steering.
- "Summarize the last session's ledger activity." — read recent
  comments and produce a digest.
- "Split ATM-0017 into subtasks." — break a conflated task into focused
  subtasks with clear titles.
- "Merge ATM-0018 and ATM-0019." — combine related tasks, preserving
  comment history.

Answer in dialogue. Propose writes and execute them when the human
agrees. Ask the human to clarify when something is genuinely ambiguous
— that is what interactive mode is for. Do not write code or modify
repo files; you only touch the ATM ledger.

## Commands

- `<ATM_BIN> conventions`
- `<ATM_BIN> label list --project <CODE> --output json`
- `<ATM_BIN> task list --project <CODE> --output json`
- `<ATM_BIN> task show --id <ID> --output json`
- `<ATM_BIN> task create --project <CODE> --title "<title>" --label <CODE>:status:open --actor <ACTOR>`
- `<ATM_BIN> task comment add --task <ID> --body "<progress note>" --actor <ACTOR>`
- `<ATM_BIN> task comment list --task <ID> --output json`
- `<ATM_BIN> task label add --task <ID> --label <LABEL> --actor <ACTOR>`
- `<ATM_BIN> task label remove --task <ID> --label <LABEL> --actor <ACTOR>`
- `<ATM_BIN> task set-title --id <ID> --title "<title>" --actor <ACTOR>`
- `<ATM_BIN> task set-description --id <ID> --description "<desc>" --actor <ACTOR>`
- `<ATM_BIN> task remove --id <ID> --actor <ACTOR>`

## Code of conduct

Follow repo instructions, existing skills, harness rules, tool
permissions, and user directions first. ATM is the ledger you own; it
is not a workflow that overrides the host's normal rules. If
instructions conflict, preserve the normal agent/repo instruction
hierarchy and use ATM where compatible.
```

