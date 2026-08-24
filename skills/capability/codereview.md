---
name: codereview
description: "Code review flow capability: schedules, tracks, and records reviews of finished implementation work against pull requests."
brief: Code review flow capability over finished implementation work; read its guide before scheduling, beginning, or finishing a review.
labels: [codereview:*, codereview-out:*]
boards: [codereview-inbox, codereview-pipeline, codereview-out-board]
---
# codereview capability — agent guide

Review of finished implementation work. codereview absorbs a task against its pull request, tracks the review through `scheduled -> reviewing -> done`, and records where the PR and the report live.

codereview is a FLOW capability. It never reads another capability's metadata and never calls another capability's verbs; it reaches upstream only through project wiring, and downstream only by stamping its own finish socket.

## Semantics

### The three lanes

- `<CODE>:codereview-inbox` — finished work with no review decision yet. Its expression is PROJECT WIRING, typically the upstream finish socket narrowed to implementation units: `scrum-stage:done AND scrum:task`.
- `<CODE>:codereview-pipeline` — reviews scheduled, under way, and finished (`codereview:* AND NOT codereview-out:*`).
- `<CODE>:codereview-out-board` — settled evictions (`codereview-out:*`), permanent until someone calls `release`.

### The Inbox IS the warning surface

`absorb` REQUIRES the pull request. A task whose PR nobody can find is simply LEFT in the inbox, and the inbox count grows. That growth is the warning — a team that does not do PRs but enables this capability watches the number climb, and a review that nobody scheduled shows up as a row rather than as silence. There is **no separate warning mechanism**, no staleness threshold, no alert: the lane count carries the whole signal, which is why absorb is never called without a PR to make it accurate.

Triage means hunting the PR — in the task's comments, in the repository, in search — and either absorbing with it or leaving the row where it is with a comment saying what was looked for.

### The two axes

`codereview:*` is the CLAIM axis, and it doubles as the state: `scheduled`, `reviewing`, `done`. A reviewed task stays claimed and stays visible.

`codereview-out:*` is the EVICT axis; its value is the reason: `not-warranted`, `superseded`.

### The sockets

- FINISH: `<CODE>:codereview:done` — this change has been reviewed.
- EVICT: `<CODE>:codereview-out:*` — codereview considered the change and declined to review it.

Both are stored labels, never metadata: intake expressions are evaluated by the store's resolver, which never reads metadata.

### State

`Meta["codereview"]` holds two LOCATORS and nothing else. The review conversation lives on the pull request, not in the ledger:

- `pr` — the pull request, as a URL or a number. Required by absorb, so a claimed task always has one.
- `report` — where the review report lives, when one was written. Optional.

### Backward flow

A finished review is a verdict, not a gate someone has to reopen. When a review requests changes, the manager either creates a FOLLOW-UP TASK for the findings — into the pool, then upstream — or runs the RE-REVIEW PAIR: the upstream capability's `reopen` plus this capability's `release`. Two audited, order-independent verbs — never composed, because no capability may un-stamp a sibling. When upstream re-finishes, the task lands back in this inbox for a fresh triage.

## Actions

- `atm capability codereview absorb --task <ID> --pr <url-or-number>` — schedule a review. `--pr` is required by design; see the warning-surface rule above.
- `atm capability codereview begin --task <ID>` — move a scheduled review to under way.
- `atm capability codereview finish --task <ID> [--report <locator>]` — stamp the finish socket, optionally recording where the report lives.
- `atm capability codereview evict --task <ID> [--reason not-warranted|superseded]` — settle a change out of codereview.
- `atm capability codereview release --task <ID> --reason "..."` — withdraw codereview's perspective entirely; the task returns to the pool.
- `atm capability codereview report --project <CODE>` — lane rosters plus findings (read-only).
- `atm capability codereview seed --project <CODE>` — ensure the vocabulary and lane boards exist.

## Converge

A converged project reads like this:

- Every inbox row has had its PR hunted for — absorbed with one, or left with a comment saying what was searched.
- Every claimed task records a `pr`; a claim without one is a hand-assigned label, not a review.
- Nothing sits in `reviewing` that nobody is reviewing. There is no threshold for this: the manager reads the roster and decides.
- Every eviction carries a reason.
- No `report` finding is left standing: each is resolved or answered with a recorded decision.
