# Design: domain types + service interfaces in `internal/core`, composition root in `cmd/atm`

Ledger task: `ATM-b9d83a` — *Refactor step 4: domain types + service/repository interfaces in core; TUI on interface; composition root to cmd/atm*
Umbrella: ATM-9eb7dc · Specification: [docs/architecture/logical-components.md](../../architecture/logical-components.md)
Date: 2026-07-16 · Status: approved, pending implementation plan

## Problem

After step 3, `internal/core` owns the label algebra but no domain types and no interfaces. The adapters are still concretely coupled:

- `internal/tui` holds a `*store.Store` field (`app.go:67`), opens the store itself (`app.go:143`), and names ~40 store methods plus ~25 store types and package helpers across its panes.
- `internal/cli` imports `internal/tui` only to default its `tuiRunner` seam to `tui.Run` (`root.go:10`, `root.go:138`).
- `internal/version` imports `internal/store` only for `MarshalSorted` (`formatters.go:8`).
- `cmd/atm/main.go` is a one-liner; there is no composition root — wiring lives inside the adapters.

Step 4 moves the domain types into `core`, defines the service interfaces the adapters consume, puts the TUI on the interface, moves wiring to `cmd/atm`, and deletes the two backwards edges.

## Decisions (settled during brainstorm)

1. **Interfaces cover the tui+cli union; only TUI call-sites flip now.** Core's service interfaces are defined from what *both* adapters call today (the brief's "exactly what tui and cli consume"), but `internal/cli` keeps its concrete `*store.Store` via `openStore` until steps 5/6. The import-rules table already permits `cli → store` during migration.
2. **Repository interfaces are a skeleton.** Their only real consumer is step 6 (carving the event-log write-engine). Step 4 declares minimal per-entity shapes as the step-6 target; nothing consumes them yet.
3. **Role interfaces + composite.** Not one flat god-interface and not per-adapter interfaces: small role interfaces (interface-segregation, per-role seams for step 6) composed into one `core.Service` that keeps wiring trivial.

## Design

### What moves into `core` (store keeps aliases)

Types move; `internal/store` keeps Go type aliases (`type Task = core.Task`) so store internals and every CLI call-site compile unchanged. Aliases are temporary scaffolding, removed when step 6 churns store anyway.

- **Domain types** (`store/types.go` → `core/types.go`): `Task`, `Label`, `Comment`, `Project`.
- **Query/read-model types the TUI names**: `QueryFilters`, `LogEntry`, `Subject`, `Pins`, `Vocabulary`, `VocabularyTerm`, `VectorMeta`, `EmbeddingConfig`, `EmbedFunc`. (`Node` is already `core.Node` since step 3; the TUI references it directly.)
- **Pure helpers the TUI calls as package functions**: `Now`, `RFC3339UTC` (time conventions), `ParseExpr` (board-expression algebra — the architecture doc already assigns board expressions to core), `ParseCommentID`, `ValidatePersonaName`. A helper moves only if it is genuinely pure (standard library only); anything store-bound becomes an interface method instead.
- **Error kinds**: `ErrNotFound`, `ErrConflict`, `ErrIntegrity` sentinels plus the `IsNotFound`/`IsConflict`/`IsIntegrity` predicates move to core; store keeps aliases so its `errors.Is` wrapping and every existing error message stay byte-identical.

`core` remains a pure leaf: no internal imports, standard library only. Anything that would drag I/O into core stays behind the interfaces.

### Service interfaces (`internal/core/service.go`)

Role interfaces named in domain terms, covering **exactly** the union of methods `internal/tui` and `internal/cli` invoke on `*store.Store` today (~75 methods; the implementation plan enumerates them mechanically from call-site greps before writing the file):

```go
type TaskService interface { /* create/get/list/group, title/description/labels, remove */ }
type ProjectService interface { /* create/get/list/config/remove, name, remotes */ }
type LabelService interface { /* add/list/show/remove, usage, seed */ }
type CommentService interface { /* create/get/list, body, remove */ }
type PersonaService interface { /* create/get/list/edit/remove */ }
type VocabularyService interface { /* get/write */ }
type ActivityService interface { /* log reads, history, watch, last-seq */ }
type IndexService interface { /* reindex, vectors, embedding config, search */ }
type PinService interface { /* get/write pins */ }
type MaintenanceService interface { /* init, store path, verify, rebuild, migrate/upgrade, prune, sync */ }

// Service is the composite the composition root injects.
type Service interface {
    TaskService
    ProjectService
    LabelService
    CommentService
    PersonaService
    VocabularyService
    ActivityService
    IndexService
    PinService
    MaintenanceService
}
```

