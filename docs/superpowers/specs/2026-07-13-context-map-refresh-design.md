# Context map refresh — design

Date: 2026-07-13
Task: ATM-0085
Status: approved

## Problem

The manager context template already instructs the manager to "bookmark repository links, documents, code paths, ... and constantly keep them fresh during manager sessions" (`internal/manager/context_v1.md:28`). Nothing backs that instruction. There is no mechanism that can tell a manager which bookmarks went stale, so in practice they never get refreshed.

The consequences are visible in the ATM project's own ledger. Sixteen `context:*` index tasks (ATM-0001 through ATM-0016) point at repositories, documents, and code paths. Several point at things that have since moved — ATM-0014 references `internal/onboard/prompt_opencode_v1.md`, a package that no longer exists. Nothing detected this. Nothing will, until an agent follows the pointer and finds nothing there.

Two distinct failures compound:

1. **Knowledge rots silently.** A pointer's subject changes, moves, or dies, and the ledger goes on asserting the old truth. No signal is raised.
2. **The map never grows.** Onboarding is designed as a one-shot ceremony (`atm manage --project <CODE> --onboarding`, "when first introduced to a repo/project"). New packages, specs, and documents appear after that run and are never added, because re-running onboarding means re-reading the whole repo — a cost nobody pays twice.

The goal: make the context map a living artifact that a manager session can reconcile against reality cheaply enough to do every session, and make current knowledge the default thing any agent reads.

## Principle: capability commands own their vocabulary

This design introduces a pattern that governs how ATM grows, and `atm context` is its first instance.

A **capability command** is a CLI subsystem that owns a slice of the label substrate. It:

- **Ensures its own vocabulary.** Before using a label or board, it creates it — idempotently, with a description. It never assumes `atm label seed` ran, never assumes the project's labels have a particular shape, and works in a project whose human curated the vocabulary differently.
- **Exposes intent-level verbs.** Callers say what they mean (`supersede this pointer`), not which labels to apply. Label names never appear in prompts, skills, or agent reasoning.
- **Owns its data formats.** Anything machine-written and machine-read (here: provenance stamps) is written and read exclusively by the capability. No other component parses it. The format can change without touching a single prompt.

**This is a paved road, not a fence.** The store continues to validate nothing. `context:*` does not become a system namespace. A human may still hand-write a context task with no provenance, hand-assign `knowledge:superseded`, rename the labels, or delete the board — nothing breaks. The capability reports what it can prove and stays quiet about the rest. ATM's thesis that "conventions are advisory only — nothing in the store validates or special-cases the documented namespaces" survives intact.

The payoff is that agent prompts stop hardcoding label strings that drift out of sync with the store — a bug class this repo has already hit (ATM-0114: tests asserting stale prompt fragments after a template rewrite).

## Architecture

Three components, with a strict division of authority:

| Component | Role | May mutate? |
|---|---|---|
| `atm context stamp` / `add` / `retarget` / `supersede` | **Recorder.** Writes pointers, provenance, and lifecycle labels. Ensures vocabulary exists. | Yes |
| `atm context check` | **Reporter.** Compares recorded provenance against reality. Renders a worklist. | **No — strictly read-only** |
| The manager (prompt) | **Decider.** Reads the worklist, judges what drift *means*, chooses the move. | Yes, via the recorder verbs |

`check` never marks anything stale. "This file changed" and "this pointer is now wrong" are different claims, and only the second is worth acting on — a helper function added to `internal/store` does not invalidate "this package is the stable in-process API". Automatic invalidation would cry wolf on every commit. Judgement stays with the model; the machine only says where to look.

Conversely, the manager never sees a hash, a JSON key, or a label string. Its knowledge reduces to: *stamp what I read, check what drifted, retarget what moved, supersede what died.*

## The witness model

A pointer records **sources**: kinded locators for the things it was derived from. The kind determines what `check` can honestly say about it.

