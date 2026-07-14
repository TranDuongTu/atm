# EventSource Storage Layout v2 - Design Spec

**Status:** Proposed
**Tracking:** ATM-0107
**Layer spec:** `docs/eventsource/02-storage-layout.md`
**Depends on:** ATM-0106 (`internal/eventsource` L0-L2 + D6 core)

## Driver

ATM-0106 defines the v2 event model, but the live store still writes v1 `projects/<CODE>/log.jsonl`. ATM-0107 must make v2 storage real while keeping the current ledger safe. The project owner explicitly wants to rewire the live store to v2, but with a new storage file so the v1 log remains available if cutover fails.

The design uses a side-by-side v2 storage file, verifies upgrade before activation, and leaves the existing `internal/store` API in place for CLI/TUI.

## Goals

- Rewire live projects to v2 storage behind the existing CLI/TUI API.
- Preserve each v1 `log.jsonl` untouched during upgrade.
- Support rollback to v1 as a safety fallback.
- Support re-upgrade after v1 accepts more writes.
- Keep the v2 source of truth text, inspectable, and GitHub-friendly for ATM-0123.
- Keep `cache.db` and other indexes derived and rebuildable.
- Define same-machine concurrency and crash recovery rules.
- Include README upgrade/rollback instructions as an implementation deliverable.

## Non-goals

- L4 sync protocol.
- GitHub Issues projection or GitHub Actions workflow.
- L5 trust, signing, or authorization.
- Version negotiation.
- v2 -> v1 export.
- Loose event-object storage.
- Automatic merge of archived failed v2 branches.

## Architecture

The authoritative v2 storage is a single per-project JSONL file:

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

`events.v2.jsonl` stores canonical raw v2 event bytes, one event per line. It is append-only during normal operation. Readers recompute each event id from the line bytes.

`log.jsonl` remains the v1 source and rollback input. Upgrade never rewrites it.

`eventsource.json` and `store.json` are operational metadata. They may cache active format, generation, frontier, replica id, HLC state, upgrade provenance, and verification information. They are not the event source. Anything correctness-critical must be recoverable from `events.v2.jsonl` plus local replica metadata.

## Upgrade and cutover

`atm store upgrade --project <CODE>` performs a side-by-side migration:

1. Lock the project.
2. Read v1 `log.jsonl`.
3. Run `eventsource.UpgradeV1`.
4. Write temp v2 JSONL.
5. Read it back, verify hashes, validate parents, build the DAG, and fold it.
6. Compare the v2 fold to v1 replay.
7. Rename temp to `events.v2.jsonl`.
8. Rebuild `cache.db`.
9. Mark the project active on v2.

`atm store upgrade --all` repeats the project flow per project. A project failure must not corrupt other projects.

If upgrade fails before activation, the project stays v1-active. Failed temp files can be retained for diagnostics but are never active.

## Rollback and re-upgrade

Rollback is an active-format switch:

```sh
atm store rollback --project <CODE> --to v1
```

Rollback does not export v2-only writes into v1. That loss is acceptable because v1 is only a safety fallback.

If the v1 log receives more writes after rollback, upgrade can run again. Re-upgrade reads the current v1 log from scratch, archives or replaces the previous `events.v2.jsonl`, and writes a fresh v2 file. Prior v2-only events remain in the archived file for manual inspection, not automatic merge.

## Authoring rules

For v2-active projects, every mutation authors through `internal/eventsource`:

- Acquire `projects/<CODE>.lock`.
- Refresh frontier from the current file while holding the lock.
- Observe frontier HLCs and persisted local HLC.
- Tick HLC.
- Author a new event with parents equal to the current frontier.
- Append canonical bytes plus newline.
- Fsync.
- Update cache/metadata or leave them rebuildable.

Two ATM sessions on the same machine and same store are safe because the per-project lock serializes writes. The second writer derives its frontier after the first writer commits, so it parents onto the first event rather than creating an artificial local conflict.

## Crash recovery

