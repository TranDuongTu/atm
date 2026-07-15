# EventSource L4 Sync & Transport — Design Spec

**Status:** Proposed
**Tracking:** ATM-0108
**Layer spec:** `docs/eventsource/03-sync-transport.md` (decisions L4-1..L4-11 are binding)
**Depends on:** ATM-0106 (`internal/eventsource` core, landed), ATM-0107 (L3 v2 storage, landed)
**Consumed by:** ATM-0123 (GitHub Issues adapter implements the `SyncTarget` Level 0 interface defined here)

## Driver

L0–L3 are live: v2 events are content-addressed, causal, foldable, and stored in one canonical `events.v2.jsonl` per project. Nothing moves them between machines yet. This spec defines the implementation of the L4 layer spec: the sync engine, the `SyncTarget` interface, the directory and git transports, and the `atm store remote`/`atm store sync` CLI.

## Goals

- Implement set-reconciliation sync (L4-1): fetch remote set, validate, ingest missing locally, publish missing remotely — per project.
- Ship the two v1 transports: directory (mirror store) and git (cached clone, system git).
- Define `SyncTarget` (Level 0, mandatory) and `Narrowing` (Level 1, optional) so ATM-0123 and future HTTP/SSH targets plug in without engine changes.
- CLI under `atm store`: `remote add/list/remove`, `sync` with `--project`, `--pull/--push`, `--dry-run`; project-less sync walks all projects with remotes.
- Bootstrap a second machine by syncing into an absent local project (persist URL as `origin`).
- Enforce the same-project root guard and the atomic staged-validation union.
- Two/three-replica convergence tests asserting byte-identical folds.

## Non-goals

- Cross-project merge (refused by the root guard; separate future task).
- Interactive have/want session mechanics, HTTP/SSH transports (interface slot only).
- Auth/signing (L5), version negotiation (X), daemons/auto-sync, event GC.
- GitHub Issues projection (ATM-0123 owns it; it consumes this interface).

## Architecture

New package `internal/eventsync` (name avoids stdlib `sync` clash) owns the protocol; `internal/store` gains ingest/bootstrap entry points; `internal/cli/store.go` gains the subcommands.

```text
internal/eventsync/
  target.go      SyncTarget, Narrowing, RemoteSnapshot, transport selection from URL
  engine.go      Plan (fetch+validate+diff) and the sync orchestration
  dirtarget.go   directory transport
  gittarget.go   git transport (cached clone under $ATM_HOME/remotes/)
internal/store/
  eventsource_sync.go   IngestV2Events (locked append+HLC+cache), BootstrapV2Project, LocalV2Snapshot
  config.go             ProjectConfig gains Remotes map[string]string
internal/cli/
  store.go              remote add/list/remove, sync subcommands
```

### Interfaces (Level 0 / Level 1)

```go
type RawEvent struct {
    ID   string // recomputed from Raw, never trusted from the wire
    Raw  []byte // canonical line bytes, preserved verbatim (D5/L0-2)
}

type RemoteSnapshot struct {
    Absent bool       // remote does not hold the project
    Events []RawEvent
    Digest string     // set digest: sha256 over sorted event ids
    State  any        // transport-private (e.g. git head the snapshot came from)
}

type SyncTarget interface {
    Fetch(ctx context.Context, project string) (*RemoteSnapshot, error)
    Publish(ctx context.Context, project string, missing []RawEvent, base *RemoteSnapshot) error
}

type Narrowing interface { // optional; no v1 target implements it
    Frontier(ctx context.Context, project string) (digest string, heads []string, err error)
    FetchSince(ctx context.Context, project string, haves []string) ([]RawEvent, error)
}
```

The engine type-asserts `Narrowing` and, when present, short-circuits a no-op sync on equal digests before fetching. Level 0 targets get the same short-circuit from `RemoteSnapshot.Digest` after fetch.

### The engine

`Plan(local, remote)` is pure: given the local `V2FileSnapshot` and the fetched `RemoteSnapshot` it returns `{toIngest, toPublish, rootMismatch, report}`. Validation before anything commits (L4-4):

1. Every remote line parses via `eventsource.Parse` (which recomputes the id from canonical bytes — a hash mismatch is a parse-level integrity error naming the remote and event).
2. `eventsource.BuildDAG` over the **union** validates parent resolution and acyclicity.
3. Root guard (L4-3): the root is the unique `project.created` event; both sides' root ids must match. Remote absent ⇒ full publish; local project absent ⇒ bootstrap.
4. Diff is exact set difference on event ids.

`toIngest` is ordered topologically with `CompareEvents` as the tiebreak, so the local file stays parent-before-child and the append order is deterministic (L4-5). Readers already tolerate arbitrary order (`BuildDAG` is order-blind) — a required property since git `merge=union` can interleave.

### Store integration

