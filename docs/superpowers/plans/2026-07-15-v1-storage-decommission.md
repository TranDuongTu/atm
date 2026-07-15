# v1 Storage Decommission Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the v1 `log.jsonl` storage code paths and the rollback stack, leaving the v2 EventSource as the sole storage engine, plus a one-shot `atm store prune-v1` to retire the frozen v1 logs.

**Architecture:** This is a subtractive refactor. The v2 paths already exist and are covered by `internal/store/eventsource_*_test.go` and the CLI goldens. Most changes delete a v1 branch from a function that keeps its v2 branch ("surgery") or delete a v1-only helper whole. The one relocation is the v1 replay: it moves from `internal/store` into `internal/eventsource` so `UpgradeV1` (kept as the stray-v1-log import tool) stays self-verifying and `equivalence_test.go` survives without depending on store internals.

**Tech Stack:** Go, `modernc.org/sqlite` (cache.db), cobra (CLI).

Spec: `docs/superpowers/specs/2026-07-15-v1-storage-decommission-design.md`. ATM task: ATM-0127.

## Global Constraints

- Actor for any ATM ledger mutation during this work: `developer@claude:opus-4.8`.
- Markdown files (README, conventions) carry no hard wrap — prose is one un-wrapped line per paragraph.
- `eventsource` must NOT import `internal/store` (the package boundary the v1Entry redeclaration exists to preserve). The replay port in Task 1 respects this.
- The full regression net is `go test ./...` from repo root; it must be green at the end of every task. The v2 suite (`internal/store/eventsource_*_test.go`) is the specific guard that surgery did not nick a v2 path.
- `eventsource.UpgradeV1`, `atm store upgrade`, and `equivalence_test.go` are KEPT. Do not delete them.
- Commit after every task. Commit messages use the `type(ATM-0127): summary` form and end with the `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` trailer.

**A note on granularity:** for the deletion tasks below, the "implementation" is removing named symbols and their now-dead helpers; the guard is that the v2 test suite stays green. Those tasks list exact symbols and the proving command rather than reproducing hundreds of deleted lines. The additive/behavioral tasks (1, 6, 7, 8) carry real code.

---

### Task 1: Port the v1 replay + semantic compare into `internal/eventsource`; self-verify `UpgradeV1`; repoint `equivalence_test.go`

This is the load-bearing first move. After it, `UpgradeV1` proves its own output and the store↔eventsource test coupling is severed — but `store.Replay` is NOT yet deleted (Tasks 3–5 remove its remaining callers first).

**Files:**
- Create: `internal/eventsource/replay.go`
- Modify: `internal/eventsource/upgrade.go` (make `UpgradeV1` self-verifying)
- Modify: `internal/eventsource/equivalence_test.go` (repoint at the eventsource-local replay; drop the `internal/store` import)
- Reference (source to port, do NOT modify yet): `internal/store/log.go:393-529` (`Replay`, `ReplayState`, `subjectMatch`), `internal/store/eventsource_upgrade.go:181-337` (`compareV2FoldToV1Replay` and its computed-label exclusion logic)

**Interfaces:**
- Produces: `eventsource.ReplayV1(logData []byte) (*ReplayResult, error)` — a pure replay of v1 log bytes into live entity snapshots (project/tasks/comments/labels), keyed by v1 alias, mirroring `store.Replay`'s semantics. `eventsource.CompareReplayToFold(rep *ReplayResult, st *State) error` — the computed-label-aware semantic comparison, returning a wrapped `ErrIntegrity` on divergence. Both operate purely on eventsource types + the `v1Entry` shape already declared in `upgrade.go`.
- Consumes: nothing from other tasks.

- [ ] **Step 1: Write the failing self-verify test**

Add to `internal/eventsource/upgrade_test.go` (or the equivalence test file) a case asserting `UpgradeV1` folds its own output and compares clean, and that a hand-corrupted event set would fail the compare. Model the fixture on the existing upgrade tests.

```go
func TestUpgradeV1SelfVerifies(t *testing.T) {
    log := buildV1Log(t) // existing helper / inline fixture used by upgrade tests
    res, err := UpgradeV1(log)
    if err != nil {
        t.Fatalf("UpgradeV1: %v", err)
    }
    st, err := FoldEvents(res.Events)
    if err != nil {
        t.Fatalf("FoldEvents: %v", err)
    }
    rep, err := ReplayV1(log)
    if err != nil {
        t.Fatalf("ReplayV1: %v", err)
    }
    if err := CompareReplayToFold(rep, st); err != nil {
        t.Fatalf("self-verify: %v", err)
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/eventsource/ -run TestUpgradeV1SelfVerifies -v`
Expected: FAIL — `ReplayV1`/`CompareReplayToFold` undefined.

