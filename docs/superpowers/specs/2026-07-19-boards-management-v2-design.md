# Boards management v2 — design

Date: 2026-07-19
Status: approved design (brainstormed 2026-07-19; tracking task ATM-87b3ae; revised 2026-07-19 after codebase self-review — see §10)
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

### 1. Capability contract — `Vocabulary` + `Exposed`

The `Capability` interface (`internal/capability/capability.go:47`) gains two pure methods (revised from one — see §10, ownership decision):

```go
// Vocabulary declares every label this capability owns for the project:
// stored labels, namespace descriptors, and boards — exactly the set
// EnsureVocabulary seeds. Pure read, no store side effect. This is the
// OWNERSHIP surface: Registry.Unmanaged subtracts it.
Vocabulary(code string) []core.Label

// Exposed declares the computed labels (boards + namespace descriptors)
// this capability surfaces in the TUI ring for the project. Pure read,
// no store side effect. Order within the slice is the capability's
// preferred ring order; the registry preserves registration order across
// capabilities. Invariant: Exposed ⊆ Vocabulary.
Exposed(code string) []core.Label
```

Implementation changes:

- **`workflow.Vocabulary(code)`** returns the 13 labels `EnsureVocabulary` seeds today (9 stored/namespace + 4 boards). **`workflow.Exposed(code)`** returns 6 labels in this order: `all-tasks`, `open-tasks`, `in-progress-tasks`, `backlog` (Expr set — the four boards) + `status:*`, `priority:*` (Expr empty, namespace descriptors).
- **`contextmap.Vocabulary(code)`** returns the 9 labels `EnsureVocabulary` seeds today. **`contextmap.Exposed(code)`** returns 1 label: `context-current` (Expr set).

`EnsureVocabulary` keeps its signature and behavior (mutates the store, returns the Expr-boards) but is refactored to iterate `Vocabulary(code)` — one literal list per capability feeds all three methods, so divergence is structurally impossible rather than merely a bug.

The capability doctrine holds: a capability author chooses what to expose. Exposing a namespace is opt-in, not a substrate rule — workflow exposes `status:*` and `priority:*`; contextmap doesn't expose `context:*`. Future capabilities decide for themselves.

### 2. Registry surface — `Unmanaged`

`Registry` gains one derived-view helper:

```go
// Unmanaged returns labels in the project's LabelList that no enabled
// capability owns via Vocabulary. Derived, not stored. The TUI renders
// these under the synthetic umbrella row; the CLI verb exposes the same
// set to the manager agent for triage.
func (r *Registry) Unmanaged(svc core.LabelService, code string) ([]core.Label, error)
```

Implementation: `svc.LabelList(code, "")` minus the **owned set** (the registry already filters by enabled set at the call site — `reg.For(project).Unmanaged(...)`). A stored label is owned when either:

1. its FullName appears in the union of `c.Vocabulary(code)` across enabled capabilities (exact match — covers boards, descriptors, and singleton labels like `comment:provenance`), or
2. it sits under an owned namespace descriptor: `<code>:<ns>:<value>` is owned when `<code>:<ns>:*` is in the vocabulary union (so ad-hoc members of an owned namespace, e.g. a hand-added `status:wip`, stay managed and surface in the `status` chart, not the umbrella).

Everything else — leftover `:*` descriptors, loose tags, members of unowned namespaces — lands in the unmanaged set. Note ownership is by `Vocabulary`, not `Exposed`: a capability's internal labels (contextmap's `context:*`, `knowledge:superseded`) are neither in the ring nor in the umbrella.

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
- **`pins`** replaces `pins.json` verbatim. `core.Pins`, `internal/store/pins.go`, and the `Pins` alias in `internal/store/types_compat.go` are removed; pin persistence moves to `config.json.boards.pins`. max-3 cap stays; the cap is enforced at write time in the new config-write path (`SetProjectBoards` returns `core.ErrUsage` on more than 3).

**Migration.** Lazy, at read time: when the boards config is read (a new `GetBoardsConfig(code)` on the store, used by TUI and CLI) and `config.json.boards` is nil but `pins.json` exists, `pins.json` is folded into the returned `BoardsConfig.Pins` in memory. The merged value is persisted the first time any boards write happens (`SetProjectBoards` — a pin toggle, hide, or reorder), which stamps `boards` into `config.json`; from then on `boards != nil` and `pins.json` is ignored. `pins.json` is left on disk (harmless, dead). No event log entry — display preferences aren't substrate state. `atm store rebuild` does not touch `config.json`/`pins.json` (it only reprojects cache.db from event logs) and plays no role in this migration.

