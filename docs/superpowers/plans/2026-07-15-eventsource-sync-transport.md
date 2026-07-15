# EventSource L4 Sync & Transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement set-reconciliation sync between replicas of the same v2 project over directory and git transports, with the `SyncTarget` interface ATM-0123 plugs into and the `atm store remote`/`atm store sync` CLI.

**Architecture:** New pure-protocol package `internal/eventsync` (interfaces, a pure `Plan` diff/validate function, dir/git targets, and a `Sync` orchestrator over a `LocalStore` interface); `internal/store` implements `LocalStore` (locked ingest with HLC observe + reprojection, bootstrap, remotes in project config); `internal/cli/store.go` gains the subcommands. Specs: `docs/eventsource/03-sync-transport.md` (L4-1..11), `docs/superpowers/specs/2026-07-15-eventsource-sync-transport-design.md` (I-1..7).

**Tech Stack:** Go 1.22+, existing `internal/eventsource` (Parse/BuildDAG/Fold/CompareEvents/Clock), existing `internal/store` v2 helpers (`readV2File`, `appendV2EventLineLocked`, `reprojectV2Locked`, `mutateStoreMeta`, `WithLock`), Cobra CLI, system `git` binary (no go-git).

## Global Constraints

- Sync is per-project set reconciliation; the union is atomic — any staged-validation failure (parse, missing parent, cycle, root mismatch) aborts the whole sync with local and remote untouched (L4-4).
- Remote event bytes are preserved verbatim end to end; ids are always recomputed via `eventsource.Parse`, never trusted from the wire (L0-2, D5).
- The same-project guard: both sides' unique `project.created` root event ids must match; mismatch is a refusal naming both roots (L4-3).
- Readers never assume event-file line order; all appends are in `BuildDAG` topological order (deterministic, `CompareEvents`-tiebroken) (L4-5).
- Ingest happens under the per-project lock, recomputes the missing set against the current file, uses the existing fsync append commit point, observes the max ingested HLC into `StoreMeta.LastHLC`, and reprojects the cache before releasing the lock (L4-5, I-3).
- Sync refuses v1-active projects with a pointer to `atm store upgrade` (I-4).
- Remotes are named, per-project, replica-local in `projects/<CODE>/config.json`; `origin` is the default name (L4-8, I-5).
- Directory publish is `O_APPEND` whole lines + fsync, never rewrite-and-rename (L4-7).
- Git publish retries non-fast-forward pushes: refetch, recompute missing, retry, 3 attempts max; commit message `chore(atm-sync): <CODE> +N events`; `.gitattributes` `merge=union` entry on first publish; system git only, missing binary is a clear error (L4-7, I-6, I-7).
- Every failure's recovery is re-running sync — no journal, no resume tokens; "pulled OK, push failed" is a legal reported state.
- No new dependencies in go.mod.
- Run `gofmt` on changed Go files and `make verify` before declaring any task done.

---

## File Structure

- Create: `internal/eventsync/target.go` — `RawEvent`, `RemoteSnapshot`, `SyncTarget`, `Narrowing`, `SetDigest`, URL parsing/transport selection.
- Create: `internal/eventsync/engine.go` — `Plan` (pure), `PlanResult`, root guard, `Sync` orchestrator + `LocalStore` interface + `Report`.
- Create: `internal/eventsync/dirtarget.go` — directory transport.
- Create: `internal/eventsync/gittarget.go` — git transport.
- Create: `internal/store/eventsource_sync.go` — `SyncSnapshot`, `SyncIngest`, `SyncBootstrap` (implements `eventsync.LocalStore` structurally), remote-config accessors.
- Modify: `internal/store/config.go` — `ProjectConfig.Remotes`.
- Modify: `internal/cli/store.go` — `remote` and `sync` subcommands.
- Modify: `README.md` — "Syncing between machines" section.
- Tests alongside each file (`*_test.go`), plus `internal/store/eventsource_sync_e2e_test.go` for convergence.

---

### Task 1: eventsync types, digest, and the pure Plan engine