- [ ] **Step 3: Port the replay**

Create `internal/eventsource/replay.go`. Port `store.Replay`'s loop (log.go:393-482) to build a `ReplayResult` of eventsource-native snapshots (define lightweight `ReplayResult{Project, Tasks, Comments, Labels}` using the fields the compare needs: name; task alias/title/description/labels; comment alias/body/task-alias/reply-to-alias/labels; label name/description/expr). Reuse the existing `v1Entry` decode and `stringList`. Do not import `internal/store`.

- [ ] **Step 4: Port the compare**

In `replay.go`, add `CompareReplayToFold(rep *ReplayResult, st *State) error`. Port `compareV2FoldToV1Replay` (eventsource_upgrade.go:181-337) verbatim in semantics, including the `computed()` / `assertedLabels()` exclusion of boards and namespace labels (use `LabelState.IsComputed()` and the eventsource namespace check). Return `fmt.Errorf("%w: ...", ErrIntegrity, ...)` — add an `ErrIntegrity` sentinel to the eventsource package if one does not already exist.

- [ ] **Step 5: Make `UpgradeV1` self-verifying**

At the end of `UpgradeV1`, before `return res, nil`, fold `res.Events`, replay the input bytes, and compare:

```go
	st, err := FoldEvents(res.Events)
	if err != nil {
		return nil, fmt.Errorf("eventsource: upgrade: self-fold: %w", err)
	}
	rep, err := replayFrom(logData) // internal helper shared with ReplayV1
	if err != nil {
		return nil, err
	}
	if err := CompareReplayToFold(rep, st); err != nil {
		return nil, err
	}
	return res, nil
```

- [ ] **Step 6: Repoint `equivalence_test.go`**

Rewrite `TestFoldOfUpgradeMatchesReplay` to compare `ReplayV1(log)` against `FoldEvents(UpgradeV1(log).Events)` using `CompareReplayToFold`, deleting the `internal/store` import and the `s.Replay`/`replayTask`/`replayComment`/`labelNames` store adapters. Keep it as the archaeological record of v1 semantics.

- [ ] **Step 7: Run the eventsource suite**

Run: `go test ./internal/eventsource/ -v`
Expected: PASS (including `TestUpgradeV1SelfVerifies` and the repointed `TestFoldOfUpgradeMatchesReplay`).

- [ ] **Step 8: Run the full suite**

Run: `go test ./...`
Expected: PASS. `store.Replay`/`compareV2FoldToV1Replay` still exist (other callers remain); nothing else changed.

- [ ] **Step 9: Commit**

