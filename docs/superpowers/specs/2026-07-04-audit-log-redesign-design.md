# Audit Log Redesign — Design Spec

**Status:** Approved (full rewrite of the audit/history subsystem)
**Approach:** Wholesale rebuild (Approach A — no migration, no compat with v2's embedded `[]HistoryEntry`).

## Driver

v2's audit surface is an embedded `[]HistoryEntry` on each `Task` and `Project`
JSON file. It is a visibility/attribution log, not a source of truth:

- Mutations other than label add/remove carry an **empty `Meta`** — there is no
  before/after, so history cannot be replayed to reconstruct state.
- `RemoveTask`, `RemoveProject`, and `LabelRemove` are silent deletions — no
  tombstone, no audit entry. Deleted entities are indistinguishable from
  never-existing ones.
- The global `labels.json` registry has **no audit trail at all**.
- Mutations on the Task entity are stamped with fragile IDs (`len(history)+1`),
  and the project history is the only narrative record of project rename — there
  is no way to know a project ever existed once it is removed.

The v3 (this redesign) vision: the append-only per-project log becomes the
source of truth of the whole store, like a WAL in a database. The entity JSON
files become materialized views (caches) regenerated from the log. Integrity is
defined operationally: **the log is deterministically replayable to reconstruct
the current state**. No silent deletes, no orphan entities, no unreplayable
mutations.

The log stays a generic mutation substrate. v2's principle that *the store has
no intrinsic workflow knowledge* survives event sourcing intact: the action enum
is closed by verb (`created` / `changed` / `removed` / `upserted`), open by
subject kind. Activity classification (analyze / implement / ask) stays in
label space; the log records that an event happened, not what "kind of work" it
represents.

## Decisions (locked)

1. **True event sourcing** — state files are replay products; the log is the
   only durable truth.
2. **One log per project** — `projects/<CODE>/log.jsonl`; preserves v2's
   per-project lock and project-as-namespace-owner principle.
3. **Integrity = deterministic replayability** — no cryptographic
   tamper-evidence, no signatures. Integrity means: no silent deletions, no
   orphan entities, every mutation reconstructable from the log alone.
4. **Payload = full after-state per event** — each entry is individually
   sufficient; replay is "last write wins on subject."
5. **Approach A — wholesale rebuild, no migration.**
6. **Cache triggers: write-through + lazy miss + `rebuild` + `verify --repair`**.
   No boot rebuild. Lazy miss compares `cache.log_seq` against `log.last_seq`.
7. **Label events route to the prefix project's log**; the global `labels.json`
   becomes a derived view.
8. **Embedded `[]HistoryEntry` deleted** from Task and Project structs.
9. **`log_seq` embedded in each cache file** as the staleness signal.
10. **Crash recovery = self-healing via `log_seq`**; partial log lines
    truncated at next read; no fsync/rename barriers.
11. **No compaction** — full audit history forever; defer until needed.
12. **Surface: `atm store log <CODE>`, `atm store verify [--repair]`,
    `atm store rebuild`**. No TUI log viewer in v1.

## Architecture & data flow

```
$ATM_HOME/
  labels.json                              # derived registry (rebuildable)
  projects/
    <CODE>.json                            # Project cache (derived)
    <CODE>/
      log.jsonl                            # THE source of truth for this project
      tasks/<CODE>-<NNNN>.json             # Task cache (derived)
    <CODE>.lock                            # per-project file lock (unchanged)
```

`labels.json` and `projects/<CODE>.json` and `projects/<CODE>/tasks/<ID>.json`
are all materialized views. The log file is the only durable source.

### Write flow

All steps run under `WithLock(code)` for the (prefix) project's lock:

1. Compose the `LogEntry` (action, subject, full after-state payload, `at`,
   `actor`, `seq` = `last_seq + 1`).
2. Append the entry as a single line to `log.jsonl`. **This is the commit
   point** — once appended, the mutation is durable.
3. Update the affected cache file(s) with the new full entity state, stamping
   the entry's `seq` into the cache's `log_seq` field.
4. For label events, also refresh the derived `labels.json` so non-replay reads
   stay fast.

### Read flow

Two distinct repair paths, by cache kind:

- **Per-entity caches** (`projects/<CODE>.json`, `projects/<CODE>/tasks/<ID>.json`)
  get O(1) lazy miss. Read the cache file; compare its `log_seq` against
  `LastLogSeq(code)`. If missing or `cache.log_seq < log.last_seq`, replay that
  one entity's events from the log under the lock and rewrite the cache. If
  `cache.log_seq > log.last_seq`, surface `ErrIntegrity`.

- **`labels.json`** is a *cross-partition* derived file (it aggregates
  `label.*` events from every project log). Lazy miss on it would require
  scanning every project log, so it is **not** lazily self-healing. It is kept
  warm by write-through after every `label.*` event, and is repaired only by
  explicit `atm store rebuild` or `atm store verify --repair`. Reads from
  `labels.json` trust the cache until told otherwise. The per-label `LogSeq`
  field inside `labels.json` is audit metadata (the seq of the last
  `label.*` event for that name), not a lazy-miss trigger.

- **History render** (TUI detail, `task show` History section): read `log.jsonl`
  directly, filter to entries whose `Subject` matches the entity, render the
  tail. No derived history array exists on the entity.

### Cross-cutting

- `labels.json` is a derived view: write-through keeps it warm; `atm store
  rebuild` regenerates it from all `label.*` events across every project log.
- Tombstones (`task.removed` / `project.removed` / `label.removed`) carry the
  entity's last full state in `payload` so replay knows what was removed; the
  cache file is deleted so the live set matches the replayed set. Replay of the
  live set = `{created events} − {removed events, last-write-wins on subject}`.

## Components & store API surface

### New types (`internal/store/types.go`)

```go
// LogEntry is one line in projects/<CODE>/log.jsonl. Append-only.
// (at, actor) is the natural key; seq is the per-project monotonic tiebreaker.
type LogEntry struct {
    Seq     int            `json:"seq"`               // monotonic per project
    At      time.Time      `json:"at"`                // RFC3339 UTC
    Actor   string         `json:"actor"`             // free-form
    Action  string         `json:"action"`            // closed enum (see below)
    Subject Subject        `json:"subject"`           // (kind, id|code|name)
    Payload json.RawMessage `json:"payload,omitempty"` // full after-state
}

// Subject identifies the entity an entry is about.
type Subject struct {
    Kind string `json:"kind"`                 // "project" | "task" | "label"
    ID   string `json:"id,omitempty"`         // task: "ATM-0001"
    Code string `json:"code,omitempty"`       // project: "ATM"
    Name string `json:"name,omitempty"`        // label: "ATM:status:open"
}
```

Existing `Project` and `Task` structs are slimmed (no `History`, add `LogSeq`):

```go
type Project struct {
    Code      string    `json:"code"`
    Name      string    `json:"name"`
    NextTaskN int      `json:"next_task_n"`
    LogSeq    int       `json:"log_seq"`        // last log entry that built this cache
    CreatedAt time.Time `json:"created_at"`
    CreatedBy string   `json:"created_by"`
    UpdatedAt time.Time `json:"updated_at"`
    UpdatedBy string   `json:"updated_by"`
}

type Task struct {
    ID          string   `json:"id"`
    ProjectCode string   `json:"project_code"`
    Title       string   `json:"title"`
    Description string   `json:"description,omitempty"`
    Labels      []string `json:"labels"`
    LogSeq      int      `json:"log_seq"`
    CreatedAt   time.Time `json:"created_at"`
    CreatedBy   string   `json:"created_by"`
    UpdatedAt   time.Time `json:"updated_at"`
    UpdatedBy   string   `json:"updated_by"`
}

type Label struct {
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    LogSeq      int    `json:"log_seq,omitempty"`
}
```

`HistoryEntry` is **deleted** (replaced by `LogEntry`). The `historyToJSON`
helper in `internal/cli/output.go` switches from a `[]HistoryEntry` input to a
`[]HistoryView` rendered from `LogEntry`s.

### New store file: `internal/store/log.go`

```go
// AppendLog appends one LogEntry to projects/<CODE>/log.jsonl under the
// project's lock. Assigns seq = lastSeq + 1. Returns the appended entry
// (with seq filled) for the caller to stamp into cache files.
func (s *Store) AppendLog(code string, e LogEntry) (LogEntry, error)

// ReadLog streams entries from projects/<CODE>/log.jsonl, oldest first.
// Truncates trailing malformed bytes (records how many bytes were truncated
// for the verify output). Stops at EOF or first unrecoverable line.
func (s *Store) ReadLog(code string) ([]LogEntry, error)

// LastLogSeq returns the last successfully appended seq, or 0 if the log is empty.
func (s *Store) LastLogSeq(code string) (int, error)

// Replay applies every log entry for a project to a fresh State, returning
// the materialized live set. Tombstones subtract from the live set; same-subject
// events are last-write-wins on the payload snapshot.
func (s *Store) Replay(code string) (*ReplayState, error)
```

`ReplayState`:

```go
type ReplayState struct {
    Project *Project
    Tasks   []*Task   // live tasks, sorted by ID
    Labels  []Label   // labels touched by this project's label.* events
}
```

### Rewritten store files

All existing mutations go through a `appendAndCache(entry, mutatingFn)` helper.
Today's `mutateTask` / `mutateProject` helpers become that pattern.

**`internal/store/task.go`** — `CreateTask`, `SetTitle`, `SetDescription`,
`TaskLabelAdd`, `TaskLabelRemove`, `RemoveTask`:
1. Compose `LogEntry{Action: "task.*", Subject: {task, id}, Payload: full Task after state}`.
2. `AppendLog` → seq assigned.
3. Write the cache file (`tasks/<ID>.json`) with `LogSeq = seq`.
4. For `task.removed`: append the tombstone entry (with the last full state in
   payload), then `os.Remove` the cache file.

**`internal/store/project.go`** — `CreateProject`, `SetProjectName`,
`RemoveProject`:
- `CreateProject` is the epoch: it creates `projects/<CODE>/`, opens
  `log.jsonl`, and appends the very first entry `{Action: "project.created",
  Subject: {project, code}, Payload: full Project after state}`. The first
  entry's `seq` is 1. Default-label seeding runs inside the same lock and
  appends `label.upserted` events after the `project.created` entry.
- `SetProjectName` appends a `project.name-changed` event whose payload
  carries the new full `Project`.
- `RemoveProject` (still under zero-task guard) appends `project.removed` then
  deletes the project directory **including** `log.jsonl`. The log goes with
  the project — there is no project-scoped audit history left to retain.
  Replay for the live project registry reflects no entry from this prefix.

**`internal/store/label.go`** — `LabelAdd`, `LabelSeed`, `LabelRemove`,
`autoRegisterLabels`:
- All append `label.upserted` (add/seed/auto-register) or `label.removed` to
  the prefix project's log (same lock as the prefix project's log).
