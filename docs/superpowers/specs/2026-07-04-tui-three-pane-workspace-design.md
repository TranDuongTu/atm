# ATM TUI Three-Pane Workspace — Design Spec

**Status:** Approved from user feedback on fullscreen TUI layout.
**Scope:** Follow-on layout and navigation refinement for the v2 Bubble Tea TUI.

## Driver

The current tabbed TUI leaves too much unused space on large terminals because
only one management surface is visible at a time. The centered pane bodies also
make the left-aligned tab navigation feel visually detached. ATM should use the
fullscreen terminal as a persistent management workspace: projects remain
visible as the anchor, tasks remain visible as the primary work surface, and
labels remain visible as the reconciliation surface.

This refinement replaces top tabs with a lazygit-style three-pane workspace.
It changes rendering and focus navigation only; it does not change the store
API, label substrate, task filtering semantics, mutation behavior, or CLI.

## Goals

- Remove the persistent top tab bar.
- Render Projects, Tasks, and Labels together on one screen.
- Keep Help as an overlay opened by `?`, not as a fourth persistent pane.
- Preserve pane-local list/detail modes.
- Preserve existing forms, confirms, toasts, task filtering, sorting, and
  mutations.
- Make focus visible through pane borders and titles.
- Keep the bottom status line as the only persistent global chrome.

## Non-Goals

- No store changes.
- No CLI changes.
- No new entity types or label semantics.
- No mouse-driven redesign.
- No overlay-based entity detail views.
- No full-screen ANSI snapshot tests.

## Workspace Layout

The root TUI renders one persistent workspace above the status line:

```text
┌─ [1] Projects ───────────────────────┐┌─ [2] Tasks ─────────────────────────┐
│ ... projects list or project detail  ││ ... tasks list/group/detail         │
│                                      ││                                      │
│                                      │├─ [3] Labels ────────────────────────┤
│                                      ││ ... labels list or label detail      │
│                                      ││                                      │
└──────────────────────────────────────┘└──────────────────────────────────────┘
STORE: <path>  SELECTED: <CODE>  theme: <name>  <focused-pane keys> actor: <id>
```

The Projects pane occupies the left column for the full workspace height. The
right column is split vertically into Tasks above Labels. Width allocation is
approximately 40 percent Projects and 60 percent right column. The Tasks and
Labels panes split the available workspace height approximately evenly.

Each pane has a border and a title that includes its numeric focus key:
`[1] Projects`, `[2] Tasks`, `[3] Labels`. The focused pane uses the active
border/accent style. Unfocused panes keep a quieter border so they remain
readable without competing for focus. There is no top navigation bar and no
extra display above the workspace.

When terminal dimensions are too small for the ideal split, panes should still
render bounded content instead of panicking. The existing width guards,
truncation helpers, and height clamping continue to apply. The exact fallback
may be cramped, but it must preserve the same three pane identities and status
line.

## Focus And Navigation

`1`, `2`, and `3` change focus to Projects, Tasks, and Labels respectively.
They no longer switch tabs or hide other panes. The focused pane alone receives
pane-local keys.

The root key handling order remains:

1. quit and global controls
2. active help/keymap overlay
3. confirm overlay
4. form overlay
5. task filter editing
6. global focus keys and theme key
7. pane-local key dispatch

`q` and `ctrl+c` still quit when no blocking overlay owns input. `T` still
cycles theme. `?` opens the help overlay. The status line shows the focused
pane's key hints.

## Pane-Local State

Entity detail views stay pane-local:

- `Enter` in Projects opens project detail inside the Projects pane.
- `Enter` in Tasks opens task detail inside the Tasks pane, or toggles a group
  header as today.
- `Enter` in Labels opens label detail inside the Labels pane.
- `Esc` backs the focused pane from detail to list, or cancels task filter
  editing in Tasks.

Detail views must not become overlays. Forms and confirms already use overlays;
making details overlays would produce overlay-on-overlay interactions during
editing. Keeping details pane-local matches the existing `projectsModel`,
`tasksModel`, and `labelsModel` list/detail state and keeps overlays reserved
for transient actions.

## Help Overlays

Help is no longer a persistent fourth tab or pane. There are two read-only
reference overlays, kept separate so each stays compact and readable:

- `?` opens the **Keys** overlay: the CLI/TUI parity table and the global
  keymap. Pressing `?` or `Esc` closes it.
- `C` opens the **Conventions** overlay: the full onboarding guide and
  suggested label namespaces. Pressing `C` or `Esc` closes it.

While one overlay is open, pressing the other reference key switches which
overlay is shown (so a user reading conventions can jump straight to the
keymap without closing first). Both overlays render as a clean full-body
replacement of the workspace area — the panes do NOT show through (an
earlier char-overlay splice produced unreadable output when the box was
nearly fullscreen). The status line remains visible beneath the overlay.
Help remains read-only: mutating keys do not perform actions while an
overlay is open.

The existing keymap overlay and Help tab should converge into these two
help overlays. There should not be both a small keymap overlay and a separate
Help tab after this refinement.

## Rendering Responsibilities

The root `Model` owns the workspace split and pane borders. The existing pane
models continue to own their content, cursor state, list/detail state, filters,
sorting, and mutations. `SetSize` calculates the inner size for each pane and
passes those sizes to the pane models.

The root view no longer calls `renderTabBar` or renders tab separators. It
renders the bordered workspace, the status line, and any active overlay layers.
Forms, confirms, help overlay, and toasts continue to layer above the rendered
workspace.

Pane body content should not repeat its pane title. The pane border title
already identifies Projects, Tasks, and Labels. Entity detail titles remain
useful and should stay, such as `Project ATM`, `Task ATM-0001`, and
`Label ATM:status:open`.

## Keymap Updates

The global keymap reference changes from tab language to pane focus language.
It should document:

- `1/2/3`: focus Projects, Tasks, Labels
- `?`: open or close the Keys help overlay
- `C`: open or close the Conventions help overlay
- `T`: cycle theme
- `q / ctrl+c`: quit

Pane-specific rows keep their existing meanings where possible. Any Help column
from the old tab model is removed or replaced with overlay behavior.

## Error Handling

Rendering must not panic on narrow or short terminal sizes. If a pane's inner
height is too small, the pane renders as many lines as fit. If a pane's inner
width is too small, titles and body lines truncate through existing helpers.
Overlay rendering continues to clamp to the terminal dimensions.

## Testing

Implementation must add or update tests for:

- the root view renders all three pane titles at once
- the root view does not render a top tab bar
- `1`, `2`, and `3` change focus without hiding the other panes
- `?` opens a help overlay (Keys) from any focused pane
- `C` opens the Conventions help overlay from any focused pane
- Help is not reachable as `4`
- pane-local detail opens inside the focused pane, not as an overlay
- `Esc` backs only the focused pane out of detail
- focused pane styling changes when focus moves
- status line hints follow the focused pane
- forms and confirms still render above the three-pane workspace
- task filter editing still receives input before global focus dispatch
- narrow and short terminal sizes render without panic

Tests should assert stable text, model state, and small rendering invariants.
Do not add brittle full-screen ANSI snapshot tests.

## Verification

Before declaring implementation complete, run:

```sh
make verify
```
