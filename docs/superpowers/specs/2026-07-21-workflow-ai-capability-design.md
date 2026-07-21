# workflow_ai capability: stage vocabulary, links, plan tracking, boards

**Task:** ATM-efebc0 · **Initiative:** ATM-4dd440 (capability extension points) · **Status:** Draft 2026-07-21

**Depends on:** ATM-2e64a5 (task metadata column + capability view hook). This spec is written against that task's committed contract (spec `docs/superpowers/specs/2026-07-21-task-metadata-column-design.md`): `core.Task.Meta map[string]string`, `Store.SetTaskCapabilityMeta(taskID, capability, payload, actor)` (blob-replace, owning capability only), `Cell{Text, Tone}` with `Annotate(task core.Task) *Cell` as a pure-read interface method, and `Registry.Annotate`. **Implementation of this spec is blocked until ATM-2e64a5 lands; the implementation plan gets a review pass at that point to catch contract drift.**

## Purpose

workflow_ai is the pilot capability of the extension-points initiative: an AI-native brainstorm→clarify→plan→implement cycle imposed as capability vocabulary, coexisting with the existing `workflow` capability as a fully independent view. It proves that a capability can own a stage discipline, machine-readable task links, and plan tracking in its own metadata key — without contaminating the substrate and without any interplay contract with other capabilities.

Doctrine honored throughout (docs/architecture/label-substrate-and-capabilities.md): labels carry only what boards select; machine state lives in the capability's metadata key; boards never query payloads; views live with the owner; the store enforces nothing — every invariant here is a paved road maintained by the capability's verbs, not a fence.

## 1. Identity and vocabulary

New package `internal/capability/workflowai`, mirroring `internal/capability/workflow`'s structure (`vocabulary.go`, `recorder.go`, `reporter.go`, `annotate.go`, `guide.md` + `guide.go`, `command.go`), registered in the capability registry after `workflow` and `contextmap`. `Name()` returns `workflow_ai`; the metadata key and the command mount both use that string.

The capability owns two label namespaces per project `<code>`, both seeded idempotently by `EnsureVocabulary` from a single vocabulary literal (the workflow pattern: Vocabulary, Exposed, and EnsureVocabulary all derive from one list):

- `<code>:stage:*` — namespace descriptor plus five stored labels: `stage:brainstormed`, `stage:clarified`, `stage:planned`, `stage:implementable`, `stage:done`. Invariant (verb-maintained): a task carries at most one `stage:*` label. **Absence of any `stage:*` label means "new"** — new is a derived state, not a label.
- `<code>:wfai:*` — namespace descriptor plus one stored marker label `wfai:revision`, stamped and removed exclusively by the link verbs. It exists so the revisions board can select revision follow-ups; the link topology itself lives in metadata.

