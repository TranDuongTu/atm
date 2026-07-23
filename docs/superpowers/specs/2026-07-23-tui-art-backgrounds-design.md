# TUI per-project procedural art backgrounds

- **Task:** ATM-4eae82
- **Date:** 2026-07-23
- **Status:** Approved design, pre-implementation

## Problem

Both workspace panes carry dead vertical space that today renders as blank
padding:

1. **[1] Projects pane** — `projectPaneSplitHeights` (`internal/tui/projects.go:82`)
   splits the pane list 30% / events 35% / summary rest. On tall terminals the
   list section holds far more rows than the project count needs, and the
   spare height is blank.
2. **[2] Tasks pane** — `renderListWithStrip` (`internal/tui/tasks_list.go:226`)
   pads the task table down to `listContentHeight()` before the boards ring,
   so any space the task rows do not use is blank filler above the ring.

Beyond wasted space, projects have no visual identity: switching between
projects (ATM, INFRA, …) gives no immediate "where am I" cue.

## Goals

- Fill both gaps with decorative ASCII art that gives each project a stable,
  instantly recognizable look and feel.
- Art redraws to the exact available gap on every terminal resize.
- Subtle idle animation (shimmer/drift), never motion-heavy.
- Zero configuration required; optional manual pin per project.
- Art must read as background: it never competes with lists, tables, events,
  or charts, and it collapses away when space is tight.

## Non-goals

- Raster/image conversion (PNG → ASCII), external art files, or user-supplied
  art assets. All themes are procedural code.
- A TUI keybinding for art assignment. `T` remains UI-palette cycling only.
- Any event-log entry for theme changes (display preference, like
  `BoardsConfig`).

## Design

### Art engine — new package `internal/tui/art`

```go
type Theme interface {
    Name() string
    // Draw paints onto the canvas at exactly w×h. Deterministic for a given
    // (seed, w, h, phase): per-project variation comes from seed, animation
    // only from phase. No runtime rand.
    Draw(c *canvas.Canvas, seed int, w, h int, phase float64)
}
```

- Canvas is `github.com/NimbleMarkets/ntcharts/canvas`, already in the tree
  (used by `renderActivityStripeCanvas`, `internal/tui/projects.go:810`).
- A registry (ordered slice) maps names → themes. Auto-assignment is
  `hash % len(registry)`, so it is stable for a fixed registry: the same
  project shows the same motif every session. Growing (or shrinking) the
  registry in a future release may re-deal unpinned projects — accepted
  trade-off; pinning is the remedy for anyone who cares. Removing or renaming
  a theme additionally invalidates pins by that name (they fall back to
  auto).
- `Render(theme Theme, seed, w, h int, phase float64, st Styles) []string`
  allocates the canvas, calls `Draw`, and styles cells: dim base color and a
  sparse accent, both derived from the **active UI theme palette** so art
  harmonizes with whatever palette `T` has selected. The motif never changes
  with `T`; only its colors do.
- Seed = FNV-1a hash of the project code, so a theme's internal layout
  (star positions, trace routes, ridge offsets) is stable per project.

### Initial themes (5)

| Name | Motif | Animation |
|---|---|---|
| `waves` | layered rolling sine ridges (`~ ≈ -`) | slow horizontal roll, crest sparkles |
| `starfield` | sparse hash-placed stars (`· ✦ * .`) | per-star twinkle cycles |
| `circuit` | right-angle traces (`─ │ ┌ ┐ └ ┘`) ending in nodes (`○ ◉`) | node pulse |
| `rain` | droplet columns (`╷ ·`) with dry gaps | downward drift, per-column speed |
| `dunes` | layered noise ridges filled `░ ▒ ▓` | near layers drift slowly |

A glyph-level prototype validated all five (scratchpad `artproto/main.go`,
2026-07-23). Known fix carried into implementation: the circuit jog logic
needs proper corner bookkeeping (the prototype emits stray `┐─┘` / `└──└`
sequences).

### Assignment model

