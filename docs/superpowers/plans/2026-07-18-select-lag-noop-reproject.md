# TUI Select Lag Fix (ATM-d402aa) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the ~15s freeze when selecting a project in the TUI by (1) skipping cache reprojection for write transactions that appended nothing, and (2) running each reprojection's cache rewrite in a single SQLite transaction.

**Architecture:** The regression (commit `a7f01d6`, ATM-3b873c) made `labelSeedV2` run `reprojectTxn` unconditionally, so the TUI's per-select `EnsureVocabulary` (4 idempotent board seeds) pays 4 full event-file folds + 4 full cache rewrites (~1,500 rows each, one implicit fsync'd SQLite transaction per row) — measured 15.4s on the live store (1,660 events / 2.3 MB, 156 tasks, 514 comments). Fix 1 adds a `Dirty()` flag to `core.ChangeSet` (set by the engine's changeSet on every successful append) and gates `reprojectTxn` on it — restoring the pre-carve no-op-seed semantics for every mutator at once. Fix 2 converts the per-row cache helpers to take a shared execer and wraps `projectSnapshotDB`'s delete+reinsert in one `db.Begin()`/`Commit()`, cutting every *real* mutation's reprojection from ~3.7s to roughly one fsync.

**Tech Stack:** Go, `database/sql` + SQLite (`modernc.org/sqlite` driver, WAL, `MaxOpenConns(1)`), event-sourced store in `internal/store/eventlog`.

## Global Constraints

- Working branch: `worktree-atm-d402aa-select-lag` in worktree `.claude/worktrees/atm-d402aa-select-lag` (already created; baseline `make verify` green).
- Commit message style follows repo history: `<type>(ATM-d402aa): <summary>` (e.g. `fix(ATM-d402aa): …`, `test(ATM-d402aa): …`).
- `make verify` must be green before the branch is declared done (it runs both Go modules + script tests; store/tui suites take several minutes).
- ATM ledger: stamp every ATM mutation with actor `developer@claude:fable-5`; record progress on task `ATM-d402aa` (`atm task comment add --task ATM-d402aa --actor developer@claude:fable-5 --label ATM:comment:progress --body "…"`) after each task lands.
- Do NOT touch `~/.config/atm` (the live store). Manual perf verification uses a fresh copy under the session scratchpad.
- Architecture boundaries are pinned by `tests/arch/imports_test.go`; nothing in this plan adds imports across the seam (core gains one interface method, no new imports).
- The store facade's tests live in `package store` (internal access, `newTestStore(t)` + `testActor` helpers); engine tests live in `package eventlog` (`testEngine(t)` helper).

---

### Task 1: ChangeSet dirty tracking in the engine

`core.ChangeSet` gains `Dirty() bool`; the engine's `changeSet` sets an unexported flag on every successful append. `EnsureLabels` appends 0..n events, so its engine primitive `appendLabelUpsertsLocked` must report how many events it appended.

**Files:**
- Modify: `internal/core/repository.go` (add `Dirty() bool` to the `ChangeSet` interface, ~line 113)
- Modify: `internal/store/eventlog/changeset.go` (add `dirty` field + set it in every appending method)
- Modify: `internal/store/eventlog/author.go:174` (`appendLabelUpsertsLocked` returns `(int, error)`)
- Test: `internal/store/eventlog/changeset_dirty_test.go` (create)

**Interfaces:**
- Consumes: existing `Engine.WithProjectBirth` / `Engine.WithProjectWrite` / `changeSet` methods (unchanged signatures except `appendLabelUpsertsLocked`).
- Produces: `core.ChangeSet.Dirty() bool` — reports whether this transaction recorded at least one change. Task 2 gates `reprojectTxn` on exactly this method.

- [ ] **Step 1: Write the failing test**

Create `internal/store/eventlog/changeset_dirty_test.go`:

```go
package eventlog

import (
	"testing"

	"atm/internal/core"
)

// TestChangeSetDirty pins the Dirty contract Task 2's reprojection gate
// relies on: a transaction is clean until an event is actually appended;
// idempotent no-ops (SeedLabel on a live label, EnsureLabels with only live
// names) leave it clean.
func TestChangeSetDirty(t *testing.T) {
	e := testEngine(t)
	if err := e.WithProjectBirth("ATM", func() error { return nil }, func(cs core.ChangeSet) error {
		if cs.Dirty() {
			t.Error("fresh birth changeSet reports dirty before any append")
		}
		if err := cs.CreateProject("Acme Task Manager", "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("CreateProject appended the root event but Dirty() is false")
		}
		return nil
	}); err != nil {
		t.Fatalf("WithProjectBirth: %v", err)
	}

	// Seed a label so the no-op paths below have a live target.
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		if cs.Dirty() {
			t.Error("fresh write changeSet reports dirty before any append")
		}
		if err := cs.SeedLabel("ATM:open-tasks", "open work", "status:open", "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("SeedLabel of an absent label appended but Dirty() is false")
		}
		return nil
	}); err != nil {
		t.Fatalf("seed txn: %v", err)
	}

	// The regression case: no-op paths must stay clean.
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		if err := cs.SeedLabel("ATM:open-tasks", "different desc", "", "developer@claude:test"); err != nil {
			return err
		}
		if cs.Dirty() {
			t.Error("SeedLabel of a live label is a no-op but Dirty() is true")
		}
		if err := cs.EnsureLabels([]string{"ATM:open-tasks"}, "developer@claude:test"); err != nil {
			return err
		}
		if cs.Dirty() {
			t.Error("EnsureLabels with only live names is a no-op but Dirty() is true")
		}
		if err := cs.EnsureLabels([]string{"ATM:brand-new"}, "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("EnsureLabels registered a new label but Dirty() is false")
		}
		return nil
	}); err != nil {
		t.Fatalf("no-op txn: %v", err)
	}

	// A dirty flag never leaks across transactions.
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		if cs.Dirty() {
			t.Error("new changeSet inherited dirty state from a previous transaction")
		}
		if err := cs.UpsertLabel("ATM:x", core.LabelFields{}, "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("UpsertLabel appended but Dirty() is false")
		}
		return nil
	}); err != nil {
		t.Fatalf("upsert txn: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/eventlog -run TestChangeSetDirty -v`
Expected: FAIL to compile with `cs.Dirty undefined (type core.ChangeSet has no field or method Dirty)`

- [ ] **Step 3: Implement dirty tracking**

In `internal/core/repository.go`, inside the `ChangeSet` interface, after `HasLiveTasks() (bool, error)` (before `Snapshot()`):

```go
	// Dirty reports whether this transaction has recorded at least one
	// change. Idempotent verbs (SeedLabel on a live label, EnsureLabels
	// with only live names) leave it false; the facade uses it to skip
	// read-model projection for transactions that changed nothing.
	Dirty() bool
```

In `internal/store/eventlog/author.go`, change `appendLabelUpsertsLocked` to count appends (its only caller is `changeSet.EnsureLabels`):

```go
// appendLabelUpsertsLocked auto-registers any label name a task/comment
// mutation asserts but the fold does not already hold live, returning how
// many label.upserted events it appended (0 when every name was live). The
// payload carries NO fields — label.upserted writes the existence slot
// unconditionally (writesOf), so an empty payload registers the label
// without clobbering a description/expr some other replica may have set.
// Caller MUST hold the project lock.
func (e *Engine) appendLabelUpsertsLocked(code string, labels []string, actor string) (int, error) {
	if len(labels) == 0 {
		return 0, nil
	}
	ctx, err := e.beginAuthorLocked(code)
	if err != nil {
		return 0, err
	}
	appended := 0
	for _, name := range labels {
		if l, ok := ctx.state.Labels[name]; ok && !l.Tombstoned {
			continue
		}
		if _, err := e.appendLocked(code, draft{
			Actor:   actor,
			Action:  actionLabelUpserted,
			Subject: eventsource.Subject{Kind: "label", Name: name},
			Payload: map[string]any{},
		}); err != nil {
			return appended, err
		}
		appended++
	}
	return appended, nil
}
```

In `internal/store/eventlog/changeset.go`:

1. Add the field:

```go
type changeSet struct {
	e             *Engine
	code          string
	rootCommitted bool
	dirty         bool
}
```

2. Add the accessor (next to `Snapshot` at the bottom of the file):

```go
// Dirty reports whether this transaction appended at least one event.
// Idempotent no-ops (SeedLabel on a live label, EnsureLabels with only live
// names) leave it false — the facade's reprojection gate keys off this.
func (cs *changeSet) Dirty() bool { return cs.dirty }
```

3. Set it in every appending method. Every successful append path flips it:

- `CreateProject`: after `commitAuthorLocked` succeeds, alongside `cs.rootCommitted = true`, add `cs.dirty = true`.
- `SetProjectName`: change the tail to

```go
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  actionProjectNameChanged,
		Subject: eventsource.Subject{Kind: "project", ID: ref, Code: cs.code},
		Payload: map[string]any{"name": name},
	})
	if err == nil {
		cs.dirty = true
	}
	return err
```

- `CreateTask`:

```go
func (cs *changeSet) CreateTask(d core.TaskDraft, actor string) (string, error) {
	_, alias, err := cs.e.appendTaskCreatedLocked(cs.code, d.Title, d.Description, d.Labels, actor)
	if err == nil {
		cs.dirty = true
	}
	return alias, err
}
```

- `mutateTask` (covers SetTaskTitle/SetTaskDescription/AddTaskLabel/RemoveTaskLabel/RemoveTask): change the tail to

```go
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  action,
		Subject: eventsource.Subject{Kind: "task", ID: ref},
		Payload: payload,
	})
	if err == nil {
		cs.dirty = true
	}
	return err
```

- `CreateComment`: same shape as `CreateTask` (set `cs.dirty = true` when `appendCommentCreatedLocked` returns nil error).
- `mutateComment` (covers SetCommentBody/AddCommentLabel/RemoveCommentLabel/RemoveComment): same tail change as `mutateTask`.
- `UpsertLabel`: change the tail to

```go
	_, err := cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  actionLabelUpserted,
		Subject: eventsource.Subject{Kind: "label", Name: name},
		Payload: payload,
	})
	if err == nil {
		cs.dirty = true
	}
	return err
```

- `SeedLabel`: only the append branch flips it (the live-label early return stays clean):

```go
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  actionLabelUpserted,
		Subject: eventsource.Subject{Kind: "label", Name: name},
		Payload: payload,
	})
	if err == nil {
		cs.dirty = true
	}
	return err
```

- `EnsureLabels`:

```go
func (cs *changeSet) EnsureLabels(names []string, actor string) error {
	appended, err := cs.e.appendLabelUpsertsLocked(cs.code, names, actor)
	if appended > 0 {
		cs.dirty = true
	}
	return err
}
```

(Note: `appended > 0` even when `err != nil` — a partial multi-label registration did append events, and a later retry inside the same txn shape must still project them if the caller swallows the error. Today no caller does, but the flag must never under-report.)

- `RemoveLabel`: same tail change as `UpsertLabel`.

Guard-only methods (`RequireProject`, `ResolveTask`, `ResolveComment`, `TaskHasLabel`, `CommentHasLabel`, `HasLiveTasks`, `Snapshot`, `ForgetProject`) never touch `dirty`. (`ForgetProject` mutates store.json, not the event file — the facade deletes the read-model rows itself on that path, so projection is irrelevant there.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/eventlog -run TestChangeSetDirty -v`
Expected: PASS

Run: `go build ./... && go test ./internal/store/eventlog ./internal/core`
Expected: all PASS (the `appendLabelUpsertsLocked` signature change has exactly one caller, `changeSet.EnsureLabels`).

- [ ] **Step 5: Commit**

```bash
git add internal/core/repository.go internal/store/eventlog/changeset.go internal/store/eventlog/author.go internal/store/eventlog/changeset_dirty_test.go
git commit -m "feat(ATM-d402aa): ChangeSet.Dirty — engine tracks whether a txn appended"
```

---

### Task 2: Gate reprojection on Dirty; restore no-op-seed semantics

`reprojectTxn` returns early when the transaction appended nothing. This restores the exact pre-carve `labelSeedV2` semantics (no-op seed → no fold, no cache rewrite) for every facade mutator at once, and fixes the stale doc comments the investigation flagged.

**Files:**
- Modify: `internal/store/cache_project.go:65` (`reprojectTxn` gains the gate)
- Modify: `internal/store/label.go:173-180` (rewrite the `labelSeedV2` doc comment that rationalized the unconditional reprojection)
- Modify: `internal/store/log.go:63-75` (fix the stale "O(1) from cache.db" claim in `ReadLogCached`'s comment)
- Test: `internal/store/reproject_skip_test.go` (create)

**Interfaces:**
- Consumes: `core.ChangeSet.Dirty() bool` from Task 1.
- Produces: `reprojectTxn(code string, cs core.ChangeSet) error` (signature unchanged) that is a no-op for clean transactions. Task 4's benchmarks assume no-op `LabelSeed` skips both `Snapshot()` and the cache rewrite.

**Safety audit (why the gate is correct for every caller):** all eleven `reprojectTxn` call sites (`label.go:169,186,200`, `task.go:85,126,279`, `comment.go:89,127,320`, `project.go:76,230`) reach it only after their appends succeed, except `labelSeedV2`, whose `SeedLabel` may be a clean no-op — the regression case. Callers with their own no-op guards (e.g. `taskLabelAddV2`'s `TaskHasLabel` check) return nil *before* `reprojectTxn`, so the gate changes nothing for them. Skipping projection for a clean txn cannot leave the cache behind the event file: a clean txn by definition did not advance it. The only behavior lost is reprojection-as-side-effect healing of an *already*-stale cache, which is exactly the pre-carve posture — staleness healing belongs to `ensureV2CacheFresh` (reads) and stays intact.

- [ ] **Step 1: Write the failing test**

Create `internal/store/reproject_skip_test.go`:

```go
package store

import "testing"

// TestNoopLabelSeedSkipsReprojection pins the fix for ATM-d402aa: a LabelSeed
// of an already-live label (the TUI's per-select EnsureVocabulary path) must
// not rewrite the project's cache rows. The canary is a row mutated directly
// in cache.db: a reprojection would restore it from the fold, a skipped
// reprojection leaves it alone.
func TestNoopLabelSeedSkipsReprojection(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme Task Manager", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	task, err := s.CreateTask("ATM", "real title", "", nil, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.LabelSeed("ATM:open-tasks", "open work", "status:open", testActor); err != nil {
		t.Fatalf("first LabelSeed: %v", err)
	}

	db, err := s.cacheDB()
	if err != nil {
		t.Fatalf("cacheDB: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET title = 'CANARY' WHERE id = ?`, task.ID); err != nil {
		t.Fatalf("plant canary: %v", err)
	}

	// No-op seed: the label is live, so the txn is clean and the canary
	// must survive (no cache rewrite).
	if err := s.LabelSeed("ATM:open-tasks", "different desc", "", testActor); err != nil {
		t.Fatalf("no-op LabelSeed: %v", err)
	}
	got, ok, err := cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after no-op seed: ok=%v err=%v", ok, err)
	}
	if got.Title != "CANARY" {
		t.Fatalf("no-op LabelSeed rewrote the cache (title %q, want CANARY)", got.Title)
	}

	// Dirty seed: a NEW label appends, so reprojection must run and restore
	// the canary row from the fold.
	if err := s.LabelSeed("ATM:in-progress-tasks", "wip", "status:in-progress", testActor); err != nil {
		t.Fatalf("dirty LabelSeed: %v", err)
	}
	got, ok, err = cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after dirty seed: ok=%v err=%v", ok, err)
	}
	if got.Title != "real title" {
		t.Fatalf("dirty LabelSeed did not reproject (title %q, want %q)", got.Title, "real title")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store -run TestNoopLabelSeedSkipsReprojection -v`
Expected: FAIL at "no-op LabelSeed rewrote the cache (title \"real title\", want CANARY)" — current code reprojects unconditionally.

(Signature verified against `internal/core/service.go`: `CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error)`.)

- [ ] **Step 3: Implement the gate**

In `internal/store/cache_project.go`, change `reprojectTxn`:

```go
// reprojectTxn is the in-transaction projection every mutator ends with — the
// old reprojectV2Locked, split across the seam: the engine folds (cs.Snapshot
// re-reads the file strictly, including this transaction's own writes), the
// facade projects. A CLEAN transaction (no appends — e.g. SeedLabel of an
// already-live label) skips both the fold and the rewrite: the event file did
// not advance, so the cache cannot be behind this txn. That skip is what keeps
// the TUI's per-select EnsureVocabulary (4 idempotent seeds) from paying 4
// full reprojections (ATM-d402aa); healing a cache that was ALREADY stale
// belongs to ensureV2CacheFresh on the read side, not here.
func (s *Store) reprojectTxn(code string, cs core.ChangeSet) error {
	if !cs.Dirty() {
		return nil
	}
	snap, err := cs.Snapshot()
	if err != nil {
		return err
	}
	return s.projectSnapshot(code, snap)
}
```

In `internal/store/label.go`, replace the `labelSeedV2` doc comment (lines 173-180, the block rationalizing the unconditional reprojection) with:

```go
// labelSeedV2 is LabelSeed's v2 body. cs.SeedLabel is itself the no-op guard:
// it folds under the lock and appends nothing when the label is already live,
// so it carries the exact begin/observe sequence (and therefore the exact HLC
// trajectory) the pre-carve labelSeedV2 had. When it appends nothing the txn
// stays clean and reprojectTxn skips entirely — the pre-carve early-return
// semantics (ATM-d402aa restored them after the carve briefly reprojected
// unconditionally, freezing the TUI on every project select).
```

In `internal/store/log.go`, fix `ReadLogCached`'s stale comment (lines 63-75). Replace the sentence claiming `LastLogSeq` is "now O(1) from cache.db" so the block reads:

```go
// ReadLogCached returns the project's log entries, memoizing the parsed
// result in memory for the Store's lifetime. The snapshot is invalidated
// whenever the cached snapshot's builtSeq falls behind LastLogSeq: a local
// append bumps the v2 event count immediately, and a remote process's append
// bumps it the same way, so one freshness check covers both cases without a
// separate local-invalidation call. LastLogSeq is NOT free — the v2
// ChangeCount reads the whole event file to count committed lines — but it is
// parse-free and far cheaper than re-folding, which is what the memo avoids.
// UpgradeProjectToV2 is the one write path that still calls
// invalidateLogSnapshot directly, since an upgrade replaces the project's
// format/media rather than advancing its event count.
//
// This keeps the TUI's per-frame renderSummary path from re-parsing
// log.jsonl on every keystroke while staying fresh under external mutation.
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store -run TestNoopLabelSeedSkipsReprojection -v`
Expected: PASS

Run: `go test ./internal/store -count=1 2>&1 | tail -5`
Expected: `ok atm/internal/store` (full facade suite still green — this is the slow suite, several minutes).

- [ ] **Step 5: Commit**

```bash
git add internal/store/cache_project.go internal/store/label.go internal/store/log.go internal/store/reproject_skip_test.go
git commit -m "fix(ATM-d402aa): skip reprojection for clean txns — no-op LabelSeed no longer rewrites the cache"
```

---

### Task 3: Single-transaction cache projection

`projectSnapshotDB` currently issues every delete/upsert as its own implicitly-committed statement (and `cacheUpsertTask`/`cacheUpsertComment` even open a private `db.Begin()` per row) — ~1,500 fsync'd commits per reprojection at live scale. Wrap the whole rewrite in one transaction. This also makes a crash mid-projection atomic instead of leaving a half-rewritten project (today's per-row commits can; the freshness row is written last so a torn state is at least detected as stale, but atomicity is strictly better).

**Files:**
- Modify: `internal/store/cache.go` (add `sqlExecer`; convert `cacheSetV2Freshness`, `cacheUpsertProject`, `cacheUpsertTask`, `cacheUpsertLabel`, `cacheUpsertComment` to take it; `cacheUpsertTask`/`cacheUpsertComment` drop their private Begin/Commit)
- Modify: `internal/store/cache_project.go` (`projectSnapshotDB` wraps the rewrite in one `db.Begin()`; `cacheDeleteProjectRows` takes the execer)
- Test: existing suites (`internal/store` exercises projection heavily); perf pinned by Task 4's benchmarks

**Interfaces:**
- Consumes: `projectSnapshotDB(db *sql.DB, code string, snap *core.ProjectSnapshot) error` — signature unchanged for its callers (`projectSnapshot`, the cacheDB migration's `reprojectAllV2`).
- Produces: `type sqlExecer interface { Exec(query string, args ...any) (sql.Result, error) }` in `internal/store/cache.go`, satisfied by both `*sql.DB` and `*sql.Tx`. Row helpers keep their names with `db *sql.DB` replaced by `x sqlExecer`. Existing `*sql.DB` call sites (`rebuild.go:90`, `project.go:292`, tests like `cache_batched_test.go`) compile unchanged.

- [ ] **Step 1: Add the execer and convert the row helpers**

In `internal/store/cache.go`, above `cacheSetV2Freshness`:

```go
// sqlExecer is the write surface the per-row cache helpers run on: *sql.DB
// for standalone point writes, *sql.Tx when a caller batches a whole
// project rewrite into one transaction (projectSnapshotDB — one fsync
// instead of one per row).
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}
```

Convert signatures (bodies keep their SQL verbatim):

- `func cacheSetV2Freshness(x sqlExecer, code string, eventCount int) error` — replace `db.Exec` with `x.Exec`.
- `func cacheUpsertProject(x sqlExecer, p *Project) error` — same.
- `func cacheUpsertLabel(x sqlExecer, l Label) error` — same.
- `func cacheUpsertTask(x sqlExecer, t *Task) error` — delete the `tx, err := db.Begin()` / `defer tx.Rollback()` / `return tx.Commit()` wrapper; run its three statements (insert task, delete task_labels, insert task_labels) directly on `x.Exec` in the same order, returning the first error.
- `func cacheUpsertComment(x sqlExecer, c *Comment) error` — same treatment as `cacheUpsertTask`.

In `internal/store/cache_project.go`:

- `func cacheDeleteProjectRows(x sqlExecer, code string) error` — replace `db.Exec` with `x.Exec` (both statements: the five-table loop and the labels LIKE-sweep).
- Rewrite `projectSnapshotDB` to run everything in one transaction:

```go
// projectSnapshotDB is projectSnapshot's DB-taking core. It does NOT call
// s.cacheDB(), so it is safe from inside cacheDB()'s cacheOnce.Do (the schema
// migration's eager reprojection) as well as from ordinary callers.
//
// The whole rewrite — delete, upserts, freshness row — runs in ONE SQLite
// transaction: a live-scale project is ~1,500 rows, and per-row implicit
// commits made each reprojection pay ~1,500 fsyncs (seconds of wall clock,
// ATM-d402aa). One transaction is one commit, and it also makes a crash
// mid-projection atomic instead of leaving a half-rewritten project.
//
// It preserves the row-level results cacheProjectFromV2StateDB produced: the
// upserts are independent rows keyed by id/name, so their relative order does
// not matter; the only order-sensitive part is delete-before-upsert per table,
// which is preserved (cacheDeleteProjectRows first, RemovedLabels deletes
// before label upserts).
func (s *Store) projectSnapshotDB(db *sql.DB, code string, snap *core.ProjectSnapshot) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := cacheDeleteProjectRows(tx, code); err != nil {
		return err
	}
	if snap.Project != nil {
		if err := cacheUpsertProject(tx, snap.Project); err != nil {
			return err
		}
	}
	for _, t := range snap.Tasks {
		if err := cacheUpsertTask(tx, t); err != nil {
			return err
		}
	}
	for _, c := range snap.Comments {
		if err := cacheUpsertComment(tx, c); err != nil {
			return err
		}
	}
	for _, name := range snap.RemovedLabels {
		if _, err := tx.Exec(`DELETE FROM labels WHERE name = ?`, name); err != nil {
			return err
		}
	}
	for _, l := range snap.Labels {
		if err := cacheUpsertLabel(tx, l); err != nil {
			return err
		}
	}
	if err := cacheSetV2Freshness(tx, code, snap.ChangeCount); err != nil {
		return err
	}
	return tx.Commit()
}
```

Deadlock check (`MaxOpenConns(1)` means a second connection request while the tx is open would block forever): between `db.Begin()` and `tx.Commit()` above, every statement goes through `tx` — nothing touches `db` or `s.cacheDB()`. Verify no helper called inside the loop reaches back to `*sql.DB` (after the conversion, none does).

- [ ] **Step 2: Build and grep for missed callers**

Run: `go build ./... && grep -rn "cacheUpsertTask(db\|cacheUpsertComment(db" internal/store --include="*.go" | grep -v _test`
Expected: clean build; remaining `(db, …)` call sites (rebuild.go, project.go, tests) compile because `*sql.DB` satisfies `sqlExecer`.

- [ ] **Step 3: Run the store suite**

Run: `go test ./internal/store -count=1 2>&1 | tail -3`
Expected: `ok atm/internal/store` — and noticeably faster than the 203s baseline (every store test that mutates now pays one commit per mutation instead of hundreds).

- [ ] **Step 4: Commit**

```bash
git add internal/store/cache.go internal/store/cache_project.go
git commit -m "perf(ATM-d402aa): project the cache rewrite in one SQLite transaction"
```

---

### Task 4: Benchmarks, full verification, ledger close-out

Pin the fixed paths with benchmarks at roughly live scale, run `make verify`, and measure the real select path against a fresh copy of the live store.

**Files:**
- Create: `internal/store/bench_reproject_test.go`
- Modify: none

**Interfaces:**
- Consumes: `Store.LabelSeed` (no-op path, Tasks 1-2), `Store.SetTaskTitle`-shaped mutators via `s.CreateTask`/`s.CreateComment` (Task 3's one-txn projection).
- Produces: benchmark numbers recorded on ATM-d402aa; no code consumed by later tasks.

- [ ] **Step 1: Write the benchmarks**

Create `internal/store/bench_reproject_test.go`:

```go
package store