| Kind | Witness recorded | What `check` can prove |
|---|---|---|
| `git:<path>` | blob/commit sha at capture | Content drift, exactly |
| `file:<path>` | content hash + mtime | Content drift, exactly |
| `url:<url>` | ETag or hash of fetched body | Content drift, when the network is reachable; `SKIPPED` otherwise |
| `external:<system>/<id>` | whatever version token the capturing agent supplied (a Jira `updated`, a Notion `last_edited_time`), or nothing | **Nothing.** Only *age*: how long since anyone verified it |

`external:` is the honest admission that ATM cannot witness a Jira ticket or a Notion page. It will not grow integrations, will not hold credentials, and will not speak a third-party API. Instead it reports `AGE` — "nobody has looked at this in 47 days" — which is a weaker signal but a true one, and still enough to drive a bounded worklist. The manager re-verifies aged external pointers using whatever access *it* has (MCP, browser tools, or asking the human) and re-stamps. Verification capability lives in the agent, which already has tools; ATM stays a ledger.

`check` degrades rather than fails: an offline run skips `url:` resolution and says so.

## Where provenance lives

Provenance is written as a comment carrying `ATM:comment:provenance`, whose body is a structured block owned entirely by `atm context`.

Chosen over a typed `sources` field on the task schema because:

- **No change to the stable store API** for a field only context tasks would use.
- **Freshness history comes free.** Comments are append-mostly, so each re-stamp leaves the previous stamp behind. The thread records *"this pointer was verified at these four revisions"* — which is exactly the "history labels" idea in a form that needs no new labels.
- The format stays private to the capability, per the principle above.

`check` reads the newest provenance comment per context task. A task with none is reported `UNVERIFIED` — not an error, a signal: nobody has ever verified this pointer against reality. This mirrors ATM's existing degradation pattern, where a namespace with no description surfaces a warning in the Boards pane rather than failing anything.

## Lifecycle and "the CLI returns the latest"

Lifecycle is its own namespace so it **composes** with kind rather than replacing it:

- `ATM:knowledge:superseded` — this pointer is obsolete; the successor is named in its description.
- Absence of the label means current. **An untagged pointer counts as current**, so a human hand-writing a context task need not know the lifecycle namespace exists.

A retired pointer stays `context:documentation` *and* gains `knowledge:superseded`. It keeps its kind, its narrative, its comment thread, and all its provenance stamps. Nothing is deleted — that is what makes it history rather than garbage collection.

The requirement that *"the CLI endpoints should return the latest"* costs **zero new code**. It is a board:

```
ATM:context-current   expr: context:* AND NOT knowledge:superseded
```

`atm context` ensures this board exists (with its description and expression) on first use, like any other part of its vocabulary. The first-contact sequence in `atm conventions` then points agents at the board rather than the raw `context:*` namespace.

Every agent — a session starting now, or one that has been running for an hour and queries again — gets current knowledge, because board membership is computed at query time. **Nothing is ever pushed to a live session.** The pull is simply correct. Superseded knowledge remains reachable for anyone who asks for it explicitly, but nobody trips over it by accident.

## Drift detection needs no new persistent state

- **DRIFT is per-task.** Each pointer carries its own recorded hashes; compare its sources against `HEAD` directly. No watermark, no "since".
- **NEW territory** (changed in git, claimed by no pointer) is the only thing needing a window. It defaults to *the most recent provenance stamp in the project*, overridable with `--since`.

So `check` introduces **no persistent state whatsoever**. Everything it needs is already in the comment threads it reads.

`check` also never learns the repo's structure. It has no notion of what a "package" or "module" or "meaningful area" is — that is ecosystem-specific and would drag ATM into a pile of language heuristics. The structural judgement happens **once**, in the manager's first read, and is frozen into the ledger as the set of stamped pointers. Every later run is pure git arithmetic over those pointers:

- changed ∧ covered → **DRIFT**
- changed ∧ uncovered → **NEW** (territory being actively worked on that the map does not mention)
- unchanged ∧ uncovered → **invisible, correctly.** If it has sat untouched since the manager read the tree and the manager chose not to point at it, that was a judgement, not an oversight. Re-surfacing it every run is nagging.

