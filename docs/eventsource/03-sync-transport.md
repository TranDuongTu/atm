# ATM Distributed Event Source — L4 Sync & Transport Protocol

**Status:** Proposed (detailed sub-spec)
**Tracking:** ATM-0108 (Design: L4 sync & transport protocol)
**Depends on:** `00-architecture.md` (D1–D6), `01-core-data-model.md` (L0–L2), `02-storage-layout.md` (L3)
**Scope:** How two replicas of the *same project* exchange events and converge: the reconciliation model, the `SyncTarget` transport interface, the v1 transports (directory, git), the CLI surface, and the failure model. Cross-project merge (use-case 2), interactive have/want mechanics, HTTP/SSH transports, trust/auth (L5), and version negotiation (X) are out of scope and noted where they attach.

## Driver

L0–L2 made events content-addressed, causal, and order-independently mergeable; L3 landed them in one canonical `events.v2.jsonl` per project. Nothing yet moves events between machines. L4 defines that movement — and defines it so the ATM-0123 GitHub adapter, a Syncthing folder, a USB stick, and a future team HTTP hub are all the *same protocol* over different carriers.

The binding constraint, fixed during brainstorming: the v1 transports are **passive media** — a directory and a git repo. Neither can run code, hold a lock for us, or answer questions. The protocol core therefore cannot require an interactive counterpart; live peers are an optimization slot, not the foundation.

## The sync model: set reconciliation, not replication

> **A sync brings the local project and a remote to the union of their event sets.**

Events are identified by content hash (D1), so "what is the other side missing" is exact set difference — no timestamps, no sequence numbers, no heuristics, no dedup rules beyond identity itself. One sync is: fetch the remote set, compute both differences, ingest what the local side lacks, publish what the remote lacks.

Union is commutative, associative, and idempotent. That is the entire correctness argument, and it is worth spelling out what it buys:

- **Any topology converges.** Hub-and-spoke through a NAS, mesh via USB stick, GitHub in the middle, all at once — any interleaving of pairwise unions drives every replica toward the same set, and D4 turns the same set into byte-identical state.
- **Any failure recovery is "run it again."** A sync that dies after ingesting but before publishing left both stores valid; the next sync completes the difference. Nothing needs journaling, resumption tokens, or cleanup.
- **Any race resolution is "union the copies."** When a passive medium forks (two writers, a file-sync conflict copy), the recovery is the same operation sync already performs, and it is lossless by construction.

Sync operates on **one project at a time** (per-project isolation, an architecture invariant). The CLI may loop over projects; the protocol never does.

### The same-project guard

Two stores may both hold a project *named* `ATM` that are different projects. Names carry no identity (L1).

> **A project's sync identity is its root `project.created` event id.** Sync proceeds only when both sides hold the same root. A mismatch is refused, reporting both root ids and directing the user to the (separate, deferred) cross-project merge operation.

A remote that does not hold the project at all is not a mismatch — it is an empty remote, and the first push publishes the full set. A v2 project upgraded from v1 has a deterministic root on every machine (D6 purity), so two independent upgrades of the same v1 store pass the guard, as designed.

### Validation before commit — the union is atomic

Fetched remote events are staged and validated before anything touches the local store:

1. every line parses as a v2 event object, raw bytes retained verbatim;
2. every event id recomputes from its canonical bytes — a hash mismatch is corruption or tampering, and D1 makes this check free;
3. every parent resolves within the union of the two sets;
4. the union DAG is acyclic;
5. the same-project guard holds.

Any failure rejects the **whole sync**: no partial ingestion, local store untouched, remote untouched. A passive remote cannot be half-trusted — if its file is corrupt, the answer is an integrity error naming the remote, mirroring L3's crash-recovery stance (never skip-and-continue).

Unknown fields on known events and events with unknown actions are **preserved byte-verbatim and forwarded** (D5, L0-2). This is an integrity requirement, not a courtesy: re-serializing through a lossy struct would change the bytes and destroy the event's identity. Events with an envelope version newer than the reader ride through sync the same way; the fold treats what it cannot interpret as inert. The version-negotiation handshake that lets endpoints agree to *do better* than this is X's, and it attaches at the capability layer below.

