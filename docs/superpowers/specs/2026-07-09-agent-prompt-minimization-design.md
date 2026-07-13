# Agent Prompt Minimization & De-duplication — Design

Date: 2026-07-09
Task: ATM-0071
Status: design approved (pending spec review)

## Problem

ATM guides two kinds of host agents through prompt surfaces that have grown
long, overlapping, and drift-prone:

- **Developing agent** — guided by three surfaces that restate each other: the
  SessionStart hook (inline string), `internal/developing/context_v1.md` (the
  rendered `ATM_CONTEXT_FILE`), and `atm-developing/SKILL.md`. The three SKILL
  variants (claude/codex/opencode) have already diverged.
- **Manager agent** — guided by *two* files that have substantively diverged:
  the installed subagent definition
  `internal/manager/plugin_assets/<agent>/atm-manager.md` (deployed verbatim at
  plugin-install time; ~220 lines; has Truth-discipline + Bootstrap, only 2
  modes) and `internal/manager/context_v1.md` (runtime-rendered for interactive
  / onboarding launches; ~318 lines; has Vocabulary/Onboarding/Inquiry/
  Self-learning + 3 modes). The `context.go:20-24` comment claiming the subagent
  body is a zero-value render of `context_v1.md` is stale — the install path
  never calls `RenderContext`.

Consequences: enhancing manager logic forces users to re-install the plugin; the
two manager files drift; the prompts duplicate content that already lives in
`atm conventions`.

## Goals

1. **Minimal, discovery-oriented prompts.** Prompts point at `atm conventions`
   and `atm <cmd> --help` instead of embedding command cheat-sheets and
   substrate explanations.
2. **No re-install on logic change.** Installed plugin assets are thin, stable
   pointers; the actual logic lives in the binary and updates with it.
3. **No drift.** One source of truth per agent; the three per-harness variants of
   any installed asset stay trivially identical.
4. **Principle-driven manager.** The manager prompt is its operating principles
   plus a short action catalog, not a procedural runbook.

## Design

### Developing agent — three surfaces, deduplicated

Each surface gets one distinct job; no cross-copy.

- **SessionStart hook** (`plugin_assets/<agent>/hooks/session-start`) → a thin
  always-on nudge that points at the skill and context file. No embedded routine
  or command list.
- **`atm-developing/SKILL.md`** → an *eager* trigger whose only job is to load
  the rendered instruction. Body is two sentences (kept tiny so the three
  variants can't drift): read `$ATM_CONTEXT_FILE`; if unset, run
  `atm conventions`. The description fires on *moments* (making progress,
  deciding, hitting a blocker, starting creative/design/implementation work),
  not on keyword nouns.
- **`context_v1.md`** → the minimal session instruction: identity line, the
  "ATM is the ledger" mandate, a *read yourself* block (the read-only CLIs:
  `conventions`, `search`, `task show`/`comment list`, `label list`), and a short
  *Working Principles* block whose intent is "delegate every write to the
  manager": respect the host harness, and "When in doubt, write to the
  atm-manager." The developing agent stays a pure reader + delegator; it issues
  no mutating commands, so the old `Manager: *` role-boundary is enforced
  structurally rather than by a rule. (Original 2026-07-09 wording said
  "delegate every write to the manager" verbatim; commit eb8f8f5 on 2026-07-12
  reworded it into the Working Principles block — same intent, new phrasing.)

Format rule: prompt files are unwrapped (one physical line per paragraph/bullet).

### Manager agent — single source, thin pointer, principles only

- **Single source of truth** = `internal/manager/context_v1.md`, rewritten to
  **principles + action catalog** (~25 lines):
  - *Your Principles* — four principles: (1) **Ownership** — autonomous owner of
    the project, presenting organized, legible knowledge for agents, humans, and
    itself; (2) **Dive Deep** — stays connected to details and relentlessly
    surfaces current information; (3) **Simplify** — relentlessly and frequently
    organizes the project, creating order from chaos; (4) **Earn Trust** —
    watches client confusion, keeps its own self-improvement as a separate task
    bucket resolved in-session, and improves via the label substrate — never by
    editing this prompt. (Original 2026-07-09 wording named three principles
    under "Who you are"; commit eb8f8f5 on 2026-07-12 expanded to the four
    named-principle form — same role, reworded.)
  - *Your Roles* — short descriptions of six roles: Planning, Grooming,
    Tracking, Asking, Glossary, Onboarding.
  - Per decision, all operational detail is discarded (Truth-discipline, mode
    runbooks, vocabulary schema, onboarding caps/idempotency, inquiry ground-
    truth recording, self-improvement task mechanics, and even the conventions
    pointer / actor-stamp reminder — both still reachable via `atm conventions`).

- **Installed subagent definition** `atm-manager.md` (×3) → a ~10-line **thin,
  stable pointer**: YAML frontmatter (`name`/`description`/`tools: Bash, Read,
  Glob, Grep`) + a bootstrap that resolves env, bails if `$ATM_PROJECT` is unset,
  and runs `atm manager render-context --project "$ATM_PROJECT" --actor
  "$ATM_ACTOR"`, then follows that output as its full instructions.

- **Delivery mechanism.** Both invocation paths converge on the one source:
  - *Subagent dispatch* (developing agent's `hint:` call): the registered
    `atm-manager` agent runs `render-context` inside its own context, so the
    manager prompt never enters the developing agent's window.
  - *Interactive / onboarding* (`atm manager <agent>`): `runManager` renders the
    same file to `ATM_CONTEXT_FILE`, unchanged.

- **Why keep the registered agent** (vs. no install): the thin pointer already
  delivers "no re-install on logic change"; keeping it registered preserves the
  `tools:` sandbox (manager structurally cannot Write/Edit repo code) and
  first-class routing (the developing prompt just says "dispatch atm-manager").

### Supporting code changes

- **`render-context` polish** (`internal/cli/manager.go`): open the store to fill
  `<PROJECT_NAME>` and stamp `<TIMESTAMP>` so the subagent path renders a
  complete prompt. Today it leaves both as unfilled placeholders.
- **Fix the stale comment** in `internal/manager/context.go:20-24` describing the
  subagent body as a zero-value render.

## Files changed

- `internal/developing/context_v1.md` — rewrite (minimal).
- `internal/developing/plugin_assets/{claude,codex,opencode}/skills/atm-developing/SKILL.md` — rewrite (eager pointer, identical across three).
- `internal/developing/plugin_assets/{claude,codex,opencode}/hooks/session-start` — thin pointer nudge.
- `internal/manager/context_v1.md` — rewrite (principles + actions).
- `internal/manager/plugin_assets/{claude,codex,opencode}/atm-manager.md` — replace with thin pointer (identical across three).
- `internal/cli/manager.go` — `render-context` fills project name + timestamp.
- `internal/manager/context.go` — fix stale comment.

## Testing

- Golden files that capture rendered prompts / dry-run output will change:
  `internal/cli/testdata/golden/developing-dry-run-*.json`, and any manager
  render goldens. Regenerate and eyeball the diffs.
- `internal/manager/plugins_test.go` / `developing/plugins_test.go`: the
  `PluginStatus` stale-detection compares installed bytes to the embedded asset;
  update expectations for the new asset content.
- Add/adjust a test asserting `render-context` output has no unrendered
  `<PLACEHOLDER>` tokens when project + actor are supplied.
- `go test ./...` green before completion.

## Out of scope

- Any further change to manager *behavior* beyond the principle reframe.
- Persona-block rendering, launcher argv, and env plumbing (unchanged).
