# Born-v2 Conversion — Design

ATM task: ATM-0127 (prerequisite sub-effort) · Date: 2026-07-15 · Persona: developer

## Context

The v1 storage decommission (ATM-0127) hit a foundation gap discovered during implementation: deleting the v1 write path forces every new project to be *born v2*, and v2 mints hex, HLC-clock-derived task/comment aliases instead of sequential `ATM-0001`. The fresh-store default is still v1 (`eventsource_meta.go:65,70`), 45 test files hardcode sequential IDs, and all 26 CLI goldens are v1-shaped. Worse, born-v2 aliases are a hash over three wall-clock/random inputs (HLC, replica id, `at`), so they are non-deterministic per run — the CLI golden and determinism tests cannot pin them.

The original decommission plan treated "make the mutators v2-only" as a one-line deletion. It is not. This spec covers the missing foundation: make v2 the sole, unconditional authoring path with *reproducible* aliases, and migrate the suite. Once this lands, every remaining decommission step (read-surface pruning, log.go primitive deletion, cache migration, prune-v1, docs) is clean dead-code removal against a green, v2-native suite.

Tasks 1–2 of the decommission (replay relocation into eventsource; rollback-stack deletion) are already landed on this branch and are unaffected.

## Scope

In scope: (1) injectable determinism seams on `Store`; (2) a deterministic v2 CLI golden harness + store-test helpers; (3) flip the default to born-v2 and delete the v1 arms of `CreateProject`/`CreateTask`/`CreateComment`; (4) migrate the test/golden suite to v2. Out of scope (belongs to the remaining decommission tasks, executed after this): pruning the v1 read-surface arms, deleting the `log.go` v1 primitives / `store.Replay` / `store.ReadLog`, the cache-schema migration, `prune-v1`, and docs.

## Design decisions

### D1 — Determinism via functional options on `store.Open`

`Store` gains three unexported seam fields, each defaulting to today's production behavior:

- `clockNow func() int64` — passed to `eventsource.NewClock(...)` in `beginV2AuthorLocked` (replacing the hardcoded `NewClock(nil)`). `nil` already means wall clock inside `NewClock`, so the default field value `nil` is a behavior-identical passthrough.
- `replicaEntropy io.Reader` — passed to `MintReplicaID(...)` in `ensureReplicaForWriteLocked` (replacing the hardcoded `rand.Reader`). Defaults to `rand.Reader`.
- `nowFn func() time.Time` — backs `Store.Now()` (replacing the hardcoded `time.Now().UTC()`). Defaults to `time.Now().UTC`.

Exposed as functional options: `store.Open(root string, opts ...Option)` with `store.WithClock(func() int64)`, `store.WithReplicaEntropy(io.Reader)`, `store.WithNow(func() time.Time)`. Production callers keep calling `store.Open(root)` unchanged. The options are general-purpose (a reproducible-authoring capability), not obviously test-only, and carry no global mutable state — so parallel tests are safe.

Reproducibility contract: with all three seams fixed and deterministic content, the pre-alias event digest — SHA-256 over `{v, parents, hlc, replica, at, actor, action, subject, payload-minus-alias}` — is reproducible, so the alias (a prefix of that digest) is reproducible. This is already proven one layer down by `eventsource`'s `TestNewTaskCreatedDeterministicForSameInputs`.

### D2 — Reproducible goldens, not normalized-away IDs

The CLI harness `normalizeOutput` scrubs timestamps and store paths but NOT hex aliases. Rather than extend it to mask `CODE-<hex>` IDs (which would weaken `determinism_test` from byte-identical to structural), the golden harness opens its store with the D1 seams fixed, so v2 aliases are genuinely reproducible. Goldens are regenerated (`-update`) to contain stable hex IDs; `determinism_test`'s two fresh stores, both seeded with the same fixed seams, stay byte-identical.

Fixed-seam values for the harness: a counter clock (millis advancing +1 per tick, mirroring `eventsource`'s `testClock`), a fixed replica entropy seed (a fresh `bytes.Reader` over a constant 16-byte seed per store `Open`, so each store mints the same replica id), and a fixed `at`. Each store `Open` gets its OWN fresh `bytes.Reader` instance (an `io.Reader` is consumed) — the harness constructs the reader per store, not once shared.

### D3 — Born-v2 by deletion only; the fresh-store default stays v1

Born-v2 is made unconditional by deleting the v1 arms of the three `Create*` mutators (their `else`/fallthrough bodies), so v2 authoring no longer depends on `ActiveFormat`. `createProjectV2` already writes an explicit `ProjectFormats[code]=v2` entry before authoring, so every created project — including on a fresh store — carries an explicit v2 entry, and `CreateTask`/`CreateComment` dispatch off that entry. Nothing born this way relies on the store default.

The fresh-store default is deliberately NOT flipped. `readStoreMeta`'s v1 fallback is the conservative "an entry-less, never-declared project is legacy v1" signal; flipping it to v2 would make a genuinely-v1 entry-less project silently misread as v2 media (the exact corruption `eventsource_meta.go`'s comment and the `SetActiveFormat(v2)` guard warn against). After decommission no such project exists in practice (all real projects carry explicit v2 entries), so the default is never load-bearing — leaving it v1 is strictly safer and costs nothing. The `SetActiveFormat(v2)` guard is likewise retained unchanged.

