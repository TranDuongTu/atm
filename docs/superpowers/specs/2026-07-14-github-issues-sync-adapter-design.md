# GitHub Issues Sync Adapter — Design Spec

**Status:** Proposed
**Tracking:** ATM-0123
**Approach:** Hybrid #3 — the ATM v2 eventsource is source of truth; GitHub Issues
is a synced projection and entry point. This dogfoods ATM-0106 (eventsource-core-v2),
ATM-0108 (L4 sync transport), and ATM-0109 (L5 trust/auth).

## Driver

The ATM project is already published at `github.com/TranDuongTu/atm` with
`verify.yml`/`release.yml` actions and a one-command install, but ATM's own task
state lives in `$ATM_HOME/projects/ATM/log.jsonl` — invisible to anyone who clones
the repo. We want the dogfood UX to feel like Jira or GitHub Issues: a developer
can open an issue in the GitHub UI, comment, label it, and the ATM manager/agents
see those mutations; conversely, manager curation and agent status updates
propagate back to the GitHub issue. The eventsource stays ATM's own (not
GitHub's), so the v2 distributed model is really exercised — this is the live
proof of ATM-0106/0108/0109, not a thin GitHub-backed store adapter.

A repo already using GitHub Issues can onboard ATM by importing its existing
issues/comments/labels into the eventsource; ongoing work stays bidirectionally
in sync. This is the general path for any GitHub-hosted project that wants ATM
curation on top of its existing issue tracker.

## Scope: a bidirectional adapter, not a store rewrite

This spec owns the bidirectional adapter that maps ATM's v2 eventsource to
GitHub Issues/labels/comments, plus the `atm github` CLI surface and the
GitHub Actions workflow that drives sync. Concretely: the identity mapping
between v2 entities and GitHub primitives; the actor mapping for GitHub-side
users; the diff/translation logic in both directions; the CLI commands; and the
`.github/workflows/atm-sync.yml` workflow.

### Prerequisites (consumed, not redefined)

- **ATM-0106** (eventsource-core-v2): event identity, HLC, DAG, fold, alias
  minting/resolution, v1→v2 upgrade. The adapter emits and reads v2 events
  through this library.
- **ATM-0107** (L3 — on-disk DAG layout for the distributed eventsource): the
  adapter reads/writes `log.jsonl` through whatever L3 defines.
- **ATM-0108** (L4 — sync & transport protocol): the adapter implements L4's
  `SyncTarget` interface (or successor) for GitHub. If L4's interface shifts,
  this adapter shifts with it — that coupling is documented.

### Down-payment on L5 (parallel, not blocking)

- **ATM-0109** (L5 — identity, trust, authorization for team sync): the GitHub
  persona scheme defined here is one concrete instance of L5's broader trust
  model. L5 may generalize "external-actor-as-replica" beyond GitHub; this spec
  defines the GitHub-specific case only.

### Sequencing

This spec cannot be implemented until ATM-0106, ATM-0107, and ATM-0108 land.
ATM-0109 (L5) can land in parallel — the GitHub persona scheme is a down-payment
on L5, not a blocker for it.

## Architecture & data flow

```
                        ┌──────────────────────┐
                        │   GitHub Issues API   │
                        │   (issues, comments,  │
                        │    labels, timeline)  │
                        └──────────┬───────────┘
                                   │  fetch / mutate
                                   ▼
        ┌─────────────────────────────────────────────┐
        │  atm github adapter (internal/githubsync)   │
        │  - identity mapping (task↔issue, etc.)      │
        │  - actor mapping (collaborator@github:<l>)   │
        │  - diff/translation (both directions)       │
        │  - GitHub API client (pagination, etag)     │
        └───────────────┬─────────────┬───────────────┘
                        │             │
            ingest      │             │   project
        (GitHub → v2)  │             │  (v2 → GitHub API)
                        ▼             ▼
        ┌─────────────────────────────────────────────┐
        │   v2 eventsource (log.jsonl) — ATM-0106/07  │
        │   source of truth; GitHub is one replica     │
        └─────────────────────────────────────────────┘
                        ▲
                        │ fold
        ┌───────────────┴───────────────┐
        │  cache.db (derived, disposable)│
        │  TUI / CLI / manager read here │
        └───────────────────────────────┘
```

The adapter is a single package `internal/githubsync` with two diff loops
(ingest, project) and one API client. It does not own `log.jsonl` or `cache.db`;
it emits events through the L4 sync protocol and reads the fold through the
existing `State` API.

