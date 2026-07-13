# Manager action model: collapse 6 modes to Curate/Recall — design

Date: 2026-07-13
Task: ATM-0120
Status: approved (brainstormed 2026-07-13)

## Problem

The manager prompt (`internal/manager/context_v1.md`) and the `atm manage` CLI
expose six action modes — planning, grooming, tracking, asking, glossary,
onboarding — wired through six boolean flags on `atm manage`. The boundaries
between the first four are artificial: they share the same operation (read the
ledger, write the ledger) and differ only in entry condition or emphasis.

This was observed live. A manager session launched as `--planning` on
2026-07-13 immediately did grooming work (triaged ATM-0119 unlabeled, scoped
ATM-0085/0086/0087 undescribed) **and** a glossary decision (ATM-0119
`type:feature`) in the same pass. Forcing a mode switch loses continuity that
was never worth preserving.

Asking has a genuinely different write contract (synthesize, cite, do not
mutate the ledger), so its boundary is worth keeping as a mode. Onboarding is
already owned by ATM-0085's repeatable-track rework, so ATM-0120 does not touch
it (see "Onboard is deferred" below).

## Decision: 2 modes, Curate default

| Old (6) | Mode after this change | Write contract |
|---|---|---|
| Planning + Grooming + Tracking + Glossary | **Curate** (default) — new | write the ledger; glossary is a posture ("maintain vocabulary as you go"), not a mode |
| Asking | **Recall** — new | read-only synthesis; cite IDs; do not mutate the ledger |
| Onboarding | **Onboarding** — unchanged | write `context:*` index tasks + seed labels; ATM-0085 owns its rename |

**Curate** is what a manager actually does in one pass — review backlog, triage
unlabeled and under-described tasks, handle a developing-agent handoff, and
decide vocabulary. The four-way split forced mode switches that broke flow.
**Recall** stays separate because its write contract differs; that boundary is
the one worth keeping as a mode.

**Curate is the default.** When no action flag is given, `validateManagerAction`
selects Curate. This removes the footgun where the most common manager session
(curation) required an explicit flag, and matches the task's framing of Curate
as the default mode. Recall and Onboarding still require their explicit flag.

## Scope decision: delete the 5 old flags (no deprecated aliases)

ATM-0120 originally proposed keeping the six old flags as deprecated aliases
mapping onto the new modes, following the `--id`→`--task` pattern in
`internal/cli/task.go`. **This design removes that.** The five flags
`--planning`, `--grooming`, `--tracking`, `--glossary`, `--asking` are deleted
entirely. No deprecated-alias machinery, no stderr warnings, no alias mapping.
Any caller passing an old flag receives cobra's standard `unknown flag` error.

This overturns the task's original "not removing the 6 flags in this change;
removal is a separate follow-up after a release cycle" non-goal. It is
deliberate: every caller of the old flag names lives in this repo (tests,
golden, README, conventions guide), so the blast radius is fully visible and
fixable in the same change.

The "two aliases mapping to the same mode" question (e.g. `--planning
--grooming` both → curate) is moot under deletion: there are no aliases.

## Onboard is deferred to ATM-0085

ATM-0085 (approved spec, planned, not yet executed) already renames
`--onboarding` → `--mapping` and restructures the onboarding track as
verify → discover → close, with `--onboarding` kept as a hidden deprecated
alias it owns. If ATM-0120 introduced a third "Onboard" mode with a new
`--onboard` flag, the two tasks would collide on the same surface — three flag
names (`--onboard` + `--mapping` + `--onboarding`) for one mode.

To avoid that collision, ATM-0120 ships Curate + Recall only. The
`--onboarding` flag, the `managerActionOnboarding` constant, the
`BuildArgvOnboard`/`ATM_ONBOARD=1` argv/env branch (`internal/cli/manager.go:296`),
and the "Onboarding" role entry in `context_v1.md` all remain exactly as-is.
When ATM-0085 executes, it reworks the onboarding surface in the shape it
already specified; the Curate/Recall modes from this change sit alongside with
no collision.

## CLI surface

`bindManagerActionFlags` binds three flags only:

```
--curate     (default) review backlog, triage, track handoffs, maintain vocabulary
--recall     read-only synthesis grounded in ledger IDs; does not mutate
--onboarding learn a repo/project and organize it for later agents   [unchanged]
```

The `managerAction` constants collapse from 6 to 3: `curate`, `recall`,
`onboarding`. The five old constants are deleted.

`managerOpts` loses the five curation/asking bools and gains two: `Curate`,
`Recall`. (`Onboarding` stays.)

`validateManagerAction` changes shape:

- Today: require exactly one of six flags, else error.
- New: if more than one of `--curate`/`--recall`/`--onboarding` is set → error
  "choose one: --curate, --recall, or --onboarding". If none is set → Curate
  (default). `--curate` is rarely needed explicitly (it is the default) but is
  accepted; `--curate --recall` errors.

The onboarding argv/env branch (`action == managerActionOnboarding`, lines
296-308) is untouched — it keys off a constant that remains.

`ATM_MANAGER_ACTION` is set to the resolved action string
(`curate`/`recall`/`onboarding`). It is set-only today (no in-tree reader), so
accepting old values as aliases would be dead code; the new names are all any
future reader would see. The `onboarding` value is unchanged.

`internal/cli/conventions.go` updates four spots (lines 76, 80, 148, 150):

- `atm manage --project <CODE> --asking` → `atm manage --project <CODE> --recall`
- the six-flag list `--planning|--grooming|--tracking|--asking|--glossary|--onboarding`
  → `--curate|--recall|--onboarding`

`README.md` lines 30-34, 105, 120: the old flag examples collapse to
`--curate`/`--recall`; `--onboarding` stays.