**`GetProjectConfig` emptiness check.** `internal/store/config.go:17` treats a config as absent when `Embedding == nil && UpdatedAt == "" && len(Remotes) == 0`; the check gains `&& Boards == nil`.

### 4. TUI Boards pane

`buildBoardRows` (`internal/tui/labels.go:562`) is rewritten. The new population:

1. **Managed set.** `reg.For(project)` → loop capabilities, call `c.Exposed(code)`, collect `(Label, ownerName=cap.Name())`. One row per returned label. `boardRow` gains an `Owner string` field.
2. **Umbrella.** `reg.Unmanaged(store, code)` → if the return is non-empty, add one synthetic row `{FullName: code+":unmanaged", Name: "unmanaged", Expandable: true, Owner: ""}`. The umbrella is omitted entirely when the unmanaged set is empty (no long tail → no row).
3. **Hidden filter.** Drop rows whose `FullName` is in `config.boards.hidden`.
4. **Order.** Stable-sort by `config.boards.order` index; rows not in `order` keep their original (capability-registration, then umbrella-last) relative order and append.
5. **Render.** Each row gains an owner tag column (muted): `all-tasks  workflow`, `status  workflow`, `unmanaged  —`. Managed rows take their **description from the stored label** when one exists (a human may have curated it), falling back to the `Exposed` literal; the count/broken/NeedsDescription derivations are unchanged.