## Identity mapping between v2 events and GitHub primitives

The bidirectional adapter needs a stable, reversible mapping. Each link is
stored on the events themselves so the projection is regenerable and ingest can
reconcile without re-deriving.

| ATM v2 entity | GitHub primitive | Mapping stored where |
|---|---|---|
| Project (`<CODE>`) | Repository (owner/name) | project config: `github.repo = "TranDuongTu/atm"` |
| Task (`<CODE>-<NNNN>`, alias) | Issue number | `payload.github_issue` on the task's creation event |
| Comment (`<ID>-c<NNNN>`, alias) | Issue comment id | `payload.github_comment` on the comment's creation event |
| Label (`<CODE>:<ns>:<value>`) | GitHub label name (verbatim) | the label name *is* the identity on both sides |

### Task ↔ issue number

Two creation directions, both converge:

- **GitHub-first:** a user opens issue #42 in the browser. The adapter ingests
  it as a `task.created` event with `payload.alias` minted by ATM and
  `payload.github_issue = 42`. ATM's task ID (e.g. `ATM-0123`) is independent of
  the issue number; the link is the `github_issue` payload field.
- **ATM-first:** a manager or agent creates task `ATM-0124` in ATM. On next
  `push`, the adapter opens a GitHub issue and emits a follow-up **`task.linked`**
  event with `payload.github_issue = <number>` and `subject.id` = the task's
  identity. This is the **external-id assignment** mechanism. `task.linked` is
  an adapter-defined action; per ATM-0106 decision 8 ("unknown actions are
  inert but causal"), the fold writes no slots for it — the external id is
  adapter metadata, not task state — but the event participates fully in the DAG
  and causality. The adapter reads `task.linked` events back when projecting to
  find a task's GitHub issue number. This is **not** the retired
  `task.meta-changed` (which ATM-0106 removes from the v2 action enum); it's a
  new action owned by this adapter, inert to the core fold.

### Comment ↔ issue comment id

Same pattern: GitHub-first ingest reads the comment id into
`payload.github_comment`; ATM-first push posts the comment and records the
returned id. ATM comments under a task map to comments under the corresponding
issue.

### Label ↔ GitHub label name (verbatim)

ATM labels are hierarchical (`ATM:status:open`); GitHub labels are flat strings.
GitHub allows colons in label names, so ATM label `ATM:status:open` becomes
GitHub label `ATM:status:open` — verbatim, no translation, reversible. Boards
(computed labels) do not get GitHub labels; they're ATM-only projections whose
description carries the expression.

If a GitHub user creates a label that doesn't match ATM's
`<CODE>:<ns>:<value>` shape, ingest creates the ATM label verbatim (ATM labels
are open-namespace per the conventions guide) and stamps its description as
"imported from GitHub; not in the project vocabulary" for the human/manager to
triage.

A cosmetic "color-coded namespace" scheme (`status:*` = green, `priority:*` =
red, etc.) is mentioned for visual grouping in the GitHub UI but is not required
for v1; v1 uses verbatim label names with default GitHub colors.

### Project ↔ repo

One ATM project ↔ one GitHub repo. The mapping lives in project config
(`atm project set-github --repo <slug>`), not in the eventsource. A project with
no `github.repo` configured has no GitHub adapter — it's a pure local project.

### Reverse projection is regenerable

Because every link is stored as a payload field on the event, the adapter can
drop the entire GitHub side and rebuild it from `log.jsonl` via the fold: walk
the folded `State`, emit one issue per task, one comment per comment, one label
per label. This is the same "derived view" contract the audit-log redesign set
for `cache.db`.

## Actor mapping

The v2 eventsource stamps every event with `persona@agent:model`. GitHub-side
mutations get a place in that model.

- **GitHub user `@alice` → `collaborator@github:alice`.** `collaborator` is a new
  built-in persona (alongside `developer`, `manager`, `admin`) for "a human
  acting through GitHub's issue tracker, not ATM directly." Its prompt is empty —
  it's a classification, not an autonomous agent. Registered automatically on
  first sight, like the other built-ins.
- **Bots** (GitHub Apps, `dependabot`, `github-actions`) → `bot@github:<login>`.
  v1 ingests them like any other GitHub user since GitHub already enforces who
  can comment/label on the repo.

### Actor identity on ingest

The GitHub API gives a stable `user.id` and `user.login` per issue/comment/label
event. The adapter stores `payload.github_actor = {"id": 1234, "login": "alice"}`
on the v2 event for traceability, and stamps `actor = "collaborator@github:alice"`.
The login is the stable actor segment (GitHub logins can be renamed, but for
ATM-scale dogfood this is acceptable; L5 ATM-0109 may revisit with the GitHub
user id as the true identity later).

### ATM-side actors stay as-is

A `developer@claude:opus-4.8` session that creates a task, then pushes to
GitHub, opens the GitHub issue authored by the ATM GitHub App (or the user
whose token the workflow uses) — but the v2 event's actor is still
`developer@claude:opus-4.8`. The GitHub-side authorship of the issue/comment is
the adapter's problem (the API call needs a credential), not the event's actor.
This separates "who caused the task mutation" (the v2 actor) from "which GitHub
account posted the issue" (the adapter's credential) — the latter is plumbing,
the former is the ledger.

### First-contact onboarding

When the adapter first sees a GitHub user it hasn't seen before, it
auto-registers the `collaborator` persona if missing (idempotent) and proceeds.
No manual persona-creation step for GitHub users. This matches the "built-in
personas are seeded automatically on first use" convention.

### Optional persona-merge escape hatch (deferred)

`atm persona create --from-github --login <login>` would let a project owner
pre-register a GitHub user with a richer persona (e.g. map `@alice` to the
existing `developer` persona instead of `collaborator`). If a GitHub login is
pre-registered this way, ingest uses the pre-registered persona. Not required
for v1 to ship; the default `collaborator@github:<login>` is sufficient.

## Diff/translation logic

The key invariant: **the v2 eventsource is authoritative; the GitHub side is a
replica reconciled to the fold.** Both directions emit v2 events into
`log.jsonl`; the GitHub API is never the source of truth.

### Direction 1 — Ingest (GitHub → v2 events)

`atm github pull --project ATM --repo TranDuongTu/atm`:

1. Fetch the full GitHub Issues state for the repo via the API (issues,
   comments, labels, timeline events for label adds/removes). Pagination;
   etag/If-Modified-Since for cheap re-polls.
2. Fold the current local `log.jsonl` to get the current `State`.
3. **Diff** the GitHub state against the folded state using the identity
   mapping above:
   - GitHub issue with no matching `payload.github_issue` in any task → **new
     task**: emit `task.created` with a minted alias, `payload.github_issue =
     <number>`, `payload.title`, `payload.body`, `payload.labels` (translated).
     Actor = `collaborator@github:<opener>`. Parents = current frontier.
   - GitHub issue with a matching task, but title/body differs from the folded
     task → emit `task.edited` with the new title/description. LWW by HLC.
   - GitHub comment with no matching `payload.github_comment` → emit
     `comment.created` with `payload.github_comment = <id>`, `payload.body`,
     `payload.task_ref` = the task's identity.
   - GitHub label added/removed on an issue vs the folded task's label set →
     emit `task.label-added`/`task.label-removed` with the label name
     (membership delta per the v2 fold rules from ATM-0106).
   - GitHub issue closed/reopened → emit `task.label-removed`/`-added` for
     `ATM:status:done`/`ATM:status:open` (closed issue ↔ `status:done`; reopened
     ↔ `status:open`). Documented as the default mapping; project config can
     override which ATM status label a closed issue maps to.
4. Append the emitted events to `log.jsonl` under the project lock, via the L4
   sync protocol (parents = current frontier; HLC = `Clock.Observe` of the
   GitHub event's timestamp).
5. Rebuild `cache.db` for the project (same as any local write).

### Idempotency

Re-running `pull` with no GitHub-side changes is a no-op — the diff produces no
events. Re-running with the same GitHub state after a partial sync converges to
the same `log.jsonl`. The `payload.github_issue`/`github_comment` fields are the
dedup keys for "have I already ingested this GitHub primitive?"

### Deleted GitHub comments/issues (v1 limitation)

GitHub doesn't truly delete issues (only closes/archives them), but comments
*can* be deleted. The adapter cannot detect deletion via polling the issues API
alone — it would need the timeline/events API or webhooks. v1 **does not ingest
comment deletions**; a deleted GitHub comment stays in the v2 eventsource as a
`comment.created` with no tombstone. Documented limitation. Webhook-driven sync
(out of scope for v1) can fix this by catching `comment.deleted` events.

### Direction 2 — Project (v2 events → GitHub API)

`atm github push --project ATM --repo TranDuongTu/atm`:

1. Fold the local `log.jsonl` to get `State`.
2. Fetch the current GitHub Issues state (same as pull's step 1).
3. **Diff** the folded state against GitHub state:
   - Task with no matching issue → **create issue** via
     `POST /repos/{owner}/{repo}/issues` with title, body, labels. Emit a
     follow-up **`task.linked`** event with `payload.github_issue = <number>`
     and `subject.id` = the task's identity (the external-id assignment
     mechanism). `task.linked` is inert-but-causal per ATM-0106 decision 8 —
     the fold writes no slots; the adapter reads it back to find the issue
     number on the next sync.
   - Task with a matching issue, but title/body/labels differ → `PATCH` the
     issue. LWW: the v2 fold is authoritative, so GitHub is updated to match.
   - Comment with no matching GitHub comment id → `POST` a comment on the
     issue. Emit a follow-up **`comment.linked`** event with
     `payload.github_comment = <id>` and `subject.id` = the comment's
     identity. Same inert-but-causal treatment as `task.linked`.
   - Comment with a matching id, body differs → `PATCH` the comment. v2
     authoritative.
   - Task gains `ATM:status:done` → close the issue. Task loses it → reopen.
     Same default mapping as ingest, configurable.
   - Label added/removed on a task → add/remove the GitHub label on the issue.
     The GitHub label is created if it doesn't exist
     (`POST /repos/{owner}/{repo}/labels`).
4. Append the "external id assignment" follow-up events to `log.jsonl`.

### Credential

The workflow runs with a GitHub App token or `GITHUB_TOKEN` with `issues:
write` and `labels: write` scopes. The adapter doesn't care which account posts;
the v2 actor is whatever the event says, the GitHub author is the token's
account.

### The sync loop (`atm github sync`)

`sync` = `pull` then `push`, in that order, with one fold in between. Pull
ingests GitHub-side mutations; push projects ATM-side mutations back out.
Running `sync` on a schedule (the Actions workflow) converges both sides. A
local developer running `atm github sync` before opening the TUI gets the
latest GitHub-side state and pushes any local work out.

### Conflict example

A GitHub user edits a task's title in the browser at HLC T1, and an ATM-side
agent edits the same task's title at HLC T2 > T1. After `pull` ingests the
GitHub edit as an event at T1, and the agent's edit was already at T2, the fold
resolves the title to T2's value (agent wins). After `push`, the GitHub issue's
title is updated to T2's value. Converged. No special conflict code — the v2
fold does it.

### The external-id link events are the only adapter-defined actions

Everything else is "emit a new event" or "call the GitHub API." The
`task.linked`/`comment.linked` events are adapter-defined actions (not in the
core v2 enum) that carry the GitHub-side id back into the eventsource. Per
ATM-0106 decision 8 they are inert-but-causal: the fold writes no slots, but
the events participate fully in the DAG and causality so the adapter can find
them on the next sync. This is the only place the adapter introduces new
actions; all other adapter writes use the core v2 actions
(`task.created`/`task.edited`/`task.label-added`/`comment.created`/etc.).

## CLI surface

New `atm github` command group. All commands require `--project <CODE>`; the
repo is read from project config (`atm project set-github --repo <slug>`) but
overridable with `--repo <slug>` for one-off use.

```
atm project set-github --project ATM --repo TranDuongTu/atm
atm github pull   --project ATM [--repo <slug>]   # GitHub → v2 events (ingest)
atm github push   --project ATM [--repo <slug>]   # v2 events → GitHub API (project)
atm github sync   --project ATM [--repo <slug>]   # pull then push (converge)
atm github status --project ATM [--repo <slug>]   # dry-run diff report
atm github import --project ATM --repo <slug>    # one-shot onboarding ingest
```

- **`set-github`** records the repo slug in project config; without it, the
  adapter commands refuse with "no GitHub repo configured for project <CODE>."
- **`pull`/`push`/`sync`** print a summary: events ingested / issues updated /
  comments created / labels synced. Non-zero exit on API failure — the v2
  eventsource is left untouched on `push` failure; GitHub API errors don't
  corrupt `log.jsonl`.
- **`status`** is a dry-run: fetches both sides, diffs, prints "GitHub would
  gain N issues, ATM would gain M tasks, K labels differ" — no writes. Useful
  before a first sync or to inspect drift.
- **`import`** is one-shot onboarding for a repo already using GitHub Issues.
  Reads all open issues (and optionally closed ones with `--include-closed`),
  emits `task.created`/`comment.created`/`label.upserted` events into
  `log.jsonl`, sets `payload.github_issue`/`github_comment` on each. This is the
  "a repo already on GitHub Issues wants to onboard ATM" path. Idempotent:
  re-running on the same repo skips already-ingested issues (the
  `payload.github_issue` dedup). Differs from `pull` only in that it's the
  *first* ingest; mechanically it's `pull` against an empty local project.

### Authentication

- **Local CLI** (a developer running `atm github sync`): reads `GITHUB_TOKEN`
  from env (a personal access token or fine-grained PAT with `issues`/`labels`
  scope on the repo). No ATM-stored credentials — ATM never holds GitHub
  tokens; the env provides them per-invocation.
- **GitHub Actions workflow**: uses the workflow's `GITHUB_TOKEN` (or a
  dedicated GitHub App token stored as a repo secret). The workflow file
  documents the required permissions (`issues: write`, `pull-requests: read`
  for issue discovery, `contents: read`).

## GitHub Actions workflow — `.github/workflows/atm-sync.yml`

```yaml
name: atm-sync
on:
  schedule:
    - cron: "*/5 * * * *"   # every 5 minutes
  workflow_dispatch: {}       # manual trigger from the Actions UI
  issues:
    types: [opened, edited, closed, reopened, labeled, unlabeled]
  issue_comment:
    types: [created, edited]
permissions:
  issues: write
  contents: read
concurrency:
  group: atm-sync
  cancel-in-progress: false
jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go install ./cmd/atm
      - run: atm store rebuild --project ATM
      - run: atm github sync --project ATM --repo ${{ github.repository }}
        env:
          GITHUB_TOKEN: ${{ secrets.ATM_SYNC_TOKEN }}
      - name: Commit log.jsonl
        run: |
          git config user.name  "atm-sync-bot"
          git config user.email "atm-sync-bot@users.noreply.github.com"
          git add .atm/ATM/log.jsonl
          git commit -m "chore(atm-sync): reconcile log.jsonl" || exit 0
          git push
```

### State persistence across runs — commit `log.jsonl` back to the repo

The workflow needs the project's `log.jsonl` to persist between scheduled runs.
The chosen option: **commit `.atm/ATM/log.jsonl` back to the repo at the end of
each sync run, with a machine-authored commit message.** Humans don't edit it;
`CONTRIBUTING.md` notes it's machine-maintained, like a lockfile. This is the
only way the eventsource is truly visible, versioned, and survives runner
recycling without a separate state repo.

This resolves the earlier "definitely not committed" tension — that applied to
humans hand-editing the eventsource. The machine-maintained commit is the
generated-artifact pattern (like a lockfile): visible in the repo, versioned
with code, free history via git, any clone has it, but not a file humans write.

### Webhook triggers

The `issues`/`issue_comment` triggers fire the workflow on GitHub-side
mutations, giving near-real-time ingest instead of waiting for the 5-minute
poll. Still polling-based from ATM's perspective (the workflow runs
`atm github sync`, which does a full diff), so no separate webhook-handling
code path.