### Order rules

> **A reader of any event file MUST NOT assume line order.** The DAG is reconstructed from the full set.

A git `merge=union` of two concurrent pushes can interleave lines arbitrarily — children before parents. That must be legal input everywhere, remote or local.

> **A writer SHOULD append in parent-before-child order.** Sync appends missing events in topological order, HLC-tiebroken, so the append order is deterministic and the local file stays causally sorted.

### Local commit

Missing events append to `events.v2.jsonl` under the per-project lock, with the same fsynced-line commit point as L3 authoring. Before releasing the lock, the local HLC observes the maximum HLC among ingested events (L0's receive rule), so the next locally-authored event cannot stamp behind anything this replica has seen.

## The SyncTarget interface

Two levels. Level 0 is mandatory and is the whole v1 implementation; Level 1 is an optional capability that shapes the slot live transports will fill.

```go
// Level 0 — mandatory. Sufficient for any transport, including passive media.
type SyncTarget interface {
    // Fetch returns the remote's complete raw event set for the project.
    // A remote that does not hold the project returns an empty snapshot
    // with Absent set, not an error.
    Fetch(ctx context.Context, project string) (RemoteSnapshot, error)

    // Publish delivers events the remote lacks. base is the snapshot the
    // difference was computed against; a transport uses it to detect and
    // survive concurrent publishers (append-only or fetch-retry, per transport).
    Publish(ctx context.Context, project string, missing []RawEvent, base RemoteSnapshot) error
}

// Level 1 — optional narrowing capability. A target advertises it by
// implementing the interface; absence means full-snapshot sync.
type Narrowing interface {
    // Frontier returns a digest of the remote set and its DAG heads.
    // Equal digests short-circuit a no-op sync without a full fetch.
    Frontier(ctx context.Context, project string) (FrontierInfo, error)

    // FetchSince returns events not reachable from the given heads.
    FetchSince(ctx context.Context, project string, haves []EventID) ([]RawEvent, error)
}
```

`RemoteSnapshot` carries the raw events, an `Absent` flag, a set digest, and transport-private state (e.g. the git commit the snapshot came from). The digest gives even Level 0 targets a cheap no-op path: equal digests, nothing to do.

Full interactive have/want session mechanics (round-tripping want lists, pack framing) are deliberately **not specified** — they are shaped by `Narrowing` and will be specified when the first live transport (HTTP/SSH) is designed. Capability advertisement for live endpoints rides X's negotiation. The ATM-0123 GitHub target implements Level 0 only.

## v1 transports

### Directory target

URL form: a filesystem path. The remote is a **mirror store**: `<root>/<CODE>/events.v2.jsonl`, the same file shape L3 defines, readable by any ATM.

- **Fetch** reads the file (absent file ⇒ absent project). A real store is itself a valid remote: point the URL at its `projects/` directory.
- **Publish** opens with `O_APPEND` and writes the missing lines plus fsync — never rewrite-and-rename. Concurrent publishers on a real filesystem interleave whole lines instead of losing a file to last-rename-wins.
- A file-sync service (Syncthing, Dropbox) racing two machines can still fork conflict copies of the file. The documented recovery is mechanical: union the copies (concatenate, sync, done) — lossless by construction, and identical to what sync itself does.

### Git target

URL form: anything git recognizes (`git@…`, `https://….git`, a local bare repo path), with an optional `//<subpath>` suffix; the default subpath is `.atm`, matching ATM-0123's in-repo layout. The event file lives at `<subpath>/<CODE>/events.v2.jsonl` in the repo.

- ATM maintains a cached clone per remote under `$ATM_HOME/remotes/`, shelling out to the system `git` binary (required on PATH only when a git remote is used).
- **Fetch** = `git fetch` + read the file at the remote head.
- **Publish** = append the missing lines, commit, push. On a non-fast-forward rejection: fetch, recompute the union against the new head, retry — bounded attempts, then fail with the retryable-error report below.
- Sync writes a `.gitattributes` entry marking the event file `merge=union` on first publish. Sync itself never relies on it (it always re-unions against the fetched head), but it makes *human-side* git merges of the file semantically correct, because line union is event-set union.
- Credentials are ambient git auth (ssh-agent, credential helpers). **L4 adds no authentication of its own** — identity and authorization are L5's.

## Remote model and CLI surface

Remotes are **named, per-project, and replica-local**, stored in `projects/<CODE>/config.json`. They are never synced as content: a remote describes *this replica's* route to the world, exactly like `.git/config`.

```sh
atm store remote add <name> <url> --project <CODE>
atm store remote list [--project <CODE>]
atm store remote remove <name> --project <CODE>

atm store sync [<name-or-url>] [--project <CODE>] [--pull | --push] [--dry-run]
```

- `atm store sync` with no `--project` syncs **every project that has at least one configured remote**, each independently. The daily multi-machine loop is one command; the protocol underneath stays per-project.
- With no remote argument, the remote named `origin` is used.
- Direction is bidirectional by default; `--pull` / `--push` restrict it.
- `--dry-run` fetches, validates, and reports the set differences (and any would-be root mismatch) without committing anything anywhere.
- A raw URL or path is accepted ad-hoc in place of a name; nothing is persisted.

Transport selection: a URL git recognizes (or ending `.git`, or containing a `//` subpath suffix on a git URL) selects the git target; an existing directory path selects the directory target; ambiguity is an error asking for an explicit form.

### Bootstrap: the second machine

`atm store sync <url> --project <CODE>` where the project does not exist locally is the clone path: fetch, validate, create the project locally from the remote set, and persist the URL as the project's `origin` — the `git clone` affordance. No separate `clone` verb. (Directory-copying the whole store also remains supported; L3's replica-remint covers it.)

