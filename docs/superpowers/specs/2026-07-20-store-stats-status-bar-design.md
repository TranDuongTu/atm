# Store stats in the status bar — design

Date: 2026-07-20
Status: approved design (brainstormed 2026-07-20; tracking task ATM-789528)

## Problem

The status bar's left side spends its space on low-value, mostly static text:
`STORE: <path>` (rarely changes mid-session), `SELECTED: <code>` (already
visible in the projects pane), and `theme: <name>` (cosmetic). Meanwhile the
store itself — how big it is, how many events it holds, which storage format
it runs — is invisible from the TUI, and the always-available `?`/`C`/`T`
keys are only discoverable through pane-specific hints. The app version is
not shown anywhere in the TUI.

## Goal

Replace the static text with a compact store-stats cluster and a fixed key
cluster, plus the app version:

```
⛃ v2 · ATM 1811 events · 2.4 MB  [a]dd [s]elect …  <toast>  ◆dock  [?]help [C]conv [T]theme  atm v1.2.11 ✓
└─ left: stats, pane hints, toast                           └─ right: docks · keys · version · refresh
```

- `STORE:`, `SELECTED:`, and `theme:` segments are **removed**.
- Left leads with the stats: format version, event count, and event-log
  size for the selected project (store-wide totals when none is selected,
  which also drops the code prefix).
- Right gains a fixed `[?]help [C]conv [T]theme` key cluster and the app
  version (dim), before the refresh-recency glyph.
- Per-pane hints stay, minus their now-redundant `[?]keys` /
  `[C]conventions` fragments.

Out of scope: any change to key *behavior* (`?`, `C`, `T` keep their current
bindings), plugin dock segments, the refresh-recency indicator, and the CLI
`atm version` command.

## Decisions (from brainstorm)

1. **Layout**: stats left, keys + version right (option A). Store path is
   dropped entirely — discoverable via CLI/env, not worth 15–40 columns.
2. **Stats follow the selected project**, falling back to store-wide totals
   when nothing is selected. (Revised during implementation: store-wide-only
   numbers never move as you navigate, which reads as a frozen status bar.
   The scoped counts are prefixed with the project code so they can't be
   mistaken for store totals.) `Version` stays store-wide in every scope —
   the storage format describes the store, not one project's slice.
3. **Size units adapt**: `<n> KB` under 1 MB, `<n.1> MB` at or above (a flat
   "0.0 MB" for small stores reads as broken).
4. **Store version** is the storage format: `v2` when every project (and the
   active format) agrees, `v1` likewise, `mixed` when per-project formats
   disagree; the store's `ActiveFormat` when there are no projects.
5. **Recompute on `refreshAll` only** — the result is cached on the Model;
   `View()` never touches the filesystem.

## Design

### 1. `core.StoreStats` — new seam on `MaintenanceService`

The TUI consumes `core.Service` only, so stats cross the hexagonal seam as a
plain value:

```go
// core/service.go
type StoreStats struct {
    SizeBytes  int64  // event-log file bytes in the requested scope
    EventCount int    // event lines in the requested scope
    Version    string // "v1", "v2", or "mixed" — always store-wide
}

// MaintenanceService gains — project "" means the whole store:
StoreStats(project string) (StoreStats, error)
```

`EventCount`/`SizeBytes` name what the numbers are without exposing file
layout; the storage-format admin surface otherwise stays on the concrete
store, unchanged.

### 2. Store implementation — `internal/store`

`(*Store).StoreStats(project)` delegates to a new engine helper in
`internal/store/eventlog`:

- Enumerate `ProjectCodesOnDisk()`.
- Per project, resolve the effective format via `ProjectFormat(code)`. Every
  project contributes its format to the version set; only projects in scope
  (all of them when `project` is `""`) contribute size and count. Pick the
  matching log file (`events.v2.jsonl` for v2, `log.jsonl` for v1).
- `os.Stat` for size; read the file and count `'\n'` bytes for the event
  count (each committed event/log entry is one newline-terminated line; an
  uncommitted partial tail therefore doesn't count). A missing file
  contributes zero — not an error.
- Version: collect the set of per-project formats; one distinct format →
  that format; more than one → `"mixed"`; no projects → `ActiveFormat` from
  store meta.

No locks taken: this is a read-only, advisory display path; a torn read
(event appended between stat and count) is harmless and corrected on the
next refresh. It must never fail the refresh — errors return zero-value
stats upstream.

### 3. TUI — `internal/tui`

- `Model` gains `storeStats core.StoreStats`, populated by a
  `refreshStoreStats()` helper (ignoring errors, keeping the previous value
  on failure). `refreshAll` calls it, and so does the project-select handler
  in `projects.go` — that handler deliberately runs no full `refreshAll`, so
  without the direct call the bar would keep the previous project's numbers
  until the next refresh tick.
- When a project is selected its code prefixes the counts
  (`⛃ v2 · ATM 1811 events · 2.4 MB`).
- `renderStatusLine` left side becomes:
  `⛃ <version>` (StatusLabel style) + ` · <n> events · <size>` (Status
  style), then the pane hint, then the toast. The `STORE:`/`SELECTED:`/
  `theme:` segments are deleted.
- Right side becomes: dock segments, `[?]help [C]conv [T]theme` (KeyMenu
  style), `atm <version.Version>` (StatusLabel/dim style), refresh glyph.
- A small `formatSize(bytes int64) string` helper implements the adaptive
  KB/MB rule (`0 KB`, `842 KB`, `1.2 MB`).
- Pane hints: `[?]keys` dropped from `projectsModel.statusHint()` variants
  and the `paneTasks`/fallback hints in `Model.statusHint()`; the fallback
  `"[?]keys [C]conventions"` becomes `""` (the fixed right cluster covers
  it).

### 4. Error handling

`StoreStats()` failure is non-fatal everywhere: the store returns whatever
it accumulated plus the error; the TUI logs nothing and keeps the last good
value. A brand-new empty store renders `⛃ v2 · 0 events · 0 KB`.

### 5. Testing

- **Store** (`internal/store`): v2 project with N events → exact
  `EventCount`, `SizeBytes` = file size, `Version` = "v2"; two projects with
  differing formats → `"mixed"`; empty store → zeros + active format;
  scoped reads sum to the store-wide read and keep `Version` store-wide; an
  unknown scope → zero counts with the version still resolved.
- **TUI**: `formatSize` table test (0, <1 MB, ≥1 MB, rounding);
  status-line render test asserting the stats cluster, key cluster, and
  `atm v…` appear and `STORE:`/`SELECTED:`/`theme:` do not; pane-hint tests
  updated for the removed fragments; a project-switch test pinning that the
  bar rescopes on select and matches each scope's true total. Note that
  selecting a project seeds its workflow vocabulary, which appends events —
  tests comparing scoped against store-wide totals must read both after
  navigation settles, not across it.
