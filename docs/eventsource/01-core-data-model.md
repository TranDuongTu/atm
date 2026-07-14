# ATM Distributed Event Source — L0–L2 Core Data Model & Convergence

**Status:** Proposed (detailed sub-spec)
**Tracking:** ATM-0106 (Design: L0-L2 core data model & convergence)
**Depends on:** `00-architecture.md` — decisions D1–D6 are inherited and must not be contradicted
**Scope:** The event envelope and its identity (L0), entity naming (L1), and the convergence rules (L2). This is the core the whole suite builds on. Storage layout (L3), sync (L4), trust (L5), and version negotiation (X) are out of scope and referenced only where they inherit a constraint.

## What this spec removes

The architecture named three properties of today's model that block distribution. This spec removes all three, and in doing so deletes more machinery than it adds:

- **(a) `Seq` as a centralized total order** → replaced by content-addressed event identity (L0).
- **(b) Entity IDs as centralized monotonic counters** → replaced by content-derived identity and a stored, immutable alias (L1). `NextTaskN` and `NextCommentN` are deleted, and with them the `task.meta-changed` action.
- **(c) No causality or replica identity** → replaced by a hash-DAG with an HLC tiebreak (L0), which turns out to supply the OR-Set's observation relation for free (L2).

Two pieces of promised machinery are also deleted, because the model no longer needs them: D3's *alias reconciliation at merge* (nothing to reconcile — aliases are immutable constants that need not be unique), and the OR-Set's *tag storage and GC* (tags are event hashes; observation is causal ancestry).

---

# L0 — Event & Identity Model

## The v2 event envelope

Today's `LogEntry` (`internal/store/log.go:57`) is `{seq, at, actor, action, subject, payload}`. The v2 envelope is:

```json
{
  "v": 2,
  "parents": ["sha256:9f2a…", "sha256:c014…"],
  "hlc": { "p": 1752480000000, "l": 0 },
  "replica": "r_7k3mq9xw2v",
  "at": "2026-07-14T09:12:03Z",
  "actor": "developer@claude:opus-4.8",
  "action": "task.title-changed",
  "subject": { "kind": "task", "id": "sha256:a3f1…" },
  "payload": { "title": "Fix the cache" }
}
```

Changes from v1, and why each exists:

| Field | Status | Purpose |
|---|---|---|
| `v` | new | Format version. Self-describing, so a reader never guesses. |
| `parents` | new | The causal frontier observed at creation (D2). Makes history a Merkle DAG. |
| `hlc` | new | Hybrid Logical Clock (D2). The deterministic tiebreak for concurrent writes. |
| `replica` | new | Which replica authored the event. Provenance, and the final tiebreak in HLC comparison. |
| `at` | kept | Human-facing wall-clock time. **Never used for ordering.** |
| `actor` | kept | Unchanged grammar (`persona@agent:model`). Orthogonal to `replica`: *who* vs *where*. |
| `action`, `subject`, `payload` | kept | Unchanged, except `subject.id` now holds an *identity*, not an alias (see L1). |
| `seq` | **removed from the envelope** | Demoted to a local display ordinal (D1). Never serialized, never synced, never hashed. |

`at` deserves emphasis: it survives only because humans want to read it. **No rule in this spec orders anything by `at`.** Wall-clock skew inverting an ordering is exactly the failure D2 exists to prevent, and leaving `at` in the envelope without saying this invites a future reader to reach for it.

## Event identity (D1)

> **The event id is `sha256:` + the lowercase hex SHA-256 of the RFC 8785 (JCS) canonical JSON of the event object with the `id` field absent.**

- **Canonical form: RFC 8785 / JSON Canonicalization Scheme.** JSON is already the log format, and keeping the log greppable and human-readable is a standing ATM value; JCS gives deterministic bytes (sorted keys, defined number and string escaping) without abandoning it.
- **Hash: SHA-256**, carried with an explicit `sha256:` prefix so a future migration to another function is expressible rather than ambiguous — this is what D5's forward-compatibility requires of an identity scheme.
- The id is never stored *inside* the event it identifies; it is derived on read and may be cached in derived views (`cache.db`).