import (
	"fmt"
	"testing"
)

// benchStore seeds a store at roughly the live ATM ledger's scale
// (ATM-d402aa: 156 tasks, 514 comments, 1660 events) so the reprojection
// benchmarks measure the regression's actual regime, not a toy. It returns
// the store plus one existing task alias for the mutation benchmark (aliases
// are engine-minted — never assume a literal like "ATM-0001").
func benchStore(b *testing.B) (*Store, string) {
	b.Helper()
	s, err := Open(b.TempDir())
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		b.Fatalf("Init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Acme Task Manager", testActor); err != nil {
		b.Fatalf("CreateProject: %v", err)
	}
	if err := s.LabelSeed("ATM:open-tasks", "open work", "status:open", testActor); err != nil {
		b.Fatalf("LabelSeed: %v", err)
	}
	var taskID string
	for i := 0; i < 150; i++ {
		task, err := s.CreateTask("ATM", fmt.Sprintf("task %03d", i), "", []string{"ATM:status:open"}, testActor)
		if err != nil {
			b.Fatalf("CreateTask %d: %v", i, err)
		}
		if taskID == "" {
			taskID = task.ID
		}
		for j := 0; j < 3; j++ {
			if _, err := s.CreateComment(task.ID, fmt.Sprintf("comment %d on %s", j, task.ID), nil, "", testActor); err != nil {
				b.Fatalf("CreateComment %d/%d: %v", i, j, err)
			}
		}
	}
	return s, taskID
}

