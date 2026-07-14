# ATM Distributed Event Source - L3 Storage Layout

**Status:** Proposed (detailed sub-spec)
**Tracking:** ATM-0107 (Design: L3 storage layout for the distributed event source)
**Depends on:** `00-architecture.md`, `01-core-data-model.md`
**Scope:** The local durable storage layout for v2 events, the v1 -> v2 cutover model, replica-local state, append/recovery rules, and the way `internal/store` consumes v2 state. Sync transport (L4), trust/auth (L5), and version negotiation (X) are out of scope.

## Driver

L0-L2 define the v2 event model and convergent fold, but the live store still writes v1 `projects/<CODE>/log.jsonl`. L3 decides how those v2 events land on disk and how ATM switches real reads/writes to them without risking the existing v1 ledger.

The storage layout must satisfy four constraints:

- Preserve the existing v1 log untouched so upgrade has a rollback source.
- Make v2 the live source of truth after a verified cutover.
- Keep the source of truth text, portable by directory copy, and easy for agents and GitHub Actions to inspect.
- Keep derived state (`cache.db`, vector indexes, frontier caches, alias indexes) disposable.

## Chosen layout: one canonical v2 JSONL file per project

Each project gets a single authoritative v2 event file, stored side-by-side with the old v1 log:

```text
$ATM_HOME/
  store.json
  cache.db
  projects/
    <CODE>/
      log.jsonl
      events.v2.jsonl
      eventsource.json
      config.json
      vectors/
    <CODE>.lock
```

`projects/<CODE>/events.v2.jsonl` is the v2 source of truth. Each line is exactly one canonical v2 event JSON object, as emitted by `internal/eventsource.Event.Raw`, followed by `\n`. The event id is not stored as a separate line prefix or envelope field; readers recompute it from the canonical bytes.

`projects/<CODE>/log.jsonl` remains the v1 source log. Upgrade never rewrites it. If a user rolls back to v1 and writes more v1 entries, a later upgrade reads the current v1 file from scratch.

`projects/<CODE>/eventsource.json` is operational metadata, not event truth. It may cache upgrade provenance, event count, file size, frontier, last verified digest, or active generation. If it is missing or stale, ATM rebuilds it by scanning `events.v2.jsonl`.

`store.json` is store-local metadata. It records active format state and replica-local authoring state, for example:

```json
{
  "active_format": "v2",
  "project_formats": { "ATM": "v2", "DOC": "v1" },
  "replica_id": "r_7k3mq9xw2v",
  "store_instance_id": "01J...",
  "last_hlc": { "p": 1752480000000, "l": 4 },
  "created_at": "2026-07-14T00:00:00Z",
  "updated_at": "2026-07-14T00:00:00Z"
}
```

Replica id and HLC state are local authoring state. They are never synced as content and never enter upgraded v1 event bytes.

### Active-format semantics

A project's effective format is `project_formats[code]` when an explicit entry exists, else `active_format`, else v1. Explicit entries always win, and the system maintains one invariant that makes the fallback safe: **every project whose media is v2 carries an explicit `project_formats` entry**, written at upgrade cutover or at v2 birth. `CreateProject` also writes an explicit entry for v1 births, so entry-less projects are exactly the legacy projects that predate format tracking — and those are v1 media by construction. A v1 value of `active_format` is therefore always read-safe for entry-less projects, while a v2 value never is (it would read a project with no event file as v2).

`active_format` consequently governs only two things: which format `CreateProject` births a new project into, and the read default for legacy entry-less projects.

- `atm store upgrade --all` sets `active_format` to `v2` only after every on-disk project has upgraded successfully — each then holds an explicit v2 entry, so the flip can never change how an existing project is read. It only makes future projects be born v2. A single-project `upgrade --project` never touches `active_format`.
- `atm store set-format --format v1|v2` is the operator override for `active_format` alone. Setting `v2` is refused while any on-disk project lacks an explicit `project_formats` entry (the error directs to `upgrade --all`). Setting `v1` is always allowed and is the documented way to make new projects be born v1 again after a rollback.
- Per-project rollback writes an explicit `project_formats[code] = "v1"` entry and never touches `active_format`.

## Rejected layouts

**Loose event objects plus manifest** (`events/sha256/<hash>.json`, `frontier.json`) maps well to Git-style have/want sync, but it adds many-file atomicity and recovery rules before ATM needs them. L4 can still build have/want reconciliation from a JSONL scan and derived indexes.

**SQLite as the v2 source of truth** gives fast indexes but works against ATM's core storage values: a reviewable text source, easy directory copy, and GitHub-friendly dogfooding. SQLite remains suitable for `cache.db` because the cache is derived and disposable.

