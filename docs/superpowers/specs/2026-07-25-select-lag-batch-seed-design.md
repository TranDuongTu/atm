# Select-lag fix: changeSet fold memoization + batch vocabulary seeding (ATM-40faff)

Task: ATM-40faff (duplicate report: ATM-013f52). Diagnosis journaled on ATM-40faff (comment ATM-40faff-c1c63).

## Problem

Selecting a project in the TUI (`'s'` in the projects pane) freezes the UI for several seconds — on every select, not just the first. Measured on a copy of the live store (~2.5k events, 3.1MB events.v2.jsonl):

- `EnsureVocabulary` on the select path: 2.42s (2.79s on a second select). Every other stage of the handler is 2–35ms except a 248ms first-touch `refreshSummary`.
- One no-op `LabelSeed`: ~250ms, 72MB / 843k allocs (`BenchmarkNoopSeedReal`).
- CPU profile: 84% of the seed transaction is `beginAuthorLocked` — `ReadV2File`/`eventsource.Parse` (JSON validity + JCS canonicalize + id recompute per line) 51%, `FoldEvents` (payload re-unmarshal + fold) 33%.

Root cause: every `LabelSeed` opens its own `WithProjectWrite` transaction, and `changeSet.SeedLabel` runs its own `beginAuthorLocked` — a full strict read + fold of the entire event log — so `EnsureVocabulary` costs one full fold per seeded label. The probe registered only the workflow capability (13 labels: 9 status/priority namespace labels + 4 boards — grown from the 4 boards ATM-d402aa measured) and measured 2.4–2.8s; the real composition root (cmd/atm/main.go) registers workflow + contextmap + workflow_ai, all three enabled on the ATM project, so a real select seeds **34 labels ≈ 8.5s**. ATM-d402aa fixed the reprojection half and explicitly recorded this residual ("batch-seeding + fold memoization") as the follow-up if ever felt; vocabulary growth 4→34 and log growth 1660→2509 events made it dominant.

## Goal and acceptance

A converged (no-op) project select folds the event log exactly once. Acceptance: the select-path `EnsureVocabulary` measures **< 400ms** (target ~250ms) on the live-store copy via the select-path probe, down from 2.4–2.8s. Behavior (seed semantics, board aggregation order, TUI handler flow) is unchanged.

## Design

Approach chosen during brainstorming (option A of three): fix at the engine/facade layer where the waste is; no TUI changes, no cache-as-authority shortcuts, no engine-wide fold cache.

### 1. Fold memoization inside `changeSet` (internal/store/eventlog/changeset.go, author.go)

A `changeSet` lives entirely inside one `WithProjectWrite` and holds the project lock, so the event file cannot change except through its own appends.

- `changeSet` gains a cached `*authorCtx`. A new `cs.begin()` helper returns the cached ctx or calls `beginAuthorLocked` once and caches it.
- Every changeSet method that currently calls `beginAuthorLocked` directly (`SeedLabel`, `RemoveLabel`, `mutateTask`, `mutateComment`, `CreateTask`, `CreateComment`, `CreateProject`, `SetProjectName`, `capabilityEvent`, `RequireProject`, `ResolveTask`, …) routes through `cs.begin()`.
- Invalidation rule: **any successful append clears the cached ctx**; the next operation refolds. No in-place patching of the cached fold — invalidate-and-refold is the whole contract. Appends only happen on dirty operations, which are rare and already pay a reprojection.
- Engine-level helpers used outside a changeSet (e.g. `appendLabelUpsertsLocked` for `EnsureLabels`) keep their current behavior; the memo is a changeSet concern. Where a changeSet method delegates to an engine append helper that does its own `beginAuthorLocked`, the helper is split so the changeSet path passes its memoized ctx in and the standalone path keeps beginning itself.

### 2. Batch seeding: `LabelSeedBatch` (internal/core/service.go, internal/store/label.go)

