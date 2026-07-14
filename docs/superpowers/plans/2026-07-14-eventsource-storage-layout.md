# EventSource Storage Layout v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewire ATM's live store to use side-by-side v2 EventSource storage while preserving v1 `log.jsonl` for rollback and re-upgrade, after the ATM-0125 alias-authoring blocker is resolved.

**Architecture:** Each project gets `projects/<CODE>/events.v2.jsonl` as the canonical v2 source of truth, with v1 `projects/<CODE>/log.jsonl` left untouched. `internal/store` remains the API consumed by CLI/TUI and dispatches by active project format; `cache.db` stays derived and rebuildable from either v1 replay or v2 fold. Upgrade writes a temp v2 file, verifies it, rebuilds cache, and activates v2 only after all checks pass.

**Tech Stack:** Go 1.22+, existing `modernc.org/sqlite` cache, existing per-project `WithLock`, existing Cobra CLI, and the ATM-0106 `internal/eventsource` package.

**Revised 2026-07-14** against the merged `internal/eventsource` API (commit `88f9b1b`, post ATM-0125): state types embed `EntityMeta` (no `Meta` field), comments carry `TaskRef`/`ReplyToRef` identities (not aliases), labels use `Expr` (not `Expression`), `TaskState` has no `ProjectCode`, creation helpers take a trailing `taken func(string) bool`, and `CommentsByCreation(taskRef)` filters by task identity. The revamp also fixes the partial-tail repair logic, reorders upgrade verification before cutover, adds the spec-required semantic comparison against v1 replay, makes projection delete-then-insert, rebuilds the cache on rollback, and threads alias-to-identity resolution through v2 authoring.

## Global Constraints

- Do not start implementation until ATM-0106 has landed, `internal/eventsource` exists, and ATM-0125 is closed.
- ATM-0125 is a hard blocker for this plan because L3 authors the first v2-native `task.created` and `comment.created` events; those events cannot safely mint stored aliases until ATM-0125 amends the L1 minting rule and adds the core authoring helper.
- `projects/<CODE>/log.jsonl` is preserved untouched by upgrade, rollback, and re-upgrade.
- `projects/<CODE>/events.v2.jsonl` stores one canonical raw v2 event JSON object per line and is the v2 source of truth.
- `cache.db`, `eventsource.json`, v2 freshness rows, frontier caches, alias indexes, and vector indexes are derived and rebuildable.
- All v2 event identity, canonicalization, DAG construction, fold semantics, HLC comparison, and v1 upgrade logic must use `internal/eventsource`; do not duplicate core logic in `internal/store`.
- A writer must derive frontier and observe HLC while holding `projects/<CODE>.lock`.
- A complete newline-terminated fsynced event line is the v2 append commit point.
- A malformed complete v2 event line, hash mismatch, missing parent, or DAG validation failure is an integrity error.
- Rollback does not export v2-only writes into v1.
- Re-upgrade after rollback archives/replaces the old v2 file and rebuilds from the current v1 log.
- README upgrade/rollback instructions are part of this implementation.
- Run `gofmt` on changed Go files and `make verify` before declaring done.

---

## File Structure

Create focused store files:

- `internal/store/eventsource_meta.go`: v2 paths, store/project metadata, active-format detection, atomic metadata writes.
- `internal/store/eventsource_file.go`: v2 JSONL read, verify, append, partial-tail recovery, archive/replace helpers.
- `internal/store/eventsource_upgrade.go`: v1-to-v2 upgrade, semantic comparison, cutover, rollback, re-upgrade.
- `internal/store/eventsource_projector.go`: conversion from `eventsource.State` to existing `Project`, `Task`, `Comment`, and `Label` cache rows.
- `internal/store/eventsource_author.go`: v2 authoring helpers, frontier/HLC refresh, parent selection, raw append, metadata/cache refresh.
- `internal/store/eventsource_replica.go`: store instance marker and replica-copy detection/reminting.

Modify existing files:

- `internal/store/store.go`: initialize metadata and expose active-format helpers.
- `internal/store/cache.go`: add guarded schema migrations for identity/freshness support.
- `internal/store/rebuild.go`: rebuild v1 projects from v1 replay and v2 projects from v2 fold.
- `internal/store/verify.go`: verify active format, v2 event file integrity, and cache freshness.
- `internal/store/log.go`: keep v1 APIs; make `ReadLog` explicitly v1-only in comments and add v2 log streaming helpers.
- `internal/store/project.go`, `task.go`, `comment.go`, `label.go`: dispatch mutators/read freshness by active project format.
- `internal/cli/store.go`: add `store upgrade` and `store rollback`, update `store log` for v2 display.
- `internal/cli/conventions.go`: mention upgrade/rollback and v2 event source.
- `README.md`: add the approved v1-to-v2 upgrade runbook.

---

### Task 0: Preflight Gate

**Files:**
- Read: `internal/eventsource/*`
- Read: `atm task show --task ATM-0125 --output json`
- Read: `docs/eventsource/01-core-data-model.md`
- Read: `docs/eventsource/02-storage-layout.md`

**Interfaces:**
- Consumes: ATM-0106 package `internal/eventsource` and the ATM-0125 alias-authoring fix.
- Produces: A verified starting point; no repository change.

- [ ] **Step 1: Confirm ATM-0125 is closed**

Run:

```bash
atm task show --task ATM-0125 --output json
```

Expected: task labels include `ATM:status:done`. If ATM-0125 is still open, stop. Do not execute L3 storage tasks, because v2-native task/comment creation would mint aliases from an unstable preimage.

- [ ] **Step 2: Confirm ATM-0106 core package exists**

Run:

```bash
test -d internal/eventsource
go test ./internal/eventsource
```

Expected: both commands succeed. If `internal/eventsource` is missing, stop and implement/merge ATM-0106 first.

- [ ] **Step 3: Confirm expected ATM-0125 authoring API**

Run:

```bash
rg -n "func UpgradeV1|func BuildDAG|func Fold|func FoldEvents|func NewEvent|func NewTaskCreated|func NewCommentCreated|func MintReplicaID|type State|type Event|type Draft|type Clock" internal/eventsource
```

Expected: matches for all listed functions/types. Confirmed 2026-07-14 against commit `88f9b1b`: the helpers exist as `NewTaskCreated(clock, replica, parents, TaskCreateDraft, taken)` and `NewCommentCreated(clock, replica, parents, CommentCreateDraft, taken)` — note the trailing `taken func(string) bool` (nil-safe), plus `NewProjectCreated(clock, replica, parents, ProjectCreateDraft)`. `CommentCreateDraft` requires `TaskAlias` and `TaskRef` (task identity) and takes `ReplyToRef` (comment identity) — there is no `ProjectCode` or `ReplyToAlias` field. If any of this no longer matches, stop and re-review; L3 must not rediscover alias ordering locally.

- [ ] **Step 4: Confirm baseline verification**

Run:

```bash
go build ./...
go test ./...
```

Expected: all packages pass before L3 changes.

---

### Task 1: v2 Paths, Metadata, and Active Format

**Files:**
- Create: `internal/store/eventsource_meta.go`
- Create: `internal/store/eventsource_meta_test.go`
- Modify: `internal/store/store.go`

**Interfaces:**
- Consumes: `Store.Root`, `projectDir`, `WriteJSON`/`ReadJSON` if available, standard `os.Rename`.
- Produces:
  - `type StoreFormat string`
  - `const StoreFormatV1 StoreFormat = "v1"`
  - `const StoreFormatV2 StoreFormat = "v2"`
  - `type StoreMeta struct`
  - `type ProjectEventsourceMeta struct`
  - `func (s *Store) storeMetaPath() string`
  - `func (s *Store) eventsV2Path(code string) string`
  - `func (s *Store) eventsourceMetaPath(code string) string`
  - `func (s *Store) readStoreMeta() (*StoreMeta, error)`
  - `func (s *Store) writeStoreMeta(m *StoreMeta) error`
  - `func (s *Store) projectFormat(code string) (StoreFormat, error)`
  - `func (s *Store) setProjectFormat(code string, f StoreFormat) error`

- [ ] **Step 1: Write failing metadata tests**

Create `internal/store/eventsource_meta_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEventsourcePaths(t *testing.T) {
	s := testStore(t)
	if got, want := s.eventsV2Path("ATM"), filepath.Join(s.StorePath(), "projects", "ATM", "events.v2.jsonl"); got != want {
		t.Fatalf("eventsV2Path = %q, want %q", got, want)
	}
	if got, want := s.eventsourceMetaPath("ATM"), filepath.Join(s.StorePath(), "projects", "ATM", "eventsource.json"); got != want {
		t.Fatalf("eventsourceMetaPath = %q, want %q", got, want)
	}
	if got, want := s.storeMetaPath(), filepath.Join(s.StorePath(), "store.json"); got != want {
		t.Fatalf("storeMetaPath = %q, want %q", got, want)
	}
}

func TestProjectFormatDefaultsToV1(t *testing.T) {
	s := testStore(t)
	f, err := s.projectFormat("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if f != StoreFormatV1 {
		t.Fatalf("format = %q, want v1", f)
	}
}

func TestSetProjectFormatPersists(t *testing.T) {
	s := testStore(t)
	if err := s.setProjectFormat("ATM", StoreFormatV2); err != nil {
		t.Fatal(err)
	}
	again, err := Open(s.StorePath())
	if err != nil {
		t.Fatal(err)
	}
	f, err := again.projectFormat("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if f != StoreFormatV2 {
		t.Fatalf("format after reopen = %q, want v2", f)
	}
	if _, err := os.Stat(filepath.Join(s.StorePath(), "store.json")); err != nil {
		t.Fatalf("store.json missing: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store -run 'Test(EventsourcePaths|ProjectFormatDefaultsToV1|SetProjectFormatPersists)' -count=1
```

