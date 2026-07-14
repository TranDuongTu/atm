# Eventsource Core v2 — Implementation Design (L0–L2 + D6)

**Status:** Proposed
**Tracking:** ATM-0106 (Design: L0-L2 core data model & convergence)
**Source spec:** `docs/eventsource/01-core-data-model.md` — the model is decided there; this document decides how it lands in this codebase.
**Plan:** `docs/superpowers/plans/2026-07-14-eventsource-core-v2.md`

## Scope: a pure library, not a store rewrite

This milestone implements L0–L2 and the D6 upgrade as a new, self-contained package **`internal/eventsource`**. It is a library of pure functions plus one clock: canonicalization, event identity, HLC, replica ids, the hash-DAG, alias minting/resolution, the fold, and the v1→v2 upgrade. It imports nothing from `internal/store` (the capstone test imports both, which is allowed for `_test` files).

**The live store is not rewired in this milestone, deliberately.** The store switchover needs L3's on-disk layout (ATM-0107, still an open design task: DAG file layout, replica-copy detection mechanism, cross-project merge representation). Wiring v2 into `internal/store` before L3 would gamble the working system on layout decisions that L3 may reverse, and delivers no user value until sync (L4) exists. What this milestone delivers instead is the entire *semantic core*, proven by tests — including the end-to-end proof that `fold(upgrade(v1 log))` reproduces today's `Replay` state — so that L3/L4 integration becomes plumbing, not modeling.

## Package layout

```
internal/eventsource/
  canon.go      Canonicalize — RFC 8785 (JCS) canonical bytes
  hlc.go        HLC stamp, Compare, Clock (Tick/Observe)
  event.go      Subject, Event, Parse, payload accessors, CompareEvents
  replica.go    MintReplicaID, ReplicaV1
  action.go     v2 action constants (task.restored in, task.meta-changed out)
  author.go     Draft, NewEvent — the single authoring path
  dag.go        BuildDAG, Reaches, Concurrent, Frontier
  fold.go       Fold: slots, maximal writers, resolution, State, ContestedSlot
  alias.go      MintTaskAlias, MintCommentAlias, State.Resolve
  upgrade.go    UpgradeV1 — the D6 pure function
  testdata/     golden v1 fixture log + golden upgraded v2 output
```

One new dependency: **`github.com/gowebpki/jcs`** (RFC 8785 reference implementation for Go). Event identity is the SHA-256 of canonical bytes; hand-rolling ES6 number serialization is exactly where a subtle bug silently destroys identities, and the library is small, stable, and single-purpose. Every hash in the system flows through the one `Canonicalize` wrapper.

## Core representation: raw bytes are the event

`Event.Raw` holds the canonical JCS bytes and is the source of truth; the struct fields (`Parents`, `HLC`, `Action`, …) are a **read-only decoded view** and are never re-encoded. This makes L0-2 (unknown-field preservation as an integrity requirement) true *by construction*: an event from a newer minor version is stored, hashed, and forwarded as its bytes, so nothing can be dropped. `Parse` accepts non-canonical JSON from the wire, canonicalizes, hashes, and decodes; `NewEvent` (authoring) marshals a draft and funnels through `Parse` so there is exactly one identity code path.

## Decisions the model spec left open

These are implementation-level decisions, each consistent with (and required by) the decided model. The ones marked **[D6-Δ]** refine the D6 table's "everything else carried across unchanged" row; the design doc gets a short amendment note pointing here (final plan task).

