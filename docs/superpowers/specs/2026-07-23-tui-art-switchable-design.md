# TUI art: switchable per-project, default off

- **Task:** revision of ATM-4eae82 (tracking id TBD)
- **Date:** 2026-07-23
- **Status:** Approved design, pre-implementation
- **Branch:** worktree-atm-art-backgrounds (builds on the completed ATM-4eae82 work)

## Problem

The shipped art (ATM-4eae82) auto-assigns a hash-picked theme to every project
and renders it in the Projects pane for the cursor row even when no project is
selected — so art appears immediately on TUI startup, before the user has
chosen anything. There is also no in-TUI way to change a project's art (only
the `atm project theme` CLI). The user wants art to be an opt-in per project,
switchable from the keyboard, and absent until a project is actually selected.

## Goals

- Art is **off by default**; a project shows nothing until the user turns it on.
- A single key **`A`** cycles the selected project's art: **none → art1 → art2
  → none**, where art1/art2 are a stable per-project pair.
- Art renders **only when a project is selected** (`projectScope != ""`), using
  the scoped project — fixing the "appears on startup" behavior.
- The chosen art **persists** per project and survives restart.
- Theme switching becomes **TUI-only**; the `atm project theme` CLI is removed.

## Non-goals

- Changing the five themes or the rendering/animation engine (unchanged).
- Any new persisted schema beyond the existing `ProjectConfig.ArtTheme`.
- A picker overlay — cycling with live preview is the whole interaction.

## Design

### Assignment model — stable pair, default none

- **Remove** the always-on hash auto-assignment. `art.Effective` no longer
  falls back to a hashed theme: it returns the pinned theme when
  `ProjectConfig.ArtTheme` names a registered theme, else **nil** (none).
- **Add** `art.Pair(code string) [2]Theme` — deterministically samples two
  *distinct* themes from the registry, seeded by the project code (FNV-1a, the
  existing `Seed`). Stable per code (ATM always offers the same two), distinct
  across projects. Requires the registry to hold ≥2 themes (it holds 5).
  - Algorithm: `i = Seed(code) % n`; `j = (Seed(code)/n) % (n-1)`; if `j >= i`
    then `j++` (so `j != i`); return `[registry[i], registry[j]]`.
- `art.Seed(code)` stays (it seeds the per-project *layout* passed to
  `Render`, independent of theme choice). `art.For` (the old hash→theme pick)
  is removed — nothing uses it after this change.
- `ProjectConfig.ArtTheme` persists the chosen theme **name**; empty/absent =
  none (the default). The store setter `SetProjectArtTheme` is unchanged.

### Rendering gate — only when selected

- Projects pane `renderArt`: render only when `projectScope != ""`, resolving
  the theme from the **scoped** project (not the cursor row):
  `theme := art.Effective(m.artPins[scope], scope)`; if `theme == nil` (none or
  below collapse threshold) → no art, the space stays blank padding.
- Tasks pane `fillGapWithArt`: already gates on `projectScope`; with
  `Effective` now returning nil for unset projects, an un-chosen project simply
  renders no art. No structural change beyond the `Effective` semantics.
- Net effect: startup (no scope) → no art anywhere; after selecting a project
  → its chosen art, or nothing if never turned on.

### Switch key `A`

- Bound in **both** the Projects and Tasks pane key handlers, so it acts on
  whichever pane is focused. Both operate on `projectScope`.
- No project scoped → no-op with a status hint ("select a project first").
- Cycle, computed from the scoped project's pair and current value:
  - `pair := art.Pair(scope)`; `cur := m.artPins[scope]`
  - `cur == ""` (none) → `pair[0].Name()`
  - `cur == pair[0].Name()` → `pair[1].Name()`
  - `cur == pair[1].Name()` → `""` (none)
  - anything else (e.g. a stale value) → `""` (normalizes to none)
- Persist immediately: `SetProjectArtTheme(scope, next, actor)`, update
  `m.artPins[scope] = next` in memory (so View reflects it without waiting for
  a refresh), and flash the result in the status line ("art: circuit" /
  "art: none"). Repaint is automatic.

### CLI removal

- Remove `newProjectThemeCmd` and its registration in `newProjectCmd`
  (`internal/cli/project.go`), and delete the `TestProjectTheme` tests
  (`internal/cli/project_test.go`).
- Keep the store method `SetProjectArtTheme`, `ProjectConfig.ArtTheme`, and its
  store tests — they back the TUI persistence.

### Keymap / help

- Add `A` to the keymap table (`internal/tui/keymap.go`): Projects = "switch
  project art", Tasks = "switch project art". `a` (add project / add task) is
  unchanged. `T` (UI palette) is unchanged.

## Testing

- `art.Pair`: deterministic, returns two distinct registered themes, stable
  per code, varies across codes; panics-free with the 5-theme registry.
- `art.Effective`: pinned valid name → that theme; empty or unknown name →
  nil (no hash fallback). Update any existing Effective test that assumed the
  hash fallback.
- Cycle logic (a small pure helper `nextArtTheme(pair, cur) string` so it is
  unit-testable without a Model): none→art1→art2→none; stale value→none.
- Key handling: `A` with a scoped project advances and persists; `A` with no
  scope is a no-op (and does not write config).
- `renderArt`: nil/blank when `projectScope == ""`; renders the scoped
  project's chosen theme; blank when the scoped project's `ArtTheme` is unset.
- Remove the CLI `TestProjectTheme`; keep the store config tests.
- Full `go test ./...` green.

## Migration / compatibility

- No schema change. Projects with no `art_theme` (the norm) render no art —
  the new default. A project whose `art_theme` was set by the now-removed CLI
  still renders that theme until the user cycles `A` (which will normalize it
  to none unless it happens to equal a pair member) — acceptable, and only
  reachable in stores that used the pre-revision CLI.
