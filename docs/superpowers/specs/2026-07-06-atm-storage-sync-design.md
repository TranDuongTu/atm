# Storage Cache Consolidation + Cross-Machine Sync — Design Spec

**Status:** Proposed
**Tracking:** ATM-0027 (originally scoped as "migrate storage backend from JSON
event source to a real database"; this spec supersedes that framing with the
one below).
**Approach:** Additive on top of the approved audit-log redesign
(`2026-07-04-audit-log-redesign-design.md`). No change to that spec's log
format or replay semantics.

## Driver

ATM-0027 started from a real symptom: noticeable TUI lag, suspected to be
caused by the no-DB, many-small-JSON-files substrate. Investigation
(ATM-0027-c0002/c0003) found the actual cause is narrower than "JSON files are
too slow": `labels.refresh` calls `LabelUsage` once per label, each scan
touching every task, and `GetTask` parses the project log twice per call while
validating cache freshness. That's an indexing/caching bug, not proof the
architecture needs a database.

Separately, a new requirement emerged: **sync a project's state across
machines** (single user, multiple machines, not usually concurrent) via a
built-in `atm store push`/`pull`, with a filesystem transport in v1 and room to
add an HTTP transport later, "taking advantage of event sourcing, not just
copying DB files."

These two threads turn out to pull in the same direction once examined
together:

- A single mutable DB file is **harder** to sync than the current per-project
  append-only `log.jsonl`, not easier — a binary DB file can only be
  whole-file-replaced by a naive sync, silently losing one side's edits on any
  divergence. An append-only log merges naturally: unseen entries from the
  other side get appended and replayed.
- The existing audit-log redesign already establishes `log.jsonl` as the
  **sole source of truth** and treats every JSON cache file as a derived,
  rebuildable view. That is exactly the shape sync needs: sync operates only
  on the source of truth (the log); anything derived is disposable and gets
  regenerated locally after a merge.
- Separately, the project owner wants to consolidate today's scattered cache
  files (`projects/<CODE>.json`, `projects/<CODE>/tasks/<ID>.json`, global
  `labels.json`) into a single local SQLite file, both because there are
  "too many cache files" today and because indexed queries are the real fix
  for the `labels.refresh` bug. This is compatible with sync as long as the
  DB stays purely local and derived — never the sync payload.

## Decisions (locked)

1. **No storage-backend migration for the source of truth.** `log.jsonl` per
   project remains the append-only source of truth, unchanged from the audit-
   log redesign. This spec does not touch `AppendLog`, `ReadLog`, `Replay`, or
   the closed action enum.
2. **Cache files consolidate into one local SQLite database**,
   `$ATM_HOME/cache.db`, replacing `projects/<CODE>.json`,
   `projects/<CODE>/tasks/<ID>.json`, and the global `labels.json`. Tables:
   `projects`, `tasks`, `labels`, `task_labels` (join).
3. **`cache.db` is purely derived and disposable** — same status as today's
   cache files. Rebuilt from the union of all project logs via the existing
   replay/lazy-miss/`rebuild` machinery. Never read or written by sync. Safe
   to delete; `atm store rebuild` regenerates it.
4. **Driver: `modernc.org/sqlite`** (pure Go, no cgo) — keeps the `atm` binary
   a single static artifact, avoids cgo cross-compile friction. WAL mode
   enabled.
5. **Sync is built on `log.jsonl`, via a `SyncTarget` interface.** v1 ships a
   filesystem implementation; the interface is designed so an HTTP
   implementation can be added later with no change to merge logic or CLI
   surface.
6. **Merge model: full-log union merge, no cursor state.** Push/pull always
   merge the complete local and target logs for a project (dedupe by natural
   key, sort by `at`, re-sequence, rebuild `cache.db` for the written side).
   No "last synced seq" bookkeeping to keep in sync with reality.
7. **Task-ID collisions are detected and refused, not auto-resolved.** If both
   sides independently created a different task under the same ID while
   unsynced, the merge for that project aborts with a report; resolution is
   manual (recreate one of the two tasks under a fresh ID).
8. **Sync scope is single-project per invocation.** `--project <CODE>` is
   required; no whole-store multi-project sync in v1, consistent with the
   existing no-cross-project-atomicity invariant.

## Architecture & data flow