## Local effects after ingest

- **Cache and views.** A sync that ingested events rebuilds the project's cache rows from the new fold, the same rebuild path L3 defines. The v2 freshness key is the event count (L3-11); the file grew, so every existing poller wakes normally.
- **Creation ordinals shift, by design.** Merged events interleave by HLC, so an entity's creation ordinal (`log_seq`) can change after sync. This is anticipated and safe: display order is HLC creation order (L1-3), the projector restamps ordinals at rebuild, and vector re-embedding decisions are text-hash-based (L3-15), so a sync causes no re-embedding storm — only genuinely new or changed text embeds.
- **Contested slots need no sync-time handling.** Concurrent edits arriving via sync simply give some slots more than one maximal writer; the fold converges and the contested board (L2-5) surfaces them. Sync never prompts, blocks, or auto-resolves — the conflict doctrine is L2's, and L4 inherits it by doing nothing.
- **Exit report.** Sync prints pulled/pushed event counts per project, and when the ingest produced newly-contested slots it says so and points at the contested board. Convergence is quiet by doctrine; it must not be *silent* — surfacing exactly what a human should review is the ATM-native half of the CRDT bargain.

## Failure model

| Failure | Outcome |
|---|---|
| Fetch fails, or staged validation fails (parse, hash, parents, cycle) | Sync aborts. Local and remote untouched. Integrity failures name the remote and the offending event. |
| Root mismatch | Refused, reporting both root ids and pointing at cross-project merge (deferred). Nothing transfers. |
| Ingest committed, publish fails (network died mid-sync) | Legal state, reported as "pulled N, push failed: <cause>". The next sync completes the push. |
| Concurrent publisher on a git remote | Non-fast-forward → fetch, re-union, retry (bounded). Exhaustion is a retryable error, never corruption. |
| Concurrent publisher on a directory remote | Whole-line `O_APPEND` interleaving; duplicate lines are harmless (dedup by id is free). Conflict copies forked by file-sync services recover by union. |
| Crash mid-ingest | L3 crash recovery: complete fsynced lines are committed events, a torn tail line is truncated. Re-running sync fetches the difference again — idempotent. |