// BenchmarkNoopLabelSeed is the TUI project-select path (EnsureVocabulary
// runs 4 of these per select). Before ATM-d402aa's fix each iteration paid a
// full fold + full cache rewrite (~3.8s on the live store); after, it is one
// begin-fold with no projection.
func BenchmarkNoopLabelSeed(b *testing.B) {
	s, _ := benchStore(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := s.LabelSeed("ATM:open-tasks", "open work", "status:open", testActor); err != nil {
			b.Fatalf("LabelSeed: %v", err)
		}
	}
}

// BenchmarkMutationReproject is one real mutation end-to-end (append + fold +
// one-transaction cache rewrite) — the path Task 3 collapsed from ~1,500
// implicit commits to one.
func BenchmarkMutationReproject(b *testing.B) {
	s, taskID := benchStore(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := s.SetTitle(taskID, fmt.Sprintf("title %d", i), testActor); err != nil {
			b.Fatalf("SetTitle: %v", err)
		}
	}
}
```

(Signatures verified against `internal/core/service.go`: `CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error)`, `CreateComment(taskID, body string, labels []string, replyTo, actor string) (*Comment, error)`, `SetTitle(id, title, actor string) error`.)

- [ ] **Step 2: Run the benchmarks and record numbers**

Run: `go test ./internal/store -bench 'BenchmarkNoopLabelSeed|BenchmarkMutationReproject' -benchtime 5x -run XXX -v`
Expected: both complete; `BenchmarkNoopLabelSeed` in single-digit milliseconds per op (fold of a ~600-event file, no projection), `BenchmarkMutationReproject` well under 100ms per op. Record the output for the ledger comment.

- [ ] **Step 3: Full suite**

Run: `make verify 2>&1 | tail -15`
Expected: all green. Note the `internal/store` and `internal/tui` package times against the baseline (203s / 341s) — Task 3 should cut both sharply; record them.

- [ ] **Step 4: End-to-end measurement on a live-scale store copy**

The select path must be re-measured against real data (fresh copy each time; NEVER the live store):

```bash
SCRATCH=/tmp/claude-1000/-home-ttran-projects-scyllas-atm/035217f2-5d75-4840-892f-0285e454664c/scratchpad
rm -rf "$SCRATCH/store-verify" && cp -r ~/.config/atm "$SCRATCH/store-verify"
```

Re-create the investigation probe (git history of the main worktree does not have it — it was never committed; write it fresh) as `internal/store/zz_lag_probe_test.go`:

```go
package store_test

