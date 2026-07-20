# Capability view — design

Date: 2026-07-20
Status: approved design (brainstormed 2026-07-20; tracking task ATM-90171b)
Builds on: `2026-07-19-boards-management-v2-design.md` (capability-authored ring, unmanaged umbrella, `BoardsConfig`), `2026-07-18-capability-namespace-manager-actions-v2-design.md` (enable/disable gate, `Registry.For`).

## Problem

After boards-management v2, the Tasks pane ([2]) ring is capability-authored, but it is still *flat*: every enabled capability's boards share one ring, with the owner shown as a muted tag per row and the unmanaged umbrella appended as a pseudo-board. The capability a board belongs to is an attribute, not a context. Working "in" workflow vs "in" contextmap means visually filtering a mixed ring, and the unmanaged umbrella occupies a ring slot even though it behaves nothing like a board.

## Goal

Make capability the first-class navigation context of pane [2] — the **Capability view**:

- Pane [2] is always in exactly **one current capability**: an enabled capability or the pseudo-capability `unmanaged`. The flat all-boards ring is removed.
- A **[C]apabilities switcher** (centered modal overlay) lists capabilities, shows enabled state, switches the current one, and enables/disables capabilities in place.
- The ring shows **only the current capability's boards**, with width layouts scaled to board count (1 → full width, 2 → 70/30, 3+ → today's 25/50/25).
- **`unmanaged` is a selectable capability, not a ring row**: no ring, a full-width label drill-down, and tasks appear only after a label is selected (never an unfiltered sweep — performance guarantee).
- The tasks-list header reads `CAPABILITY: <name>    TOTAL: <cap>/<project> tasks    SORT: <mode>`, replacing `PROJECT:`/`FOCUS:`.
- The **LABELS column is removed** from the tasks table.
- The pinned-tabs box moves to the **very bottom** of pane [2].

Out of scope: any change to what capabilities expose (`Exposed`/`Vocabulary` contracts), the `Registry.Unmanaged` subtraction, CLI verbs (no new CLI; `atm project capability add/remove` remains the scripted path), the board-authoring flow, and pane [1].

## Decisions (from brainstorm)

1. **Always in a capability.** No "all capabilities" flat mode remains.
2. **Header counts = capability-total / project-total.**
3. **`C` is contextual**: capabilities switcher when pane [2] is focused; conventions overlay elsewhere (pane [1], as today).
4. **Current capability persists per project** in `config.json` → `boards.capability`.
5. **Ring layouts**: 1 board → 100%, 2 → 70/30 (selected/next), 3+ → 25/50/25 prev/selected/next.
6. **Unmanaged idle = empty list + hint** ("select a label to see its tasks"); no query runs until a label is selected.
7. **`unmanaged` always listed** in the switcher, with its label count; selectable even when empty ("nothing unmanaged" drill-down).
8. **Pins are global** across capabilities; a pin jump switches capability; the pinned-tabs box renders at the bottom of the pane.
9. **Switcher is a centered modal** (option A mockup) over a dimmed pane; on open the cursor sits on the current capability's row.
10. **Tasks table drops the LABELS column** (flat and grouped variants): `ID  TITLE  UPDATED`. Labels remain visible in task detail and through the board/drill-down context.

## Design

### 1. Concept & persistence

**Current capability** is a per-project TUI state: one of the project's enabled capability names, or the sentinel `unmanaged`. It is persisted in `core.BoardsConfig`:

```go
type BoardsConfig struct {
    Order      []string `json:"order,omitempty"`
    Hidden     []string `json:"hidden,omitempty"`
    Pins       []string `json:"pins,omitempty"`
    Capability string   `json:"capability,omitempty"` // current capability view
}
```

Written through the existing `SetProjectBoards` read-modify-write on every switch (TUI actor stamped). Reads via `GetBoardsConfig`. No new store event; older binaries ignore the key.

**Resolution rule** (applied on load and on every refresh): if the persisted value is not `unmanaged` and not in the project's enabled set, fall back to the first enabled capability in registration order; if none are enabled, `unmanaged`. The resolved value is *not* written back on read — only explicit switches write.

### 2. `capabilityModel` — new `internal/tui/capabilities.go`

Owns the capability concern only. State: entry list, current name, overlay open/closed, overlay cursor.

**Entries** are built on refresh from `registry.Describe()` (every registered capability: name + summary) joined with the project's enabled set (`Project.Capabilities` via `Model.regFor`), plus one `unmanaged` entry appended last carrying the unmanaged label count and owned-task count.

**Overlay** (centered modal over dimmed pane [2], help-overlay style):