`*store.Store` satisfies these **structurally** — the `eventsync.LocalStore` pattern (`libs/eventsource/sync/engine.go:86`), generalized. Store gains a single compile-time assertion:

```go
var _ core.Service = (*store.Store)(nil)
```

Method signatures use core types only. No interface method may name an event, replica, HLC, or projector.

### Repository skeleton (`internal/core/repository.go`)

Per-entity repository interfaces — `TaskRepository`, `LabelRepository`, `ProjectRepository`, `CommentRepository` — with minimal read/write shapes mirroring what the store's projector/cache layer already provides. Doc-commented as the declared target for step 6 (ATM-3b873c); **nothing consumes them in step 4**, and step 6 may refine the shapes when the write-engine carve is actually studied.

### Wiring: TUI on the interface, composition root in `cmd/atm`

- `Model.store` becomes `core.Service` (`app.go:67`); every pane and sub-model follows.
- `tui.Run(svc core.Service, actor string) error`; `NewModelOpts` carries the service, not a path. Store resolve/open (`store.ResolveStorePath`, `store.Open`) leaves `tui` entirely.
- Auto-init stays in `NewModel`, expressed through the interface (`StorePath()`, `Init("")`) — behavior identical for a first-run TUI launch.
- `internal/cli` drops the `tui` import. The `tuiRunner` seam keeps its `func(storePath, actor string) error` signature but loses its default: `cli.Execute(deps cli.Deps)` takes an explicit dependency struct (`Deps{RunTUI: ...}`), and a nil runner returns an explicit "tui runner not wired" error. Existing CLI tests that stub `runTUI` keep working.
- `cmd/atm/main.go` becomes the composition root: it builds the closure — resolve path → `store.Open` → `tui.Run(s, actor)` — and passes it to `cli.Execute`. `cmd/atm` may import anything; it contains wiring only, no domain or presentation logic.

### `internal/version` becomes a pure leaf

`formatters.go` drops `store.MarshalSorted` and emits its JSON with plain `encoding/json`: Go marshals map keys in sorted order, and `SetIndent("", "  ")` + `SetEscapeHTML(false)` reproduce the current output byte-for-byte for the flat version map. `internal/version` ends the step importing no internal package.

### Test-file allowance

`internal/tui` *_test.go files may still import `internal/store` to construct the real implementation behind the interface (integration-style, the same posture eventsync's tests take). The tui import rule applies to production files.

## Error handling

Zero behavior change. Error sentinels move by aliasing, so every `errors.Is` chain, message string, and CLI exit code is unchanged. The only new error is the deliberate "tui runner not wired" guard, which is unreachable through `cmd/atm`.

## Testing

- `make verify` green on both modules; no golden output changes anywhere (CLI goldens, version output, TUI behavior).
- Compile-time interface conformance via the `var _ core.Service` assertion.
- **New import-boundary test**: a small `go list`-driven test (under `tests/` or `internal/core`) asserting the architecture doc's import-rules table for the packages this step touches — `internal/tui` (non-test) imports only `core` + external libraries, `internal/version` imports no internal package, `internal/core` imports nothing internal. It turns the spec's "enforceable heart" into CI and guards steps 5/6.

## Acceptance criteria (from ATM-b9d83a)

- Build and tests green (`make verify`).
- `internal/tui` production files import only `internal/core` (and `tui`-internal packages / external libs).
- `internal/version` imports no internal package.
- The import-rules table in `docs/architecture/logical-components.md` holds; the new boundary test enforces it.

## Out of scope

- Flipping `internal/cli` call-sites to the core interfaces (steps 5/6).
- The capability registry and `internal/capability/*` (step 5).
- Carving store's write-engine behind the repository interfaces (step 6); step 4 only declares the skeleton.
- Removing the temporary type aliases in store (falls out of step 6).