**Unknown-field preservation is an integrity requirement, not a courtesy.** D5 says unknown fields are preserved and forwarded. Under content addressing this becomes load-bearing: a reader that *drops* an unknown field from a newer event can no longer reproduce that event's canonical bytes, so it can no longer verify or recompute its hash — the event's identity is destroyed in transit. Therefore an implementation MUST parse events into a form that retains unrecognized fields verbatim (a map, not a lossy struct) and MUST re-emit them byte-faithfully.

## Replica identity

A **replica** is one copy of a store on one machine. It is not a user, not an agent, and not an actor — those are `actor`, and they are orthogonal.

- A replica id is 128 random bits, rendered as `r_` + Crockford base32 (e.g. `r_7k3mq9xw2v`).
- It is minted once, on first use, and persisted in the store directory. It is *local state*: it is embedded in the events a replica authors, but it is never itself synced as content.
- **A directory copy of a store copies its replica id.** An implementation MUST detect this (the copied store and the original now share an id, which would let two machines mint colliding HLC stamps) and re-mint on first write after a copy is detected. The detection mechanism is L3's problem; the *requirement* is stated here because it is an L0 invariant.
- **`_v1` is reserved** and MUST NOT be minted. It is used solely by the D6 upgrade (see below).

## Hybrid Logical Clock (D2)

An HLC stamp is `(p, l)`: physical milliseconds and a logical counter. It carries no replica id of its own — the envelope's `replica` field supplies that, and duplicating it inside `hlc` would only add redundant bytes to the hash.

**On authoring an event:**
```
p' = max(local.p, now_ms())
l' = (p' == local.p) ? local.l + 1 : 0
stamp = (p', l')
local = (p', l')
```

**On receiving an event `e`:**
```
p' = max(local.p, e.hlc.p, now_ms())
l' = if   p' == local.p == e.hlc.p  then max(local.l, e.hlc.l) + 1
     elif p' == local.p             then local.l + 1
     elif p' == e.hlc.p             then e.hlc.l + 1
     else                                0
local = (p', l')
```

**Comparison** is lexicographic on the triple `(hlc.p, hlc.l, replica)`, taking the replica id from the envelope. Because replica ids are unique, this is a **total order** over all events in the system, and it is identical on every replica. That totality is what lets every LWW tiebreak in L2 be deterministic without coordination.

The HLC is *only* a tiebreak. It never establishes causality — `parents` does. An HLC stamp being larger does not mean an event happened after; it means that when two events are genuinely concurrent, every replica picks the same winner.

## Causality

Event `a` **happens-before** `b` iff `a` is reachable from `b` by following `parents`. Two events are **concurrent** iff neither is reachable from the other. This is the only definition of concurrency in the suite, and every conflict rule in L2 is phrased in terms of it.

A new event's `parents` is the set of **current frontier** events — those held by the authoring replica that are not a parent of any other event it holds.

---

# L1 — Naming & ID Allocation

L1 was scoped as "design a collision-free ID allocator." It turns out not to need one.

## Entity identity comes free from D1

> **A task *is* its `task.created` event. Its identity is that event's id.**