### Concurrency

With `concurrency: { group: atm-sync, cancel-in-progress: false }` two syncs
don't race on the same `log.jsonl`. `cancel-in-progress: false` lets an
in-flight sync finish rather than being cancelled by a newer webhook-triggered
run.

## Testing, verification & rollout

### Testing

The adapter is two diff loops plus a GitHub API client. Tests layer as:

**Unit tests (pure diff logic, no network):**
- GitHub issue → v2 event translation: each GitHub primitive type maps to the
  expected v2 event with the right action, payload, actor, parents. Golden
  fixtures: a captured GitHub API response → the expected v2 events.
- v2 fold → GitHub API mutation diff: a folded `State` vs a GitHub state → the
  expected sequence of API calls (create issue / patch issue / add label /
  etc.). Pure diff, no real HTTP.
- Identity mapping round-trip: GitHub issue → v2 event → projected back to
  GitHub issue → same issue. The `payload.github_issue`/`github_comment`
  fields survive the round-trip.
- Idempotent re-pull / re-push: no-op when nothing changed.
- Closed/reopened issue ↔ `status:done`/`status:open` mapping, including a
  project with a custom closed-status config.
- External-id link event: ATM-first task creation → push →
  `task.linked` event emitted with `payload.github_issue` set → subsequent
  push is a no-op (the issue exists).

