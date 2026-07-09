# TUI Indexer Integration — Design Spec

**Status:** Draft (awaiting user review)
**Date:** 2026-07-09
**Depends on:** `2026-07-08-atm-memory-substrate-retrieval-design.md` (indexing model, `store.Watch`, embedding config, vector meta), `2026-07-02-tasks-management-v2-tui-mockups-design.md` (three-pane workspace + status line + overlay stack), `2026-07-08-persona-activity-projects-pane-overlay-design.md` (the actors-overlay modal pattern this spec generalizes into a plugin dock).
**Spawns:** none. The deferred default-actor decision is tracked separately as ATM-0072.

## Driver

Today the indexer is a foreground CLI only: `atm index --project <CODE>` runs `store.Watch` in the terminal that started it. While a human is consulting ATM in the TUI there is no way to see whether the index is fresh, start it, configure the embedding model, or watch its progress without dropping to a shell. The user asked to bring the indexer into the TUI: a status-bar indicator (off/on/running) and a key-bound overlay to configure the embedding model, kick the indexer off, and monitor its log. During brainstorm the scope grew to a small plugin architecture so the indexer is the first of future right-side status-bar plugins, and the actor segment was removed from the status bar (the deeper "what does the TUI stamp as actor" question is deferred to ATM-0072).

## Decisions (locked during brainstorming)