```
$ATM_HOME/
  cache.db                                  # THE local materialized cache (derived, disposable)
  projects/
    <CODE>/
      log.jsonl                             # THE source of truth for this project (unchanged)
    <CODE>.lock                             # per-project file lock (unchanged)
```

`projects/<CODE>.json`, `projects/<CODE>/tasks/<ID>.json`, and the top-level
`labels.json` are removed; their content lives in `cache.db` instead.

### Cache write flow (replaces today's per-file cache write)

Unchanged commit point from the audit-log redesign: `AppendLog` to
`log.jsonl` under the project's lock is still the durable write. What changes
is step 3 of that flow:

1. Compose and append the `LogEntry` (unchanged).
2. **Write-through into `cache.db`** instead of a per-entity JSON file: upsert
   the affected row(s) (`projects`, `tasks`, `labels`, `task_labels`), stamping
   the project's `last_applied_seq` for staleness checks.
3. No separate `labels.json` refresh step — labels live in `cache.db` and are
   queried with normal indexed SQL (this removes the audit-log spec's special
   case where the global label registry "is not lazily self-healing").

### Cache read flow (lazy miss, per project)

`cache.db` stores one `last_applied_seq` per project. A read compares that
against `LastLogSeq(code)`; if stale (or the project has no rows yet), replay
that project's log and upsert the resulting rows, same self-healing contract
as today's per-entity lazy miss — just against database rows instead of
per-entity files.

`atm store verify` / `atm store rebuild` extend naturally: `Rebuild()` drops
and replays every project into `cache.db` in one pass instead of rewriting a
tree of JSON files.

### Sync flow

```go
// SyncTarget abstracts where a project's log lives outside the local store.
// v1: filesystem implementation. Future: HTTP implementation, same contract.
type SyncTarget interface {
    FetchLog(code string) ([]LogEntry, error)      // empty, nil if project absent at target
    WriteLog(code string, entries []LogEntry) error // atomically replace the target's log
}
```

**Filesystem `SyncTarget` (v1):** target is a directory shaped like
`<path>/<CODE>/log.jsonl` — the same per-project layout as `$ATM_HOME/projects/`,
so a target directory is itself a valid mini-store (`--store <path>` can open
it directly for inspection or as an independent local store).

**Merge algorithm** (shared by `push` and `pull`):

1. Read the local log and the target's log for `<CODE>` in full (target log is
   empty if the project doesn't exist there yet).
2. **Task-ID collision check** (see below). If any collision is found, abort:
   write nothing, report the colliding ID(s) and both conflicting
   `task.created` events.
