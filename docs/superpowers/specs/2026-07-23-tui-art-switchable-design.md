# TUI art: switchable per-project, default off, dual-pane pair

- **Task:** ATM-cac464 (revision of the shipped ATM-4eae82 art feature)
- **Date:** 2026-07-24 (this revision supersedes the 2026-07-23 design; the
  locked decisions below differ from that earlier version)
- **Status:** Approved design, pre-implementation
- **Branch:** worktree-atm-art-backgrounds (builds on the completed ATM-4eae82
  work, unmerged)

## Problem

The shipped art (ATM-4eae82) auto-assigns a single hash-picked theme to every
project and renders it in the Projects pane for the cursor row even when no
project is selected — so art appears immediately on TUI startup, before the
user has chosen anything. The art set is also the original five procedural
themes, and there is no in-TUI way to turn art on or off (only the removed
`atm project theme` CLI set a single theme name).

The user wants art to be an opt-in per project, shown as a **pair** of
graphics (one in each of the two existing art canvas spaces — the Projects
pane slot and the Tasks pane gap), drawn from a refreshed six-theme motion
set, defaulting off and toggled from the keyboard.

## Goals

- Art is **off by default**; a project shows nothing until the user turns it
  on.
- Each project has a stable **pair** of two distinct themes sampled
  deterministically from the registry by the project code
  (`art.Pair(code) [2]Theme`). The pair is random-looking but stable per
  project and generally differs across projects.
- When art is on for a scoped project, **both** pinned themes render
  simultaneously: pair[0] in the Projects pane art slot, pair[1] in the Tasks
  pane gap. There is no single-active-theme cycling.
- A single key **`A`** toggles all art for the scoped project on/off
  (persisted). No project scoped → no-op with a status hint.
- Art renders **only when a project is selected** (`projectScope != ""`),
  using the scoped project — fixing the "appears on startup" behavior.
- The on/off state **persists** per project and survives restart.
- Theme switching is **TUI-only**; the `atm project theme` CLI is removed.

## Non-goals

- Changing the rendering/animation engine (the `Frame`/`Theme`/`Render`
  pipeline is unchanged; the same 600ms idle-gated tick drives both panes).
- A picker overlay or per-pane independent choice — the pair is assigned, not
  hand-picked, and both show together when art is on.
- Re-rolling the pair from the keyboard. `A` is a binary toggle only; the
  pair for a project is fixed by its code. (A future task may add re-roll.)

## Theme registry — six motion themes

The registry is replaced with six motion-graphics themes, registered in this
fixed order (order is part of the `Pair` contract — append-only from here):

1. `galaxy` — spiral galaxy: bright accent core + 2 logarithmic arms of
   stars, slow rotation, twinkling stars.
2. `lorenz` — Lorenz strange-attractor: a particle integrates each phase;
   its trail draws as a fading phosphor line.
3. `matrix` — dense matrix-rain columns with brightness-gradient heads and
   dimmer trailing tails.
4. `tunnel` — perspective flight through square rings receding to a
   vanishing point; motion is into the screen. Nearest ring accent.
5. `skyline` — night-city silhouette: buildings with a height gradient, a
   clean roof bar, a window grid lit by a diagonal lights sweep.
6. `constellation` — star graph connected by faint shifting lines; stars
   twinkle.

The original five themes (`waves`, `starfield`, `circuit`, `rain`, `dunes`)
and their files are **removed**. The new themes follow the existing `Theme`
interface (`Name()`, `Draw(*Frame, seed, phase)`) and the existing two-lane
color model (base + accent via `Frame.Set`/`SetAccent`). They collapse below
the existing `art.MinW`/`art.MinH` thresholds like the old themes.

CPU note (measured on the prototypes at 120x20): each theme's `Draw` is
1-6 us; full `Render` is ~2.5 ms and is dominated by the fixed ntcharts canvas
blit, not the theme. All six are effectively equal in cost; richer-looking
themes are not more expensive.

## Design

### Assignment model — stable pair, default off

- **Remove** the always-on hash auto-assignment. `art.Effective` no longer
  falls back to a hashed theme: it returns the pinned theme when its name
  matches a registered theme, else **nil** (none). `art.For` (the old
  hash-to-theme pick) is removed — nothing uses it after this change.
- **Add** `art.Pair(code string) [2]Theme` — deterministically samples two
  *distinct* themes from the registry, seeded by the project code (FNV-1a, the
  existing `Seed`). Stable per code (ATM always offers the same two),
  distinct across projects. Requires the registry to hold >=2 themes (it
  holds 6).
  - Algorithm: `i = Seed(code) % n`; `j = (Seed(code)/n) % (n-1)`; if `j >= i`
    then `j++` (so `j != i`); return `[registry[i], registry[j]]`.
- `art.Seed(code)` stays (it seeds the per-project *layout* passed to
  `Render`, independent of theme choice).

### Persistence model — on/off flag, not a theme name