- **Auto (default):** `themeFor(code) = registry[fnv1a(code) % len(registry)]`.
  No state written; effectively pinned because the hash is stable — a project
  shows the same motif every session.
- **Manual pin:** `ProjectConfig` (`internal/core/config.go:31`) gains
  `ArtTheme string \`json:"art_theme,omitempty"\``. When set and known, it
  overrides auto-assign. When set but unknown (theme renamed/typo), fall back
  to auto-assign silently.
- **Setter:** `Store.SetProjectArtTheme(code, theme string)` following the
  exact RMW + `WithLock` pattern of `SetProjectBoards`
  (`internal/store/config.go`). Empty string clears the pin. Validates theme
  name against the registry (or `auto`). Exposed on the core service
  interface.
- **CLI:** `atm project theme <CODE> [<name>|auto]`
  - no arg: print effective theme, whether pinned or auto, and available
    theme names.
  - `<name>`: pin. `auto`: clear the pin.

### Projects pane layout

`projectPaneSplitHeights` (`internal/tui/projects.go:82`) becomes a 4-way
split, top to bottom:

1. **List — fixed page size of 5 data rows** (9 lines total: caption, header,
   rule, 5 rows, footer). `listPageSize` returns 5 regardless of height;
   `[` / `]` paging and `windowLines` behavior are unchanged.
2. **Art — flex**: absorbs all remaining height.
3. **Events** — keeps its current height rule (35% of pane, collapse under 4).
4. **Summary** — keeps the rest, at the bottom, as today.

On short terminals the art region shrinks first; below the collapse threshold
(see Edge handling) the pane renders exactly as today minus the taller list.
When no project is selected, the current events-fold behavior is preserved;
art still renders for the pane using the cursor row's project (or blank when
the project list is empty).

### Tasks pane layout

`renderListWithStrip` (`internal/tui/tasks_list.go:226`): the task table no
longer pads itself to `listContentHeight()`. Instead:

1. Table renders its actual rows (header + rows + footer).
2. **Art fills `listContentHeight() − tableRows`**, anchored directly above
   the boards ring.
3. Boards ring (`stripHeight = 8`) and pinned tabs (`pinnedBoxHeight = 3`)
   are unchanged.

Art uses the theme of the task pane's current project scope. When the table
fills the pane, the art region is zero and nothing renders — identical to
today. When no project scope is active, the region stays blank padding.

### Animation

- One `tea.Tick` at **600ms** advances a `phase` counter on the root model
  and triggers repaint.
- The tick is scheduled only while the workspace view is active: no overlay,
  no detail view, no form. Leaving those states resumes it; the phase clock
  is monotonic (missed time while paused is irrelevant — generators only need
  forward motion, not wall-clock fidelity).
- Generators are written so ≤ ~10% of cells change between adjacent phases —
  shimmer, not scrolling marquees.

### Edge handling

- **Collapse threshold:** art regions under 3 lines or 16 columns render as
  blank padding (today's behavior).
- Unknown pinned theme → auto-assign fallback (never an error, never blank).
- Empty registry or nil canvas failures → blank padding.
- All drawing clamps to canvas bounds; no panics at 1×1.

## Testing

- **Golden-frame tests per theme:** fixed (seed, w, h, phase) → exact glyph
  grid. Deterministic drawing makes these stable. One golden per theme at
  44×8 plus a cramped 30×4, and one animation pair (phase p vs p+1) asserting
  the changed-cell budget.
- **Split-height math:** table-driven tests for the new
  `projectPaneSplitHeights` (list fixed at 9 lines, art flex, events/summary
  rules preserved, collapse ordering) and the tasks-pane art height
  (`listContentHeight − rows`, zero-floor).
- **Assignment:** `themeFor` stability (same code → same theme), pin
  override, unknown-pin fallback.
- **Config RMW:** `SetProjectArtTheme` set/clear/validate, mirroring the
  boards config tests.
- **CLI:** `atm project theme` show/set/auto paths.

## Out of scope / future

- User-defined or plugin themes.
- Art in other views (detail panes, overlays).
- Cross-fade transitions between themes.