Expected: build fails with undefined metadata symbols.

- [ ] **Step 3: Implement metadata types and helpers**

Create `internal/store/eventsource_meta.go`:

```go
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"atm/internal/eventsource"
)

type StoreFormat string

const (
	StoreFormatV1 StoreFormat = "v1"
	StoreFormatV2 StoreFormat = "v2"
)

type StoreMeta struct {
	ActiveFormat    StoreFormat            `json:"active_format,omitempty"`
	ReplicaID       string                 `json:"replica_id,omitempty"`
	StoreInstanceID string                 `json:"store_instance_id,omitempty"`
	LastHLC         *eventsource.HLC       `json:"last_hlc,omitempty"`
	ProjectFormats map[string]StoreFormat `json:"project_formats,omitempty"`
	CreatedAt       time.Time              `json:"created_at,omitempty"`
	UpdatedAt       time.Time              `json:"updated_at,omitempty"`
}

type ProjectEventsourceMeta struct {
	Generation     string    `json:"generation,omitempty"`
	EventCount     int       `json:"event_count"`
	FileSize       int64     `json:"file_size"`
	Frontier       []string  `json:"frontier,omitempty"`
	LastVerifiedAt time.Time `json:"last_verified_at,omitempty"`
	UpgradedFrom   string    `json:"upgraded_from,omitempty"`
}

func (s *Store) storeMetaPath() string {
	return filepath.Join(s.Root, "store.json")
}

func (s *Store) eventsV2Path(code string) string {
	return filepath.Join(s.projectDir(code), "events.v2.jsonl")
}

func (s *Store) eventsourceMetaPath(code string) string {
	return filepath.Join(s.projectDir(code), "eventsource.json")
}

func (s *Store) readStoreMeta() (*StoreMeta, error) {
	raw, err := os.ReadFile(s.storeMetaPath())
	if os.IsNotExist(err) {
		return &StoreMeta{ActiveFormat: StoreFormatV1, ProjectFormats: map[string]StoreFormat{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var m StoreMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m.ActiveFormat == "" {
		m.ActiveFormat = StoreFormatV1
	}
	if m.ProjectFormats == nil {
		m.ProjectFormats = map[string]StoreFormat{}
	}
	return &m, nil
}

func (s *Store) writeStoreMeta(m *StoreMeta) error {
	if m.ProjectFormats == nil {
		m.ProjectFormats = map[string]StoreFormat{}
	}
	now := Now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(s.storeMetaPath()), 0o755); err != nil {
		return err
	}
	tmp := s.storeMetaPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.storeMetaPath())
}

func (s *Store) projectFormat(code string) (StoreFormat, error) {
	m, err := s.readStoreMeta()
	if err != nil {
		return "", err
	}
	if f, ok := m.ProjectFormats[code]; ok && f != "" {
		return f, nil
	}
	if m.ActiveFormat != "" {
		return m.ActiveFormat, nil
	}
	return StoreFormatV1, nil
}

func (s *Store) setProjectFormat(code string, f StoreFormat) error {
	m, err := s.readStoreMeta()
	if err != nil {
		return err
	}
	if m.ProjectFormats == nil {
		m.ProjectFormats = map[string]StoreFormat{}
	}
	m.ProjectFormats[code] = f
	return s.writeStoreMeta(m)
}
```

- [ ] **Step 4: Ensure Init creates a default store meta**

Modify `internal/store/store.go` in `Init` after `cacheDB()` succeeds:

```go
	_, err := s.cacheDB()
	if err != nil {
		return err
	}
	m, err := s.readStoreMeta()
	if err != nil {
		return err
	}
	return s.writeStoreMeta(m)
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/store -run 'Test(EventsourcePaths|ProjectFormatDefaultsToV1|SetProjectFormatPersists)' -count=1
go test ./internal/store -run TestStore -count=1
```

Expected: tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/store/eventsource_meta.go internal/store/eventsource_meta_test.go internal/store/store.go
git commit -m "feat(ATM-0107): add eventsource v2 metadata and format state"
```

---

### Task 2: v2 JSONL Reader, Verifier, and Append Commit Point

**Files:**
- Create: `internal/store/eventsource_file.go`
- Create: `internal/store/eventsource_file_test.go`

**Interfaces:**
- Consumes: `internal/eventsource.Parse`, `eventsource.BuildDAG`, `eventsource.Fold`, `eventsV2Path`.
- Produces:
  - `type V2FileSnapshot struct`
  - `func (s *Store) readV2FileAt(path string, repairTail bool) (*V2FileSnapshot, error)` — path-parameterized so Task 4 can verify a temp file before cutover
  - `func (s *Store) readV2File(code string, repairTail bool) (*V2FileSnapshot, error)`
  - `func (s *Store) verifyV2File(code string) (*V2FileSnapshot, error)`
  - `func (s *Store) appendV2EventLineLocked(code string, raw []byte) error`
  - `func (s *Store) archiveV2FileLocked(code, reason string) (string, error)`

- [ ] **Step 1: Write failing file tests**

Create `internal/store/eventsource_file_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/eventsource"
)

