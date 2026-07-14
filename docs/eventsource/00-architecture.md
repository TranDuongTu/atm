# ATM Distributed Event Source — Architecture

**Status:** Proposed (architecture / overview)
**Tracking:** ATM-0105 (Design: distributed event sourcing & cross-machine sync/merge)
**Supersedes the sync half of:** `docs/superpowers/specs/2026-07-06-atm-storage-sync-design.md` (that spec's cache.db consolidation shipped; its `atm store push/pull` sync sketch — full-log union-merge, dedup by `(at,actor,action,subject)`, re-sequence, LWW-by-wallclock, manual task-ID collision resolution — is replaced by the model below).
**Scope of this document:** the map of the spec suite and the load-bearing cross-layer decisions every sub-spec must honor. Mechanics live in the per-layer specs; this doc is deliberately thin.

## Vision

Make ATM to agents what git is to developers: a durable, mergeable ledger of the past, present, and future of what agents build — that works across machines, across projects, and across teammates. Agents are ephemeral and numerous; they come and go, run on different machines, and touch different parts of the same work. The event source is the shared substrate that lets any of them diverge and later re-converge without a central server and without losing work. The medium-term goal is a formal, versioned specification so that any ATM implementation — regardless of version or local storage — can merge and sync with any other.

The three concrete use-cases this suite must serve, in order of increasing difficulty: (1) one user, one project, multiple machines, merged later; (2) two independent projects merged into one with combined knowledge; (3) teammates working locally on a project, synced later. All three need the same core primitive — a log that can diverge and re-merge without collisions — and differ only in trust, identity, and how often two sides touch the same entity.

## Starting position

ATM is already event-sourced. Each project is a per-project append-only `log.jsonl` (`internal/store/log.go`); state is a pure fold over it (`Store.Replay`); `cache.db` and vector indexes are rebuildable derived views. Three properties of the *current* model block distribution, and the whole suite exists to remove them:

- **(a) `Seq` is a centralized total order.** Assigned as `last+1` under a per-project file lock. Two machines both assign `seq=42` to different events → collision on merge. Seq cannot be identity.
- **(b) Entity IDs are centralized monotonic counters.** `NextTaskN` → `ATM-0105`, `NextCommentN` → `ATM-0105-c0003`. Two machines independently mint the same ID for different entities → collision the old sync design could only *detect and refuse*.
- **(c) No causality or replica identity.** Nothing records which node authored an event or what it happened-after, so concurrent and sequential edits are indistinguishable and there is no principled merge rule — only wall-clock LWW, which clock skew can invert.

## The spec suite

Delivered as a dependency-ordered stack of sub-specs, each with one job. Higher layers depend only on lower ones; this document is the shared contract they all reference.

```
 X · Versioning & Compatibility  — cross-cutting; threads through every layer
─────────────────────────────────────────────────────────────────────────────
 L5 · Identity, Trust & Auth      — replica/actor auth, event signing, who-may-write
 L4 · Sync / Transport            — have/want reconciliation, incremental exchange, SyncTarget
 L3 · Storage Layout              — how events land on disk; portable-by-copy; disposable indexes
─────────────────────────────────────────────────────────────────────────────  ← THE CORE
 L2 · Merge & Convergence         — deterministic order-independent fold → identical state
 L1 · Naming & ID Allocation      — collision-free entity IDs without a central counter
 L0 · Event & Identity Model      — content-addressed event id, replica identity, causality
```

L0–L2 are inseparable and are what every other layer builds on. They are specified together as the first detailed sub-spec (`01-core-data-model.md`). L3, L4, L5, and X each get their own sub-spec and their own design/plan/implementation cycle.

## Load-bearing decisions (locked for the suite)

Every sub-spec inherits these. Changing one is a change to the architecture, not a local decision.

### D1 — Events are content-addressed

An event's identity is the hash of its canonical serialization (a deterministic, version-stable byte encoding — exact scheme defined in L0). Identical events hash identically, so deduplication is *correct by construction* rather than a heuristic over `(at, actor, action, subject)`; distinct events never share an ID. `Seq` is demoted from identity to, at most, a per-replica local display ordinal, and is never part of what syncs. This removes obstacle (a).

### D2 — Causality is a hash-DAG with an HLC tiebreak (hybrid)

Each event names the hashes of the frontier it observed when it was created — its causal parents — so history is a Merkle DAG, exactly like git. This gives tamper-evident integrity (a hash chain), true concurrency detection (two events are concurrent iff neither is an ancestor of the other), and cheap sync reconciliation (a node computes "what am I missing" by walking the other side's frontier). Each event *also* carries a Hybrid Logical Clock stamp — `(physical_time, logical_counter, replica_id)` — which supplies a skew-resistant, deterministic total order used purely as the last-writer-wins tiebreak in L2. Vector clocks are rejected: their per-event size grows with the number of replicas, a bad fit for many ephemeral agents. This removes obstacle (c).

### D3 — Entity IDs are a stable identity plus a local display alias

Every task and comment has a stable, collision-free internal identity (a ULID or content-derived id, decided in L1). The human-friendly `ATM-0105` / `ATM-0105-c0003` is a *display alias* mapped onto that identity and assigned locally. At merge, aliases are deterministically reconciled (re-derived from a canonical rule so all replicas agree), while the underlying identity never moves. Two machines both minting `ATM-0105` for different tasks is no longer a collision to refuse — the identities differ and one alias is remapped, preserving human-friendly IDs and all cross-references that resolve through identity. This removes obstacle (b) without the old design's manual-abort.

### D4 — State is a deterministic, order-independent CRDT fold

Current state is a pure function of the *set* of events a replica holds, not the order they arrived. Any two replicas holding the same event set compute byte-identical state (strong eventual consistency). Per-field resolution: scalar fields (task title/description, comment body, project name, label description, label expression) are last-writer-wins registers keyed on the HLC stamp from D2; label membership is an observed-remove set (OR-Set) so a concurrent add and remove resolve deterministically rather than by arrival luck; entity deletion is a tombstone that a concurrent edit cannot resurrect. The closed action enum from today's log is the starting vocabulary; L2 defines each action's CRDT semantics.

**Computed labels (boards).** A label carrying an expression (`docs/superpowers/specs/2026-07-13-computed-labels-boards-design.md`) is a *saved query* over the label substrate — a board. Its expression is one more LWW register in the list above, so it adds no new resolution rule and the interop spec grows by a clause rather than a concept. Two consequences bind L2, and neither is optional:

- **Computed membership is derived, never stored, and is therefore outside the OR-Set.** A task never carries a computed label. A concurrent "make label computed" and "assign that label to a task" must resolve as *computed-ness wins*: the stored assignment survives in the OR-Set but is inert — the resolver ignores it. Without this rule the two replicas disagree on whether the label is asserted or derived, and D4's byte-identical-state guarantee fails.
- **Merge can create a reference cycle no replica wrote.** Boards may reference other boards. Replica A points board `a` at `b` while replica B points `b` at `a`; both writes are individually valid and neither replica ever held a cycle, yet the order-independent fold produces one. Cycle rejection at write time is therefore necessary but *not sufficient*: the resolver must carry a visited-set guard and surface a cyclic board as broken. This is a general shape worth noting beyond boards — any future field holding a reference between entities inherits the same hazard under D4.

### D5 — Format is versioned; unknown is tolerated, never dropped

The spec carries a format version. Nodes negotiate a common version on sync (capability negotiation, defined in X). Unknown fields on a known event, and events whose action a reader does not understand, are *preserved and forwarded* — never silently dropped — so a newer writer and an older reader can share a project without the older node corrupting history it cannot fully interpret. This is what "merge and sync regardless of version" requires, and it constrains every layer's serialization choices.

### D6 — The current log is format v1; upgrade is a lossless local replay

This suite defines format v2. An existing `log.jsonl` is treated as v1 and upgraded by a one-time local replay: each existing entry is canonicalized and hashed (D1), assigned an HLC (D2), and parented onto the prior frontier to reconstruct a linear DAG (D2). The upgrade is mechanical and lossless — not the wholesale delete-and-rebuild prior ATM specs used — because a single-node linear history is a degenerate DAG. Derived views (`cache.db`, vectors) rebuild from the upgraded log as they do today.

**The upgrade must be a pure function of the v1 log.** No local or replica-specific input may enter the canonical bytes of an upgraded event. A store is portable by directory copy, so the same project is routinely upgraded on more than one machine; if the local replica id fed the hash, each machine would compute a *different* event id for the same historical event, and on the first sync every event in the shared history would appear twice. Because a task's identity is its creation event (L1), every task would fork in two — the whole ledger duplicates.

Concretely: upgraded events carry a reserved replica id (`_v1`, identical on every machine) and an HLC derived solely from the entry's recorded `at` and `seq`. Both are already in the v1 log and are byte-identical on every copy of it, so any two machines upgrading the same log produce byte-identical v2 DAGs and converge trivially. `at` and `seq` are the only ordering information v1 ever recorded, and are therefore the only honest source for the upgraded HLC.

A v1 node cannot sync at all: it has no hashes, no HLC, and no DAG, so it cannot participate. Upgrading is a *precondition* for distribution, not a compatibility mode — and after the upgrade the log simply *is* v2, so no dual-format reader exists in the steady state. All v1 knowledge lives in the one-time migration path and nowhere else. (D5's version negotiation governs v2 and beyond; it was never about v1.)

## Invariants preserved from today's model

The suite is additive to ATM's existing philosophy; these do not change:

- The event log remains the sole source of truth; `cache.db` and vector indexes stay derived, disposable, and rebuildable, and are never the sync payload.
- A store remains portable by directory copy.
- Labels remain the query/classification substrate; nothing here adds a status field or state machine.
- Actor identity stays `persona@agent:model`; L0 extends provenance with a replica id and L5 adds optional signing, but the existing actor grammar is unchanged.
- Per-project isolation holds: sync and merge operate one project at a time (cross-project *merge*, use-case 2, is a distinct explicit operation defined in L3/L4, not implicit whole-store sync).

## What this document does not decide

Deferred to the sub-specs, on purpose: the exact canonical serialization and hash function (L0); the concrete internal id scheme and the alias-reconciliation rule (L1); the per-action CRDT operation table and tombstone/GC policy (L2); on-disk layout of the DAG and how cross-project merge is represented (L3); the reconciliation wire protocol and `SyncTarget` implementations (L4); the signing/trust/authorization model (L5); the version-negotiation handshake and compatibility rules (X). This doc only fixes the decisions those specs must not contradict.

## Sub-spec index

| Spec | Layer | Status |
|------|-------|--------|
| `00-architecture.md` | overview | Proposed (this doc) |
| `01-core-data-model.md` | L0 + L1 + L2 | Proposed |
| `02-storage-layout.md` | L3 | Proposed |
| `03-sync-transport.md` | L4 | Not started |
| `04-identity-trust-auth.md` | L5 | Not started |
| `05-versioning-compat.md` | X | Not started |