```bash
git add internal/eventsource/
git commit -m "refactor(ATM-0127): port v1 replay + semantic compare into eventsource; self-verify UpgradeV1

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Delete the rollback stack and the re-upgrade path

**Files:**
- Modify: `internal/cli/store.go` (delete `rollbackCmd`, lines 236-262, and its `AddCommand`)
- Modify: `internal/store/eventsource_upgrade.go` (delete `RollbackProjectToV1` 394-428, `rebuildProjectCacheFromV1Locked` 441-477, `RollbackReport` 24-27; remove the re-upgrade archive block 124-137 and rewrite the upgrade to use the eventsource self-verify from Task 1 instead of `compareV2FoldToV1Replay`)
- Delete/trim tests: `internal/store/eventsource_upgrade_test.go` rollback + re-upgrade cases; any `store rollback` CLI test.

**Steps:**

- [ ] **Step 1: Delete the rollback CLI command** — remove `rollbackCmd` and its `cmd.AddCommand(rollbackCmd)` from `newStoreCmd`.

- [ ] **Step 2: Delete `RollbackProjectToV1`, `rebuildProjectCacheFromV1Locked`, and `RollbackReport`.**

- [ ] **Step 3: Simplify `UpgradeProjectToV2`.** Remove the re-upgrade archive block (eventsource_upgrade.go:124-137) — with no rollback a project is never re-upgraded, so a pre-existing `events.v2.jsonl` at cutover is now an error, not a displacement: return `fmt.Errorf("%w: project %q already has a v2 file", ErrConflict, code)` if `os.Stat(s.eventsV2Path(code))` succeeds. Replace the `s.compareV2FoldToV1Replay(code, state)` call (line 119) with the eventsource self-verify path: `rep, err := eventsource.ReplayV1(raw)` then `eventsource.CompareReplayToFold(rep, state)`. (Note: `UpgradeV1` already self-verifies internally as of Task 1; this second compare guards the user's data against a disk read-back and may be dropped if judged redundant — keep it for the read-back guard.)

- [ ] **Step 4: Prune now-dead references.** `cacheClearV2Freshness` (cache.go:126) loses its only caller (`rebuildProjectCacheFromV1Locked`) — delete it too. `UpgradeReport.ArchivedPath` loses its only writer; leave the field (harmless) or drop it if no test reads it.

- [ ] **Step 5: Delete the rollback/re-upgrade tests** in `eventsource_upgrade_test.go` and any CLI rollback test.

- [ ] **Step 6: Build + test.**

Run: `go build ./... && go test ./internal/store/ ./internal/cli/ -v`
Expected: PASS. `store.Replay` is now called only by search/indexer/rebuild/verify (removed next).

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(ATM-0127): delete the rollback stack and re-upgrade path

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Prune v1 branches from the read/query surface

Removes the last `store.Replay` / `ReadLog` callers.

**Files:**
- Modify: `internal/store/search.go` (`textSearch`: delete the `Replay` arm ~138,148; keep `v2CompatEntities`)
- Modify: `internal/store/indexer.go` (delete the v1 `Replay` source ~56 and `LastLogSeq`-based v1 staleness ~131,137,220; keep the v2 `v2CompatEntities` source and the v2 freshness key)
- Modify: `internal/store/rebuild.go` (`Rebuild`: delete the v1 branch ~88-113; keep the v2 branch 48-87)
- Modify: `internal/store/verify.go` (`VerifyProject`: delete the v1 branch ~94-144; delete `checkProjectCache`/`checkTaskCache`/`checkCommentCache` 186-238; keep `checkV2Cache`, `populateAuxReports`, `extractTruncatedBytes`)
- Modify: `internal/store/log.go` (`LastLogSeq` 241-254: delete the v1 arm, keep the `v2EventCount` arm; `readLogForViews` 271-284: delete the `ReadLog` arm, keep `readV2LogEntries`)
- Modify: `internal/cli/store.go` (`logCmd`: delete the v1 `ReadLog` branch 85-108; the command now only serves the v2 display path 62-83, so drop the `ProjectFormatForCLI==v2` gate and always render v2)

**Steps:**

- [ ] **Step 1: Prune search** — delete the `Replay` arm of `textSearch`; the function keeps only `v2CompatEntities`. Run `go test ./internal/store/ -run TestSearch -v`; expected PASS.

- [ ] **Step 2: Prune indexer** — delete the v1 source and v1 staleness; the indexer reads `v2CompatEntities` and keys freshness on the v2 key only. Run `go test ./internal/store/ -run 'Index|Indexer|Vector' -v`; expected PASS.

- [ ] **Step 3: Prune `Rebuild` and `VerifyProject`** — delete their v1 branches and the three `checkXCache` helpers. Run `go test ./internal/store/ -run 'Rebuild|Verify' -v`; expected PASS.

- [ ] **Step 4: Prune `LastLogSeq` and `readLogForViews`** — each keeps only its v2 arm. `History`/`HistoryE`/`ReadLogCached` are unchanged (they call `readLogForViews`).

- [ ] **Step 5: Simplify `store log` CLI** — delete the v1 branch; always render the v2 display. Update or delete any `store log` test that fed a v1 project.

- [ ] **Step 6: Build + full test.**

Run: `go build ./... && go test ./...`
Expected: PASS. `store.Replay` / `store.ReadLog` now have no non-test callers (a `grep -rn 's.Replay\|\.ReadLog(' internal --include=*.go | grep -v _test` should be empty outside `log.go` itself).

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(ATM-0127): serve search/indexer/rebuild/verify/log from v2 only

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Prune v1 branches from the mutators

**Files:**
- Modify: `internal/store/project.go` — `CreateProject`/`SetName`/`Remove`: delete the v1 append bodies; keep the `…V2` delegates. `getProjectWithRebuild` 241-321: delete the v1 LogSeq-staleness branch 280-320, keep the v2 branch 250-278. Delete `lastProjectEventSeq` 324-336 and `rebuildProjectFromLog` 338-382.
- Modify: `internal/store/task.go` — mutators: delete v1 append bodies and the `NextTaskN` minting (51,85); keep `…V2` delegates. `getTaskWithRebuild`: delete v1 branch; delete `lastTaskEventSeq` 409-421 and `rebuildTaskFromLog` 425-455. Keep `appendLabelUpsertsLocked` only if still referenced by a v2 path; otherwise delete (its v2 mirror is `appendV2LabelUpsertsLocked`).
- Modify: `internal/store/comment.go` — `CreateComment`: delete the v1 body 40-113 including `NextCommentN` minting (55-56,86) and the `ActionTaskMetaChanged` append (89-95); keep `createCommentV2`. `getCommentWithRebuild`: delete v1 branch; delete `lastCommentEventSeq` 363-375 and `rebuildCommentFromLog` 377-407.
- Modify: `internal/store/label.go` — `LabelAdd`/`LabelSeed`/`LabelRemove`: delete v1 append bodies; keep `labelUpsertV2`/`labelSeedV2`/`labelRemoveV2`.
- Delete tests: `comment_log_test.go`; `NextTaskN`/`NextCommentN` regression cases in `project_test.go`, `task_test.go`, `rebuild_test.go`, `comment_test.go`.

**Steps:**

- [ ] **Step 1:** Prune `project.go`; run `go test ./internal/store/ -run 'Project' -v`.
- [ ] **Step 2:** Prune `task.go`; run `go test ./internal/store/ -run 'Task' -v`.
- [ ] **Step 3:** Prune `comment.go`; run `go test ./internal/store/ -run 'Comment' -v`.
- [ ] **Step 4:** Prune `label.go`; run `go test ./internal/store/ -run 'Label' -v`.
- [ ] **Step 5:** Delete the v1-pinning test cases listed above.
- [ ] **Step 6:** Build + full test.

Run: `go build ./... && go test ./...`
Expected: PASS. The mutators no longer call `appendLogLocked`; the `getXWithRebuild` closures no longer call `rebuildXFromLog`.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(ATM-0127): route all mutators through v2 only; drop v1 append bodies

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Delete the `log.go` v1 primitives and `store.Replay`

Now that nothing calls them.

**Files:**
- Modify: `internal/store/log.go` — delete `appendLog` (99-112), `appendLogLocked` (118-157), `marshalLogLine` (159-165), `ReadLog` (173-204), `detectPartialTail`/`partialTailError` (206-229), `lastLogSeqLocked`/`setLastLogSeqLocked` (292-325), `Replay` + `ReplayState` build (393-482), and `subjectMatch` (514-529) if only `Replay`-adjacent code used it. KEEP: `logPath` (87-89, needed by prune-v1), `IsIntegrity`, `LastLogSeq` (v2-only now), `readLogForViews`/`ReadLogCached`/`invalidateLogSnapshot`/log-snapshot memoizer, `History`/`HistoryE`. Remove the v1-only entries from the action enum (17-55) only if unreferenced by eventsource — the action-name constants are also used by the v2 display; check with grep before deleting any.
- Modify: `internal/store/store.go` — the `logSnapshot` machinery (29-45) STAYS (v2 uses it via `ReadLogCached`).
- Delete tests: `log_test.go`, `lastlogseq_cache_test.go`, `readlog_cached_test.go` (if it pinned v1; keep any v2 `ReadLogCached` coverage — move it to a v2 test if needed).

**Steps:**

- [ ] **Step 1:** Delete the listed symbols from `log.go`. After each deletion run `go build ./...` to catch a surviving caller.
- [ ] **Step 2:** `grep -rn 'ReplayState\|func.*Replay\b' internal/store --include=*.go | grep -v _test` — expect empty.
- [ ] **Step 3:** Delete the v1-pinning test files listed above; salvage any v2-relevant assertions first.
- [ ] **Step 4:** Build + full test.

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(ATM-0127): delete v1 log primitives and store.Replay

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: cache.db schema migration + `types.go` field cleanup

Rename the ordinal, drop the v1 counters, and version the cache so an old cache.db rebuilds instead of being read with a stale schema. Types and cache change together (one compile unit).

**Files:**
- Modify: `internal/store/types.go` — rename `Project.LogSeq`→`Ordinal`, `Task.LogSeq`→`Ordinal`, `Comment.LogSeq`→`Ordinal`, `Label.LogSeq`→`Ordinal` (json tag `ordinal,omitempty`). Delete `Project.NextTaskN` and `Task.NextCommentN`.
- Modify: `internal/store/cache.go` — new DDL (below), `PRAGMA user_version` migration, delete `lastLogSeqMetaKey`/`cacheSetLastLogSeq`/`cacheGetLastLogSeq` (81-105), update every `INSERT`/`SELECT` that names `log_seq`/`next_task_n`/`next_comment_n`, and update the scans (`cache.go:224,233,280,317,405,457,464,504,651` and the `next_comment_n` positions).
- Modify: `internal/store/eventsource_projector.go` — `taskFromV2`/`commentFromV2`/`labelFromV2`/`projectFromV2` set `Ordinal` instead of `LogSeq`; update the stale comments at 119-121.
- Modify: any other reader of these fields (`cli/output.go` handled in Task 7; grep for `.LogSeq`/`.NextTaskN`/`.NextCommentN`).

**New cache DDL** (`cacheSchema`): rename the three `log_seq` columns to `ordinal`, drop `next_task_n` from `projects`, drop `next_comment_n` from `tasks`:

```sql
CREATE TABLE IF NOT EXISTS projects (
	code TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	ordinal INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	project_code TEXT NOT NULL,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	ordinal INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL
);
-- labels.ordinal, comments.ordinal likewise; task_labels/comment_labels/meta unchanged.
```

**Migration in `cacheDB()`** — replace the ad-hoc `ALTER … swallow` blocks (cache.go:171-190+) for these columns with a version gate. Fresh DBs are `user_version=0`; bump the current schema to `2`:

```go
	const cacheSchemaVersion = 2
	var uv int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&uv); err != nil {
		s.cacheErr = err
		return
	}
	if uv < cacheSchemaVersion {
		// cache.db is derived and rebuildable: on any schema change drop the
		// derived tables and recreate at the new shape. The next read
		// re-projects each project from its events.v2.jsonl via ensureV2CacheFresh.
		for _, t := range []string{"projects", "tasks", "task_labels", "labels", "comments", "comment_labels", "meta"} {
			if _, err := db.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
				s.cacheErr = err
				return
			}
		}
		if _, err := db.Exec(cacheSchema); err != nil {
			s.cacheErr = err
			return
		}
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, cacheSchemaVersion)); err != nil {
			s.cacheErr = err
			return
		}
	}
