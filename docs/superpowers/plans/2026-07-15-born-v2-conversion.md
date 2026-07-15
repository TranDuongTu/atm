# Born-v2 Conversion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make v2 the sole, unconditional authoring path (new projects born v2, hex IDs) with *reproducible* aliases, and migrate the test/golden suite — the missing foundation the v1 decommission needs.

**Architecture:** Inject three determinism seams into `Store` via functional options on `store.Open` (production defaults unchanged), wire them into the CLI golden harness, pre-migrate incidental sequential-ID references to dynamic capture, then flip to born-v2 by deleting the v1 `Create*` arms and regenerating goldens.

**Tech Stack:** Go, `modernc.org/sqlite`, cobra, `eventsource` package (Clock/HLC/replica).

Spec: `docs/superpowers/specs/2026-07-15-born-v2-conversion-design.md`. ATM task: ATM-0127.

## Global Constraints

- Actor for any ATM ledger mutation: `developer@claude:opus-4.8`.
- Markdown files carry no hard wrap (one line per paragraph).
- Production behavior must be byte-for-byte unchanged when `store.Open(root)` is called with NO options: wall clock, `rand.Reader` replica entropy, `time.Now().UTC()` for `at`. A test must assert this.
- `eventsource` must NOT import `internal/store` (unchanged rule).
- Full `go test ./...` from repo root must be green at the end of every task.
- SCOPE FENCE: this sub-effort deletes only the v1 *authoring* arms of `CreateProject`/`CreateTask`/`CreateComment` and *v1-authoring* tests. It does NOT delete the v1 read-surface arms (search/indexer/rebuild/verify), the `log.go` primitives, `store.Replay`/`store.ReadLog`, or the cache columns — those belong to the decommission tasks that follow this plan.
- The fresh-store default format stays v1 (do NOT change `readStoreMeta`'s fallback or the `SetActiveFormat` guard).
- Commit after every task; message form `type(ATM-0127): summary` ending with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

---

### Task B1: Determinism seams on `Store` via functional options

**Files:**
- Modify: `internal/store/store.go` — `Store` seam fields, `Option` type, `WithClock`/`WithReplicaEntropy`/`WithNow`, `Open(root, opts...)`, `Now()` backed by `nowFn`.
- Modify: `internal/store/eventsource_author.go` — `beginV2AuthorLocked` builds the clock from the seam; author `at` uses `s.Now()`.
- Modify: `internal/store/eventsource_replica.go` — `ensureReplicaForWriteLocked` reads entropy from the seam.
- Test: `internal/store/store_seams_test.go` (new).

**Interfaces:**
- Produces: `store.Option`, `store.WithClock(func() int64) Option`, `store.WithReplicaEntropy(io.Reader) Option`, `store.WithNow(func() time.Time) Option`; `store.Open(root string, opts ...Option) (*Store, error)`.

- [ ] **Step 1: Write the failing reproducibility + production-default test**

```go
package store

import (
	"bytes"
	"testing"
	"time"
)

// fixed seams: counter clock (+1ms/tick), constant replica seed, fixed at.
func fixedSeamOpts() []Option {
	var n int64 = 1_752_480_000_000
	return []Option{
		WithClock(func() int64 { n++; return n }),
		WithReplicaEntropy(bytes.NewReader(bytes.Repeat([]byte{0xAB}, 16))),
		WithNow(func() time.Time { return time.Date(2026, 7, 14, 9, 12, 3, 0, time.UTC) }),
	}
}

func TestSeamsMakeV2AuthoringReproducible(t *testing.T) {
	mk := func() string {
		s, err := Open(t.TempDir(), fixedSeamOpts()...)
		if err != nil { t.Fatal(err) }
		if err := s.Init(""); err != nil { t.Fatal(err) }
		if _, err := s.CreateProjectV2Test("ATM", "Agent Tasks", "developer@claude:test"); err != nil { t.Fatal(err) }
		tk, err := s.CreateTask("ATM", "hello", "", nil, "developer@claude:test")
		if err != nil { t.Fatal(err) }
		return tk.ID
	}
	a, b := mk(), mk()
	if a != b {
		t.Fatalf("v2 alias not reproducible under fixed seams: %q vs %q", a, b)
	}
}

func TestProductionOpenKeepsRandomIdentity(t *testing.T) {
	// No options: two fresh stores must mint DIFFERENT replica ids (random),
	// proving the defaults are still wall clock + rand.Reader.
	r1 := mustReplicaID(t, mustOpen(t))
	r2 := mustReplicaID(t, mustOpen(t))
	if r1 == r2 || r1 == "" {
		t.Fatalf("production Open should mint distinct random replicas, got %q,%q", r1, r2)
	}
}
```

Use whatever project/task creation entry points exist for a v2-active project (the test above names `CreateProjectV2Test`/`mustReplicaID`/`mustOpen` as illustrative helpers — replace with the real ones: a project created on a store where the seams are fixed will still be v1 by default, so either seed with `SetActiveFormat(v2)` first or call the v2 creation path directly). The essential assertions are the two: reproducible-under-fixed-seams, and distinct-random-under-defaults.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store/ -run 'Seams|ProductionOpen' -v`
Expected: FAIL — `Option`/`WithClock`/etc. undefined.

- [ ] **Step 3: Add the seams**

In `store.go`, add fields to `Store` and the options:

```go
type Store struct {
	// ... existing fields ...
	clockNow       func() int64     // nil => eventsource.NewClock uses wall clock
	replicaEntropy io.Reader        // nil => rand.Reader
	nowFn          func() time.Time // nil => time.Now().UTC
}

type Option func(*Store)

// WithClock fixes the millisecond source feeding the v2 HLC clock. Production
// omits it (wall clock). Tests pass a counter for reproducible aliases.
func WithClock(f func() int64) Option { return func(s *Store) { s.clockNow = f } }

// WithReplicaEntropy fixes the entropy the store's replica id is minted from.
func WithReplicaEntropy(r io.Reader) Option { return func(s *Store) { s.replicaEntropy = r } }

// WithNow fixes the wall-clock source for event `at` stamps.
func WithNow(f func() time.Time) Option { return func(s *Store) { s.nowFn = f } }
```

Apply in `Open` (locate the existing constructor and thread options + defaults):

```go
func Open(root string, opts ...Option) (*Store, error) {
	s := &Store{ /* existing init */ }
	for _, o := range opts { o(s) }
	if s.replicaEntropy == nil { s.replicaEntropy = rand.Reader }
	if s.nowFn == nil { s.nowFn = func() time.Time { return time.Now().UTC() } }
	// ... rest of existing Open ...
	return s, nil
}
```

Back `Now()` with the seam:

```go
func (s *Store) Now() time.Time { return s.nowFn() }
```

- [ ] **Step 4: Thread the clock + entropy seams**

In `eventsource_author.go` `beginV2AuthorLocked`, replace `eventsource.NewClock(nil)` with `eventsource.NewClock(s.clockNow)` (nil default = wall clock, behavior-identical). Confirm the author `at` already flows through `s.Now()`; if it calls `time.Now()` directly, route it through `s.Now()`.

In `eventsource_replica.go` `ensureReplicaForWriteLocked`, replace the `rand.Reader` argument(s) to `MintReplicaID(...)` with `s.replicaEntropy`.

- [ ] **Step 5: Run the tests + full suite**

Run: `go test ./internal/store/ -run 'Seams|ProductionOpen' -v` (PASS), then `go test ./...` (all green — production defaults unchanged, so no existing test shifts).

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(ATM-0127): injectable clock/replica/at seams on store.Open for reproducible v2 authoring

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task B2: Wire deterministic seams into the CLI golden harness

Prepares the harness so that when goldens are regenerated for v2 (Task B4) they are reproducible. Green now because seams don't change v1 output (v1 IDs are sequential; v1 timestamps are already scrubbed by `normalizeOutput`).

**Files:**
- Modify: `internal/cli/harness_test.go` — `newGoldenHarness`/`newGoldenHarnessAt` open the store with fixed seams; each store `Open` gets a FRESH `bytes.Reader` over a constant seed (an `io.Reader` is consumed once).

**Steps:**

- [ ] **Step 1:** In `harness_test.go`, add a helper returning the fixed seam options with a FRESH entropy reader per call:

```go
func deterministicSeamOpts() []store.Option {
	var n int64 = 1_752_480_000_000
	return []store.Option{
		store.WithClock(func() int64 { n++; return n }),
		store.WithReplicaEntropy(bytes.NewReader(bytes.Repeat([]byte{0xAB}, 16))),
		store.WithNow(func() time.Time { return time.Date(2026, 7, 14, 9, 12, 3, 0, time.UTC) }),
	}
}
```

Change every `store.Open(dir)` in the harness to `store.Open(dir, deterministicSeamOpts()...)`. Call `deterministicSeamOpts()` per `Open` (fresh reader).

- [ ] **Step 2:** Run the CLI suite: `go test ./internal/cli/ -v`. Expected PASS with NO golden changes (v1 output unaffected). If any golden diff appears, STOP and report — it means a v1 golden depended on wall-clock output the scrub missed; investigate before proceeding.

- [ ] **Step 3:** Full suite `go test ./...` green.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "test(ATM-0127): open the CLI golden store with fixed determinism seams

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task B3: Pre-migrate incidental sequential-ID references to dynamic capture

Convert tests that create a task/comment and later reference it by a hardcoded `CODE-0001`/`-c0001` — where the ID is merely a handle, not the assertion's subject — to capture the returned ID. Green on v1 (`tk.ID == "ATM-0001"`), and forward-green on v2 (`tk.ID == "ATM-<hex>"`). Leave tests whose SUBJECT is v1 sequential minting (they assert the specific sequential value as the point) — Task B4 deletes those.

**Files (survey first, then edit):** run `grep -rlnE '[A-Z]{2,}-[0-9]{3,4}([^0-9]|$)|-c[0-9]{3,4}' --include=*_test.go internal` and triage each hit into: (a) incidental handle → convert here; (b) v1-minting-behavior assertion → leave for B4; (c) upgraded-project numeric ID from a v1 fixture that stays numeric → leave. Do this per package to keep commits reviewable.

**Steps:**

- [ ] **Step 1:** Triage the survey into buckets (a)/(b)/(c); record the (b) list in the report — B4 consumes it.
- [ ] **Step 2:** Convert bucket (a) in `internal/store/*_test.go`: replace hardcoded IDs with the captured return value of the creating call (`tk.ID`, `c.ID`, or `resp.ID`). Run `go test ./internal/store/ -v`; green.
- [ ] **Step 3:** Convert bucket (a) in `internal/cli/*_test.go` and `internal/tui/*_test.go`. For CLI tests that only have the JSON output, capture the created ID from the create command's output rather than hardcoding. Run those packages; green.
- [ ] **Step 4:** Full suite `go test ./...` green (still all v1 — behavior unchanged, only test bookkeeping).
- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "test(ATM-0127): capture task/comment ids dynamically instead of hardcoding sequential ids

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

If the diff is very large, split Step 2/3 into per-package commits — each must leave the suite green.

---

### Task B4: Flip to born-v2 — delete the v1 `Create*` arms, delete v1-authoring tests, regenerate goldens

The atomic flip. After this, no v1-active project can be created; every new project is born v2 with hex IDs.

**Files:**
- Modify: `internal/store/project.go` — delete the v1 `CreateProject` body; `CreateProject` calls `createProjectV2` unconditionally (drop the format dispatch/`else`).
- Modify: `internal/store/task.go` — delete the v1 `CreateTask` body incl. `NextTaskN` minting; `CreateTask` calls `createTaskV2` unconditionally.
- Modify: `internal/store/comment.go` — delete the v1 `CreateComment` body incl. `NextCommentN` minting and the `ActionTaskMetaChanged` companion append; `CreateComment` calls `createCommentV2` unconditionally.
- Delete tests: the bucket (b) tests from B3 (v1 sequential-minting assertions) and any test that constructs a v1 project via `CreateProject`/`CreateTask` and cannot survive v2 — including `comment_id_test.go`'s sequential-comment-id cases and the like. Do NOT delete the v1 read-path primitive tests whose subject (`ReadLog`/`Replay`) survives to a later task UNLESS they author their fixture via the now-v2 `Create*` and therefore no longer compile/pass; if so, delete them (their dead code is removed by the later decommission task).
- Regenerate: `internal/cli/testdata/golden/*` via `-update`.

**Steps:**

- [ ] **Step 1:** Delete the v1 arms of the three `Create*` mutators; make each call its `…V2` body unconditionally. `go build ./...` — fix any now-unused helper/import within the scope fence (do NOT reach into read-surface/log primitives).
- [ ] **Step 2:** Run `go test ./internal/store/ -v`. Delete/repair the failing v1-authoring tests: bucket (b) from B3 (sequential-minting assertions) get deleted; any surviving test now creating a v2 project should pass as-is (born v2). Record every deleted test in the report with a one-line reason.
- [ ] **Step 3:** Run `go test ./internal/tui/ -v` and `go test ./internal/eventsource/ -v`; repair/delete as in Step 2 (TUI seed helpers now yield v2 projects — that is expected).
- [ ] **Step 4:** Regenerate CLI goldens: `go test ./internal/cli/ -run <golden-tests> -update`. Inspect the diff — IDs change from sequential to stable hex; the `determinism-*` goldens stay byte-identical between the two stores. `next_task_n` is NOT removed here (that is a later task); flag if any golden changes in an unexpected way.
- [ ] **Step 5:** Full `go test ./...` green.
- [ ] **Step 6:** Drive the real binary: build `atm`, `atm project create` + `atm task create` on a fresh temp store, confirm the new project has `events.v2.jsonl`, no `log.jsonl`, and a hex task id. (Use the /verify or /run skill.)
- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(ATM-0127): new projects are born v2 (delete v1 Create* arms); regenerate goldens

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## After this plan

The v1 decommission resumes on top of a v2-native, green suite, in this order (from `docs/superpowers/plans/2026-07-15-v1-storage-decommission.md`, now clean because no v1 projects exist and no per-test upgrade hacks are needed):

1. Task 3 — prune the v1 read-surface arms (search/indexer/rebuild/verify/`store log`) and, now unblocked, `LastLogSeq`/`readLogForViews` too.
2. Task 5 — delete the `log.go` v1 primitives + `store.Replay`/`store.ReadLog`.
3. Task 6 — cache.db migration (drop `next_task_n`/`next_comment_n`, rename `log_seq`→`ordinal`) + CLI `next_task_n` output removal + goldens.
4. Task 8 — `atm store prune-v1`.
5. Task 9 — docs.
6. Final whole-branch review + finish.

## Self-Review

**Spec coverage:** D1 seams → B1. D2 reproducible goldens → B2 + B4 Step 4. D3 born-v2 by deleting v1 Create arms, default stays v1 → B4 (Global Constraints pin the default-stays-v1 rule). D4 staged migration (dynamic-capture then flip) → B3 then B4. Testing strategy (reproducibility test, production-default test, real-binary drive) → B1 Step 1, B4 Step 6.

**Placeholder scan:** B1 carries real seam code; the test helper names flagged as illustrative are called out explicitly with the real-entry-point instruction, not left vague. B3's file list is a survey-then-triage (the set is discovered, not guessable) — bounded by the concrete grep and the (a)/(b)/(c) triage rule.

**Type consistency:** `Option`/`WithClock`/`WithReplicaEntropy`/`WithNow`/`Open(root, opts...)` are consistent between B1's interface block, the code, and B2's harness use. `clockNow`/`replicaEntropy`/`nowFn` field names are consistent across store.go, eventsource_author.go, eventsource_replica.go.