**Files:**
- Create: `internal/eventsync/target.go`
- Create: `internal/eventsync/engine.go`
- Test: `internal/eventsync/engine_test.go`

**Interfaces:**
- Consumes: `eventsource.Parse([]byte) (*Event, error)`, `eventsource.BuildDAG([]*Event) (*DAG, error)`, `(*DAG).Events() []*Event` (deterministic topo order).
- Produces (later tasks rely on these exact names):

```go
package eventsync

type RawEvent struct {
    ID  string // recomputed from Raw via eventsource.Parse
    Raw []byte // canonical line bytes, verbatim
}

type RemoteSnapshot struct {
    Absent bool
    Events []RawEvent
    Digest string
    State  any // transport-private (e.g. git head)
}

type SyncTarget interface {
    Fetch(ctx context.Context, project string) (*RemoteSnapshot, error)
    Publish(ctx context.Context, project string, missing []RawEvent, base *RemoteSnapshot) error
}

type Narrowing interface {
    Frontier(ctx context.Context, project string) (digest string, heads []string, err error)
    FetchSince(ctx context.Context, project string, haves []string) ([]RawEvent, error)
}

func SetDigest(ids []string) string // "sha256:" + hex sha256 of sorted ids joined by "\n"

type PlanResult struct {
    ToIngest  []*eventsource.Event // topological order, ready to append
    ToPublish []RawEvent
    RemoteAbsent bool
    LocalAbsent  bool
}

var ErrRootMismatch = errors.New("eventsync: different projects (root project.created mismatch); cross-project merge is a separate operation")

func Plan(local []*eventsource.Event, remote *RemoteSnapshot) (*PlanResult, error)
```

- [ ] **Step 1: Write the failing tests** — `internal/eventsync/engine_test.go`. Build local/remote sets with `eventsource.NewProjectCreated`/`NewTaskCreated` (see `internal/eventsource/author_test.go` for the pattern: `NewClock(func() int64 {...})`, replica ids like `"r_aaaaaaaaaa"`). Cases:

```go
func TestPlanDisjointSetsDiffBothWays(t *testing.T)   // A has root+e1, remote has root+e2 → ToIngest=[e2], ToPublish=[e1]
func TestPlanIdenticalSetsNoOp(t *testing.T)          // same events both sides → empty diffs
func TestPlanRemoteAbsentPublishesAll(t *testing.T)   // RemoteSnapshot{Absent:true} → ToPublish = all local, RemoteAbsent
func TestPlanLocalAbsentIngestsAll(t *testing.T)      // local nil → ToIngest = all remote topo-ordered, LocalAbsent
func TestPlanRootMismatchRefused(t *testing.T)        // two different project.created roots → ErrRootMismatch, both root ids in message
func TestPlanRemoteMissingParentRejected(t *testing.T)// remote event whose parent is in neither side → error naming the event
func TestPlanRemoteBadLineRejected(t *testing.T)      // RawEvent{Raw: []byte("{not json")} → parse error
func TestPlanIngestOrderTopological(t *testing.T)     // child before parent in remote slice → ToIngest parents-first
func TestSetDigestOrderIndependent(t *testing.T)      // shuffled ids → same digest
```

- [ ] **Step 2: Run tests, verify failure** — `go test ./internal/eventsync/ -run 'TestPlan|TestSetDigest' -v` → FAIL (package does not exist).

- [ ] **Step 3: Implement.** `target.go` holds the types above verbatim plus `SetDigest` (sort a copy, join with `\n`, sha256). `engine.go`:

```go
func Plan(local []*eventsource.Event, remote *RemoteSnapshot) (*PlanResult, error) {
    res := &PlanResult{RemoteAbsent: remote.Absent, LocalAbsent: len(local) == 0}
    localByID := map[string]*eventsource.Event{}
    for _, e := range local { localByID[e.ID] = e }
    remoteByID := map[string]*eventsource.Event{}
    union := slices.Clone(local)
    for _, re := range remote.Events {
        ev, err := eventsource.Parse(re.Raw) // recomputes id; rejects bad lines
        if err != nil { return nil, fmt.Errorf("eventsync: remote event: %w", err) }
        if remoteByID[ev.ID] == nil {
            remoteByID[ev.ID] = ev
            if localByID[ev.ID] == nil { union = append(union, ev) }
        }
    }
    d, err := eventsource.BuildDAG(union) // validates parents + acyclicity over the union
    if err != nil { return nil, fmt.Errorf("eventsync: staged validation: %w", err) }
    lr, rr := rootOf(local), rootOfMap(remoteByID)
    if lr != "" && rr != "" && lr != rr {
        return nil, fmt.Errorf("%w: local %s, remote %s", ErrRootMismatch, lr, rr)
    }
    for _, e := range d.Events() { // topo order, deterministic
        if localByID[e.ID] == nil { res.ToIngest = append(res.ToIngest, e) }
    }
    for _, e := range local {
        if remoteByID[e.ID] == nil && !remote.Absent || remote.Absent {
            res.ToPublish = append(res.ToPublish, RawEvent{ID: e.ID, Raw: e.Raw})
        }
    }
    return res, nil
}
```

