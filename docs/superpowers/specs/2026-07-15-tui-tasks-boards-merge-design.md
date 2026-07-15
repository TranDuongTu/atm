# TUI Tasks/Boards Merge — Design Spec

**Status:** Approved from user feedback on the merged Tasks browsing pane.
**Date:** 2026-07-15
**Supersedes the layout of:** ATM-0082 (three-pane workspace) — the `[3] Boards` pane is removed; its logic merges into `[2] Tasks`.

## Driver

The three-pane workspace splits the right column 75/25 between Tasks and Boards. The Boards pane is a list of computed labels (boards + namespaces) with a three-level drill-down (table -> chart -> detail) that drives the Tasks pane's focus. Two panes for one workflow — browsing tasks by board — forces the user to context-switch between a list of boards and a separate list of tasks, and the 25% Boards slice is too short to read a chart breakdown comfortably.

ATM should present browsing tasks by board as a single, full-height experience. Boards become a thumbnail carousel above the task list, not a sibling pane. The three-level drill-down survives inside the selected thumbnail. The default board is "Open Tasks" — a capability-owned board, not a hardcoded namespace — and the user can pin boards for quick access.

## Goals

- Remove the `[3] Boards` pane; merge its logic into `[2] Tasks`.
- `[2] Tasks` takes the full right-column height.
- Boards render as a horizontal thumbnail strip at the top of `[2]`: prev (25%) / SELECTED (50%) / next (25%).
- The SELECTED thumbnail reuses the existing `boardsModel` level render (chart/detail), sized to the thumbnail.
- A single board ring (Open Tasks default -> boards + namespaces intermixed, sorted) is cycled by `[` / `]`.
- `Enter` opens the task detail (existing behavior); `>` / `<` drill the SELECTED thumbnail's levels.
- Arrows always browse the task list; `[]` always switch boards; one focus, fully modeless.
- The "Open Tasks" board is owned by a new capability (`internal/workflow`), ensured idempotently like `context-current`. No privileged label.
- Users can pin boards (`p`), jump to them (`Shift-1`..`Shift-9`), and pins persist per project.

## Non-Goals

- No store API changes beyond the new per-project `pins.json` helpers (reusing `ReadJSON` / `WriteFileAtomic` / `WithLock`).
- No label-substrate changes; `open-tasks` is a normal board.
- No new log actions.
- No mouse-driven redesign.
- No overlay-based entity detail views.
- No full-screen ANSI snapshot tests.
- No new compact thumbnail renderer; the existing level renderers are reused.

## Workspace Layout

The three-pane workspace becomes two panes:

```text
┌─ [1] Projects ─────────────────────┐┌─ [2] Tasks ───────────────────────────────────┐
│  projects list / detail            ││  [prev 25%] [ SELECTED board 50% ] [next 25%]  │
│  (full height, left column)        ││  ──────────────────────────────────────────────│
│                                    ││  task browsing list (arrows j/k)               │
│                                    ││  ...                                            │
│                                    ││  pinned: [1] open-tasks [2] status:*  ...       │
└────────────────────────────────────┘└─────────────────────────────────────────────────┘
STORE: <path>  SELECTED: <CODE>  theme: <name>  <focused-pane keys>  actor: <id>
```

- Left/right split stays 40/60 (`splitWorkspaceWidths` unchanged).
- `[1] Projects` keeps the full workspace height (unchanged).
- `[2] Tasks` now takes the full right-column height. `splitRightColumnHeights` and the second pane allocation are removed.
- Inside `[2]`, vertical stack from top to bottom:
  1. **Board thumbnail strip** — one row (prev / SELECTED / next), fixed height (~8 lines) so the chart bar breakdown stays readable.
  2. **Task browsing list** — fills the remaining height.
  3. **Pinned boards row** — one compact line at the bottom, only when pins exist.

Pane identity/titles: `[1] Projects`, `[2] Tasks`. The `[3]` title and `paneLabels` focus target go away. `numPanes` becomes 2; `3` no longer switches panes.

