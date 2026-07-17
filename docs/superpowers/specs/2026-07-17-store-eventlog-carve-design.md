# Store event-log write-engine carve — design (refactor step 6)

Ledger: ATM-3b873c, step 6 of 6 under ATM-9eb7dc. Specification of record:
[docs/architecture/logical-components.md](../../architecture/logical-components.md).
This document records the step-6 design decisions taken with the user on
2026-07-17 and is the input to the implementation plan.

## Problem

`internal/store` is one package where twelve `eventsource_*.go` files, the
sqlite read-cache, the domain service methods, and the plain-JSON side stores
all share the `Store` struct's private state (`store.go:17-46`). Two boundary
violations remain above it:

- `internal/cli/store_sync.go` imports `atm/libs/eventsource/sync` and passes
  the concrete store as `eventsync.LocalStore` — the only place outside
  `internal/store` that names event-sourcing concepts.
- The `atm store …` admin commands call the concrete `*store.Store`, which the
  import table in the architecture doc allows "only until step 6 moves the
  remaining admin surface behind interfaces".

Additionally, `internal/store/core_aliases.go` (temporary step-4 re-exports)
is explicitly marked for removal by this step, and `internal/core/repository.go`
declares CRUD-shaped repository interfaces that nothing consumes and whose
shape (`PutTask`/`DeleteTask`) cannot honestly describe an append-only event
log.

## Goals

1. The event-log write-engine becomes a coherent, separable unit behind
   domain-termed interfaces; the read-cache and the write-engine no longer
   share private state.
2. Nothing above `internal/store` names an event, replica, HLC, or projector —
   compile-enforced, arch-tested.