| # | Decision |
|---|----------|
| D1 | **Status tri-state: stopped / idle / working** (plus `off` when no embedding config and `error` when the last delta errored). Mapped to a dock segment `<icon> <state>`: `off`, `stopped`, `on` (idle), `running` (working), `error`. |
| D2 | **In-process TUI goroutine.** The TUI starts `store.Watch` as a goroutine for the selected project when the user presses Start. It dies when the TUI quits. Replaces `atm index` as the way to keep an index fresh while consulting ATM. The CLI `atm index` is unchanged. |
| D3 | **Full index management surface in the overlay.** Edit embedding config, start/stop watcher, reindex once, drop model, monitor log. No second overlay for config editing — editing is inline in the overlay's Config block. |
| D4 | **Embedding edit form = the existing `formField`/`Form` field logic rendered in place.** Prefill from current config; a `p` key fills the nomic-embed-text preset (localhost:11434/v1, dim=768, threshold=0.55, search_query:/search_document: prefixes). Saved via `store.SetEmbeddingConfig(code, cfg, m.actor)`. The TUI's actor stays `"default"` (deferred to ATM-0072). |
| D5 | **Status-bar indicator reflects the selected project only** (`m.projectScope`). No segment when no project is selected. |
| D6 | **Tea messages from the indexer goroutine (Approach A).** The goroutine is pure: `store.Watch(ctx, code, embedFn, progressFn)`. `progressFn` sends messages onto a buffered channel (cap 256, non-blocking drop-oldest on overflow). The root `Update` drains the channel on a periodic tick and mutates `indexerModel` — all model mutation happens in `Update`; no locking around TUI fields; the goroutine holds no `*tea.Program`, no TUI symbols. |
| D7 | **Plugin dock on the right of the status bar.** The `actor:` segment is removed. The right side becomes a row of compact plugin indicators, right-aligned where `actor:` used to be. Each plugin ships an icon glyph (single Unicode symbol, no emoji per the repo rule). Indexer icon: `⌬` (U+232C, "benzene") with a font-fallback of `#` if it breaks the column grid. |
| D8 | **`g` prefix keybinds.** `g` is a leader key: `g` then `1` opens the indexer overlay (plugin #1). Future plugins get `g2`, `g3`, etc. `g` alone does nothing (waits for the next key, vim-leader style). Only one plugin overlay open at a time; Esc closes it. Existing globals (`?`, `C`, `T`, `q`, `ctrl+c`, `1`/`2`/`3` pane focus) are unchanged — `g` was free at the root level. |
| D9 | **Hard reset on plugin failure.** A plugin's watcher can error (endpoint down, panic, ctx-cancel race). The indexer has `resetIndexer()`: cancel ctx, drain channel, block on a `done` channel until the goroutine returns, clear logs, `state→stopped`, `lastError→""`, keep config/status snapshots. Safe from any state. Triggered on: (a) the `S` stop key, (b) the framework's 3-strikes auto-reset (3 consecutive errors within 30s → reset + toast the error line so the user isn't stuck watching a tight retry loop), (c) project switch, (d) quit. `Start` after an error always `reset` then `start` — clean re-embed from the current log delta (`PendingIndex` recomputes from `meta.LastLogSeq` + `text_hash`; `WriteVectorBatch` is atomic per batch, so a partial prior batch is harmless). |
| D10 | **Save vs start/stop separated.** `[s]` (lowercase) saves embedding config to disk; it does not touch the watcher. `[S]` (uppercase) is a dumb start/stop toggle for the watcher using the current saved config. To pick up freshly-saved config while running: `S` (stop) then `S` (start) — two presses, no hidden smart-restart. The Status block surfaces when a restart is needed: if the watcher is running on config that has been saved-changed since it started, show a dim hint line `Status: <icon> running (config changed — S to restart)` using a `watcherStartedAt` vs `config.UpdatedAt` timestamp comparison; the hint clears once restarted. |
| D11 | **Actor segment removed from the TUI status bar.** The `m.actor` field stays on `Model` (still stamps `"default"` on mutations pending ATM-0072); we just stop rendering it. The "Activity by persona" overlay keeps working unchanged — it reads `log.actor` + `actor.Resolve`, not the TUI field. |

## Section 1: Architecture

### New type: `plugin` interface (`internal/tui/plugin.go`)

A small, real plugin registry — not a speculative framework, just enough that "the indexer is the first plugin" is a true statement and the next one is one struct away.

```go
type plugin interface {
    ID() string                       // "indexer"
    Icon() string                     // "⌬"
    OverlayKey() string               // "1" -> opened via g 1
    DockLabel(state any) string       // "<icon> running" (already-resolved state)
    DockColor(state any, s Styles) lipgloss.Style
    State(m *Model) any               // pull current state into a snapshot
    Open(m *Model)                    // enter overlay (refresh state, etc.)
    Close(m *Model)                   // exit overlay (does not stop the watcher)
    Reset(m *Model)                   // hard reset on failure / project switch
    HandleKey(k tea.KeyMsg, m *Model) tea.Cmd  // dispatched when its overlay is open
    Render(m *Model) string           // overlay body
}
```

### Plugin registry + overlay routing (in `app.go`)

- `Model.plugins []plugin` in registration order; `Model.pluginOverlay int` (-1 when none).
- `Model.pluginPrefixActive bool` set by `g`, cleared on the next key.
- `handleKey`: if `pluginOverlay != -1`, route to that plugin's `HandleKey`; else if `k == "g"`, set `pluginPrefixActive = true`; else if `pluginPrefixActive` and `k` matches a plugin's `OverlayKey`, open it; else clear the prefix flag. Existing overlay precedence (help → confirm → form → actors) runs before the plugin check — plugins are a parallel overlay kind, not a rewrite of `actorsOverlay`.
- `renderStatusLine` iterates `m.plugins`, calls `State(m)` + `DockLabel` + `DockColor`, joins with a separator, right-aligns the result. The left side (`STORE:` / `SELECTED:` / `theme:` / keymenu hint / toast) is unchanged. The `actor:` segment is removed.
- `View()`: if `m.pluginOverlay != -1`, `placeOverlay(out, m.plugins[m.pluginOverlay].Render(m))` after the existing overlay layers.
- `SetSize`: when a plugin overlay is open, size the plugin's render area to the same `helpBoxSize` (~80% of workspace) the help/actors overlays use.

### Plugin supervisor (`internal/tui/plugin.go`)

A small helper that wraps `Reset` calls with a debounce window so the 3-strikes rule lives in the framework, not in each plugin. The store-side `Watch` exponential backoff (per-delta) stays; the framework's reset is for "this plugin is clearly broken, give the user a clean slate." Tracks per-plugin error timestamps; when 3 errors land within 30s it calls `plugin.Reset(m)` and toasts the last error line.

### Indexer plugin (`internal/tui/indexer.go`)

Implements `plugin`. Owns the `indexerModel`:

```go
type indexerState int
const (
    idxOff     indexerState = iota // no embedding config
    idxStopped                      // config present, watcher not started
    idxIdle                         // watcher running, caught up
    idxWorking                      // watcher running, embedding in delta
    idxError                        // last delta errored; watcher halted
)

type indexerModel struct {
    m *Model
    state      indexerState
    lastError  string
    logs       []string          // bounded ring, cap 1000, drop-oldest
    logOffset  int               // user scroll; -1 = tail
    cfg        *store.EmbeddingConfig  // snapshot from GetProjectConfig
    status     []indexStatusRow       // ListVectorModels + VectorMeta + LastLogSeq
    cancel     context.CancelFunc     // nil when not running
    done       chan struct{}           // closed when goroutine returns
    startedAt  time.Time               // for config-changed hint
    editMode   bool
    editFields []formField             // reuse form.go's formField
    editCursor int
    msgCh      chan indexerMsg         // cap 256
}

type indexerMsg struct {
    kind  indexerMsgKind  // progress | state | error | done
    line  string          // progress text
    state indexerState     // state transition (state msg)
    err   string           // error text (error msg)
}
```

The watcher goroutine runs `store.Watch(ctx, code, embedFn, progressFn)`:

- `embedFn` = `embed.New(*cfg.Embedding).Embed` (exactly like `cli/index.go`). Built at start from the current saved config snapshot.
- `progressFn` = non-blocking send onto `msgCh` (drop-oldest on overflow; never block the indexer).
- The goroutine sends `state{idxWorking}` before a delta, `state{idxIdle}` when a delta reports `Indexed==0` and the log is caught up, `error{err}` when `Watch` returns a non-ctx error, `done{}` when it returns.
- The root `Update` returns a `tea.Tick` (every ~120ms while the overlay is open OR the goroutine is alive) whose command drains `msgCh` into `indexerModel.logs` and applies state transitions. All `indexerModel` mutation happens in `Update`.

Lifecycle hooks on the root model:

- `startIndexer(code)`: if `code == ""` or no embedding config → toast + return. Build `embedFn`, create ctx (`cancel` + `done`), spin goroutine. Set `state→idxWorking` (initial pass), `startedAt = Now()`. The goroutine's first `ReindexOnce` flips to `idxIdle` or reports `idxError`.
- `stopIndexer()`: cancel ctx; block on `done`; drain `msgCh`; set `state→idxStopped`, `cancel = nil`.
- `resetIndexer()`: `stopIndexer()` + clear `logs`, `state→idxStopped`, `lastError→""`, keep `cfg`/`status` snapshots. Called by the supervisor on 3-strikes, by project switch, by quit, and by `S` when stopping.
- `refreshIndexerStatus()`: re-read `GetProjectConfig`, `ListVectorModels`, `VectorMeta`, `LastLogSeq`. Called on project selection, on overlay `Open`, after `s` save, after `r` reindex, after `d` drop, and on the periodic tick while the overlay is open.

### Key invariant preserved

The storage/search/index engine never calls a model; only `embed.New` does, and it's injected into `store.Watch` exactly as the CLI does. No new model-touching boundary; the substrate spec's R3 holds.

## Section 2: Status bar

Right-aligned plugin dock. When `m.projectScope == ""` the dock is empty. Otherwise, for the indexer plugin:

| `indexerModel.state` | dock segment | meaning |
|---|---|---|
| `idxOff` | `<icon> off` | no `embedding` block in config.json |
| `idxStopped` | `<icon> stopped` | config present, watcher not started |
| `idxIdle` | `<icon> on` | watcher running, caught up |
| `idxWorking` | `<icon> running` | watcher running, embedding in progress |
| `idxError` | `<icon> error` | watcher errored on the last delta and is halted |

Color via existing + one new style: `off`/`stopped` dim (`Status`), `on` green-leaning (`StatusOK` — new field in `theme.go` mirroring `StatusLabel`'s derivation), `running` bold accent (`StatusLabel`), `error` `Warning`. If `Styles` lacks a green style, add `StatusOK` as one field; no other style changes.

The `actor:` segment is removed from `renderStatusLine` (D11). The `m.actor` field stays on `Model` for now (still stamps `"default"` on mutations pending ATM-0072).

## Section 3: Plugin keybinds

`g` is a leader key at the root level (no overlay/form/confirm/actors active). `g` sets `pluginPrefixActive`; the next key either matches a plugin's `OverlayKey` (opens that plugin's overlay) or clears the flag. Indexer `OverlayKey()` returns `"1"`, so `g 1` opens the indexer overlay.

Inside the indexer overlay, no prefix — single-letter keys route to the plugin's `HandleKey`:

**View mode:**
- `e` → toggle edit mode on the Config block.
- `s` → save (write `SetEmbeddingConfig`); does not touch the watcher. Exits edit mode → view.
- `S` → start/stop toggle: `idxStopped` → `startIndexer(code)`; `idxIdle`/`idxWorking` → `resetIndexer()`. Disabled (toast) in `idxOff`.
- `r` → reindex once: run `ReindexOnce` synchronously on a `tea.Cmd` (one delta pass, no watch), stream progress into the log pane. Disabled in edit mode, in `idxOff`, or while watcher running (toast "stop the watcher first" or "use S to start").
- `d` → drop model: confirm overlay → `DropVectors(code, model)`. Disabled in edit mode or when no index file.
- `Esc` → close overlay (watcher keeps running in background; the dock icon keeps reflecting state).
- `j`/`k`, `PgUp`/`PgDn`, `G` → scroll the log pane (`G` jumps to tail).
- `T` → cycle theme (existing global, works inside).

**Edit mode** (toggled by `e`):
- The Config block fields become editable in place (reuse `formField` + the `Form.Update` key handling, rendered inline in the Config region). Status/Index/log stay visible.
- `Tab` / arrows → next/prev field.
- `p` → nomic preset: fill `model=nomic-embed-text`, `endpoint=http://localhost:11434/v1`, `dim=768`, `threshold=0.55`, `query_prefix=search_query: `, `doc_prefix=search_document: ` at once.
- `s` → save: validate required (model, endpoint), write `SetEmbeddingConfig(code, cfg, m.actor)`, exit edit mode → view, `refreshIndexerStatus()`. Does not restart the watcher.
- `Esc` → cancel edit (revert to view, no write, no restart).
- `r` and `d` are disabled in edit mode.

**Config-changed hint:** in view mode, when the watcher is running (`idxIdle`/`idxWorking`) and `watcherStartedAt < cfg.UpdatedAt`, render a dim hint line under the Status block: `<icon> running (config changed — S to restart)`. Clears once `S` restarts (`startedAt` resets).

## Section 4: Indexer overlay layout

Opened by `g 1`. Refuses to open with a toast "select a project first" when `m.projectScope == ""`. Centered modal sized like `helpBoxSize` (~80% of workspace). Title: `Indexer — <CODE>`. Four regions, top to bottom:

```
┌ Indexer — ATM ─────────────────────────────────────────────────┐
│                                                                 │
│  Embedding model:   nomic-embed-text                            │
│  Endpoint:          http://localhost:11434/v1                    │
│  Dim / threshold:   768 / 0.55                                  │
│  Prefixes:          search_query: / search_document:             │
│                                                                 │
│  Status:    ⌬ running                                           │
│  Index:     nomic-embed-text  count=42  last_log_seq=1287       │
│             behind=0   (log at 1287)                            │
│                                                                 │
│  [e] edit config   [s] save   [S] start/stop   [r] reindex once  │
│  [d] drop model   [Esc] close                                   │
│                                                                 │
│  ── log ────────────────────────────────────────────────────── │
│  embedding 3/42 ATM-0042 (task)                                  │
│  embedding 4/42 ATM-0043 (task)                                  │
│  ...                                                             │
│  indexed 42 (model=nomic-embed-text); index at log_seq 1287     │
│  ────────────────────────────────────────────────────────────  │
│  [Esc] close                                                     │
└─────────────────────────────────────────────────────────────────┘
```

1. **Config block** — read-only snapshot from `GetProjectConfig(code)`. If absent → `Embedding model: (none — press [e] to configure)` and `s`/`S`/`r`/`d` are disabled (toast on press). In edit mode, the fields become editable in place (labels + value + trailing underline cursor, mirroring `Form.View`).
2. **Status block** — `⌬ <state>` + the index summary from `ListVectorModels` + `VectorMeta` + `LastLogSeq` (one model row; if multiple model files exist for migration, list each on a line with its behind-count — each is a `d` drop target). Plus the config-changed hint line when applicable. Refreshed on `Open`, after any verb, and on the periodic tick.
3. **Action row** — `[e] edit config   [s] save   [S] start/stop   [r] reindex once   [d] drop model   [Esc] close`. In edit mode, replaced by `[Tab] next field   [s] save   [p] nomic preset   [Esc] cancel`.
4. **Log pane** — bottom ~40% of the modal. Bounded ring (cap 1000 lines, drop-oldest). `logOffset` is the index of the topmost visible log line; `-1` means tail (auto-follow: new lines shift the view so the newest line stays visible). `j`/PgDn move the offset toward the tail (and `-1` sticks once reached); `k`/PgUp move toward the head and pin the offset (disabling auto-follow until the user presses `G`, which resets to `-1`). Empty state: `(no log lines yet)`. Same ring the dock's `running` state is derived from.

Lifecycle:
- `Open`: refresh config + status; if the watcher is already running, show the live log (the goroutine runs while the project is selected, not while the overlay is open). Start the periodic tick (~120ms) to drain the channel + re-snapshot status while the overlay is open.
- `Close` (Esc): stop the tick; **do not stop the watcher** — it keeps running in the background; the dock icon keeps reflecting state. Only `S` / project-switch / quit stops it.
- `Reset` (framework 3-strikes, or project switch while overlay open): clear the log ring, set `stopped`, toast the error line. Overlay stays open so the user sees the error.

## Section 5: Data flow

### Start watcher (`S` from stopped)

```
S (stopped) -> startIndexer(code):
  cfg := GetProjectConfig(code).Embedding; if nil -> toast "no embedding configured; press e"
  client := embed.New(*cfg)
  embedFn := func(text, role string) ([]float64, error) { return client.Embed(text, role) }
  ctx, cancel := context.WithCancel(context.Background())
  done := make(chan struct{})
  m.indexer.cancel = cancel; m.indexer.done = done; m.indexer.startedAt = Now()
  m.indexer.state = idxWorking
  go func() {
    err := store.Watch(ctx, code, embedFn, progressFn)
    if err != nil && !errors.Is(err, context.Canceled) { send error{err} }
    send done{}
  }()
```

### Stop watcher (`S` from running, or reset)

```
S (running) -> resetIndexer():
  cancel()
  <-done                 // block until goroutine returns; no leak
  drain msgCh
  m.indexer.state = idxStopped; m.indexer.cancel = nil; m.indexer.lastError = ""
  // logs cleared, cfg/status snapshots kept
```

### Drain loop (root `Update` tick)

```
tick (every 120ms while overlay open OR goroutine alive):
  for non-blocking recv from m.indexer.msgCh:
    case progress{line}: append to logs (drop-oldest if cap)
    case state{s}:       m.indexer.state = s
    case error{err}:     m.indexer.lastError = err; m.indexer.state = idxError
                         supervisor.recordError(plugin) -> maybe Reset
    case done{}:         if state != idxError: m.indexer.state = idxStopped
```

### Save config (`s`)

```
s (edit mode) -> validate required (model, endpoint)
  -> store.SetEmbeddingConfig(code, cfg, m.actor)
  -> exit edit mode; refreshIndexerStatus()
  // does not restart the watcher; Status block shows the
  // config-changed hint if the watcher is running on stale config
```

### Reindex once (`r`)

```
r (view mode, watcher stopped) -> run ReindexOnce on a tea.Cmd:
  cfg := GetProjectConfig(code).Embedding; if nil -> toast
  client := embed.New(*cfg); embedFn := client.Embed
  progress := func(msg string) { send progress{msg} }
  res, err := store.ReindexOnce(code, embedFn, progress)
  if err: toast + log "index error: <err>"
  else:   log "indexed N (model=M); index at log_seq S"; refreshIndexerStatus()
```

### Drop model (`d`)

```
d (view mode, index file present) -> confirm overlay
  -> store.DropVectors(code, model)
  -> toast "dropped vector index <code>/<model>"; refreshIndexerStatus()
```

## Section 6: Error handling

- No embedding config for the selected project → dock shows `<icon> off`; `S`/`r`/`d` toast "no embedding configured; press `e`"; `e` opens edit mode with empty fields (or `p` for nomic preset).
- Endpoint unreachable during a delta → the store-side `Watch` exponential backoff retries per-delta (unchanged); `progressFn` logs `index error: <err>`; after 3 errors within 30s the framework `resetIndexer()` and toasts the last error line; `state→idxError` so the dock shows `<icon> error`. The user can `S` to restart cleanly.
- ctx-cancel race on stop → goroutine returns `context.Canceled`; `done` closes; `state→idxStopped` (not `idxError`).
- `ReindexOnce` write-batch rejected (model/dim mismatch) → the store returns `ErrUsage`; `r` surfaces it as a toast + log line; the overlay stays usable.
- `DropVectors` on a missing model file → `ErrNotFound`; `d` surfaces it as a toast; refresh.
- Goroutine leak on stop/reset → prevented by blocking on `done` before zeroing state; no stale writes into a reused `indexerModel`.
- Project switch while overlay open → `resetIndexer()` (stops goroutine for the old project), then the overlay refreshes against the new project's config/status (may show `<icon> off` if the new project has no embedding configured).
- Plugin panic → recover in the goroutine wrapper, send `error{recovered err}`, let the supervisor reset.

## Section 7: Testing

Same layered structure as the existing TUI tests (`app_test.go`, `actors_test.go`, etc.). No real endpoint is contacted — tests inject a fake `EmbedFunc` via a test seam (see below).

| Area | Invariants |
|---|---|
| `plugin_test.go` | `g` prefix sets flag; `g 1` opens indexer overlay; non-matching key clears flag; only one plugin overlay open; `Esc` closes; `T`/`?`/`C` globals still layer on top. |
| `indexer_test.go` | Dock label/color per state; `State()` snapshot reflects model; `Open`/`Close`/`Reset` lifecycle; `Reset` blocks on `done` (no leak). |
| `indexer_runtime_test.go` | Start watcher with a fake embedder (test seam: `indexerModel.embedFnBUILDER func(*store.EmbeddingConfig) store.EmbedFunc` overridable in tests) → `idxWorking` → `idxIdle` on caught-up; Stop → `idxStopped` + `done` closed; drain loop advances logs on tick; 3 errors in 30s → reset + toast. |
| `indexer_overlay_test.go` | Refuses to open with no project (toast); config block renders config / "(none)"; `e` toggles edit mode; `p` fills nomic preset; `s` writes `SetEmbeddingConfig` + exits edit; `S` toggles runtime; `r` runs `ReindexOnce` once (fake embedder); `d` confirm → `DropVectors`; log ring cap + drop-oldest; scroll keys. |
| `status_line_test.go` | Actor segment absent; plugin dock right-aligned; `<icon> <state>` per state; empty when no project. |
| `theme_test.go` | `StatusOK` style present; green-leaning. |
| `app_test.go` | Existing overlay precedence (help → confirm → form → actors) unaffected; plugin overlay slots in after actors in `View()`; project switch calls `resetIndexer`. |

**Test seam for the embedder:** `indexerModel` holds an `embedFnBuilder func(*store.EmbeddingConfig) store.EmbedFunc` field, defaulted to the real `embed.New(*cfg).Embed` closure in `NewModel`. Tests override it with a fake embedder (returns deterministic vectors) so no HTTP is contacted. This mirrors the substrate spec's `EmbedFunc` injection pattern and keeps the engine model-free.

**Verification gate:** `make verify` (`make build && make test`) unchanged.

## Section 8: Files touched (additive)

- `internal/tui/plugin.go` (new) — `plugin` interface, registry helpers, `pluginSupervisor` (3-strikes debounce).
- `internal/tui/indexer.go` (new) — `indexerModel`, `indexerMsg`, lifecycle (`startIndexer`/`stopIndexer`/`resetIndexer`/`refreshIndexerStatus`), overlay render + `HandleKey`, edit mode, nomic preset.
- `internal/tui/app.go` (modify) — `Model.plugins`/`pluginOverlay`/`pluginPrefixActive`; `handleKey` `g` prefix routing; `View()` plugin overlay layer; `renderStatusLine` plugin dock + actor removal; `SetSize` plugin sizing; project-switch `resetIndexer`; quit `resetIndexer`.
- `internal/tui/theme.go` (modify) — add `StatusOK` style (green-leaning), mirroring `StatusLabel`'s derivation.
- `internal/tui/keymap.go` (modify) — add `g` leader + plugin overlay keys to the keymap reference table (help Section 2).
- `internal/tui/help.go` (modify) — mention `g <n>` plugin overlay keys in the keys help.
- `internal/tui/*_test.go` (new + modify) — per Section 7.
- No store, CLI, or embed changes. `atm index` CLI is unchanged.

## Section 9: Rollout (layered commits, each green)

1. `theme.go` `StatusOK` + test.
2. `plugin.go` interface + registry + supervisor (no plugins registered yet) + tests.
3. `app.go` plugin dock in status line (empty dock; actor segment removed) + `g` prefix routing (no-op until a plugin registers) + tests.
4. `indexer.go` model + lifecycle + drain loop + overlay render (read-only: config block + status block + log pane; no verbs yet) + tests with fake embedder.
5. `indexer.go` `S` start/stop + `Reset` on project switch/quit + tests.
6. `indexer.go` `e`/`s` edit mode + nomic preset + `SetEmbeddingConfig` save + tests.
7. `indexer.go` `r` reindex once + `d` drop model + confirm + tests.
8. `indexer.go` 3-strikes supervisor integration + error-state dock + tests.
9. `keymap.go`/`help.go` keys help update + tests.

**Compatibility:** fully additive on the TUI surface. No store/CLI/embed changes. The `actor:` segment removal is the only user-visible status-bar change besides the new dock. The `m.actor` field is untouched (ATM-0072).

## Out of scope (this spec)

- **Changing what actor the TUI stamps on mutations** (ATM-0072). This spec only hides the segment.
- **CLI `atm index` changes.** The CLI watcher is unchanged; the TUI runs its own in-process watcher. They share `store.Watch`.
- **TUI search UI.** Still CLI-only (`atm search`), per the substrate spec's out-of-scope.
- **Auto-starting the watcher on TUI launch.** The user starts it explicitly with `S`. (A future "auto-start on project select" is a one-line follow-up if desired.)
- **Multiple concurrent per-project watchers.** One watcher per selected project; switching projects resets and (if the user re-presses `S`) starts the new project's watcher.
- **Folding the actors overlay into the plugin registry.** It stays a pane-level drill-down (`P` in Projects). A later refactor can register it as a plugin if desired.