- **Sync requires a v2-active project.** A v1 project cannot sync (D6); refuse with a pointer to `atm store upgrade`.
- `IngestV2Events(code, events)` runs under the project lock: re-read the local file (the diff may be stale by the time the lock is held — recompute missing against current), append raw bytes verbatim via `appendV2EventLineLocked`, fsync, `Clock.Observe` the max ingested HLC and persist it via `mutateStoreMeta`, then rebuild the project's cache rows through the existing projector. Creation ordinals restamp; vector staleness stays text-hash-based (L3-15) so no re-embedding storm.
- `BootstrapV2Project(code, events)`: create the project directory, write all events topologically, write the explicit `project_formats[code]="v2"` entry **before** first read, project the fold into cache, persist the source URL as remote `origin`.
- **Remotes** live in `ProjectConfig.Remotes` (`projects/<CODE>/config.json`) — replica-local, never synced as content (L4-8).

### Directory target

URL: an existing directory path. Layout: `<root>/<CODE>/events.v2.jsonl` (a real store is a valid remote via its `projects/` dir). Fetch = read file (absent ⇒ `Absent`). Publish = open `O_APPEND`, write missing lines, fsync — never rewrite-and-rename, so concurrent publishers interleave whole lines; duplicate lines are harmless (dedup by id). Conflict copies forked by file-sync services recover by union — documented in the README runbook.

### Git target

URL: anything git recognizes, optional `//<subpath>` suffix (default `.atm`); event file at `<subpath>/<CODE>/events.v2.jsonl` in the repo. Cached clone per remote under `$ATM_HOME/remotes/<hash-of-url>/`, all operations shell out to system `git` (required on PATH only for git remotes; missing git is a clear error). Fetch = `git fetch` + read at remote head. Publish = append lines to the working copy, commit (`chore(atm-sync): <CODE> +N events`), push; non-fast-forward ⇒ fetch, recompute missing against the new head, retry (3 attempts, then a retryable error — never corruption). First publish also commits a `.gitattributes` marking the event file `merge=union`. Credentials are ambient git auth (L4-11).

### Transport selection

Explicit rules, ambiguity is an error: a URL git recognizes (`git@`, `ssh://`, `http(s)://…(.git)`, or any URL carrying a `//<subpath>` suffix) ⇒ git; an existing local directory ⇒ directory; a local path that is also a git repo URL form is disambiguated by requiring `git::` prefix or a trailing `.git` for the git reading.

### CLI surface

```sh
atm store remote add <name> <url> --project <CODE>
atm store remote list [--project <CODE>]
atm store remote remove <name> --project <CODE>
atm store sync [<name-or-url>] [--project <CODE>] [--pull|--push] [--dry-run] [--json]
```

- No `--project` on `sync` ⇒ every project with ≥1 configured remote, independently; per-project failures are reported and do not abort the loop.
- No remote argument ⇒ `origin`. Raw URL/path accepted ad-hoc, nothing persisted.
- `--dry-run` runs fetch + Plan only and reports would-be transfers/root mismatch.
- Bootstrap: `sync <url> --project <CODE>` with no local project pulls, validates, creates, persists `origin` (L4-9).
- Exit report: pulled/pushed counts per project; if ingest produced newly-contested slots (diff the contested set before/after fold), say so and point at the contested board (L4-10).

### Failure model (implementation commitments)

- Fetch/validation failure ⇒ abort, both sides untouched; integrity errors name remote + offending event id.
- Ingest committed, publish failed ⇒ report "pulled N, push failed: cause"; exit nonzero only if nothing succeeded; next sync completes the push.
- Every failure's recovery is re-running `atm store sync` — no journal, no resume tokens.

## Testing

- **Unit (`internal/eventsync`):** Plan diff/validation per rejection class (bad hash, missing parent, cycle, root mismatch); topological append determinism; transport selection table; digest equality no-op.
- **Convergence integration (`internal/store`):** two stores diverge → sync through a shared dir remote → byte-identical fold state; three-replica property test with randomized sync order/pairings asserting convergence (the D4 promise end-to-end).
- **Git target:** local bare repo; forced non-fast-forward with two publishers proving retry unions rather than clobbers; subpath handling; missing-git error.
- **Directory target:** concurrent publish interleaving; conflict-copy union recovery.
- **Bootstrap:** second-machine clone path, `origin` persistence, distinct replica ids, root guard across two *different* projects both named the same.
- **CLI:** remote CRUD round-trip; sync dry-run output; v1-project refusal; walk-all-projects loop isolation.

## Documentation deliverables

README section "Syncing between machines": remote setup, daily `atm store sync`, git remote with GitHub, conflict-copy recovery runbook, v1-must-upgrade note.

## Decisions (implementation-level, on top of layer spec L4-1..11)

| # | Decision |
|---|---|
| I-1 | Package `internal/eventsync`; the engine's `Plan` is a pure function, side effects live in store ingest + target publish. |
| I-2 | `RemoteSnapshot.Digest` = sha256 over the sorted event-id set; used for no-op short-circuit at both levels. |
| I-3 | Ingest recomputes the missing-set under the project lock (fetch-time diff may be stale); append is verbatim raw bytes. |
| I-4 | Sync refuses v1-active projects with an upgrade pointer. |
| I-5 | Remotes in `ProjectConfig.Remotes` map; `origin` is the default remote name and the bootstrap-persisted one. |
| I-6 | Git publish retry: 3 bounded non-fast-forward attempts; commit message `chore(atm-sync): <CODE> +N events`. |
| I-7 | Cached clones under `$ATM_HOME/remotes/<hash>`; system git binary, no go-git dependency. |