## Prompt template

`internal/manager/context_v1.md`, "Your Roles" section (lines 17-24) collapses
from 6 entries to 3, with glossary folded as a posture sentence under Curate:

```
## Your Roles

- **Curate** — keep the ledger legible and current: review open backlog, triage
  unlabeled and under-described tasks, handle developing-agent handoffs, and
  maintain the project's shared vocabulary (recurring terms, short definitions,
  naming consistency) as you go. If you are not clear about what a Task should
  do, ask the user one by one to clarify.
- **Recall** — recall and link knowledge on request, grounded in cited IDs; you
  digest your own journal too, connecting related tasks and keeping them
  searchable. Read-only: synthesize and cite; do not mutate the ledger.
- **Onboarding** — when first introduced to a repo/project, learn it and
  organize it into a substrate a later agent can pick up.
```

- Planning + Grooming + Tracking + Glossary merge into **Curate**. The glossary
  responsibility ("maintain the project's shared language...") becomes a clause
  inside Curate, not a separate bullet — the "posture, not a mode" framing.
- Asking → **Recall**, with the read-only write contract made explicit.
- Onboarding entry unchanged.

`internal/manager/context.go` needs no code change: the action-block render
(`context.go:42-45`) is name-agnostic, so the three new action strings flow
through the existing `Focus this session on **{action}**.` sentence naturally.

## Testing

`internal/cli/manager_test.go` — six cases touch the old flags, all updated:

- `TestManageCodexPlanningLaunchJSON` → rename to
  `TestManageCodexCurateLaunchJSON`; flag `--planning` → `--curate`. Golden
  `manage-codex-planning-launch.json` → `manage-codex-curate-launch.json`;
  `ATM_MANAGER_ACTION: planning` → `curate`.
- `TestManageLaunchAutoCreatesProject` → `--planning` → `--curate`.
- `TestManageRequiresExactlyOneAction` → repurpose to
  `TestManageActionSelection`: the "no flag" case now *succeeds* (Curate
  default); the `--planning --grooming` case → `--curate --recall` (errors as
  conflicting).
- `TestManageRejectsDryRunAndActor` → `--planning` → `--curate` (these test
  unknown-flag rejection; the flag name is incidental).
- `TestManagePersonaEnvAndActor` → `--planning` → `--curate`; assertion
  `"ATM_MANAGER_ACTION": "planning"` → `"curate"`.
- `TestManagerCommandRemoved` → `--planning` → `--curate` (tests that the
  removed `atm manager` top-level command is gone; flag name incidental).
- `TestManageOllamaOnboarding`, `TestManagerOnboardEnvHasATMOnboard`,
  `TestManagerOnboardArgvUsesAutoPrompt` → unchanged (use `--onboarding`).

New cases added:

- `TestManageCurateIsDefault` — `atm manage --project FOO` (no action flag)
  succeeds and `ATM_MANAGER_ACTION=curate`.
- `TestManageRejectsConflictingActions` — `--curate --recall` errors.
- `TestManageOldFlagsRemoved` — `--planning`, `--grooming`, `--tracking`,
  `--glossary`, `--asking` each fail as unknown flags (regression guard against
  accidental re-introduction).

`internal/manager/context_test.go`:

- `TestRenderContextActionCatalogPresent` (lines 47-54) asserts
  `{Tracking, Asking, Glossary, Onboarding}` present → change to
  `{Curate, Recall, Onboarding}`. Remove the now-stale fragments.
- Other cases (placeholder substitution, principles, generic placeholders,
  persona) unchanged.

`internal/cli/testdata/golden/`: rename `manage-codex-planning-launch.json` →
`manage-codex-curate-launch.json`; update the `ATM_MANAGER_ACTION` value
inside.

No new test for env old-value acceptance — `ATM_MANAGER_ACTION` is set-only.

## Non-goals

- **No deprecated aliases.** The 5 old flags are deleted, not aliased. This
  overturns ATM-0120's original "keep as deprecated aliases" framing.
- **No Onboard collapse this change.** The third mode is deferred to ATM-0085.
  `--onboarding`, `managerActionOnboarding`, `BuildArgvOnboard`, `ATM_ONBOARD=1`,
  and the "Onboarding" role entry stay exactly as-is.
- **No ATM_MANAGER_ACTION old-value acceptance.** It is set-only; accepting
  legacy values is dead code.
- **No change** to manager persona/principles (`context_v1.md` lines 10-15),
  the action-block render code in `internal/manager/context.go`, the
  atm-manager subagent thin-pointer contract (it calls
  `atm manage-context`; the render path is unaffected), or the store data model.
- **No TUI changes.** The TUI does not surface manager action flags.

## Cross-refs

- Supersedes ATM-0120's original "deprecated aliases" framing (scope decision
  above).
- Defers to ATM-0085 for the onboarding-surface rename and repeatable-track
  rework; ATM-0120 explicitly leaves `--onboarding` alone so the two tasks do
  not collide.
- ATM-0098 (manager "asking" capability design) — Recall is the home for the
  asking cognition ATM-0098 specifies; coordinate so ATM-0098's
  recall+synthesis work lands under the Recall name. The prompt entry here
  names Recall; ATM-0098 owns the deeper cognition design.
- Supersedes the six-action framing in
  `2026-07-08-manager-knowledge-base-onboarding-unification-design.md` and the
  "make manager sessions explicit by requiring exactly one action flag" line in
  `2026-07-11-cli-user-surface-simplification-design.md` (Curate becomes the
  default, so "exactly one" no longer holds).

## Verify gate

`make verify` (build + test + golden + conventions guide round-trip).