The persisted per-project art state changes from a single theme name to a
single **on/off boolean**. The pair itself is derived from the code (never
persisted), so only the toggle needs storing.

- **Replace** `ProjectConfig.ArtTheme string` with
  `ProjectConfig.ArtOn bool` (`json:"art_on,omitempty"`).
- **Rename** the store setter `SetProjectArtTheme(code, theme, actor)` to
  `SetProjectArtOn(code, on bool, actor string) error`, keeping the same
  read-modify-write-under-lock shape (mirrors `SetProjectBoards`). The old
  setter is removed.
- Update the store's "config empty" guard in `internal/store/config.go` to
  key on `ArtOn` instead of `ArtTheme` (the `c.ArtTheme == ""` conjunct
  becomes `!c.ArtOn`).
- The in-memory TUI cache `m.artPins map[string]string` becomes
  `m.artOn map[string]bool`, loaded from `cfg.ArtOn` at refresh.

### Rendering — both panes, gated on scope + on

- Projects pane `renderArt(height int)` (`internal/tui/projects.go`): render
  only when `projectScope != ""` **and** `m.artOn[scope]` is true. Resolve the
  theme as `art.Pair(scope)[0]`; if the theme is nil or the region is below
  `art.MinW`/`art.MinH`, the space stays blank padding.
- Tasks pane `fillGapWithArt` (`internal/tui/tasks_list.go`): already gates on
  `projectScope`; add the same `m.artOn[scope]` check and resolve
  `art.Pair(scope)[1]`. Below threshold → no art (unchanged behavior).
- Both panes use the existing shared `m.artPhase` and `art.Seed(scope)`, so
  the pair animates in lockstep and the layout is deterministic per project.
- Net effect: startup (no scope) → no art anywhere; after selecting a project
  with art on → pair[0] in Projects pane, pair[1] in Tasks pane; with art off
  → nothing in either pane.

### Toggle key `A`

- Bound in **both** the Projects and Tasks pane key handlers, so it acts on
  whichever pane is focused. Both operate on `projectScope`.
- No project scoped → no-op with a status hint ("select a project first").
- Scoped: flip `m.artOn[scope]`, persist via
  `SetProjectArtOn(scope, !on, actor)`, and flash the result in the status
  line ("art: on" / "art: off"). Repaint is automatic. Because the pair is
  derived from the code, toggling on never needs to choose a theme — it just
  shows the project's fixed pair.
- The toggle is the **only** art key. There is no cycle, no per-pane choice,
  no re-roll.

### CLI removal

- Remove `newProjectThemeCmd` and its registration in `newProjectCmd`
  (`internal/cli/project.go`), and delete the `TestProjectTheme` tests
  (`internal/cli/project_test.go`).
- Keep the store method (renamed to `SetProjectArtOn`), `ProjectConfig.ArtOn`,
  and their store tests — they back the TUI persistence.

### Keymap / help

- Add `A` to the keymap table (`internal/tui/keymap.go`): Projects = "toggle
  project art", Tasks = "toggle project art". `a` (add project / add task) is
  unchanged. `T` (UI palette) is unchanged.

## Testing

- `art.Pair`: deterministic, returns two distinct registered themes, stable
  per code, varies across codes; panics-free with the 6-theme registry;
  never returns the same theme twice.
- `art.Effective`: pinned valid name → that theme; empty or unknown name →
  nil (no hash fallback). Update any existing Effective test that assumed the
  hash fallback.
- New theme `Draw` functions: each renders without panic across a range of
  sizes (incl. below-collapse) and phases; the six `Name()`s are unique and
  match the registry order.
- Store: `SetProjectArtOn` writes and clears `ArtOn` under lock; the config
  empty-guard treats `ArtOn==false` as empty. Update the existing
  `TestSetProjectArtTheme` to the new signature/field.
- Toggle: `A` with a scoped project flips `artOn[scope]` and persists;
  `A` with no scope is a no-op and does not write config.
- `renderArt`: blank when `projectScope == ""`; blank when `artOn[scope]` is
  false; renders `Pair(scope)[0]` when on. `fillGapWithArt`: same gating,
  renders `Pair(scope)[1]`.
- Remove the CLI `TestProjectTheme`; keep/convert the store config tests.
- Full `go test ./...` green and `make verify` clean.

## Migration / compatibility

- Schema change: `art_theme` (string) → `art_on` (bool). A project with a
  legacy `art_theme` from the now-removed CLI is **not** migrated to `art_on`
  — the old field is simply ignored on load (unknown fields are tolerated by
  the JSON decoder), and the project renders no art until the user presses
  `A`. This is acceptable: the pre-revision CLI was short-lived and unmerged.
  No store migration code is added.
- No other persisted structures change.

## Open question resolved

The earlier 2026-07-23 spec had key `A` *cycle* a single active theme
(`none -> art1 -> art2 -> none`). The user's 2026-07-24 decision replaces
that: both pinned themes show at once (one per pane) and `A` is a binary
on/off toggle. The cycle helper and its tests are not needed; the spec above
is the source of truth.