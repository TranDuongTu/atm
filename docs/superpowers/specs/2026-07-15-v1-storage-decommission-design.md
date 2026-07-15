# v1 Storage Decommission — Design

ATM task: ATM-0127 · Date: 2026-07-15 · Persona: developer

## Context

The v1→v2 storage migration (ATM-0107, L3) has landed: every log-derived view (history, activity, search, embedding indexer) reads the v2 EventSource, new projects are born v2, and every real project is upgraded and running on v2. The only deliberate reason v1 code survived L3 was the rollback safety net. On 2026-07-15 the human made the product decision to give up that net — no rollback, ever — and declared the stability soak passed. This spec covers the terminal task of the migration: delete the v1 `log.jsonl` storage code paths and reverse the rollback design decisions.

This is a subtractive change. The v2 paths already exist and are covered by `internal/store/eventsource_*_test.go` and the CLI goldens. The risk is not building something new; it is nicking a shared v2 path while excising the v1 branch tangled next to it.

## Scope

Full v1 decommission. In scope: delete the rollback stack, delete all v1 `log.jsonl` read/write code paths, add `atm store prune-v1`, migrate the `cache.db` schema, regenerate goldens, update docs. Out of scope: the v2 EventSource itself, and `eventsource.UpgradeV1` — which is kept as the one-shot import tool for stray old v1 logs.

## Design decisions (resolved 2026-07-15)

### D1 — the v1 replay parser moves into the eventsource package

`Store.Replay` (the v1 `log.jsonl` → in-memory replay) is deleted from the store. A self-contained v1 replay is kept inside `internal/eventsource`, so that:

- `eventsource.UpgradeV1` remains self-verifying: at import time it folds the v2 events it just produced and compares against a fresh v1 replay before writing them. A rare, high-stakes import path should prove its own correctness.
- `equivalence_test.go` (the archaeological record of v1 semantics that ATM-0127 says must stay) is repointed at the eventsource-local replay, severing the current `store`↔`eventsource` test coupling.

The store's `compareV2FoldToV1Replay` is deleted; its verification responsibility relocates into `UpgradeV1` (or an eventsource helper it calls).

### D2 — drop the v1 cache columns; rename the ordinal

The `cache.db` schema loses `next_task_n`, `next_comment_n`, and `log_seq`. Reconciliation of the two things that could mean:

- `next_task_n` / `next_comment_n` are pure v1 counters with no v2 meaning — deleted outright, along with the `Project.NextTaskN` and `Task.NextCommentN` Go fields.
- The `log_seq` column does NOT hold a v1 log sequence on a v2 project — the projector (`eventsource_projector.go:52-58`) already computes an in-memory creation **ordinal** during the fold and stores it here; the column name is a v1 leftover. So we do not need a new ordinal source and v2 list ordering is undisturbed. We rename the column `log_seq` → `ordinal` and the Go fields `Project.LogSeq` / `Task.LogSeq` / `Comment.LogSeq` / `Label.LogSeq` → `Ordinal`, still fed from the projector. The end state: the name `log_seq` appears nowhere.

`cache.db` is a rebuildable cache. The migration is a cache-schema-version bump that forces a rebuild-from-events on next open — not an in-place `ALTER` dance. The `last_log_seq` meta helpers (`cacheSetLastLogSeq` / `cacheGetLastLogSeq` / `lastLogSeqMetaKey`) are deleted; the v2 freshness helpers (`cacheSetV2Freshness` / `cacheGetV2Freshness` / `v2FreshnessMetaKey`) stay.

### D3 — `atm store prune-v1` archives by default

New verb: `atm store prune-v1 [--project <CODE> | --all]`. It refuses unless the target project is v2-active and passes `VerifyProject` clean. Then, by default, it archives `log.jsonl` (moves it aside under the existing archive-naming convention used by `archiveV2FileLocked`, so the bytes are recoverable off the hot path) and deletes the project's `last_log_seq` meta row. A `--delete` flag hard-removes `log.jsonl` instead of archiving. `prune-v1` never touches a born-v2 project (it has no `log.jsonl`) — it reports and skips.

## What gets deleted vs surgically pruned

Two edit shapes, from the code map. "Surgery" means delete the v1 branch of a function that also has a v2 branch we keep.

### Whole deletions

- Rollback stack: `store rollback` CLI command (`internal/cli/store.go`), `Store.RollbackProjectToV1`, `rebuildProjectCacheFromV1Locked`, `RollbackReport` (`eventsource_upgrade.go`).
- Re-upgrade support: the `archiveV2FileLocked("reupgrade")` displacement path inside `UpgradeProjectToV2` and its rationale comments. With no rollback, a project is either never-upgraded v1 or upgraded v2; re-running upgrade on a v2 project refuses.
- `store.Replay` and `compareV2FoldToV1Replay` (see D1; replay logic relocated to eventsource).
- `log.go` v1 primitives: `appendLog`, `appendLogLocked`, `marshalLogLine`, `ReadLog`, `detectPartialTail` / `partialTailError`, `lastLogSeqLocked` / `setLastLogSeqLocked`, `logPath`, the v1-only `Replay`/`ReplayState`, and the v1-only action-enum surface no longer referenced after the mutators are pruned.
- Per-entity v1 helpers: `lastProjectEventSeq` / `lastTaskEventSeq` / `lastCommentEventSeq`, `rebuildProjectFromLog` / `rebuildTaskFromLog` / `rebuildCommentFromLog`.
- `verify.go` v1 checks: `checkProjectCache` / `checkTaskCache` / `checkCommentCache` (LogSeq-comparison cache checks). Keep `checkV2Cache`, `populateAuxReports`, `extractTruncatedBytes`.
- `cache.go`: the `last_log_seq` meta trio.
- v1-pinning tests, deleted with the code they pin: `log_test.go`, `lastlogseq_cache_test.go`, `readlog_cached_test.go`, `comment_log_test.go`, and the `NextTaskN` regression cases in `project_test.go` / `rebuild_test.go` / `comment_test.go`.