`rootOf` scans for `Action == "project.created"` and returns its id ("" when absent; two distinct roots *within one side* is also `ErrRootMismatch` — a store can't hold two roots for one project).

- [ ] **Step 4: Run tests, verify pass** — same command → PASS.
- [ ] **Step 5: Commit** — `git add internal/eventsync && git commit -m "feat(ATM-0108): eventsync Plan engine, SyncTarget interfaces, set digest"`

---

### Task 2: Directory target

**Files:**
- Create: `internal/eventsync/dirtarget.go`
- Test: `internal/eventsync/dirtarget_test.go`

**Interfaces:**
- Produces: `func NewDirTarget(root string) *DirTarget`; `DirTarget` implements `SyncTarget`. Event file path: `<root>/<CODE>/events.v2.jsonl`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDirFetchAbsent(t *testing.T)                 // empty temp root → Absent
func TestDirFetchReadsEvents(t *testing.T)            // write two canonical lines → Events with recomputed IDs, Digest set
func TestDirPublishAppendsOnly(t *testing.T)          // publish to existing file → old bytes are a prefix of new bytes (no rewrite)
func TestDirPublishCreatesProjectDir(t *testing.T)    // absent → dir + file created
func TestDirConcurrentPublishInterleavesWholeLines(t *testing.T) // two goroutines publish disjoint 50-event batches → file parses, union complete
func TestDirDuplicateLinesHarmless(t *testing.T)      // publish same events twice → Fetch dedups by id (Plan would too)
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/eventsync/ -run TestDir -v`.
- [ ] **Step 3: Implement.** Fetch: `os.ReadFile`, `IsNotExist` → `{Absent: true}`; split on `\n`, skip empty tail, each line `RawEvent{ID: parsed.ID, Raw: line-copy}` (parse to recompute id; a torn tail line without trailing `\n` is skipped as uncommitted, matching L3's commit-point rule); `Digest = SetDigest(ids)`. Publish: `os.MkdirAll`, `os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)`, one `Write` per event of `append(raw, '\n')`, then `f.Sync()`. Never rename.
- [ ] **Step 4: Run, verify PASS.**
- [ ] **Step 5: Commit** — `git commit -m "feat(ATM-0108): directory sync target (mirror store, O_APPEND publish)"`

---

### Task 3: Git target

**Files:**
- Create: `internal/eventsync/gittarget.go`
- Test: `internal/eventsync/gittarget_test.go`

**Interfaces:**
- Produces: `func NewGitTarget(cacheDir, url, subpath string) *GitTarget` (subpath default `.atm` applied by caller/URL parser); implements `SyncTarget`. `GitTarget.workdir` = `<cacheDir>/<sha256(url)[:12]>`.

- [ ] **Step 1: Write the failing tests.** All tests `t.Skip` if `exec.LookPath("git")` fails. Helper `initBareRepo(t) string` runs `git init --bare` + an initial empty commit via a scratch clone (so HEAD/branch exist).

```go
func TestGitFetchAbsent(t *testing.T)            // bare repo without the file → Absent
func TestGitPublishThenFetchRoundTrip(t *testing.T)
func TestGitPublishWritesGitattributes(t *testing.T)  // "<subpath>/<CODE>/events.v2.jsonl merge=union" line committed
func TestGitNonFastForwardRetryUnions(t *testing.T)   // two GitTargets (separate cacheDirs) on one bare remote: A publishes e1, B (stale) publishes e2 → B retries; final file holds e1 AND e2
func TestGitPublishRetryExhaustionErrors(t *testing.T)// stub: make push always fail by making remote a read-only path after clone → error mentions retry
func TestGitMissingBinaryError(t *testing.T)          // PATH="" via t.Setenv → clear "git binary not found" error
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/eventsync/ -run TestGit -v`.
- [ ] **Step 3: Implement.** `run(ctx, dir string, args ...string)` wraps `exec.CommandContext("git", ...)` capturing stderr into errors. Fetch: ensure clone (`git clone <url> <workdir>` once, else `git fetch origin` + `git reset --hard origin/HEAD`), read `<workdir>/<subpath>/<CODE>/events.v2.jsonl` exactly like DirTarget; `State` = head commit hash. Publish loop (attempts 1..3): refresh to `origin/HEAD`; re-read file; skip events already present (by id); append remaining lines; ensure `.gitattributes` in `<subpath>/` contains the `merge=union` entry (write+stage if missing); `git add -A <subpath>`; `git commit -m "chore(atm-sync): <CODE> +N events"`; `git push origin HEAD`; on push failure, continue loop; after 3 failures return retryable error. If after refresh nothing is missing, return nil (someone else already published them).
- [ ] **Step 4: Run, verify PASS.**
- [ ] **Step 5: Commit** — `git commit -m "feat(ATM-0108): git sync target (cached clone, bounded NFF retry, merge=union)"`

---

### Task 4: Transport selection from URL

**Files:**
- Modify: `internal/eventsync/target.go`
- Test: `internal/eventsync/target_test.go`

**Interfaces:**
- Produces: `func SelectTarget(storeRemotesDir, raw string) (SyncTarget, error)` and `func splitGitURL(raw string) (url, subpath string, ok bool)`.

- [ ] **Step 1: Write the failing table test**

```go
func TestSelectTarget(t *testing.T) {
    dir := t.TempDir() // exists → DirTarget
    cases := []struct{ in, kind, url, sub string; err bool }{
        {in: dir, kind: "dir"},
        {in: "git@github.com:u/r.git", kind: "git", sub: ".atm"},
        {in: "git@github.com:u/r.git//store", kind: "git", sub: "store"},
        {in: "https://host/u/r.git", kind: "git", sub: ".atm"},
        {in: "ssh://host/r", kind: "git", sub: ".atm"},
        {in: "git::/tmp/bare.git//x", kind: "git", sub: "x"},
        {in: filepath.Join(dir, "missing"), err: true},   // neither an existing dir nor a git URL
    }
    ...
}
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/eventsync/ -run TestSelectTarget -v`.
- [ ] **Step 3: Implement.** Order: `git::` prefix → git (strip, then split `//<subpath>` from the end, but only after a `.git` or for `git::` always at last `//`); `git@`/`ssh://` prefixes or `.git` suffix or `.git//` infix → git (`splitGitURL` cuts at `.git//`); existing directory → `NewDirTarget`; else error `"not an existing directory and not a recognizable git URL (use git:: to force)"`. Subpath defaults to `.atm` when absent.
- [ ] **Step 4: Run, verify PASS.**
- [ ] **Step 5: Commit** — `git commit -m "feat(ATM-0108): transport selection from remote URL"`

---

### Task 5: Remote config in ProjectConfig

**Files:**
- Modify: `internal/store/config.go`
- Test: `internal/store/config_test.go` (extend)

**Interfaces:**
- Produces: `ProjectConfig.Remotes map[string]string`; `(s *Store) SetProjectRemote(code, name, url, actor string) error`, `RemoveProjectRemote(code, name, actor string) error`, `ProjectRemotes(code string) (map[string]string, error)`.

