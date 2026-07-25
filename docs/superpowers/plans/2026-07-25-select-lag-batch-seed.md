# Select-Lag Fix (ATM-40faff): changeSet Fold Memoization + Batch Vocabulary Seeding — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A converged project select in the TUI folds the event log exactly once (~250ms on the live store) instead of 34 times (~8.5s), by memoizing the fold inside a changeSet and seeding each capability's vocabulary in one batch transaction.

**Architecture:** Two engine/facade-layer changes, no TUI changes. (1) `changeSet` caches its `authorCtx` after the first `beginAuthorLocked` and every append path invalidates the cache. (2) A new `core.LabelService.LabelSeedBatch` runs all of a vocabulary's seeds in one `WithProjectWrite`; the three capabilities and the registry call it instead of per-label `LabelSeed`. Spec: `docs/superpowers/specs/2026-07-25-select-lag-batch-seed-design.md`.

**Tech Stack:** Go, SQLite cache projection, event-sourced JSONL log (`atm/libs/eventsource`), Bubble Tea TUI. Test framework: standard `go test`.

## Global Constraints

- Work in the worktree `/home/ttran/projects/scyllas/atm/.claude/worktrees/atm-40faff-select-lag` (branch `worktree-atm-40faff-select-lag`). Never `cd` out of it.
- NEVER point a dev build or test at the real store `~/.config/atm` — a schema-changing build against it breaks the installed binary. Benchmarks/probes use a fresh copy: `cp -r ~/.config/atm "$SCRATCH/storecopy"` and `ATM_BENCH_STORE=$SCRATCH/storecopy`, where `SCRATCH=/tmp/claude-1000/-home-ttran-projects-scyllas-atm/8370a0a9-319b-4785-9364-7e1694228fec/scratchpad`.
- Markdown prose is written as single un-wrapped lines (no hard wrapping).
- ATM ledger mutations (Task 5 journaling) stamp actor `developer@claude:fable-5`.
- Behavior invariants that must not change: `SeedLabel` per-label semantics (create when absent, description-only upsert when live and non-empty description differs, expressions create-only), the reprojection dirty gate (clean transactions never rewrite the cache — ATM-d402aa), board aggregation order (registration order, vocabulary order within a capability).
- Tasks run SEQUENTIALLY in this one worktree — never dispatch two implementers editing overlapping files at once.
- Every commit message ends with:

```
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0176egiw71gKAeKPoskym5wo
```

---

### Task 1: changeSet fold memoization (eventlog)

Every `changeSet` method today calls `beginAuthorLocked` — a full strict read + fold of `events.v2.jsonl` — on every invocation. A changeSet holds the project lock for its whole life, so the file cannot change under it except through its own appends: memoize the fold, invalidate on append.

**Files:**
- Modify: `internal/store/eventlog/engine.go:42-53` (Engine struct — add fold counter)
- Modify: `internal/store/eventlog/author.go:57` (beginAuthorLocked — increment counter)
- Modify: `internal/store/eventlog/changeset.go` (memo + call-site rewrite)
- Test: `internal/store/eventlog/changeset_memo_test.go` (new)

**Interfaces:**
- Consumes: existing `testEngine(t)` scaffold (see `changeset_dirty_test.go`), `Engine.ChangeCount(code)`.
- Produces: unexported `cs.begin() (*authorCtx, error)`, `cs.invalidate()`, `cs.append(d draft) (*eventsource.Event, error)`; `Engine.beginFolds atomic.Int64` (unexported, read by package tests). Later tasks rely only on the unchanged public behavior of `core.ChangeSet`.

- [ ] **Step 1: Add the fold counter (test instrumentation)**

In `internal/store/eventlog/engine.go`, add to the `Engine` struct (after the `counts` field) and add `"sync/atomic"` to the imports:

```go
	// beginFolds counts beginAuthorLocked calls — i.e. full strict
	// read+fold passes over the event file. Test instrumentation for the
	// changeSet fold memo (ATM-40faff); not part of any contract.
	beginFolds atomic.Int64
```

In `internal/store/eventlog/author.go`, make the first line of `beginAuthorLocked` (before `e.ReadV2File`):

```go
	e.beginFolds.Add(1)
```

- [ ] **Step 2: Write the failing test**

Create `internal/store/eventlog/changeset_memo_test.go`:

```go
package eventlog

import (
	"testing"

	"atm/internal/core"
)

// TestChangeSetMemoizesFold pins the ATM-40faff contract: within one
// changeSet, operations reuse the first beginAuthorLocked fold; any
// successful append invalidates the memo; the operation after an append
// observes the appended state through a fresh fold.
func TestChangeSetMemoizesFold(t *testing.T) {
	e := testEngine(t)
	if err := e.WithProjectBirth("ATM", func() error { return nil }, func(cs core.ChangeSet) error {
		return cs.CreateProject("Acme Task Manager", "developer@claude:test")
	}); err != nil {
		t.Fatalf("WithProjectBirth: %v", err)
	}
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		return cs.SeedLabel("ATM:open-tasks", "open work", "status:open", "developer@claude:test")
	}); err != nil {
		t.Fatalf("seed txn: %v", err)
	}

	// Five no-op seeds in ONE transaction fold exactly once.
	before := e.beginFolds.Load()
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		for i := 0; i < 5; i++ {
			if err := cs.SeedLabel("ATM:open-tasks", "open work", "", "developer@claude:test"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("no-op txn: %v", err)
	}
	if got := e.beginFolds.Load() - before; got != 1 {
		t.Errorf("5 no-op SeedLabels folded %d times, want 1", got)
	}

	// An append invalidates the memo: a re-seed of the JUST-appended label
	// must see it live (fresh fold) and append nothing — the event count
	// grows by exactly 1 across the transaction.
	countBefore, err := e.ChangeCount("ATM")
	if err != nil {
		t.Fatalf("ChangeCount: %v", err)
	}
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		if err := cs.SeedLabel("ATM:fresh-board", "fresh", "status:open", "developer@claude:test"); err != nil {
			return err
		}
		if err := cs.SeedLabel("ATM:fresh-board", "fresh", "", "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("dirty seed did not mark the transaction dirty")
		}
		return nil
	}); err != nil {
		t.Fatalf("dirty txn: %v", err)
	}
	countAfter, err := e.ChangeCount("ATM")
	if err != nil {
		t.Fatalf("ChangeCount: %v", err)
	}
	if countAfter-countBefore != 1 {
		t.Errorf("dirty seed + no-op re-seed appended %d events, want 1 (stale memo would double-append)", countAfter-countBefore)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails for the right reason**

Run: `go test ./internal/store/eventlog -run TestChangeSetMemoizesFold -v`
Expected: FAIL with `5 no-op SeedLabels folded 5 times, want 1` (the event-count assertion already passes today because each SeedLabel refolds; only the memo assertion fails).

- [ ] **Step 4: Implement the memo in changeset.go**

Add the field and helpers. The struct (`internal/store/eventlog/changeset.go:10-15`) becomes:

```go
type changeSet struct {
	e             *Engine
	code          string
	rootCommitted bool
	dirty         bool
	// ctx memoizes beginAuthorLocked's fold for the life of this
	// transaction (ATM-40faff). Safe because the changeSet holds the
	// project lock: the event file cannot change except through this
	// transaction's own appends, and every append path invalidates.
	ctx *authorCtx
}
```

Add below the struct:

```go
// begin returns the memoized authorCtx, folding at most once per
// transaction until an append invalidates.
func (cs *changeSet) begin() (*authorCtx, error) {
	if cs.ctx != nil {
		return cs.ctx, nil
	}
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return nil, err
	}
	cs.ctx = ctx
	return ctx, nil
}

// invalidate drops the memoized fold. Called after EVERY path that may
// have appended to the event file, success or error — refolding is always
// safe, serving a stale fold never is.
func (cs *changeSet) invalidate() { cs.ctx = nil }

// append routes a changeSet append through the engine and drops the memo.
// appendLocked still performs its own beginAuthorLocked (an extra fold on
// dirty operations — unchanged from before the memo); the memo's win is
// the no-op path, which never reaches here.
func (cs *changeSet) append(d draft) (*eventsource.Event, error) {
	defer cs.invalidate()
	return cs.e.appendLocked(cs.code, d)
}
```

Then rewrite the call sites, all within `internal/store/eventlog/changeset.go`:

1. Replace every `ctx, err := cs.e.beginAuthorLocked(cs.code)` with `ctx, err := cs.begin()`. Occurrences (13): `CreateProject`, `SetProjectName`, `capabilityEvent`, `mutateTask`, `mutateComment`, `SeedLabel`, `RemoveLabel`, `RequireProject`, `ResolveTask`, `ResolveComment`, `TaskHasLabel`, `CommentHasLabel`, `HasLiveTasks`.
2. Replace every `cs.e.appendLocked(cs.code, draft{...})` with `cs.append(draft{...})`. Occurrences (7): `SetProjectName`, `capabilityEvent`, `mutateTask`, `mutateComment`, `UpsertLabel`, `SeedLabel` (the create branch), `RemoveLabel`.
3. `CreateProject` commits via `commitAuthorLocked` directly: insert `cs.invalidate()` immediately after the `if err := cs.e.commitAuthorLocked(cs.code, ev); err != nil { return err }` block (i.e. before `cs.rootCommitted = true`), and also in the error branch — simplest is to change the block to:

```go
	err = cs.e.commitAuthorLocked(cs.code, ev)
	cs.invalidate()
	if err != nil {
		return err
	}