This deletes: `Project.NextTaskN`/`Task.NextCommentN` minting in the v1 Create bodies and the `ActionTaskMetaChanged` companion append. It does NOT yet delete the v1 read-surface arms, the `log.go` primitives, or `store.Replay`/`store.ReadLog` — those still have callers (the read arms) and are removed by the later decommission tasks. They become dead (no v1 projects can be created) but remain compiling.

### D4 — Test migration: dynamic-capture first, then flip

To avoid one giant red-until-done commit, migration is staged so most of it lands while v1 is still the default and stays green:

1. Tests that hardcode a sequential ID they merely need a *handle* to (`CreateTask(...)` then reference `ATM-0001`) are rewritten to capture the returned `tk.ID` dynamically. This is green on v1 (`tk.ID == "ATM-0001"`) AND green on v2 (`tk.ID == "ATM-<hex>"`), decoupling the bulk of the churn from the flip.
2. Tests that assert v1-specific *behavior* that cannot exist under v2 (sequential-ID minting semantics, `NextTaskN`/`NextCommentN`, v1 log-tail/seq-gap mechanics) are deleted — but only those whose subject is the v1 authoring path this sub-effort removes. v1 read-path tests whose subject (ReadLog/Replay) is deleted by a LATER task are left for that task.
3. The flip itself (D3) lands with: the default flip, the v1 Create-arm deletions, deletion of the v1-authoring tests from step 2 that can no longer construct their fixtures, and golden regeneration. The suite is green at the flip commit.

## Components / files

- `internal/store/store.go` — `Store` seam fields; `Option` type + `WithClock`/`WithReplicaEntropy`/`WithNow`; `Open(root, opts...)`; `Now()` backed by `nowFn`.
- `internal/store/eventsource_author.go` — `beginV2AuthorLocked` uses `NewClock(s.clockNow)`; author `at` uses `s.Now()`.
- `internal/store/eventsource_replica.go` — `ensureReplicaForWriteLocked` uses `s.replicaEntropy`.
- `internal/store/eventsource_meta.go` — unchanged (fresh-store default stays v1; `SetActiveFormat` guard retained). Listed only to record it is deliberately untouched.
- `internal/store/task.go`/`comment.go`/`project.go` — delete the v1 `Create*` arms (keep the `…V2` bodies); drop `NextTaskN`/`NextCommentN` minting + the `ActionTaskMetaChanged` companion append.
- `internal/cli/harness_test.go` — golden harness opens store with fixed seams.
- Test suite + `internal/cli/testdata/golden/*` — migrated per D4.

## Testing strategy

- After the seams land (D1), the full suite must be green with production defaults unchanged — the seams are a no-op until a test uses them.
- New unit test: two stores opened with identical fixed seams + identical CLI seed produce byte-identical output including aliases (the reproducibility contract). This is effectively `determinism_test` regenerated for v2.
- The flip commit's gate: full `go test ./...` green, goldens regenerated and inspected to differ ONLY in the expected ways (sequential→hex IDs, no `next_task_n` churn yet — that is a later task).
- End with a real-binary drive: `atm project create` / `task create` on a fresh store now yields a v2 project (hex IDs, `events.v2.jsonl`, no `log.jsonl`).

## Risks

- **The flip is a global switch** — the moment `CreateTask` is v2-only, every test authoring v1 data breaks. Mitigated by D4's dynamic-capture pre-migration, which moves most churn off the flip commit; the residual flip commit is large but bounded to the v1-authoring tests + goldens.
- **Golden non-determinism** — if any authoring nondeterminism source is missed, goldens flake. Mitigated by the D2 reproducibility unit test, which must pass before goldens are regenerated.
- **Accidentally changing production identity** — the seams must default to today's exact behavior (wall clock, `rand.Reader`, `time.Now().UTC()`). A test asserting production `Open(root)` still mints a random replica and stamps wall time guards this.
- **Scope creep into later tasks** — this sub-effort must not delete the v1 read arms / log primitives / Replay; doing so breaks the build (their callers remain). The fence is: delete only the v1 *authoring* arms and *v1-authoring* tests.