When terminal dimensions are too small for the ideal split, panes still render bounded content via the existing width guards, truncation helpers, and height clamping. The strip height clamps down (e.g. to 4 lines) on short terminals; the task list gets whatever remains.

## The Board Ring and the Open Tasks Board

### Board ring

Pane `[2]` holds a single ordered ring of browseable boards. Membership is the existing `buildBoardRows` output (boards with an `Expr` + emergent namespaces, intermixed, sorted by display name) — exactly what `[3]` lists today. The ring is rebuilt on `refresh()`.

### Default: Open Tasks

When a project is selected (or the ring is rebuilt and no board is selected), the SELECTED board is the **Open Tasks** board. If it is absent (a human deleted it), the capability re-ensures it on project select; if still absent, the ring falls back to the first board.

### Open Tasks board ownership — a new capability

A new package `internal/workflow` (mirroring `internal/contextmap`) owns the Open Tasks board's vocabulary:

```go
// internal/workflow/vocabulary.go
package workflow

import "atm/internal/store"

func BoardOpenTasks(code string) string { return code + ":open-tasks" }
func openTasksExpr() string             { return "status:open" }

// EnsureVocabulary creates the Open Tasks board with a description, if absent.
// Idempotent; never overwrites a human's curated description (LabelSeed upserts
// only when the label is absent). Self-bootstrapping: works without `atm label seed`.
func EnsureVocabulary(s *store.Store, code, actor string) error {
    return s.LabelSeed(BoardOpenTasks(code),
        "every open task: the project's active work. Default board in the TUI.",
        openTasksExpr(), actor)
}
```

- `LabelSeed` is the existing idempotent ensure primitive that `contextmap.EnsureVocabulary` uses. It never overwrites a human's curated description.
- The TUI calls `workflow.EnsureVocabulary` on project select, before rebuilding the board ring, so the default exists. The same ensure is invoked from the CLI project-select path so a non-TUI user gets the board too.
- **No privileged label.** A human can edit or delete `ATM:open-tasks` like any board (capability = paved road, not a fence). If deleted, the next project-select re-ensures it. The TUI selects by the well-known name `BoardOpenTasks(code)`; if absent after ensure, it falls back to `ring[0]`.
- The TUI render path never references `status:open`. It only knows the board's well-known name; the expression lives in the capability.

### Selection state