- The derived `labels.json` is kept warm by write-through after every label
  event.
- `autoRegisterLabels` continues to be called by `CreateTask`/`TaskLabelAdd`,
  but now it appends one `label.upserted` event per newly-registered label
  (kept sort-stable in log order).

### Action enum (closed, validated in `AppendLog`)

| Action | Subject | Payload |
|---|---|---|
| `project.created` | project | full Project |
| `project.name-changed` | project | full Project |
| `project.removed` | project | full Project (last state before removal) |
| `task.created` | task | full Task |
| `task.title-changed` | task | full Task |
| `task.description-changed` | task | full Task |
| `task.label-added` | task | full Task |
| `task.label-removed` | task | full Task |
| `task.removed` | task | full Task (last state) |
| `label.upserted` | label | full Label |
| `label.removed` | label | full Label (last state) |

11 actions in v1. The enum is **closed by verb, open by subject kind**: future
entity kinds (e.g. `comment`) inherit the same `{created, changed, removed,
upserted}` verbs scoped to their subject kind. New workflow classifications
(analyze / implement / ask) do **not** add verbs; they live in label space.

The old `Meta["label"]` field is gone — the full Task payload *contains* the
new label set, so the field is implicit.

### Multi-subject mutation atomicity (label assignment)

When a mutation touches multiple subjects (the new-label assignment case, or
default label seeding), the rule is:

> **All log entries for one mutation append before any cache file writes.**

Order guarantees:

- **Log-before-cache.** If we crash between `AppendLog` and `writeCacheFile`,
  the cache is stale (`cache.LogSeq < log.LastSeq`); lazy miss rebuilds.
- **Labels-before-references.** For multi-subject mutations, `label.*` events
  append before any entity event that references the new label. So at any
  replay boundary line K, if line K references `ATM:status:open`, an earlier
  `label.upserted` for `ATM:status:open` exists at line <K. Replay is naive —
  apply in order — and never sees a dangling reference.

For `TaskLabelAdd(taskID, "ATM:activity:analysis", actor)` where the label is
new:

```
WithLock(ATM):
  lastSeq := LastLogSeq(ATM)              # e.g. 12
  e1 := AppendLog(ATM, label.upserted)    # seq=13
  e2 := AppendLog(ATM, task.label-added)  # seq=14, payload has Task with labels including analysis
  writeCacheFile(label, LogSeq=13)
  writeCacheFile(task, LogSeq=14)
  refreshDerivedLabelsJSON()
```

When the label is already registered, only the `task.label-added` entry is
appended (1 entry). When N new labels are added in one mutation, N+1 entries
append in sort order, all under the same lock.

### Default-label seeding at project create

```
WithLock(ATM):
  AppendLog(project.created)              # seq=1
  for each default label in sort order:
    AppendLog(label.upserted)             # seq=2, 3, ...
  writeCacheFile(project)
  for each default label: writeCacheFile(label)
  refreshDerivedLabelsJSON()
```