func testV2Event(t *testing.T, action string) *eventsource.Event {
	t.Helper()
	clock := eventsource.NewClock(func() int64 { return 1000 })
	ev, err := eventsource.NewEvent(clock, "r_0123456789abcdefghjkmnpqrs", nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  action,
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestAppendAndReadV2File(t *testing.T) {
	s := testStore(t)
	ev := testV2Event(t, "project.created")
	if err := s.WithLock("ATM", func() error {
		return s.appendV2EventLineLocked("ATM", ev.Raw)
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.readV2File("ATM", false)
	if err != nil {
		t.Fatal(err)
	}
	if snap.EventCount != 1 {
		t.Fatalf("EventCount = %d, want 1", snap.EventCount)
	}
	if snap.Events[0].ID != ev.ID {
		t.Fatalf("event id = %s, want %s", snap.Events[0].ID, ev.ID)
	}
}

func TestReadV2FileTruncatesPartialTailOnlyWhenRepairRequested(t *testing.T) {
	s := testStore(t)
	ev := testV2Event(t, "project.created")
	if err := os.MkdirAll(filepath.Dir(s.eventsV2Path("ATM")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.eventsV2Path("ATM"), append(append([]byte{}, ev.Raw...), []byte("\n{\"partial\"")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readV2File("ATM", false); err == nil {
		t.Fatal("expected integrity error without repairTail")
	}
	snap, err := s.readV2File("ATM", true)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TruncatedBytes == 0 {
		t.Fatal("expected truncated byte count")
	}
	raw, err := os.ReadFile(s.eventsV2Path("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "partial") {
		t.Fatalf("partial tail not truncated: %s", raw)
	}
}

func TestReadV2FileRejectsMalformedCompleteLine(t *testing.T) {
	s := testStore(t)
	if err := os.MkdirAll(filepath.Dir(s.eventsV2Path("ATM")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.eventsV2Path("ATM"), []byte("{not-json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readV2File("ATM", true); err == nil {
		t.Fatal("expected malformed complete line to fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store -run 'Test(AppendAndReadV2File|ReadV2File)' -count=1
```

Expected: build fails with undefined v2 file helpers.

- [ ] **Step 3: Implement v2 file helpers**

Create `internal/store/eventsource_file.go`:

```go
package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"atm/internal/eventsource"
)

type V2FileSnapshot struct {
	Events         []*eventsource.Event
	EventCount     int
	FileSize       int64
	TruncatedBytes int
	Frontier       []string
}

// readV2FileAt reads a v2 event file. The commit point is a complete,
// newline-terminated line (L3-7): every byte after the last '\n' is an
// uncommitted partial tail — even if it happens to parse as JSON. A
// bufio.Scanner would hide that distinction (it yields an unterminated
// tail as a normal line), so the split is done on the raw bytes.
func (s *Store) readV2FileAt(path string, repairTail bool) (*V2FileSnapshot, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &V2FileSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}

	body, tail := raw, 0
	if n := len(raw); n > 0 && raw[n-1] != '\n' {
		cut := bytes.LastIndexByte(raw, '\n') + 1
		body, tail = raw[:cut], n-cut
	}
	if tail > 0 {
		if !repairTail {
			return nil, fmt.Errorf("%w: %s has %d bytes of uncommitted partial tail", ErrIntegrity, path, tail)
		}
		if err := os.Truncate(path, int64(len(body))); err != nil {
			return nil, err
		}
	}

	var events []*eventsource.Event
	lines := bytes.Split(body, []byte("\n"))
	for i, line := range lines {
		if i == len(lines)-1 && len(line) == 0 {
			break // split artifact after the final newline
		}
		ev, err := eventsource.Parse(line)
		if err != nil {
			// A complete line that fails to parse is an integrity error,
			// never a repair target (spec crash-recovery rules).
			return nil, fmt.Errorf("%w: %s:%d: %v", ErrIntegrity, path, i+1, err)
		}
		events = append(events, ev)
	}

	dag, err := eventsource.BuildDAG(events)
	if err != nil {
		return nil, fmt.Errorf("%w: %s DAG: %v", ErrIntegrity, path, err)
	}
	return &V2FileSnapshot{
		Events:         events,
		EventCount:     len(events),
		FileSize:       int64(len(body)),
		TruncatedBytes: tail,
		Frontier:       dag.Frontier(),
	}, nil
}

func (s *Store) readV2File(code string, repairTail bool) (*V2FileSnapshot, error) {
	return s.readV2FileAt(s.eventsV2Path(code), repairTail)
}

// verifyV2File is the strict read: parse, recompute ids, validate parents,
// build the DAG — and never repair.
func (s *Store) verifyV2File(code string) (*V2FileSnapshot, error) {
	return s.readV2File(code, false)
}

func (s *Store) appendV2EventLineLocked(code string, raw []byte) error {
	path := s.eventsV2Path(code)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(raw); err != nil {
		return err
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return err
	}
	return f.Sync()
}

func (s *Store) archiveV2FileLocked(code, reason string) (string, error) {
	path := s.eventsV2Path(code)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	reason = strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(reason)
	dst := filepath.Join(s.projectDir(code), fmt.Sprintf("events.v2.%s.%d.jsonl", reason, time.Now().UTC().Unix()))
	return dst, os.Rename(path, dst)
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/store -run 'Test(AppendAndReadV2File|ReadV2File)' -count=1
```

Expected: tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/store/eventsource_file.go internal/store/eventsource_file_test.go
git commit -m "feat(ATM-0107): add v2 event file IO and verification"
```

---

### Task 3: v2 Projection Into Existing Cache Rows

**Files:**
- Create: `internal/store/eventsource_projector.go`
- Create: `internal/store/eventsource_projector_test.go`
- Modify: `internal/store/cache.go`

**Interfaces:**
- Consumes: `eventsource.State`, `eventsource.ProjectState`, `eventsource.TaskState`, `eventsource.CommentState`, `eventsource.LabelState` from ATM-0106. These types embed `EntityMeta` (fields `ID`, `Alias`, `Tombstoned`, `CreatedAt`, `CreatedBy`, `UpdatedAt`, `UpdatedBy` are promoted — there is no `Meta` field); comments reference their task and reply target by identity (`TaskRef`, `ReplyToRef`); labels use `Expr`; `TaskState` has no `ProjectCode` (the event file is per-project).
- Produces:
  - `func (s *Store) cacheProjectFromV2State(code string, st *eventsource.State, eventCount int) error`
  - `func cacheDeleteProjectRows(db *sql.DB, code string) error`
  - `func projectFromV2(p *eventsource.ProjectState) *Project`
  - `func taskFromV2(code string, t *eventsource.TaskState, ordinal int) *Task`
  - `func commentFromV2(c *eventsource.CommentState, taskAlias, replyToAlias string, ordinal int) *Comment`
  - `func labelFromV2(l *eventsource.LabelState, ordinal int) Label`
  - guarded cache schema columns: `identity`, `alias`, and v2 freshness meta rows.

- [ ] **Step 1: Write failing projection test**

Create `internal/store/eventsource_projector_test.go` with a fixture that builds v2 state through ATM-0106 helpers:

```go
package store

import (
	"testing"

	"atm/internal/eventsource"
)

func TestCacheProjectFromV2StateWritesCompatibilityRows(t *testing.T) {
	s := testStore(t)
	clock := eventsource.NewClock(func() int64 { return 1000 })
	project, err := eventsource.NewEvent(clock, "r_0123456789abcdefghjkmnpqrs", nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.created",
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := eventsource.NewEvent(clock, "r_0123456789abcdefghjkmnpqrs", []string{project.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "task.created",
		Subject: eventsource.Subject{Kind: "task"},
		Payload: map[string]any{"alias": "ATM-abcdef", "title": "First", "description": "Body", "labels": []string{"ATM:status:open"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := eventsource.FoldEvents([]*eventsource.Event{project, task})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cacheProjectFromV2State("ATM", state, 2); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if p.Code != "ATM" || p.Name != "Agent Tasks Management" {
		t.Fatalf("project = %#v", p)
	}
	tk, err := s.GetTask("ATM-abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Title != "First" || tk.Description != "Body" {
		t.Fatalf("task = %#v", tk)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store -run TestCacheProjectFromV2StateWritesCompatibilityRows -count=1
```

Expected: build fails with undefined projection helper or ATM-0106 type mismatches.

- [ ] **Step 3: Add guarded cache migrations**

Modify `internal/store/cache.go` after the existing guarded `ALTER TABLE labels ADD COLUMN expr` block:

```go
		for _, stmt := range []string{
			`ALTER TABLE projects ADD COLUMN identity TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE tasks ADD COLUMN identity TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE tasks ADD COLUMN alias TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE comments ADD COLUMN identity TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE comments ADD COLUMN alias TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE labels ADD COLUMN identity TEXT NOT NULL DEFAULT ''`,
		} {
			if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
				s.cacheErr = err
				return
			}
		}
		for _, stmt := range []string{
			`CREATE INDEX IF NOT EXISTS idx_tasks_identity ON tasks(identity)`,
			`CREATE INDEX IF NOT EXISTS idx_tasks_alias ON tasks(alias)`,
			`CREATE INDEX IF NOT EXISTS idx_comments_identity ON comments(identity)`,
			`CREATE INDEX IF NOT EXISTS idx_comments_alias ON comments(alias)`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				s.cacheErr = err
				return
			}
		}
```

- [ ] **Step 4: Implement v2 projection**

Create `internal/store/eventsource_projector.go`. The ATM-0106 state types embed `EntityMeta`, so meta fields are promoted (`t.Alias`, `t.Tombstoned`, ...). Comments carry identities (`TaskRef`, `ReplyToRef`); the projector maps them back to aliases through `st.Tasks` / `st.Comments`. Projection is delete-then-insert for the project's rows: an upsert-only projector would leave tombstoned entities and rows from a discarded v2 branch (re-upgrade) in the cache.

```go
package store

import (
	"database/sql"
	"sort"

	"atm/internal/eventsource"
)

// cacheProjectFromV2State replaces the project's cache rows with the live
// entities of a v2 fold. eventCount is the number of events in the file the
// fold came from; it is the v2 freshness key.
func (s *Store) cacheProjectFromV2State(code string, st *eventsource.State, eventCount int) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	if err := cacheDeleteProjectRows(db, code); err != nil {
		return err
	}
	for _, p := range st.Projects {
		if p.Code != code || p.Tombstoned {
			continue
		}
		if err := cacheUpsertProject(db, projectFromV2(p)); err != nil {
			return err
		}
	}
	commentAlias := func(id string) string {
		if c, ok := st.Comments[id]; ok {
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
		if err := cacheUpsertTask(db, taskFromV2(code, t, ordinal)); err != nil {
			return err
		}
		for i, c := range st.CommentsByCreation(t.ID) {
			if c.Tombstoned {
				continue
			}
			if err := cacheUpsertComment(db, commentFromV2(c, t.Alias, commentAlias(c.ReplyToRef), i+1)); err != nil {
				return err
			}
		}
	}
	names := make([]string, 0, len(st.Labels))
	for name, l := range st.Labels {
		if l.Tombstoned {
			if _, err := db.Exec(`DELETE FROM labels WHERE name = ?`, name); err != nil {
				return err
			}
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		if err := cacheUpsertLabel(db, labelFromV2(st.Labels[name], i+1)); err != nil {
			return err
		}
	}
	return cacheSetV2Freshness(db, code, eventCount)
}

// cacheDeleteProjectRows removes the project's task/comment rows and the
// project row itself — the per-project mirror of the global wipe Rebuild
// does. Labels stay: the labels table is store-global (merged across
// projects), so only tombstoned names are deleted, above.
func cacheDeleteProjectRows(db *sql.DB, code string) error {
	for _, stmt := range []string{
		`DELETE FROM comment_labels WHERE comment_id IN (SELECT c.id FROM comments c JOIN tasks t ON t.id = c.task_id WHERE t.project_code = ?)`,
		`DELETE FROM comments WHERE task_id IN (SELECT id FROM tasks WHERE project_code = ?)`,
		`DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE project_code = ?)`,
		`DELETE FROM tasks WHERE project_code = ?`,
		`DELETE FROM projects WHERE code = ?`,
	} {
		if _, err := db.Exec(stmt, code); err != nil {
			return err
		}
	}
	return nil
}

func projectFromV2(p *eventsource.ProjectState) *Project {
	// NextTaskN and LogSeq are v1 bookkeeping; they are meaningless for a
	// v2-active project, and every v2 read path must branch by format
	// before the v1 freshness checks that would read them (Task 9).
	return &Project{
		Code:      p.Code,
		Name:      p.Name,
		CreatedAt: p.CreatedAt,
		CreatedBy: p.CreatedBy,
		UpdatedAt: p.UpdatedAt,
		UpdatedBy: p.UpdatedBy,
	}
}

func taskFromV2(code string, t *eventsource.TaskState, ordinal int) *Task {
	labels := append([]string(nil), t.Labels...)
	sort.Strings(labels)
	return &Task{
		ID:          t.Alias,
		ProjectCode: code,
		Title:       t.Title,
		Description: t.Description,
		Labels:      labels,
		LogSeq:      ordinal,
		CreatedAt:   t.CreatedAt,
		CreatedBy:   t.CreatedBy,
		UpdatedAt:   t.UpdatedAt,
		UpdatedBy:   t.UpdatedBy,
	}
}

func commentFromV2(c *eventsource.CommentState, taskAlias, replyToAlias string, ordinal int) *Comment {
	labels := append([]string(nil), c.Labels...)
	sort.Strings(labels)
	return &Comment{
		ID:        c.Alias,
		TaskID:    taskAlias,
		ReplyTo:   replyToAlias,
		Body:      c.Body,
		Labels:    labels,
		LogSeq:    ordinal,
		CreatedAt: c.CreatedAt,
		CreatedBy: c.CreatedBy,
		UpdatedAt: c.UpdatedAt,
		UpdatedBy: c.UpdatedBy,
	}
}

func labelFromV2(l *eventsource.LabelState, ordinal int) Label {
	return Label{Name: l.Name, Description: l.Description, Expr: l.Expr, LogSeq: ordinal}
}
```

Also add `cacheSetV2Freshness`/`cacheGetV2Freshness` helpers in `cache.go` using `meta` keys like `last_v2_event_count:<CODE>`; the stored value is the event count of the file the cache was projected from.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/store -run TestCacheProjectFromV2StateWritesCompatibilityRows -count=1
go test ./internal/store -run TestCache -count=1
```

Expected: projection and cache tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/store/eventsource_projector.go internal/store/eventsource_projector_test.go internal/store/cache.go
git commit -m "feat(ATM-0107): project v2 fold state into cache rows"
```

---

### Task 4: Upgrade, Cutover, Rollback, and Re-upgrade

**Files:**
- Create: `internal/store/eventsource_upgrade.go`
- Create: `internal/store/eventsource_upgrade_test.go`

**Interfaces:**
- Consumes: `eventsource.UpgradeV1(logData []byte) (*UpgradeResult, error)`, `readV2FileAt`, `verifyV2File`, `cacheProjectFromV2State`, `setProjectFormat`, `s.Replay`.
- Produces:
  - `type UpgradeReport struct`
  - `type RollbackReport struct`
  - `func (s *Store) UpgradeProjectToV2(code string) (*UpgradeReport, error)`
  - `func (s *Store) UpgradeAllToV2() ([]UpgradeReport, error)`
  - `func (s *Store) RollbackProjectToV1(code string) (*RollbackReport, error)`
  - `func (s *Store) compareV2FoldToV1Replay(code string, st *eventsource.State) error`
  - `func (s *Store) rebuildProjectCacheFromV1Locked(code string) error`

- [ ] **Step 1: Write failing upgrade tests**

Create `internal/store/eventsource_upgrade_test.go`:

```go
package store

import (
	"os"
	"testing"
)

func TestUpgradeProjectToV2PreservesV1LogAndActivatesV2(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "First task", "desc", []string{"ATM:status:open"}, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.UpgradeProjectToV2("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Project != "ATM" || rep.Events == 0 || rep.Format != StoreFormatV2 {
		t.Fatalf("bad report: %#v", rep)
	}
	after, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("v1 log changed during upgrade")
	}
	if _, err := os.Stat(s.eventsV2Path("ATM")); err != nil {
		t.Fatalf("events.v2.jsonl missing: %v", err)
	}
	f, err := s.projectFormat("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if f != StoreFormatV2 {
		t.Fatalf("format = %q, want v2", f)
	}
	if _, err := s.GetTask("ATM-0001"); err != nil {
		t.Fatalf("cache not rebuilt from v2: %v", err)
	}
}

func TestReupgradeArchivesPreviousV2File(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RollbackProjectToV1("ATM"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "after rollback", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.projectDir("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	archived := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "events.v2.reupgrade.") {
			archived = true
		}
	}
	if !archived {
		t.Fatal("previous v2 file was not archived on re-upgrade")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store -run 'Test(UpgradeProjectToV2PreservesV1LogAndActivatesV2|ReupgradeArchivesPreviousV2File)' -count=1
```

Expected: build fails with undefined upgrade APIs.

- [ ] **Step 3: Implement upgrade APIs**

Create `internal/store/eventsource_upgrade.go`:

```go
package store

import (
	"os"
	"path/filepath"

	"atm/internal/eventsource"
)

type UpgradeReport struct {
	Project      string      `json:"project"`
	Format       StoreFormat `json:"format"`
	Events       int         `json:"events"`
	ArchivedPath string      `json:"archived_path,omitempty"`
}

type RollbackReport struct {
	Project string      `json:"project"`
	Format  StoreFormat `json:"format"`
}

func (s *Store) UpgradeProjectToV2(code string) (*UpgradeReport, error) {
	rep := &UpgradeReport{Project: code, Format: StoreFormatV2}
	err := s.WithLock(code, func() error {
		raw, err := os.ReadFile(s.logPath(code))
		if err != nil {
			return err
		}
		up, err := eventsource.UpgradeV1(raw)
		if err != nil {
			return err
		}

		// 1. Write the candidate file. Nothing existing is touched yet.
		tmp := s.eventsV2Path(code) + ".tmp"
		if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		for _, ev := range up.Events {
			if _, err := f.Write(ev.Raw); err != nil {
				_ = f.Close()
				return err
			}
			if _, err := f.Write([]byte("\n")); err != nil {
				_ = f.Close()
				return err
			}
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}

		// 2. Verify the candidate BEFORE it becomes events.v2.jsonl (L3-3):
		// re-read it, recompute every id, validate parents, build the DAG,
		// and fold.
		snap, err := s.readV2FileAt(tmp, false)
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}
		state, err := eventsource.FoldEvents(snap.Events)
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}

		// 3. Semantic comparison against the current v1 replay (spec upgrade
		// step 6). The package-level equivalence test guards the code path;
		// this guards the user's actual data at cutover time.
		if err := s.compareV2FoldToV1Replay(code, state); err != nil {
			_ = os.Remove(tmp)
			return err
		}

		// 4. Only now displace the previous v2 file (re-upgrade) and cut over.
		// A failed upgrade must leave both the v1 log and any prior v2 file
		// exactly as they were.
		if _, err := os.Stat(s.eventsV2Path(code)); err == nil {
			archived, err := s.archiveV2FileLocked(code, "reupgrade")
			if err != nil {
				return err
			}
			rep.ArchivedPath = archived
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(tmp, s.eventsV2Path(code)); err != nil {
			return err
		}
		if err := s.cacheProjectFromV2State(code, state, snap.EventCount); err != nil {
			return err
		}
		if err := s.setProjectFormat(code, StoreFormatV2); err != nil {
			return err
		}
		rep.Events = snap.EventCount
		return nil
	})
	return rep, err
}

// compareV2FoldToV1Replay fails the upgrade when the v2 fold and the v1
// replay disagree on any semantic field ATM exposes today, keyed by alias:
// the project's name; the live task set with title, description, and sorted
// labels per alias; the live comment set with body, sorted labels, task
// alias, and reply-to alias per alias; and each label's name, description,
// and expr. Implement by building alias-keyed maps from st (live entities
// only, identities mapped to aliases as in the projector) and from
// s.Replay(code), then diffing; report the first difference with enough
// context to debug (entity kind, alias, field).
func (s *Store) compareV2FoldToV1Replay(code string, st *eventsource.State) error {
	// ... see field checklist above ...
}

func (s *Store) UpgradeAllToV2() ([]UpgradeReport, error) {
	codes, err := s.projectCodesOnDisk()
	if err != nil {
		return nil, err
	}
	out := make([]UpgradeReport, 0, len(codes))
	for _, code := range codes {
		rep, err := s.UpgradeProjectToV2(code)
		if err != nil {
			return out, err
		}
		out = append(out, *rep)
	}
	return out, nil
}

// RollbackProjectToV1 switches the active format AND rebuilds the project's
// cache rows from the v1 replay. The cache still holds v2-derived rows whose
// LogSeq ordinals mean nothing to the v1 freshness checks (`cache LogSeq >
// log LastSeq` → ErrIntegrity) and whose NextTaskN is unset; leaving them in
// place would break v1 reads and writes immediately after rollback.
func (s *Store) RollbackProjectToV1(code string) (*RollbackReport, error) {
	rep := &RollbackReport{Project: code, Format: StoreFormatV1}
	err := s.WithLock(code, func() error {
		if err := s.setProjectFormat(code, StoreFormatV1); err != nil {
			return err
		}
		return s.rebuildProjectCacheFromV1Locked(code)
	})
	return rep, err
}

// rebuildProjectCacheFromV1Locked mirrors the per-project body of Rebuild:
// cacheDeleteProjectRows, then s.Replay(code) and re-insert the live set
// (project row, tasks, comments, labels).
func (s *Store) rebuildProjectCacheFromV1Locked(code string) error {
	// ... Replay + cacheDeleteProjectRows + cacheUpsert* ...
}
```

Add missing imports in the test file (`strings`) if the compiler reports it.

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/store -run 'Test(UpgradeProjectToV2PreservesV1LogAndActivatesV2|ReupgradeArchivesPreviousV2File)' -count=1
```

Expected: tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/store/eventsource_upgrade.go internal/store/eventsource_upgrade_test.go
git commit -m "feat(ATM-0107): upgrade projects to side-by-side v2 storage"
```

---

### Task 5: Verify and Rebuild Dispatch by Active Format

**Files:**
- Modify: `internal/store/rebuild.go`
- Modify: `internal/store/verify.go`
- Create: `internal/store/eventsource_verify_test.go`

**Interfaces:**
- Consumes: `projectFormat`, `readV2File`, `verifyV2File`, `cacheProjectFromV2State`.
- Produces:
  - `VerifyReport.Format StoreFormat`
  - `VerifyReport.V2Events int`
  - v2-aware `Rebuild`
  - v2-aware `VerifyProject`

- [ ] **Step 1: Write failing verify/rebuild tests**

Create `internal/store/eventsource_verify_test.go`:

```go
package store

import (
	"os"
	"testing"
)

func TestVerifyProjectReportsV2Format(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	r, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if r.Format != StoreFormatV2 {
		t.Fatalf("Format = %q, want v2", r.Format)
	}
	if r.V2Events == 0 {
		t.Fatalf("V2Events = %d, want > 0", r.V2Events)
	}
}

func TestRebuildUsesV2ForV2ActiveProject(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	if err := os.Remove(s.cachePath()); err != nil {
		t.Fatal(err)
	}
	s.cacheOnce = sync.Once{}
	s.cacheDBConn = nil
	rep, err := s.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tasks == 0 {
		t.Fatalf("rebuild report = %#v", rep)
	}
	if _, err := s.GetTask(tk.ID); err != nil {
		t.Fatalf("GetTask after v2 rebuild: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store -run 'Test(VerifyProjectReportsV2Format|RebuildUsesV2ForV2ActiveProject)' -count=1
```

Expected: build fails until `VerifyReport` and rebuild dispatch are added. If the test needs `sync`, add it to imports.

- [ ] **Step 3: Modify `VerifyReport`**

Add fields to `internal/store/verify.go`:

```go
	Format    StoreFormat `json:"format"`
	V2Events  int         `json:"v2_events,omitempty"`
	V2FileOK  bool        `json:"v2_file_ok,omitempty"`
```

In `VerifyProject`, branch early by format:

```go
	format, err := s.projectFormat(code)
	if err != nil {
		return nil, err
	}
	report := &VerifyReport{Project: code, LogOK: true, Format: format}
	if format == StoreFormatV2 {
		snap, err := s.verifyV2File(code)
		if err != nil {
			report.LogOK = false
			report.Diverged = true
			return report, nil
		}
		report.V2FileOK = true
		report.V2Events = snap.EventCount
		report.LogEntries = snap.EventCount
		state, err := eventsource.FoldEvents(snap.Events)
		if err != nil {
			report.LogOK = false
			report.Diverged = true
			return report, nil
		}
		report.Caches = append(report.Caches, s.checkV2Cache(code, state, snap.EventCount)...)
		for _, c := range report.Caches {
			if c.Status != "ok" {
				report.Diverged = true
			}
		}
		return report, nil
	}
```

Create `checkV2Cache` in `verify.go` or a new small helper file. It should compare the v2 freshness meta row and key cache rows that already exist:

```go
func (s *Store) checkV2Cache(code string, st *eventsource.State, eventCount int) []CacheCheck {
	db, err := s.cacheDB()
	if err != nil {
		return []CacheCheck{{Kind: "project", ID: code, Status: "corrupt"}}
	}
	if got, ok, err := cacheGetV2Freshness(db, code); err != nil {
		return []CacheCheck{{Kind: "project", ID: code, Status: "corrupt"}}
	} else if !ok {
		return []CacheCheck{{Kind: "project", ID: code, Status: "missing", LastEventSeq: eventCount}}
	} else if got != eventCount {
		return []CacheCheck{{Kind: "project", ID: code, Status: "stale", CacheLogSeq: got, LastEventSeq: eventCount}}
	}
	return []CacheCheck{{Kind: "project", ID: code, Status: "ok", CacheLogSeq: eventCount, LastEventSeq: eventCount}}
}
```

- [ ] **Step 4: Modify `Rebuild`**

In `internal/store/rebuild.go`, inside the loop over codes:

```go
		format, err := s.projectFormat(code)
		if err != nil {
			return rep, err
		}
		if format == StoreFormatV2 {
			snap, err := s.verifyV2File(code)
			if err != nil {
				return rep, err
			}
			state, err := eventsource.FoldEvents(snap.Events)
			if err != nil {
				return rep, err
			}
			if err := s.cacheProjectFromV2State(code, state, snap.EventCount); err != nil {
				return rep, err
			}
			rep.Projects++
			rep.Tasks += len(state.Tasks)
			rep.Labels += len(state.Labels)
			_ = os.RemoveAll(s.vectorsDir(code))
			continue
		}
```

Keep the existing v1 replay path as the `else`/fallthrough.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/store -run 'Test(VerifyProjectReportsV2Format|RebuildUsesV2ForV2ActiveProject)' -count=1
go test ./internal/store -run 'TestStoreVerify|TestStoreRebuild' -count=1
```

Expected: tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/store/rebuild.go internal/store/verify.go internal/store/eventsource_verify_test.go
git commit -m "feat(ATM-0107): verify and rebuild v2-active projects"
```

---

### Task 6: CLI Upgrade, Rollback, and README Runbook

**Files:**
- Modify: `internal/cli/store.go`
- Modify: `internal/cli/store_test.go`
- Modify: `internal/cli/conventions.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `Store.UpgradeProjectToV2`, `Store.UpgradeAllToV2`, `Store.RollbackProjectToV1`.
- Produces:
  - `atm store upgrade --project <CODE>`
  - `atm store upgrade --all`
  - `atm store rollback --project <CODE> --to v1`

- [ ] **Step 1: Write failing CLI tests**

Append to `internal/cli/store_test.go`:

```go
func TestStoreUpgradeProjectAndRollback(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "upgrade", "--project", "ATM")
	mustContain(t, out, "upgraded\tATM\tv2")
	out = runArgsOut(t, st, "store", "verify", "ATM")
	mustContain(t, out, "format: v2")
	out = runArgsOut(t, st, "store", "rollback", "--project", "ATM", "--to", "v1")
	mustContain(t, out, "rolled back\tATM\tv1")
}

func TestStoreUpgradeAll(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "project", "create", "--code", "DOC", "--name", "docs", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "upgrade", "--all")
	mustContain(t, out, "upgraded\tATM\tv2")
	mustContain(t, out, "upgraded\tDOC\tv2")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli -run 'TestStoreUpgrade(ProjectAndRollback|All)' -count=1
```

Expected: command not found or flag errors.

- [ ] **Step 3: Add CLI commands**

In `internal/cli/store.go`, add `upgradeCmd` and `rollbackCmd` before `return cmd`:

```go
	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade v1 project logs to side-by-side EventSource v2 storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			all, _ := cmd.Flags().GetBool("all")
			if all == (project != "") {
				return fmt.Errorf("%w: pass exactly one of --project or --all", store.ErrUsage)
			}
			if all {
				reps, err := s.UpgradeAllToV2()
				if err != nil {
					return err
				}
				if st.isJSON() {
					return writeJSON(st.stdout(), reps)
				}
				for _, r := range reps {
					fmt.Fprintf(st.stdout(), "upgraded\t%s\t%s\tevents=%d\n", r.Project, r.Format, r.Events)
				}
				return nil
			}
			rep, err := s.UpgradeProjectToV2(project)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), rep)
			}
			fmt.Fprintf(st.stdout(), "upgraded\t%s\t%s\tevents=%d\n", rep.Project, rep.Format, rep.Events)
			return nil
		},
	}
	upgradeCmd.Flags().String("project", "", "project code to upgrade")
	upgradeCmd.Flags().Bool("all", false, "upgrade all projects")
	cmd.AddCommand(upgradeCmd)

	rollbackCmd := &cobra.Command{
		Use:   "rollback",
		Short: "Switch a project back to the preserved v1 log",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			to, _ := cmd.Flags().GetString("to")
			if project == "" || to != string(store.StoreFormatV1) {
				return fmt.Errorf("%w: rollback requires --project <CODE> --to v1", store.ErrUsage)
			}
			rep, err := s.RollbackProjectToV1(project)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), rep)
			}
			fmt.Fprintf(st.stdout(), "rolled back\t%s\t%s\n", rep.Project, rep.Format)
			return nil
		},
	}
	rollbackCmd.Flags().String("project", "", "project code to roll back")
	rollbackCmd.Flags().String("to", "", "target format; only v1 is supported")
	cmd.AddCommand(rollbackCmd)
```

Also update `emitVerify` text output:

```go
	fmt.Fprintf(st.stdout(), "project: %s\nformat: %s\nlog_entries: %d\nlog_ok: %t\ntruncated: %d\ndiverged: %t\n", r.Project, r.Format, r.LogEntries, r.LogOK, r.Truncated, r.Diverged)
```

- [ ] **Step 4: Update README**

In `README.md`, expand the `## Store` section with:

```markdown
### Upgrade An Existing Store To EventSource v2

ATM preserves each existing v1 project log during upgrade. The upgrade writes a new v2 event file next to it, verifies the result, rebuilds `cache.db`, and only then switches the project to v2.

```sh
atm store path
atm store verify
atm store upgrade --project ATM
atm store verify
```

To upgrade every project:

```sh
atm store upgrade --all
atm store verify
```

The preserved v1 log stays at:

```text
$ATM_HOME/projects/<CODE>/log.jsonl
```

The v2 source of truth is:

```text
$ATM_HOME/projects/<CODE>/events.v2.jsonl
```

If upgrade fails, ATM leaves the project on v1. To switch back before continuing:

```sh
atm store rollback --project ATM --to v1
```

Rollback does not copy v2-only writes back into v1. If you write more data while back on v1, run upgrade again; ATM rebuilds the v2 event file from the current v1 log and moves the previous v2 file aside.
```

- [ ] **Step 5: Update conventions text**

In `internal/cli/conventions.go`, add one bullet near existing store commands:

```go
"atm store upgrade --project <CODE> — upgrade a preserved v1 log to side-by-side EventSource v2 storage",
```

Update golden files with the repository's existing golden update flow if the conventions tests require it.

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./internal/cli -run 'TestStoreUpgrade(ProjectAndRollback|All)|TestStoreVerifyClean|TestStoreRebuild' -count=1
go test ./internal/cli -run 'Test.*Conventions|TestDeterminism' -count=1
```

Expected: tests pass; update goldens only if the conventions tests direct it.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/store.go internal/cli/store_test.go internal/cli/conventions.go internal/cli/testdata README.md
git commit -m "feat(ATM-0107): expose v2 store upgrade and rollback commands"
```

---

### Task 7: v2 Authoring Helper With Lock-Scoped Frontier and HLC

**Files:**
- Create: `internal/store/eventsource_author.go`
- Create: `internal/store/eventsource_author_test.go`

**Interfaces:**
- Consumes: `eventsource.Clock`, `eventsource.NewEvent`, `eventsource.NewTaskCreated` / `NewCommentCreated` / `NewProjectCreated` (all creation helpers take a trailing `taken func(string) bool`; nil-safe), `eventsource.State.Resolve`, `readV2File`, `appendV2EventLineLocked`, `readStoreMeta`, `writeStoreMeta`.
- Produces:
  - `type V2Draft struct`
  - `type v2AuthorCtx struct` — snapshot, fold state, clock, and replica for one locked write
  - `func (s *Store) beginV2AuthorLocked(code string) (*v2AuthorCtx, error)`
  - `func (s *Store) commitV2AuthorLocked(code string, ev *eventsource.Event) error`
  - `func (c *v2AuthorCtx) resolveTaskRef(alias string) (string, error)` / `resolveCommentRef` — alias → identity for `subject.id`, `task_ref`, `reply_to_ref`
  - `func (s *Store) appendV2Locked(code string, draft V2Draft) (*eventsource.Event, error)`
  - `func (s *Store) appendV2TaskCreatedLocked(code, title, description string, labels []string, actor string) (*eventsource.Event, string, error)`
  - `func (s *Store) appendV2CommentCreatedLocked(code, taskAlias, body string, labels []string, replyToAlias, actor string) (*eventsource.Event, string, error)`
  - `func (s *Store) currentReplicaIDLocked() (string, error)`

- [ ] **Step 1: Write failing authoring test**

Create `internal/store/eventsource_author_test.go`:

```go
package store

import (
	"testing"

	"atm/internal/eventsource"
)

func TestAppendV2LockedParentsSecondLocalWriteOnFirst(t *testing.T) {
	s := testStore(t)
	if err := s.setProjectFormat("ATM", StoreFormatV2); err != nil {
		t.Fatal(err)
	}
	var firstID string
	if err := s.WithLock("ATM", func() error {
		ev, err := s.appendV2Locked("ATM", V2Draft{
			Actor:   "admin@cli:unset",
			Action:  "project.created",
			Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
			Payload: map[string]any{"alias": "ATM", "name": "x"},
		})
		if err != nil {
			return err
		}
		firstID = ev.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WithLock("ATM", func() error {
		ev, err := s.appendV2Locked("ATM", V2Draft{
			Actor:   "admin@cli:unset",
			Action:  "project.name-changed",
			Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
			Payload: map[string]any{"name": "y"},
		})
		if err != nil {
			return err
		}
		if len(ev.Parents) != 1 || ev.Parents[0] != firstID {
			t.Fatalf("parents = %#v, want [%s]", ev.Parents, firstID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store -run TestAppendV2LockedParentsSecondLocalWriteOnFirst -count=1
```

Expected: undefined authoring helper symbols.

- [ ] **Step 3: Implement authoring helper**

Create `internal/store/eventsource_author.go`:

```go
package store

import (
	"crypto/rand"
	"fmt"

	"atm/internal/eventsource"
)

type V2Draft struct {
	Actor   string
	Action  string
	Subject eventsource.Subject
	Payload map[string]any
}

// v2AuthorCtx is everything a locked writer needs: the current snapshot and
// fold (frontier, alias→identity resolution, taken-alias sets), a clock that
// has observed the persisted local HLC and every event in the file, and the
// writing replica id. It must only be built while holding the project lock.
type v2AuthorCtx struct {
	snap    *V2FileSnapshot
	state   *eventsource.State
	clock   *eventsource.Clock
	replica string
}

func (s *Store) beginV2AuthorLocked(code string) (*v2AuthorCtx, error) {
	snap, err := s.readV2File(code, true)
	if err != nil {
		return nil, err
	}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		return nil, err
	}
	replica, err := s.currentReplicaIDLocked()
	if err != nil {
		return nil, err
	}
	clock := eventsource.NewClock(nil)
	m, err := s.readStoreMeta()
	if err != nil {
		return nil, err
	}
	if m.LastHLC != nil {
		clock.Observe(*m.LastHLC) // spec authoring step 5: the persisted local HLC
	}
	for _, ev := range snap.Events {
		clock.Observe(ev.HLC)
	}
	return &v2AuthorCtx{snap: snap, state: state, clock: clock, replica: replica}, nil
}

// commitV2AuthorLocked appends the event line — the commit point — and then
// persists the local HLC. The metadata write is rebuildable state; the event
// line is the truth.
func (s *Store) commitV2AuthorLocked(code string, ev *eventsource.Event) error {
	if err := s.appendV2EventLineLocked(code, ev.Raw); err != nil {
		return err
	}
	m, err := s.readStoreMeta()
	if err != nil {
		return err
	}
	h := ev.HLC
	m.LastHLC = &h
	return s.writeStoreMeta(m)
}

// resolveTaskRef and resolveCommentRef map a user-facing alias to the
// identity the event model needs (subject.id, task_ref, reply_to_ref).
// An alias collision surfaces as *eventsource.AmbiguousError — the caller
// reports the candidates; never silently pick one (L1-4).
func (c *v2AuthorCtx) resolveTaskRef(alias string) (string, error) {
	m, err := c.state.Resolve(alias)
	if err != nil {
		return "", err
	}
	if m.Kind != "task" {
		return "", fmt.Errorf("%w: %q is a %s, not a task", ErrUsage, alias, m.Kind)
	}
	return m.ID, nil
}

func (c *v2AuthorCtx) resolveCommentRef(alias string) (string, error) {
	m, err := c.state.Resolve(alias)
	if err != nil {
		return "", err
	}
	if m.Kind != "comment" {
		return "", fmt.Errorf("%w: %q is a %s, not a comment", ErrUsage, alias, m.Kind)
	}
	return m.ID, nil
}

func (s *Store) appendV2Locked(code string, draft V2Draft) (*eventsource.Event, error) {
	ctx, err := s.beginV2AuthorLocked(code)
	if err != nil {
		return nil, err
	}
	ev, err := eventsource.NewEvent(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.Draft{
		At:      Now(),
		Actor:   draft.Actor,
		Action:  draft.Action,
		Subject: draft.Subject,
		Payload: draft.Payload,
	})
	if err != nil {
		return nil, err
	}
	return ev, s.commitV2AuthorLocked(code, ev)
}

func (s *Store) appendV2TaskCreatedLocked(code, title, description string, labels []string, actor string) (*eventsource.Event, string, error) {
	ctx, err := s.beginV2AuthorLocked(code)
	if err != nil {
		return nil, "", err
	}
	taken := map[string]bool{}
	for _, t := range ctx.state.Tasks {
		taken[t.Alias] = true
	}
	ev, alias, err := eventsource.NewTaskCreated(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.TaskCreateDraft{
		ProjectCode: code,
		At:          Now(),
		Actor:       actor,
		Title:       title,
		Description: description,
		Labels:      labels,
	}, func(a string) bool { return taken[a] })
	if err != nil {
		return nil, "", err
	}
	return ev, alias, s.commitV2AuthorLocked(code, ev)
}

func (s *Store) appendV2CommentCreatedLocked(code, taskAlias, body string, labels []string, replyToAlias, actor string) (*eventsource.Event, string, error) {
	ctx, err := s.beginV2AuthorLocked(code)
	if err != nil {
		return nil, "", err
	}
	taskRef, err := ctx.resolveTaskRef(taskAlias)
	if err != nil {
		return nil, "", err
	}
	replyToRef := ""
	if replyToAlias != "" {
		if replyToRef, err = ctx.resolveCommentRef(replyToAlias); err != nil {
			return nil, "", err
		}
	}
	taken := map[string]bool{}
	for _, c := range ctx.state.Comments {
		if c.TaskRef == taskRef {
			taken[c.Alias] = true
		}
	}
	ev, alias, err := eventsource.NewCommentCreated(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.CommentCreateDraft{
		TaskAlias:  taskAlias,
		TaskRef:    taskRef,
		ReplyToRef: replyToRef,
		At:         Now(),
		Actor:      actor,
		Body:       body,
		Labels:     labels,
	}, func(a string) bool { return taken[a] })
	if err != nil {
		return nil, "", err
	}
	return ev, alias, s.commitV2AuthorLocked(code, ev)
}

func (s *Store) currentReplicaIDLocked() (string, error) {
	m, err := s.readStoreMeta()
	if err != nil {
		return "", err
	}
	if m.ReplicaID == "" {
		id, err := eventsource.MintReplicaID(rand.Reader)
		if err != nil {
			return "", err
		}
		m.ReplicaID = id
		if err := s.writeStoreMeta(m); err != nil {
			return "", err
		}
	}
	return m.ReplicaID, nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/store -run TestAppendV2LockedParentsSecondLocalWriteOnFirst -count=1
```

Expected: tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/store/eventsource_author.go internal/store/eventsource_author_test.go
git commit -m "feat(ATM-0107): author v2 events from lock-scoped frontier"
```

---

### Task 8: Rewire Live Mutations for v2-Active Projects

**Files:**
- Modify: `internal/store/project.go`
- Modify: `internal/store/task.go`
- Modify: `internal/store/comment.go`
- Modify: `internal/store/label.go`
- Create: `internal/store/eventsource_live_write_test.go`

**Interfaces:**
- Consumes: `projectFormat`, `appendV2Locked`, `appendV2TaskCreatedLocked`, `appendV2CommentCreatedLocked`, `eventsource.FoldEvents`, `cacheProjectFromV2State`.
- Produces: v2-active write paths for project, label, task, and comment mutators.

- [ ] **Step 1: Write failing live-write test**

Create `internal/store/eventsource_live_write_test.go`:

```go
package store

import (
	"os"
	"testing"
)

func TestV2ActiveTaskMutationWritesOnlyEventsV2(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	before, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "v2 task", "desc", []string{"ATM:status:open"}, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("v1 log changed while project is v2-active")
	}
	snap, err := s.verifyV2File("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if snap.EventCount == 0 {
		t.Fatal("expected v2 events")
	}
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "v2 task" {
		t.Fatalf("task title = %q", got.Title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store -run TestV2ActiveTaskMutationWritesOnlyEventsV2 -count=1
```

Expected: v1 log changes or v2 cache does not update.

- [ ] **Step 3: Add v2 branch at each mutator entry**

At the start of these methods, after validation and before v1 append logic, branch by active format:

```go
	if f, err := s.projectFormat(projectCode); err != nil {
		return nil, err
	} else if f == StoreFormatV2 {
		return s.createTaskV2(projectCode, title, description, labels, actor)
	}
```

Use equivalent branches for:

- `CreateProject`: if default store active format is v2, call `createProjectV2`.
- `SetProjectName`: call `setProjectNameV2`.
- `CreateTask`, `SetTitle`, `SetDescription`, `TaskLabelAdd`, `TaskLabelRemove`, `RemoveTask`: call v2 variants.
- `CreateComment`, `SetCommentBody`, `CommentLabelAdd`, `CommentLabelRemove`, `RemoveComment`: call v2 variants.
- `LabelAdd`, `LabelSeed`, `LabelRemove`: call v2 variants.

- [ ] **Step 4: Implement v2 variants in existing domain files**

Each v2 variant should:

1. Hold `WithLock(code, ...)`.
2. Validate using the existing cache-backed helpers.
3. Resolve the target's identity from the fold state (`beginV2AuthorLocked` + `resolveTaskRef`/`resolveCommentRef`) — the fold keys every slot write for a mutation off `subject.id`, which is the entity's identity hash, never its alias.
4. Emit scalar/membership/removal v2 events with `appendV2Locked`; emit task/comment creation with the creation-specific helpers from Task 7; emit project creation with `eventsource.NewProjectCreated` for the uniform surface.
5. Re-read `events.v2.jsonl`, fold, and call `cacheProjectFromV2State`.
6. Return the compatibility row from cache.

Subject and payload contract per action (from `writesOf` in `internal/eventsource/fold.go` — these exact keys or the event writes no slots):

| Action | Subject | Payload keys |
|---|---|---|
| `task.title-changed` | `{Kind: "task", ID: <task identity>}` | `title` |
| `task.description-changed` | `{Kind: "task", ID: <task identity>}` | `description` |
| `task.label-added` / `task.label-removed` | `{Kind: "task", ID: <task identity>}` | `label` (string or list) |
| `task.removed` / `task.restored` | `{Kind: "task", ID: <task identity>}` | — |
| `comment.body-changed` | `{Kind: "comment", ID: <comment identity>}` | `body` |
| `comment.label-added` / `comment.label-removed` | `{Kind: "comment", ID: <comment identity>}` | `label` (string or list) |
| `comment.removed` | `{Kind: "comment", ID: <comment identity>}` | — |
| `project.name-changed` | `{Kind: "project", ID: <project identity>, Code: code}` | `name` |
| `project.removed` | `{Kind: "project", ID: <project identity>, Code: code}` | — |
| `label.upserted` | `{Kind: "label", Name: name}` | `description` and/or `expr` (only the fields being set) |
| `label.removed` | `{Kind: "label", Name: name}` | — |

Creation events (`task.created`, `comment.created`, `project.created`) go through the Task 7 helpers only; L3 never assembles a creation payload or mints an alias itself (ATM-0125).

Concrete `createTaskV2` shape:

```go
func (s *Store) createTaskV2(projectCode, title, description string, labels []string, actor string) (*Task, error) {
	var created *Task
	err := s.WithLock(projectCode, func() error {
		if _, err := s.getProjectLocked(projectCode); err != nil {
			return err
		}
		_, alias, err := s.appendV2TaskCreatedLocked(projectCode, title, description, labels, actor)
		if err != nil {
			return err
		}
		snap, err := s.verifyV2File(projectCode)
		if err != nil {
			return err
		}
		state, err := eventsource.FoldEvents(snap.Events)
		if err != nil {
			return err
		}
		if err := s.cacheProjectFromV2State(projectCode, state, snap.EventCount); err != nil {
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

The same pattern applies to comments through `appendV2CommentCreatedLocked`; L3 never constructs task/comment creation aliases itself.

- [ ] **Step 5: Run focused write tests**

Run:

```bash
go test ./internal/store -run 'TestV2ActiveTaskMutationWritesOnlyEventsV2|Test.*Task|Test.*Comment|Test.*Label|Test.*Project' -count=1
```

Expected: all focused store tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/store/project.go internal/store/task.go internal/store/comment.go internal/store/label.go internal/store/eventsource_live_write_test.go
git commit -m "feat(ATM-0107): route v2-active mutations to events.v2.jsonl"
```

---

### Task 9: v2 Reads, History, and Store Log Display

**Files:**
- Modify: `internal/store/project.go`
- Modify: `internal/store/task.go`
- Modify: `internal/store/comment.go`
- Modify: `internal/store/query.go`
- Modify: `internal/store/log.go`
- Modify: `internal/cli/store.go`
- Create: `internal/store/eventsource_live_read_test.go`

**Interfaces:**
- Consumes: `projectFormat`, v2 cache freshness, `readV2File`.
- Produces:
  - v2-aware point reads and list reads.
  - `store log <CODE>` displays v2 event ordinals for v2-active projects.

- [ ] **Step 1: Write failing read/log tests**

Create `internal/store/eventsource_live_read_test.go`:

```go
package store

import "testing"

func TestV2ActiveReadRebuildsMissingCache(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	tk, _ := s.CreateTask("ATM", "before", "", nil, "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM tasks`)
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "before" {
		t.Fatalf("title = %q", got.Title)
	}
}
```

Append to `internal/cli/store_test.go`:

```go
func TestStoreLogShowsV2EventsForV2ActiveProject(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_ = runArgsOut(t, st, "store", "upgrade", "--project", "ATM")
	out := runArgsOut(t, st, "store", "log", "ATM")
	mustContain(t, out, "project.created")
	mustContain(t, out, "sha256:")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/store -run TestV2ActiveReadRebuildsMissingCache -count=1
go test ./internal/cli -run TestStoreLogShowsV2EventsForV2ActiveProject -count=1
```

Expected: cache miss or log output failure.

- [ ] **Step 3: Add v2 cache freshness path to point reads**

In `GetProject`, `GetTask`, and `GetComment`, when `projectFormat(code) == StoreFormatV2`, use a helper:

```go
func (s *Store) rebuildProjectFromV2(code string) error {
	snap, err := s.verifyV2File(code)
	if err != nil {
		return err
	}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		return err
	}
	return s.cacheProjectFromV2State(code, state, snap.EventCount)
}
```

The v2 point-read pattern should be:

```go
if f, _ := s.projectFormat(code); f == StoreFormatV2 {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	if fresh, _ := s.v2CacheFresh(code); !fresh {
		if err := s.WithLock(code, func() error { return s.rebuildProjectFromV2(code) }); err != nil {
			return nil, err
		}
	}
	return cacheGetTask(db, id)
}
```

Define the freshness probe alongside it — it must be cheaper than a full parse:

```go
// v2CacheFresh compares the cached freshness value (cacheGetV2Freshness)
// against the current event count, taken as a newline count of
// events.v2.jsonl without parsing events. A missing file counts as zero.
func (s *Store) v2CacheFresh(code string) (bool, error)
```

The v2 branch must run BEFORE the existing v1 freshness checks (`cache LogSeq > log LastSeq` → `ErrIntegrity`): v2 cache rows carry creation ordinals in `LogSeq` that are unrelated to the v1 log sequence.

- [ ] **Step 4: Add v2 log streaming**

In `internal/cli/store.go`, in `store log` command, branch on project format:

```go
if f, _ := s.ProjectFormatForCLI(args[0]); f == store.StoreFormatV2 {
	events, err := s.ReadV2LogForDisplay(args[0])
	if err != nil {
		return err
	}
	if st.isJSON() {
		return writeJSON(st.stdout(), events)
	}
	for _, e := range events {
		fmt.Fprintf(st.stdout(), "%d\t%s\t%s\t%s\t%s\t%s\n", e.Ordinal, store.RFC3339UTC(e.At), e.Actor, e.Action, e.Subject, e.ID)
	}
	return nil
}
```

Add exported store helpers:

```go
type V2LogView struct {
	Ordinal int       `json:"ordinal"`
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Actor   string    `json:"actor"`
	Action  string    `json:"action"`
	Subject string    `json:"subject"`
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/store -run TestV2ActiveReadRebuildsMissingCache -count=1
go test ./internal/cli -run TestStoreLogShowsV2EventsForV2ActiveProject -count=1
```

Expected: tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/store internal/cli/store.go internal/cli/store_test.go
git commit -m "feat(ATM-0107): read and inspect v2-active projects"
```

---

### Task 10: Replica-Copy Detection

**Files:**
- Create: `internal/store/eventsource_replica.go`
- Create: `internal/store/eventsource_replica_test.go`
- Modify: `internal/store/eventsource_author.go`

**Interfaces:**
- Consumes: `StoreMeta.StoreInstanceID`, `StoreMeta.ReplicaID`, `eventsource.MintReplicaID`.
- Produces:
  - `func (s *Store) ensureReplicaForWriteLocked() (string, error)`
  - `func (s *Store) localInstanceMarkerPath() string`

- [ ] **Step 1: Write failing replica-copy test**

Create `internal/store/eventsource_replica_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopiedStoreRemintsReplicaBeforeWrite(t *testing.T) {
	original := testStore(t)
	_, _ = original.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = original.UpgradeProjectToV2("ATM")
	first, err := original.ensureReplicaForWriteLocked()
	if err != nil {
		t.Fatal(err)
	}
	copyDir := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(original.StorePath(), copyDir); err != nil {
		t.Fatal(err)
	}
	copied, err := Open(copyDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := copied.ensureReplicaForWriteLocked()
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatalf("copied store kept replica id %s", first)
	}
}
```

Implement `copyTree` in the test file using `filepath.WalkDir`, `os.MkdirAll`, and `os.ReadFile`/`os.WriteFile`.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store -run TestCopiedStoreRemintsReplicaBeforeWrite -count=1
```

Expected: undefined replica-copy helper or same replica id.

- [ ] **Step 3: Implement marker-based detection**

Create `internal/store/eventsource_replica.go`:

```go
package store

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"

	"atm/internal/eventsource"
)

type localInstanceMarker struct {
	StoreInstanceID string `json:"store_instance_id"`
	ReplicaID       string `json:"replica_id"`
	StorePath       string `json:"store_path"`
}

func (s *Store) localInstanceMarkerPath() string {
	return filepath.Join(s.Root, ".atm-local-instance.json")
}

func (s *Store) ensureReplicaForWriteLocked() (string, error) {
	m, err := s.readStoreMeta()
	if err != nil {
		return "", err
	}
	if m.StoreInstanceID == "" {
		m.StoreInstanceID, err = eventsource.MintReplicaID(rand.Reader)
		if err != nil {
			return "", err
		}
	}
	if m.ReplicaID == "" {
		m.ReplicaID, err = eventsource.MintReplicaID(rand.Reader)
		if err != nil {
			return "", err
		}
	}
	var marker localInstanceMarker
	raw, readErr := os.ReadFile(s.localInstanceMarkerPath())
	if readErr == nil {
		_ = json.Unmarshal(raw, &marker)
	}
	if marker.StoreInstanceID != "" && marker.StoreInstanceID == m.StoreInstanceID && marker.StorePath != s.Root {
		m.ReplicaID, err = eventsource.MintReplicaID(rand.Reader)
		if err != nil {
			return "", err
		}
	}
	if err := s.writeStoreMeta(m); err != nil {
		return "", err
	}
	marker = localInstanceMarker{StoreInstanceID: m.StoreInstanceID, ReplicaID: m.ReplicaID, StorePath: s.Root}
	out, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return "", err
	}
	out = append(out, '\n')
	if err := os.WriteFile(s.localInstanceMarkerPath(), out, 0o644); err != nil {
		return "", err
	}
	return m.ReplicaID, nil
}
```

Change `currentReplicaIDLocked` in `eventsource_author.go` to call `ensureReplicaForWriteLocked`.

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/store -run TestCopiedStoreRemintsReplicaBeforeWrite -count=1
```

Expected: test passes.

- [ ] **Step 5: Commit**

```bash
git add internal/store/eventsource_replica.go internal/store/eventsource_replica_test.go internal/store/eventsource_author.go
git commit -m "feat(ATM-0107): remint replica ids for copied stores"
```

---

### Task 11: End-to-End Verification and Documentation Polish

**Files:**
- Modify: `README.md`
- Modify: `internal/cli/conventions.go`
- Modify: `internal/cli/testdata/golden/*` if conventions goldens change
- Create: `internal/store/eventsource_e2e_test.go`

**Interfaces:**
- Consumes: all prior tasks.
- Produces: final regression coverage and user-facing docs.

- [ ] **Step 1: Write end-to-end test**

Create `internal/store/eventsource_e2e_test.go`:

```go
package store

import "testing"

func TestEventsourceV2EndToEndUpgradeWriteRebuildVerifyRollbackReupgrade(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.CreateTask("ATM", "before", "", []string{"ATM:status:open"}, "admin@cli:unset")
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "after v2", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if r, err := s.VerifyProject("ATM"); err != nil {
		t.Fatal(err)
	} else if r.Diverged || !r.LogOK || r.Format != StoreFormatV2 {
		t.Fatalf("verify after v2 write = %#v", r)
	}
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if tasks := s.ListTasks(QueryFilters{Project: "ATM"}); len(tasks) != 2 {
		t.Fatalf("tasks after rebuild = %d, want 2", len(tasks))
	}
	if _, err := s.RollbackProjectToV1("ATM"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "after rollback", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	if tasks := s.ListTasks(QueryFilters{Project: "ATM"}); len(tasks) != 2 {
		t.Fatalf("tasks after re-upgrade = %d, want 2 v1-derived tasks", len(tasks))
	}
}
```

- [ ] **Step 2: Run focused end-to-end test**

Run:

```bash
go test ./internal/store -run TestEventsourceV2EndToEndUpgradeWriteRebuildVerifyRollbackReupgrade -count=1
```

Expected: test passes; the v2-only task is absent after rollback/re-upgrade, matching the spec.

- [ ] **Step 3: Run documentation/golden tests**

Run:

```bash
go test ./internal/cli -run 'Test.*Conventions|TestDeterminism|TestStore' -count=1
```

Expected: tests pass. If golden tests intentionally fail because conventions text changed, run the repository's documented `-update` command for those tests, inspect the fixture diff, and commit the updated goldens.

- [ ] **Step 4: Run full verification**

Run:

```bash
gofmt -w internal/store internal/cli
go build ./...
go test ./...
make verify
```

Expected: all commands pass.

- [ ] **Step 5: Commit**

```bash
git add README.md internal/cli internal/store
git commit -m "test(ATM-0107): cover eventsource v2 storage end to end"
```

---

## Self-Review Checklist

- L3-1 single `events.v2.jsonl`: Tasks 2, 4, 7, 8.
- L3-2 preserved v1 `log.jsonl`: Tasks 4, 6, 11.
- L3-3 verified side-by-side cutover: Tasks 4, 5, 6.
- L3-4 rollback without v2-to-v1 export: Tasks 4, 6, 11.
- L3-5 re-upgrade archive/replace: Task 4.
- L3-6 lock-scoped frontier/HLC: Task 7.
- L3-7 append commit and crash recovery: Task 2.
- L3-8 replica-copy detection: Task 10.
- L3-9 `internal/store` compatibility API: Tasks 3, 5, 8, 9.
- L3-10 README instructions: Task 6 and Task 11.

## Execution Handoff

The ATM-0125 gate is satisfied: the task closed 2026-07-14 (commit `8f7ed12`) with the creation helpers `NewTaskCreated` / `NewCommentCreated` / `NewProjectCreated` in `internal/eventsource/author.go`, and this plan was revised the same day against the merged API at commit `88f9b1b` (see the **Revised** note at the top). Execute tasks in order, starting with Task 0 as a sanity gate. Use frequent commits exactly as listed so failures can be bisected by storage layer.