3. `internal/cli` stops importing `internal/store` entirely (completes the
   import table's target row for cli).
4. No behavior change: every CLI output byte-identical (goldens are the gate),
   all eventsource e2e/determinism/sync tests still green.

## Non-goals

- No new features, no output changes, no event-format changes.
- No promotion of anything to `libs/` and no SDK work.
- The plain-JSON side stores (config, pins, agents, vocabulary, personas) stay
  file-backed in the facade; they were never event-sourced.
- The TUI is untouched (it already consumes only `core.Service`).

## Decisions (with the alternatives that were rejected)

1. **Sever cli→store fully** rather than only killing the eventsource-type
   leaks. The architecture doc assigns the admin-surface interface move to
   step 6, and no later step exists to pick it up.
2. **Single-subpackage carve**: new `internal/store/eventlog` owns everything
   that touches `libs/eventsource`; package `store` remains the facade.
   Rejected: same-package file reorg (boundary would be convention, not
   compiler-enforced) and a second `cache` subpackage (no second consumer of
   the cache; plumbing without enforcement gain).
3. **Intent-shaped engine contract in core**, replacing the CRUD repositories.
   An event log records intents; `PutTask`(diff-the-struct) would erase the
   distinctions the history views render. The `eventsync.LocalStore` pattern,
   generalized, as the plan of record prescribes. Rejected: keeping CRUD repos
   as the cache contract (the write path would still need its own intent
   contract; core would gain interfaces both implemented and consumed inside
   store) and deleting the core interfaces outright (inverts "core declares,
   store implements").
4. **`core.StorageAdmin` in storage-neutral vocabulary**, beside
   `core.Service`. Format identifiers cross the seam as opaque strings
   (`"v1"`/`"v2"`) that store validates; report types move to core as neutral
   structs. Core learns that storage *maintenance* exists; it never learns
   that persistence is event-sourced. Rejected: a `core/admin` subpackage
   (another import-table row for little gain) and a cli-defined consumer-side
   interface (contract would live in the adapter).

## Target shape

```
internal/store            facade: implements core.Service + core.StorageAdmin
  │   sqlite cache + projection, query, domain services,
  │   plain-JSON side stores (config/pins/agents/vocabulary/persona)
  ├── internal/store/eventlog   the ONLY importer of libs/eventsource
  │       events.v2.jsonl I/O + tail repair, authoring funnel,
  │       HLC/replica identity, store.json format meta, v1→v2 upgrade,
  │       sync engine (LocalStore impl + orchestration), fold→core converters
  └── internal/store/fslock     leaf: the file-lock primitive both import
```

The lock leaf landed as `internal/store/fsio` (lock + atomic JSON + `core.MarshalSorted`), a refinement of this spec's `fslock` name discovered when the JSON helpers turned out to be shared by both sides.

### What moves into `internal/store/eventlog`

Wholesale, with their tests, preserving semantics verbatim:

| Today | Content |
|---|---|
| `eventsource_file.go` | event-file read/append, tail repair, strict verify |
| `eventsource_author.go` | `beginV2AuthorLocked`/`commitV2AuthorLocked`, the `appendV2*` funnel, ref resolvers, alias collision sets |
| `eventsource_replica.go` | replica minting, copy-detection re-mint |
| `eventsource_meta.go` | `store.json` (`StoreMeta`, `LastHLC`, formats), `mutateStoreMeta`, `withProjectFormatLock`, lock ordering |
| `eventsource_upgrade.go` | v1→v2 migration (temp-write → verify → compare → rename) |
| `eventsource_sync.go` | `SyncSnapshot`/`SyncIngest`/`SyncBootstrap` (`eventsync.LocalStore`) |
| `cli/store_sync.go` orchestration | `eventsync.Sync` invocation, target selection (`SelectTarget`, dir/git), options plumbing |
| projector converters | `taskFromV2`/`commentFromV2`/`labelFromV2`/`projectFromV2` — the fold→core translation |
| `eventsource_views.go`, v2 parts of `log.go`/`read.go` | event-file parsing for log views and freshness counts, returned as core types |

The engine exposes **core-typed state only**: fold results cross the seam as
core snapshots (tasks/comments/labels/project), never as
`eventsource.Event`/`State`. Event counts cross as plain ints (the freshness
key). `V2Draft`, `V2FileSnapshot`, `StoreFormat`, `StoreMeta`,
`UpgradeReport`-internals et al. stop being exported from any package the
adapters can see.

### What stays in the facade (`internal/store`)

- sqlite cache (`cache.go`, `cache_schema.go`, `rebuild.go`, `verify.go`
  reporting), projection into rows, query/search/vectors/indexer.
- Domain service files (`task.go`, `label.go`, `comment.go`, `project.go`,
  `persona.go`, …) — they keep their orchestration (actor validation,
  vocabulary checks) and call the engine through the core interfaces instead
  of the `appendV2*Locked` helpers.
- Plain-JSON side stores; builtin-persona seeding.
- `core.Service` and `core.StorageAdmin` implementations.

### Construction and seams

`store.Open(root, opts...)` keeps its signature and constructs the engine
internally — `cmd/atm` does not change shape. The determinism options
(`WithClock`, `WithReplicaEntropy`, `WithNow`) thread into engine
construction; `store_seams_test.go` continues to pin that option-less `Open`
is byte-for-byte production behavior and that the seams make v2 authoring
reproducible.

The `Store` struct sheds the write-engine fields (`clockNow`,
`replicaEntropy`, format/meta access) into the engine; it keeps the cache
handle (`cacheOnce`/`cacheDBConn`), the log-snapshot memo, and a reference to
the engine.

## The engine contract (reshaped `core/repository.go`)

The CRUD interfaces are replaced by intent-shaped writer interfaces in domain
terms, mirroring the closed action set the log actually records. Sketch (final
signatures are the plan's job; they are extracted from today's funnel call
sites, not invented):

```go
// core — names indicative, shapes settled at plan time.
type TaskWriter interface {
    CreateTask(project string, t TaskDraft, actor string) (id string, err error)
    SetTaskTitle(id, title, actor string) error
    SetTaskDescription(id, description, actor string) error
    AddTaskLabel(id, label, actor string) error
    RemoveTaskLabel(id, label, actor string) error
    RemoveTask(id, actor string) error
}
// CommentWriter, LabelWriter, ProjectWriter: the comment/label/project
// equivalents of the same actions, all carrying actor.

type SnapshotReader interface {
    ProjectSnapshot(code string) (ProjectSnapshot, error) // core-typed fold result
    ChangeCount(code string) (int, error)                 // the freshness key
}
```

The engine's concrete type implements them; the facade consumes them. This is
the seam a future alternative persistence would implement.

**Projection stays under the project lock.** Today every mutation ends with
`reprojectV2Locked` before the lock releases, and the sqlite freshness row is
keyed on event count. To preserve that exactly, the engine is constructed with
a facade-supplied, core-typed **projection hook**:

```go
// store → eventlog at construction:
onCommit func(code string, snap core.ProjectSnapshot, changeCount int) error
```

invoked after the commit point (event line appended, `LastHLC` persisted) and
before the project lock releases, on every mutation, ingest, upgrade, and
bootstrap path that projects today. Write-through semantics, failure ordering,
and the freshness key are unchanged; the hook is also exactly where read-cache
and write-engine separate.

## The admin seam (`core.StorageAdmin`)

A storage-maintenance role interface beside `core.Service`, wired separately
by the composition root (it does not join the `core.Service` composite — the
TUI never needs it):

```go
type StorageAdmin interface {
    VerifyStorage(project string) (VerifyReport, error)   // + all-projects form
    RebuildDerived() (RebuildReport, error)
    UpgradeStorage(project string) (UpgradeReport, error)  // + all form
    PruneLegacy(project string) (PruneReport, error)
    SetStorageFormat(format string) error                  // "v1"/"v2", store-validated
    StorageFormats() (map[string]string, error)            // for `store … --json` surfaces
    ReadChangeLog(project string, opts LogViewOptions) (LogView, error)
    SyncProject(project, remote string, opts SyncOptions) (SyncReport, error)
}
```

Report/view types (`VerifyReport`, `RebuildReport`, `UpgradeReport`,
`PruneReport`, `SyncReport`, `LogView`) move to core as storage-neutral
structs carrying exactly the fields the CLI already prints. The CLI's
user-facing vocabulary (flags saying `v2`, command names like `upgrade`) is
unchanged — neutrality is about types and imports, not about the strings users
see. Sync target selection (dir vs git) moves inside the engine; the CLI
passes the remote name/URL and prints the returned report.

Method-name collisions with today's exported store methods (`Verify`,
`Rebuild`, `SetActiveFormat`, …) are avoided by giving the core interface the
names above; store keeps thin adapters where the old names must survive for
tests.

## CLI and composition root

- `internal/cli` loses every `atm/internal/store` and
  `atm/libs/eventsource/...` import. `cliState` consumes `core.Service` +
  `core.StorageAdmin`, both injected from `cmd/atm` (which constructs the one
  concrete store satisfying both).
- `internal/store/core_aliases.go` is deleted; surviving `store.X` references
  in cli flip to `core.X`.
- The import table in `logical-components.md` is amended: cli's row drops the
  step-6 store exception; new rows for `internal/store/eventlog` (may import
  `core`, `fslock`, `libs/eventsource`) and `internal/store/fslock` (nothing
  internal); `internal/store` row becomes `core`, `eventlog`, `fslock`,
  `seed`.

## Arch-test additions (`tests/arch/imports_test.go`)

1. `internal/cli` does not import `atm/internal/store` (production files).
2. Only `internal/store/eventlog` may import `atm/libs/eventsource/...` —
   in particular the store facade may not.
3. The facade does not reference event-sourcing identifiers (the import rule
   makes this structural; no grep-test needed).
4. Existing tui rules stay; tighten per the step-4 breadcrumb where the
   satellite purge allows.

## Testing and parity strategy

- **Byte parity**: full `atm store …` help and output surface via the existing
  cli goldens; zero golden churn expected. Any needed regeneration is a design
  bug, not a test update.
- **Moved tests move with their code**: the eventsource e2e, determinism
  (`store_seams_test.go` stays facade-side against `Open`), live-read/write,
  sync e2e, upgrade, verify suites keep their assertions substantively
  unmodified (package/import lines only).
- **Staged, green-at-every-commit sequencing** (ordering is the plan's job):
  extract `fslock` leaf → move engine files into `eventlog` with a temporary
  wide constructor → introduce the core intent interfaces + projection hook
  and flip the facade onto them → move sync orchestration in → introduce
  `core.StorageAdmin` + report-type moves → sever cli imports + delete
  `core_aliases.go` → arch tests + doc amendments.
- `make verify` green at every commit; smoke test on a throwaway store
  (create/mutate/sync/upgrade round-trip) before merge.

## Risks

- **Lock-order and commit-point semantics crossing a package seam** — the
  project → store-meta lock order and the append-then-persist-HLC ordering
  are load-bearing (documented in `eventsource_author.go` /
  `eventsource_meta.go`). Mitigation: verbatim code movement, the existing
  live-write and sync e2e tests, and keeping both lock names on the one
  `fslock` primitive.
- **The projection hook** changes call topology (facade callback instead of a
  method call within one package). Mitigation: hook runs at exactly the old
  `reprojectV2Locked` call sites; live-read/live-write tests pin freshness
  behavior.
- **Breadth of the cli type-flip** once `core_aliases.go` goes — mechanical
  but wide. Mitigation: it is a dedicated, compiler-driven step; no behavior
  edits ride along.

## Acceptance criteria (restating the ledger task, sharpened)

1. Build and tests green, including eventsource e2e and determinism tests.
2. No exported event-sourcing types from any package adapters can import;
   `internal/cli` imports neither `internal/store` nor `libs/eventsource`.
3. Arch tests enforce the new boundaries; import table updated.
4. CLI output byte-identical (goldens unchanged).
5. `core_aliases.go` gone; `core/repository.go` reshaped to the intent
   contract with the engine as its implementer.
