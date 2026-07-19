# Boards management v2 — design

Date: 2026-07-19
Status: approved design (brainstormed 2026-07-19; tracking task ATM-87b3ae)
Builds on: `2026-07-18-capability-namespace-manager-actions-v2-design.md` (capability namespace, `EnsureVocabulary` returns owned boards, `Registry.For` enable/disable gate).

## Problem

After the capability-namespace v2 work, capabilities are the only seeding path and `EnsureVocabulary` returns the boards a capability owns. Per-project capability enable/disable (`Registry.For`, `atm project capability add/remove`) gates which capability commands mount and which vocabularies get seeded. But the TUI Boards pane still derives its ring the *old* way — `buildBoardRows` (`internal/tui/labels.go:562`) walks `LabelList` and:

1. turns every label with an `Expr` into a board row, and
2. derives a namespace row (`<ns>:*`) from *every* prefix seen on any stored label, whether or not any capability owns that namespace.

Three concerns fall out:

- **Emergent namespace boards leak past the capability gate.** Disable `workflow` and its four boards vanish from the seeded set, but if any task still carries `ATM:status:open`, the `status:*` namespace row reappears — derived from the stored label, not from any capability. Ad-hoc prefixes like `type:*`, `repo:*`, `doc:*` show up with no owner and no way to silence them short of removing the labels. The Boards pane drifts cluttered with namespace rows the user never asked for.
- **No ring ordering.** `buildBoardRows` sorts alphabetically (`labels.go:641`); `selectDefault` hard-codes `all-tasks` else first (`labels.go:258`). The user has no way to put `open-tasks` ahead of `backlog`, or to keep `status:*` next to `priority:*`.
- **No way to hide a board without disabling its capability.** Today's only visibility toggle is per-capability enable/disable, which is too coarse: hiding `priority:*` means disabling `workflow`, which also drops `all-tasks`, `open-tasks`, `in-progress-tasks`, `backlog`, and `status:*`.

The capability-authored model is in place; the TUI's display layer hasn't caught up.

## Goal

- The Boards pane ring is exactly what the enabled capabilities expose (boards + namespace descriptors), plus **one synthetic "unmanaged" umbrella** that groups every label no capability owns. The long tail collapses into one entry instead of N namespace rows.
- Each ring row carries its **owner** (`Capability.Name()`), surfaced in the TUI as a muted tag.
- Per-project **display preferences** — ring order, hidden boards, and pin slots — live in one place: `projects/<CODE>/config.json` under a new `boards` key. `pins.json` is migrated into it.
- A read-only CLI verb `atm capability unmanaged --project <CODE>` exposes the unmanaged set to the manager agent, which uses existing substrate verbs (`atm task label add/remove`, `atm project boards hide`) to triage and redistribute.
- The capability contract stays minimal: capability authors choose what to expose; the substrate stores nothing about "boards vs namespaces" — that distinction is encoded in label shape (`Expr` set vs `:*` name).

Out of scope: changing what `EnsureVocabulary` seeds; changing the `Registry.For` enable/disable gate; pin-slot jump feature (Shift-1..3) semantics; new task/label substrate events for display preferences.

## Design

### 1. Capability contract — `Exposed`

The `Capability` interface (`internal/capability/capability.go:46`) gains one method:

```go
// Exposed declares the computed labels (boards + namespace descriptors)
// this capability surfaces in the TUI ring for the project. Pure read,
// no store side effect. Order within the slice is the capability's
// preferred ring order; the registry preserves registration order across
// capabilities. Returns the same set as EnsureVocabulary's board return
// plus any namespace descriptors (:* labels with Expr="") the capability
// owns and wants surfaced.
Exposed(code string) []core.Label
```

Two implementation changes:

- **`workflow.Exposed(code)`** returns 6 labels in this order: `all-tasks`, `open-tasks`, `in-progress-tasks`, `backlog` (Expr set — the four boards) + `status:*`, `priority:*` (Expr empty, namespace descriptors). Today `workflow.EnsureVocabulary` returns only the four boards; `Exposed` widens the surfaced set to include the two namespaces workflow owns.
- **`contextmap.Exposed(code)`** returns 1 label: `context-current` (Expr set). Unchanged from today's `EnsureVocabulary` return.

