# workflow_ai capability: action-oriented reframe

**Task:** TBD · **Initiative:** workflow_ai capability evolution · **Status:** Draft 2026-07-23

**Supersedes (in part):** `docs/superpowers/specs/2026-07-21-workflow-ai-capability-design.md` — the ladder, boards, verbs, payload, and guide text defined here replace the corresponding sections of that spec. The package identity, metadata-key contract, capability-independence doctrine, and link topology are unchanged and remain authoritative from the 2026-07-21 spec.

## Purpose

The workflow_ai capability climbs tasks through a brainstorm → clarify → plan → implement cycle over the `stage:*` namespace. Its boards are the surface an operator (human or agent) reads to answer *what do I do next*. Three problems motivated this reframe:

1. **Boards read as past states, not next actions.** `brainstormed-tasks` describes a state achieved, not the action pending. An operator scanning for "what needs brainstorm" finds no board named for that action; the same applies to every rung. The blob of 32 undifferentiated `brainstormed` tasks (ATM's current state) is unreadable as a work queue.
2. **`new-tasks` (`NOT stage:*`) sweeps up context pointers.** Context pointers belong to the `contextmap` capability, never to `workflow_ai`. Because "new" was absence-as-new (no `stage:*` label), the 24 context pointers in ATM's backlog all appeared on the `new-tasks` board, polluting the workflow_ai intake queue with territory that is not its own.
3. **`planned` vs `implementable` is confusing.** The two rungs look like the same thing; the `ready` verb that bridges them is a sizing gate that `revision_of` links already handle better. A follow-up task shares its parent's plan via the link; the manager walks links to stay aware. No separate "sized" rung is needed.

The reframe: boards orient around the next action; stages are explicit stamps (not absence-as-new); the ladder is artifact-gated (each rung beyond `queued` has a locatable artifact); the `planned`/`implementable` distinction collapses into one `planned` rung that means "cleared for implementation".

Doctrine honored throughout (docs/architecture/label-substrate-and-capabilities.md): labels carry only what boards select; machine state lives in the capability's metadata key; boards never query payloads; views live with the owner; the store enforces nothing — every invariant here is a paved road maintained by the capability's verbs, not a fence.

## 1. The ladder — five explicit, artifact-gated rungs

```
stage:queued → stage:brainstormed → stage:clarified → stage:planned → stage:done
```

| Stage | Meaning | Artifact gate | Verb that lands here |
|---|---|---|---|
| `queued` | entry — in the workflow_ai cycle, not yet brainstormed | none | `queue` (new verb) |
| `brainstormed` | problem explored; exploration notes recorded (task comments/description) | exploration notes exist | `brainstorm` |
| `clarified` | scope + success criteria settled; a spec file exists | spec locator recorded in payload | `clarify --kind <kind> --ref <ref>` |
| `planned` | implementation plan recorded AND sized for one session; cleared for implementation | plan locator recorded in payload | `plan --kind <kind> --ref <ref>` |
| `done` | cycle complete | completed work | `done` |

**`queued` is a real stored label, not absence-as-new.** This is the explicit entry that solves problem #2: context pointers never get queued, so they never appear on any workflow_ai board. A task with no `stage:*` label is simply not in the workflow_ai cycle — it may be a context pointer (contextmap's domain), a naked jotting, or a deliberate deferral. The `queue` verb is the sanctioned entry stamp.

**`clarified` is artifact-gated.** Where the prior design treated `clarified` as "scope and success criteria settled" (a judgement), it now requires a locatable spec — a spec file under `docs/superpowers/specs/` (or a commit, or an ephemeral pointer). The `clarify` verb gains `--kind` and `--ref`, mirroring `plan`. The spec locator is recorded in the payload's new `spec` field. `report` verifies both spec and plan locators.

**`planned` absorbs `implementable`.** The two-rung distinction collapses: `planned` means "has a recorded plan AND is cleared for implementation". Sizing is handled by `revision_of` links — a planned task bigger than one session is split into follow-ups that share the parent's plan via the link; the manager walks links to stay aware. The `ready` verb and `stage:implementable` label are removed.

**No skipped rungs.** Tasks advance one rung at a time and only when the next rung's artifact exists. `plan` from `queued`/`brainstormed` is rejected (must `clarify` first). `done` from `queued`/`brainstormed`/`clarified` is rejected (must `plan` first).

**Adoption / backfill.** When this reframe lands over an existing backlog, legacy tasks are stamped directly to the rung their existing evidence supports — completed work is its own evidence, so a finished task is stamped `stage:done` outright. Raw `atm task label add` is the sanctioned path for backfill; the one-rung climb governs live refinement, not adoption. See §6 for the specific migration.

## 2. Boards — action-oriented, one stage per board

Six boards, seeded with the vocabulary, selecting labels only:

| Board | Expr | Reads as |
|---|---|---|
| `<code>:to-brainstorm` | `stage:queued` | "what do I brainstorm next" |
| `<code>:to-clarify` | `stage:brainstormed` | "what do I clarify (write a spec for) next" |
| `<code>:to-plan` | `stage:clarified` | "what do I plan next" |
| `<code>:to-implement` | `stage:planned` | "what can I implement now" |
| `<code>:revisions` | `wfai:revision AND NOT stage:done` | open follow-ups still needing refinement |
| `<code>:done-tasks` | `stage:done` | completed through the cycle |

Each board selects exactly one stage (the next action's input), not a range. The prior design merged two stages per board (`brainstormed OR clarified`, `planned OR implementable`) — that made boards read as "in a state" rather than "do this next". With the ladder collapsed to five explicit rungs, one board per stage is exact.

The old `new-tasks` board (`NOT stage:*`) is **removed**. It was the board that swept up context pointers. Replaced by `to-brainstorm` = `stage:queued`.

`Exposed` ring order: `to-brainstorm`, `to-clarify`, `to-plan`, `to-implement`, `revisions`, `done-tasks`, then the `stage:*` and `wfai:*` namespace descriptors.

## 3. Metadata payload — add `spec` locator

All workflow_ai machine state lives under `Meta["workflow_ai"]` as one versioned JSON object, written blob-replace via `SetTaskCapabilityMeta` with a read-modify-write inside each verb:

```json
{
  "v": 1,
  "spec":  {"kind": "file", "ref": "docs/superpowers/specs/2026-07-23-x-design.md", "recorded_at": "...", "actor": "..."},
  "plan":  {"kind": "file", "ref": "docs/superpowers/plans/2026-07-23-x.md",      "recorded_at": "...", "actor": "..."},
  "revision_of": "ATM-aaaaaa",
  "relates_to": ["ATM-bbbbbb"],
  "demoted": {"at": "...", "by": "...", "reason": "..."}
}
```

- New `SpecRecord` type mirrors `PlanRecord` (`Kind`, `Ref`, `RecordedAt`, `Actor`); same three kinds (`file`/`commit`/`ephemeral`).
- `Payload.Spec()` / `SetSpec` / `ClearSpec` mirror the plan accessors.
- `clarify --kind --ref` writes `spec` before the label swap; from `clarified`/`planned` it updates the locator in place (same shape as `plan`'s update path).
- `demote` clears **both** `spec` and `plan` records, plus the stage label.
- Absent fields mean "nothing recorded". A payload whose fields are all absent is deleted outright. Unknown fields survive rewrite (degrade-never-reject applied to ourselves) — unchanged from the 2026-07-21 spec.

## 4. Verbs

| Verb | Transition | Guard |
|---|---|---|
| `queue` (new) | new → `queued` | none — explicit entry stamp |
| `brainstorm` | `queued` → `brainstormed` | — |
| `clarify --kind --ref` (changed) | `brainstormed` → `clarified`; from `clarified`/`planned` updates `spec` in place | non-empty `--ref`; writes `spec` before label swap |
| `plan --kind --ref` (unchanged shape) | `clarified` → `planned`; from `planned` updates `plan` in place | non-empty `--ref`; writes `plan` before label swap |
| `done` | `planned` → `done` | — |
| `demote --reason` (changed) | any → `queued` (was: any → new) | clears `spec` + `plan` records, keeps links, logs reason as comment |
| `report` (changed) | read-only | verifies `spec` (clarified/planned) and `plan` (planned) locators |
| `link`/`unlink`/`links`/`seed` | unchanged | — |

**Removed:** `ready` verb and `StageImplementable`.

**Demote target change.** Demote now resets to `queued` (not absence/new), so a demoted task stays in the cycle on the `to-brainstorm` board rather than vanishing from all boards. This closes the loop — demotion is a re-queue, not an exit. The reason is still logged as a task comment (audit trail), and the `demoted` breadcrumb is still written to the payload.

**Report change.** `PlanCheck` walks `stage:clarified OR stage:planned` tasks. For each: verifies `spec` exists (clarified and planned both carry a spec); verifies `plan` exists (planned only). Findings distinguish spec-at-risk from plan-at-risk in `Finding.Detail`. The reporter never demotes; the operator decides, then `demote --reason`.

## 5. Context-pointer exclusion — how problem #2 resolves

No code change to `contextmap`. The resolution is structural:

1. `to-brainstorm` selects `stage:queued` (a real label), not `NOT stage:*`. Context pointers never carry `stage:queued`, so they never appear on any workflow_ai board — they remain on contextmap's `context-current` board exclusively.
2. The manager persona is capability-agnostic by design (`skills/persona/manager.md`: "Learn them at runtime and assume nothing about them in advance"). It reads each capability's guide at session start via `atm capability <name> guide`. The workflow_ai guide (rendered from `guide.go`) is where the rule lives: context pointers are contextmap's domain; never queue them into workflow_ai. No manager-prompt or plugin-asset change is needed.
3. Backfill: the manager stamps existing workflow tasks `stage:queued`; context pointers are left untouched. The 24 context pointers currently polluting `new-tasks` disappear from workflow_ai boards the moment `new-tasks` is replaced by `to-brainstorm`.

This is the "no change in code, manager understands each capability's logic" answer — the label substrate change does the work, not cross-capability coupling.

## 6. Migration & backfill

Existing ATM tasks carry the old vocabulary. The migration is a one-time backfill, not a live migration:

1. **Vocabulary reseed.** `atm capability workflow_ai seed --project ATM` must replace the old board labels with the new ones. `LabelSeed` upserts only when absent, so the implementation must force-update board descriptions/exprs (or remove the old `new-tasks`/`brainstormed-tasks`/`planned-tasks` labels and create `to-brainstorm`/`to-clarify`/`to-plan`/`to-implement`). Old stage label `stage:implementable` is removed from the vocabulary; `stage:queued` is added.
2. **Task backfill** (manager's job, raw `atm task label add/remove`):
   - Tasks with `stage:implementable` → restamp to `stage:planned` (the rung collapsed into one). Currently ATM-0074, ATM-0123.
   - Tasks with `stage:brainstormed`/`stage:clarified`/`stage:planned`/`stage:done` → keep their stage (the names are unchanged). For `clarified`/`planned` tasks, the manager records a `spec`/`plan` locator if one isn't already in the payload (backfill to evidence).
   - Tasks with no stage (the 24 context pointers) → **untouched**. They were never in workflow_ai; they stay in contextmap.
   - Tasks with no stage that ARE workflow tasks (none currently, but the rule holds) → stamp `stage:queued`.
3. **Old board cleanup.** `new-tasks`, `brainstormed-tasks`, `planned-tasks` labels are removed from the store (capability-owned, so `atm label remove` is sanctioned); `to-brainstorm`/`to-clarify`/`to-plan`/`to-implement` replace them. `revisions` and `done-tasks` keep their names.
4. **`ready` verb removal.** Drop the command and the `Recorder.Ready` method. Any task that was `stage:implementable` is now `stage:planned` — already cleared for implementation.

The backfill is idempotent and can run in passes; the manager journals each task's restamp as a comment.

## 7. Code change surface

Files touched, grouped by concern:

**Stage ladder (`stage.go`, `recorder.go`):**
- `StageQueued = "queued"` added as a real stored label. `StageNew` sentinel stays (used by guards/reporters for "no stage label").
- Remove `StageImplementable`.
- `Brainstorm` transition: `queued → brainstormed` (was: `new → brainstormed`).
- New `Queue` method: `new → queued`.
- `Clarify` gains `kind, ref` params; writes `spec` record before label swap; allows in-place update from `clarified`/`planned`.
- `Ready` method removed.
- `Done` transition: `planned → done` (was: `implementable → done`).
- `Demote` resets to `queued` (not new); clears both `spec` and `plan`.

**Payload (`payload.go`):**
- `SpecRecord` type + `Spec()`/`SetSpec`/`ClearSpec` accessors.
- `Demote` clears `spec` too.

**Vocabulary & boards (`vocabulary.go`):**
- 5 stage labels: `queued`, `brainstormed`, `clarified`, `planned`, `done` (drop `implementable`).
- 6 boards: `to-brainstorm`, `to-clarify`, `to-plan`, `to-implement`, `revisions`, `done-tasks` (drop `new-tasks`, `brainstormed-tasks`, `planned-tasks`).
- Each board selects exactly one stage (not a range).
- `Exposed` ring order updated.

**Reporter (`reporter.go`):**
- `PlanCheck` walks `stage:clarified OR stage:planned`; verifies `spec` on both, `plan` on `planned`.
- `Finding.Detail` distinguishes spec-at-risk vs plan-at-risk.

**Commands (`command.go`):**
- Add `queue` stage command.
- `clarify` gains `--kind`/`--ref` flags (mirror `plan`).
- Remove `ready` command.
- `report` short text updated.

**Guide (`guide.go`):**
- Full rewrite of Semantics, Boards, Actions, Converge per this design.

**Tests:** all `*_test.go` in the package updated to the new ladder, new verb signatures, new boards, new payload `spec` field.

**Developing orientation:** one-line note added — context pointers are contextmap's domain, never queue them into workflow_ai.

## 8. What does not change

- **Package identity, metadata-key contract, capability-independence doctrine** — unchanged from the 2026-07-21 spec.
- **Link topology** (`revision_of`, `relates_to`, `wfai:revision` marker) — unchanged.
- **`wfai:framework` marker** — unchanged.
- **Manager persona and plugin assets** — unchanged; the manager learns the new vocabulary at runtime via the guide.
- **Store enforcement model** — still a paved road, not a fence.