- Row format: marker (`▶` current), enabled state (`●` enabled / `○` disabled / `—` for unmanaged), name, summary, count (`6 boards` for capabilities, `5 labels · 12 tasks` for unmanaged).
- On open, the cursor starts on the current capability's row.
- Keys: `↑/↓` move; `Enter` switch to the row under the cursor — if disabled, **enable it and switch in one stroke**; `space` toggles enable/disable without switching; `Esc` closes without changes.
- Enable/disable call the existing `EnableCapability`/`DisableCapability` repository methods with the TUI actor, then `refreshAll()`. Disabling the *current* capability applies the resolution rule immediately. A disabled capability's labels flow to unmanaged automatically (`Registry.Unmanaged` already computes this).

**Switch flow**, one direction, no cycles: `capabilityModel` updates current → persists (`SetProjectBoards`) → `boardsModel` rebuilds for the new scope → `applyFocus` resets the task filter. On persist failure the in-memory switch still happens; the error goes to the status line.

### 3. Ring under a capability — `boardsModel` changes

- `buildBoardRows` filters `reg.For(project).Exposed(code)` to rows with `Owner == current`. No umbrella append; `capability.UmbrellaFullName` sentinel, its shadow guard, and the ring's owner-tag column are **removed** (the header names the owner).
- `hidden`/`order` config apply within the scope, unchanged semantics. Entries referencing other capabilities' boards stay in config, inert until that capability is current.
- `selectDefault`: `all-tasks` if the capability exposes it, else the first ring row.
- **Widths** (`splitStripWidths` gains the row count): 1 board → single full-width cell; 2 boards → 70% selected / 30% next (`[`/`]` swap); 3+ → today's 25/50/25 prev/selected/next, wraparound. Strip height stays `stripHeight = 8`.
- All in-capability keys keep today's meanings: `[`/`]` cycle, Shift-arrows drill/chart-cursor, `p` pin, `n/e/S/d/l` board authoring.

### 4. Unmanaged mode

When current = `unmanaged`, `boardsModel` renders no ring. The strip area (same height) shows the **umbrella drill-down full-width**: `buildUmbrellaRows` over `reg.Unmanaged` with the existing meter-bar rendering, as the top and only level (Esc does not climb out of it; there is no ring above). Existing umbrella keys apply: Shift-↑/↓ label cursor, Shift-→ drill namespace rows into chart/detail, Shift-← back.