### History render API

To keep the CLI/TUI output schema stable for consumers, a compact view struct
is rendered from `LogEntry`:

```go
// HistoryView is a compact read projection from LogEntry.
type HistoryView struct {
    Seq    int       `json:"seq"`
    Action string    `json:"action"`
    Actor  string    `json:"actor"`
    At     time.Time `json:"at"`
}
```

`store.History(code, subject) []HistoryView` — scans `log.jsonl`, filters
entries whose `Subject` matches, returns the seq/action/actor/at tuple per
event (no payload — that's for replay, not for human display). The TUI/CLI
render this identical to today's history rows, plus an optional `[seq]`
decoration.

### Verify and rebuild (`internal/store/verify.go`, `internal/store/rebuild.go`)

```go
type VerifyReport struct {
    Project    string
    LogEntries int
    LogOK      bool             // log parses cleanly, no truncation needed
    Truncated  int              // bytes truncated from malformed tail
    Caches     []CacheCheck
    Diverged   bool
}

type CacheCheck struct {
    Path         string
    Status       string  // "ok" | "stale" | "missing" | "corrupt"
    CacheLogSeq  int
    LastEventSeq int
}
```

`Verify()` runs `Replay` for each project, then for each live entity compares
the replayed state to the on-disk cache. Mismatches recorded with the
recommended fix.

`Rebuild()` is `Verify --repair`'s implementation:

```go
// Rebuild regenerates every cache file from the logs. Top-level:
//   - Replay every project log
//   - Write each Project / Task / Label cache
//   - Rewrite labels.json from all label.* events across projects
func (s *Store) Rebuild() (*RebuildReport, error)
```

### CLI surface (`internal/cli/store.go` — new file)

```
atm store path                                  # unchanged
atm store log <CODE> [--from N] [--to N] [--output json|text]  # NEW
atm store verify [--repair]                     # NEW
atm store rebuild                               # NEW
```

`atm store log` is agents' primary audit surface — they can `tail` the JSONL
or grep it. Defaults to text table (seq / at / actor / action / subject);
`--output json` returns the raw entries.

## Concurrency, ordering, crash recovery, error handling

### Lock scope and the partition invariant

The lock primitive itself does not change: `WithLock(code)` per project
(`<CODE>.lock`), held for the duration of one complete mutation. The new
invariant the lock protects:

> Every log entry with `Subject.Code == <CODE>` (or a label subject whose
> prefix is `<CODE>`) is appended to `projects/<CODE>/log.jsonl`, and is
> appended *only* while the project lock is held.

The lock now protects more state than before: the log line (commit point), the
cache file, and (for label mutations) the global `labels.json` derived file.
Label mutations take *their prefix project's* lock; that lock is the same lock
that protects `log.jsonl` for that prefix project. So `labels.json` is
rewritten only while a project lock is held; cross-project label edits
serialize on different lock files and don't collide on the global file because
their label prefix differs. This preserves v2's existing `labelProject(name)`
lock-router, now extended to cover the log append as well.

There is no separate global log and no separate global lock.

### Crash recovery (lazy self-healing)

The write order `append log → write cache → (optional derived refresh)` under
one lock produces four crash windows. Recovery uses the `log_seq` comparison;
there is no separate journal/recovery code.

**Window 1 — Log append partial, cache never written.** On next read,
`ReadLog` parses line-by-line. A partial trailing line (no terminating
newline / broken JSON) is truncated; the truncated byte count is reported in
the parse report. The next successful mutation's `AppendLog` writes a new line.

**Window 2 — Log line fully appended, cache write failed (or partial).** Cache
file is absent or contains `LogSeq < log.LastSeq`. Lazy miss on next read
detects this, replays that one entity from its first event to the latest,
rewrites the cache.

**Window 3 — Both committed, but derived `labels.json` write failed.** The
global registry is a cross-partition derived file (see Read flow) and is not
lazily self-healing. The next `atm store verify` reports it as `stale` or
`missing`; `atm store verify --repair` or `atm store rebuild` regenerates it
from the union of `label.*` events across all project logs.

**Window 4 — Sequence number gap from manual editing.** If someone hand-edits
the log to delete a line, subsequent entries have `Seq` jumps. The lazy-miss
check (`cache.LogSeq < log.LastSeq`) still triggers a correct rebuild by
replaying the full log. The gap itself is reported by `Verify` but **not
auto-repaired** — logs are authoritative; manual edits are evidence the human
must reconcile.

### Error handling and exit codes

Reuses v2's sentinels (`ErrNotFound` / `ErrConflict` / `ErrUsage`) plus one
new one, `ErrIntegrity`, for log-specific failures that an agent should treat
differently from routine not-found.

| Situation | Sentinel | Code | Behavior |
|---|---|---|---|
| `AppendLog` partial line detected at next `ReadLog` | `ErrIntegrity` | 5 | `ReadLog` truncates, returns the partial-line byte count; caller may proceed or escalate |
| Sequence gap detected by `Verify` | `ErrIntegrity` | 5 | reported, never auto-repaired |
| Cache file unreadable (corrupt JSON, not just stale) | `ErrIntegrity` | 5 | `verify --repair` rebuilds from log; reads fall back to lazy-miss replay |
| Cache present but `LogSeq > log.LastSeq` | `ErrIntegrity` | 5 | cache claims a future seq; `--repair` rewrites from replay |
| `RemoveProject` called with live tasks | `ErrConflict` | 4 | unchanged |
| `TaskLabelAdd` references a label with non-existent prefix project | `ErrUsage` | 2 | unchanged from v2 |
| Mutation references a subject with no `created` event | `ErrUsage` | 2 | detected during replay; rare caller-side mistake |
| Cache `LogSeq < log.LastSeq` (ordinary staleness) | none | 0 | silent self-heal, not an error |
| Unknown action passed to `AppendLog` | `ErrUsage` | 2 | rejected before logging, no line appended |

Exit code 5 is new (v2 used 0/1/2/3/4). It gives agents a distinct signal to
run `atm store verify`.

### What does NOT change

- `WithLock`, `ReadJSON` / `WriteJSON`, `RFC3339UTC` / `Now`, `ParseTaskID` /
  `RenderTaskID` / `SortTaskIDs`, error sentinels, the per-project directory
  layout, `--store` path resolution, `ATM_HOME` defaults.
- `ValidateProjectCode`, `ValidateLabelName`, `labelProject`, free-form actor.
- CLI global flags, JSON output envelope, TUI lifecycle.
- Process startup does **not** rebuild caches. No "init phase" that scans logs.
- The closed action enum is enforced in `AppendLog`. Unknown action →
  `ErrUsage` before logging.

## Testing, verification & rollout

### Testing approach

Same layered structure as v2: unit tests per store file, mirroring today's
`task_test.go` / `project_test.go` / `label_test.go` / `query_test.go`, plus
new `log_test.go` / `verify_test.go` / `rebuild_test.go`. CLI tests use the
existing `testdata/` golden pattern. TUI tests mirror `app_test.go`.

**New store test invariants:**

| Area | Invariant |
|---|---|
| `AppendLog` | Monotone `seq`; unknown action rejected with `ErrUsage`; partial write (hand-truncated `log.jsonl`) recovered by `ReadLog` returning truncated byte count. |
| `ReadLog` | Parses oldest-first; stops at first unrecoverable line; returns `[]LogEntry` and parse report. |
| `Replay` | Deterministic — fixed `log.jsonl` → same `ReplayState` every call. Tombstones remove subjects. Multi-subject mutations replay in order; a `task.label-added` referencing a `label.upserted` earlier in the log has the label present in the materialized registry. |
| `Replay` (project create) | Replays `project.created` + N `label.upserted` → Project with seeded labels in derived registry view. |
| Cache self-heal (lazy miss) | Hand-delete cache → next read returns correct entity. Hand-write stale `log_seq` → next read triggers replay. Hand-write `log_seq > log.LastSeq` → `ErrIntegrity`. |
| Crash recovery windows | Simulate each of windows 1–4 by manipulating on-disk state between locked mutations; assert next read or `verify --repair` produces canonical state. |
| Ordering | `label.*` events appear before any entity event that references the label. Verified by `Replay` after every test mutation. |
| `Verify` | Detects stale cache, missing cache, corrupt cache, malformed trailing log bytes, seq gaps. Each reported with the right `CacheCheck.Status`. |
| `Rebuild` | After hand-corrupting every cache file and deleting `labels.json`, `Rebuild` regenerates them byte-identical to a clean write-through run. |
| `RemoveProject` | Appends `project.removed` tombstone, deletes project dir including `log.jsonl`. `ListProjects` no longer returns it. `atm store log <CODE>` returns `ErrNotFound`. Replay for the global registry reflects no labels from this prefix. |
| `task.removed` | Appends tombstone carrying the last full Task state in payload. `GetTask` returns `ErrNotFound`. `Replay` excludes the task. `History` view for that subject still renders `created` / `label-added` / etc. from the log (subject matches). |
| Log compaction | No compaction code exists; tests assert the log never shrinks after any sequence of mutations. |
| Action enum | Unknown action passed to `AppendLog` → `ErrUsage`, no line appended. |

**Carryover invariants from v2** (unchanged behavior, re-tested against the new
pipeline): project create validates `^[A-Z]{3,6}$` and refuses duplicates;
label prefix-project match; auto-registration; zero-task guard on
`RemoveProject`; `ListTasks` AND-intersects exact labels and ignores wildcards;
`GroupTasks` multi-membership with `others` bucket; `LabelRemove` reports
`retained_usage`; determinism (sorted JSON, stable ordering, RFC3339 UTC).

**CLI tests** (golden pattern, text + JSON):
- `atm store log <CODE>` text table and `--output json` shape.
- `atm store verify` clean report; `--repair` after hand-corruption.
- `atm store rebuild` after deleting caches.
- Existing command output schema preserved except: `history` array entries
  now carry `seq`, the `meta` field is gone, and a top-level `log_seq` is
  added.

**TUI tests**: project-detail `H` toggle renders a `HistoryView` slice derived
from `log.jsonl`; task-detail History section reads the same source. View
snapshot test updates the expected render to include the new `[seq]`
decoration. No new TUI pane (no log viewer in v1).

### Verification gate

`make verify` remains the gate (`make build && make test`). No new make
targets. `.golangci.yml` carries over unchanged. `internal/store/log_test.go`
and `internal/store/verify_test.go` are part of `make test` like every other
file.

### CLI / JSON surface impact

Deliberate, narrow changes:

| Field | Before | After | Reason |
|---|---|---|---|
| `task show --output json` `history[].meta` | present, sometimes empty | **removed** | replaced by `payload` (full after-state, used only for replay, not exposed in the reader-facing `HistoryView`) |
| `task show --output json` `history[].seq` | absent | **added** | log sequence is part of the audit trail; agents need it to reference log lines |
| `task show --output json` top-level `log_seq` | absent | **added** | cache's stamp, for clients to detect freshness |

All three are additive except `meta`, which is removed. The TUI text-mode
history render keeps its current shape (action / actor / relative time) plus
the new `[seq]` prefix.

The TUI/CLI parity table in `help.go` gets a new row for `atm store log /
verify / rebuild`. The `atm conventions` output mentions the audit log as one
of the agent's first-contact surfaces: `atm store log <CODE>` to read project
history.

### Rollout

Approach A — wholesale rebuild, no migration. One commit per layer, `make
verify` green before the next layer lands:

1. **Delete commit.** Drop `History []HistoryEntry` field and its append sites
   in `task.go` / `project.go` / `types.go`; drop the `HistoryEntry` type;
   drop today's `appendHistoryAt` helpers. Tree does not build between this
   commit and the store rebuild commit — expected and acceptable per v2's
   rollout precedent.
2. **`log.go` + log tests.** New `LogEntry`, `Subject`, action enum,
   `AppendLog` / `ReadLog` / `LastLogSeq` / `Replay`. Tests cover monotone seq,
   truncation recovery, replay determinism, tombstones.
3. **`task.go` / `project.go` / `label.go` rewrites + tests.** Every mutation
   becomes `AppendLog → writeCacheFile → (optional) refreshDerivedLabelsJSON`.
   `RemoveProject` writes a tombstone then deletes the project dir.
   `RemoveTask` writes a tombstone then deletes the cache. `LabelAdd` /
   `LabelSeed` / `autoRegisterLabels` / `LabelRemove` append `label.*` events;
   `labels.json` becomes purely derived and write-through.
4. **`verify.go` + `rebuild.go` + tests.** Implement `Verify`, `Rebuild`, and
   the cache `LogSeq` self-heal check in the read path (`GetTask` /
   `GetProject` / `LabelList`).
5. **`cli/store.go` + tests.** `atm store log`, `atm store verify --repair`,
   `atm store rebuild` commands with golden text/JSON output.
6. **`cli/output.go` + `tui/*` updates + tests.** `HistoryView` render from
   `LogEntry`; update golden files for the new `history[].seq` and top-level
   `log_seq`; update TUI history render.
7. **`docs` + `atm conventions` text + README.** Mention `atm store log` as
   an agent first-contact surface.

No data migration tool. v2 users delete their `$ATM_HOME` or point `--store`
at a fresh dir (same as v2 itself).

## Extensibility (forward-compatible design)

The log's action enum is closed by verb but open by subject kind. Future
entity kinds (e.g. `Comment`) inherit the same verbs scoped to their subject
kind:

```go
// Future: new Subject.Kind = "comment"
Subject{Kind: "comment", ID: "ATM-0001-c1"}
// Actions inherit:
comment.created    // payload: full Comment after-state, including parent_task_id and labels
comment.changed    // payload: full Comment after-state (e.g. body edit or label add)
comment.removed    // tombstone, payload: last state
```

Replay rules are unchanged. Adding `Comment` later is purely additive — no
new verbs, no schema change to `LogEntry`, no replay logic change.

**Activity classification stays in label space.** When an agent analyzes a
task and adds a comment, the action is `comment.created`; the comment's
payload includes `labels: ["ATM:activity:analysis"]`. A dashboard aggregating
"analyze vs implement vs ask" reads comments from logs and facets on
`ATM:activity:*` labels — the same filter-driven faceting the TUI Tasks tab
already uses. The log stays a generic mutation substrate; the workflow
taxonomy evolves in label space where agents and humans invent it without
store changes. v2's principle *the store has no intrinsic workflow knowledge*
survives event sourcing intact.

The split:

- **Log answers "what happened and when"** — entity mutations, ordered,
  replayable.
- **Labels answer "what kind of work it was"** — open namespace, advisory,
  facetable.

## Out of scope (v1 of the audit log redesign)

- **Compaction / log checkpointing.** Logs grow monotonically; no `--compact`
  flag, no auto-rotate, no snapshot-by-rewrite. Deferred until a project's log
  actually strains replay latency.
- **TUI raw log viewer.** `atm store log <CODE>` is the only raw-log surface
  in v1. A TUI pane rendering the project log tail is deferred.
- **Custom action verbs beyond the closed enum.** Workflow classifications
  stay in label space; the action enum stays bounded to `{created, changed,
  removed, upserted}`. Future entity kinds add the same four verbs scoped to
  their subject kind.
- **Cryptographic tamper-evidence.** No hash chaining, no signatures, no
  sealed heads. Integrity = deterministic replayability only, per decision.
- **Cross-project atomic mutations.** No transaction spanning multiple
  project logs. The per-project partition invariant stays.
- **Streaming / tailing the log.** `atm store log` reads and exits; no
  `--follow` / `tail -f` equivalent in v1.
- **Schema versioning of log entries.** No `schema_version` field on entries.
  The action enum + subject-kind pair is the implicit version. A future v2 of
  the audit log would add a `schema` field at log open.
- **Actor authentication / key signing.** Actor remains free-form string. No
  keyring, no signature on entries.
- **Comment entities.** Confirmed supported by the design, but not implemented
  in v1. Adding `comment.*` events is purely additive when ready.
- **Per-entry hash / Merkle structure.** Rejected with the cryptographic
  option; not needed for the replayability contract.