3. Union the two entry sets, deduped by natural key `(at, actor, action,
   subject)` — `seq` is not part of identity; it is reassigned in step 4. This
   reuses the natural key the `LogEntry` type already documents (`seq` as "the
   per-project monotonic tiebreaker", not identity).
4. Sort the deduped union by `at`, assign `seq = 1..N` in that order.
5. Write the merged, re-sequenced log to the destination — `pull`: local;
   `push`: target.
6. Rebuild `cache.db` for that project on the destination side from the
   merged log (full replay; cheap at personal-task-tracker scale, avoids
   incremental-patch ordering bugs).

`push` and `pull` are the same merge operation, differing only in which side
receives the merged log. There is no separate bidirectional `sync` command in
v1 — running `push` then `pull` (or vice versa) converges both sides.

**Why this is safe, not just simple:** the audit-log redesign already defines
"last write wins per subject" as how the store reconciles any two mutations on
the same task/label/project during replay. Merging two divergent logs by
timestamp and replaying is that exact same rule, applied at sync time — no new
conflict-resolution concept is introduced.

**Documented limitation:** this is wall-clock (`at`) ordering, so clock skew
between machines could make an actually-later edit lose to an actually-earlier
one after merge. Acceptable for the stated single-user/multi-machine,
rarely-concurrent use case. Not solved with vector clocks or similar — out of
scope for v1.

### Task-ID collision detection

Only task IDs can collide (label identity is the label's name, which is
user-chosen content, not a counter — two independent upserts of the same name
are the same label, not a collision).

Detection: group all `task.created` entries (from both logs) by `Subject.ID`.
An ID collides if it has more than one `created` event with **differing**
`at`/`actor`/payload (same `at`+`actor`+payload is the same event seen on both
sides — normal dedup, not a collision).

Resolution in v1 is **manual, not automatic**: the merge aborts for that
project, the CLI reports each colliding ID with both conflicting creation
events (actor, `at`, title, source side), and the user resolves by recreating
one of the two tasks under a fresh ID (accepting that task restarts its own
history). Silent auto-renumbering is explicitly rejected for v1: a task ID may
already be referenced elsewhere (a commit message, another task's
`ATM:related:` label, a conversation) with no way to detect or fix those
external references if the ID moves without the user's awareness.

## CLI surface

```
atm store push --target <path> --project <CODE>
atm store pull --target <path> --project <CODE>
```

- `--target <path>`: v1 filesystem path only. Designed so a future
  `--target http://...` (or equivalent) reuses the same `SyncTarget` interface
  and merge algorithm with no change to this command surface.
- Both commands print a merge summary (entries contributed by each side) on
  success, or a collision report (colliding ID(s) + both conflicting creation
  events) with non-zero exit and nothing written, on abort.
- Idempotent: re-running with nothing new on either side is a no-op merge
  with the same result.
- `pull` against a target with no project directory: error, "nothing to
  pull." `push` against a target with no project directory: creates it
  (first-time publish).
- Single project per invocation (`--project` required); no whole-store sync
  in v1 — consistent with the existing no-cross-project-atomicity invariant,
  and each project's log is already independent.

`atm conventions` gains a mention of `atm store push`/`pull` as the
multi-machine workflow, alongside the existing `atm store log`/`verify`/
`rebuild` first-contact surfaces.

## Testing, verification & rollout

**cache.db tests** (replacing today's per-file cache tests):
- Rebuild-from-log golden tests per entity kind (mirrors existing
  `rebuild_test.go` pattern), now asserting DB row contents instead of JSON
  file contents.
- Lazy-miss staleness check per project (`last_applied_seq` comparison).
- `labels.refresh`-equivalent query returns usage counts via one indexed
  query — regression test asserting no per-label full-table scan.
- `atm store verify`/`rebuild` extended to cover `cache.db` staleness/
  corruption in place of per-file `CacheCheck`.

**Sync tests:**
- Clean fast-forward (one side strictly ahead) — merge produces the
  superset, correctly re-sequenced.
- True interleave (both sides have unseen entries, no ID collision) —
  merges, re-sequences, replays correctly; replayed state matches applying
  both sides' mutations in timestamp order.
- Task-ID collision — detected, merge aborts, **nothing written to either
  side**, report contains both conflicting creation events.
- Idempotent re-push/re-pull — no-op, byte-identical result.
- Empty target (first publish via `push`) — creates target project
  directory + log + `cache.db`.
- `pull` from nonexistent target project — errors cleanly.

**Rollout** (layered commits, `make verify` green before each lands):
1. `cache.db` + rebuild/lazy-miss, replacing the JSON cache files and
   `labels.json`. Store-level tests updated to assert DB state. TUI/CLI read
   paths repointed at DB queries (no behavior change from the caller's
   perspective — same JSON/text output shapes).
2. `SyncTarget` interface + filesystem implementation + merge algorithm +
   task-ID collision detection, with unit tests per the list above.
3. `atm store push`/`pull` CLI commands + golden text/JSON output tests.
4. Docs: `atm conventions` update, README, mention of `cache.db` as a safe-
   to-delete local artifact (and a note that external whole-directory sync
   tools pointed at `$ATM_HOME` should exclude `cache.db*` if anyone does
   that instead of using the built-in commands — harmless since it's fully
   derived, but pointless to sync and could be corrupted by a naive file-copy
   tool mid-write).

**Verification gate:** `make verify` (`make build && make test`) remains the
gate per AGENTS.md. No new make targets.

## Out of scope (v1)

- HTTP `SyncTarget` implementation — interface supports it; not built yet.
- Whole-store (multi-project) sync in one invocation.
- Automatic task-ID collision renumbering — manual resolution only.
- True concurrent multi-user sync (real-time collaboration). This spec's
  merge model assumes single-user-multiple-machines, occasional divergence,
  not continuous concurrent writers.
- Vector clocks / logical clocks to protect against wall-clock skew during
  merge.
- Encryption/auth of sync targets (filesystem permissions are the only
  access control in v1; an HTTP target would need to define its own).
- Migrating existing `$ATM_HOME` installations — same as prior specs'
  precedent (v2, audit-log redesign), this is a wholesale rebuild with no
  migration tooling; existing stores are rebuilt fresh or deleted.
