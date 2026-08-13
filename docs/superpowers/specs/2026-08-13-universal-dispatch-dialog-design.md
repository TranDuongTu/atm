# Universal dispatch dialog

- **Task:** ATM-29f8b0
- **Date:** 2026-08-13
- **Status:** Approved design, pre-implementation

## Problem

The TUI dispatch dialog (`internal/tui/dispatch.go`) is context-bound: its persona
is fixed at open time by the pane and selection that triggered it, and the persona
appears only in the dialog title, never as a field. Consequences:

1. **Concierge and admin are reachable only through the projects pane's
   "activity by persona" chart drill.** `D` on the projects pane opens a manager
   dialog; `D` on the tasks pane opens a developer dialog; the persona chart's
   `D` (via `projects.openDispatchForPersona`) is the only path to concierge and
   admin. On a fresh project with no activity there are no persona groups, so
   concierge/admin cannot be dispatched from the TUI at all.
2. **Project requirement is hardcoded per kind.** `dispatchKind.projectRequired()`
   returns true only for manager and developer, hardcoding the mapping the CLI
   already derives from each persona's `project_optional` frontmatter. Two
   sources of truth that can drift.
3. **`D` is a no-op outside the two panes with a selection** (and toasts
   "no project scope for dispatch" when a task has no project), so the dialog
   cannot be opened at all in a fresh workspace.

## Goals

- One universal dispatch dialog where the persona is a selectable field, always
  available regardless of how the dialog was opened.
- Dispatch logic is identical in every context; context only preselects
  defaults (persona, project, task).
- The persona picker lists every store persona (built-ins + customs), the same
  list the `V` personas overlay shows.
- A persona that requires a project, opened with no project scope, refuses to
  dispatch with an inline explanation rather than failing obscurely.
- `D` opens the dialog from any pane, including an empty workspace.

## Decisions of record

- **Persona is a data-driven field; the `dispatchKind` enum is deleted.**
  The dialog holds a persona list snapshot (`store.ListPersonas()`) and a
  cursor, cycled with `p`. Project-requirement is read from each persona's
  `project_optional` spec via a new `core.Persona.ProjectOptional` field.
  (Rejected: keeping the kind enum and adding a persona override — leaves the
  hardcoded context-dependent mapping in place; two sources of truth.)
- **Context supplies defaults only.** The universal `D` handler resolves the
  opening context into (default persona, project, task) and passes them to
  `open()`; the dialog logic is identical in every context.
- **`admin` stays in the picker and stays project-optional.** `--persona admin`
  routes to `launchTUI` (`internal/cli/root.go`), so dispatching admin opens a
  fresh TUI in the chosen terminal surface; it needs no project and no agent
  readiness gate. This matches today's behavior via the chart.
- **Concierge defaults for an empty context.** With no selection and no scope,
  `D` opens with concierge preselected (the one built-in that launches without
  a project).
- **Task and repo defaults persist across persona switches.** A bound task
  stays bound (rendered as the Task line) and is passed as `--task` whenever a
  project is present; the Repo line and its up/down cycle show whenever a
  project is present.
- **No-project refusal is inline and non-spawning.** A project-requiring persona
  with no scope renders a warning line and Enter shows a toast instead of
  spawning.
- Supersedes the "Persona is fixed per trigger" decision in
  `2026-07-23-tui-agent-dispatch-design.md` (which explicitly rejected a persona
  picker). The old spec remains as the record of the original dispatch
  surface, terminals, and launcher `--task` work.

## Design

### 1. Data: `core.Persona.ProjectOptional`

Add `ProjectOptional bool` to `core.Persona` (`internal/core/types.go`).

Populate it from the persona spec:

- `builtinPersona(spec)` (`internal/store/persona.go`) copies
  `spec.ProjectOptional` (concierge true; manager, developer false).
- `parsePersonaDoc` copies the parsed spec's `ProjectOptional`, so a custom
  persona `.md` that declares `project_optional: true` round-trips. Store-created
  customs (whose composed doc omits the key) default to false → required,
  matching today's launcher behavior.

`admin` exception: although admin's frontmatter does not declare
`project_optional`, the dialog treats admin as project-optional because
`--persona admin` routes to a fresh TUI that ignores `--project`. This matches
current behavior (`dispatchKind.projectRequired()` returns false for admin).

### 2. `dispatchModel` rework (`internal/tui/dispatch.go`)

Replace the `kind dispatchKind` field with:

```go
type dispatchModel struct {
    m             *Model
    personas      []*core.Persona // snapshot of store.ListPersonas() at open
    personaCursor int
    project       string
    taskID        string
    taskTitle     string
    agents        []agentOption
    cursor        int
    targets       []string
    targetCursor  int
    preview       string
    previewErr    string
    repos         []core.RepoConfig
    repoCursor    int
}
```

Behavior:

- `persona()` returns `personas[personaCursor].Name`.
- `projectRequired()` returns `!personas[personaCursor].ProjectOptional`,
  with the admin exception from section 1.
- `open(defaultPersona, project, taskID, taskTitle string)`:
  - snapshots `store.ListPersonas()` into `personas`;
  - preselects the index whose name equals `defaultPersona` (falls back to
    concierge when not found);
  - stores the context defaults;
  - loads `project.ProjectRepos` whenever `project != ""` (was developer-only);
  - refreshes the target preview.
- New key `p` cycles `personaCursor` with wrap-around. Existing keys unchanged:
  `←/→` agent, `↑/↓` repo, `t` target, `Enter` dispatch, `Esc` close.
- `title()`: task ID when bound, else `<project> · <persona>` when a project is
  present, else just the persona name.
- `submit()`:
  - refuses (toast + stays open) when a project-requiring persona has no
    `project`;
  - builds `atm --persona <p> [--project <code>] --agent <name> [--task <id>]`;
    `--project` only when `projectRequired()`, `--task` only when a task is
    bound and a project is present;
  - admin: `atm --persona admin`, no project, no task, no agent-ready gate
    (agent picker still renders; admin ignores it).
- `renderOverlay()`:
  - adds a `Persona:  ‹ name ›` line with the persona description as a hint
    (truncated like other lines);
  - Task line renders whenever a task is bound (not only for developer);
  - Repo line renders whenever `project != ""` (not only for developer);
  - a warning line when the selected persona requires a project but none is
    set: `⚠ persona requires a project scope`;
  - dialog title becomes `Dispatch` (persona moved into the field);
  - help line becomes
    `[p]persona  [←/→]agent  [↑/↓]repo  [t]target  [Enter]dispatch  [Esc]close`
    (repo segment present when a project is in scope).

### 3. Universal `D` handler (`internal/tui/app.go`)

Replace the `case "D":` block (currently lines ~649–676) with a context →
defaults resolution that always opens:

- projects pane, persona chart drilled → default persona = drilled persona key,
  project = projectScope.
- projects pane, project row selected → manager + row code.
- tasks pane, task row selected → developer + task's project + task.
- every other case → concierge + projectScope (may be empty).

The "no project scope for dispatch" toast is removed. `D` no longer checks the
pane — it always opens the dialog with resolved defaults.

`projects.openDispatchForPersona` (`internal/tui/projects.go:336`) collapses to
a name lookup: `open(persona, projectScope, "", "")`, no kind mapping, no
manager fallback for unknown personas (the dialog falls back to concierge
itself).

### 4. Keymap and help (`internal/tui/keymap.go`, `help.go`)

- The `D` keymap row updates from `dispatch manager` / `dispatch developer on
  task` to `dispatch (persona picker)` for all panes.
- `help.go` conventions text and any status hints mentioning dispatch update
  to describe the persona picker.
- Existing persona-chart status hint that asserts no `[p]` remains unchanged
  (`actors_test.go:175` guards it and must keep passing).

## Testing

- **Store** (`internal/store/persona_test.go`): `builtinPersona` carries
  `ProjectOptional` (concierge true, manager/developer false); a custom persona
  `.md` with `project_optional: true` round-trips through `GetPersona`; admin
  stays project-optional via the dialog exception (store-level: admin is
  required as today — the exception lives in the TUI dialog).
- **Dialog** (`internal/tui/dispatch_test.go` rework):
  - `open` preselects the named default; unknown name falls back to concierge.
  - `p` cycles personas with wrap; project-required persona with no scope
    renders the warning and Enter toasts without spawning.
  - Bound task persists across persona switch and yields `--task` (when
    project present); switching to developer shows the task line again.
  - Repo line renders for a project-requiring persona opened from a project —
    update `TestDispatchManagerUnchangedByRepoPicker` (manager now shows Repo).
  - Concierge argv omits `--project`; manager/developer include it.
  - Admin opens a TUI dispatch: no `--project`, no task.
- **Universal D** (`internal/tui/app_test.go`):
  - D opens from the tasks pane with no task → concierge.
  - D opens from an empty workspace → concierge.
  - Existing manager/developer/drilled-persona default tests still pass.
  - The "no project scope" toast test is removed.
- **Render**: persona line with description; help line shows `[p]persona`.
- **Keymap/help**: `D` row text asserted.

## Implementation stages (one branch)

1. `core.Persona.ProjectOptional` + store population + tests.
2. `dispatchModel` rework (persona field, `p` key, project-required refusal,
   repo/task defaulting, render) + dialog tests.
3. Universal `D` handler + `openDispatchForPersona` collapse + tests.
4. Keymap/help/docs (README, CHANGELOG, old-spec note) + ledger updates.

## References

- `docs/superpowers/specs/2026-07-23-tui-agent-dispatch-design.md` — original
  dispatch design; its persona-fixed-per-trigger decision is superseded here.