`boardsModel` gains a `selected` field (the selected board's `FullName`, e.g. `ATM:open-tasks` or `ATM:status:*`) replacing the implicit cursor-at-L0 used by the old list. `[` / `]` move the ring index; the SELECTED thumbnail and the Tasks list both follow the selected board.

## Thumbnail Strip Rendering

The top strip is one horizontal row, three cells: **prev (25%) / SELECTED (50%) / next (25%)** of pane `[2]` inner width.

```text
┌─────────────┬──────────────────────────────────┬─────────────┐
│  prev board │   SELECTED board                 │  next board │
│  (name)     │   (name + description)           │  (name)     │
│             │   ┌─ level content ────────────┐ │             │
│  N tasks    │   │ chart bar breakdown /      │ │  N tasks    │
│             │   │ label detail /             │ │             │
│             │   │ table preview              │ │             │
│             │   └────────────────────────────┘ │             │
└─────────────┴──────────────────────────────────┴─────────────┘
```

- **prev/next cells** are quiet: board display name + task count, rendered with the inactive border style. They orient the user for `[]` but hold no interactive content.
- **SELECTED cell** reuses the existing `boardsModel` level render for the selected board, sized to the 50% width:
  - Namespace board (e.g. `ATM:status:*`) at L0 -> the namespace chart (`renderChart`): bar breakdown of member labels. `>` drills into a member label's detail inside this cell; `<` returns to the chart.
  - Stored/board label (e.g. `ATM:open-tasks`, `ATM:next-sprint`) at L0 -> the label detail (`renderDetail`): name, usage, description. For Open Tasks this is the "label logic description."
  - The L0 flat table (`renderTable`) does not appear as a thumbnail level — the ring *is* the table. A board's thumbnail starts at its first meaningful level (chart for namespaces, detail for leaf boards).
- The strip height is fixed (~8 lines) so a chart's bars + the prev/next counts fit. The level content is padded/clamped to that height via existing `padToHeight` / `windowLines`.
- Because the renderer is reused, every existing boards-test invariant (chart bars, detail fields) holds inside the thumbnail without a second renderer.

### Sizing

A new helper splits pane `[2]` inner width into `25% / 50% / 25%` (each clamped to a minimum so narrow terminals still render a name). The SELECTED cell's `boardsModel.SetSize` is called with the 50% width and the strip height so `renderChart` / `renderDetail` window correctly.

## Navigation and Keys

Pane `[2]` has one focus. Keys, in dispatch order after global/overlay handling:

| Key | Action |
|---|---|
| `j` / `down`, `k` / `up` | Browse the **task list** below the strip (existing cursor nav). |
| `g` | Top of task list. |
| `[` / `]` | Move the board ring **prev / next**; SELECTED thumbnail + task list follow. |
| `Enter` | Open the **task detail** for the task under the cursor (existing behavior, always). |
| `>` | Drill the SELECTED thumbnail one level deeper (chart -> detail). |
| `<` | Climb the SELECTED thumbnail one level out (detail -> chart -> L0). |
| `Esc` | From task detail -> task list; cancel task filter editing. (Thumbnail level-climbing uses `<`, not Esc, to keep Esc scoped to the task list.) |
| `p` | Pin/unpin the SELECTED board. |
| `Shift-1`..`Shift-9` (`!` `@` `#` `$` `%` `^` `&` `*` `(`) | Jump to the 1st..9th pinned board. |
| `s` | Cycle task sort (existing). |
| `a` | New task (existing). |
| `n` / `e` / `S` | Board authoring on the SELECTED board (new / edit / seed defaults). These move out of the old `[3]` keyset into `[2]`, scoped to the selected board. |
| `d` / `l` | Label authoring. At the chart level (after `>` into a namespace board), operate on the chart-cursor's label (existing `describe` / `remove` behavior). At a leaf board's detail level, `d` describes that board's label. |

### Drill-down scope

`>` / `<` operate on the SELECTED thumbnail's internal level state. Arrows never touch the thumbnail — they always browse tasks. This keeps a single, modeless focus inside `[2]` (no sub-modes), matching "arrows browse tasks, `[]` switch boards."

### Tasks list focus coupling

The existing Boards -> Tasks focus coupling survives: when the ring selection changes, `[2]` calls `tasks.setFocus` with the selected board's focus (namespace present-focus for `status:*`; `focusOff` + the board's `FullName` filter for a leaf board like `open-tasks`). The task list below re-renders to that board's membership. The existing `taskFocus` / `focusCaption` machinery is reused unchanged.

### Removed keys

`3` no longer focuses a pane. The old `[3]`-specific `j/k/g/[]` list navigation inside the boards table is gone (the ring replaces it).

## Pinning and Persistence

### Pinning

`p` toggles pin on the SELECTED board. The pinned set is an ordered list of board `FullName`s, per project.

### Jumping

`Shift-1`..`Shift-9` (the shifted number row: `!` `@` `#` `$` `%` `^` `&` `*` `(`) jump to the 1st..9th pinned board — moves the ring selection to that board (same effect as cycling `[]` to it). Beyond 9 pins, extras are visible in the pinned row but have no jump key.

### Pinned row

A single compact line at the bottom of pane `[2]`, only when pins exist:

```text
 pinned: [1] open-tasks [2] status:* [3] next-sprint
```

Each entry shows its 1-based index and the board's display name. The row is read-only display; selection happens via the jump keys or `[]`.

### Persistence

A new per-project file under `$ATM_HOME`, mirroring `vocabulary.json`:

```
<store>/projects/<CODE>/pins.json
```

```go
type Pins struct {
    UpdatedAt time.Time `json:"updated_at"`
    Actor     string    `json:"actor"`
    Boards    []string  `json:"boards"` // ordered FullName list, e.g. "ATM:open-tasks"
}
```