The same holds for comments, and for labels (identity is the label's name within the project — labels are named, not minted). No ULID, no allocator, no counter, no coordination. D3 left the choice open between a ULID and a content-derived id; content-derived wins because **D1 already computes it**.

`subject.id` therefore carries an *identity* (`sha256:a3f1…`), not a display alias. Every cross-entity reference — a comment's task, a `reply-to`, a board's referenced labels — resolves through identity, and identity never moves.

## The alias is a stored constant

The human-facing `ATM-0142` is a **display alias**. Three properties define it, and together they make the entire collision problem evaporate:

> **1. It is stored, not derived.** The alias is a field on the creation event, written once at birth.
> **2. It is immutable.** No event ever changes an alias. It is not even an LWW register — it is a *constant*, so there is nothing to resolve, ever.
> **3. It need not be unique.**

**Stored, not derived, is what makes legacy ids work without a cutoff rule.** A *derived* alias (recompute the hash prefix on read) would compute `ATM-3e9d` for the task humans call `ATM-0106`, forcing an exception like *"ids below ATM-0130 are legacy"* — a brittle, project-specific rule that would have to be threaded through every reader. Stored instead, **reading is uniform**: every task simply *has* an alias, and no reader can tell an old one from a new one or has any reason to care.

"Legacy" exists only at **mint** time, and even there it is not a classification but a property of the data in hand:

- the alias **is already present** — the D6 upgrade copies it verbatim from v1's `subject.id`; or
- the alias **must be minted** — a hash prefix (below).

Two code paths that never meet. Neither asks *"is this old?"*.

**Minting rule.** For a task, the alias is `<PROJECT_CODE>-<prefix>`, where `prefix` is the first 6 lowercase hex characters of the creation event's SHA-256, extended by the minting replica to the shortest length ≥ 6 that is unambiguous among the aliases it currently holds. For a comment, it is `<task-alias>-c<prefix>`, with a 4-character minimum (a comment's prefix need only disambiguate within its task).

```
ATM-7f3a2b          a task
ATM-7f3a2b-c9b2e    a comment on it
```

Local extension is sound *because the alias is stored*: it does not need to be globally re-derivable, only globally *readable*. Two replicas concurrently minting the same prefix for different tasks is possible (~1 in 16.7M) and is **not an error** — see below.

## Aliases need not be unique

This invariant is dropped, and dropping it dissolves the last collision class in the model.

Identity is detached from the project code: the `ATM-` in an alias is **just characters in a display string**. It carries no semantics and nothing resolves through it. So two tasks may legitimately hold the string `ATM-0142` — most plausibly after merging two *different* projects that both used the code `ATM` and both minted that alias under v1 (use-case 2). Both tasks are intact, both identities are distinct, and nothing is lost. The clash exists **only at lookup**, and is resolved the way git resolves an ambiguous short hash:

```
$ atm task show ATM-0142
error: ambiguous alias 'ATM-0142' — 2 tasks match:
  ATM-0142  a3f19c…  "Fix cache"   (project ATM, merged 2026-05-02)
  ATM-0142  7b2e04…  "Add export"  (project ATM, merged 2026-05-02)
disambiguate with an identity prefix: atm task show a3f1
```

**Rejected: refuse the merge and require a rename.** It buys nothing. Renaming a project does not rewrite the stored, immutable aliases inside it — `ATM-0142` still reads `ATM-0142` — so the ambiguity survives the rename. Rewriting the aliases *would* clear it, at the cost of rotting every prose reference to them, which is precisely what this design exists to prevent.

Resolution order for a user-supplied string: exact alias match → unique identity prefix → ambiguity error listing candidates. Never silently pick one.

## Why not ascending IDs

Recorded because it is the design's most visible cost and will be questioned again.

Ascending aliases (`ATM-0142`) require a central authority to mint them. Subversion had `r142` and a server; **git gave up ordinal revision numbers, and that was the price of being distributed.** ATM is making the same trade for the same reason.

The concrete failure: two replicas of one project both mint `ATM-0142` for *different* tasks. Both tasks survive (their identities differ), but one must lose the name — and then prose that says *"blocked by ATM-0142"* resolves to the **wrong task**. Not dangling: *confidently wrong*. The fold cannot repair it, because comment bodies are opaque strings to the CRDT and rewriting user prose is both undefined and a violation of the log being the source of truth. Prose rot is inherent to *any* alias collision under *any* remap rule, so the only real fix is to make collision impossible by construction.

And the collision is **not rare**. Use-case 1 is one user, one project, several machines: filing one task on the laptop and one on the desktop before syncing is the *normal* path, not an edge case. An ascending scheme would demand human prose repair on nearly every merge.

What ordinality actually bought, checked against the code: **nothing**. `internal/store/log.go:399` sorts tasks by `ID < ID` — a lexicographic sort that equals creation order only by the accident of zero-padding. It is replaced by sorting on the **HLC creation stamp**, which is the *true* creation order and, unlike ID order, remains meaningful after a merge. `NextTaskN`/`NextCommentN` die under D3 regardless of scheme. Ordinality was a human affordance, never an invariant.

