# Store Event-Log Write-Engine Carve (Refactor Step 6, ATM-3b873c) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Carve the event-log write-engine out of `internal/store` into `internal/store/eventlog` — the only package importing `libs/eventsource` — behind intent-shaped `core` interfaces; add `core.StorageAdmin` for the admin surface; sever `internal/cli`'s import of `internal/store` entirely. Zero behavior change.

**Architecture:** Package `store` becomes the facade (sqlite cache + projection, domain services, plain-JSON side stores) implementing `core.Service` + `core.StorageAdmin`. The engine implements `core.Journal`/`core.ChangeSet` (locked write transactions in domain terms) and hands state across the seam only as `core.ProjectSnapshot`. Projection stays under the project lock: facade code projects explicitly inside each write transaction (the exact old `reprojectV2Locked` call sites); engine-internal paths (sync ingest/bootstrap, upgrade) project through an `OnProject` hook the facade supplies at construction. Spec: `docs/superpowers/specs/2026-07-17-store-eventlog-carve-design.md`.

**Tech Stack:** Go 1.x multi-module workspace (root module `atm` + `libs/eventsource` via go.work), sqlite (`cache.db`), cobra, `go test` golden harness with `-update`.

## Global Constraints

- Branch: `atm-3b873c-store-eventlog-carve` (already created; spec committed as 7eb3878). Work at repo root `/home/ttran/projects/scyllas/atm`.
- Every task ends green: `make verify` (or `go build ./... && go test ./...` plus the libs module) passes before each commit.
- **Byte parity is the gate:** zero cli golden churn. If any task makes a golden test fail, that task has a bug — fix the code, never regenerate goldens. The only permitted `-update` run is the Task 10 zero-diff check.
- **Semantics move verbatim.** Lock order (project → store-meta), the append-then-persist-LastHLC commit point, tail-repair vs strict read postures, crash-window orderings (format entry before first append; media delete before entry removal), and every error message string move unchanged. When a step says "move", the function body is copied, not rewritten.
- Event **bytes** are pinned by the existing determinism suite (`store_seams_test.go`, `eventsource_live_write_test.go`, e2e tests). Those tests' assertions must not be weakened.
- **No test is lost.** Task 1 snapshots the test-function inventory; Task 10 verifies every pre-existing test name still exists (moves between packages are fine).
- Import rules on completion (enforced by Task 9 arch tests): only `internal/store/eventlog` imports `atm/libs/eventsource/...`; `internal/cli` production files import neither `atm/internal/store` nor any `atm/internal/store/...` subpackage; `internal/core` stays a pure leaf. Test files (`_test.go`) are exempt.
- NEVER run a dev-built `atm` against `~/.config/atm`. Smoke tests use `ATM_HOME=$(mktemp -d)` or `--store` at a scratch path.
- Commit messages: `refactor(ATM-3b873c): ...` / `test(ATM-3b873c): ...` / `docs(ATM-3b873c): ...`, each ending with the Co-Authored-By and Claude-Session trailers used on 7eb3878.
- Markdown files: prose as single un-wrapped lines (no hard wrapping).
- Scratch dir for baselines: `SCRATCH=/tmp/claude-1000/-home-ttran-projects-scyllas-atm/25bfe217-a463-4158-9733-9fad5dceaab5/scratchpad`.

---

### Task 1: `internal/store/fsio` leaf + `core.MarshalSorted`; baselines

**Files:**
- Create: `internal/store/fsio/fsio.go`, `internal/store/fsio/json.go`, `internal/store/fsio/fsio_test.go`, `internal/core/json.go`
- Modify: `internal/store/lock.go` (shrinks to a delegation), `internal/store/json.go` (shrinks to aliases)

**Interfaces:**
- Consumes: nothing new.
- Produces: `fsio.WithLock(projectsDir, name string, fn func() error) error`; `fsio.WriteFileAtomic(path string, v any) error`; `fsio.WriteJSON(path string, v any) error`; `fsio.ReadJSON(path string, v any) error`; `core.MarshalSorted(v any) ([]byte, error)`. `store.WithLock`/`store.WriteJSON`/etc. keep working unchanged (delegations), so no other file changes in this task.

- [ ] **Step 1: Capture branch-wide baselines (before ANY code change)**

```bash
SCRATCH=/tmp/claude-1000/-home-ttran-projects-scyllas-atm/25bfe217-a463-4158-9733-9fad5dceaab5/scratchpad
go build -o "$SCRATCH/atm-before" ./cmd/atm
for c in "store" "store path" "store log" "store verify" "store rebuild" "store upgrade" "store prune-v1" "store set-format" "store remote" "store remote add" "store remote list" "store remote remove" "store sync"; do echo "=== atm $c --help ==="; "$SCRATCH/atm-before" $c --help; done > "$SCRATCH/store-help-before.txt" 2>&1
grep -rho '^func Test[A-Za-z0-9_]*' --include='*_test.go' internal tests | sort > "$SCRATCH/tests-before.txt"
wc -l "$SCRATCH/store-help-before.txt" "$SCRATCH/tests-before.txt"
```

The help file is the byte-parity target for the whole branch; the test inventory is Task 10's no-lost-tests baseline.

- [ ] **Step 2: Write the failing fsio test**

Create `internal/store/fsio/fsio_test.go`:

```go
package fsio

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWithLockNestsDifferentNames pins the one nesting rule the store relies
// on (project lock -> store-meta lock): DIFFERENT names nest, and the
// registry survives sequential re-acquisition of the same name.
func TestWithLockNestsDifferentNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	ran := false
	err := WithLock(dir, "ABC", func() error {
		return WithLock(dir, "store-meta", func() error {
			ran = true
			return nil
		})
	})
	if err != nil || !ran {
		t.Fatalf("nested WithLock: err=%v ran=%v", err, ran)
	}
	if err := WithLock(dir, "ABC", func() error { return nil }); err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ABC.lock")); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
}

func TestJSONRoundTripAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.json")
	if err := WriteJSON(path, map[string]any{"b": 2, "a": 1}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var got map[string]any
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}
```

Run: `go test ./internal/store/fsio/` — Expected: FAIL (package does not exist).

- [ ] **Step 3: Implement fsio by moving the existing code**

