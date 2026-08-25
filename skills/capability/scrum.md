---
name: scrum
description: "Scrum flow capability: absorbs raw work into an EPIC -> Story -> Task/Bug/Design pipeline with spec/plan tracking."
brief: Scrum flow capability over the unclaimed pool; read its guide before absorbing, decomposing, or staging development work.
labels: [scrum:*, scrum-stage:*, scrum-out:*]
boards: [scrum-inbox, scrum-pipeline, scrum-out-board]
---
# scrum capability — agent guide

The first stage of the flow. Raw work reaches scrum's Inbox and leaves it one of three ways: claimed into the pipeline with a type, evicted with a reason, or deliberately deferred with a comment saying why.

scrum is a FLOW capability: it sees the project as three lanes and declares where work enters, finishes, and leaves. It never reads another capability's metadata and never calls another capability's verbs. Downstream capabilities reach it only through its declared sockets, and only as project wiring.

## Semantics

### The three lanes

- `<CODE>:scrum-inbox` — eligible work scrum has not decided about. Its expression is PROJECT WIRING (`atm project wiring set --capability scrum`) with an invariant tail that hides whatever scrum already claimed or evicted. By default the eligibility is the unclaimed pool: work no enabled flow capability has taken. A non-empty inbox is the signal to dispatch the manager.
- `<CODE>:scrum-pipeline` — what scrum is building (`scrum:* AND NOT scrum-out:*`). Finished units stay visible; done is a state, not a disappearance.
- `<CODE>:scrum-out-board` — settled evictions (`scrum-out:*`). Permanent until someone calls `release`. The `-board` suffix is not decoration: a bare `scrum-out` label would read as a member of the `scrum-out:*` namespace.

### The three axes

`scrum:*` is the CLAIM/TYPE axis. Presence of any value means "claimed by scrum"; a claimed unit carries exactly one type.

| type | what it is | flows downstream |
|---|---|---|
| `epic` | product-level requirement, decomposed into stories | no |
| `story` | a portion of an epic's design, decomposed into tasks | yes |
| `task` | PR-sized implementation unit | yes |
| `bug` | defect to fix | yes |
| `design` | design/spec work whose deliverable is an executable plan | NEVER |

Design tasks never flow downstream — there is no build to verify and no PR to review. That exclusion lives in the DOWNSTREAM capability's wiring expression, not in any code here.

`scrum-stage:*` is the working stage of a claimed unit: `brainstormed`, `planned`, `implementing`, `review`, `done`.

`scrum-out:*` is the EVICT axis; its value is the reason: `duplicate`, `out-of-scope`, `not-worth-it`, `covered-by`.

The verbs keep the claim and evict axes mutually exclusive. The store enforces none of this — it is a paved road, not a fence.

### The sockets

- FINISH: `<CODE>:scrum-stage:done`. It certifies that the unit converged — a task when it is built, a story when every live child is done, an epic when every live story is done. Downstream capabilities wire their inbox eligibility to this label. Stamping it on a story or an epic is REFUSED while any live child is undone; the refusal names the offenders.
- EVICT: `<CODE>:scrum-out:*`. Any member means scrum considered the work and declined it.

Both are stored labels, never metadata: intake expressions are evaluated by the store's resolver, which never reads metadata.

### State

Unit topology and locators live in `Meta["scrum"]`, and only OUTBOUND facts are stored:

- `part_of` — this unit's parent (a story's epic, a task's story). At most one.
- `depends_on` — execution dependencies. Blocking is DERIVED from these by the reporter, never stamped as a label: a unit can be planned and blocked at the same time.
- `covered_by` — the task that covers this one; required by the `covered-by` and `duplicate` evictions.
- `spec`, `plan` — repo-relative locators. Pointers, not content.

Inbound views (a parent's children, a task's dependents) are derived by scanning the project, so they can never go stale against a child that moved.

### Backward flow

Nothing automatic ever points backward. Rework has exactly two shapes, both manager decisions:

1. A FOLLOW-UP TASK for the findings — into the pool, then scrum. No label cycles.
2. The RE-REVIEW PAIR: `atm capability scrum reopen` here, plus the downstream capability's own `release`. Two audited, order-independent verbs — never composed, because no capability may un-stamp a sibling.

## Actions

- `atm capability scrum absorb --task <ID> --type epic|story|task|bug|design [--stage <stage>]` — claim an inbox task. `--stage done` reads already-finished work in without pretending it still has to be built. Refuses a task scrum has evicted (release it first).
- `atm capability scrum add --project <CODE> --title "..." --type <t> [--part-of <ID>] [--stage <stage>]` — create a child born into the pipeline. The parent must already be claimed by scrum.
- `atm capability scrum stage --task <ID> --stage brainstormed|planned|implementing|review|done` — move a claimed unit along the working axis.
- `atm capability scrum evict --task <ID> [--reason duplicate|out-of-scope|not-worth-it|covered-by] [--covered-by <ID>]` — settle work out of scrum. `covered-by` requires `--covered-by`.
- `atm capability scrum release --task <ID> --reason "..."` — withdraw scrum's perspective entirely: every scrum label and its whole payload go, the reason is recorded as a comment, and the task returns to the pool.
- `atm capability scrum reopen --task <ID> --reason "..."` — un-finish a done unit (done -> implementing), with the reason on the record.
- `atm capability scrum link --task <ID> --depends-on <ID>|--part-of <ID>` — record one outbound edge. Refuses self-links, cross-project links, and direct cycles.
- `atm capability scrum unlink --task <ID> --depends-on <ID>|--part-of <ID>` — remove one.
- `atm capability scrum spec --task <ID> --path <p>` — record the spec locator.
- `atm capability scrum plan --task <ID> --path <p>` — record the implementation-plan locator.
- `atm capability scrum links --task <ID>` — one unit's topology, outbound and inbound (read-only).
- `atm capability scrum report --project <CODE>` — lane rosters plus findings (read-only).
- `atm capability scrum seed --project <CODE>` — ensure the vocabulary and lane boards exist.

## Converge

A converged project reads like this:

- The Inbox is empty, or every row in it carries a comment saying why it is deferred.
- Every claimed unit carries exactly one type and one stage, and the stage matches reality.
- Every child's `part_of` points at a live, claimed parent.
- Every parent whose live children are all done is itself stamped `scrum-stage:done` — until it is, downstream sees nothing.
- Every eviction carries a reason, and `covered-by`/`duplicate` evictions carry `covered_by`.
- Every finished design task records its `plan` locator.
- No `report` finding is left standing: each is resolved or answered with a recorded decision.

## Duty: manager

### Triage
List the inbox: `atm task list --project <CODE> --label <CODE>:scrum-inbox`.
For each task: absorb real dev work with `atm capability scrum absorb --task <ID> --type <t>` —
pick `epic` for product-level requirements needing decomposition, `task`/`bug` for direct
PR-sized work, `design` for spec work; historical already-done work absorbs with `--stage done`.
Evict non-dev work (`out-of-scope`), duplicates (`duplicate` with `--covered-by`), and noise
(`not-worth-it`). Defer only with a recorded comment.

### Advance
Run `atm capability scrum report --project <CODE>`. Findings and their resolutions:
orphan children → re-link (`scrum link`) or release; parents with all children done but
not stamped → verify then `scrum stage --stage done`; unresolved depends_on → dispatch or
wait, comment either way. Decompose clarified epics into stories and stories into
PR-sized tasks/designs with `scrum add --part-of`. Keep priority current (guide Semantics);
dispatch developer personas for tasks whose plan exists and dependencies are clear.

### Route
Fresh `scrum-out:*` evictions need no routing (scrum is first-stage; its evictions are
terminal judgments). Fresh evictions ARRIVING from downstream (qa/codereview Route rules)
land here as reopen calls — expect them, don't originate them.