**Accepted cost:** a project that existed under v1 carries mixed alias formats forever (`ATM-0001`…`ATM-0130` legacy, `ATM-7f3a2b` onward). Visually inconsistent; completely safe. The alternative — re-deriving all aliases for uniformity — would break every commit message, doc, and comment that names a task, including this spec suite.

---

# L2 — Merge & Convergence

State is a pure function of the *set* of events a replica holds (D4). Two replicas holding the same set compute byte-identical state. Nothing below consults arrival order, `at`, or `seq`.

## Slots, and the one rule that governs all of them

Every mutable piece of state is a **slot**:

| Slot kind | Key | Written by |
|---|---|---|
| scalar field | `(entity, field)` | `*.title-changed`, `*.description-changed`, `*.body-changed`, `*.name-changed`, `label.upserted` (description, expression) |
| label membership | `(entity, label)` | `*.label-added`, `*.label-removed` |
| existence | `(entity)` | `*.removed`, `task.restored` |

> **The maximal-writer rule.** For a slot `s`, let `W(s)` be the events that write it. The **maximal writers** are those in `W(s)` that are not a causal ancestor of any other member of `W(s)`. A slot's value is determined *solely* by its maximal writers; every dominated write is superseded and ignored.

This single rule gives conflict detection for free:

> **A slot is *contested* iff it has more than one maximal writer** (they are pairwise concurrent, by definition of maximal). An entity is contested iff any of its slots is.

Contested-ness is thus a **pure function of the event set** — every replica derives it identically, with no stored state, no marker events, and no coordination. It is the substrate for the conflict doctrine below.

## Resolving a slot

Among the maximal writers:

| Slot kind | Rule | Bias |
|---|---|---|
| **scalar field** | highest HLC wins | *(none possible — see below)* |
| **label membership** | if any maximal writer is an `add`, the label is **present**; else absent | **add-wins** |
| **existence** | if any maximal writer is `restored`, the entity is **live**; else if any is `removed`, it is **tombstoned**; else live | **restore-wins** |

**The unifying principle:** *where a slot admits a "keep" outcome and a "drop" outcome, keep wins.* Add-wins and restore-wins are the same rule applied at two levels. A scalar field is the sole exception, and only because both outcomes are *content* — two different titles cannot both be kept, so the HLC decides and the contested board reports it.

Add-wins costs nothing to implement. The OR-Set that D4 mandates needs a unique tag per add and the set of tags each remove observed — and **the hash-DAG already supplies both**. An add's tag *is* its event id; what a remove observed *is* its set of causal ancestors. Concretely:

> Label `L` is present on entity `T` iff some `add(T,L)` event is **not** a causal ancestor of any `remove(T,L)` event.

No tag storage, no tombstone accumulation, no GC. The OR-Set is a *read* of the DAG.

*(Correcting the record: D4 justifies the OR-Set as resolving concurrent add/remove "deterministically rather than by arrival luck." That reasoning does not distinguish it from LWW, which is equally deterministic and equally not arrival-luck. The real difference is **who wins**, and the choice is made here on purpose: a concurrent removal must not silently discard a label someone just applied. The conclusion D4 reached stands; its stated reason did not.)*

## Action table

| Action | Semantics |
|---|---|
| `project.created`, `task.created`, `comment.created` | Immutable birth event; establishes identity and the stored alias. Deduplication is free — the same event has the same hash. Labels supplied at creation **desugar into `label-added` writes tagged by the creation event's id**, so creation needs no special case in the OR-Set. |
| `project.name-changed`, `task.title-changed`, `task.description-changed`, `comment.body-changed` | Scalar slot; highest HLC among maximal writers. |
| `label.upserted` | Scalar slots for the label's `description` and `expression`, resolved independently. |
| `task.label-added` / `-removed`, `comment.label-added` / `-removed` | Membership slot; add-wins. |
| `project.removed`, `task.removed`, `comment.removed`, `label.removed` | Existence slot; tombstone. A **concurrent** edit cannot resurrect (D4). |
| **`task.restored`** *(new)* | Existence slot; resurrection. See below. |
| `task.meta-changed` | **Retired.** It existed only to bump `NextCommentN` (`internal/store/comment.go:53,84`), which no longer exists. v1 instances ride through the D6 upgrade and are inert — preserved, never dropped (D5). |