This eliminates the heuristic, eliminates the ignore-list problem (git already honours `.gitignore`), and keeps the tool honest: it only reports what it can prove.

## Command surface

```
atm context add       --task <ID> --kind <documentation|repository|agent|question> --source <kinded-locator>...
    Ensures context:<kind> exists, applies it, records sources, writes the first provenance stamp.

atm context stamp     --task <ID>
    Re-verifies: the subject is unchanged in meaning; record a fresh witness.

atm context retarget  --task <ID> --source <kinded-locator>...
    The subject survived but moved. Same task ID, new sources, new stamp.

atm context supersede --task <ID> --by <NEW-ID> --reason "<text>"
    The subject died or was replaced. Applies knowledge:superseded to the old task,
    records the supersession in its description, ensures the context-current board exists.

atm context check     --project <CODE> [--since <rev>]
    Read-only report. Mutates nothing.
```

`retarget` and `supersede` being distinct verbs *is* the moved-vs-died policy. The verb name makes the manager's judgement explicit and recorded, so two managers cannot handle the same drift inconsistently.

Sample `check` output:

```
$ atm context check --project ATM
  (new territory since d1f8cc4, the most recent stamp)

DRIFT (2)        provable content change
  ATM-0007  git:internal/store — 6 files changed since verified
  ATM-0009  git:internal/tui — path moved or deleted

NEW (1)          changed in git, claimed by no pointer
  internal/embed/ — 4 files

AGE (2)          unprovable; re-verify by hand
  ATM-0021  external:jira/ATM-441 — 47d since verified
  ATM-0022  external:notion/arch-notes — 90d since verified

UNVERIFIED (1)   no provenance stamp
  ATM-0014

OK (13)  |  SKIPPED (1 url, offline)
```

## Onboarding becomes repeatable

The manager's `--onboarding` track is renamed to one that does not imply "first time" (`--mapping`), with `--onboarding` retained as a hidden deprecated alias — the flag-deprecation pattern already proven in `internal/cli/task.go` and recorded in ATM-0113.

The track's prompt is restructured as **verify → discover → close**:

1. **Verify.** Run `check`. Work the worklist: `DRIFT` → read the task's description against the actual diff, then `stamp` (still accurate), `retarget` (moved), or `supersede` (died). `AGE` → re-verify with the agent's own tools. `UNVERIFIED` → read, confirm, `stamp`.
2. **Discover.** Work the `NEW` list: is this territory worth a pointer? If so, create the task and `add` it.
3. **Close.** Everything reported is now either stamped or deliberately ignored.

**First run is the degenerate case**: verify has nothing to check, discover does all the work, and the session looks exactly like today's onboarding. **A mature repo's run costs minutes**, because most pointers come back `OK` and are never re-read. That collapse in cost is what turns onboarding from a ceremony into something runnable every session.

## Testing

- **`check` purity**: assert the store is byte-identical before and after a `check` run, including in a project with drift, age, and unverified pointers present.
- **Vocabulary bootstrap**: run every verb against a project created *without* `label seed`; assert the required labels and the `context-current` board are created with descriptions, and that a second run is idempotent.
- **Witness kinds**: table-driven over `git` / `file` / `url` / `external` — each producing the right verdict for unchanged, changed, moved, and deleted subjects.
- **Offline degradation**: `url:` sources report `SKIPPED`, not failure, with no network.
- **Board semantics**: an untagged context task appears in `context-current`; a superseded one does not; the superseded task retains its kind label and full comment thread.
- **Flag alias**: `--onboarding` and `--mapping` both resolve; the deprecated one is hidden and warns.

## Out of scope

- **TUI surface for context health.** The CLI report suffices to start; a Boards-pane view can follow.
- **Staleness policy config for external sources.** Age is reported as a number and the manager judges. A declared per-kind freshness budget (`jira: 30d`) can be added later without changing the format.
- **Coverage roots / declared globs.** Rejected in favour of git-delta only. Old-and-uncovered territory stays invisible by design.
