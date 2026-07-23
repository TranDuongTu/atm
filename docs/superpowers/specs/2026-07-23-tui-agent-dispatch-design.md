# TUI agent dispatch: persona overlay + manager/developer dispatch dialogs

- **Task:** ATM-4b7e24
- **Date:** 2026-07-23
- **Status:** Approved design, pre-implementation

## Problem

Agent sessions are launched only from the shell: `atm --persona <name> --project
<CODE> --agent <host>` renders the context prompt and execs the host agent in
the current terminal (`internal/session`, `internal/cli/session.go`). The TUI —
where an operator actually surveys projects and tasks — cannot start one. Two
gaps follow:

1. There is no way to dispatch a manager or developer session from the TUI
   into a separate terminal surface (herdr pane, tmux window, terminal tab),
   so operating ATM means leaving it.
2. There is no way to hand a session a specific task: the launcher knows
   persona and project, but "work on ATM-xxxx" must be typed into the agent's
   chat by hand.

Personas are likewise invisible in the TUI: `skills/persona/*.md` built-ins
can only be inspected via the CLI.

## Goals

- Dispatch a **manager** session from the projects pane and a **developer**
  session on the selected task from the tasks pane, each with a chosen host
  agent, into an automatically detected terminal surface.
- Hand the developer session its task through the existing context-file
  mechanism, usable from the plain CLI too.
- Browse personas in the TUI (read-only).
- Fire-and-forget: the TUI spawns and moves on; visibility comes from the
  multiplexer itself and from the agent's ledger activity.

## Decisions of record

- **Dispatch surface detection is automatic**, in order **herdr → tmux →
  terminal tab/window**; first available wins. No per-dispatch target picker
  and no configured default backend. ([herdr](https://herdr.dev/) is an agent
  multiplexer with a `herdr pane run`-style CLI and socket API.)
- Terminal fallback opens a **new tab or window** via a built-in spawn table
  for common emulators, overridable by a config command template. It never
  degrades to printing a command.
- **Fire-and-forget lifecycle.** No session registry, no status plumbing, no
  kill/jump actions. (Rejected for now: tracking dispatched sessions in the
  TUI.)
- **Persona is fixed per trigger** — manager from the projects pane, developer
  from a task row. No persona field in either dialog. (Rejected: one generic
  dispatch form with a persona picker.)
- The developer dialog's task is **bound to the selected row**, display-only.
- **Persona management is a separate read-only overlay**: view built-in (and
  custom) personas only. No personality customization from the TUI for now.
  (Rejected for now: editing personality overlays in the TUI; the CLI
  `atm persona <name> personality --edit` path remains the way to customize.)
- The spawned command is always the existing launcher invocation
  (`atm --persona … --agent … [--task …]`); the dispatch package composes
  argv and target only, never duplicating render/env logic.

## Design

### 1. `internal/dispatch` — target detection and spawning

```go
type Spec struct {
    Title string   // window/pane/tab label
    Argv  []string // the atm launcher invocation
    Dir   string   // working directory
}

type Target interface {
    Name() string      // "herdr" | "tmux" | "terminal"
    Available() bool   // env/binary detection, no side effects
    Spawn(Spec) error
}

// Detect returns the first available target in precedence order
// herdr → tmux → terminal, or an error naming what to configure.
func Detect(cfg Config, env EnvFunc, lookPath LookPathFunc) (Target, error)
```

- **herdr**: detected via the env herdr injects into managed panes
  (`HERDR_ENV=1` / `HERDR_SOCKET_PATH`). Spawns via herdr's CLI —
  `herdr pane split` to create the pane, `herdr pane run <pane> "<cmd>"` to
  execute in it (cwd and label set through the pane-creation params; herdr's
  socket API exposes both).
- **tmux**: `tmux new-window -n <title> -c <dir> <argv…>`. Detected via
  `$TMUX` plus the binary on `$PATH`.
- **terminal**: a spawn table for common emulators detected via their
  environment fingerprints — kitty (`KITTY_LISTEN_ON`/`TERM`), wezterm
  (`WEZTERM_UNIX_SOCKET`), gnome-terminal, konsole, foot, alacritty — each
  entry mapping to that emulator's new-tab (preferred) or new-window spawn
  command. A config template overrides detection entirely, stored in a
  user-level `dispatch.json` at the store root (sibling and same
  read/write pattern as `agents.json`):

  ```json
  { "terminal_cmd": "kitty @ launch --type=tab --cwd {dir} -- {cmd}" }
  ```

  Placeholders: `{cmd}` (shell-quoted argv), `{dir}`, `{title}`. Config wins
  over detection; an emulator that is running but not in the table and no
  template set → detection fails with a message naming the `terminal_cmd`
  setting and its file. Setting it is a hand edit for now — no CLI/TUI
  editor.

Constructors take `env`/`lookPath` functions so tests fake the environment;
`Spawn` goes through an injected runner so argv assembly is asserted without
real processes.

Working directory: the TUI's cwd (the same directory a shell launch runs from
today). Title format: `<CODE> · <persona>[ · <task-id>]` (e.g.
`ATM · manager`, `ATM · developer · ATM-4b7e24`).