## `task.restored` (new action)

D4 says a tombstone wins over a concurrent edit. It does *not* say deletion is irreversible — and irreversible is intolerable here.

The scenario is real and destructive: replica A deletes a task; replica B, unaware, writes three comments and a decision on it. On merge the tombstone wins and **all of B's work goes inert** — present in the log, unreachable in the UI. Without a way back, the only recovery is recreating the task by hand, which mints a *new identity* and rots every reference to the old one.

`task.restored` is an ordinary event that writes the existence slot. Because a restore is authored *after* observing the delete, it is **causally later** — so it strictly dominates the tombstone and D4's rule ("a *concurrent* edit cannot resurrect") is untouched. The task returns **on its original identity**, so its alias, its references, and B's inert comments all become live again. Nothing is recreated.

Concurrent `removed` + `restored` (reachable only via a double-delete) resolves **restore-wins**, per the unifying principle, and lands on the contested board.

**Only `task.restored` is added in v2.** Comments, labels, and projects get no restore verb, deliberately: the destructive case that motivates this is *a task deleted out from under someone's work*, and a task is the only entity that owns other entities' work. If the same need arises for another kind, the shape is already defined here — an event writing the existence slot, causally later than the tombstone — and adding it is a vocabulary change, not a model change.

## The conflict doctrine: converge always, surface contested slots

> **The fold never blocks, never prompts, and never waits for a human.**

This is not a preference; it is forced by D4. If merge paused for a human, a replica that had resolved and a replica that had not would hold *identical event sets* but compute *different state* — strong eventual consistency would be gone, and with it the ability to sync onward. **A gated merge is not a merge.**

But converging silently is not enough either. When a scalar slot is contested, the HLC picks a winner and a human's edit is *shelved* — and work vanishing quietly is precisely the failure ATM exists to prevent. So:

1. **The fold converges deterministically.** Nothing blocks.
2. **The losing write is never destroyed.** It remains in the log and in the DAG. LWW selects a *current value*; it does not erase history.
3. **Contested slots are surfaced**, via the derived predicate above. Because contested-ness is a pure function of the event set, this is a **computed label — a board** (ATM-0115), not a new mechanism.
4. **A manager resolves it with an ordinary write** whose `parents` include both contested events. That write is now a descendant of both, so it becomes the **unique maximal writer** of the slot — the slot is no longer contested, and **board membership evaporates on its own.** No `resolved` marker, no state to clean up. The DAG records the resolution. This is exactly git's merge-commit shape.

The resolution happens **once**, on whichever replica the human is at. It is *just more events*, and it syncs like any others; the other replica does not redo the work, it *receives the answer*. Both sides then derive "no longer contested" from the same event set, and the board clears everywhere.

This gives an ATM-native answer to a question most CRDT systems decline to answer: *where do the writes we threw away go?* **Onto a board, for a human to review.**

Out of scope here, deliberately: the board's label vocabulary and its CLI surface. That is a capability, and it belongs in a feature task following the ATM-0085 pattern (a command that ensures its own labels and board idempotently, exposing intent-level verbs so prompts never hardcode label strings). L2's job is only to define contested-ness as a derived predicate — which it has.

## Computed labels (boards)

Inherited from D4, as amended (ATM-0105-c0004). Both bindings are mandatory.

**Computed membership is derived, never stored, and sits outside the OR-Set.** A label is *computed* iff its `expression` slot resolves non-empty. For a computed label, **all membership slots referencing it are inert** — the resolver ignores them and derives membership by evaluating the expression instead. A concurrent *"make label computed"* (replica A) and *"assign that label to a task"* (replica B) therefore resolves as **computed-ness wins**: B's assignment survives in the OR-Set but has no effect. Without this rule the two replicas disagree on whether the label is asserted or derived, and D4's byte-identical-state guarantee fails.