Create `internal/store/fsio/fsio.go` — the body of `internal/store/lock.go` moved verbatim, with `getHeldLock` taking the directory instead of a `*Store` (the lock file lives at `<projectsDir>/<name>.lock`, exactly `Store.lockPath` today; the registry key stays the bare name, exactly today's cross-Store global behavior):

```go
// Package fsio holds the store's two filesystem primitives — the
// cross-process advisory lock and atomic JSON file I/O — shared by the store
// facade and the eventlog engine. It knows nothing about domains or events.
package fsio

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

var locker = struct {
	sync.Mutex
	fds map[string]*heldLock
}{fds: map[string]*heldLock{}}

type heldLock struct {
	mu  sync.Mutex
	f   *os.File
	cnt int
}

// WithLock serializes fn against every other holder of name, in this process
// (mutex) and across processes (flock on <projectsDir>/<name>.lock). It is
// NOT reentrant for the same name; different names nest. The registry is
// keyed by bare name — identical to the pre-carve Store.WithLock behavior.
func WithLock(projectsDir, name string, fn func() error) error {
	h := getHeldLock(projectsDir, name)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cnt == 0 {
		if err := acquireFlock(h.f); err != nil {
			return err
		}
	}
	h.cnt++
	defer func() {
		h.cnt--
		if h.cnt == 0 {
			_ = releaseFlock(h.f)
		}
	}()
	return fn()
}

func getHeldLock(projectsDir, name string) *heldLock {
	locker.Lock()
	defer locker.Unlock()
	h, ok := locker.fds[name]
	if !ok {
		_ = os.MkdirAll(projectsDir, 0o755)
		f, err := os.OpenFile(filepath.Join(projectsDir, name+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			h = &heldLock{}
		} else {
			h = &heldLock{f: f}
		}
		locker.fds[name] = h
		return h
	}
	return h
}

func acquireFlock(f *os.File) error {
	if f == nil {
		return fmt.Errorf("lock file not open")
	}
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

func releaseFlock(f *os.File) error {
	if f == nil {
		return nil
	}
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
```

Create `internal/core/json.go` — `MarshalSorted` + `sortKeys` moved VERBATIM from `internal/store/json.go` (they import only `encoding/json`, `sort`, `strings`; core stays I/O-free).

Create `internal/store/fsio/json.go` — `WriteFileAtomic`, `WriteJSON`, `ReadJSON` moved verbatim from `internal/store/json.go`, with `MarshalSorted(v)` replaced by `core.MarshalSorted(v)` (import `atm/internal/core`).

Rewrite `internal/store/lock.go` to:

```go
package store

import "atm/internal/store/fsio"

// WithLock delegates to the shared fsio primitive; the registry, nesting and
// cross-process semantics are unchanged (see fsio.WithLock).
func (s *Store) WithLock(code string, fn func() error) error {
	return fsio.WithLock(s.projectsDir(), code, fn)
}
```

Rewrite `internal/store/json.go` to:

```go
package store

import (
	"atm/internal/core"
	"atm/internal/store/fsio"
)

// Delegations kept so the store's many callers stay unchanged; the
// implementations live in core (pure marshaling) and fsio (file I/O).
var (
	MarshalSorted   = core.MarshalSorted
	WriteFileAtomic = fsio.WriteFileAtomic
	WriteJSON       = fsio.WriteJSON
	ReadJSON        = fsio.ReadJSON
)
```

Delete `internal/store/json_test.go`'s moved coverage ONLY if it tests the moved functions directly through package store — otherwise leave it; the delegations keep it passing. (Default: leave it.)

- [ ] **Step 4: Verify**

Run: `go build ./... && go test ./internal/store/... ./internal/core/ && go test ./internal/cli/ ./tests/arch/`
Expected: PASS everywhere — pure delegation, no behavior change. `lock_test.go` still passes against `Store.WithLock`.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "refactor(ATM-3b873c): extract fsio leaf (lock + atomic JSON); MarshalSorted to core"
```

---

### Task 2: Core persistence contract — rewrite `core/repository.go`

**Files:**
- Rewrite: `internal/core/repository.go`

**Interfaces:**
- Consumes: existing `core` types (`Task`, `Comment`, `Label`, `Project`, `LogEntry`).
- Produces: `core.TaskDraft`, `core.CommentDraft`, `core.LabelFields`, `core.ProjectSnapshot`, `core.ProjectWriter`, `core.TaskWriter`, `core.CommentWriter`, `core.LabelWriter`, `core.ChangeSet`, `core.Journal`. Tasks 3-6 implement and consume these EXACT names and signatures.

- [ ] **Step 1: Replace the CRUD placeholders with the intent contract**

Rewrite `internal/core/repository.go` in full:

```go
package core

// The persistence seam, reshaped by refactor step 6 (ATM-3b873c) from the
// CRUD placeholders step 4 declared. An event-sourced store records INTENTS,
// so the writer interfaces mirror the closed action set the log records —
// PutTask(diff-the-struct) would erase the distinctions the history views
// render. internal/store/eventlog implements this contract; the store facade
// consumes it. Nothing here names an event, replica, HLC, or projector.

// TaskDraft carries a task-creation intent.
type TaskDraft struct {
	Title       string
	Description string
	Labels      []string
}

// CommentDraft carries a comment-creation intent. TaskID and ReplyTo are the
// user-facing aliases; resolution to identities happens behind the seam.
type CommentDraft struct {
	TaskID  string
	ReplyTo string
	Body    string
	Labels  []string
}

// LabelFields is a partial label upsert: a nil field is not asserted by the
// write, so a concurrent writer's value for it is never clobbered.
type LabelFields struct {
	Description *string
	Expr        *string
}

// ProjectSnapshot is one project's full live state in domain terms, ready
// for a read-model to project: creation-ordered tasks and comments with
// display ordinals precomputed, name-sorted labels, plus the names of labels
// that are no longer live (rows a projection must delete). ChangeCount is
// the freshness key: the number of committed changes the snapshot reflects.
type ProjectSnapshot struct {
	Project       *Project
	Tasks         []*Task
	Comments      []*Comment // TaskID / ReplyTo carry aliases
	Labels        []Label
	RemovedLabels []string
	ChangeCount   int
}

type ProjectWriter interface {
	// CreateProject records the project's birth. Valid only inside
	// WithProjectBirth (the file must be empty).
	CreateProject(name, actor string) error
	SetProjectName(name, actor string) error
	// ForgetProject drops the project's storage registration so a removed
	// project can be recreated. The caller deletes the media and read-model.
	ForgetProject() error
}

type TaskWriter interface {
	CreateTask(d TaskDraft, actor string) (id string, err error)
	SetTaskTitle(id, title, actor string) error
	SetTaskDescription(id, description, actor string) error
	AddTaskLabel(id, label, actor string) error
	RemoveTaskLabel(id, label, actor string) error
	RemoveTask(id, actor string) error
}

type CommentWriter interface {
	CreateComment(d CommentDraft, actor string) (id string, err error)
	SetCommentBody(id, body, actor string) error
	AddCommentLabel(id, label, actor string) error
	RemoveCommentLabel(id, label, actor string) error
	RemoveComment(id, actor string) error
}

type LabelWriter interface {
	// UpsertLabel asserts exactly the non-nil fields.
	UpsertLabel(name string, f LabelFields, actor string) error
	// SeedLabel upserts description/expr unless the label is already live
	// (idempotent vocabulary seeding; never overwrites a curated entry).
	SeedLabel(name, description, expr, actor string) error
	// EnsureLabels registers any of the names not already live, asserting no
	// fields (auto-registration at assign time).
	EnsureLabels(names []string, actor string) error
	// RemoveLabel unregisters a live label; ErrNotFound otherwise.
	RemoveLabel(name, actor string) error
}

// ChangeSet is one locked write transaction against a single project: intent
// writes, existence guards answered from the same consistent state the
// writes apply to, and a Snapshot of the state including this transaction's
// own writes. It is only valid inside the WithProject* closure it was handed
// to.
type ChangeSet interface {
	ProjectWriter
	TaskWriter
	CommentWriter
	LabelWriter

	// RequireProject errors with ErrNotFound unless the project is live.
	RequireProject() error
	// ResolveTask / ResolveComment error with ErrNotFound (unknown alias) or
	// ErrUsage (ambiguous, or wrong kind) exactly like the mutating verbs.
	ResolveTask(id string) error
	ResolveComment(id string) error
	TaskHasLabel(id, label string) (bool, error)
	CommentHasLabel(id, label string) (bool, error)
	HasLiveTasks() (bool, error)
	LabelLive(name string) (bool, error)

	Snapshot() (*ProjectSnapshot, error)
}

// Journal is the write-engine seam the store facade consumes. A transaction
// sees and appends the project's full change history under the project lock;
// birth establishes a brand-new project (WithProjectWrite would refuse it).
type Journal interface {
	WithProjectWrite(code string, fn func(ChangeSet) error) error
	WithProjectBirth(code string, fn func(ChangeSet) error) error
	// Snapshot is a strict, lock-free point-in-time read (integrity errors
	// wrap ErrIntegrity).
	Snapshot(code string) (*ProjectSnapshot, error)
	// ChangeCount is the freshness key: committed changes on disk now.
	ChangeCount(code string) (int, error)
	// LogEntries renders the history as display entries; on an integrity
	// failure it returns the recoverable prefix ALONGSIDE the error.
	LogEntries(code string) ([]LogEntry, error)
	// MediaExists reports ErrConflict when the project has media on disk in
	// any format, nil when genuinely absent.
	MediaExists(code string) error
}
```

- [ ] **Step 2: Verify nothing consumed the old CRUD shapes**

Run: `grep -rn 'TaskRepository\|LabelRepository\|ProjectRepository\|CommentRepository' --include='*.go' . | grep -v repository.go`
Expected: no output. Then `go build ./... && go test ./internal/core/ ./tests/arch/` — Expected: PASS (`TestCoreIsAPureLeaf` still green).

- [ ] **Step 3: Commit**

```bash
git add internal/core/repository.go && git commit -m "refactor(ATM-3b873c): reshape core persistence contract to intent writers + ProjectSnapshot + Journal"
```

---

### Task 3: `internal/store/eventlog` skeleton — Engine; file/replica/meta move

**Files:**
- Create: `internal/store/eventlog/engine.go`
- Move (git mv + transform): `internal/store/eventsource_file.go` → `internal/store/eventlog/file.go`; `internal/store/eventsource_replica.go` → `internal/store/eventlog/replica.go`; `internal/store/eventsource_meta.go` → `internal/store/eventlog/meta.go`
- Move tests that compile against the Engine alone (see Step 4): `eventsource_file_test.go`, `eventsource_meta_test.go`, `eventsource_replica_test.go` (leave any in store that construct a full `*Store`)
- Create: `internal/store/eventlog_bridge.go` (transitional forwarders + type aliases; deleted by Task 9)
- Modify: `internal/store/store.go` (`Store` gains `eng`, `Open` constructs it)

**Interfaces:**
- Consumes: `fsio.WithLock`, `fsio.ReadJSON/WriteJSON`, `core` sentinels.
- Produces: `eventlog.Engine` with `New(root string, o Options) *Engine`; `Options{ClockNow func() int64; ReplicaEntropy io.Reader; Now func() time.Time; OnProject func(code string, snap *core.ProjectSnapshot) error; OnMediaReplaced func(code string)}`; exported (within the store subtree) engine methods `ReadStoreMeta`, `MutateStoreMeta`, `ProjectFormat`, `DispatchFormat`, `WithProjectFormatLock`, `SetProjectFormat`, `RemoveProjectFormat`, `SetActiveFormat`, `ProjectCodesOnDisk`, `ReadV2File`, `VerifyFile`, `AppendEventLineLocked`, `EnsureReplicaForWriteLocked`, `EventsV2Path`, `LogPath`, `WithLock`; types `eventlog.StoreFormat` (consts `StoreFormatV1/V2`), `eventlog.StoreMeta`, `eventlog.ProjectEventsourceMeta`, `eventlog.V2FileSnapshot`. Tasks 4-6 build on exactly these.

- [ ] **Step 1: Create the Engine**

Create `internal/store/eventlog/engine.go`:

```go
// Package eventlog is the event-sourced write-engine behind internal/store:
// the ONLY package in the repository that imports atm/libs/eventsource. It
// owns events.v2.jsonl and store.json, authors and ingests events, and hands
// state upward exclusively as core-typed snapshots. The facade supplies the
// read-model projection through the OnProject hook; the engine never touches
// sqlite.
package eventlog

import (
	"io"
	"path/filepath"
	"time"

	"atm/internal/core"
	"atm/internal/store/fsio"
)

// Options carries the determinism seams (mirroring store.WithClock /
// WithReplicaEntropy / WithNow — the facade threads them through) and the
// facade hooks. All fields must be non-nil except the hooks, which are only
// invoked by paths that project (nil is fine for engines used in tests that
// never sync or upgrade).
type Options struct {
	ClockNow       func() int64 // nil => eventsource.NewClock uses wall clock
	ReplicaEntropy io.Reader
	Now            func() time.Time
	// OnProject projects a snapshot into the facade's read-model. Invoked
	// under the project lock at the exact points the pre-carve code called
	// reprojectV2Locked from engine-internal paths (sync ingest/bootstrap,
	// upgrade).
	OnProject func(code string, snap *core.ProjectSnapshot) error
	// OnMediaReplaced runs after an upgrade replaces a project's media
	// (facade drops its log memo and wipes vector indexes).
	OnMediaReplaced func(code string)
}

type Engine struct {
	root string
	opts Options
}

func New(root string, o Options) *Engine { return &Engine{root: root, opts: o} }

func (e *Engine) now() time.Time { return e.opts.Now() }

func (e *Engine) projectsDir() string { return filepath.Join(e.root, "projects") }
func (e *Engine) projectDir(code string) string {
	return filepath.Join(e.projectsDir(), code)
}
func (e *Engine) EventsV2Path(code string) string {
	return filepath.Join(e.projectDir(code), "events.v2.jsonl")
}
func (e *Engine) LogPath(code string) string {
	return filepath.Join(e.projectDir(code), "log.jsonl")
}
func (e *Engine) eventsourceMetaPath(code string) string {
	return filepath.Join(e.projectDir(code), "eventsource.json")
}
func (e *Engine) storeMetaPath() string { return filepath.Join(e.root, "store.json") }

// WithLock delegates to the shared fsio primitive (same registry the facade
// uses, so facade-held and engine-held locks exclude each other correctly).
func (e *Engine) WithLock(name string, fn func() error) error {
	return fsio.WithLock(e.projectsDir(), name, fn)
}
```

- [ ] **Step 2: Move file/replica/meta**

```bash
git mv internal/store/eventsource_file.go internal/store/eventlog/file.go
git mv internal/store/eventsource_replica.go internal/store/eventlog/replica.go
git mv internal/store/eventsource_meta.go internal/store/eventlog/meta.go
```

Transform each moved file (mechanical, compiler-driven):
- `package store` → `package eventlog`.
- Receiver `(s *Store)` → `(e *Engine)`; `s.` → `e.` within bodies.
- Exported renames (these are the engine's API for the facade during migration): `readV2FileAt` → `readFileAt` (stays unexported — only file.go internals), `readV2File` → `ReadV2File`, `verifyV2File` → `VerifyFile`, `appendV2EventLineLocked` → `AppendEventLineLocked`, `readStoreMeta` → `ReadStoreMeta`, `writeStoreMeta` stays unexported, `mutateStoreMeta` → `MutateStoreMeta`, `projectFormat` → `ProjectFormat`, `dispatchFormat` → `DispatchFormat`, `withProjectFormatLock` → `WithProjectFormatLock`, `setProjectFormat` → `SetProjectFormat`, `removeProjectFormat` → `RemoveProjectFormat`, `ensureReplicaForWriteLocked` → `EnsureReplicaForWriteLocked`. `SetActiveFormat`, `ProjectFormatForCLI` (drop — the facade keeps its own delegating method, see bridge), `StoreFormat`/`StoreMeta`/`ProjectEventsourceMeta`/`V2FileSnapshot` keep their names in the new package.
- `ReadJSON`/`WriteJSON` → `fsio.ReadJSON`/`fsio.WriteJSON`; `Now()` in `writeStoreMeta` → `core.Now()`; sentinels `ErrUsage`/`ErrConflict`/`ErrIntegrity`/`ErrNotFound` → `core.ErrUsage` etc.; `s.WithLock(...)` → `e.WithLock(...)`; `s.Now()` → `e.now()`; `s.replicaEntropy` → `e.opts.ReplicaEntropy`.
- `SetActiveFormat` calls `s.projectCodesOnDisk()` — move that function's body from `internal/store/store.go` into `meta.go` as `(e *Engine) ProjectCodesOnDisk() ([]string, error)` and keep a one-line delegation `func (s *Store) projectCodesOnDisk() ([]string, error) { return s.eng.ProjectCodesOnDisk() }` in `store.go`.
- `testHookAfterDispatchFormat` moves with meta.go as a package var of eventlog; any store-package test assigning it moves in Step 4 or switches to `eventlog.TestHookAfterDispatchFormat` (export it as `TestHookAfterDispatchFormat` if a store-side test still needs it — check with `grep -rn testHookAfterDispatchFormat internal/store`).

- [ ] **Step 3: Wire the Engine into Store and bridge the moved symbols**

In `internal/store/store.go`, add the field and construction. `Store` KEEPS `clockNow`/`replicaEntropy`/`nowFn` (the `Option` funcs set them before the engine exists); `Open` builds the engine after defaults are filled:

```go
type Store struct {
	Root string
	// ... existing fields unchanged ...

	// eng is the event-log write-engine (internal/store/eventlog). Hooks are
	// wired in Task 6; until then engine-internal projection paths are still
	// facade methods.
	eng *eventlog.Engine
}
```

At the end of `Open`, after the `nowFn` default:

```go
	s.eng = eventlog.New(abs, eventlog.Options{
		ClockNow:       s.clockNow,
		ReplicaEntropy: s.replicaEntropy,
		Now:            s.nowFn,
	})
	return s, nil
```

Create `internal/store/eventlog_bridge.go` — transitional forwarders so every not-yet-moved store file compiles unchanged (Task 9 deletes this file; each of Tasks 4-6 deletes the forwarders it obsoletes):

```go
package store

// Transitional bridge to internal/store/eventlog while the carve is in
// flight (refactor step 6, ATM-3b873c). Tasks 4-6 shrink it; Task 9 deletes
// it. Nothing new may start depending on these names.

import "atm/internal/store/eventlog"

type StoreFormat = eventlog.StoreFormat
type StoreMeta = eventlog.StoreMeta
type ProjectEventsourceMeta = eventlog.ProjectEventsourceMeta
type V2FileSnapshot = eventlog.V2FileSnapshot

const (
	StoreFormatV1 = eventlog.StoreFormatV1
	StoreFormatV2 = eventlog.StoreFormatV2
)

func (s *Store) readStoreMeta() (*eventlog.StoreMeta, error) { return s.eng.ReadStoreMeta() }
func (s *Store) mutateStoreMeta(fn func(m *eventlog.StoreMeta) error) error {
	return s.eng.MutateStoreMeta(fn)
}
func (s *Store) projectFormat(code string) (eventlog.StoreFormat, error) {
	return s.eng.ProjectFormat(code)
}
func (s *Store) dispatchFormat(code string) (eventlog.StoreFormat, error) {
	return s.eng.DispatchFormat(code)
}
func (s *Store) withProjectFormatLock(code string, want eventlog.StoreFormat, fn func() error) error {
	return s.eng.WithProjectFormatLock(code, want, fn)
}
func (s *Store) setProjectFormat(code string, f eventlog.StoreFormat) error {
	return s.eng.SetProjectFormat(code, f)
}
func (s *Store) removeProjectFormat(code string) error { return s.eng.RemoveProjectFormat(code) }
func (s *Store) SetActiveFormat(f eventlog.StoreFormat) error { return s.eng.SetActiveFormat(f) }
func (s *Store) ProjectFormatForCLI(code string) (eventlog.StoreFormat, error) {
	return s.eng.ProjectFormat(code)
}
func (s *Store) eventsV2Path(code string) string { return s.eng.EventsV2Path(code) }
func (s *Store) readV2File(code string, repairTail bool) (*eventlog.V2FileSnapshot, error) {
	return s.eng.ReadV2File(code, repairTail)
}
func (s *Store) verifyV2File(code string) (*eventlog.V2FileSnapshot, error) {
	return s.eng.VerifyFile(code)
}
func (s *Store) appendV2EventLineLocked(code string, raw []byte) error {
	return s.eng.AppendEventLineLocked(code, raw)
}
func (s *Store) currentReplicaIDLocked() (string, error) {
	return s.eng.EnsureReplicaForWriteLocked()
}
```

Then DELETE the now-duplicated originals from the not-yet-moved store files: `store.go`'s `storeMetaPath`/`eventsV2Path`/`eventsourceMetaPath` path helpers if they remain anywhere (grep), and `eventsource_author.go`'s `currentReplicaIDLocked` (the bridge provides it). Adjust `eventsource_author.go`'s remaining references via the bridge names (it still compiles in package store, calling the forwarders). NOTE: `V2FileSnapshot`'s raw `.Raw` byte handling and `AppendEventLineLocked(code string, raw []byte)` — check the actual parameter type in `file.go` when moving (`ev.Raw` is what `commitV2AuthorLocked` passes) and keep it identical.

- [ ] **Step 4: Move the tests that test the moved code**

For each of `eventsource_file_test.go`, `eventsource_meta_test.go`, `eventsource_replica_test.go`: if the test exercises only moved functions, `git mv` it to `internal/store/eventlog/` (rename package, `Store`-construction → `eventlog.New(root, eventlog.Options{...})` with explicit seams). If it constructs a full `*Store` or drives domain mutators, LEAVE it in package store — the bridge keeps it green. Do not delete any test function.

- [ ] **Step 5: Verify**

Run: `go build ./... && go test ./internal/store/... && go test ./internal/cli/ ./internal/tui/ ./tests/arch/`
Expected: PASS. Also `grep -rn '"atm/libs/eventsource"' internal/store/*.go | grep -v _test` — the remaining importers must be exactly: `eventsource_author.go`, `eventsource_projector.go`, `eventsource_read.go`, `eventsource_views.go`, `eventsource_sync.go`, `eventsource_upgrade.go`, `verify.go`, `task.go`, `comment.go`, `label.go`, `project.go` (Tasks 4-6 clear these).

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "refactor(ATM-3b873c): eventlog engine skeleton — file/replica/meta move behind Engine"
```

---

### Task 4: Authoring behind `core.ChangeSet`; facade mutators flip

**Files:**
- Move: `internal/store/eventsource_author.go` → `internal/store/eventlog/author.go`
- Create: `internal/store/eventlog/changeset.go`, `internal/store/eventlog/snapshot.go`
- Modify: `internal/store/{task.go,comment.go,label.go,project.go,eventsource_projector.go}` (projector split: converters leave, sqlite stays), `internal/store/eventlog_bridge.go` (shrink)
- Move tests exercising authoring internals to eventlog (same rule as Task 3 Step 4); `eventsource_author_test.go` in particular

**Interfaces:**
- Consumes: Task 2's `core.ChangeSet`/`core.Journal` contract, Task 3's Engine internals.
- Produces: `(*Engine).WithProjectWrite(code string, fn func(core.ChangeSet) error) error`; `(*Engine).WithProjectBirth(...)` (same signature); `(*Engine).Snapshot(code string) (*core.ProjectSnapshot, error)`; `(*Engine).MediaExists(code string) error`; facade helpers `(*Store).projectSnapshot(code string, snap *core.ProjectSnapshot) error`, `(*Store).projectSnapshotDB(db *sql.DB, code string, snap *core.ProjectSnapshot) error`, `(*Store).reprojectTxn(code string, cs core.ChangeSet) error`. Compile-time asserts: `var _ core.ChangeSet = (*changeSet)(nil)` in eventlog.

- [ ] **Step 1: Move author.go and make it engine-internal**

```bash
git mv internal/store/eventsource_author.go internal/store/eventlog/author.go
```

Transform: package/receiver as in Task 3; `V2Draft` → unexported `draft`; `beginV2AuthorLocked` → `beginAuthorLocked`, `commitV2AuthorLocked` → `commitAuthorLocked`, `appendV2Locked` → `appendLocked`, `appendV2TaskCreatedLocked` → `appendTaskCreatedLocked`, `appendV2CommentCreatedLocked` → `appendCommentCreatedLocked`, `appendV2LabelUpsertsLocked` → `appendLabelUpsertsLocked`, `v2AuthorCtx` → `authorCtx`; bridge-call sites become direct engine calls (`e.ReadV2File(code, true)`, `e.ReadStoreMeta()`, `e.MutateStoreMeta`, `e.AppendEventLineLocked`, `e.EnsureReplicaForWriteLocked`, `eventsource.NewClock(e.opts.ClockNow)`, `e.now()`); sentinels → `core.*`. Move the action-string constants (`ActionProjectCreated` ... `ActionCommentRemoved`) from `internal/store/log.go` into `author.go` unexported (`actionTaskTitleChanged` etc.) — verify first that nothing outside internal/store uses them: `grep -rn 'store.Action' --include='*.go' . | grep -v internal/store` must be empty.

- [ ] **Step 2: Snapshot conversion (fold → core), moved from the projector**

Create `internal/store/eventlog/snapshot.go`. `convertState` is the ordering/conversion logic of the old `cacheProjectFromV2StateDB` MINUS the sqlite calls, and the four `*FromV2` converters move here verbatim (with `Task` etc. now `core.Task`):

```go
package eventlog

import (
	"sort"

	"atm/internal/core"
	"atm/libs/eventsource"
)

// Snapshot is the strict lock-free read: verify the file, fold, convert.
// Integrity failures wrap core.ErrIntegrity (VerifyFile's posture).
func (e *Engine) Snapshot(code string) (*core.ProjectSnapshot, error) {
	snap, err := e.VerifyFile(code)
	if err != nil {
		return nil, err
	}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		return nil, err
	}
	return convertState(code, state, snap.EventCount), nil
}

// convertState renders a fold as the domain snapshot the facade projects:
// the same iteration order, ordinal assignment, alias resolution and
// tombstone handling cacheProjectFromV2StateDB used, so the projected rows
// are byte-identical to the pre-carve projection.
func convertState(code string, st *eventsource.State, eventCount int) *core.ProjectSnapshot {
	out := &core.ProjectSnapshot{ChangeCount: eventCount}
	for _, p := range st.Projects {
		if p.Code != code || p.Tombstoned {
			continue
		}
		out.Project = projectFromV2(p)
	}
	commentAlias := func(id string) string {
		if c, ok := st.Comments[id]; ok && !c.Tombstoned {
			return c.Alias
		}
		return ""
	}
	ordinal := 0
	for _, t := range st.TasksByCreation() {
		ordinal++
		if t.Tombstoned {
			continue
		}
		out.Tasks = append(out.Tasks, taskFromV2(code, t, ordinal))
		for i, c := range st.CommentsByCreation(t.ID) {
			if c.Tombstoned {
				continue
			}
			out.Comments = append(out.Comments, commentFromV2(c, t.Alias, commentAlias(c.ReplyToRef), i+1))
		}
	}
	names := make([]string, 0, len(st.Labels))
	for name, l := range st.Labels {
		if l.Tombstoned {
			out.RemovedLabels = append(out.RemovedLabels, name)
			continue
		}
		names = append(names, name)
	}
	sort.Strings(out.RemovedLabels)
	sort.Strings(names)
	for i, name := range names {
		out.Labels = append(out.Labels, labelFromV2(st.Labels[name], i+1))
	}
	return out
}
```

(then `projectFromV2`, `taskFromV2`, `commentFromV2`, `labelFromV2` moved verbatim from `eventsource_projector.go`, returning `*core.Project` / `*core.Task` / `*core.Comment` / `core.Label`.)

Also add to snapshot.go: `func (e *Engine) MediaExists(code string) error` — the body of `projectMediaExists` moved from `internal/store/project.go` (uses `e.LogPath`, `e.EventsV2Path`, `core.ErrConflict`).

- [ ] **Step 3: The ChangeSet transaction**

Create `internal/store/eventlog/changeset.go`. The mapping is exact — each method is one of the old store helpers' engine half, event bytes unchanged:

| ChangeSet method | Old code it carries |
|---|---|
| `CreateProject(name, actor)` | `createProjectV2`'s root-event block (`NewProjectCreated` + `commitAuthorLocked`); sets `cs.rootCommitted` |
| `SetProjectName` | `setProjectNameV2`'s begin/resolve/append |
| `ForgetProject` | `e.RemoveProjectFormat(cs.code)` |
| `CreateTask(d, actor)` | `appendTaskCreatedLocked` |
| `SetTaskTitle/SetTaskDescription/RemoveTask/RemoveTaskLabel` | `mutateTaskV2`'s inner body with the matching action + payload (`{"title":..}`, `{"description":..}`, nil, `{"label":..}`) |
| `AddTaskLabel` | `taskLabelAddV2`'s final append (action task.label-added, `{"label":..}`) — the guard/ensure stay facade-side |
| `CreateComment(d, actor)` | `appendCommentCreatedLocked` |
| `SetCommentBody/RemoveComment/RemoveCommentLabel/AddCommentLabel` | `mutateCommentV2`'s inner body with the matching action/payload |
| `UpsertLabel(name, f, actor)` | `labelUpsertV2`'s append; payload gets `description`/`expr` keys only for non-nil fields |
| `SeedLabel` | `labelSeedV2`'s body (live no-op + description-always/expr-if-nonempty payload) |
| `EnsureLabels` | `appendLabelUpsertsLocked` |
| `RemoveLabel` | `labelRemoveV2`'s fold not-found check + append |
| `RequireProject` | `beginAuthorLocked` + `resolveProjectRef` |
| `ResolveTask/ResolveComment` | begin + `resolveTaskRef`/`resolveCommentRef`, result discarded |
| `TaskHasLabel/CommentHasLabel` | begin + resolve + fold `Labels` scan (the no-op checks of `taskLabelAddV2`/`commentLabelAddV2`) |
| `HasLiveTasks` | `v2HasTasksGuardLocked`'s scan, returning bool |
| `LabelLive` | begin + `state.Labels[name]` live check |
| `Snapshot()` | `e.Snapshot(cs.code)` (strict re-read, exactly `reprojectV2Locked`'s read posture) |

Skeleton (fill every method per the table; each begins its own `authorCtx` exactly like the old helpers did per call):

```go
package eventlog

import (
	"fmt"

	"atm/internal/core"
	"atm/libs/eventsource"
)

type changeSet struct {
	e             *Engine
	code          string
	rootCommitted bool
}

var _ core.ChangeSet = (*changeSet)(nil)

// WithProjectWrite is the format-gated write transaction every live mutator
// runs in: project lock + under-lock v2 re-check (the old
// withProjectFormatLock), with fn free to interleave guards, intent appends
// and the facade's own reads/projection.
func (e *Engine) WithProjectWrite(code string, fn func(core.ChangeSet) error) error {
	return e.WithProjectFormatLock(code, StoreFormatV2, func() error {
		return fn(&changeSet{e: e, code: code})
	})
}

// WithProjectBirth establishes a brand-new v2 project: plain project lock
// (never the format gate — the format is being ESTABLISHED), entry written
// before the first append, best-effort entry rollback if fn fails before the
// root event committed. Exactly createProjectV2's crash-window contract.
func (e *Engine) WithProjectBirth(code string, fn func(core.ChangeSet) error) error {
	return e.WithLock(code, func() error {
		if err := e.SetProjectFormat(code, StoreFormatV2); err != nil {
			return err
		}
		cs := &changeSet{e: e, code: code}
		if err := fn(cs); err != nil {
			if !cs.rootCommitted {
				_ = e.RemoveProjectFormat(code)
			}
			return err
		}
		return nil
	})
}
```

- [ ] **Step 4: Split the projector; add the facade projection helpers**

In `internal/store/eventsource_projector.go` (rename the file to `internal/store/cache_project.go` with `git mv`): delete `reprojectV2Locked`, `cacheProjectFromV2State`, the four `*FromV2` converters (moved in Step 2), and rewrite the db writer to consume the snapshot. Keep `cacheDeleteProjectRows` verbatim:

```go
// projectSnapshot replaces the project's cache rows from a domain snapshot.
// Caller MUST hold the project lock (or be the cacheDB migration, which uses
// the DB-taking variant).
func (s *Store) projectSnapshot(code string, snap *core.ProjectSnapshot) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.projectSnapshotDB(db, code, snap)
}

func (s *Store) projectSnapshotDB(db *sql.DB, code string, snap *core.ProjectSnapshot) error {
	if err := cacheDeleteProjectRows(db, code); err != nil {
		return err
	}
	if snap.Project != nil {
		if err := cacheUpsertProject(db, snap.Project); err != nil {
			return err
		}
	}
	for _, t := range snap.Tasks {
		if err := cacheUpsertTask(db, t); err != nil {
			return err
		}
	}
	for _, c := range snap.Comments {
		if err := cacheUpsertComment(db, c); err != nil {
			return err
		}
	}
	for _, name := range snap.RemovedLabels {
		if _, err := db.Exec(`DELETE FROM labels WHERE name = ?`, name); err != nil {
			return err
		}
	}
	for _, l := range snap.Labels {
		if err := cacheUpsertLabel(db, l); err != nil {
			return err
		}
	}
	return cacheSetV2Freshness(db, code, snap.ChangeCount)
}

// reprojectTxn is the in-transaction projection every mutator ends with —
// the old reprojectV2Locked, split across the seam: the engine folds
// (cs.Snapshot re-reads the file strictly), the facade projects.
func (s *Store) reprojectTxn(code string, cs core.ChangeSet) error {
	snap, err := cs.Snapshot()
	if err != nil {
		return err
	}
	return s.projectSnapshot(code, snap)
}
```

Keep a transitional `reprojectV2Locked` (sync/upgrade/read paths still call it until Tasks 5-6):

```go
func (s *Store) reprojectV2Locked(code string) error {
	snap, err := s.eng.Snapshot(code)
	if err != nil {
		return err
	}
	return s.projectSnapshot(code, snap)
}
```

ORDERING NOTE (verify while editing): `cacheProjectFromV2StateDB` upserted comments nested inside the task loop and labels after the task loop with tombstone-deletes interleaved. `projectSnapshotDB` preserves row-level results because upserts are independent rows keyed by id/name; the only order-sensitive part is delete-before-upsert per table, which is preserved (`cacheDeleteProjectRows` first, `RemovedLabels` deletes before label upserts).

- [ ] **Step 5: Flip the facade mutators**

`internal/store/task.go` — `createTaskV2` becomes (validation, guard order, and post-read all preserved):

```go
func (s *Store) createTaskV2(projectCode, title, description string, labels []string, actor string) (*Task, error) {
	var created *Task
	err := s.eng.WithProjectWrite(projectCode, func(cs core.ChangeSet) error {
		if err := cs.RequireProject(); err != nil {
			return err
		}
		if err := s.validateTaskLabelsV2Locked(projectCode, labels); err != nil {
			return err
		}
		if err := cs.EnsureLabels(labels, actor); err != nil {
			return err
		}
		alias, err := cs.CreateTask(core.TaskDraft{Title: title, Description: description, Labels: labels}, actor)
		if err != nil {
			return err
		}
		if err := s.reprojectTxn(projectCode, cs); err != nil {
			return err
		}
		db, err := s.cacheDB()
		if err != nil {
			return err
		}
		t, ok, err := cacheGetTask(db, alias)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: task %q", ErrNotFound, alias)
		}
		created = t
		return nil
	})
	return created, err
}
```

`mutateTask` (replaces `mutateTaskV2`; `SetTitle`/`SetDescription`/`TaskLabelRemove`/`RemoveTask` route through it with a per-intent closure):

```go
func (s *Store) mutateTask(id, actor string, do func(cs core.ChangeSet) error) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, _, err := s.taskProjectFormat(id)
	if err != nil {
		return err
	}
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := do(cs); err != nil {
			return err
		}
		return s.reprojectTxn(code, cs)
	})
}

func (s *Store) SetTitle(id, title, actor string) error {
	if title == "" {
		return fmt.Errorf("%w: title is required", ErrUsage)
	}
	return s.mutateTask(id, actor, func(cs core.ChangeSet) error { return cs.SetTaskTitle(id, title, actor) })
}
```

`taskLabelAddV2` (order preserved: resolve → validate → no-op → ensure → append):

```go
func (s *Store) taskLabelAddV2(code, id, label, actor string) error {
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := cs.ResolveTask(id); err != nil {
			return err
		}
		if err := s.validateTaskLabelsV2Locked(code, []string{label}); err != nil {
			return err
		}
		if has, err := cs.TaskHasLabel(id, label); err != nil {
			return err
		} else if has {
			return nil
		}
		if err := cs.EnsureLabels([]string{label}, actor); err != nil {
			return err
		}
		if err := cs.AddTaskLabel(id, label, actor); err != nil {
			return err
		}
		return s.reprojectTxn(code, cs)
	})
}
```

Apply the same transformation to `comment.go` (`createCommentV2` resolves task then reply-to via `cs.ResolveTask`/`cs.ResolveComment`, then `cs.EnsureLabels`, `cs.CreateComment`, `s.reprojectTxn`, cache read; `mutateComment` mirrors `mutateTask`; `commentLabelAddV2` mirrors `taskLabelAddV2` with `cs.CommentHasLabel`/`cs.AddCommentLabel`), to `label.go` (`labelUpsertV2` → `cs.UpsertLabel` with `LabelFields` built from the old payload keys; `labelSeedV2` → `cs.SeedLabel`; `labelRemoveV2` → `cs.RemoveLabel` + `reprojectTxn` + the retained-usage cache count, all inside the closure), and to `project.go`:

`createProjectV2` (birth):

```go
func (s *Store) createProjectV2(code, name, actor string) (*Project, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Project
	err = s.eng.WithProjectBirth(code, func(cs core.ChangeSet) error {
		if err := s.eng.MediaExists(code); err != nil {
			return err
		}
		if _, ok, err := cacheGetProject(db, code); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("%w: project %q already exists", ErrConflict, code)
		}
		if err := cs.CreateProject(name, actor); err != nil {
			return err
		}
		for _, l := range seed.Labels {
			expr := l.Expr
			f := core.LabelFields{Description: &l.Description}
			if expr != "" {
				f.Expr = &expr
			}
			if err := cs.UpsertLabel(code+":"+l.Suffix, f, actor); err != nil {
				return err
			}
		}
		if err := s.reprojectTxn(code, cs); err != nil {
			return err
		}
		p, err := s.getProjectLocked(code)
		if err != nil {
			return err
		}
		created = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
```

CAUTION: `&l.Description` on a range variable — take a local copy (`d := l.Description; f := core.LabelFields{Description: &d}`) to avoid aliasing across iterations. The old media/entry crash-window is preserved by `WithProjectBirth` (entry first, rollback only before the root commit) — but note the order change: the old code checked media/cache BEFORE writing the entry; `WithProjectBirth` writes the entry first and rolls it back when fn fails before the root commit, which is the same observable contract the old code documented (entry pointing at absent file is benign, and the rollback covers the media-exists failure path). Confirm `TestCreateProject*` and the live-write tests stay green — they pin this.

`removeProjectV2`:

```go
func (s *Store) removeProjectV2(code string) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.eng.WithProjectWrite(code, func(cs core.ChangeSet) error {
		if err := cs.RequireProject(); err != nil {
			return err
		}
		if has, err := cs.HasLiveTasks(); err != nil {
			return err
		} else if has {
			return fmt.Errorf("%w: project %q has tasks — remove tasks first", ErrConflict, code)
		}
		if err := os.RemoveAll(s.projectDir(code)); err != nil {
			return err
		}
		if err := cs.ForgetProject(); err != nil {
			return err
		}
		if err := cacheDeleteProjectRows(db, code); err != nil {
			return err
		}
		return cacheClearV2Freshness(db, code)
	})
}
```

Delete from store: `v2HasTasksGuardLocked`, `v2LiveProject` references, `projectMediaExists` (moved), the `V2Draft` usages, and the author-related bridge forwarders (`readV2File`? — still used by views/sync until Tasks 5-6; delete only `currentReplicaIDLocked` and anything now unused: let the compiler and `go vet` guide). Remove `"atm/libs/eventsource"` from task.go/comment.go/label.go/project.go imports.

- [ ] **Step 6: Verify hard**

Run: `go build ./... && go test ./internal/store/... && go test ./internal/cli/ ./internal/tui/`
Expected: PASS — in particular `store_seams_test.go` (reproducible aliases through the new seam threading), `eventsource_live_write_test.go`, `eventsource_e2e_test.go`, and the full cli golden suite (mutation outputs byte-identical).
Also: `grep -rn '"atm/libs/eventsource"' internal/store/*.go | grep -v _test` — must now list ONLY `eventsource_read.go`, `eventsource_views.go`, `eventsource_sync.go`, `eventsource_upgrade.go`, `verify.go`.

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "refactor(ATM-3b873c): authoring behind core.ChangeSet; facade mutators on the engine txn"
```

---

### Task 5: Read side onto the engine — snapshot, log views, rebuild, verify

**Files:**
- Move: `internal/store/eventsource_views.go` → `internal/store/eventlog/views.go` (except `v2CompatEntities`, which stays — it is a cache read)
- Modify: `internal/store/{eventsource_read.go → delete after absorption, log.go, rebuild.go, verify.go, cache.go}`, `internal/core/storage.go` (create, first type: `LogView`), `internal/store/eventlog_bridge.go` (shrink)
- Move: `eventsource_views_test.go`, `eventsource_live_read_test.go` per the Task 3 Step 4 rule

**Interfaces:**
- Consumes: `Engine.Snapshot` (Task 4), `Engine.VerifyFile`/`ReadV2File`.
- Produces: `(*Engine).ChangeCount(code string) (int, error)`; `(*Engine).LogEntries(code string) ([]core.LogEntry, error)`; `(*Engine).DisplayLog(code string) ([]core.LogView, error)`; `core.LogView` (the old `store.V2LogView`, fields and JSON tags byte-identical); compile assert `var _ core.Journal = (*Engine)(nil)` added in eventlog. Facade: `LastLogSeq`, `readLogForViews`, `ReadV2LogForDisplay` (delegating, unchanged signatures for now), `rebuildProjectFromV2`, `v2CacheFresh`, `ensureV2CacheFresh`, `Verify`/`VerifyProject`, `Rebuild` all engine-backed.

- [ ] **Step 1: Create `internal/core/storage.go` with LogView**

```go
package core

import "time"

// Storage-maintenance read models (refactor step 6). Kept storage-neutral:
// core knows maintenance exists, never that persistence is event-sourced.
// Field order and JSON tags are frozen — CLI output marshals these directly.

// LogView is one row of `atm store log`: the change history in its
// deterministic total order, subjects rendered the way a user names them.
type LogView struct {
	Ordinal int       `json:"ordinal"`
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Actor   string    `json:"actor"`
	Action  string    `json:"action"`
	Subject string    `json:"subject"`
}
```

(Verify field-for-field against `V2LogView` in the source before deleting it — tags must match exactly.)

- [ ] **Step 2: Move the view/read internals into the engine**

`git mv internal/store/eventsource_views.go internal/store/eventlog/views.go`, then transform: `ReadV2LogForDisplay` → `(e *Engine) DisplayLog(code string) ([]core.LogView, error)` (returns `core.LogView`; body verbatim); `readV2LogEntries` → `(e *Engine) LogEntries(code string) ([]core.LogEntry, error)`; `readV2EventPrefix`/`v2LogEntriesFrom`/`v2SubjectDisplay` stay unexported engine helpers; `LogEntry`/`Subject` → `core.LogEntry`/`core.Subject`; `IsIntegrity` → `core.IsIntegrity`. MOVE `v2CompatEntities` back out first — cut it into `internal/store/search_compat.go` (new file, package store, body unchanged; it reads the cache).

Absorb `internal/store/eventsource_read.go`: `v2EventCount` → `(e *Engine) ChangeCount(code string) (int, error)` (body verbatim, reading `e.EventsV2Path(code)`); `rebuildProjectFromV2` becomes facade code in `rebuild.go`:

```go
func (s *Store) rebuildProjectFromV2(code string) error {
	snap, err := s.eng.Snapshot(code)
	if err != nil {
		return err
	}
	return s.projectSnapshot(code, snap)
}
```

`rebuildEntityCacheLocked`, `v2CacheFresh`, `ensureV2CacheFresh` move into `rebuild.go`/`cache.go` (facade — they gate on the cache) with `s.v2EventCount(code)` → `s.eng.ChangeCount(code)` and `s.projectFormat` via the bridge. Delete `eventsource_read.go`.

In `internal/store/log.go`: `LastLogSeq` body → `if _, err := s.eng.ProjectFormat(code); err != nil { return 0, err }; return s.eng.ChangeCount(code)`; `readLogForViews` → `if _, err := s.eng.ProjectFormat(code); err != nil { return nil, err }; return s.eng.LogEntries(code)`. `ReadLogCached`/`History`/`HistoryE`/`invalidateLogSnapshot`/`subjectMatch` unchanged. Keep `logPath` only if still referenced (upgrade/prune move in Task 6 — until then keep it).

In `internal/store/verify.go`: replace the `verifyV2File`+`FoldEvents` block of `VerifyProject` with one `s.eng.Snapshot(code)` call — on error: `if !IsIntegrity(err) { return nil, err }; report.LogOK = false; report.Diverged = true; return report, nil` (both old branches collapsed — they were identical); on success: `report.V2FileOK = true; report.V2Events = snap.ChangeCount; report.LogEntries = snap.ChangeCount; report.Caches = append(report.Caches, s.checkV2Cache(code, snap.ChangeCount)...)`. Change `checkV2Cache(code string, eventCount int)` (drop the unused `*eventsource.State` param). Drop verify.go's eventsource import.

In `internal/store/cache.go` + `rebuild.go`: the eager migration path (`reprojectAllV2`) flips from fold-based projection to `s.eng.Snapshot(code)` + `s.projectSnapshotDB(db, code, snap)` per project (keep the DB-taking variant — cacheDB is not reentrant).

Add `ReadV2LogForDisplay` transitional delegation to the bridge (cli still calls it until Task 8):

```go
type V2LogView = core.LogView

func (s *Store) ReadV2LogForDisplay(code string) ([]core.LogView, error) { return s.eng.DisplayLog(code) }
```

Add to `internal/store/eventlog/engine.go` (now that all methods exist):

```go
var _ core.Journal = (*Engine)(nil)
```

- [ ] **Step 3: Verify**

Run: `go build ./... && go test ./internal/store/... ./internal/cli/ ./internal/tui/`
Expected: PASS — live-read tests, verify tests, `store log` goldens byte-identical.
`grep -rn '"atm/libs/eventsource"' internal/store/*.go | grep -v _test` — must list ONLY `eventsource_sync.go`, `eventsource_upgrade.go`.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "refactor(ATM-3b873c): read side on Engine — Snapshot/LogEntries/DisplayLog/ChangeCount; verify+rebuild engine-backed"
```

---

### Task 6: Sync, upgrade, prune into the engine; cli loses eventsync

**Files:**
- Move: `internal/store/eventsource_sync.go` → `internal/store/eventlog/sync.go`; `internal/store/eventsource_upgrade.go` → `internal/store/eventlog/upgrade.go`; `internal/store/prune.go` → `internal/store/eventlog/prune.go`
- Create: `internal/store/eventlog/sync_remote.go` (SyncProject orchestration), additions to `internal/core/storage.go` (`SyncOptions`, `SyncReport`)
- Modify: `internal/store/store.go` (wire the hooks; facade delegations `SyncProject`, `UpgradeProjectToV2`, `UpgradeAllToV2`, `PruneProjectV1`), `internal/cli/store_sync.go` (drop eventsync)
- Move: sync/upgrade tests per the Task 3 Step 4 rule (`eventsource_sync_test.go` likely moves; `eventsource_sync_e2e_test.go` and `eventsource_upgrade_test.go` likely stay in store, driving facade methods — adapt call sites, never assertions)

**Interfaces:**
- Consumes: `eventsync` (`atm/libs/eventsource/sync`): `LocalStore`, `Sync`, `Options`, `Report`, `SelectTarget`.
- Produces: `core.SyncOptions{Pull, Push, DryRun bool}`; `core.SyncReport{Project string; Pulled, Pushed int; Bootstrapped bool; NewlyContested int; RemoteAbsent, DryRun bool; PushErr string}`; `(*Engine).SyncProject(ctx context.Context, code, url string, opts core.SyncOptions) (*core.SyncReport, error)`; `(*Engine).UpgradeProject(code) (*core.UpgradeReport-shape, error)` (type still `eventlog.UpgradeReport` until Task 7 moves it — see note), `(*Engine).UpgradeAll()`, `(*Engine).PruneLegacy(code string, del bool)`; facade `(*Store).SyncProject(ctx, code, url, opts)` and unchanged-signature delegations for `UpgradeProjectToV2`/`UpgradeAllToV2`/`PruneProjectV1`. Engine hooks wired: `OnProject`, `OnMediaReplaced`.

- [ ] **Step 1: Move the three files into the engine**

Transform as before (package, receivers, bridge-calls → direct engine calls, sentinels → core). The three `reprojectV2Locked(code)` call sites (SyncIngest, SyncBootstrap, and upgrade's `cacheProjectFromV2State` block) become:

```go
	// engine-internal projection: fold what is now on disk and hand it to
	// the facade's read-model hook (the pre-carve reprojectV2Locked site).
	if err := e.reprojectLocked(code); err != nil {
		return err
	}
```

with, in `sync.go`:

```go
func (e *Engine) reprojectLocked(code string) error {
	if e.opts.OnProject == nil {
		return nil
	}
	snap, err := e.Snapshot(code)
	if err != nil {
		return err
	}
	return e.opts.OnProject(code, snap)
}
```

EXCEPTION — upgrade.go step 4 already has `state` and `snap.EventCount` in hand from the TEMP file verification, and projected from those (`cacheProjectFromV2State(code, state, snap.EventCount)`) AFTER the rename: replace with `e.opts.OnProject(code, convertState(code, state, snap.EventCount))` (nil-guarded) — identical inputs, no re-read. Upgrade's vectors-wipe + `invalidateLogSnapshot` lines are replaced by `if e.opts.OnMediaReplaced != nil { e.opts.OnMediaReplaced(code) }` at the same position (after `SetProjectFormat`). `UpgradeReport`/`PruneReport` move with their files (exported from eventlog; the store bridge gets `type UpgradeReport = eventlog.UpgradeReport`, `type PruneReport = eventlog.PruneReport` so cli keeps compiling; Task 7 relocates them to core). `ErrSyncNeedsV2` moves to eventlog; bridge: `var ErrSyncNeedsV2 = eventlog.ErrSyncNeedsV2`. `projectMediaExists` calls → `e.MediaExists`. `UpgradeAllToV2` → `(e *Engine) UpgradeAll()` using `e.ProjectCodesOnDisk()`.

- [ ] **Step 2: Sync orchestration inside the engine**

Add to `internal/core/storage.go`:

```go
// SyncOptions selects sync direction; neither flag set means both.
type SyncOptions struct {
	Pull   bool
	Push   bool
	DryRun bool
}

// SyncReport is one project's sync outcome. PushErr carries a push-leg
// failure as text ("" = none); a hard failure is returned as an error
// instead of a report.
type SyncReport struct {
	Project        string
	Pulled         int
	Pushed         int
	Bootstrapped   bool
	NewlyContested int
	RemoteAbsent   bool
	DryRun         bool
	PushErr        string
}
```

Create `internal/store/eventlog/sync_remote.go`:

```go
package eventlog

import (
	"context"
	"path/filepath"

	"atm/internal/core"
	eventsync "atm/libs/eventsource/sync"
)

// SyncProject reconciles one project against the remote at url: transport
// selection and the set-union sync engine live HERE, so nothing above store
// ever names an event. The caller resolves remote names to URLs (that is
// project-config knowledge) and persists bootstrap origins from the report.
func (e *Engine) SyncProject(ctx context.Context, code, url string, opts core.SyncOptions) (*core.SyncReport, error) {
	target, err := eventsync.SelectTarget(filepath.Join(e.root, "remotes"), url)
	if err != nil {
		return nil, err
	}
	rep, err := eventsync.Sync(ctx, e, target, code, eventsync.Options{Pull: opts.Pull, Push: opts.Push, DryRun: opts.DryRun})
	if err != nil {
		return nil, err
	}
	out := &core.SyncReport{
		Project:        rep.Project,
		Pulled:         rep.Pulled,
		Pushed:         rep.Pushed,
		Bootstrapped:   rep.Bootstrapped,
		NewlyContested: rep.NewlyContested,
		RemoteAbsent:   rep.RemoteAbsent,
		DryRun:         rep.DryRun,
	}
	if rep.PushErr != nil {
		out.PushErr = rep.PushErr.Error()
	}
	return out, nil
}
```

(`eventsync.Sync(ctx, e, ...)` — the Engine, not the Store, is now the `eventsync.LocalStore`; `SyncSnapshot`/`SyncIngest`/`SyncBootstrap` moved onto it in Step 1. Check `eventsync.Report`'s exact field set when writing the copy — every field the CLI prints must be carried.)

- [ ] **Step 3: Wire hooks and facade delegations**

In `store.Open`, extend the engine construction:

```go
	s.eng = eventlog.New(abs, eventlog.Options{
		ClockNow:       s.clockNow,
		ReplicaEntropy: s.replicaEntropy,
		Now:            s.nowFn,
		OnProject:      func(code string, snap *core.ProjectSnapshot) error { return s.projectSnapshot(code, snap) },
		OnMediaReplaced: func(code string) {
			_ = os.RemoveAll(s.vectorsDir(code))
			s.invalidateLogSnapshot(code)
		},
	})
```

(Preserve the OLD upgrade ordering inside upgrade.go: projection, then `SetProjectFormat`, then media-replaced (wipe + invalidate) — re-check against the pre-move body: steps 4/5/6 there were rename → project → set-format → wipe → invalidate.)

Facade delegations in a new `internal/store/admin.go`:

```go
func (s *Store) SyncProject(ctx context.Context, code, url string, opts core.SyncOptions) (*core.SyncReport, error) {
	return s.eng.SyncProject(ctx, code, url, opts)
}
func (s *Store) UpgradeProjectToV2(code string) (*eventlog.UpgradeReport, error) {
	return s.eng.UpgradeProject(code)
}
func (s *Store) UpgradeAllToV2() ([]eventlog.UpgradeReport, error) { return s.eng.UpgradeAll() }
func (s *Store) PruneProjectV1(code string, del bool) (*eventlog.PruneReport, error) {
	return s.eng.PruneLegacy(code, del)
}
```

Delete the Store's `SyncSnapshot`/`SyncIngest`/`SyncBootstrap` (moved), and the old upgrade/prune method bodies.

- [ ] **Step 4: Flip `internal/cli/store_sync.go` off eventsync**

Remove the `eventsync` import. `syncResult.report` becomes `*core.SyncReport`; `opts := eventsync.Options{...}` becomes `opts := core.SyncOptions{Pull: pull, Push: push, DryRun: dryRun}`; delete the `remotesDir := filepath.Join(...)` line (and the now-unused `path/filepath` import); `runProjectSync` drops its `remotesDir` param and its target-selection block, calling:

```go
	report, err := s.SyncProject(ctx, code, url, opts)
```

`failed()` becomes `r.err != nil || (r.report != nil && r.report.PushErr != "")`; `toJSON` maps `PushError: rep.PushErr` directly; `emitSyncResults`' text branch prints `fmt.Fprintf(w, "  push failed: %s\n", rep.PushErr)` under `rep.PushErr != ""`. Every output string stays byte-identical. (`internal/cli` still imports `atm/internal/store` here until Task 8 — only the eventsync edge dies now.)

- [ ] **Step 5: Verify**

Run: `go build ./... && go test ./internal/store/... ./internal/cli/ && (cd libs/eventsource && go test ./...)`
Expected: PASS — sync e2e, upgrade, prune suites and the `store sync`/`store upgrade` goldens all byte-identical.
Checks: `grep -rn '"atm/libs/eventsource' internal/store/*.go internal/cli/*.go | grep -v _test` → no output (root store files and cli are clean); `grep -rln '"atm/libs/eventsource' internal | grep -v _test | grep -v 'internal/store/eventlog/'` → no output.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "refactor(ATM-3b873c): sync/upgrade/prune into the engine; OnProject+OnMediaReplaced hooks; cli drops eventsync"
```

---

### Task 7: `core.StorageAdmin` + report types to core

**Files:**
- Modify: `internal/core/storage.go` (interface + moved report types), `internal/store/verify.go`, `internal/store/rebuild.go`, `internal/store/eventlog/upgrade.go`, `internal/store/eventlog/prune.go`, `internal/store/admin.go`, `internal/store/eventlog_bridge.go` (aliases for the moved types)

**Interfaces:**
- Consumes: everything above.
- Produces: `core.StorageAdmin` (below); `core.VerifyReport`, `core.CacheCheck`, `core.VectorIndexInfo`, `core.RebuildReport`, `core.UpgradeReport`, `core.PruneReport` — all field-for-field and tag-for-tag identical to today's store/eventlog types, except `Format` fields typed `string` (was `StoreFormat`, itself a string kind — JSON output unchanged). `var _ core.StorageAdmin = (*Store)(nil)`. Task 8's cli flip consumes exactly these names.

- [ ] **Step 1: Move the report types to core**

Append to `internal/core/storage.go`: `VerifyReport` + `CacheCheck` + `VectorIndexInfo` (from `internal/store/verify.go`, verbatim; `Format StoreFormat` → `Format string`), `RebuildReport` (from `rebuild.go`), `UpgradeReport` (from `eventlog/upgrade.go`; `Format StoreFormat` → `Format string`), `PruneReport` (from `eventlog/prune.go`). Delete the originals; in eventlog, `rep := &UpgradeReport{...}` becomes `rep := &core.UpgradeReport{Project: code, Format: string(StoreFormatV2)}` (and similarly wherever a `StoreFormat` value lands in a report field — wrap with `string(...)`). In the store bridge add `type VerifyReport = core.VerifyReport` etc. for all six, so cli compiles unchanged until Task 8.

- [ ] **Step 2: Declare StorageAdmin and implement it on Store**

Append to `internal/core/storage.go`:

```go
// StorageAdmin is the storage-maintenance seam the CLI's `atm store ...`
// command group consumes, beside Service (the composition root wires both to
// the same concrete store). Format identifiers cross as opaque strings the
// store validates.
type StorageAdmin interface {
	VerifyStorage() ([]VerifyReport, error)
	VerifyStorageProject(project string) (*VerifyReport, error)
	RebuildDerived() (*RebuildReport, error)
	UpgradeStorage(project string) (*UpgradeReport, error)
	UpgradeAllStorage() ([]UpgradeReport, error)
	PruneLegacy(project string, del bool) (*PruneReport, error)
	SetStorageFormat(format string) error
	StorageFormat(project string) (string, error)
	ReadChangeLog(project string) ([]LogView, error)
	SyncProject(ctx context.Context, project, url string, opts SyncOptions) (*SyncReport, error)
}
```

(add `"context"` to core/storage.go imports.)

In `internal/store/admin.go`, add the interface methods as thin delegations to the existing exported methods (which stay — store tests use them):

```go
var _ core.StorageAdmin = (*Store)(nil)

func (s *Store) VerifyStorage() ([]core.VerifyReport, error)          { return s.Verify() }
func (s *Store) VerifyStorageProject(p string) (*core.VerifyReport, error) { return s.VerifyProject(p) }
func (s *Store) RebuildDerived() (*core.RebuildReport, error)         { return s.Rebuild() }
func (s *Store) UpgradeStorage(p string) (*core.UpgradeReport, error) { return s.UpgradeProjectToV2(p) }
func (s *Store) UpgradeAllStorage() ([]core.UpgradeReport, error)     { return s.UpgradeAllToV2() }
func (s *Store) SetStorageFormat(format string) error {
	return s.eng.SetActiveFormat(eventlog.StoreFormat(format))
}
func (s *Store) StorageFormat(p string) (string, error) {
	f, err := s.eng.ProjectFormat(p)
	return string(f), err
}
func (s *Store) ReadChangeLog(p string) ([]core.LogView, error) { return s.eng.DisplayLog(p) }
```

(`PruneLegacy` already matches by name — rename the Task 6 delegation if needed so the set is exact; `SyncProject` from Task 6 already matches. `SetActiveFormat`'s unknown-format `ErrUsage` message must remain exactly `unknown store format %q`.)

- [ ] **Step 3: Verify + commit**

Run: `go build ./... && go test ./internal/store/... ./internal/cli/ ./internal/core/`
Expected: PASS, goldens untouched.

```bash
git add -A && git commit -m "refactor(ATM-3b873c): core.StorageAdmin + storage report types in core; Store implements"
```

---

### Task 8: Sever cli→store — injected constructors, full type flip

**Files:**
- Modify: `internal/cli/root.go` (Deps, cliState, openStore/openAdmin, launchTUI), `internal/cli/env.go`, `internal/cli/init.go`, `internal/cli/store.go`, `internal/cli/store_integrity.go`, `internal/cli/store_migrate.go`, `internal/cli/store_sync.go`, `internal/cli/errors.go`, `internal/cli/output.go`, `internal/cli/task.go`, `internal/cli/comment.go`, `internal/cli/project.go`, `internal/cli/vocabulary.go`, `internal/cli/search.go`, `internal/cli/agent_resolve.go`, `internal/cli/developing.go`, `internal/cli/manager.go`, `internal/cli/launcher_shared.go`, `cmd/atm/main.go`, `internal/cli/harness_test.go` (+ any test files that construct `cliState`)

**Interfaces:**
- Consumes: `core.Service`, `core.StorageAdmin`, `core.MarshalSorted`, all core types/sentinels.
- Produces: `Deps{RunTUI; Registry; OpenService func(storePath string) (core.Service, error); OpenAdmin func(storePath string) (core.StorageAdmin, error)}`; `(*cliState).openStore() (core.Service, error)`; `(*cliState).openAdmin() (core.StorageAdmin, error)`. After this task NO production file in internal/cli imports `atm/internal/store`.

- [ ] **Step 1: Deps + cliState plumbing**

In `internal/cli/root.go`: drop the `atm/internal/store` import. `Deps` gains:

```go
type Deps struct {
	RunTUI func(storePath, actor string) error
	// Registry holds the capability commands the composition root enabled;
	// nil behaves as empty (no capability commands mount).
	Registry *capability.Registry
	// OpenService / OpenAdmin construct the store for a --store/ATM_HOME
	// path (unresolved; the constructor resolves it). The CLI never names
	// the concrete store — the composition root injects these.
	OpenService func(storePath string) (core.Service, error)
	OpenAdmin   func(storePath string) (core.StorageAdmin, error)
}
```

`cliState`: delete `storeOpts []store.Option`; add `openServiceFn func(string) (core.Service, error)` and `openAdminFn func(string) (core.StorageAdmin, error)`; `Execute` copies them from `deps`. Replace `openStore`:

```go
func (s *cliState) openStore() (core.Service, error) {
	if s.openServiceFn == nil {
		return nil, fmt.Errorf("store opener not wired (composition root must set Deps.OpenService)")
	}
	return s.openServiceFn(s.flags.store)
}

func (s *cliState) openAdmin() (core.StorageAdmin, error) {
	if s.openAdminFn == nil {
		return nil, fmt.Errorf("store opener not wired (composition root must set Deps.OpenAdmin)")
	}
	return s.openAdminFn(s.flags.store)
}
```

`launchTUI`: `root := store.ResolveStorePath(s.flags.store)` → `root := s.flags.store` (cmd/atm's RunTUI already resolves; verify by reading main.go — it calls `store.ResolveStorePath(storePath)`).

`cmd/atm/main.go` becomes:

```go
package main

import (
	"os"

	"atm/internal/capability"
	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"
	"atm/internal/cli"
	"atm/internal/core"
	"atm/internal/store"
	"atm/internal/tui"
)

// main is the composition root: it constructs the concrete store, assembles
// the capability registry, and hands the adapters their dependencies. No
// domain or presentation logic here.
func main() {
	reg := capability.NewRegistry(workflow.New(), contextmap.New())
	open := func(storePath string) (*store.Store, error) {
		return store.Open(store.ResolveStorePath(storePath))
	}
	openService := func(storePath string) (core.Service, error) {
		s, err := open(storePath)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	openAdmin := func(storePath string) (core.StorageAdmin, error) {
		s, err := open(storePath)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	runTUI := func(storePath, actor string) error {
		s, err := open(storePath)
		if err != nil {
			return err
		}
		return tui.Run(s, actor, reg)
	}
	os.Exit(cli.Execute(cli.Deps{RunTUI: runTUI, Registry: reg, OpenService: openService, OpenAdmin: openAdmin}))
}
```

(The error paths return explicit `nil` so a typed-nil `*store.Store` never hides in the interface.)

- [ ] **Step 2: Flip the admin commands to `openAdmin` + core types**

`store.go`: the `path` command body becomes `s, err := st.openStore(); ... s.StorePath()` (same output — `StorePath` is on `core.MaintenanceService`). `store_integrity.go`: `st.openStore()` stays for nothing — it uses `ReadV2LogForDisplay`/`VerifyProject`/`Verify`/`Rebuild`: switch to `st.openAdmin()` with `ReadChangeLog`/`VerifyStorageProject`/`VerifyStorage`/`RebuildDerived`; `store.V2LogView` → `core.LogView`; `store.RFC3339UTC` → `core.RFC3339UTC`; `store.VerifyReport` → `core.VerifyReport`; `store.ErrIntegrity` → `core.ErrIntegrity`. `store_migrate.go`: `openAdmin()`; `UpgradeStorage`/`UpgradeAllStorage`/`PruneLegacy`/`SetStorageFormat(format)`; `store.UpgradeProjectToV2` messages/flags unchanged; `store.PruneReport` → `core.PruneReport`; the `--all` upgrade loop keeps printing identical lines. `store_sync.go`: `st.openStore()` keeps serving remotes/codes (core.Service); the sync call becomes `admin, err := st.openAdmin()` + `admin.SyncProject(...)`; `store.ErrUsage` → `core.ErrUsage`; `syncProjectCodes`/`listProjectRemotes`/`resolveSyncRemote`/`runProjectSync` signatures take `core.Service` (+ `core.StorageAdmin` where they sync).

- [ ] **Step 3: Flip the remaining production files**

For each of `errors.go`, `output.go`, `task.go`, `comment.go`, `project.go`, `vocabulary.go`, `search.go`, `agent_resolve.go`, `developing.go`, `manager.go`, `launcher_shared.go`, `init.go`, `env.go`: replace the `"atm/internal/store"` import with `"atm/internal/core"` (where not already present) and `store.` → `core.` for every remaining reference — they are all types (`core.Task`, `core.QueryFilters`, `core.AgentsConfig`, ...), sentinels (`core.ErrUsage`, `core.IsNotFound`, ...), and helpers (`core.RFC3339UTC`, `core.MarshalSorted`, `core.ParseExpr`, `core.ParseCommentID`, `core.IsNamespaceName`). `init.go` additionally: `store.Open(store.ResolveStorePath(...), st.storeOpts...)` → `st.openStore()`; `persistInitSetup(s *store.Store, ...)` → `persistInitSetup(s core.Service, ...)` (its body uses only Service methods — the compiler confirms). Where a call was `store.Open` + method chain, the `core.Service` returned by `openStore` serves it.

Verification: `grep -rn '"atm/internal/store"' internal/cli/*.go | grep -v _test` → no output. `go build ./...` — fix residuals compiler-first.

- [ ] **Step 4: Rewire the test harness**

`internal/cli/harness_test.go` (test files MAY import store): where the harness built `st := &cliState{flags: ..., registry: testRegistry(), storeOpts: opts}`, construct instead:

```go
	openService := func(p string) (core.Service, error) {
		s, err := store.Open(store.ResolveStorePath(p), opts...)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	openAdmin := func(p string) (core.StorageAdmin, error) {
		s, err := store.Open(store.ResolveStorePath(p), opts...)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	st := &cliState{flags: globalFlags{output: outputJSON}, registry: testRegistry(), openServiceFn: openService, openAdminFn: openAdmin}
```

(mirror whatever the harness's current seam-threading looks like — the golden harness sets `storeOpts` today precisely so events authored inside command execution are reproducible; the closures must keep passing the same opts). Fix every other `_test.go` that set `storeOpts` or constructed `cliState` the old way (`grep -rn 'storeOpts' internal/cli`).

- [ ] **Step 5: Verify byte parity hard**

Run: `go build ./... && go test ./internal/cli/ ./internal/store/... ./internal/tui/ ./tests/arch/`
Expected: PASS with ZERO golden diffs (`git status --porcelain internal/cli/testdata` empty).

```bash
go build -o "$SCRATCH/atm-after8" ./cmd/atm
for c in "store" "store path" "store log" "store verify" "store rebuild" "store upgrade" "store prune-v1" "store set-format" "store remote" "store remote add" "store remote list" "store remote remove" "store sync"; do echo "=== atm $c --help ==="; "$SCRATCH/atm-after8" $c --help; done > "$SCRATCH/store-help-after.txt" 2>&1
diff "$SCRATCH/store-help-before.txt" "$SCRATCH/store-help-after.txt"
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "refactor(ATM-3b873c): sever cli->store — OpenService/OpenAdmin injection, cli on core types only"
```

---

### Task 9: Delete the aliases; de-alias store internals; arch tests

**Files:**
- Delete: `internal/store/core_aliases.go`, `internal/store/eventlog_bridge.go` (or shrink to what store internals still genuinely need — target: delete)
- Modify: every `internal/store/*.go` still using an alias; `tests/arch/imports_test.go`

**Interfaces:**
- Consumes: everything in place.
- Produces: arch rules `TestCLIDoesNotImportStore`, `TestOnlyEventlogImportsEventsourceLib`; store internals reference `core.X` / `eventlog.X` / `fsio.X` directly.

- [ ] **Step 1: De-alias package store**

Delete `internal/store/core_aliases.go`. Then flip store internals compiler-first — for each identifier the aliases provided (`ErrNotFound`, `ErrConflict`, `ErrIntegrity`, `ErrUsage`, `IsNotFound`, `IsConflict`, `IsIntegrity`, `IsUsage`, `Now`, `RFC3339UTC`, `TaskIDRe`, `ParseTaskID`, `CommentIDRe`, `ParseCommentID`, `ValidatePersonaName`, `IsNamespaceName`, `ParseExpr`, `Atoms`, and the type aliases `Task`/`Label`/`Comment`/... ):

```bash
cd internal/store
sed -i -E 's/\b(ErrNotFound|ErrConflict|ErrIntegrity|ErrUsage|IsNotFound|IsConflict|IsIntegrity|IsUsage|RFC3339UTC|TaskIDRe|ParseTaskID|CommentIDRe|ParseCommentID|ValidatePersonaName|IsNamespaceName|ParseExpr|Atoms)\b/core.\1/g' *.go
```

then fix over-eager replacements (`core.core.`, occurrences inside comments/strings that matter, the package's own `Now()` uses — `Now` is deliberately excluded from the sed; flip its uses by hand where they meant `core.Now`) and add `"atm/internal/core"` imports where missing. Domain-type aliases: keep `type Task = core.Task` etc.? NO — delete them too and qualify (`*core.Task` in signatures). This is wide but mechanical; the compiler is the checklist. EXCEPTION: exported API compatibility for store package consumers — after Task 8 the only production consumers of package store are `cmd/atm` (via interfaces) and tests; store tests may be updated mechanically with the same sed. If the churn balloons, keeping ONLY the pure type aliases (`Task`, `Label`, `Comment`, `Project`, ...) in a single `types_compat.go` with a doc comment is an acceptable landing point — but the error sentinels and function aliases MUST go.
Same treatment for `eventlog_bridge.go`: inline each remaining forwarder's call site (`s.eng.X(...)`) and delete the file; `V2LogView`, `StoreFormat` etc. aliases go — remaining store references say `eventlog.StoreFormatV2` directly. If `ReadV2LogForDisplay`/`UpgradeProjectToV2`/... still have store-test callers, keep those as real methods in `admin.go` (they are the StorageAdmin delegation targets anyway).

- [ ] **Step 2: Arch tests**

In `tests/arch/imports_test.go`, add (and update the `TestTUIDoesNotImportStore` doc comment's "Tighten it when steps 5-6 land" sentence to reflect that step 6 landed):

```go
// TestCLIDoesNotImportStore is refactor step 6's boundary: the CLI consumes
// core.Service + core.StorageAdmin, both injected by cmd/atm. Neither the
// concrete store nor any of its subpackages may be named by cli production
// files.
func TestCLIDoesNotImportStore(t *testing.T) {
	for f, imps := range internalImports(t, "internal/cli") {
		for _, p := range imps {
			if p == "atm/internal/store" || strings.HasPrefix(p, "atm/internal/store/") {
				t.Errorf("%s imports %q; internal/cli consumes core interfaces injected by the composition root", f, p)
			}
		}
	}
}

// TestOnlyEventlogImportsEventsourceLib pins the carve: the event-sourcing
// library is an implementation detail of internal/store/eventlog. Nothing
// else in the module — the store facade included — may name it.
func TestOnlyEventlogImportsEventsourceLib(t *testing.T) {
	for _, dir := range []string{
		"cmd/atm", "internal/activity", "internal/actor", "internal/agent",
		"internal/capability", "internal/capability/contextmap", "internal/capability/workflow",
		"internal/cli", "internal/core", "internal/developing", "internal/embed",
		"internal/manager", "internal/seed", "internal/store", "internal/store/fsio",
		"internal/tui", "internal/tui/components", "internal/version",
	} {
		for f, imps := range internalImports(t, dir) {
			for _, p := range imps {
				if strings.HasPrefix(p, "atm/libs/eventsource") {
					t.Errorf("%s imports %q; only internal/store/eventlog may import the eventsource library", f, p)
				}
			}
		}
	}
}
```

(Adjust the directory list against the real tree: `ls internal internal/tui internal/capability` — every package directory with .go files except `internal/store/eventlog` must be covered; if a listed dir has no .go files `internalImports` fails loudly — drop it from the list.)

- [ ] **Step 3: Verify + commit**

Run: `go build ./... && go test ./... && (cd libs/eventsource && go test ./...)`
Expected: PASS everywhere, goldens untouched. `grep -rn '"atm/internal/store"' internal/cli --include='*.go' | grep -v _test` → empty.

```bash
git add -A && git commit -m "test(ATM-3b873c): delete step-4 aliases; arch tests pin cli->store severed and eventsource behind eventlog"
```

---

### Task 10: Docs, smoke, no-lost-tests, ledger close-out

**Files:**
- Modify: `docs/architecture/logical-components.md` (import table + component rows), `docs/superpowers/specs/2026-07-17-store-eventlog-carve-design.md` (fsio naming refinement note)

- [ ] **Step 1: Amend the architecture doc**

In `docs/architecture/logical-components.md`:
- Component table: `internal/store` row's "Responsibility" now says it implements core's repositories via the `internal/store/eventlog` engine; add rows for `internal/store/eventlog` (the only importer of `libs/eventsource`; owns events.v2.jsonl + store.json + sync/upgrade) and `internal/store/fsio` (lock + atomic JSON leaf).
- Import table: `internal/cli` row drops "`store` only until step 6 moves the remaining admin surface behind interfaces" → `core`, `capability` (registry only), satellites; add `internal/store/eventlog` → `core`, `store/fsio`, `libs/eventsource`; add `internal/store/fsio` → `core`; `internal/store` → `core`, `store/eventlog`, `store/fsio`, `seed`.
- Migration table: no wording change needed for row 6 (it describes this step); if the surrounding prose says step 6 is pending, update it to say the migration is complete.

In the spec, append one line under "Target shape": the lock leaf landed as `internal/store/fsio` (lock + atomic JSON + `core.MarshalSorted`), a refinement of the spec's `fslock` name discovered when the JSON helpers turned out to be shared by both sides.

- [ ] **Step 2: No-lost-tests check**

```bash
grep -rho '^func Test[A-Za-z0-9_]*' --include='*_test.go' internal tests | sort > "$SCRATCH/tests-after.txt"
comm -23 "$SCRATCH/tests-before.txt" "$SCRATCH/tests-after.txt"
```

Expected: empty (renames count as losses — if a test was genuinely renamed during a move, document each one in the commit message; deletions are not acceptable).

- [ ] **Step 3: Golden zero-diff check**

```bash
go test ./internal/cli/ -update && git status --porcelain internal/cli/testdata
```

Expected: `git status` output EMPTY — regenerating goldens changes nothing. If `TestRootNoArgsLaunchesTUI*` fails under `-update` (pre-existing flake noted on main), rerun without `-update` to confirm green and note it. Revert any incidental golden touch: `git checkout internal/cli/testdata`.

- [ ] **Step 4: Smoke test on a throwaway store**

```bash
go build -o "$SCRATCH/atm" ./cmd/atm
export ATM_HOME=$(mktemp -d)
"$SCRATCH/atm" project create --code DEMO --name Demo --actor admin@smoke
"$SCRATCH/atm" task create --project DEMO --title "smoke task" --actor developer@smoke
"$SCRATCH/atm" task list --project DEMO
"$SCRATCH/atm" store verify
"$SCRATCH/atm" store log DEMO
"$SCRATCH/atm" store rebuild
REMOTE=$(mktemp -d)
"$SCRATCH/atm" store remote add origin "$REMOTE" --project DEMO --actor admin@smoke
"$SCRATCH/atm" store sync --project DEMO --actor admin@smoke
ATM_HOME=$(mktemp -d) "$SCRATCH/atm" store sync "$REMOTE" --project DEMO --actor admin@smoke   # bootstrap pull into a second store
unset ATM_HOME
```

Expected: every command succeeds; the second store bootstraps DEMO (sync line reports pulled events); verify reports no divergence. (Exact flag names: check `--help` if `project create`/`task create` flags differ — use whatever the current CLI surface is; the point is a create→mutate→verify→sync round trip through the carved engine.)

- [ ] **Step 5: Final verify + commit + ledger**

Run: `make verify` — Expected: green (all packages + script tests).

```bash
git add -A && git commit -m "docs(ATM-3b873c): amend architecture doc for the eventlog carve; spec fsio note"
```

Then record completion on the ledger (adjust wording to reality):

```bash
atm task comment add --task ATM-3b873c --actor "developer@claude:<model>" --label "ATM:comment:progress" --body "Implementation complete on branch atm-3b873c-store-eventlog-carve: <summary of what landed, deviations, verification evidence>. Ready for merge review."
```

Do NOT merge to main in this task — merging is a separate user decision (superpowers:finishing-a-development-branch).

---

## Self-review notes (already applied)

- **Spec coverage:** engine subpackage (T3-6), only-importer rule (T9), intent contract (T2/T4), StorageAdmin + severed cli (T7-8), core_aliases deletion (T9), projection-under-lock preserved (T4's `reprojectTxn` at the exact old call sites; hook only for engine-internal paths — a refinement of the spec's "hook on every path", recorded in the spec amendment in T10 if the executor prefers; the observable contract is identical), byte parity (T1 baseline, T8/T10 gates), arch tests (T9), docs (T10).
- **Known judgment points for the executor:** exact exported-name choices inside eventlog may drift from this plan where the compiler demands (e.g. parameter types of `AppendEventLineLocked`); the LIST of files still importing eventsource after each task is the real invariant. Test files moving between packages follow the "compiles against Engine alone" rule. `WithProjectBirth`'s entry-first ordering vs the old check-first ordering is contract-equivalent (documented in T4 Step 5) — if any test pins the difference, restore check-first by exposing the checks before `SetProjectFormat` inside `WithProjectBirth` via a `preflight func() error` option and re-verify.
- **Type consistency:** `core.ChangeSet`/`core.Journal` (T2) are consumed with those exact names in T4-6; `core.LogView` (T5) in T7-8; `core.SyncOptions`/`SyncReport` (T6) in T7-8; `Deps.OpenService/OpenAdmin` (T8) in harness + main.