## Upgrade and cutover

Upgrade is side-by-side and verified before activation:

1. Acquire `projects/<CODE>.lock`.
2. Read the current v1 `projects/<CODE>/log.jsonl`.
3. Run the D6 upgrade (`eventsource.UpgradeV1`) to produce canonical v2 events.
4. Write those events to a temporary file in the project directory.
5. Read the temp file back, recompute every event id, validate parents, build the DAG, and fold it.
6. Compare the v2 fold to the current v1 replay for the semantic fields ATM exposes today.
7. Atomically rename the temp file to `events.v2.jsonl`.
8. Rebuild `cache.db` from the v2 fold.
9. Mark the project active on v2 only after all prior steps succeed.

If any step fails before activation, the project stays on v1 and `log.jsonl` remains untouched.

Upgrade REFUSES a project whose effective format is already v2. Upgrade reads from the frozen v1 log, so running it against a v2-active project would rebuild stale state and archive the LIVE `events.v2.jsonl` — silently discarding every post-cutover write, because archived files are manual-recovery evidence, never auto-merged — and a v2-born project has no v1 log to read at all. Re-upgrade is legal only from a v1-active, post-rollback state (L3-5). `atm store upgrade --all` therefore SKIPS effective-v2 projects rather than erroring, counting them as already-upgraded for the `active_format` flip decision; this is what makes retrying a partially-failed `--all` safe for the projects that already cut over.

Cutover also deletes the project's `vectors/` directory: vector entries are keyed by the v1 log seq, which is meaningless under v2 and would poison dedup and staleness checks. The embedding indexer re-embeds from the v2 fold using the v2 freshness key defined under "Store and cache integration".

## Rollback and re-upgrade

Rollback is an explicit active-format switch back to v1. It does not export v2-only events into v1. This is acceptable because v1 is a safety fallback, not the long-term mode.

Rollback requires v1 media: it REFUSES with a clear error when `projects/<CODE>/log.jsonl` is absent. A v2-born project has no v1 fallback — replaying the missing log would delete the project's cache rows and reinsert nothing, leaving a zombie that is effective-v1 with zero media: unreadable, and unrecreatable because the media-based existence check still sees `events.v2.jsonl`.

Rollback also rebuilds the project's `cache.db` rows from the v1 replay before returning. The cache otherwise still holds v2-derived rows whose freshness bookkeeping (`LogSeq`, `NextTaskN`) is meaningless to v1 readers and writers, which would trip integrity checks or produce stale reads immediately after the switch.

Rollback additionally deletes the project's `vectors/` directory for the same reason cutover does: v2 entries carry creation ordinals in `log_seq`, which would poison v1 dedup and staleness. The next index pass re-embeds from the v1 replay.

Rollback writes an explicit `project_formats[code] = "v1"` entry and does not touch `active_format`: after a per-project rollback, new projects are still born in whatever format `active_format` names. An operator who has rolled back and wants v1 births again runs `atm store set-format --format v1`.

If v1 accepts new writes after rollback, re-upgrade is allowed. The re-upgrade reads the current v1 log from scratch, moves any existing `events.v2.jsonl` aside, and writes a fresh v2 file. Unchanged v1 entries produce the same v2 event ids because D6 is a pure function of the v1 bytes. New v1 suffix entries produce new v2 events at the end of the upgraded linear chain.

ATM must not silently merge an archived v2 branch with the re-upgraded v1 branch. Archived v2 files are evidence for manual recovery, not automatic inputs.

## Authoring after cutover

For a v2-active project, every mutation authors a v2 event directly:

1. Acquire `projects/<CODE>.lock`.
2. Refresh the current frontier from `events.v2.jsonl` while holding the lock.
3. Ensure the store has a valid replica id.
4. Detect whether the store copy needs a new replica id before writing.
5. Observe the HLCs in the current frontier and the persisted local HLC.
6. Tick the local HLC.
7. Create the event with parents equal to the current frontier.
8. Append the raw canonical event bytes plus `\n` to `events.v2.jsonl`.
9. Fsync the file.
10. Update metadata and cache while still holding the lock, or leave them to be rebuilt later.

No writer may author from a frontier captured before it acquired the project lock. This is what makes two ATM sessions on the same machine and store safe: the first writer commits, the second writer then derives a frontier that includes the first event and authors a causal descendant.

Authored events reference entities by identity, not alias: mutations set `subject.id` to the target's identity hash, and comment creation carries `payload.task_ref` / `payload.reply_to_ref` identities. The writer resolves user-facing aliases to identities from the fold computed under the same lock; an ambiguous alias is an error surfaced to the caller with the candidates, never a silent pick. Task and comment creation goes through the core authoring helpers (`NewTaskCreated`, `NewCommentCreated`, `NewProjectCreated`), which own alias minting (ATM-0125).