**Merge can create a reference cycle that no replica ever wrote.** Boards may reference boards. Replica A points board `a` at `b` while replica B points `b` at `a`. Both writes are individually valid; neither replica ever held a cycle; the order-independent fold produces one. **Write-time cycle rejection is necessary but not sufficient.**

> **General rule — the reference guard.** Any resolver that follows a reference between entities MUST carry a visited set and MUST surface a cycle as a **broken** entity — never hang, never return empty. This is stated as a general rule, not a board-specific patch: *any* field holding an inter-entity reference inherits this hazard under D4, and future fields must inherit the guard with it.

A cyclic board resolves to `broken`; it is reported, not silently empty. A silently-empty board is indistinguishable from a board that legitimately matches nothing, which would hide the corruption.

## Retention

**No tombstone or event GC in v2.** The log is the source of truth, ATM logs are small, and every GC scheme trades away either history or determinism. Revisit only if real logs stop being small.

---

# D6 — The v1 → v2 upgrade

An existing `log.jsonl` is v1 and is upgraded by a one-time local replay. Per the D6 correction (ATM-0105-c0005):

> **The upgrade MUST be a pure function of the v1 log.** No local or replica-specific input may enter the canonical bytes of an upgraded event.

A store is portable by directory copy, so the same project is routinely upgraded on more than one machine — the laptop/desktop case *is* use-case 1. If the local replica id fed the hash, the same historical event would hash to a **different id on each machine**; on the first sync every event in the shared history would appear twice, and since a task *is* its creation event, **every task would fork in two.** The entire ledger would duplicate. This is the expected path, not a corner case.

For each v1 entry, in `seq` order:

| v2 field | Derived from |
|---|---|
| `replica` | the reserved constant **`_v1`** — identical on every machine |
| `hlc` | `(p = at in ms, l = seq)` — both already in the v1 log |
| `parents` | `[id of the previous upgraded entry]`; `[]` for the first |
| `subject.id` | the identity of the referenced entity's upgraded creation event |
| alias | **copied verbatim** from v1's `subject.id` (`"ATM-0106"`) |
| everything else | carried across unchanged |

`at` and `seq` are the only ordering information v1 ever recorded, so they are the only honest source for the upgraded HLC. Using `seq` as the logical counter also guarantees a strict order even where `at` values collide.

Every input is byte-identical on every copy of the log, so **any two machines upgrading the same log produce byte-identical v2 DAGs** and converge trivially. A single-node linear history is a degenerate DAG; the upgrade is mechanical and lossless.