```

(Keep the `identity`/`alias` v2 columns from cache.go:187-190 — fold them into `cacheSchema` directly now that we recreate on version bump, so the post-migration schema needs no follow-up ALTERs. Verify against the current live columns before finalizing the DDL.)

- [ ] **Step 1: Write the failing migration test**

```go
func TestCacheMigratesLegacySchema(t *testing.T) {
    dir := t.TempDir()
    // Hand-create a cache.db at the OLD schema (log_seq + next_task_n columns, user_version 0).
    seedLegacyCacheDB(t, filepath.Join(dir, "cache.db"))
    s := openStoreWithV2Project(t, dir) // a project with events.v2.jsonl on disk
    // A read that projects the cache must succeed and expose the ordinal, not log_seq.
    tasks, err := s.ListTasks("ATM")
    if err != nil {
        t.Fatalf("ListTasks after migration: %v", err)
    }
    if len(tasks) == 0 || tasks[0].Ordinal == 0 {
        t.Fatalf("expected migrated ordinal, got %+v", tasks)
    }
    // No stale last_log_seq meta row survives.
    // (assert via a direct query that meta has no 'last_log_seq:%' key)
}
```

- [ ] **Step 2:** Run it; expected FAIL (compile error on `.Ordinal`, or schema mismatch).
- [ ] **Step 3:** Apply the type rename, the new DDL, the `user_version` migration, and delete the `last_log_seq` meta helpers. Fix all `.LogSeq`/`.NextTaskN`/`.NextCommentN` references surfaced by the build.
- [ ] **Step 4:** Run the migration test; expected PASS.
- [ ] **Step 5:** Full test.

Run: `go build ./... && go test ./...`
Expected: PASS. Some goldens containing `next_task_n` will still fail — they are fixed in Task 7. If a golden test fails ONLY on `next_task_n`, that is expected here; note it and proceed (Task 7 regenerates them). Prefer to sequence so Task 7 immediately follows.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(ATM-0127): drop v1 cache columns, rename log_seq->ordinal, version cache.db

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Remove `next_task_n` from CLI output and regenerate goldens

**Files:**
- Modify: `internal/cli/output.go` — delete the `NextTaskN` field + `json:"next_task_n"` tag (46-52), its population from `p.NextTaskN` (127), `renderNextTaskN` (202-206), and its use in the project text/column render (213,219).
- Modify goldens under `internal/cli/testdata/golden/`: `project-create.json`, `project-list.json`, `project-show.json`, `project-set-name.json`, `project-remove-zero-task.json`, `determinism-project-list.json`, `determinism-project-show-code-ATM.json`, `determinism-project-show-code-DEMO.json` (verify each actually contains `next_task_n` before editing).

**Steps:**

- [ ] **Step 1:** Delete the `NextTaskN` output surface from `output.go`.
- [ ] **Step 2:** Regenerate goldens with the repo's golden-update mechanism (find it first: `grep -rn "UPDATE_GOLDEN\|-update\b\|golden" internal/cli/*_test.go | head`). Typically `go test ./internal/cli/ -run <GoldenTest> -update`.
- [ ] **Step 3:** Inspect the golden diff — it must remove ONLY the `next_task_n` key (and rename any `log_seq`→`ordinal` if a golden exposed it). Any other change is a regression to investigate.
- [ ] **Step 4:** Full test.

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(ATM-0127): drop next_task_n from CLI output; regenerate goldens

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Add `atm store prune-v1`

Retires the frozen `log.jsonl` of upgraded projects. Archives by default; `--delete` hard-removes. Refuses unless the project is v2-active and verifies clean.

**Files:**
- Create: `internal/store/prune.go` (`Store.PruneProjectV1`, `PruneReport`, `archiveLogFileLocked`)
- Modify: `internal/cli/store.go` (add `pruneCmd`)
- Create/Modify tests: `internal/store/prune_test.go`

**Interfaces:**
- Produces: `Store.PruneProjectV1(code string, del bool) (*PruneReport, error)`; `PruneReport{Project string; Pruned bool; Archived string; Deleted bool; Reason string}`.

- [ ] **Step 1: Write the failing tests**

```go
func TestPruneV1_RefusesNonV2(t *testing.T) {
    s, _ := newV1ActiveProject(t) // a project still on v1
    rep, err := s.PruneProjectV1("ATM", false)
    if err != nil { t.Fatalf("PruneProjectV1: %v", err) }
    if rep.Pruned { t.Fatalf("expected skip on a v1-active project, got %+v", rep) }
}

func TestPruneV1_ArchivesByDefault(t *testing.T) {
    s, dir := newUpgradedProject(t) // v2-active, log.jsonl still on disk
    rep, err := s.PruneProjectV1("ATM", false)
    if err != nil { t.Fatalf("PruneProjectV1: %v", err) }
    if !rep.Pruned || rep.Archived == "" { t.Fatalf("expected archive, got %+v", rep) }
    if _, err := os.Stat(filepath.Join(dir, "projects", "ATM", "log.jsonl")); !os.IsNotExist(err) {
        t.Fatalf("log.jsonl should be gone after archive")
    }
    if _, err := os.Stat(rep.Archived); err != nil { t.Fatalf("archive missing: %v", err) }
}

func TestPruneV1_DeleteRemoves(t *testing.T) {
    s, dir := newUpgradedProject(t)
    rep, err := s.PruneProjectV1("ATM", true)
    if err != nil { t.Fatalf("PruneProjectV1: %v", err) }
    if !rep.Deleted { t.Fatalf("expected delete, got %+v", rep) }
    if _, err := os.Stat(filepath.Join(dir, "projects", "ATM", "log.jsonl")); !os.IsNotExist(err) {
        t.Fatalf("log.jsonl should be gone after delete")
    }
}

func TestPruneV1_SkipsBornV2(t *testing.T) {
    s, _ := newBornV2Project(t) // no log.jsonl
    rep, err := s.PruneProjectV1("ATM", false)
    if err != nil { t.Fatalf("PruneProjectV1: %v", err) }
    if rep.Pruned { t.Fatalf("expected skip for born-v2, got %+v", rep) }
}
```

- [ ] **Step 2:** Run them; expected FAIL (`PruneProjectV1` undefined).

- [ ] **Step 3: Implement `Store.PruneProjectV1`**

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type PruneReport struct {
	Project  string `json:"project"`
	Pruned   bool   `json:"pruned"`
	Archived string `json:"archived,omitempty"`
	Deleted  bool   `json:"deleted,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// PruneProjectV1 retires an upgraded project's frozen log.jsonl. It refuses
// unless the project is v2-active and verifies clean; a v1-active or born-v2
// project is skipped (Pruned=false, Reason set). By default the log is
// archived (recoverable); del=true removes it outright.
func (s *Store) PruneProjectV1(code string, del bool) (*PruneReport, error) {
	rep := &PruneReport{Project: code}
	err := s.WithLock(code, func() error {
		f, err := s.projectFormat(code)
		if err != nil {
			return err
		}
		if f != StoreFormatV2 {
			rep.Reason = "not v2-active"
			return nil
		}
		if _, err := os.Stat(s.logPath(code)); os.IsNotExist(err) {
			rep.Reason = "born v2 (no v1 log)"
			return nil
		} else if err != nil {
			return err
		}
		// The v1 log is the only surviving pre-cutover copy; do not retire it
		// unless the live v2 cache is provably consistent with the event file.
		vr, err := s.VerifyProject(code)
		if err != nil {
			return err
		}
		if vr.Diverged || !vr.LogOK {
			return fmt.Errorf("%w: project %q does not verify clean; refusing to prune", ErrIntegrity, code)
		}
		if del {
			if err := os.Remove(s.logPath(code)); err != nil {
				return err
			}
			rep.Deleted = true
		} else {
			dst, err := s.archiveLogFileLocked(code)
			if err != nil {
				return err
			}
			rep.Archived = dst
		}
		rep.Pruned = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rep, nil
}

// archiveLogFileLocked moves log.jsonl aside under a collision-safe timestamped
// name, mirroring archiveV2FileLocked. Caller holds the project lock.
func (s *Store) archiveLogFileLocked(code string) (string, error) {
	path := s.logPath(code)
	base := filepath.Join(s.projectDir(code), fmt.Sprintf("log.pruned.%d", time.Now().UTC().Unix()))
	for n := 0; ; n++ {
		dst := base + ".jsonl"
		if n > 0 {
			dst = fmt.Sprintf("%s.%d.jsonl", base, n)
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		if err := os.Rename(path, dst); err != nil {
			_ = os.Remove(dst)
			return "", err
		}
		return dst, nil
	}
}
```

(The spec's "delete the `last_log_seq` meta row" is already satisfied storewide by Task 6's cache recreate, so prune does not touch meta.)

- [ ] **Step 4:** Run the prune tests; expected PASS.

- [ ] **Step 5: Add the CLI command** in `newStoreCmd`:

```go
	pruneCmd := &cobra.Command{
		Use:   "prune-v1",
		Short: "Retire upgraded projects' frozen v1 log.jsonl (archive by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			all, _ := cmd.Flags().GetBool("all")
			del, _ := cmd.Flags().GetBool("delete")
			if all == (project != "") {
				return fmt.Errorf("%w: pass exactly one of --project or --all", store.ErrUsage)
			}
			codes := []string{project}
			if all {
				codes, err = s.ProjectCodes()
				if err != nil {
					return err
				}
			}
			reps := make([]*store.PruneReport, 0, len(codes))
			for _, c := range codes {
				rep, err := s.PruneProjectV1(c, del)
				if err != nil {
					return err
				}
				reps = append(reps, rep)
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), reps)
			}
			for _, r := range reps {
				switch {
				case r.Deleted:
					fmt.Fprintf(st.stdout(), "pruned\t%s\tdeleted\n", r.Project)
				case r.Pruned:
					fmt.Fprintf(st.stdout(), "pruned\t%s\tarchived %s\n", r.Project, r.Archived)
				default:
					fmt.Fprintf(st.stdout(), "skipped\t%s\t%s\n", r.Project, r.Reason)
				}
			}
			return nil
		},
	}
	pruneCmd.Flags().String("project", "", "project code to prune")
	pruneCmd.Flags().Bool("all", false, "prune all eligible projects")
	pruneCmd.Flags().Bool("delete", false, "delete log.jsonl instead of archiving it")
	cmd.AddCommand(pruneCmd)
```

(Confirm the store's project-enumeration method name — `ProjectCodes` / `projectCodesOnDisk` / `ListProjects`; use the exported one the CLI already uses elsewhere.)

- [ ] **Step 6:** Build + full test.

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(ATM-0127): add 'atm store prune-v1' to retire frozen v1 logs

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Docs — drop the upgrade/rollback runbook, keep a one-line v1-import note

**Files:**
- Modify: `internal/cli/conventions.go` — remove the `log.jsonl` / `store upgrade` / `store rollback` runbook text (both the Markdown body ~23,109-111 and the JSON `where_tasks_live` string ~134). Keep one line noting `atm store upgrade` imports a stray v1 `log.jsonl`. Update the storage description to name `events.v2.jsonl` as the sole store.
- Modify: `README.md` — drop the upgrade/rollback runbook (~76,85-89); keep a one-line v1-import note. No hard wrap.
- Check: any conventions golden/test that pins the changed text — regenerate if present.

**Steps:**

- [ ] **Step 1:** Edit `conventions.go` (both copies of the text).
- [ ] **Step 2:** Edit `README.md`.
- [ ] **Step 3:** `grep -rn 'rollback\|log.jsonl\|RollbackProjectToV1\|next_task_n' README.md internal/cli/conventions.go docs/eventsource/02-storage-layout.md` — confirm only the intended one-line import note remains (docs/ design/plan files are historical and may keep their references; do not rewrite history docs).
- [ ] **Step 4:** Full test (a conventions test may assert on the text).

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "docs(ATM-0127): drop upgrade/rollback runbook; keep one-line v1-import note

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Final verification

**Steps:**

- [ ] **Step 1: Full suite + vet.**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS, no vet complaints.

- [ ] **Step 2: Dead-code sweep.** Confirm no v1 remnants linger:

Run: `grep -rn 'RollbackProjectToV1\|rebuildProjectFromLog\|appendLogLocked\|ReadLog\b\|last_log_seq\|NextTaskN\|NextCommentN\|store.Replay\|s.Replay(' internal --include=*.go | grep -v _test`
Expected: empty (or only the kept `logPath`/`History` neighbors, none of the deleted symbols).

- [ ] **Step 3: Drive the real binary via /verify.** Invoke the `verify` skill (or `run`) to build `atm` and exercise, against a temp v2 store: `project create`, `task create`/`list`, `task comment add`/`comment list`, `store log`, `search`, `history`, `store verify`, and `store prune-v1 --project <CODE>` (assert the log is archived and the store still lists/reads correctly afterward). Confirm behavior, not just a passing unit suite.

- [ ] **Step 4: Update the ledger.** Comment on ATM-0127 with the outcome (what shipped, `prune-v1` usage, the D1/D2/D3 resolutions as built), stamped `developer@claude:opus-4.8`, and move it to `status:done` once the branch is merged.

- [ ] **Step 5: Finish the branch.** Use the `superpowers:finishing-a-development-branch` skill to choose merge/PR.

---

## Self-Review

**Spec coverage:** D1 (replay relocation, self-verifying UpgradeV1, equivalence_test repointed) → Task 1. D2 (drop counters, rename log_seq→ordinal, cache version bump) → Task 6. D3 (prune-v1, archive default, refuse-unless-clean) → Task 8. "Whole deletions" (rollback stack) → Task 2; (log.go primitives) → Task 5. "Surgery" (read surface) → Task 3; (mutators) → Task 4. CLI output next_task_n → Task 7. Docs → Task 9. Testing/verify → Task 10. All spec sections map to a task.

**Placeholder scan:** additive tasks (1, 6, 7, 8) carry real code; deletion tasks name exact symbols and line ranges plus the guard test. Two deliberate "confirm before finalizing" notes remain (the cache `identity`/`alias` columns in Task 6; the project-enumeration method name in Task 8) — these require reading one current symbol at execution time rather than guessing, and are called out explicitly rather than left vague.

**Type consistency:** `Ordinal` replaces `LogSeq` uniformly across `Project`/`Task`/`Comment`/`Label` (Task 6) and the projector (`taskFromV2`/`commentFromV2`/`labelFromV2`/`projectFromV2`). `PruneReport`/`PruneProjectV1` names are consistent between Task 8's interface block, implementation, and CLI. `ReplayV1`/`CompareReplayToFold`/`ReplayResult` are consistent between Task 1's interface, the self-verify wiring, and Task 2's upgrade rewrite.