- [ ] **Step 1: Write failing tests** — set/get/remove round-trip; remove of unknown name → `ErrNoMatch`; empty name/url → `ErrUsage`; config with only remotes (no embedding) still loads (guards the `GetProjectConfig` nil-return condition).
- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/store/ -run TestProjectRemote -v`.
- [ ] **Step 3: Implement.** Add `Remotes map[string]string \`json:"remotes,omitempty"\`` to `ProjectConfig`; extend the nil-check in `GetProjectConfig` to `c.Embedding == nil && c.UpdatedAt == "" && len(c.Remotes) == 0`. Accessors follow the `SetEmbeddingConfig` shape exactly: `validateActor`, `WithLock`, read-merge-`WriteFileAtomic`.
- [ ] **Step 4: Run, verify PASS.**
- [ ] **Step 5: Commit** — `git commit -m "feat(ATM-0108): per-project sync remotes in project config"`

---

### Task 6: Store ingest, snapshot, and bootstrap (LocalStore implementation)

**Files:**
- Create: `internal/store/eventsource_sync.go`
- Test: `internal/store/eventsource_sync_test.go`

**Interfaces:**
- Consumes: `readV2File`, `appendV2EventLineLocked`, `reprojectV2Locked`, `mutateStoreMeta`, `dispatchFormat`, `setProjectFormat`, `WithLock`, `eventsource.FoldEvents`, `eventsource.Clock`.
- Produces (must match `eventsync.LocalStore` in Task 7 *exactly* — structural typing is the decoupling):

```go
func (s *Store) SyncSnapshot(code string) (events []*eventsource.Event, absent bool, err error)
func (s *Store) SyncIngest(code string, incoming []*eventsource.Event) (ingested, newlyContested int, err error)
func (s *Store) SyncBootstrap(code string, incoming []*eventsource.Event) error
var ErrSyncNeedsV2 = errors.New(`project is v1-active and cannot sync; run "atm store upgrade" first`)
```

- [ ] **Step 1: Write failing tests** (use the temp-store pattern from `internal/store/eventsource_e2e_test.go`; create a v2-born project, author tasks via the public mutators):

```go
func TestSyncSnapshotV1Refused(t *testing.T)          // v1 project → ErrSyncNeedsV2
func TestSyncSnapshotAbsentProject(t *testing.T)      // unknown code → absent=true, no error
func TestSyncIngestAppendsTopoAndReprojects(t *testing.T) // ingest events authored in a second store → ListTasks sees them; file gained exactly len(incoming) lines
func TestSyncIngestIdempotent(t *testing.T)           // ingest same events twice → second returns ingested=0
func TestSyncIngestObservesHLC(t *testing.T)          // ingest event with HLC far ahead → next locally-authored event's HLC.p >= ingested p
func TestSyncIngestReportsNewlyContested(t *testing.T)// concurrent title edits from two replicas → newlyContested == 1
func TestSyncBootstrapCreatesProject(t *testing.T)    // bootstrap into empty store → project listed, format entry "v2", fold matches source
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/store/ -run TestSync -v`.
- [ ] **Step 3: Implement.**