Failure surfacing: `Detect`/`Spawn` errors land in the TUI status bar; nothing
is retried.

### 2. Session launcher: `--task` and `ATM_TASK`

New optional `--task <id>` flag on the persona launch path
(`internal/cli/session.go` `launchSession` and the hidden
`atm session-context`):

- Validated against the project: the task must exist in `--project`'s store;
  a bad ID fails before any host is launched.
- Exported to the host as `ATM_TASK=<id>` (omitted when empty, like
  `ATM_MODE`).
- Rendered into the context file by `internal/session.RenderContext` as an
  assignment block: the session's assigned task is `<id>` — read it first
  with `atm task show <id>` — and all work in the session serves it.

Because it rides the context file, it behaves identically for `launch: hook`
and `launch: prompt` personas, and it is usable straight from the shell
without the TUI.

### 3. TUI: personas overlay (read-only)

A dedicated overlay (following the capability-view pattern), opened by its own
key from the main views:

- **List**: every persona — built-ins from `skills/persona/`, plus store
  customs — showing name, description, declared modes, and launch type.
- **Detail** (enter): the persona's effective prompt, read-only, scrollable.
- No create, edit, or personality customization. Esc closes.

### 4. TUI: dispatch dialogs

Two thin dialogs sharing one internal component (agent cycle-picker + target
preview + dispatch/cancel). Persona appears in the dialog title, never as a
field.

**Projects pane — dispatch key on the selected project:**

```
╭─ Dispatch manager ── ATM ───────────────────────────╮
│                                                     │
│  Agent:    ‹ claude ›                               │
│            ready                                    │
│                                                     │
│  Target:   tmux · new window “ATM · manager”        │
│                                                     │
│         [ Dispatch ]        [ Cancel ]              │
╰─────────────────────────────────────────────────────╯
   ←/→ change agent · enter dispatch · esc close
```

**Tasks pane — dispatch key on the selected task row:**

```
╭─ Dispatch developer ── ATM-4b7e24 ──────────────────╮
│                                                     │
│  Task:     ATM-4b7e24                               │
│            TUI agent dispatch: manage personas + …  │
│                                                     │
│  Agent:    ‹ opencode ›                             │
│            ready                                    │
│                                                     │
│  Target:   herdr · pane “ATM · developer · ATM-4b7e24”
│                                                     │
│         [ Dispatch ]        [ Cancel ]              │
╰─────────────────────────────────────────────────────╯
   ←/→ change agent · enter dispatch · esc close
```

Behavior:

- **Agent** is the only interactive field: a cycle-picker over
  `agent.Catalog()` with readiness (`agent.Status`) evaluated per entry. An
  unready entry renders its missing-bin/plugin hint and the Dispatch action
  refuses while it is selected. Selection order follows the catalog.
- **Task** (developer dialog only) is display-only: ID plus title echo of the
  selected row.
- **Target** is the read-only `Detect` result phrased as what will happen,
  including the exact title to be created. Detection failure renders as an
  error line and disables Dispatch.
- Enter on a ready agent calls `dispatch.Spawn` with
  `atm --persona <p> --project <CODE> --agent <name> [--task <id>]`,
  closes the dialog, and reports success or the spawn error in the status
  bar. Fire-and-forget: no record is kept in the TUI.

The dialogs are dedicated overlay sub-models following the existing
`capabilityModel` pattern (`internal/tui/dispatch.go`), not a `form.go`
select/cycle field; `form.go` is unchanged. Keybinding specifics (which
letter, help-pane entries) follow the existing keymap conventions in
`internal/tui/keymap.go`: `D` dispatches a manager (projects pane) or
developer-on-task (tasks pane) session; `V` opens the personas browser.

## Testing

- `internal/dispatch`: detection precedence with faked env/lookPath (herdr
  beats tmux beats terminal; absence cascades); per-target argv assembly via
  the injected runner; terminal spawn-table entries; config-template
  substitution and shell quoting of `{cmd}`; no-target error message.
- Launcher: `--task` validation (missing task fails, wrong project fails),
  `ATM_TASK` in the env map, assignment block present in the rendered
  context, omitted when no task.
- TUI: personas overlay list/detail rendering; dialog trigger presets
  (manager from projects, developer from task row with bound task); agent
  cycle order and unready-agent refusal; target preview and
  detection-failure disable; dispatch invocation argv (through a fake
  dispatcher); status-bar outcomes. Existing `internal/tui` test patterns
  apply.

## Implementation stages (one branch)

1. Session launcher `--task` / `ATM_TASK` / context assignment block.
2. `internal/dispatch` package: targets, detection, config template.
3. TUI dispatch dialogs (overlay sub-models) + keybindings + status-bar wiring.
4. TUI personas overlay.
5. Docs (README, CHANGELOG) + ledger updates.