### Surgery (delete v1 branch, keep v2)

- Every mutator in `project.go` / `task.go` / `comment.go` / `label.go`: drop the v1 append body, keep the `…V2` delegate. This removes `appendLogLocked` call sites, `NextTaskN` minting (`task.go`), and `NextCommentN` minting plus its `ActionTaskMetaChanged` companion append (`comment.go`).
- The three `getProjectWithRebuild` / `getTaskWithRebuild` / `getCommentWithRebuild`: keep the v2 freshness branch, drop the v1 LogSeq-staleness branch.
- `LastLogSeq`: keep the v2 arm (`v2EventCount`), delete the v1 arm.
- `readLogForViews`: keep the v2 dispatch (`readV2LogEntries`), delete the v1 `ReadLog` arm. `ReadLogCached` / `History` / `HistoryE` / the log-snapshot memoizer stay — they are format-agnostic and now serve only v2.
- `Rebuild` and `VerifyProject`: keep the v2 branch, delete the v1 (`Replay`-based) branch.
- `search.textSearch`: keep the `v2CompatEntities` arm, delete the `Replay` arm.
- `indexer`: keep the v2 (`v2CompatEntities`) source, delete the v1 (`Replay`) source; freshness keyed on the v2 key only.
- `store log` CLI: keep the v2 branch, delete the v1 `ReadLog` branch.

### CLI / output / docs

- `output.go`: remove the `NextTaskN` field and its `json:"next_task_n"` tag, `renderNextTaskN`, and the project-list column. Regenerate the affected goldens under `internal/cli/testdata/golden/` (`project-create`, `project-list`, `project-show`, `project-set-name`, `project-remove-zero-task`, `determinism-project-list`, `determinism-project-show-code-ATM`, `determinism-project-show-code-DEMO` — verify each for the `next_task_n` key before editing).
- `conventions.go`: drop the `log.jsonl` / `store upgrade` / `store rollback` runbook text (both the Markdown body and the JSON `where_tasks_live` string), keep a one-line v1-import note.
- `README.md`: drop the upgrade/rollback runbook (around lines 76, 85-89); keep a one-line v1-import note.

## Sequencing

Ordered so the tree compiles and the suite passes at every step:

1. **Relocate v1 replay.** Add a self-contained v1 replay to `internal/eventsource`; make `UpgradeV1` self-verifying; repoint `equivalence_test.go`. Delete `store.Replay` + `compareV2FoldToV1Replay`. (Everything else still compiles because the remaining `Replay` callers are removed in later steps — so this step actually lands together with step 3's search/indexer/rebuild/verify surgery to avoid a broken intermediate. Grouped in the plan.)
2. **Delete the rollback stack** (CLI + `RollbackProjectToV1` + `rebuildProjectCacheFromV1Locked` + re-upgrade path).
3. **Prune v1 branches** from mutators, the three `getXWithRebuild`, `LastLogSeq`, `readLogForViews`, `Rebuild`, `VerifyProject`, `search`, `indexer`, `store log`.
4. **Delete `log.go` v1 primitives** and the per-entity v1 helpers now unreferenced.
5. **cache.db migration**: rename `log_seq` → `ordinal`, drop `next_task_n` / `next_comment_n`, delete the `last_log_seq` meta helpers, bump the cache schema version.
6. **types.go**: drop `NextTaskN` / `NextCommentN`, rename `LogSeq` → `Ordinal`.
7. **CLI output + `prune-v1`**: remove `next_task_n` from JSON and the list column; add the `prune-v1` verb; regenerate goldens.
8. **Docs**: `conventions.go` + `README.md`.
9. **Verify**: full `go test ./...`, then drive real `atm` commands against a v2 store (create/list/comment/history/search/verify/prune-v1) via the /verify skill.

Steps 1 and 3 are interdependent through `Replay` and will be planned as one coherent unit; the numbering above is the reading order, not a hard commit boundary.

## Testing strategy

- v2 behavior is already covered by `eventsource_*_test.go` and the goldens — these are the regression net that proves the surgery didn't nick a v2 path. They must stay green throughout.
- v1-pinning tests are deleted with their code (listed above). `equivalence_test.go` survives, repointed at the eventsource-local replay (D1).
- New coverage: `prune-v1` — refuses on a v1-active project, refuses when verify is dirty, archives by default, `--delete` removes, deletes the `last_log_seq` meta row, and is a no-op skip on a born-v2 project.
- Final gate: `/verify` drive against a real v2 store, exercising the user-facing flows, not just the unit suite.

## Risks

- **Nicking a shared v2 path during surgery.** Mitigated by the existing v2 test suite staying green after each pruning step and by pruning one file at a time.
- **cache-schema bump.** Because `cache.db` rebuilds from events, a version bump is safe; the risk is forgetting to bump it, leaving a stale-schema cache. The migration step must bump the version and a test must confirm an old-schema cache is rebuilt, not read.
- **`Replay` relocation correctness.** The eventsource-local replay must be semantically identical to the deleted `store.Replay`; `equivalence_test.go` is the guard, and it must be repointed and passing before `store.Replay` is deleted.