## Crash recovery

The append commit point is a durable, newline-terminated event line in `events.v2.jsonl` after fsync.

If a process crashes while holding `projects/<CODE>.lock`, the OS releases the file lock when the process exits. The next process recovers by scanning the event file.

Recovery rules:

- Crash before writing an event line: no event happened.
- Crash during an event line write: if the tail is incomplete or malformed and lacks a trailing newline, truncate only that tail.
- Crash after writing and fsyncing the event line but before metadata/cache updates: the event happened; metadata and cache rebuild from the event file.
- A malformed complete line, hash mismatch, missing parent, or DAG validation failure is an integrity error. ATM must not skip it and continue writing automatically.
- `eventsource.json` is always rebuildable. Stale or corrupt metadata must not override the event file.

## Replica-copy detection

L0 requires a copied store to remint its replica id before two machines author divergent events with the same replica id. L3 supplies the mechanism.

`store.json` stores a `store_instance_id` and the current `replica_id`. A machine-local marker records which store instances this machine has written. On first write, ATM compares the store instance against the marker. If the store appears to be a copied instance that has not been claimed on this machine, ATM remints `replica_id` before authoring and records the claim locally.

The exact marker file is implementation detail, but the invariant is not: two machines must not continue authoring with the same copied replica id.

Same-machine multi-process sessions are not a replica-copy problem. They share the same replica id and are serialized by the per-project lock.

## Project existence, creation, and removal

The authoritative "does this project exist" test is media presence: a project exists iff `projects/<CODE>/log.jsonl` OR `projects/<CODE>/events.v2.jsonl` exists. A v2-born project has no v1 log, so an existence check keyed to `log.jsonl` alone would let `CreateProject` clobber it.

When the effective birth format is v2, `CreateProject` authors `project.created` as the event file's root event through `NewProjectCreated`, seeds the default labels as `label.upserted` v2 events, writes the explicit `project_formats[code] = "v2"` entry, and projects the fold into `cache.db`. It never touches `log.jsonl`.

`RemoveProject` on a v2-active project appends nothing to v1 — `log.jsonl` stays byte-identical, per the global no-v1-writes rule for v2-active projects. It keeps the existing has-tasks guard, deletes the project directory (including `events.v2.jsonl` and `vectors/`), REMOVES the project's `project_formats` entry from `store.json`, and deletes the project's cache rows including the v2 freshness row. Removing the entry matters: recreation is then governed by `active_format` rather than by a stale v2 entry pointing at a project with no event file. v1 removal semantics are unchanged (tombstone append, then directory removal) except that a `project_formats` entry, if present, is removed there too.

## Store and cache integration

`internal/store` remains the public in-process API used by CLI and TUI. L3 rewires the implementation behind that API.

For v2-active projects:

- Mutations author v2 events instead of v1 `LogEntry` rows.
- Reads fold `events.v2.jsonl` through `internal/eventsource` and materialize existing `store.Project`, `store.Task`, `store.Comment`, and `store.Label` compatibility views.
- CLI/TUI continue displaying task and comment aliases as the user-facing ids.
- Identity hashes remain internal unless needed for ambiguity, history, sync, or debugging.

`cache.db` remains derived. Rebuild for a v2 project scans `events.v2.jsonl`, builds the DAG, folds state, and writes cache rows. The v1 `last_log_seq` freshness key is not meaningful for v2; the v2 freshness key is the **event count** of `events.v2.jsonl` (a cheap newline count, no parsing). The cache stores the projected event count per project; the cache is fresh iff that value equals the file's current count. Event count is sufficient before L4 because the local store only ever appends; L4 sync may upgrade the key to a frontier digest.

`Store.LastLogSeq` — the staleness probe the TUI indexer pane, the embedding watcher, `ReadLogCached`, and `atm index status` already poll — returns this event count for v2-active projects. Branching inside that one method is what lets every existing poller wake on v2 appends with zero caller changes.

### User-facing ID grammar across generations