```

4. `CreateTask`, `CreateComment`, and `EnsureLabels` delegate to engine helpers that append internally (`appendTaskCreatedLocked`, `appendCommentCreatedLocked`, `appendLabelUpsertsLocked`): add `defer cs.invalidate()` as the first line of each of the three methods.

- [ ] **Step 5: Run the test and the full eventlog package**

Run: `go test ./internal/store/eventlog -v -run 'TestChangeSetMemoizesFold|TestChangeSetDirty'` then `go test ./internal/store/eventlog`
Expected: PASS (both new and existing dirty-contract tests).

- [ ] **Step 6: Run the store facade package (heaviest consumer of changeSet)**

Run: `go test ./internal/store`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/store/eventlog/engine.go internal/store/eventlog/author.go internal/store/eventlog/changeset.go internal/store/eventlog/changeset_memo_test.go
git commit -m "perf(ATM-40faff): memoize the changeSet fold, invalidate on append"
```

---

### Task 2: `LabelSeedBatch` on the store facade

One `WithProjectWrite` for a whole vocabulary: with Task 1's memo, N converged seeds cost one fold. The batch also reprojects on the mid-batch error path so the cache never lags durable appends.

**Files:**
- Modify: `internal/core/service.go:52-59` (LabelService interface)
- Modify: `internal/store/label.go` (implementation, after `LabelSeed` at line 119)
- Modify: `internal/capability/workflow/vocabulary_test.go:19-31` (recordingLabelService gains a batch interceptor)
- Test: `internal/store/label_seed_batch_test.go` (new)

**Interfaces:**
- Consumes: `cs.SeedLabel(name, description, expr, actor)` (unchanged), `s.reprojectTxn(code, cs)` (dirty-gated, `internal/store/cache_project.go:84`), guards `ValidateLabelName`, `s.validateActor`, `s.labelProjectExists`, `s.eng.DispatchFormat`, `labelProject(name)` (`internal/store/label.go:289`).
- Produces: `LabelSeedBatch(labels []core.Label, actor string) error` on `core.LabelService` and `*store.Store` — single-project batches only, `core.ErrUsage` on mixed projects, nil on empty batch. Tasks 3 and 4 call exactly this signature.

- [ ] **Step 1: Write the failing facade test**

Create `internal/store/label_seed_batch_test.go` (package `store`; `newTestStore`, `testActor`, `cacheGetTask` already exist in the package — see `reproject_skip_test.go` for the canary pattern):

