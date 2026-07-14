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
  "replica_id": "r_7k3mq9xw2v",
  "store_instance_id": "01J...",
  "last_hlc": { "p": 1752480000000, "l": 4 },
  "created_at": "2026-07-14T00:00:00Z",
  "updated_at": "2026-07-14T00:00:00Z"
}
```

Replica id and HLC state are local authoring state. They are never synced as content and never enter upgraded v1 event bytes.

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

## Rollback and re-upgrade

Rollback is an explicit active-format switch back to v1. It does not export v2-only events into v1. This is acceptable because v1 is a safety fallback, not the long-term mode.

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

## Store and cache integration

`internal/store` remains the public in-process API used by CLI and TUI. L3 rewires the implementation behind that API.

For v2-active projects:

- Mutations author v2 events instead of v1 `LogEntry` rows.
- Reads fold `events.v2.jsonl` through `internal/eventsource` and materialize existing `store.Project`, `store.Task`, `store.Comment`, and `store.Label` compatibility views.
- CLI/TUI continue displaying task and comment aliases as the user-facing ids.
- Identity hashes remain internal unless needed for ambiguity, history, sync, or debugging.

`cache.db` remains derived. Rebuild for a v2 project scans `events.v2.jsonl`, builds the DAG, folds state, and writes cache rows. The v1 `last_log_seq` freshness key is not meaningful for v2; v2 freshness must use event-file-derived data such as event count, file size, mtime, or a frontier digest.

The cache should gain enough identity-aware columns/indexes to support ambiguous aliases and identity-prefix lookup without changing the CLI surface prematurely. It may keep alias strings in existing `id` columns during the first rewire to reduce blast radius, but the source of truth is identity plus stored alias in the event file.

## User-facing upgrade surface

The README and CLI help must include an explicit upgrade runbook. The intended surface is:

```sh
atm store upgrade --project <CODE>
atm store upgrade --all
atm store rollback --project <CODE> --to v1
atm store verify
atm store rebuild
```

The README must state:

- v1 `log.jsonl` is preserved untouched.
- v2 `events.v2.jsonl` is written separately.
- failed upgrade does not cut over.
- `atm store verify` validates the active store.
- rollback does not copy v2-only writes back into v1.
- re-running upgrade after v1 rollback rebuilds/replaces the v2 event file from the current v1 log.

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
- upgraded projects matched v1 replay at cutover time.

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
| **L3-4** | Rollback switches active format to v1 but does not export v2-only writes. |
| **L3-5** | Re-upgrade after v1 fallback rebuilds v2 from the current v1 log and archives/replaces the previous v2 file. |
| **L3-6** | The per-project lock is the local write serialization point; frontier/HLC refresh happens under that lock. |
| **L3-7** | A complete fsynced event line is the v2 append commit point; metadata and cache are rebuildable. |
| **L3-8** | Replica-copy detection remints replica id before first write from a copied store. |
| **L3-9** | `internal/store` stays the CLI/TUI API while its backing implementation is rewired to v2 for active projects. |
| **L3-10** | README upgrade/rollback instructions are an explicit implementation deliverable. |