**A v1 node cannot sync.** It has no hashes, no HLC, and no DAG, so it cannot participate at all. **Upgrading is a precondition for distribution, not a compatibility mode.** After the upgrade the log simply *is* v2 — there is no dual-format reader in the steady state, and all v1 knowledge lives in the one-time migration path and nowhere else. (D5's version negotiation governs v2 and beyond; it was never about v1.)

**The generalizable lesson**, worth carrying into every later layer: *any derivation that must agree across replicas may consume only bytes that are themselves shared.* Reaching for local state — a replica id, a wall clock, filesystem ordering — inside a hash is the failure mode.

---

# Implementation refinements (ATM-0106 implementation phase)

The reference implementation is `internal/eventsource` (see `docs/superpowers/specs/2026-07-14-eventsource-core-v2-design.md` for the full list of implementation decisions). Two D6 refinements and two L2 clarifications discovered during implementation are recorded here so the spec stays honest:

- **Creation events carry no `subject.id`** — an event cannot contain its own hash. The v1 alias moves from `subject.id` to `payload.alias` during the upgrade; `subject.id` on every non-creation event holds the entity identity as specified.
- **The D6 "carried across unchanged" row is refined**: the upgrade also (a) synthesizes identity references `task_ref`/`reply_to_ref` on `comment.created` from the v1 alias references, which stay in the payload verbatim; (b) synthesizes per-slot membership deltas (`payload.label`) on `*.label-added/-removed` from consecutive v1 snapshots — a pure function of the log, so D6's purity rule is untouched; (c) materializes absent `description`/`expr` keys on `label.upserted` as `""`, converting v1's replace-the-record semantics into v2's write-present-keys slot semantics without changing any outcome.
- **`label.upserted` writes the label's existence slot as "live"** — remove-then-reupsert resurrects a label, matching v1 semantics; concurrent upsert‖remove resolves live (keep beats drop).
- **The HLC total order carries a defensive fourth key** (the event id): two different projects upgraded under `_v1` can collide on `(p, l, replica)` after a cross-project merge; the id keeps the fold deterministic there.
- **JSON `null` is an absent list, not a list containing the empty string.** The v1 log writes `"labels": null` for any entity created with no labels (a nil Go slice marshals to `null`), and `encoding/json` decodes `null` into a `string` without error. A payload accessor that decodes a scalar before a list therefore yields `[""]` — a phantom empty-string label on every such entity. The reference implementation treats a `null` payload value as an absent list. This was caught by the equivalence capstone and by nothing else: every unit test asserts the model against itself, while the capstone asserts it against a real v1 log.

---

# What this spec does not decide

- **On-disk layout** of the DAG, and how cross-project merge is represented — L3.
- **Replica-copy detection** (re-minting a replica id after a directory copy). The invariant is stated in L0; the mechanism is L3's.
- **Cross-project alias ambiguity** beyond the lookup rule above. Merging two projects that share a code is an explicit human-initiated operation — L3/L4.
- **The contested board's vocabulary and CLI** — a feature task under the ATM-0085 capability pattern.
- **Wire protocol** for have/want reconciliation — L4.
- **Signing, trust, authorization** — L5. Note that content-addressing (D1) already gives tamper-evidence for free; L5 adds *attribution*.
- **Version negotiation handshake** — X.

# Summary of decisions

| # | Decision |
|---|---|
| **L0-1** | Event id = `sha256:` + SHA-256 of RFC 8785 canonical JSON, `id` field excluded. |
| **L0-2** | Unknown-field preservation is an *integrity* requirement — dropping one destroys the ability to recompute the hash. |
| **L0-3** | A replica is a store copy on a machine, id `r_` + base32(128 bits); `_v1` reserved. Orthogonal to `actor`. |
| **L0-4** | HLC `(p, l, r)` compared lexicographically gives a deterministic total order. It is a tiebreak only; `parents` alone establishes causality. `at` never orders anything. |
| **L1-1** | A task *is* its `task.created` event. Identity comes free from D1 — no ULID, no allocator. |
| **L1-2** | The alias is **stored, immutable, and need not be unique**. This is what lets legacy ids survive with no cutoff rule. |
| **L1-3** | Ascending IDs are abandoned (the git-not-Subversion trade). Ordinality was a human affordance; display order comes from the HLC creation stamp. |
| **L1-4** | Ambiguous aliases resolve git-style at lookup; never silently pick one. D3's alias-reconciliation machinery is deleted. |
| **L2-1** | The **maximal-writer rule** governs every slot; *contested* = more than one maximal writer, a pure function of the event set. |
| **L2-2** | **Keep beats drop**: add-wins for membership, restore-wins for existence. Scalar fields tiebreak on HLC because both outcomes are content. |
| **L2-3** | The OR-Set is free: tags are event ids, observation is causal ancestry. No tag storage, no GC. |
| **L2-4** | **`task.restored`** is added; deletion must not be irreversible. `task.meta-changed` is retired. |
| **L2-5** | **Converge always; surface contested slots on a board.** The fold never blocks — a gated merge would break D4. Resolution is an ordinary write parented on both sides, and board membership evaporates on its own. |
| **L2-6** | Computed-ness beats a stored assignment; a **general visited-set reference guard** surfaces cycles as broken. |
| **L2-7** | No GC in v2. |
| **D6** | The upgrade is a **pure function of the v1 log** (`_v1` replica, HLC from `at`+`seq`). A v1 node cannot sync; upgrading is a precondition for distribution. |
