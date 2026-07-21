# Task metadata column + capability view hook — Design Spec

**Status:** Draft 2026-07-21.
**Date:** 2026-07-21
**Task:** ATM-2e64a5 — *Task metadata column + capability view hook*
**Initiative:** ATM-4dd440 — capability extension points. Doctrine in `docs/architecture/label-substrate-and-capabilities.md` (§"The metadata column", §"Capability independence", §"Views live with the owner").
**Builds on:** `2026-07-18-capability-namespace-manager-actions-v2-design.md` (capability interface + registry), `2026-07-17-store-eventlog-carve-design.md` (event log engine), `2026-07-15-v1-storage-decommission-design.md` (cache rebuild discipline).

## Driver

Capabilities need self-managed per-task state — workflow_ai's plan pointers and stage bookkeeping, contextmap's provenance stamps — and the initiative closed the door on the legacy home for it: machine-readable comment formats are deprecated (`comment:provenance` is the named migration target), because they plant one capability's machine state in another surface's territory and make the comment thread a parse target when it exists for prose. The replacement is the substrate's second uniform mechanism, the same move labels made for shared state: a per-task metadata column, keyed by capability, opaque to everyone but the owner.

The column is useless if it cannot be seen. The companion half is the capability view hook: the capability interprets its own payload and renders a cell into a contextual column in the TUI tasks list — the user selects which capability owns the column, the capability decides text and emphasis, and raw payloads are never displayed. This is the seam the initiative's later tasks stand on: workflow_ai (ATM-efebc0) writes and annotates through it, and the contextmap migration (ATM-a2e902) retires `comment:provenance` onto it.

Nothing in-tree writes metadata when this task ships; the column is proven by the event/fold/cache test suite, and the view hook has real consumers on day one because both existing capabilities can annotate from data they already have.

## Scope

- One new `core.Task` field: `Meta map[string]string` (capability name → opaque payload).
- One new event action: `task.capability-meta-set` `{task, capability, payload}`, empty payload = clear.
- Changeset writer + store mutator (actor-required), fold projection to a `meta!<capability>` scalar slot, cache `meta` column + schema version bump, canon/golden extension.
- `atm task show` presence display: capability name + payload size per key, content never interpreted.
- One new `Capability` interface method: `Annotate(task core.Task) *Cell`, `Cell{Text, Tone}`; implementations for workflow (from status/priority labels) and contextmap (from existing provenance stamps).
- One new TUI tasks-list column rendering the selected capability's cells; selected-capability state cycled by a keybinding, persisted per project.

## Non-Goals

- No writer of `Meta` ships in this task. workflow_ai (ATM-efebc0) and the contextmap migration (ATM-a2e902) are the consumers, later.
- No generic CLI verb to write metadata (`atm task meta set` does not exist). Only capability verbs write, each to its own key. Raw store inspection remains the recovery escape hatch, morally equivalent to hand-assigning labels.
- No board/query access to payloads. Boards select over labels only; anything filterable must be projected into labels by the owning capability's verbs. The litmus test (architecture doc): if a board or another capability could ever need it, it is a label; if only the owner needs it, it is metadata.
- No append/patch payload semantics. Replace-only until a real consumer proves the need (candidate: the future comments-as-capability extraction).
- No per-key last-writer/timestamp in the projection or cache. The event log already carries the full audit trail; duplicating it into the projection is scope without a consumer.
- No pre-styled ANSI from capabilities, no lipgloss in capability packages. Cells are data (text + semantic tone); the TUI owns theming, width, and truncation.
- No custom per-capability panes. A capability needing more than a cell is the existing TUI overlay seam's territory.
- No packaging/identity work. Capability names becoming load-bearing keys is noted for ATM-e39512 (third-party packaging design), not solved here.

## Part 1: the substrate

### Data model

`core.Task` gains `Meta map[string]string` — capability `Name()` → payload. The payload is an opaque string: the store never parses it, and the owning capability chooses the encoding. Conventions for owners (doctrine, not validation): embed a format version and read your own old formats (degrade-never-reject applied to yourself); keep payloads small — pointers to big content, never the content.

A nil/absent map means no capability has state on the task. Keys are independent: setting one capability's payload never touches another's.

### Event

One new action, `task.capability-meta-set`, payload fields `{task, capability, payload}`. Empty `payload` clears the key — one action, no separate delete event, mirroring how a scalar slot naturally represents absence.

The name is deliberately not `task.meta-changed`. That string existed in v1 as a comment-counter bump on the parent task, was retired in v2 (ATM-0106; `libs/eventsource/action.go:4-8`), and v1 instances still ride through upgraded logs as unknown actions that write no slots (`replay.go:134-138`, `upgrade.go:128`, `fold.go:58`). Reusing the string would make legacy events suddenly write slots with a foreign payload shape; a fresh name keeps them inert forever.

The constant lands in both mirrors: `libs/eventsource/action.go` and `internal/store/eventlog/author.go`. The changeset gains a writer (shape of `EnableCapability`, `changeset.go:129-150`); the store gains a mutator `SetTaskCapabilityMeta(taskID, capability, payload, actor)` requiring an actor like every mutation.

### Projection and cache

The fold projects the event to a scalar slot keyed `meta!<capability>` — the same key-shaping pattern as the existing `capability!<name>` membership slots (`fold.go:17-23`) — and the rebuild reads the slots back into `Task.Meta`. Unknown-action doctrine is untouched.