The append commit point is a complete, newline-terminated, fsynced event line.

Recovery behavior:

- No line written: no event happened.
- Partial tail without newline: truncate the tail.
- Complete line with invalid JSON, hash mismatch, missing parent, or DAG failure: integrity error; do not skip and continue.
- Metadata/cache crash: rebuild from the event file.
- Upgrade temp crash: leave active format unchanged.

## Replica-copy detection

The implementation must persist `replica_id` and a `store_instance_id`. A machine-local marker records store instances this machine has claimed. Before a first write from a copied store, ATM must remint `replica_id` so two machines do not author divergent HLC stamps with the same replica id.

Same-machine multiple processes are not copy detection. They intentionally share one replica id and rely on the project lock.

## Store integration

`internal/store` remains the compatibility API. The rewire should minimize CLI/TUI churn:

- Existing mutators call a v2 authoring path for v2-active projects.
- Existing readers consume cache rows materialized from v2 fold state.
- Point-history and debug surfaces can read events directly.
- Task/comment aliases remain the user-facing ids.
- Identity hashes are exposed only when needed for ambiguity or diagnostics.

`cache.db` remains derived. V2 cache freshness cannot use `last_log_seq`; it needs event-file-derived freshness such as event count, file size, mtime, or frontier digest.

The cache should add identity-aware columns/indexes while preserving alias display. Existing `id` fields can remain alias-oriented for the first rewire if that reduces blast radius.

## CLI and README deliverables

The implementation plan must include CLI help, `atm conventions`, and README updates. README should include this operational runbook:

```sh
atm store path
atm store verify
atm store upgrade --project ATM
atm store verify
```

For all projects:

```sh
atm store upgrade --all
atm store verify
```

Rollback:

```sh
atm store rollback --project ATM --to v1
```

README text must explain:

- v1 `projects/<CODE>/log.jsonl` is preserved.
- v2 `projects/<CODE>/events.v2.jsonl` is written separately.
- failed upgrade does not cut over.
- rollback ignores v2-only writes.
- after v1 rollback and more v1 writes, running upgrade again rebuilds/replaces the v2 file from the current v1 log.
- `cache.db` is derived and can be rebuilt.

## ATM-0123 compatibility

ATM-0123 should treat GitHub Issues as a synced projection over the v2 event source. L3 provides a stable, reviewable, GitHub-friendly `events.v2.jsonl` file and verification primitives. ATM-0123 owns GitHub issue/comment/label mapping, GitHub actors, triggers, and projection bookkeeping.

## Testing strategy

- Upgrade success: v1 fixture upgrades to `events.v2.jsonl`, verifies, rebuilds cache, and marks active v2.
- Upgrade failure: malformed v1 or fold mismatch leaves project v1-active and does not replace prior v2 file.
- Re-upgrade: rollback to v1, append v1 events, upgrade again, archive previous v2, and produce deterministic ids for unchanged v1 entries.
- Same-machine concurrency: two store handles/process simulations serialize under the project lock; second event parents first.
- Crash recovery: partial tail truncates; complete bad line fails integrity.
- Metadata rebuild: delete/corrupt `eventsource.json`, verify/rebuild reconstructs it from `events.v2.jsonl`.
- Cache rebuild: delete `cache.db`, rebuild from v2 fold.
- Replica-copy detection: copied store remints replica id before first write.
- CLI golden tests: `store upgrade`, `store rollback`, `store verify`, and README/conventions text.

## Implementation notes

The implementation should land in small steps:

1. Add v2 paths, metadata types, and active-format detection.
2. Add v2 event file reader/writer and verification helpers.
3. Add upgrade/cutover command.
4. Add cache rebuild from v2 fold.
5. Rewire store reads for v2-active projects.
6. Rewire store writes for v2-active projects.
7. Add rollback/re-upgrade handling.
8. Add replica-copy detection.
9. Update README, CLI help, and conventions.

No implementation should bypass `internal/eventsource` for event identity, canonicalization, DAG construction, or fold semantics.