```go
func (s *Store) SyncSnapshot(code string) ([]*eventsource.Event, bool, error) {
    if !s.ProjectExistsAnyMedia(code) { return nil, true, nil } // reuse the L3 media-presence check
    f, err := s.dispatchFormat(code)
    if err != nil { return nil, false, err }
    if f != FormatV2 { return nil, false, ErrSyncNeedsV2 }
    snap, err := s.readV2File(code, false)
    if err != nil { return nil, false, err }
    return snap.Events, false, nil
}

func (s *Store) SyncIngest(code string, incoming []*eventsource.Event) (int, int, error) {
    var ingested, newly int
    err := s.WithLock(code, func() error {
        snap, err := s.readV2File(code, false)
        if err != nil { return err }
        before, err := eventsource.FoldEvents(snap.Events)
        if err != nil { return err }
        have := map[string]bool{}
        for _, e := range snap.Events { have[e.ID] = true }
        var maxHLC eventsource.HLC
        all := snap.Events
        for _, e := range incoming { // already topo-ordered by Plan; recompute under lock (I-3)
            if have[e.ID] { continue }
            if err := s.appendV2EventLineLocked(code, e.Raw); err != nil { return err }
            have[e.ID] = true; ingested++; all = append(all, e)
            if e.HLC.Compare(maxHLC) > 0 { maxHLC = e.HLC }
        }
        if ingested == 0 { return nil }
        if err := s.mutateStoreMeta(func(m *StoreMeta) error { // Clock.Observe semantics, persisted
            if m.LastHLC == nil || maxHLC.Compare(*m.LastHLC) > 0 { h := maxHLC; h.L++; m.LastHLC = &h }
            return nil
        }); err != nil { return err }
        after, err := eventsource.FoldEvents(all)
        if err != nil { return err }
        newly = contestedDelta(before.Contested, after.Contested)
        return s.reprojectV2Locked(code)
    })
    return ingested, newly, err
}
```

`contestedDelta` keys `ContestedSlot` by `Entity+"\x00"+Kind+"\x00"+Field` and counts keys in `after` missing from `before`. `SyncBootstrap`: refuse if project exists in any media; `MkdirAll` the project dir; `WithLock`: append all events topo-order, `setProjectFormat(code, FormatV2)` BEFORE the first read path can run, then `reprojectV2Locked`. (If the exact existing helper names differ — e.g. the media-presence check — locate them in `internal/store/eventsource_meta.go`/`store.go` and use the existing ones rather than inventing parallels.)

- [ ] **Step 4: Run, verify PASS.**
- [ ] **Step 5: Commit** — `git commit -m "feat(ATM-0108): store-side sync snapshot, locked ingest, bootstrap"`

---

### Task 7: Sync orchestrator

**Files:**
- Modify: `internal/eventsync/engine.go`
- Test: `internal/eventsync/sync_test.go`

**Interfaces:**
- Consumes: `Plan`, `SyncTarget`, Task 6's method set.
- Produces:

```go
type LocalStore interface {
    SyncSnapshot(project string) ([]*eventsource.Event, bool, error)
    SyncIngest(project string, incoming []*eventsource.Event) (ingested, newlyContested int, err error)
    SyncBootstrap(project string, incoming []*eventsource.Event) error
}

type Options struct { Pull, Push, DryRun bool } // both false ⇒ both true (bidirectional default)

type Report struct {
    Project        string
    Pulled, Pushed int
    Bootstrapped   bool
    NewlyContested int
    RemoteAbsent   bool
    DryRun         bool
    PushErr        error // pull committed, push failed — legal state (L4 failure model)
}

func Sync(ctx context.Context, local LocalStore, target SyncTarget, project string, opt Options) (*Report, error)
```

- [ ] **Step 1: Write failing tests** with an in-memory `fakeStore` and `fakeTarget` (both ~20 lines, backed by maps):

```go
func TestSyncBidirectional(t *testing.T)       // each side missing one event → Pulled=1, Pushed=1, both sides converged
func TestSyncPullOnlySkipsPublish(t *testing.T)
func TestSyncPushOnlySkipsIngest(t *testing.T)
func TestSyncDryRunTouchesNothing(t *testing.T) // counts reported, fake mutation hooks never called
func TestSyncBootstrapsAbsentLocal(t *testing.T)
func TestSyncLocalAbsentRemoteAbsentIsError(t *testing.T) // nothing to sync on either side
func TestSyncPushFailureReportedNotFatal(t *testing.T)    // target.Publish errors → Report.PushErr set, no error returned, Pulled intact
func TestSyncDigestShortCircuit(t *testing.T)  // target with Narrowing: equal Frontier digest → Fetch never called
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/eventsync/ -run TestSync -v`.
- [ ] **Step 3: Implement.** Normalize `Options` (neither ⇒ both). If target implements `Narrowing` and `opt.Pull && opt.Push`: compare `Frontier` digest against `SetDigest(local ids)` — equal ⇒ zero report. Else `Fetch`, `SyncSnapshot`, `Plan`. DryRun ⇒ fill counts from `PlanResult`, return. LocalAbsent && remote has events && Pull ⇒ `SyncBootstrap` (Report.Bootstrapped). Else Pull ⇒ `SyncIngest(plan.ToIngest)`. Push && len(ToPublish)>0 ⇒ `Publish`; error lands in `Report.PushErr` (fatal only if pull did nothing and pull was disabled — then return it).
- [ ] **Step 4: Run, verify PASS.**
- [ ] **Step 5: Commit** — `git commit -m "feat(ATM-0108): Sync orchestrator over LocalStore + SyncTarget"`