`EnsureVocabulary` is unchanged. It stays the seed-time verb (mutates the store, returns its own slice for callers that want post-seed state); `Exposed` is the display-time read (pure, returns what the capability surfaces in the ring). They return overlapping but not identical sets — `Exposed` includes namespace descriptors; `EnsureVocabulary` returns only Expr-boards. Both are derived from the same capability-owned vocabulary list; divergence is a bug.

The capability doctrine holds: a capability author chooses what to expose. Exposing a namespace is opt-in, not a substrate rule — workflow exposes `status:*` and `priority:*`; contextmap doesn't expose `context:*`. Future capabilities decide for themselves.

### 2. Registry surface — `Unmanaged`

`Registry` gains one derived-view helper:

```go
// Unmanaged returns labels in the project's LabelList that no enabled
// capability declares via Exposed. Derived, not stored. The TUI renders
// these under the synthetic umbrella row; the CLI verb exposes the same
// set to the manager agent for triage.
func (r *Registry) Unmanaged(svc core.LabelService, code string) ([]core.Label, error)
```

Implementation: `svc.LabelList(code, "")` minus the union of `c.Exposed(code)` for each `c` in `r` (the registry already filters by enabled set at the call site — `reg.For(project).Unmanaged(...)`). Both boards (Expr != "") and namespace descriptors (`:*`) are subtracted by FullName. Leftover `:*` descriptors and any label whose prefix isn't owned land in the unmanaged set.

The umbrella is **not** in this return — it's a TUI/CLI sentinel derived from the *presence* of unmanaged labels (non-empty return → render the umbrella row). The `Unmanaged` helper returns the constituent labels; the caller decides how to present them.

### 3. Per-project config — `BoardsConfig`

`core.ProjectConfig` (`internal/core/config.go:14`) gains a `Boards` field:

```go
type BoardsConfig struct {
    Order  []string `json:"order,omitempty"`   // ring order override (FullName list)
    Hidden []string `json:"hidden,omitempty"`  // hidden FullNames
    Pins   []string `json:"pins,omitempty"`    // pin-slot FullNames (max 3)
}

type ProjectConfig struct {
    UpdatedAt string             `json:"updated_at,omitempty"`
    UpdatedBy string             `json:"updated_by,omitempty"`
    Embedding *EmbeddingConfig   `json:"embedding,omitempty"`
    Remotes   map[string]string  `json:"remotes,omitempty"`
    Boards    *BoardsConfig      `json:"boards,omitempty"`
}
```

`config.json` shape:
```json
{
  "embedding": {...},
  "remotes": {...},
  "boards": {
    "order":  ["ATM:all-tasks", "ATM:open-tasks", "ATM:status:*", "ATM:priority:*", "ATM:unmanaged"],
    "hidden": ["ATM:context-current"],
    "pins":   ["ATM:all-tasks", "ATM:open-tasks"]
  }
}
```

- **`order`** is a partial override. Any exposed label not in `order` falls back to capability-registration order at the end. The umbrella (`ATM:unmanaged`) defaults to the last position. Entries in `order` that no capability exposes and that aren't the umbrella are ignored silently (defensive against typos and against stale entries after a capability is disabled).
- **`hidden`** takes precedence over `order` — a hidden board never appears in the ring, never appears in `order` validation, and never appears as a pin candidate. Hidden persists across capability re-enable: hide `status:*`, disable+re-enable `workflow`, `status:*` stays hidden. Display preference, not state.
- **`pins`** replaces `pins.json` verbatim. `core.Pins` and `internal/store/pins.go` are removed; `WritePins`/`GetPins` migrate to read/write `config.json.boards.pins`. max-3 cap stays; the cap is enforced at write time in the new config-write path.

**Migration.** On first read after upgrade: if `config.json.boards` is nil but `pins.json` exists, load `pins.json` into the in-memory `BoardsConfig.Pins` and persist on the next config write. No event log entry — display preferences aren't substrate state. `pins.json` is left in place until overwritten by a config write; a one-shot `atm store rebuild` (or any config-touching verb) absorbs it. After migration, `pins.json` is ignored.

### 4. TUI Boards pane

`buildBoardRows` (`internal/tui/labels.go:562`) is rewritten. The new population:

1. **Managed set.** `reg.For(project)` → loop capabilities, call `c.Exposed(code)`, collect `(Label, ownerName=cap.Name())`. One row per returned label. `boardRow` gains an `Owner string` field.
2. **Umbrella.** `reg.Unmanaged(store, code)` → if the return is non-empty, add one synthetic row `{FullName: code+":unmanaged", Name: "unmanaged", Expandable: true, Owner: ""}`. The umbrella is omitted entirely when the unmanaged set is empty (no long tail → no row).
3. **Hidden filter.** Drop rows whose `FullName` is in `config.boards.hidden`.
4. **Order.** Stable-sort by `config.boards.order` index; rows not in `order` keep their original (capability-registration, then umbrella-last) relative order and append.
5. **Render.** Each row gains an owner tag column (muted): `all-tasks  workflow`, `status  workflow`, `unmanaged  —`. The umbrella drills into the current namespace-chart view (the old `buildBoardRows` emergent derivation, scoped to unmanaged labels only — same chart/detail rendering, same drill semantics).

`selectDefault` (`labels.go:253`) keeps today's logic: `all-tasks` if present in the ring, else first ring member. The umbrella is never the default — `selectDefault` skips it.

The owner tag uses an existing muted style (`m.styles.Muted`); no new theme color. The owner column is narrow (10 chars, truncated with `lipgloss.Width`-aware truncation as `boardTableLine` already does for the name column).

### 5. CLI surface

Four new verbs, all under existing namespaces:

- **`atm capability unmanaged --project <CODE>`** — read-only. Lists every unmanaged label with name + description + usage count. JSON envelope: `{project, labels: [{name, description, usage}]}`. This is the manager agent's read path. Implemented as a thin wrapper over `Registry.Unmanaged`.
- **`atm project boards reorder --project <CODE> --name <FULL> [--before <FULL> | --after <FULL> | --first | --last]`** — writes `config.json.boards.order`. One board per invocation (the typical case); `--first`/`--last` shortcuts.
- **`atm project boards hide --project <CODE> --name <FULL>`** — writes `config.json.boards.hidden` (add).
- **`atm project boards show --project <CODE> --name <FULL>`** — writes `config.json.boards.hidden` (remove).

`atm project pins` shape is unchanged (still a list/add/remove verb trio) but the read/write target moves to `config.json.boards.pins`. The old `pins.json` path is no longer consulted after migration.

All verbs require `--actor` (mutating) or default it (read-only), matching existing `atm project` verbs. They write via the existing `core.ProjectConfig` read-modify-write path — no new store event, no event-log entry. The `updated_at`/`updated_by` stamps on `ProjectConfig` are refreshed on every write.

### 6. Manager prompt / autopilot

The autopilot procedure (carried in the conventions / a capability guide section) gains a triage step. No manager-specific code — the verb is the contract:

> Run `atm capability unmanaged --project <CODE>`. For each unmanaged prefix, decide whether its tasks should carry a capability-owned label instead (e.g. replace `type:bug` with `status:open` + `priority:high` if the task is genuinely triageable onto the workflow paved road). Use `atm task label remove --label <CODE>:<unmanaged>` and `atm task label add --label <CODE>:<owned>` to redistribute. Hide prefixes you deliberately don't want to see with `atm project boards hide --name <CODE>:<ns>:*`. Re-run `atm capability unmanaged` to verify.

The manager prompt's `<CAPABILITY_ROLES>` enumeration is unchanged — `unmanaged` is a CLI verb on the substrate surface (`atm capability unmanaged`), not a capability. The manager reads it the same way it reads `atm label list` or `atm task list`: as a substrate query.

### 7. Architecture / layering

- `internal/core` — `BoardsConfig` type; `ProjectConfig` gains the `Boards` field. `core.Pins` is removed. `core.Service` loses `GetPins`/`WritePins` and gains `SetProjectBoards(code string, b *BoardsConfig, actor string) error` (a per-field write helper mirroring the existing `SetEmbeddingConfig` shape in `internal/store/config.go:23`). Read is via the existing `GetProjectConfig` (`core/service.go:38`).
- `internal/store` — `pins.go` is removed. `internal/store/config.go` gains `SetProjectBoards(code, b, actor)` following the `SetEmbeddingConfig` pattern (read-modify-write under the project lock). No new store event.
- `internal/capability` — `Capability.Exposed`; `Registry.Unmanaged`. Tests cover the union/diff correctness and the per-capability `Exposed` returns.
- `internal/capability/workflow` — `Exposed(code)` returns the 6 labels.
- `internal/capability/contextmap` — `Exposed(code)` returns `context-current`.
- `internal/cli` — `atm capability unmanaged`; `atm project boards reorder/hide/show`; `atm project pins` retargeted to config.
- `internal/tui` — `buildBoardRows` rewrite; owner column; umbrella drill-in; `loadPins`/`persistPins` retargeted to `ProjectConfig.Boards.Pins`.