**Integration tests (mocked GitHub API):**
- A `httptest.Server` standing in for the GitHub API; the adapter runs full
  `pull`/`push`/`sync` against it. Verifies the actual API call sequence,
  pagination handling, etag/If-Modified-Since, and error handling (API failure
  leaves `log.jsonl` untouched on push).
- The `atm github import` onboarding path: a repo with 50 open issues +
  comments + labels ingests into an empty project; the resulting `log.jsonl`
  folds to the expected `State` with all `payload.github_issue`/
  `github_comment` links set.
- Concurrent edit conflict: GitHub-side title edit at T1 + ATM-side title edit
  at T2 → after `sync`, fold resolves to T2's title and the GitHub issue is
  updated to match.

**End-to-end test (real GitHub repo, gated):**
- A dedicated test repo (e.g. `TranDuongTu/atm-sync-test`) with a known fixture.
  The CI workflow runs `atm github sync` against it on every PR touching the
  adapter, asserts the folded state matches the expected fixture, and asserts
  the repo's issues match. Skipped if `ATM_E2E_TOKEN` is not set (so forks/PRs
  from contributors don't run it). This is the only test that hits the real
  GitHub API; everything else is mocked.

**Workflow tests:**
- `.github/workflows/atm-sync.yml` validated by `make verify`'s existing
  scripts-test (YAML lint, sanity-check the cron and permissions). The
  workflow's actual execution is exercised by the e2e test above, not by unit
  tests.