- `GetPins(code) (*Pins, error)` — missing file -> `(nil, nil)` (empty-state, like `GetVocabulary`).
- `WritePins(code string, p *Pins) error` — validates actor, stamps `UpdatedAt`, writes under the existing per-project lock (`WithLock`) via `WriteFileAtomic`.
- Stored in `internal/store` alongside `vocabulary.go` (a generic per-project JSON file, not capability-owned data).

### TUI integration

`boardsModel` loads pins on project select and on `refresh()` (cheap read). `p` rewrites `pins.json` then refreshes. A pin whose board no longer exists is dropped on the next load (defensive prune, no error). The Open Tasks board is a normal pin candidate — not auto-pinned, so a human decides whether it occupies a slot.

## Capability CLI Surface

Following the capability pattern (`docs/architecture/label-substrate-and-capabilities.md`):

- `internal/workflow/vocabulary.go` — `EnsureVocabulary(s, code, actor)` ensures `ATM:open-tasks` (expr `status:open`) idempotently via `LabelSeed`. Pure vocabulary-ensure, no verbs, no private data format. This is a minimal capability: it owns one board's vocabulary and exposes nothing else (no recorder/reporter split needed — there is no machine-readable format, no verbs beyond ensure).
- Wired into the CLI so a non-TUI user also gets the board: the ensure runs on the project-select path, mirroring how `contextmap.EnsureVocabulary` is called from `atm context` recorders. The TUI and CLI share one ensure call site.
- `atm conventions` gains one line pointing agents at `ATM:open-tasks` as the default "open work" board (parallel to the existing `context-current` line). Golden conventions testdata updates accordingly.

## Rendering Responsibilities

The root `Model` still owns the workspace split and pane borders. `SetSize` calculates `[2]`'s inner size and propagates it to `tasksModel`, which now also owns the thumbnail strip + pinned row layout. `boardsModel` continues to own its level content, cursor state, and the focus coupling; it gains the ring selection and pin state. The strip renderer calls into `boardsModel` to render the SELECTED cell's level content at the 50% width.

The root view no longer renders a `[3]` pane or a right-column vertical join. It renders `[1]` + `[2]` horizontally, the status line, and any active overlay layers (forms/confirms/help/actors/plugin).

## Keymap Updates

The global keymap reference (`internal/tui/keymap.go`) changes:

- `1/2`: focus Projects, Tasks. `3` is removed.
- `[` / `]`: prev/next board (was prev/next page in Tasks; in the merged pane it switches the board ring).
- `>` / `<`: drill the SELECTED thumbnail in / out.
- `Enter`: open task detail (unchanged).
- `p`: pin board. `Shift-1`..`Shift-9`: jump to pinned board.
- `n` / `e` / `d` / `l` / `S`: board authoring on the SELECTED board (moved from the old `[3]` pane).

## Error Handling

Rendering must not panic on narrow or short terminals. If the strip height clamps below the chart's needs, the SELECTED cell renders as many lines as fit. If a cell's width is too small, names and counts truncate through the existing helpers. Pin file read errors surface as a toast and fall back to the empty pin set, never blocking the workspace.

## Testing

- `internal/workflow`: `EnsureVocabulary` is idempotent; creates `open-tasks` with the right expr/desc in a fresh project; does not overwrite a human-curated description; works without `atm label seed`.
- `internal/store`: `GetPins` / `WritePins` round-trip; missing file -> empty; prune of stale board pins on load.
- `internal/tui`:
  - Workspace renders `[1] Projects` + `[2] Tasks` only; no `[3]` title.
  - `2` focuses `[2]`; `3` is a no-op.
  - On project select, the Open Tasks board is ensured and selected by default; ring falls back to `ring[0]` if absent.
  - `[]` moves the ring; SELECTED thumbnail + task list follow; `>` / `<` drill the thumbnail levels; `Enter` opens task detail.
  - Strip width split is 25/50/25; SELECTED renders the namespace chart for `status:*`, the label detail for a leaf board.
  - `p` pins/unpins; `Shift-N` jumps; pinned row renders at the bottom; pins persist across a reload.
  - Narrow/short terminals render without panic.

## Verification

Before declaring implementation complete, run:

```sh
make verify
```