# Lazygit-Style TUI Rewrite Design

**Date**: 2026-06-25  
**Status**: Approved for implementation planning  
**Supersedes**: The tab-based TUI structure in `001-tasks-management/contracts/tui.md` and `001-tasks-management/tui-mockups.md` for the next TUI implementation pass.

## Purpose

Rewrite `atm tui` as a lazygit-style workspace: a persistent left navigation column for the main operational panes and a contextual right column for details and actions. The TUI remains a thin client over `internal/store` and must preserve CLI/store parity. The rewrite replaces the current tab-oriented TUI model instead of adapting the tab shell.

## Goals

- Replace top-level tabs with one persistent workspace.
- Use a 30% left column for navigation and a 70% right column for contextual details and actions.
- Keep four left panes visible and stacked vertically: Projects, Tasks, Summary, Help.
- Let number keys `1`-`4` focus left panes.
- Let `j`/`k` and Up/Down move inside the currently focused pane only.
- Use `Space` on a project to set the active project scope; pressing `Space` on the scoped project clears scope back to all projects.
- Default Tasks and Summary to all projects when no project is scoped.
- Remove Actors from primary navigation and surface actor/claims context inside the Tasks right column.
- Keep all TUI mutations backed by existing `store.*` operations exposed through CLI parity.

## Non-Goals

- No mouse-first workflow.
- No auto-refresh.
- No custom theme or persisted layout settings.
- No new actor-management workflow.
- No new TUI-only store mutation.
- No pixel-perfect clone of lazygit internals; the goal is the navigation and pane model.

## Architecture

The root TUI model is rebuilt around a `workspace` concept instead of tabs. It owns:

- Store handle, actor id, store-ready state, startup state.
- Current terminal dimensions.
- Focused left pane: Projects, Tasks, Summary, or Help.
- Cursor state per left pane.
- Active project scope, or none for all projects.
- Hovered project and hovered task.
- Active right-column section for the focused pane.
- Filter state, form state, overlay state, toast/error state.
- Cached workspace snapshot derived from store reads.

The rewrite may retain small reusable primitives where useful, such as form fields, toast rendering, filters, and overlays, but it should not preserve the old tab models as the internal navigation architecture.

## Layout

The screen has a persistent header, a two-column body, and a footer.

```text
+--------------------------------------------------------------------------------+
| atm | scope: all or <PROJECT> | actor: <id> | store: <path> | r refresh | q quit |
+--------------------------------------------------------------------------------+
| LEFT 30%                              | RIGHT 70%                                |
| [1] Projects                          | Contextual detail/actions               |
|   > ATM                               | for the focused left pane                |
|     DEMO                              |                                          |
|                                       |                                          |
| [2] Tasks                             |                                          |
|   > ATM-0002 Fix claim race           |                                          |
|     DEMO-0001 Seed fixture            |                                          |
|                                       |                                          |
| [3] Summary                           |                                          |
|   all projects: open 18 review 3      |                                          |
|                                       |                                          |
| [4] Help                              |                                          |
+--------------------------------------------------------------------------------+
| focused pane keymap | selection | toast/error                                    |
+--------------------------------------------------------------------------------+
```

The left column width target is 30% with practical minimum and maximum widths so narrow terminals remain usable. The right column receives the remaining width.

## Selection Model

The TUI distinguishes hover from scope:

- Hovered project: the row under the Projects cursor.
- Active project scope: the project selected with `Space`, or none.
- Hovered task: the row under the Tasks cursor.

Moving the Projects cursor changes only the hovered project and Projects right-column preview. It does not filter Tasks or Summary. Pressing `Space` on the hovered project sets active scope. Pressing `Space` again on the same active project clears scope.

When scope changes:

- Tasks refresh to all tasks or scoped-project tasks.
- Summary refreshes to all-project or scoped-project aggregates.
- Existing cursors are preserved where possible; otherwise they clamp to the nearest valid row.

## Pane Behavior

### Projects

The Projects pane lists all projects regardless of active scope. Rows visually distinguish:

- Focused pane cursor.
- Active scoped project.
- Non-selected projects.

When Projects is focused, the right column is a vertical stack of navigable panes:

- Facts/edit pane: code, name, type axis, created/updated metadata, rename/type-axis actions.
- Labels pane: allowed labels, add/remove label actions, retained usage notes.
- Repos pane: repo paths, add/remove repo actions.
- Guide pane: guide sections, refs, freshness status, guide edit actions.
- Advanced pane: removal/destructive actions guarded by confirmation and store constraints.

Left/Right navigation selects between these stacked right-column panes while Up/Down remains reserved for movement inside the focused left pane. Forms stay field-based and call the same store functions as the CLI-backed behavior.

### Tasks

The Tasks pane lists all tasks by default. If a project is scoped, it lists tasks only for that project. The cursor selects the task shown in the right column.

When Tasks is focused, the right column includes:

- Task facts: id, project, title, description, status, labels.
- Actions: status, edit title/description, claim/unclaim, add/remove labels.
- Dependencies and links: outgoing links, computed incoming links, stale markers.
- Entries and timeline: todos, followups, discussions, review events, history.
- Actor/claims context: claimant, assignee where present, current actor action affordances, and related workload signals available from store reads.

Actors are not a left pane or top-level navigation item.

### Summary

Summary defaults to all projects and switches to the active project when scoped. The left Summary pane shows compact contextual metrics. The right column is a detailed dashboard:

- Counts by status.
- Review queue.
- Open followups.
- Guide health and stale/missing refs.
- Task queue and next-task candidates.
- Text charts using terminal-safe characters.

Summary must avoid adding new persistent analytics. It renders from existing store data.

### Help

Help remains static. The left Help pane can act as a compact topic index. The right column shows:

- Global keys.
- Pane-specific keys.
- Project-scope behavior.
- Form and overlay behavior.
- CLI/TUI parity notes.

## Keyboard Model

Global keys:

- `1` focus Projects.
- `2` focus Tasks.
- `3` focus Summary.
- `4` focus Help.
- `j`/`k` and Up/Down move inside the focused left pane.
- `Space` toggles project scope when Projects is focused.
- `r` refreshes store-backed data.
- `/` filters the focused list where applicable.
- `?` opens help or focuses Help, depending on current overlay state.
- `Esc` closes filters, forms, and overlays.
- `q` quits when no form/overlay is active.

Right-column section navigation uses Left/Right when the focused pane exposes stacked sections, especially Projects. Up/Down continues to move the cursor inside the focused left pane. Key conflicts must favor active form input first, overlay second, filter third, then workspace navigation.

## Data Flow

Refresh builds a workspace snapshot from store reads:

- Projects list.
- Task list with current filters and scope.
- Summary/dashboard data for all projects or scoped project.
- Context for the hovered task when needed.
- Guide status where Summary or Projects detail needs it.

Mutations call existing `store.*` functions and then refresh the affected snapshot. The TUI must not create a separate data path, must preserve actor provenance, and must surface the same stable error classes used by CLI behavior.

## Error Handling

Errors appear inline in forms or as footer/toast messages. Existing stable semantics are preserved:

- `2 usage` for invalid input.
- `3 not-found` for missing task/project references.
- `4 conflict` for duplicate labels/projects, invalid transitions, stale claims, or guarded removals.

Destructive operations require confirmation. Disabled or unavailable actions should show an explicit hint rather than fail silently.

## Testing

Model-level tests should cover:

- `1`-`4` switch focused left pane.
- `j`/`k` move only inside the focused pane.
- `Space` on a project sets scope, and pressing it again clears scope.
- Tasks default to all projects and filter when scoped.
- Summary defaults to all projects and filters when scoped.
- Actors are absent from primary navigation.
- Task right column includes actor/claims context.
- Projects right column exposes stacked action panes and navigates between them.
- Help right column remains static.
- Mutating actions still call store-backed flows and refresh state.

Full verification remains `make verify`.

## Compatibility Impact

This design intentionally changes the behavioral contract described by the existing tab-based TUI contract and mockups. The CLI and `internal/store` contracts remain stable. During implementation planning, the tab-era TUI docs should either be updated or clearly superseded so future agents do not implement against conflicting navigation requirements.