- `core.LabelService` gains `LabelSeedBatch(labels []Label, actor string) error`. Each entry carries Name/Description/Expr with `LabelSeed`'s exact per-label semantics: create when absent (description always, expr if non-empty), description-only upsert when live and the supplied non-empty description differs, expressions create-only.
- Validation before the lock, matching `LabelSeed`'s guard order: every name validated, actor validated, all names must resolve to the **same** project code (mixed-project batches are rejected), project existence and `DispatchFormat` checked once.
- Store implementation: one `WithProjectWrite`; loop `cs.SeedLabel` over the batch; stop at the first error; one `reprojectTxn` after the loop, skipped when the transaction stayed clean (existing ATM-d402aa dirty gate). An empty batch is a no-op success.

### 3. Callers: capabilities and registry (internal/capability)

- `workflow.EnsureVocabulary`, `contextmap.EnsureVocabulary`, and `workflowai.EnsureVocabulary` become a single `LabelSeedBatch(Vocabulary(code), actor)` call, then derive boards (`Expr != ""`) in vocabulary order as today. All three already loop `LabelSeed` over their `vocabulary(code)` list, and the `Capability` interface documents the invariant that `Vocabulary` declares "exactly the set EnsureVocabulary seeds", so this is a mechanical substitution.
- `Registry.EnsureVocabulary` collects `Vocabulary(code)` across enabled capabilities into **one** batch call, then derives the board union per registration order — one fold per select even for multi-capability projects. Per-capability `EnsureVocabulary` remains the standalone single-capability converge entry (CLI `capability seed`, project capability add).

## Data flow (select path, after the fix)

`'s'` handler → `Registry.EnsureVocabulary` → collect vocabularies (pure, no I/O) → `LabelSeedBatch` → one `WithProjectWrite`: `cs.begin()` folds once; each `SeedLabel` checks the memoized fold — converged labels are no-ops; a missing/differing label appends, clearing the memo so the next label refolds against the updated log → one gated reprojection → boards returned exactly as today.

Converged select: 1 fold ≈ 250ms. Genuinely dirty ensure (first-ever select of a new project — tiny log — or an edited description): 1 + one refold per appended label, each already paying reprojection anyway.

## Error handling

- Bad input (invalid name, mixed projects, missing actor) fails before any I/O.
- Mid-batch failure: seeds stop at the first error. Events appended before the failure are durable — identical to today's per-label transactions. New hazard covered: the batch runs its gated reprojection **on the error path too** whenever the transaction is dirty, so the cache never lags the log because of a failed batch. (Cache-freshness machinery would also self-heal readers; not relied upon.)
- Memo correctness: append ⇒ clear is the only invalidation needed; there is no other mutation channel while the project lock is held. A test pins that an operation after an append observes the appended state.

## Testing (TDD order)

1. eventlog unit: a fold counter (unexported, read from package tests) pins that N no-op `SeedLabel`s in one changeSet perform exactly 1 `beginAuthorLocked`; an append invalidates (count 2); a post-append operation observes the appended state.
2. store facade: `LabelSeedBatch` — no-op batch leaves the cache-row canary untouched (extends the `TestNoopLabelSeedSkipsReprojection` pattern); dirty batch appends only changed labels and reprojects once; mid-batch error still reprojects; mixed-project batch rejected; empty batch succeeds.
3. capability: workflow/contextmap ensure emit one batch call; existing board aggregation-order tests (`TestEnsureVocabularyAggregatesBoardsInRegistrationOrder` and friends) pass unchanged.
4. benchmarks/acceptance: batch-seed bench at live scale plus the select-path probe (`internal/tui/select_probe_test.go`, `ATM_BENCH_STORE` against a store copy) upgraded to register all three production capabilities (workflow, contextmap, workflowai) like cmd/atm/main.go: `EnsureVocabulary` < 400ms, from ~8.5s at the real registry's 34 seeds.

## Out of scope (recorded follow-ups)

- Engine-wide fold cache keyed by (file size, last seq) — approach C from brainstorming; would make all writers ~ms but carries cross-process invalidation and memory-retention risk.
- In-place patching of the memoized fold on append (only worth it if dirty ensures are ever felt).
- Moving `EnsureVocabulary` off the UI thread (unnecessary once the work is ~250ms).
- The 248ms first-touch `refreshSummary` (noise next to the fix; revisit only if still felt afterward).