---

### Task 8: CLI — atm store remote add/list/remove

**Files:**
- Modify: `internal/cli/store.go` (follow the existing subcommand pattern, e.g. `verifyCmd` at `internal/cli/store.go:161`)
- Test: `internal/cli/store_test.go` (extend, following its existing command-invocation pattern)

**Interfaces:**
- Consumes: Task 5's `SetProjectRemote`/`RemoveProjectRemote`/`ProjectRemotes`.
- Produces: `atm store remote add <name> <url> --project <CODE>`, `atm store remote list [--project <CODE>]`, `atm store remote remove <name> --project <CODE>`; list output `NAME\tURL` per line (all projects when no `--project`, prefixed `CODE\t`).

- [ ] **Step 1: Write failing CLI tests** — add/list/remove round-trip; add without `--project` → usage error; remove unknown → `ErrNoMatch` surfaced; `--json` on list emits `{"project":..,"name":..,"url":..}` rows.
- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/cli/ -run TestStoreRemote -v`.
- [ ] **Step 3: Implement** the `remoteCmd` cobra tree inside `newStoreCmd`, `cmd.AddCommand(remoteCmd)` beside the existing `AddCommand` calls.
- [ ] **Step 4: Run, verify PASS.**
- [ ] **Step 5: Commit** — `git commit -m "feat(ATM-0108): atm store remote add/list/remove"`

---

### Task 9: CLI — atm store sync

**Files:**
- Modify: `internal/cli/store.go`
- Test: `internal/cli/store_test.go` (extend)

**Interfaces:**
- Consumes: `eventsync.Sync`, `eventsync.SelectTarget`, `ProjectRemotes`, `SetProjectRemote` (bootstrap persists `origin`, I-5).
- Produces: `atm store sync [<name-or-url>] [--project <CODE>] [--pull|--push] [--dry-run] [--json]`.

- [ ] **Step 1: Write failing CLI tests** (dir remotes in temp dirs — no git needed):

```go
func TestStoreSyncPullPushAgainstDirRemote(t *testing.T) // two temp stores, shared dir remote: A push, B sync → B has A's task
func TestStoreSyncDefaultRemoteOrigin(t *testing.T)
func TestStoreSyncAdHocURLNotPersisted(t *testing.T)
func TestStoreSyncDryRunReportsNoChange(t *testing.T)
func TestStoreSyncAllProjectsWithRemotes(t *testing.T)   // no --project: two projects, one remote each; one remote broken → other still syncs, exit nonzero, both reported
func TestStoreSyncV1ProjectRefusedWithUpgradeHint(t *testing.T)
func TestStoreSyncBootstrapPersistsOrigin(t *testing.T)  // sync <url> --project NEW into empty store → project exists, remote "origin" saved
func TestStoreSyncReportsContested(t *testing.T)         // concurrent title edit both stores → output mentions contested
```

- [ ] **Step 2: Run, verify FAIL** — `go test ./internal/cli/ -run TestStoreSync -v`.
- [ ] **Step 3: Implement.** Resolve remote arg: name found in project's `Remotes` → its URL; else treat as ad-hoc URL. No `--project` ⇒ iterate every project with ≥1 remote (each independently; collect per-project errors, print all reports, exit nonzero if any failed). Per project: `SelectTarget($ATM_HOME/remotes, url)` → `eventsync.Sync(ctx, st.store, target, code, opts)`. Text report per project: `CODE: pulled N, pushed M` plus `(bootstrapped)`, `push failed: <err>`, and `N newly contested slot(s) — review the contested board` when nonzero. `--json` emits the `Report` struct.
- [ ] **Step 4: Run, verify PASS.**
- [ ] **Step 5: Commit** — `git commit -m "feat(ATM-0108): atm store sync command"`

---

### Task 10: Convergence integration tests (the D4 promise, end to end)

**Files:**
- Create: `internal/store/eventsource_sync_e2e_test.go`

**Interfaces:**
- Consumes: everything above; no new API.

- [ ] **Step 1: Write the tests** (these are the deliverable — they should pass immediately if Tasks 1–9 are correct; any failure is a real bug):

```go
func TestTwoStoreConvergenceByteIdenticalFold(t *testing.T)
// store A creates project+tasks, dir-remote R; A sync; B bootstraps from R;
// A and B each author divergent edits (title edit vs comment add) with distinct replica ids;
// A sync; B sync; A sync (three passes settle both directions);
// assert: identical event-id SETS in both files, and reflect.DeepEqual on both FoldEvents States
// (files may differ in line ORDER by append history — the set and the fold are the contract).