The unifying property: **every failure's recovery is "run sync again."** That is not a slogan but a consequence of union idempotence, and any future transport must preserve it.

## Testing requirements

- **Unit:** set difference and staging validation (each rejection class); topological append order determinism; transport selection; remote config round-trip.
- **Convergence integration:** a two-store harness — author divergent events in A and B, sync both through a shared remote, assert **byte-identical folds** (the D4 promise, tested end-to-end); then a three-replica property test randomizing sync order and pairings, asserting convergence regardless of interleaving.
- **Git target:** tests against a local bare repo, including a forced non-fast-forward race (two publishers) proving the retry loop unions rather than clobbers.
- **Directory target:** concurrent-publish interleaving test; conflict-copy union recovery test.
- **Guard tests:** root mismatch refusal; corrupt remote (bad hash, missing parent, cycle) refusal with local store proven byte-untouched.
- **Bootstrap:** second-machine clone path, including `origin` persistence and replica distinctness.

## Out of scope

- **Cross-project merge** (use-case 2) — a follow-on design task; the guard here refuses it, never mangles it.
- **Interactive have/want session mechanics** — shaped by `Narrowing`, specified with the first live transport.
- **HTTP/SSH transports** — future `SyncTarget` implementations; the git-vs-GitHub analogy is the intended extension path.
- **Authentication, signing, authorization** — L5. L4 rides ambient transport credentials only.
- **Version negotiation handshake** — X. L4's baseline is D5 preserve-and-forward.
- **Daemons, watch modes, auto-sync scheduling** — the sync driver is the user, a script, or the ATM-0123 GitHub Action.
- **Event GC / partial history transfer** — full sets only; revisit with L2's retention posture if logs stop being small.

## Summary of decisions

| # | Decision |
|---|---|
| **L4-1** | Sync is **set reconciliation**: bring both sides to the union of their event sets. Union idempotence makes every topology converge and every failure recoverable by re-running. |
| **L4-2** | The protocol core is **passive-medium-first**: no step requires an interactive counterpart. Live peers are an optimization slot, not the foundation. |
| **L4-3** | A project's sync identity is its **root `project.created` event id**; mismatched roots refuse to sync (cross-project merge is a separate, deferred operation). |
| **L4-4** | **Staged validation, atomic union**: parse, hash-recompute, parent resolution, acyclicity, and the root guard all pass before anything commits; any failure aborts the whole sync. |
| **L4-5** | Readers never assume event-file line order; writers append topologically (HLC-tiebroken). Local ingest appends under the L3 project lock and advances the local HLC past everything ingested. |
| **L4-6** | `SyncTarget` is two-level: **Level 0 (Fetch/Publish full snapshots) mandatory**; **Level 1 (`Narrowing`: frontier digest, fetch-since) optional**, shaping future live transports. Snapshot digests give Level 0 a cheap no-op path. |
| **L4-7** | v1 ships two targets: **directory** (mirror store, `O_APPEND` publish, conflict-copy recovery = union) and **git** (cached clone, system git, bounded non-fast-forward retry, `merge=union` gitattribute, subpath default `.atm` matching ATM-0123). |
| **L4-8** | Remotes are **named, per-project, replica-local** (`projects/<CODE>/config.json`, never synced). CLI lives under `atm store`: `remote add/list/remove`, `sync [remote] [--project] [--pull|--push] [--dry-run]`; project-less `sync` walks all projects with remotes. |
| **L4-9** | **Bootstrap = sync into an absent local project**: pull, validate, create, persist the URL as `origin`. No separate clone verb. |
| **L4-10** | Sync-time conflict handling is **nothing**: the fold converges, the contested board surfaces, and the exit report points at it. L4 adds no prompts, no auto-resolution, no new conflict machinery. |
| **L4-11** | L4 adds **no authentication** (ambient transport credentials; identity/authorization are L5) and **no version handshake** (D5 preserve-and-forward baseline; negotiation is X). |