import (
	"os"
	"testing"
	"time"

	"atm/internal/capability/workflow"
	"atm/internal/core"
	"atm/internal/store"
)

func TestLagProbe(t *testing.T) {
	root := os.Getenv("ATM_LAG_PROBE_STORE")
	if root == "" {
		t.Skip("ATM_LAG_PROBE_STORE not set")
	}
	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = s.ListTasks(core.QueryFilters{Project: "ATM"})
	start := time.Now()
	if err := workflow.EnsureVocabulary(s, "ATM", "developer@claude:probe"); err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	t.Logf("EnsureVocabulary (select path): %s", time.Since(start))
}
```

Run: `ATM_LAG_PROBE_STORE="$SCRATCH/store-verify" go test ./internal/store -run TestLagProbe -v`
Expected: EnsureVocabulary under ~600ms (baseline before fix: 15.4s). The residual is 4 × beginAuthorLocked fold (~90ms each on 1,660 events) — acceptable; if further reduction is ever wanted, batching the 4 seeds into one write txn is the recorded follow-up, out of scope here.

Then delete the probe (it must not land):

```bash
rm internal/store/zz_lag_probe_test.go && rm -rf "$SCRATCH/store-verify"
```

- [ ] **Step 5: Commit benchmarks and update the ledger**

```bash
git add internal/store/bench_reproject_test.go
git commit -m "test(ATM-d402aa): reprojection benchmarks at live-ledger scale"
```

Record results on the ledger (fill in the measured numbers):

```bash
atm task comment add --task ATM-d402aa --actor "developer@claude:fable-5" --label "ATM:comment:progress" --body "Fix landed on worktree-atm-d402aa-select-lag: (1) core.ChangeSet.Dirty + reprojectTxn gate — clean txns (no-op LabelSeed) skip fold+rewrite entirely; (2) projectSnapshotDB rewrites the cache in ONE SQLite transaction. Measured on a live-store copy: EnsureVocabulary select path <measured> (was 15.4s); benchmarks: no-op seed <measured>/op, real mutation <measured>/op; make verify green (store suite <measured>, was 203s)."
```

- [ ] **Step 6: Final review gate**

Use superpowers:requesting-code-review against the branch diff (`git diff main...HEAD`), then superpowers:finishing-a-development-branch to decide merge/PR. The TUI verification (actually driving the app and selecting a project) belongs here — use the `verify` skill / `run` skill before merging.

---

## Explicitly Not Doing (recorded for the reviewer)

- **Batching EnsureVocabulary's 4 seeds into one write txn** (would drop the residual ~400ms to ~100ms): needs a new `core.LabelService` batch method; recorded on ATM-d402aa as a follow-up candidate, YAGNI until the residual is felt.
- **Memoizing `beginAuthorLocked`'s fold within a changeSet**: deeper engine surgery ("two-begin shape" is load-bearing per changeset.go comments); same follow-up bucket.
- **Cache-based pre-check before seeding** (skip the fold when cache says boards exist): violates the carve's "the fold, not cache.db, is the authority" principle for write decisions.
- **`ChangeCount` O(1) via eventsource meta**: at 0.4ms/call on the live store it is not the lag; the stale comment is fixed instead (Task 2).

## Self-Review Notes

- Spec coverage: Fix 1 → Tasks 1-2; Fix 2 → Task 3; evidence/verification → Task 4; stale-comment cleanup folded into Task 2. The two agreed fix directions are both covered.
- Type consistency: `Dirty() bool` (core interface, Task 1) is what `reprojectTxn` calls (Task 2); `sqlExecer` (Task 3) is satisfied by `*sql.DB`/`*sql.Tx` so untouched callers compile; `appendLabelUpsertsLocked (int, error)` has exactly one caller, updated in the same task.
- Service signatures used in test/benchmark code (`CreateTask`, `CreateComment`, `SetTitle`) verified against `internal/core/service.go` during planning; task aliases are engine-minted, so the mutation benchmark threads the real alias from `benchStore` instead of assuming a literal.