**Umbrella selection and drill.** Selecting the umbrella in the ring applies **no task filter** (`setFocus(focusOff, "")` — the Tasks pane shows all tasks, as when no board is selected); `ATM:unmanaged` is not a real label and cannot be a filter token. Drill-in enters a new **umbrella sub-table** level: the old emergent derivation (today's `buildBoardRows` namespace-plus-loose-label logic) run over *only the unmanaged labels*, rendered with the existing L0 table style. Rows in the sub-table drill into the existing chart/detail views with unchanged semantics; Esc climbs back sub-table → ring. This adds one drill level to the `lLevel` state machine for the umbrella path only.

`selectDefault` (`labels.go:253`) keeps today's logic: `all-tasks` if present in the ring, else first ring member. The umbrella is never the default — `selectDefault` skips it (a ring containing only the umbrella leaves `selected` empty and the Tasks pane unfiltered).

The owner tag uses an existing muted style (`m.styles.Muted`); no new theme color. The owner column is narrow (10 chars, truncated with `lipgloss.Width`-aware truncation as `boardTableLine` already does for the name column).

### 5. CLI surface

Four new verbs, all under existing namespaces:

- **`atm capability unmanaged --project <CODE>`** — read-only. Lists every unmanaged label with name + description + usage count. JSON envelope: `{project, labels: [{name, description, usage}]}`. This is the manager agent's read path. Implemented as a thin wrapper over `Registry.Unmanaged`.
- **`atm project boards reorder --project <CODE> --name <FULL> [--before <FULL> | --after <FULL> | --first | --last]`** — writes `config.json.boards.order`. One board per invocation (the typical case); `--first`/`--last` shortcuts. Semantics: materialize the current effective ring order (enabled capabilities' `Exposed` in registration order, umbrella sentinel last, existing `order` override applied), move the named entry, write the full result back as `order`.
- **`atm project boards hide --project <CODE> --name <FULL>`** — writes `config.json.boards.hidden` (add).
- **`atm project boards show --project <CODE> --name <FULL>`** — writes `config.json.boards.hidden` (remove).

Pins remain **TUI-only** (there is no `atm project pins` CLI today and this design adds none — corrected in review, §10): the `[p]` toggle and Shift-1..3 jumps are unchanged; only their persistence target moves to `config.json.boards.pins`. The old `pins.json` path is no longer consulted after migration.

All verbs require `--actor` (mutating) or default it (read-only), matching existing `atm project` verbs. They write via the existing `core.ProjectConfig` read-modify-write path — no new store event, no event-log entry. The `updated_at`/`updated_by` stamps on `ProjectConfig` are refreshed on every write.

### 6. Manager prompt / autopilot

The autopilot procedure (carried in the conventions / a capability guide section) gains a triage step. No manager-specific code — the verb is the contract:

> Run `atm capability unmanaged --project <CODE>`. For each unmanaged prefix, decide whether its tasks should carry a capability-owned label instead (e.g. replace `type:bug` with `status:open` + `priority:high` if the task is genuinely triageable onto the workflow paved road). Use `atm task label remove --label <CODE>:<unmanaged>` and `atm task label add --label <CODE>:<owned>` to redistribute. Hide prefixes you deliberately don't want to see with `atm project boards hide --name <CODE>:<ns>:*`. Re-run `atm capability unmanaged` to verify.

The manager prompt's `<CAPABILITY_ROLES>` enumeration is unchanged — `unmanaged` is a CLI verb on the substrate surface (`atm capability unmanaged`), not a capability. The manager reads it the same way it reads `atm label list` or `atm task list`: as a substrate query.

### 7. Architecture / layering

- `internal/core` — `BoardsConfig` type; `ProjectConfig` gains the `Boards` field. `core.Pins` is removed. The `PinService` role interface (`core/service.go:94`) is removed from the `Service` composite; `ProjectService` gains `GetBoardsConfig(code string) (*BoardsConfig, error)` (read, with lazy pins.json fold-in) and `SetProjectBoards(code string, b *BoardsConfig, actor string) error` (a per-field write helper mirroring the existing `SetEmbeddingConfig` shape in `internal/store/config.go:23`; enforces the max-3 pins cap). `GetProjectConfig` (`core/service.go:38`) stays for whole-config reads.
- `internal/store` — `pins.go` is removed (and the `Pins` alias in `types_compat.go`). `internal/store/config.go` gains `GetBoardsConfig(code)` and `SetProjectBoards(code, b, actor)` following the `SetEmbeddingConfig` pattern (read-modify-write under the project lock); the `GetProjectConfig` emptiness check gains `Boards == nil`. No new store event.
- `internal/capability` — `Capability.Vocabulary` + `Capability.Exposed`; `Registry.Unmanaged` (ownership = vocabulary FullNames + owned-descriptor prefixes). Tests cover the diff correctness and the per-capability `Vocabulary`/`Exposed` returns.
- `internal/capability/workflow` — one literal vocabulary list feeds `Vocabulary` (13), `Exposed` (6), and the refactored `EnsureVocabulary`.
- `internal/capability/contextmap` — same shape: `Vocabulary` (9), `Exposed` (`context-current`), refactored `EnsureVocabulary`.
- `internal/cli` — `atm capability unmanaged`; `atm project boards reorder/hide/show`. No pins CLI.
- `internal/tui` — `buildBoardRows` rewrite; owner column; umbrella sub-table drill level; `loadPins`/`persistPins` retargeted to `GetBoardsConfig`/`SetProjectBoards`.

No new packages. The capability doctrine holds: surfaced concerns live in capabilities; the substrate stores labels and config; the TUI renders.

### 8. Testing

- **`internal/capability`**: `Registry.Unmanaged` correctness — `LabelList` minus the owned set (vocabulary FullNames + owned-descriptor prefix coverage) returns exactly the unmanaged set, for several configurations (no unmanaged; capability-internal labels like `status:open`/`context:agent` are NOT unmanaged; ad-hoc member of an owned namespace like `status:wip` is NOT unmanaged; leftover `:*` descriptors and loose tags ARE unmanaged; workflow disabled → its labels become unmanaged). `Vocabulary`/`Exposed` per capability return the documented sets (workflow 13/6, contextmap 9/1) and `Exposed ⊆ Vocabulary`. The diff is stable across repeated calls (no store mutation). `EnsureVocabulary` still seeds exactly `Vocabulary`.
- **`internal/store/config`**: `BoardsConfig` round-trip JSON; `Order`/`Hidden`/`Pins` field independence; `GetBoardsConfig` folds legacy `pins.json` in when `boards` is nil and ignores it once `boards` is set; max-3 cap enforced by `SetProjectBoards`; `GetProjectConfig` non-nil for a boards-only config.
- **`internal/tui/labels`**: ring population uses `Exposed` + umbrella, not emergent derivation; owner tag rendering for managed rows and `—` for the umbrella; stored-label description wins over the `Exposed` literal; `order` application (partial override, unmatched fall back to registration order, umbrella default last); `hidden` filter (hidden board never in ring, never in pin candidates); umbrella selection applies no task filter; umbrella drill-in shows the emergent sub-table scoped to unmanaged labels, then chart/detail; `selectDefault` skips the umbrella.
- **`internal/cli`**: new verb goldens (`atm capability unmanaged` JSON + text, `atm project boards reorder/hide/show` round-trip against `config.json`). The `--name` flag accepts both board FullNames (`ATM:open-tasks`) and namespace FullNames (`ATM:status:*`).
- **Existing tests**: every test that seeded ad-hoc namespaces via task labels (e.g. `type:bug`) and expected them in the L0 ring updates — they now appear only under the umbrella, drilled in. Tests that asserted the alphabetical sort of L0 update to assert capability-registration order + `order` override. Pin tests migrate from `pins.json` fixtures to `config.json.boards.pins` fixtures.

### 9. Migration / compatibility

- **`pins.json` → `config.json.boards.pins`.** Lazy: `GetBoardsConfig` folds `pins.json` in while `config.json.boards` is nil; the first boards write persists the merged value, after which `pins.json` is dead. `pins.json` is left in place; `atm store rebuild` does not touch it (rebuild only reprojects cache.db). No event log entry.
- **Existing projects with no `boards` config.** `config.boards` is nil → default order (capability-registration, umbrella last), no hidden, no pins. Behavior matches today's TUI for projects with no pins, except the ring population changes (capability-authored + umbrella instead of emergent).
- **Disabled capability with hidden boards.** `config.boards.hidden` may list FullNames a disabled capability would have exposed. They're hidden regardless — no error, no warning. When the capability is re-enabled, the entries take effect again.
- **`order` entries for non-existent boards.** Silently ignored. Defensive against typos and against stale entries after a capability is disabled or a board renamed.
- **Binary upgrade.** No new store event, no schema change, no event-source upgrade. The change is in config.json shape + TUI/CLI behavior. A user rolling back to an older binary would lose the `boards` config (older binary ignores the `boards` key) but keep `pins.json` if it was never absorbed. Safe.

## Open questions (resolved in brainstorm)

1. **Umbrella FullName** — `ATM:unmanaged` (TUI/CLI sentinel, never a real label). The TUI and CLI never call `LabelAdd("ATM:unmanaged", ...)`; the name is only used as a row identifier and an `order`/`hidden` key. A user who hand-creates a real label that collides — either the tag `ATM:unmanaged` or the namespace `ATM:unmanaged:*` (both pass the label-name validator) — shadows the sentinel: the umbrella row is suppressed and the real label renders as a normal unmanaged row (the sentinel loses). The guard is one check in `buildBoardRows`, covering both shapes.
2. **Hidden persists across capability re-enable.** Confirmed: display preference, not state. Hide `status:*`, disable+re-enable `workflow`, `status:*` stays hidden.
3. **User reorders individual labels within the unified ring.** Confirmed: capability blocks don't move as units. The ring is one flat list; `order` is a per-FullName list.

## §10 Revision notes (2026-07-19 self-review against the codebase)

Corrections made after verifying the approved design against the tree, with the ownership/pins/umbrella decisions confirmed by the user:

1. **Ownership ≠ exposure.** The original `Unmanaged = LabelList − union(Exposed)` diff would have classed every capability-internal label (`status:open`, `priority:high`, all of contextmap's `context:*`/`knowledge:*`/`comment:provenance` set) as unmanaged — a permanently non-empty umbrella on every contextmap project, and a triage prompt aimed at capability bookkeeping. Resolved: the contract gains a pure `Vocabulary` (ownership) alongside `Exposed` (ring display); `Unmanaged` subtracts vocabulary FullNames plus members under owned `:*` descriptors (§1, §2).
2. **`atm project pins` does not exist.** Pins are TUI-only (`[p]`, Shift-1..3). The design keeps them TUI-only; only the persistence target moves (§5).
3. **Umbrella selection/drill were underspecified.** Resolved: selection applies no task filter; drill-in is a new sub-table level running the old emergent derivation over unmanaged labels only (§4).
4. **`atm store rebuild` never touches `config.json`/`pins.json`** — migration is lazy read + persist-on-first-boards-write only (§3, §9).
5. Smaller fixes: `GetProjectConfig` emptiness check gains `Boards` (§3); `types_compat.go` `Pins` alias removal (§7); sentinel guard covers the plain-tag collision too (open question 1); managed rows prefer the stored label's curated description (§4); `reorder` materializes the effective order before moving (§5).