v2 aliases are hash-derived: `MintTaskAlias` yields `<CODE>-` + at least 6 lowercase hex chars (locally extended while taken), and `MintCommentAlias` yields `<task-alias>-c` + at least 4 lowercase hex chars. v1 ids are numeric (`<CODE>-0001`, `<CODE>-0001-c0002`). The store's ID gates (`TaskIDRe`, `CommentIDRe`, `ParseTaskID`, `ParseCommentID`) accept BOTH generations through one relaxed grammar — suffix segments are `\d+` or lowercase hex of at least the minted minimum length — and the relaxation is global rather than per-media: the parse gate's only job is deriving the project code, which must happen BEFORE the project's format is knowable, so per-media strictness is circular; v1 numeric ids remain a strict subset, and a v2-shaped id aimed at a v1 project falls through to a clean not-found. The numeric segments of a v2 alias parse as 0, and no v2 code path may key on them — v2 paths key on the full alias string. Same-task reply validation compares the reply target's task-alias prefix (everything before the final `-c<suffix>`) against the task alias, never numeric segments; this is well-defined in both generations because v1 `RenderCommentID` and v2 `MintCommentAlias` both build comment ids as `<task-alias>-c<suffix>`. Deriving the project code from either generation inside `ParseCommentID` is what keeps CLI `--history` and the TUI comment panes working with zero caller changes.

### Log-derived views

Every view derived from the log stays behind the same `internal/store` methods (L3-9); v2-awareness is a branch inside the method, never a new caller-facing API:

- **History**: `History` renders `HistoryView`s for a v2 project from the event file — events sorted by the `CompareEvents` total order, `Seq` set to the 1-based ordinal in that order, filtered by the caller's subject after the fold restores entity aliases into each compatibility entry (callers pass aliases; events carry identities). The DAG is strictly richer than a linear log; this linear compatibility rendering is a deliberate L3 flattening for display, and DAG-aware history is L4's problem.
- **Activity**: `ReadLogCached` returns a v2 project's events as compatibility `[]LogEntry` — same ordinal `Seq`, with `At`/`Actor`/`Action` from the event and subject aliases restored from the fold. That is everything `internal/activity` and the TUI activity/summary panes consume, so they change zero lines. `ReadLog` remains v1-only (it backs `Replay`, the upgrade comparison, and rollback, which must read v1 bytes); `atm activity` switches to `ReadLogCached`.
- **Text search**: for a v2 project, text search reads the freshness-gated cache rows instead of a v1 replay — the same gate point reads use, and the same rows list commands display, so no second projection code path exists. Vector dedup keeps the LAST entry when `log_seq` ties (append order wins): a v2 re-embedding reuses the entity's stable creation ordinal, so a first-wins tie-break would keep the stale vector.
- **Embedding indexer freshness**: for a v2 project, `PendingIndex` enumerates the freshness-gated cache rows; re-embedding decisions are made by text hash (exact, content-addressed) rather than seq comparison. `VectorEntry.LogSeq` carries the entity's creation ordinal; `VectorMeta.LastLogSeq` stores the v2 event count captured at the start of the index pass, so `Behind` in `atm index status` means events-behind and mid-pass appends conservatively leave the index behind. Vector indexes are deleted at every format switch (upgrade cutover and rollback) because entries keyed in the other format's sequence space would poison dedup and staleness; the next pass re-embeds from scratch.
- **List freshness**: project-scoped task lists are gated behind the same v2 cache freshness probe as point reads. v1 masked the missing gate via write-through; v2 must gate explicitly.

### CLI output for v2 projects

`next_task_n` has no v2 meaning — aliases are hash-derived, not sequential. Project JSON keeps the field with value `0`, documented as "not applicable"; `atm project list` renders `-` for it (unambiguous: v1 projects always have `next_task_n >= 1`). Task and comment `log_seq` in JSON output is the v2 creation ordinal — a deliberate reuse of the field name, not the v1 log seq.

The cache should gain enough identity-aware columns/indexes to support ambiguous aliases and identity-prefix lookup without changing the CLI surface prematurely. It may keep alias strings in existing `id` columns during the first rewire to reduce blast radius, but the source of truth is identity plus stored alias in the event file.

## User-facing upgrade surface

The README and CLI help must include an explicit upgrade runbook. The intended surface is:

```sh
atm store upgrade --project <CODE>
atm store upgrade --all
atm store rollback --project <CODE> --to v1
atm store set-format --format v1|v2
atm store verify
atm store rebuild
```

The README must state:

- v1 `log.jsonl` is preserved untouched.
- v2 `events.v2.jsonl` is written separately.
- failed upgrade does not cut over.
- `atm store upgrade --all` additionally flips `active_format` to v2 on full success, so new projects are born v2 from then on.
- `atm store set-format` overrides only the birth/default format; it refuses `--format v2` while any project lacks an explicit format entry, and `--format v1` is the way to stop v2 births after a rollback.
- `atm store verify` validates the active store.
- rollback does not copy v2-only writes back into v1 and does not change `active_format`.
- rollback refuses a project with no `log.jsonl` (a v2-born project has no v1 state to roll back to).
- re-running upgrade after v1 rollback rebuilds/replaces the v2 event file from the current v1 log.
- upgrade refuses a project that is already v2-active, and `upgrade --all` skips such projects, so retrying after a partial failure never rewrites a live v2 project.
- upgrade and rollback each delete the project's vector indexes; the next `atm index` pass re-embeds.