Capability independence: workflow_ai never reads or writes `status:*`, `priority:*`, `context:*`, or any other capability's labels or metadata key. A task may simultaneously carry `status:open` (workflow's view) and `stage:planned` (workflow_ai's view); neither capability sees the other.

## 2. Boards

Five boards, seeded with the vocabulary, selecting labels only:

| Board | Expr | Meaning |
|---|---|---|
| `<code>:new-tasks` | `NOT stage:*` | not yet brainstormed (includes naked jottings) |
| `<code>:brainstormed-tasks` | `stage:brainstormed OR stage:clarified` | in refinement |
| `<code>:planned-tasks` | `stage:planned OR stage:implementable` | has a recorded plan |
| `<code>:revisions` | `wfai:revision AND NOT stage:done` | open follow-ups of a bigger planned task |
| `<code>:done-tasks` | `stage:done` | completed through the cycle |

Exprs are written project-relative here for readability; seeded exprs use the project-qualified label names, same as workflow's boards. `Exposed` ring order: the five boards in the table's order, then the `stage:*` and `wfai:*` namespace descriptors.

## 3. Metadata payload — the single machine-state key

All workflow_ai machine state lives under `Meta["workflow_ai"]` as one versioned JSON object, written blob-replace via `SetTaskCapabilityMeta` with a read-modify-write inside each verb:

```json
{
  "v": 1,
  "plan": {"kind": "file", "ref": "docs/superpowers/plans/2026-07-21-x.md", "recorded_at": "2026-07-21T04:00:00Z", "actor": "developer@claude:unset"},
  "revision_of": "ATM-aaaaaa",
  "relates_to": ["ATM-bbbbbb"],
  "demoted": {"at": "2026-07-21T05:00:00Z", "by": "developer@claude:unset", "reason": "plan file deleted in repo cleanup"}
}
```

- `plan.kind` is one of `file` (repo-relative path), `commit` (git revision), `ephemeral` (free-form pointer to a conversation/session — unverifiable by construction). `recorded_at`/`actor` capture who recorded the plan and when.
- `revision_of` holds at most one parent task ID; `relates_to` is a deduplicated list. Links are stored on the child/source side only; inbound views are computed by scanning (see reporters).
- `demoted` is a breadcrumb of the most recent demotion; it is overwritten by the next demotion, and the full history lives in the event log and the task's comment thread.
- Absent fields mean "nothing recorded". A payload whose fields are all absent is deleted outright (the key is written as empty) rather than left as `{"v":1}` noise — presence of the key should mean presence of state.

Payload conventions per the metadata-column spec: the capability reads only its own key; the version field is embedded; **unknown fields survive rewrite** (each verb decodes to a generic map, mutates only the fields it owns, re-encodes) so an older binary never destroys a newer binary's state — degrade-never-reject applied to ourselves. Payloads stay small: locators and IDs, never content.

## 4. Verbs

All verbs mount flat under `atm capability workflow_ai <verb>`, built over the capability `Env` exactly as workflow's `command.go` does, and honor the global `--output json|text` convention. Recorders take `--task`; every mutation requires the actor from the environment like every other ATM mutation.

### 4.1 Stage recorders — the guarded ladder

The ladder is `new → brainstormed → clarified → planned → implementable → done`, climbed one rung at a time. Each verb: reads the task, evaluates the guard against the task's current `stage:*` labels, then performs the stage swap using workflow's proven pattern — add the target label first, then remove every other `stage:*` label (no transactions; add-first bounds the worst case to a recoverable extra label, and re-running converges). Guard evaluation on a hand-edited task carrying several stage labels: if the required predecessor is among them, the verb proceeds and the swap self-heals the invariant; if the task already carries exactly the target, the verb is an idempotent no-op with zero store calls; otherwise the verb fails, naming the current stage label(s) and the required predecessor.

- `brainstorm --task X` → `stage:brainstormed`. Guard: X is new (no `stage:*` label).
- `clarify --task X` → `stage:clarified`. Guard: brainstormed.
- `plan --task X --kind file|commit|ephemeral --ref <ref>` → `stage:planned`. Guard: clarified (transition), **or** planned/implementable (re-plan: updates the plan record in place, stage unchanged — for a moved plan file or a re-planning pass). Recording the plan locator IS the transition: the payload write and the label swap happen in the same verb, payload first (a planned task must never lack a plan record; a leftover plan record on a still-clarified task is the recoverable direction).
- `ready --task X` → `stage:implementable`. Guard: planned, and a plan record exists in the payload. Marks the task as sized for one implementation session — the split/review step happens before this verb.
- `done --task X` → `stage:done`. Guard: implementable.
- `demote --task X --reason "<why>"` → back to new, from any stage. Removes the `stage:*` label(s), clears `plan` from the payload, writes the `demoted` breadcrumb, and appends the reason as a task comment (audit trail). `--reason` is required. Links (`revision_of`, `relates_to`) and the `wfai:revision` marker survive — topology is true regardless of stage. Demoting an already-new task is a no-op that still self-heals a leftover plan record if one exists.

There is no `implement` verb: implementation is the dev session itself, gated by prompt guidance (§6) — an agent asked to implement checks `stage:implementable` and refuses otherwise.

### 4.2 Link recorders

- `link --task X --revision-of Y` — sets `revision_of: Y` in X's payload and stamps `<code>:wfai:revision` on X. Guards: Y exists in the same project; X ≠ Y; X has no different parent already (unlink first); Y's own payload does not name X as ITS parent (direct two-node cycle). Deeper cycles are not walked (non-goal; the `links` reporter makes them visible).
- `link --task X --relates-to Y` — appends Y to X's `relates_to`, deduplicated. Guards: Y exists in the same project; X ≠ Y. `relates_to` is intentionally semantics-free: stored one-directional on X, surfaced bidirectionally by the reporter.
- `unlink --task X --revision-of Y` — clears `revision_of` (Y must match the stored parent) and removes the `wfai:revision` marker label.
- `unlink --task X --relates-to Y` — removes Y from `relates_to`.

Exactly one of `--revision-of`/`--relates-to` per invocation, for both `link` and `unlink`.

### 4.3 Reporters — pure, never mutate

- `report` — the plan-staleness check. Scope: every project task carrying `stage:planned` or `stage:implementable`. For each, verify the plan record: `file` → the path exists relative to the current working directory; `commit` → the revision resolves in the cwd's git repository (`git rev-parse --verify --quiet <ref>^{commit}`); `ephemeral` → always reported as unverifiable/at-risk; a planned-stage task with no plan record at all (hand-edited) → reported as broken. Output: one line per finding — task ID, stage, finding (e.g. `ATM-aaaa  planned  plan file missing: docs/…`); healthy tasks are summarized in a count, not listed. The reporter NEVER demotes: demotion is the decider's explicit `demote` call. Running `report` outside a repo/wrong cwd makes file/commit plans unverifiable — the report says so rather than calling them missing.
- `links --task X` — shows X's outbound links from its own payload (parent, related) and its inbound links (tasks naming X as `revision_of` or in `relates_to`), computed by listing the project's tasks and decoding workflow_ai payloads. O(n) over project tasks by design — no parent-side child lists, one writer per fact.

## 5. Annotate — the contextual column

`Annotate(task core.Task) *Cell`, pure over the task value (labels + own payload; no store, no filesystem — on-disk plan verification belongs to `report` only):

- No `stage:*` label → `nil` (empty cell; workflow_ai has nothing to say about tasks outside its cycle, even if they carry links).
- Text: the stage short name (`brainstormed`, `clarified`, `planned`, `implementable`, `done`); for planned/implementable, the plan kind is appended: `planned·file`, `implementable·ephemeral`. A planned/implementable task with no plan record renders `planned·no-plan`.
- Tone: `ToneAttention` when the plan is ephemeral or missing at planned/implementable stages (plan at risk beats everything); else `ToneOK` for `implementable`; else `ToneNeutral`. `ToneStale` is reserved — without filesystem access, Annotate cannot distinguish stale from healthy; that distinction lives in `report`.

Malformed payload (unparseable JSON): Annotate degrades to the label-only cell (stage name, ToneNeutral) — never errors, never renders raw payload.

## 6. Guide and prompt guidance

`guide.md` served verbatim by the uniform `guide` subcommand, with `## Brief` and `## Autopilot` sections (spec §7 of the capability contract). Its operating doctrine, which is the initiative's prompt-guidance deliverable:

- **Never implement a task whose stage is not `implementable`.** An agent asked to implement first checks the stage and raises instead of proceeding. The ladder is the paved road: brainstorm before clarify, clarify before plan, plan before ready.
- **A task is sized to one plan a framework like superpowers can execute in a single session.** When a planned task is bigger than that, split it: create linked follow-up tasks (`link --revision-of` the parent), each entering the cycle at its own stage. The revisions board is the queue of follow-ups still needing refinement.
- **Plans may be ephemeral** (a conversation, not a committed doc). Record them as `kind=ephemeral` honestly. Run `report` at session start; when a plan cannot be located or recovered, the decider demotes the task to new with a reason — replanning is cheaper than implementing against a ghost.
- Autopilot: the mechanical loop — `report` → demote the unrecoverable → advance tasks whose next rung's evidence exists → never skip rungs.

## 7. Testing

Mirror workflow's test layout, table-driven throughout:

- **vocabulary_test**: Vocabulary/Exposed/EnsureVocabulary derive from the single literal (contract parity with workflow); board exprs parse; Exposed ⊆ Vocabulary; seeding is idempotent.
- **recorder_test**: every ladder rung's happy path; every wrong-rung guard failure with its message; idempotent no-op re-runs (zero store calls); self-healing swap on a hand-edited multi-stage task; `plan` transition vs re-plan; `ready` without a plan record fails; `demote` clears stage + plan, keeps links, writes breadcrumb, requires reason; demote of an already-new task.
- **link_test** (in recorder_test or its own file): parent guards (missing target, self-link, second parent, direct cycle); marker stamp/removal paired with payload writes; relates-to dedup; unlink mismatch errors.
- **reporter_test**: report findings for missing file / present file / unresolvable commit / ephemeral / planned-with-no-record; healthy-count summarization; outside-repo degradation. Filesystem/git checks tested against a temp dir fixture.
- **payload round-trip**: unknown-field preservation across a verb's read-modify-write; malformed-payload degradation paths.
- **annotate_test**: table over (stage × plan kind × malformed payload) → expected Cell text/tone/nil.
- Registry/uniform-command integration: workflow_ai appears in `capability list`, guide serves, vocabulary seeds on project selection — matching the existing capabilities' integration tests.

Schema/testing caveat inherited from ATM-2e64a5: never run a dev build against `~/.config/atm`; smoke against a store copy (`ATM_HOME`).

## 8. Non-goals

- No automatic demotion — the reporter reports, the decider decides.
- No interplay contract: no reading other capabilities' labels or metadata, no migration of existing `status:*` state into stages.
- No deep cycle detection in link guards (direct two-node cycle only).
- No parent-side child lists in payloads; inbound links are always computed.
- No board over payload content, ever.
- No new substrate features: everything here is labels the substrate already supports plus the ATM-2e64a5 metadata contract.

## Settled decisions (brainstorm 2026-07-21)

1. Spec + plan now; implementation blocked on ATM-2e64a5 landing, with a plan review then.
2. New = absence of `stage:*`; `stage:done` is a real label; exactly-one-stage across five labels.
3. Links: topology in metadata, `wfai:revision` marker label for the board. Two link types: `revision_of` (single parent) and generic `relate_to`.
4. Plan record: typed locator `{kind: file|commit|ephemeral, ref, recorded_at, actor}`; reporter verifies what it can; ephemeral is honest and always at-risk.
5. Verbs are imperative (`brainstorm`, `clarify`, `plan`, `ready`, `done`, `demote`); `implement` is the dev session, gated by prompt guidance, not a verb.
6. Demote clears stage + plan, keeps links, logs the reason as a comment; `--reason` required.
7. Boards OR-compose so five boards cover six states; flat verb tree.