1. **Creation events carry no `subject.id`.** A task *is* its creation event, and an event cannot contain its own hash. So `task.created`/`comment.created`/`project.created` have a subject of just `{kind}` (plus `code` for projects, kept for readability); every *other* event's `subject.id` holds the entity's identity. **[D6-Δ]** The upgrade deletes the v1 alias from the creation event's `subject.id` and moves it to `payload.alias`.
2. **The stored alias lives at `payload.alias` on the creation event.** For projects the alias is the project code. v2-authored creation payloads are deltas: `{alias, title, description?, labels?}` for tasks, `{alias, task_ref, reply_to_ref?, body, labels?}` for comments, `{alias, name}` for projects.
3. **Cross-entity references in payloads are identities under the keys `task_ref` / `reply_to_ref`.** **[D6-Δ]** The upgrade synthesizes them on `comment.created` from the v1 `task_id`/`reply_to` aliases (which stay in the payload verbatim). Label references (in expressions) stay names — a label's name *is* its identity within the project, so label subjects keep `{kind, name}` and never get an `id`.
4. **[D6-Δ] Membership deltas are synthesized.** A v1 `*.label-added`/`-removed` payload is a whole-entity snapshot that never names the changed label, but the v2 fold needs per-slot deltas. The upgrade tracks each entity's label list through the linear v1 history and adds the diff to the payload under `label` (a string; an array in the never-observed multi-label case). This is a pure function of the log bytes, so D6's purity rule is untouched. The fold reads **only** `label` on membership actions — never the snapshot's `labels` key, which on a `-removed` event lists the *remaining* labels and would corrupt the delta.
5. **[D6-Δ] `label.upserted` gets explicit empty fields.** v1 replay *replaces* the whole label record (an absent `description`/`expr` key means `""`), while the v2 slot rule is "an upsert writes the scalar slots whose keys are present" (that per-key independence is what lets concurrent description-edit and expr-edit both win). The upgrade reconciles the two by materializing absent `description`/`expr` keys as `""` — v1 semantics are reproduced exactly, and v2-authored upserts write only the keys they mean.
6. **`label.upserted` also writes the label's existence slot as "live".** The model's slot table lists only `*.removed`/`task.restored` on existence, but v1 semantics (and plain sense) require remove-then-reupsert to resurrect a label. An upsert observed the tombstone, so it causally dominates it; concurrent upsert‖remove resolves live, which is the keep-beats-drop principle applied consistently.
7. **Creation events write their scalar slots** (title/description/body/name from the creation payload). Any well-formed edit of an entity is a causal descendant of its creation (you cannot name an entity's identity without holding its creation event), so creation values are dominated by any later edit — initial values fall out with zero special cases.
8. **Unknown actions are inert but causal.** Any action the fold doesn't recognize — including the retired `task.meta-changed` riding through the upgrade — writes no slots but fully participates in the DAG, frontier, and causality. D5 preservation needs no special case.
9. **Contested is structural, reported with enough context to filter.** `ContestedSlot` reports every slot with >1 maximal writer (the model's exact definition), with writers sorted by the HLC total order (scalar winner = last). Same-outcome noise (two concurrent adds of the same label) is *not* suppressed here — filtering is vocabulary policy and belongs to the contested-board capability task. Membership slots of computed labels are the one exception: they are inert (L2-6), so they are excluded.
10. **Dangling writes are inert, not errors.** An event whose `subject.id` names an entity with no creation event in the set writes slots that no entity materializes (entities materialize only from creation events; labels from their first `label.upserted`). Deterministic, and forward-compatible with partial sets arriving via future sync.
11. **The HLC total order gets a defensive fourth key.** `CompareEvents` orders by `(hlc.p, hlc.l, replica)` and finally by event id. The triple is total for live replicas, but two *different* projects both upgraded under `_v1` can collide on the triple after a cross-project merge (L3's use-case 2); the id tiebreak keeps the fold deterministic there instead of silently order-dependent.
12. **`parents` is sorted lexicographically and deduplicated** at authoring time — array order is not significant, so it must not be able to fork identities.
13. **Upgrade errors are loud.** `UpgradeV1` fails on any malformed line, dangling alias reference, or duplicate creation — the upgrade is lossless or it does not happen. (Today's `ReadLog` tolerates a malformed tail; the one-time upgrade must not.)
14. **`Fold` input contract:** `BuildDAG` deduplicates by id, rejects missing parents and cycles, and fixes a deterministic topological order (Kahn's algorithm, ready set ordered by `CompareEvents`). Reachability is ancestor bitsets — O(1) `Reaches` after O(V·E/64) construction; fine for ATM-scale logs, no premature cleverness.

## Public API (what the plan builds)

```go
// canon.go
func Canonicalize(raw []byte) ([]byte, error)

// hlc.go
type HLC struct{ P, L int64 }               // json: {"p":…,"l":…}
func (a HLC) Compare(b HLC) int
type Clock struct{ /* mu, now, last */ }
func NewClock(now func() int64) *Clock       // nil now → wall clock (ms)
func (c *Clock) Tick() HLC                   // authoring
func (c *Clock) Observe(h HLC)               // receiving

// event.go
type Subject struct{ Kind, ID, Name, Code string }   // all but Kind omitempty
type Event struct {
    ID  string   // "sha256:"+hex of Raw — derived, never serialized
    Raw []byte   // canonical JCS bytes — the source of truth
    V int; Parents []string; HLC HLC; Replica string
    At time.Time; Actor, Action string; Subject Subject
    Payload json.RawMessage
}
func Parse(line []byte) (*Event, error)
func (e *Event) PayloadString(key string) (string, bool)
func (e *Event) PayloadStringOrList(key string) []string
func CompareEvents(a, b *Event) int          // (p, l, replica, id)

// replica.go
const ReplicaV1 = "_v1"
func MintReplicaID(r io.Reader) (string, error)  // "r_" + 26 Crockford base32 chars

// author.go
type Draft struct{ At time.Time; Actor, Action string; Subject Subject; Payload map[string]any }
func NewEvent(clock *Clock, replica string, parents []string, d Draft) (*Event, error)

// dag.go
func BuildDAG(events []*Event) (*DAG, error)
func (d *DAG) Reaches(anc, desc string) bool     // strict happens-before
func (d *DAG) Concurrent(a, b string) bool
func (d *DAG) Frontier() []string
func (d *DAG) Events() []*Event                  // deterministic topo order
func (d *DAG) Get(id string) *Event

// fold.go
func Fold(d *DAG) *State
func FoldEvents(events []*Event) (*State, error)
type State struct {
    Projects map[string]*ProjectState  // by identity
    Tasks    map[string]*TaskState     // by identity
    Comments map[string]*CommentState  // by identity
    Labels   map[string]*LabelState    // by name
    Contested []ContestedSlot
    Frontier  []string
}
type EntityMeta struct{ ID, Alias string; Tombstoned bool; CreatedAt time.Time; CreatedBy string; CreatedHLC HLC; CreatedReplica string; UpdatedAt time.Time; UpdatedBy string }
type ContestedSlot struct{ Entity, Kind, Field string; Writers []string }
func (s *State) TasksByCreation() []*TaskState   // display order = HLC creation stamp (L1-3)
func (s *State) CommentsByCreation(taskRef string) []*CommentState

// alias.go
func MintTaskAlias(projectCode, eventID string, taken func(string) bool) string     // ≥6 hex chars
func MintCommentAlias(taskAlias, eventID string, taken func(string) bool) string    // ≥4 hex chars
func (s *State) Resolve(input string) (Match, error)  // exact alias → identity prefix → *AmbiguousError / ErrNoMatch

// upgrade.go
type UpgradeResult struct{ Events []*Event; IdentityByAlias map[string]string }
func UpgradeV1(logData []byte) (*UpgradeResult, error)
```

## Test strategy

- **Known-answer canonicalization:** the RFC 8785 example object (numbers, unicode escapes, literals) plus key-sorting cases pin the dependency's behavior.
- **Identity stability:** parsing the same event reformatted (whitespace, key order, an unknown `future_field`) yields the same id; the golden upgrade file pins concrete hashes so any drift in canonical bytes fails loudly.
- **Per-rule fold tests:** each L2 scenario from the model spec gets a direct test — concurrent title LWW + contested, add-wins vs concurrent remove, tombstone-beats-concurrent-edit, restore-after-delete revives on the original identity, concurrent remove+restore resolves restore-wins and is contested, computed-ness beats a concurrent assignment, resolution write parented on both contested events clears the board.
- **Order-independence property:** folding seeded random permutations (with injected duplicates) of an event set yields deep-equal state — D4's strong eventual consistency, tested directly.
- **D6 golden + purity:** a handcrafted v1 fixture log upgrades to a committed golden v2 file (regenerate with `-update`); byte-identical output on repeated runs; `UpgradeV1` takes no clock and no replica — purity by construction.
- **Equivalence capstone:** drive a real `store.Store` through project/task/comment/label mutations, then assert `FoldEvents(UpgradeV1(log.jsonl).Events)` reproduces `store.Replay` semantic state (titles, descriptions, bodies, labels, existence, aliases, references) exactly. Timestamps and retired counters (`NextTaskN`, `NextCommentN`) are out of comparison scope.

## The reference guard (L2-6) needs no new code here

The fold never follows inter-entity references — it derives slots, and computed-label membership is simply inert. Expression evaluation (where a merge-created board cycle would bite) stays in the query-path resolver, and `internal/store/resolve.go` already carries the mandated visited-set guard (`ErrCyclicExpr`). When L3 wires v2 state into the resolver, that guard inherits unchanged; surfacing a cyclic board as `broken` rather than an error is part of the contested-board capability task.

## Out of scope (unchanged from the model spec)

On-disk DAG layout, replica-copy detection mechanism, and store integration (L3 / ATM-0107); sync protocol (L4); trust (L5); the contested board's vocabulary and CLI (feature task under the ATM-0085 pattern); version negotiation (X).