func TestThreeReplicaRandomizedConvergence(t *testing.T)
// three stores, one shared dir remote; 30 iterations of seeded rand: pick a store,
// author a mutation (create/edit/label/comment) or sync; finish with two full sync rounds
// for every store; assert all three folds deep-equal and id sets equal. Seed logged for replay.

func TestConflictCopyRecoveryByUnion(t *testing.T)
// simulate a file-sync fork: copy remote file to events.v2.jsonl.conflict, let both fork copies
// gain disjoint appends, then cat conflict file's lines onto the main one (the documented runbook);
// sync → everything converges, no duplicates in fold.
```

- [ ] **Step 2: Run** — `go test ./internal/store/ -run 'TestTwoStore|TestThreeReplica|TestConflictCopy' -v` → PASS (fix any exposed bug where it lives, not in the test).
- [ ] **Step 3: Run the full gates** — `gofmt -l .` (empty), `make verify` → PASS.
- [ ] **Step 4: Commit** — `git commit -m "test(ATM-0108): multi-replica convergence and conflict-copy recovery e2e"`

---

### Task 11: README runbook + ledger close-out

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Write the "Syncing between machines" README section:** remote setup (`atm store remote add origin ~/Sync/atm`), daily loop (`atm store sync`), second-machine bootstrap (`atm store sync <url> --project CODE`), git remotes incl. `//subpath` and ambient credentials, v1-must-upgrade note, conflict-copy recovery runbook (concatenate copies, run sync — union is lossless), and "push failed? just run sync again".
- [ ] **Step 2: Verify docs claims against behavior** — run each documented command once against a scratch store (`ATM_HOME=$(mktemp -d)`).
- [ ] **Step 3: Commit** — `git commit -m "docs(ATM-0108): README sync runbook"`
- [ ] **Step 4: Record completion on the ledger** — `atm task comment add --task ATM-0108 --label ATM:comment:progress --body "L4 implementation complete: internal/eventsync (Plan/Sync/dir/git targets), store ingest+bootstrap, atm store remote/sync CLI, convergence e2e green, README runbook. make verify green at <commit>."` with the session actor, then ask the atm-manager to review status labels.

---

## Self-review notes (already applied)

- Spec coverage: L4-1..11 and I-1..7 each map to a task (L4-6 Narrowing: interface in Task 1, digest short-circuit in Task 7; no v1 target implements it — per spec).
- Type consistency: `SyncSnapshot/SyncIngest/SyncBootstrap` signatures are identical in Task 6 (methods) and Task 7 (interface) — structural satisfaction is the import-cycle escape; do not rename one side.
- The exact names of L3-internal helpers (`ProjectExistsAnyMedia`, media checks) may differ — Task 6 says to reuse the existing ones from `eventsource_meta.go`/`store.go`, never to invent parallels.