## ATM-0123 compatibility boundary

L3 does not design GitHub sync, but it deliberately chooses a GitHub-friendly source file. A GitHub Action can read, verify, diff, and commit a single stable text event source without depending on `cache.db` or a loose-object store.

ATM-0123 owns the GitHub Issues mapping, GitHub-derived actors, workflow triggers, and projection bookkeeping. L3 owns only the local canonical v2 event file and verification primitives that ATM-0123 will consume.

## Verification requirements

`atm store verify` for v2 must prove:

- every line parses as canonical v2 JSON;
- every event id recomputes from its raw bytes;
- every parent exists;
- the DAG is acyclic;
- fold succeeds deterministically;
- derived cache state can be rebuilt;
- upgraded projects matched v1 replay at cutover time;
- vector indexes and inquiry counts are still reported for v2 projects exactly as for v1.

`atm store rebuild` must rebuild `cache.db` from v2 event files for v2-active projects and from v1 logs only for v1-active projects.

## Out of scope

- L4 sync transport and have/want reconciliation.
- GitHub Issues projection details.
- L5 signing, trust, and authorization.
- X version negotiation.
- v2 -> v1 export.
- Automatic merging of archived failed v2 branches after rollback.
- Loose object storage.
- Event garbage collection.

## Summary of decisions

| # | Decision |
|---|---|
| **L3-1** | The v2 source of truth is `projects/<CODE>/events.v2.jsonl`, one canonical event JSON object per line. |
| **L3-2** | v1 `projects/<CODE>/log.jsonl` is preserved untouched through upgrade and rollback. |
| **L3-3** | Upgrade is side-by-side, verified, and atomically cut over only after cache rebuild succeeds. |
| **L3-4** | Rollback switches active format to v1 but does not export v2-only writes; it refuses when `log.jsonl` is absent, so a v2-born project can never be rolled back into a media-less zombie. |
| **L3-5** | Re-upgrade after v1 fallback rebuilds v2 from the current v1 log and archives/replaces the previous v2 file; upgrade refuses effective-v2 projects, and `upgrade --all` skips them as already-upgraded for the flip decision. |
| **L3-6** | The per-project lock is the local write serialization point; frontier/HLC refresh happens under that lock. |
| **L3-7** | A complete fsynced event line is the v2 append commit point; metadata and cache are rebuildable. |
| **L3-8** | Replica-copy detection remints replica id before first write from a copied store. |
| **L3-9** | `internal/store` stays the CLI/TUI API while its backing implementation is rewired to v2 for active projects. |
| **L3-10** | README upgrade/rollback instructions are an explicit implementation deliverable. |
| **L3-11** | The v2 sequence surface is the event count of `events.v2.jsonl`: `LastLogSeq` returns it for v2-active projects, `VectorMeta.LastLogSeq` stores it at index time, and per-entity `LogSeq`/`log_seq` is the creation ordinal. |
| **L3-12** | Log-derived views — history, activity, text search, index freshness, list freshness — branch inside `internal/store`; CLI/TUI callers and `internal/activity` are unchanged (`ReadLog` stays v1-only; `atm activity` moves to `ReadLogCached`). |
| **L3-13** | A project exists iff `log.jsonl` or `events.v2.jsonl` exists; v2 `RemoveProject` appends no v1 entry, deletes the directory, removes the `project_formats` entry, and deletes cache rows. |
| **L3-14** | Explicit `project_formats` entries always win and every v2-media project has one; `active_format` governs only birth format and the legacy entry-less default; `upgrade --all` flips it to v2 only after all projects hold explicit entries; `set-format --format v2` refuses while any project lacks one. |
| **L3-15** | Vector indexes are wiped at every format switch (cutover and rollback); the indexer rebuilds them via text-hash comparison, and vector dedup is last-wins on `log_seq` ties. |
| **L3-16** | v2 project output renders `next_task_n` as not applicable (`0` in JSON, `-` in text); `log_seq` in task/comment output is the v2 creation ordinal by decision, not accident. |
| **L3-17** | Task/comment ID parsing accepts both alias generations globally (v1 numeric ids and v2 hash aliases through one relaxed grammar); same-task reply validation uses the task-alias prefix, and v2 code paths key on the full alias string, never on numeric segments. |