### Rollout

Layered commits, `make verify` green before each lands. Depends on
ATM-0106/0107/0108 having landed first.

1. **Actor + persona scheme** (`collaborator@github:<login>`,
   auto-registration on first sight). No external-id link events yet, no API
   calls. Pure store-level work; unit tests for actor parsing/registration.
2. **Identity mapping + GitHub API client** (the translation layer, no CLI
   yet). Unit tests with golden fixtures + `httptest` integration tests. This
   is the bulk of the adapter.
3. **`atm github pull`/`push`/`sync`/`status`/`import` CLI commands**.
   Integration tests against the mocked API. The `set-github` project config
   command.
4. **`.github/workflows/atm-sync.yml`** + the `log.jsonl` commit-back step.
   `CONTRIBUTING.md` note about the machine-maintained file.
5. **Onboarding dogfood:** run `atm github import --project ATM --repo
   TranDuongTu/atm` to ingest the ATM repo's own GitHub Issues (if any) into
   project ATM's `log.jsonl`. From that point, the ATM project is
   bidirectionally synced with its own GitHub Issues — the real dogfood.
6. **Docs:** `atm conventions` gains a "GitHub-hosted projects" section; README
   documents the `atm github` commands and the workflow.

**Verification gate:** `make verify` (`make build && make test`) remains the
gate per AGENTS.md. No new make targets.