**Selection mechanism**: on entering unmanaged mode the label cursor is *unset* and the tasks list renders **empty with a hint** ("select a label to see its tasks") — no task query executes (extends today's `focusUmbrellaIdle`). The first Shift-↑/↓ places the cursor on a row and applies that label as the tasks filter — exact token for leaf labels, namespace-present for `ns:*` rows; every subsequent cursor move re-applies. Each query is label-filtered, so the unfiltered-sweep case never occurs. Board authoring keys and `p` are inert here; the hint line drops `[[/]]board`/`[p]` and shows `[Shift-↑/↓]labels  [Shift-→]drill`.

When the unmanaged set is empty, the drill-down shows "nothing unmanaged" and the tasks list keeps the idle hint.

### 5. Header & counts

`headerLine()` (`tasks_list.go`) becomes:

```
CAPABILITY: workflow    TOTAL: 150/200 tasks    SORT: updated-desc
```

- `200` = project task total (existing `listTaskIDs` count).
- `150` = **capability-owned task count**: tasks carrying at least one label the capability owns, per the same ownership rule as `Registry.Unmanaged` (vocabulary FullNames + members of owned `:*` descriptors). For `unmanaged`, the existing `unmanagedTaskCount`. Deliberate consequence: workflow's count reflects tasks on the paved road (`status:`/`priority:` labeled), not the all-tasks board match — otherwise it would always equal the project total and carry no signal.
- To avoid duplicating ownership logic in the TUI, `internal/capability` gains one pure read: `Registry.OwnedLabels(code, capName string) []core.Label` (the per-capability vocabulary + descriptor set `Unmanaged` already unions internally). The TUI counts tasks against it the way `unmanagedTaskCount` counts today.
- Counts are computed on refresh, never per frame.

`PROJECT:`/`FOCUS:` are dropped: the project shows in pane [1]; the active filter shows in the strip/pinned tabs.

### 6. Tasks table — LABELS column removed

Flat list header becomes ` ID  TITLE  UPDATED`; the freed width goes to TITLE. The grouped/tree variant drops its label column the same way. The `showing X-Y of Z` footer is unchanged. Labels remain visible in the task detail view and are implied by the board/drill-down context that produced the list.

### 7. Pins & pane stacking

Pins stay one global per-project list (`boards.pins`, max 3), spanning capabilities. `p` toggles on ring boards (inert in unmanaged mode); `Shift-1..3` jumps to a pin — if its owner is not the current capability, the jump **switches capability first** (persisting, full switch flow), then selects the board. `Shift-0` recenter unchanged.

Pane [2] vertical order becomes: header → tasks list → board strip (or unmanaged drill-down) → **pinned-tabs box** (`renderListWithStrip` reorders; pinned box keeps `pinnedBoxHeight = 3`).

### 8. Keys & help

- `statusHint()` list mode: `[C]apabilities  [↑/↓]tasks  [[/]]board  [s]ort  [a]dd  [p]pin/unpin  [Enter]detail  [?]keys` — `[C]` first. Unmanaged variant per §4.
- `handleListKey` routes `C`: pane [2] focused → open switcher. Pane [1] keeps `C` = conventions overlay. `keymap.go` `keymapRows` documents the split (`C` row: Projects = conventions, Tasks = capabilities); the `?` overlay picks it up via `keymapTable()`.
- While the switcher is open, all keys route to `capabilityModel` (`↑/↓/Enter/space/Esc`); other input is swallowed, matching existing overlay behavior.

### 9. Removals

- Umbrella ring row, `UmbrellaFullName` usage in ring building, sentinel shadow guard, `selectDefault` umbrella-skip.
- Ring owner-tag column.
- `focusUmbrellaIdle` naming may stay but its meaning narrows to unmanaged-mode idle.
- Tasks table LABELS column.

Note `lLevelUmbrella` and the umbrella build/render code survive — repurposed as unmanaged mode's main surface.

### 10. Edge cases

- **Capability exposing zero boards**: strip shows a "no boards exposed" placeholder; tasks list unfiltered (project scope).
- **Zero enabled capabilities**: resolution lands on `unmanaged`; the switcher is the way back (Enter enables + switches).
- **Persisted capability disabled/unknown** (e.g. by CLI while TUI runs): resolution rule falls back on next refresh; no write-back.
- **Concurrent CLI enable/disable**: periodic `refreshTickCmd` rebuilds entries; a vanished current capability triggers the resolution rule.
- **Persist failure on switch**: in-memory switch proceeds; status line shows the error.

## Architecture / layering

- `internal/core` — `BoardsConfig.Capability` field.
- `internal/store` — `config.go` round-trips the new field (no emptiness-check change needed: a boards config with only `Capability` set still round-trips through the existing `Boards == nil` check added in v2).
- `internal/capability` — one pure read `Registry.OwnedLabels(code, capName)`; `Unmanaged` refactored to use it internally so the ownership rule stays single-sourced.
- `internal/tui` — new `capabilities.go` (`capabilityModel`); `labels.go` sheds umbrella-in-ring + owner column and gains scope filtering; `thumbnails.go` width variants + stacking order; `tasks_list.go` header, column removal, `C` routing, hints; `keymap.go`/`help.go` rows.
- No CLI changes.

## Testing

- **`internal/capability`**: `OwnedLabels` per capability matches what `Unmanaged` subtracts (property: `LabelList − ⋃ OwnedLabels(enabled) == Unmanaged`); stable across calls.
- **`internal/store`**: `Capability` field JSON round-trip; boards config with only `Capability` set persists and reads back.
- **`capabilities_test.go`**: entry building (enabled/disabled/unmanaged-last); overlay cursor starts on current; Enter switches; Enter on disabled enables + switches; space toggles without switching; Esc no-op; disable-current fallback; resolution rule (disabled persisted value, unknown value, zero enabled); persistence write on switch; persist-failure keeps in-memory switch.
- **`labels_test.go`**: ring scoped to current owner; no umbrella row; owner column gone; `selectDefault` within capability; unmanaged mode renders drill-down with no Esc-to-ring; label selection sets the filter; empty unmanaged set copy.
- **`thumbnails_test.go`**: `splitStripWidths` for n=1 (100%), n=2 (70/30), n≥3 (25/50/25); pane stacking with pins at bottom.
- **`tasks_list` tests**: header format + counts (capability-owned / project); LABELS column absent in flat and grouped renders; `C` opens switcher in pane [2] and conventions in pane [1]; hint variants (normal vs unmanaged).
- **Existing tests**: umbrella-in-ring tests (sentinel shadowing, umbrella row presence, `selectDefault` skip) deleted or repurposed to unmanaged mode; owner-tag render assertions removed; task-list goldens updated for the dropped column and new header.

## Migration / compatibility

- Existing `config.json` without `boards.capability` → resolution rule (first enabled capability). No write until the user switches.
- Rollback to an older binary: the `capability` key is ignored; nothing breaks.
- No store events, no event-log schema change, no cache change.