```go
package store

import (
	"errors"
	"testing"

	"atm/internal/core"
)

func TestLabelSeedBatch(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme Task Manager", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	task, err := s.CreateTask("ATM", "real title", "", nil, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Dirty batch: creates both labels with LabelSeed's exact semantics.
	batch := []core.Label{
		{Name: "ATM:status:open", Description: "open work"},
		{Name: "ATM:open-tasks", Description: "open board", Expr: "status:open"},
	}
	if err := s.LabelSeedBatch(batch, testActor); err != nil {
		t.Fatalf("dirty batch: %v", err)
	}
	board, err := s.LabelShow("ATM:open-tasks")
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if board.Description != "open board" || board.Expr != "status:open" {
		t.Errorf("board = %+v, want seeded description and expr", board)
	}

	// No-op batch: the transaction stays clean, so the cache is not
	// rewritten — a planted canary row survives (ATM-d402aa gate).
	db, err := s.cacheDB()
	if err != nil {
		t.Fatalf("cacheDB: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET title = 'CANARY' WHERE id = ?`, task.ID); err != nil {
		t.Fatalf("plant canary: %v", err)
	}
	if err := s.LabelSeedBatch(batch, testActor); err != nil {
		t.Fatalf("no-op batch: %v", err)
	}
	got, ok, err := cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after no-op batch: ok=%v err=%v", ok, err)
	}
	if got.Title != "CANARY" {
		t.Errorf("no-op batch rewrote the cache (title %q, want CANARY)", got.Title)
	}

	// A dirty batch (changed description) reprojects: the canary is healed.
	dirty := []core.Label{{Name: "ATM:status:open", Description: "updated open work"}}
	if err := s.LabelSeedBatch(dirty, testActor); err != nil {
		t.Fatalf("re-dirty batch: %v", err)
	}
	got, ok, err = cacheGetTask(db, task.ID)
	if err != nil || !ok {
		t.Fatalf("cacheGetTask after dirty batch: ok=%v err=%v", ok, err)
	}
	if got.Title != "real title" {
		t.Errorf("dirty batch did not reproject (title %q, want %q)", got.Title, "real title")
	}

	// Mixed-project batches are rejected before any I/O.
	mixed := []core.Label{
		{Name: "ATM:status:open", Description: "x"},
		{Name: "OTHER:status:open", Description: "x"},
	}
	if err := s.LabelSeedBatch(mixed, testActor); !errors.Is(err, core.ErrUsage) {
		t.Errorf("mixed-project batch err = %v, want ErrUsage", err)
	}

	// Empty batch is a no-op success.
	if err := s.LabelSeedBatch(nil, testActor); err != nil {
		t.Errorf("empty batch err = %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store -run TestLabelSeedBatch -v`
Expected: compile FAIL — `s.LabelSeedBatch undefined`.

- [ ] **Step 3: Implement**

In `internal/core/service.go`, add to `LabelService` directly under the `LabelSeed` line:

```go
	// LabelSeedBatch converges a whole vocabulary in ONE write transaction
	// (one event-log fold for a converged batch — ATM-40faff), with
	// LabelSeed's exact per-label semantics. All labels must belong to one
	// project (ErrUsage otherwise); an empty batch is a no-op.
	LabelSeedBatch(labels []Label, actor string) error
```

In `internal/store/label.go`, add after `LabelSeed` (line 134):

```go
// LabelSeedBatch is LabelSeed over a whole vocabulary in one transaction:
// same guards, same per-label convergence semantics, one fold for a
// converged batch and one gated reprojection at the end. The reprojection
// also runs on the mid-batch error path — earlier appends are durable and
// the cache must not lag the log.
func (s *Store) LabelSeedBatch(labels []core.Label, actor string) error {
	if len(labels) == 0 {
		return nil
	}
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code := labelProject(labels[0].Name)
	for _, l := range labels {
		if err := ValidateLabelName(l.Name); err != nil {
			return err
		}
		if lc := labelProject(l.Name); lc != code {
			return fmt.Errorf("%w: label batch spans projects %q and %q", core.ErrUsage, code, lc)
		}
	}
	if err := s.labelProjectExists(labels[0].Name); err != nil {
		return err
	}
	if _, err := s.eng.DispatchFormat(code); err != nil {
		return err
	}
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		var seedErr error
		for _, l := range labels {
			if seedErr = cs.SeedLabel(l.Name, l.Description, l.Expr, actor); seedErr != nil {
				break
			}
		}
		if err := s.reprojectTxn(code, cs); err != nil && seedErr == nil {
			return err
		}
		return seedErr
	})
}
```

(The mid-batch error reprojection is deliberately untested: `cs.SeedLabel` only errors on I/O faults that need injection machinery this codebase doesn't have; the behavior is pinned by the code shape above and the review gate.)

In `internal/capability/workflow/vocabulary_test.go`, add to `recordingLabelService` (below its `LabelSeed` method) so recording keeps working when Task 3 switches EnsureVocabulary to the batch call:

```go
func (r *recordingLabelService) LabelSeedBatch(labels []core.Label, actor string) error {
	for _, l := range labels {
		r.seedCalls = append(r.seedCalls, labelSeedCall{l.Name, l.Description, l.Expr, actor})
	}
	return r.LabelService.LabelSeedBatch(labels, actor)
}
```

- [ ] **Step 4: Run the test and both affected packages**

Run: `go test ./internal/store -run TestLabelSeedBatch -v` then `go test ./internal/store ./internal/core/... ./internal/capability/...`
Expected: PASS everywhere (`core.LabelService` implementers all embed or are `*store.Store`; the compiler surfaces any missed implementer — fix by delegation, not by removing the method).

- [ ] **Step 5: Commit**

```bash
git add internal/core/service.go internal/store/label.go internal/store/label_seed_batch_test.go internal/capability/workflow/vocabulary_test.go
git commit -m "feat(ATM-40faff): LabelSeedBatch — one transaction per vocabulary"
```

---

### Task 3: Capabilities seed via one batch call

`workflow`, `contextmap`, and `workflowai` each loop `s.LabelSeed` per label today. Each becomes exactly one `LabelSeedBatch(vocabulary(code), actor)` call. The `Capability` interface documents that `Vocabulary` declares "exactly the set EnsureVocabulary seeds", so the substitution is mechanical.

**Files:**
- Modify: `internal/capability/workflow/vocabulary.go:89-100`
- Modify: `internal/capability/contextmap/vocabulary.go:66-78`
- Modify: `internal/capability/workflowai/vocabulary.go:84-95`
- Test: `internal/capability/workflow/vocabulary_test.go` (new assertion)

**Interfaces:**
- Consumes: `LabelSeedBatch(labels []core.Label, actor string) error` from Task 2.
- Produces: unchanged signatures `EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error)` in all three packages; unchanged returned boards (Expr != "" in vocabulary order).

- [ ] **Step 1: Write the failing call-shape test (workflow package, representative)**

Add to `internal/capability/workflow/vocabulary_test.go`:

```go
// batchCountingService proves EnsureVocabulary issues ONE batch call and
// zero per-label calls (ATM-40faff: one fold per ensure).
type batchCountingService struct {
	core.LabelService
	batches int
	singles int
}

func (b *batchCountingService) LabelSeed(name, description, expr, actor string) error {
	b.singles++
	return b.LabelService.LabelSeed(name, description, expr, actor)
}

func (b *batchCountingService) LabelSeedBatch(labels []core.Label, actor string) error {
	b.batches++
	return b.LabelService.LabelSeedBatch(labels, actor)
}

func TestEnsureVocabularySeedsInOneBatch(t *testing.T) {
	s := newTestStore(t)
	svc := &batchCountingService{LabelService: s}
	if _, err := EnsureVocabulary(svc, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if svc.batches != 1 || svc.singles != 0 {
		t.Errorf("EnsureVocabulary made %d batch / %d single seed calls, want 1 / 0", svc.batches, svc.singles)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/capability/workflow -run TestEnsureVocabularySeedsInOneBatch -v`
Expected: FAIL with `0 batch / 13 single seed calls, want 1 / 0`.

- [ ] **Step 3: Implement in all three capability packages**

Replace the body of `EnsureVocabulary` in `internal/capability/workflow/vocabulary.go` (keep the existing doc comment, append one sentence: "Seeding happens in one LabelSeedBatch transaction — one event-log fold when already converged (ATM-40faff)."):

```go
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	vocab := vocabulary(code)
	if err := s.LabelSeedBatch(vocab, actor); err != nil {
		return nil, err
	}
	var boards []core.Label
	for _, l := range vocab {
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	return boards, nil
}
```

Apply the IDENTICAL body change (same doc-comment sentence) to `internal/capability/contextmap/vocabulary.go` `EnsureVocabulary` and `internal/capability/workflowai/vocabulary.go` `EnsureVocabulary` — each already has a package-local `vocabulary(code)` returning its full label list, so the replacement body is byte-identical in all three files.

- [ ] **Step 4: Run the three capability packages**

Run: `go test ./internal/capability/...`
Expected: PASS — including the existing recorder-based order tests (the Task 2 batch interceptor preserves the recorded per-label sequence) and each package's converge tests against a real store.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflow/vocabulary.go internal/capability/workflow/vocabulary_test.go internal/capability/contextmap/vocabulary.go internal/capability/workflowai/vocabulary.go
git commit -m "perf(ATM-40faff): capabilities seed vocabulary in one batch"
```

---

### Task 4: Registry batches across capabilities

`Registry.EnsureVocabulary` currently calls each capability's `EnsureVocabulary` in registration order — one transaction (and one fold) per capability. Collect every capability's `Vocabulary(code)` into ONE batch instead: one fold per select regardless of how many capabilities are enabled (ATM has three). Per-capability `EnsureVocabulary` remains the standalone single-capability converge entry (CLI `capability seed`, `workflowai` plan flow at `internal/capability/workflowai/command.go:371`).

**Files:**
- Modify: `internal/capability/capability.go:176-192` (Registry.EnsureVocabulary), `internal/capability/capability.go:97-103` (interface doc note)
- Test: `internal/capability/capability_test.go` (rewrite the three EnsureVocabulary tests + add a fake service)

**Interfaces:**
- Consumes: `Capability.Vocabulary(code) []core.Label` (existing interface method), `svc.LabelSeedBatch` from Task 2.
- Produces: unchanged signature `(r *Registry) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error)`; boards = Expr != "" labels of the concatenated vocabularies, registration order preserved. The TUI select handler (`internal/tui/projects.go:414`) and all other callers compile unchanged.

- [ ] **Step 1: Rewrite the registry tests to the batch contract (failing first)**

In `internal/capability/capability_test.go`, add the fake service, then REPLACE the three tests `TestEnsureVocabularyLoopsAllInOrder`, `TestEnsureVocabularyStopsAtFirstError`, `TestEnsureVocabularyAggregatesBoardsInRegistrationOrder` with the versions below. Do not modify `fakeCap` — its `vocab` field already exists and its `EnsureVocabulary` recorder keeps serving `TestCommandsPreserveRegistrationOrder` and interface conformance.

```go
// fakeSeedService records LabelSeedBatch calls. Only the batch method is
// ever invoked by the registry; the embedded nil LabelService panics on
// anything else, which is exactly the pin we want.
type fakeSeedService struct {
	core.LabelService
	batches [][]core.Label
	actors  []string
	err     error
}

func (f *fakeSeedService) LabelSeedBatch(labels []core.Label, actor string) error {
	f.batches = append(f.batches, append([]core.Label(nil), labels...))
	f.actors = append(f.actors, actor)
	return f.err
}

// TestEnsureVocabularyBatchesAcrossCapabilities: the registry concatenates
// every capability's Vocabulary in registration order into ONE batch call
// (one event-log fold per select — ATM-40faff).
func TestEnsureVocabularyBatchesAcrossCapabilities(t *testing.T) {
	var calls []string
	svc := &fakeSeedService{}
	reg := NewRegistry(
		&fakeCap{name: "workflow", calls: &calls, vocab: []core.Label{
			{Name: "ATM:status:open", Description: "open"},
			{Name: "ATM:open-tasks", Description: "board", Expr: "status:open"},
		}},
		&fakeCap{name: "contextmap", calls: &calls, vocab: []core.Label{
			{Name: "ATM:context-current", Description: "board", Expr: "context:*"},
		}},
	)
	boards, err := reg.EnsureVocabulary(svc, "ATM", "tester")
	if err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	if len(svc.batches) != 1 {
		t.Fatalf("batches = %d, want exactly 1", len(svc.batches))
	}
	if len(svc.batches[0]) != 3 || svc.batches[0][0].Name != "ATM:status:open" || svc.batches[0][2].Name != "ATM:context-current" {
		t.Errorf("batch = %+v, want the 3 labels in registration+vocabulary order", svc.batches[0])
	}
	if len(svc.actors) != 1 || svc.actors[0] != "tester" {
		t.Errorf("actors = %v, want [tester]", svc.actors)
	}
	if len(calls) != 0 {
		t.Errorf("registry called per-capability EnsureVocabulary %v; it must batch via Vocabulary instead", calls)
	}
	want := []core.Label{
		{Name: "ATM:open-tasks", Description: "board", Expr: "status:open"},
		{Name: "ATM:context-current", Description: "board", Expr: "context:*"},
	}
	if len(boards) != len(want) {
		t.Fatalf("boards = %+v, want %+v", boards, want)
	}
	for i, b := range boards {
		if b != want[i] {
			t.Errorf("boards[%d] = %+v, want %+v", i, b, want[i])
		}
	}
}

// TestEnsureVocabularyStopsAtFirstError: a batch error surfaces and no
// boards are returned.
func TestEnsureVocabularyStopsAtFirstError(t *testing.T) {
	boom := errors.New("boom")
	svc := &fakeSeedService{err: boom}
	var calls []string
	reg := NewRegistry(&fakeCap{name: "workflow", calls: &calls, vocab: []core.Label{{Name: "ATM:x", Description: "d"}}})
	if boards, err := reg.EnsureVocabulary(svc, "ATM", "tester"); !errors.Is(err, boom) || boards != nil {
		t.Fatalf("EnsureVocabulary = (%v, %v), want (nil, boom)", boards, err)
	}
}

// TestEnsureVocabularyEmptyRegistryTouchesNothing: no capabilities → no
// service call at all (also covers the nil-svc callers in older tests).
func TestEnsureVocabularyEmptyRegistryTouchesNothing(t *testing.T) {
	svc := &fakeSeedService{}
	boards, err := NewRegistry().EnsureVocabulary(svc, "ATM", "tester")
	if err != nil || boards != nil {
		t.Fatalf("EnsureVocabulary = (%v, %v), want (nil, nil)", boards, err)
	}
	if len(svc.batches) != 0 {
		t.Errorf("empty registry issued %d batch calls, want 0", len(svc.batches))
	}
}
```

- [ ] **Step 2: Run to verify the new tests fail**

Run: `go test ./internal/capability -run 'TestEnsureVocabulary' -v`
Expected: FAIL — `TestEnsureVocabularyBatchesAcrossCapabilities` reports 0 batches and per-capability calls recorded; the error test fails because the old implementation never touches svc.

- [ ] **Step 3: Implement the registry batch**

Replace `Registry.EnsureVocabulary` in `internal/capability/capability.go` (keep location, replace doc + body):

```go
// EnsureVocabulary converges every registered capability's vocabulary for
// the project in ONE LabelSeedBatch transaction (one event-log fold per
// select — ATM-40faff), and returns the union of the board labels
// (Expr != "") in registration order, vocabulary order within a
// capability. It relies on the Capability contract that Vocabulary
// declares exactly the set EnsureVocabulary seeds; per-capability
// EnsureVocabulary remains the standalone single-capability converge.
func (r *Registry) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	if r == nil {
		return nil, nil
	}
	var all []core.Label
	for _, c := range r.caps {
		all = append(all, c.Vocabulary(code)...)
	}
	if len(all) == 0 {
		return nil, nil
	}
	if err := svc.LabelSeedBatch(all, actor); err != nil {
		return nil, err
	}
	var boards []core.Label
	for _, l := range all {
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	return boards, nil
}
```

Also append one sentence to the `EnsureVocabulary` doc comment in the `Capability` interface (`internal/capability/capability.go:97-103`): "The Registry batches vocabularies across capabilities itself and does not call this method; it remains the standalone single-capability converge entry."

- [ ] **Step 4: Run capability and TUI packages**

Run: `go test ./internal/capability/... ./internal/tui ./internal/cli/...`
Expected: PASS. If any test outside `capability_test.go` asserted per-capability EnsureVocabulary calls from the registry, update it to the batch contract in the same spirit as Step 1 — but a `grep -rn "EnsureVocabulary" internal --include=*_test.go` beforehand is the fast way to enumerate them.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/capability.go internal/capability/capability_test.go
git commit -m "perf(ATM-40faff): registry batches all capability vocabularies into one seed transaction"
```

---

### Task 5: Acceptance probes, live-store measurement, full verify, ledger

Recreate the diagnosis probes (they live uncommitted in the MAIN checkout, not this worktree) upgraded to the real three-capability registry, measure against a fresh live-store copy, run the full suite, and journal the results.

**Files:**
- Create: `internal/tui/select_probe_test.go`
- Create: `internal/store/seed_probe_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1-4; `openRealStore` precedent in `internal/tui/bench_real_store_test.go`; `benchActor` (existing in package tui tests).
- Produces: committed, `ATM_BENCH_STORE`-gated probes for future regression checks; measured numbers journaled on ATM-40faff.

- [ ] **Step 1: Create the select-path probe with the production registry**

Create `internal/tui/select_probe_test.go`:

```go
package tui

// Select-path timing probe for ATM-40faff: times each stage of the
// projects-pane 's' (select) handler against a COPY of a live store, with
// the SAME capability registry as cmd/atm/main.go. Points at the copy via
// ATM_BENCH_STORE; skipped otherwise.

import (
	"os"
	"testing"
	"time"

	"atm/internal/capability"
	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"
	"atm/internal/capability/workflowai"
	"atm/internal/store"
)

func TestSelectPathTimings(t *testing.T) {
	root := os.Getenv("ATM_BENCH_STORE")
	if root == "" {
		t.Skip("ATM_BENCH_STORE not set")
	}
	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	reg := capability.NewRegistry(workflow.New(), contextmap.New(), workflowai.New())
	m, err := NewModel(NewModelOpts{Service: s, Actor: benchActor, Registry: reg})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	code := "ATM"
	var ensure1, ensure2 time.Duration
	stage := func(name string, d *time.Duration, f func()) {
		st := time.Now()
		f()
		el := time.Since(st)
		if d != nil {
			*d = el
		}
		t.Logf("%-24s %v", name, el)
	}

	m.projectScope = code
	stage("EnsureVocabulary", &ensure1, func() {
		if _, err := m.regFor(code).EnsureVocabulary(m.store, code, m.actor); err != nil {
			t.Fatalf("EnsureVocabulary: %v", err)
		}
	})
	stage("capability.refresh", nil, func() { m.capability.refresh() })
	stage("boards.refresh", nil, func() { m.boards.refresh() })
	stage("boards.selectDefault", nil, func() { m.boards.selectDefault() })
	stage("tasks.refresh", nil, func() { m.tasks.refresh() })
	stage("refreshStoreStats", nil, func() { m.refreshStoreStats() })
	stage("refreshSummary", nil, func() { m.projects.refreshSummary() })
	stage("EnsureVocabulary#2", &ensure2, func() {
		if _, err := m.regFor(code).EnsureVocabulary(m.store, code, m.actor); err != nil {
			t.Fatalf("EnsureVocabulary#2: %v", err)
		}
	})

	// ATM-40faff acceptance: converged ensure < 400ms (was ~8.5s at the
	// production registry's 34 seeds). Generous headroom over the ~250ms
	// target so machine variance does not flake the probe.
	for name, d := range map[string]time.Duration{"EnsureVocabulary": ensure1, "EnsureVocabulary#2": ensure2} {
		if d > 400*time.Millisecond {
			t.Errorf("%s took %v, want < 400ms (ATM-40faff)", name, d)
		}
	}
}
```

- [ ] **Step 2: Create the single-seed probe**

Create `internal/store/seed_probe_test.go`:

```go
package store

// No-op seed benchmark for ATM-40faff regression checks against a COPY of
// a live store. Points at the copy via ATM_BENCH_STORE; skipped otherwise.

import (
	"os"
	"testing"
)

func BenchmarkNoopSeedReal(b *testing.B) {
	root := os.Getenv("ATM_BENCH_STORE")
	if root == "" {
		b.Skip("ATM_BENCH_STORE not set")
	}
	s, err := Open(root)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	const actor = "developer@claude:fable-5"
	if err := s.LabelSeed("ATM:status:open", "workflow state: open; task is not started or is being considered", "", actor); err != nil {
		b.Fatalf("warm seed: %v", err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := s.LabelSeed("ATM:status:open", "workflow state: open; task is not started or is being considered", "", actor); err != nil {
			b.Fatalf("seed: %v", err)
		}
	}
}
```

- [ ] **Step 3: Measure against a FRESH copy of the live store**

```bash
SCRATCH=/tmp/claude-1000/-home-ttran-projects-scyllas-atm/8370a0a9-319b-4785-9364-7e1694228fec/scratchpad
rm -rf "$SCRATCH/storecopy" && cp -r ~/.config/atm "$SCRATCH/storecopy"
ATM_BENCH_STORE=$SCRATCH/storecopy go test ./internal/tui -run TestSelectPathTimings -v
ATM_BENCH_STORE=$SCRATCH/storecopy go test ./internal/store -run XXX -bench BenchmarkNoopSeedReal -benchtime 10x
```

Expected: `TestSelectPathTimings` PASSES with both `EnsureVocabulary` stages < 400ms (record the exact numbers), single no-op `LabelSeed` still ~250ms (it is one label = one fold; the win is the batch).

- [ ] **Step 4: Full suite**

Run: `make verify`
Expected: green (unit suites + script tests). Fix regressions before proceeding — do not skip.

- [ ] **Step 5: Commit the probes**

```bash
git add internal/tui/select_probe_test.go internal/store/seed_probe_test.go
git commit -m "test(ATM-40faff): ATM_BENCH_STORE acceptance probes for the select path"
```

- [ ] **Step 6: Journal the results on the ledger**

```bash
atm task comment add --task ATM-40faff --actor "developer@claude:fable-5" --body "Implementation complete on worktree-atm-40faff-select-lag (<list the 5 commit shas>). Measured on a fresh live-store copy (ATM_BENCH_STORE): select-path EnsureVocabulary <measured>ms first / <measured>ms second (was ~8.5s at the production 3-capability registry; acceptance < 400ms), one fold per converged select. make verify green. Probes committed for regression checks."
atm task label remove --task ATM-40faff --label ATM:stage:clarified --actor "developer@claude:fable-5"
atm task label add --task ATM-40faff --label ATM:stage:in-progress --actor "developer@claude:fable-5"
```

(Replace the `<...>` placeholders with the actual shas and measured numbers from Steps 3-5. Merge/branch finishing and the final stage flip to done happen via the superpowers:finishing-a-development-branch skill after review, not in this task.)