## Out of scope (v1)

- **GitHub Projects (the kanban board feature)** — only Issues/labels/comments
  in v1. GitHub Projects has a separate GraphQL API and a different data model;
  a follow-on spec can map ATM boards to GitHub Projects columns.
- **PRs as a task surface** — issues only. PRs could map to tasks with a
  `context:repository`-style label, but the bidirectional sync semantics (PR
  review state vs task status) are non-trivial; deferred.
- **Webhook-driven real-time ingest** — the workflow already fires on
  `issues`/`issue_comment` webhook events, but the ingest is still a full diff.
  A true incremental webhook handler (one event in, one v2 event out, no full
  diff) is a follow-on optimization.
- **Comment deletion ingestion** — v1 cannot detect GitHub comment deletions
  via polling. Requires webhook handling; documented limitation.
- **Multi-repo mapping** — one ATM project ↔ one GitHub repo in v1. A project
  spanning multiple repos is a follow-on.
- **GitHub Enterprise Server** — API-compatible in principle; not tested in
  v1. The adapter uses the public GitHub API endpoints; a `--api-base` flag
  could enable GES later.
- **Label color policy** — the cosmetic color-coded namespace scheme is
  mentioned but not required; v1 uses verbatim label names with default GitHub
  colors.
- **Pre-registered GitHub→persona merge** (`atm persona create --from-github`)
  — deferred; the default `collaborator@github:<login>` is sufficient for v1.
- **ATM-0109 (L5 trust) generalization** — the GitHub persona scheme here is a
  down-payment on L5, not the full trust model. L5 may generalize
  "external-actor-as-replica" beyond GitHub.