The cache tasks table gains a nullable `meta` TEXT column (JSON object, key → payload), projected symmetrically with `capabilitiesToCache`/`FromCache`. `cacheSchemaVersion` is bumped; the schema is recreated wholesale on next open (no ALTER path, `cache_schema.go:3-7`), forcing a rebuild-from-events. Consequence for development: all work and testing runs against a store copy, never the live `~/.config/atm` — a schema-changing dev build against the shared cache breaks the installed binary.

Sync requires no work: the engine is action-agnostic once the constant exists. The canon (`libs/eventsource/canon.go`) and the golden log (`libs/eventsource/testdata/v2-golden.jsonl`) are extended with the new action.

### Visibility and isolation

Opaque is not invisible. `atm task show` lists presence per key — capability name and payload size — without interpreting content, including keys whose owner is disabled or not registered ("present, uninterpretable"). Payloads are retained when a capability is disabled (enablement is a fence on the tooling surface, never on data) and die with the task.

Isolation is enforced where it can be honest. The CLI surface has no generic metadata writer; only capability verbs write, each passing its own `Name()` to the store API. In-process Go cannot be truly fenced — any package could read the map — so the fence is the CLI surface plus the independence doctrine (a capability never addresses another's key), the same trust model as hand-assigned labels. The store validates nothing, including payloads: advisory, always.

## Part 2: the view hook

### Interface

`internal/capability/capability.go` gains:

```go
type Tone int

const (
	ToneNeutral Tone = iota
	ToneOK
	ToneAttention
	ToneStale
)

type Cell struct {
	Text string // short interpreted text, e.g. "planned", "needs-clarification"
	Tone Tone   // semantic emphasis; the TUI maps it to theme colors
}

type Capability interface {
	// ...existing methods...
	Annotate(task core.Task) *Cell // nil = nothing to say about this task
}
```

`Annotate` goes on the interface itself, not a side-interface: with two in-tree implementers the migration is trivial, and the packaging design (ATM-e39512) wants a complete interface to freeze. The contract is plain data — no ANSI, no styles — so it survives serialization across a future process boundary unchanged.

`Annotate` is read-only by construction: it receives a value and returns data. No reporter writes metadata; the reporter-purity invariant is untouched.

A capability whose own payload is unreadable handles it itself: return an Attention cell (e.g. `unreadable`), never panic, never leak raw bytes.

### Day-one implementations

Both existing capabilities implement `Annotate` from data they already have — no metadata required, which gives the column real content immediately and proves the hook end-to-end before any payload exists:

- **workflow**: from its own labels — `in-progress` (ToneOK), `blocked` (ToneAttention), `done` (ToneNeutral), `open` (ToneNeutral); a priority marker appended when a `priority:*` label is present (e.g. `open · high`). Nil for tasks with no status label.
- **contextmap**: for `context:*` tasks, from its existing stamps (`LatestStamp`): `no stamp` (ToneAttention), fresh (ToneOK, e.g. `fresh 2d`), old (ToneStale, e.g. `stale? 40d`) using its existing age heuristics; superseded pointers `superseded` (ToneNeutral). Nil for non-context tasks.

workflow_ai (ATM-efebc0) later becomes the first implementer that reads `t.Meta[Name()]` — the full pipeline.

### TUI column

The tasks list gains one contextual column between TITLE and LABELS; the header is the selected capability's `Name()`. Selection model:

- The TUI holds a **selected capability** per project. Default: the first enabled capability in registry order.
- A keybinding cycles the selection through enabled capabilities plus **off** (exact key chosen at implementation after checking `keymap.go` for a free one; candidate `[m]`). Off removes the column and returns its width to TITLE.
- The selection persists per project in the same side-file family as pins, surviving restarts.

Rendering: the TUI calls the selected capability's `Annotate` per visible row on the already-loaded `core.Task` (Meta arrives from the cache; no extra store reads), maps `Tone` to theme colors, truncates to the column width. Nil renders an empty cell. Both the flat list (`renderFlatList`) and the grouped view (`renderGroup`) carry the column; `taskColumnWidths` gains the column's width when a capability is selected.

## Testing

All development and tests run against a store copy — the schema bump forces a wholesale cache rebuild on first open.

1. **eventsource**: fold round-trip for `task.capability-meta-set` — set, overwrite, clear-via-empty, two capabilities on one task independent; canon equivalence and golden log extended; upgrade regression proving retired v1 `task.meta-changed` events still write no slots.
2. **store**: mutator requires an actor and unknown task fails cleanly; cache round-trip rebuilds `Meta` byte-identically; `atm task show` presence lines including a key whose capability is not registered.
3. **capability**: `Annotate` unit tests — workflow label combinations (each status, with/without priority, no status → nil); contextmap stamp presence/age/superseded cases and non-context → nil.
4. **TUI**: column renders the selected capability's cells; cycle key walks enabled capabilities + off; selection persists across reopen; header follows selection; width math with the column on and off.

## Sequencing sketch

1. Event + changeset + mutator + fold + canon/golden (eventsource and store layers, tests first).
2. Cache column + schema bump + `atm task show` presence.
3. `Cell`/`Tone` + `Annotate` on the interface + workflow and contextmap implementations.
4. TUI column, selection state, keybinding, persistence.

Each step lands green before the next; the implementation plan (writing-plans) will refine this into commit-sized steps.