No new packages. The capability doctrine holds: surfaced concerns live in capabilities; the substrate stores labels and config; the TUI renders.

### 8. Testing

- **`internal/capability`**: `Registry.Unmanaged` correctness — `LabelList` minus union of `Exposed` returns exactly the unmanaged set, for several configurations (no unmanaged, only `:*` descriptors unmanaged, only stored labels unmanaged, mixed). `Exposed` per capability returns the documented set (workflow 6, contextmap 1). The diff is stable across repeated calls (no store mutation).
- **`internal/core/config`**: `BoardsConfig` round-trip JSON; `Order`/`Hidden`/`Pins` field independence; migration of legacy `pins.json` into `Boards.Pins` on first read; max-3 cap enforced on write.
- **`internal/tui/labels`**: ring population uses `Exposed` + umbrella, not emergent derivation; owner tag rendering for managed rows and `—` for the umbrella; `order` application (partial override, unmatched fall back to registration order, umbrella default last); `hidden` filter (hidden board never in ring, never in pin candidates); umbrella drill-in shows the old chart view scoped to unmanaged labels; `selectDefault` skips the umbrella.
- **`internal/cli`**: new verb goldens (`atm capability unmanaged` JSON + text, `atm project boards reorder/hide/show` round-trip, `atm project pins` reads/writes config). The `--name` flag accepts both board FullNames (`ATM:open-tasks`) and namespace FullNames (`ATM:status:*`).
- **Existing tests**: every test that seeded ad-hoc namespaces via task labels (e.g. `type:bug`) and expected them in the L0 ring updates — they now appear only under the umbrella, drilled in. Tests that asserted the alphabetical sort of L0 update to assert capability-registration order + `order` override. Pin tests migrate from `pins.json` fixtures to `config.json.boards.pins` fixtures.

### 9. Migration / compatibility

- **`pins.json` → `config.json.boards.pins`.** First read after upgrade: load `pins.json` if `config.json.boards` is nil; persist on next config write. `pins.json` is left in place until overwritten; `atm store rebuild` absorbs it in one shot if desired. No event log entry.
- **Existing projects with no `boards` config.** `config.boards` is nil → default order (capability-registration, umbrella last), no hidden, no pins. Behavior matches today's TUI for projects with no pins, except the ring population changes (capability-authored + umbrella instead of emergent).
- **Disabled capability with hidden boards.** `config.boards.hidden` may list FullNames a disabled capability would have exposed. They're hidden regardless — no error, no warning. When the capability is re-enabled, the entries take effect again.
- **`order` entries for non-existent boards.** Silently ignored. Defensive against typos and against stale entries after a capability is disabled or a board renamed.
- **Binary upgrade.** No new store event, no schema change, no event-source upgrade. The change is in config.json shape + TUI/CLI behavior. A user rolling back to an older binary would lose the `boards` config (older binary ignores the `boards` key) but keep `pins.json` if it was never absorbed. Safe.

## Open questions (resolved in brainstorm)

1. **Umbrella FullName** — `ATM:unmanaged` (TUI/CLI sentinel, never a real label). The TUI and CLI never call `LabelAdd("ATM:unmanaged", ...)`; the name is only used as a row identifier and an `order`/`hidden` key. A user who hand-creates a real `unmanaged` namespace (`ATM:unmanaged:*`) would shadow the sentinel — `buildBoardRows` must guard against this: if a real label named `ATM:unmanaged:*` exists, the umbrella row is suppressed and the real label renders as a normal unmanaged row (the sentinel loses). The guard is one check in `buildBoardRows`.
2. **Hidden persists across capability re-enable.** Confirmed: display preference, not state. Hide `status:*`, disable+re-enable `workflow`, `status:*` stays hidden.
3. **User reorders individual labels within the unified ring.** Confirmed: capability blocks don't move as units. The ring is one flat list; `order` is a per-FullName list.