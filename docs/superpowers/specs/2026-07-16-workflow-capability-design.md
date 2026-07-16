# Workflow capability — Design Spec

**Status:** Approved (all five design sections) 2026-07-16.
**Date:** 2026-07-16
**Task:** ATM-e23fe5 (scope split off from ATM-18111b, which retains the recently-updated-tasks-board half)
**Supersedes the vocabulary-only role of:** `internal/workflow` (defined in `2026-07-15-tui-tasks-boards-merge-design.md`).

## Driver

After the Boards/Tasks merge (ATM-2412f2), the TUI always has a SELECTED board, defaulting to `ATM:open-tasks` (`status:open`). A task created with `a` and no labels — a quick jotting — carries no `status:*` label and is therefore invisible under every board in the ring. Jottings vanish from the default view.

At the same time, status transitions today are raw substrate ops (`atm task label add/remove --label ATM:status:<value>`), so agent prompts hardcode `status:*` label strings — the exact hardcoding the capability pattern (`docs/architecture/label-substrate-and-capabilities.md`) exists to prevent.

## Scope

Promote `internal/workflow` from a vocabulary-only capability into a full capability that owns the status-transition paved road, mirroring `internal/contextmap`:

- Three ensured boards: `ATM:backlog` (`NOT status:*`), `ATM:open-tasks` (`status:open`), `ATM:in-progress-tasks` (`status:in-progress`).
- Intent-level transition verbs under a new `atm workflow` top-level command (separate from the substrate-level `atm task`).
- A read-only `status` reporter.
- Conventions updated: workflow lives in **capabilities** (the paved road), the store stays neutral; a project can replace `internal/workflow` with a different transition model.

## Non-Goals

- No store API changes (uses existing `TaskLabelAdd` / `TaskLabelRemove` / `GetTask` / `LabelSeed`).
- No state machine in the store; no enforcement. The capability is a paved road, not a fence — raw `atm task label add/remove --label status:*` still works.
- No new status values (uses the four seeded: open, in-progress, blocked, done).

  **Amendment 2026-07-16:** an earlier draft of this spec claimed five seeded values, including `todo`, and defined an `atm workflow queue` verb targeting it. That premise was false: `internal/seed/seed.go` seeds only open, in-progress, done, blocked, and `status:todo` was *deliberately retired* — `internal/seed/seed_test.go` (`TestDroppedNamespacesAbsent`) guards its absence. Since `store.TaskLabelAdd` does not require a label to be registered, a `queue` verb would have silently minted an undescribed `status:todo` label, violating ATM's own label-hygiene code-of-conduct. Decision (project owner, 2026-07-16): drop the `queue` verb and `StatusTodo`; the capability ships four verbs.
- No rename or redefinition of `open-tasks` (deferred to a separate design).
- No TUI board-ring changes beyond the new boards appearing as normal ring members.

## Command surface (`atm workflow`)

A new top-level command `atm workflow`, paralleling `atm context`. The core `atm task` stays substrate-level (raw label ops). All verbs take a task id (positional or `--task`); all mutating verbs require `--actor` (via `requireMutatingActor`, like `atm context add`).

### Mutating verbs (scrum intent, swap semantics)

Each verb resolves the task's project, computes the prefixed target label (`<CODE>:status:<value>`), **adds the target, then removes every other** `<CODE>:status:*` label on the task — a swap via existing store calls. The store permits multiple `status:*` labels on a hand-edited task, so the recorder scans and removes **all** other matching `<CODE>:status:*` labels, restoring the exactly-one invariant.

**Amendment 2026-07-16 (add-before-remove).** An earlier draft specified remove-then-add. Review found that ordering unsafe: the store has no transactions, and `TaskLabelAdd` runs its own validation *after* the removes would already have landed — so a failed add (bad target, or a genuine I/O error) deterministically left the task with **no status label at all**, silently dropping it off every board. Add-before-remove has a strictly better failure mode: if the add fails, nothing was removed and the task keeps its original status; if a later remove fails, the task carries the target plus a leftover — the exactly-one invariant is violated but no status is lost, and re-running the verb converges. This buys the safety without the enum-validation surface the Non-Goals rule out. Decision: project owner, 2026-07-16. No-op (and an "already <status>" message) when the task already carries the target status as its sole status.

| Verb | Target status | One-line intent |
|---|---|---|
| `atm workflow start <id>` | in-progress | someone is now on this |
| `atm workflow open <id>` | open | (re)open for consideration |
| `atm workflow block <id>` | blocked | cannot proceed pending something else |
| `atm workflow complete <id>` | done | finished |

On success, each prints a single line: `<id>: status -> <value>` (or `<id>: status <prior> -> <value>` when a prior status was swapped out), and emits the updated task JSON when `--output json` is set, matching the `atm task label add` shape.

### Reporter (read-only)

`atm workflow status <id>` — prints the task's current status value, or `untriaged` when no `status:*` label is present. Pure: store byte-identical before and after (testable, like `atm context check`).

### Vocabulary ensure

`atm workflow seed --project <CODE>` — ensures all three boards idempotently (`backlog`, `open-tasks`, `in-progress-tasks`) with descriptions, via `workflow.EnsureVocabulary`. Self-bootstrapping; does not assume `atm label seed` ran. This is the CLI entry point for non-TUI users; it is also called from `atm project create` (right after `CreateProject`) and `atm label seed --project` (alongside the default seed labels), replacing the current `workflow.EnsureVocabulary` call sites that only ensured `open-tasks`.

### Argument resolution and errors

- Task id resolution reuses `resolveTaskID` (handles `--task` / `--id` / legacy). Project code is derived from the task id prefix (the existing `taskProjectFormat` path) — no `--project` flag needed on the verbs, since a status label is always project-scoped and the id carries the project.
- Unknown status target is a programming error (the verbs are fixed to the four seeded values) — never user-supplied, so no enum validation surface.
- Errors from the store propagate as-is; the capability adds no validation of its own beyond "swap the status label."

## Capability internals (`internal/workflow`)

`internal/workflow` grows from vocabulary-only to a full capability, mirroring `internal/contextmap`'s recorder/reporter split. All label names and expressions live here; the CLI verbs and the TUI never reference `status:*` or `status:open` directly.

### Vocabulary

Three boards, each a normal label with an expression (capability = paved road; a human can edit or delete any of them):

```go
// internal/workflow/vocabulary.go
func BoardBacklog(code string) string         { return code + ":backlog" }
func BoardOpenTasks(code string) string       { return code + ":open-tasks" }        // existing
func BoardInProgressTasks(code string) string { return code + ":in-progress-tasks" }

func backlogExpr() string         { return "NOT status:*" }
func openTasksExpr() string       { return "status:open" }                  // existing
func inProgressTasksExpr() string { return "status:in-progress" }

func EnsureVocabulary(s *store.Store, code, actor string) error {
    for _, b := range []struct{ name, desc, expr string }{
        {BoardBacklog(code),         "tasks with no status label: incoming jottings awaiting triage. Review queue alongside open-tasks.", backlogExpr()},
        {BoardOpenTasks(code),       "every open task: the project's active work. Default board in the TUI.", openTasksExpr()},
        {BoardInProgressTasks(code), "tasks someone is actively working on (status:in-progress).", inProgressTasksExpr()},
    } {
        if err := s.LabelSeed(b.name, b.desc, b.expr, actor); err != nil {
            return err
        }
    }
    return nil
}
```

`LabelSeed` upserts only when absent — a human's curated description is never overwritten, matching the existing contract.

### Status constants (private to the capability)

```go
// internal/workflow/status.go
const (
    StatusOpen       = "open"
    StatusInProgress = "in-progress"
    StatusBlocked    = "blocked"
    StatusDone       = "done"
)

// StatusNamespace is the label suffix prefix this capability owns.
const StatusNamespace = "status"
```

These are the only place the string `"status"` appears in the capability. CLI verbs and the TUI reference `workflow.StatusInProgress` etc., never the literal.

### Recorder — the transition verbs

```go
// internal/workflow/recorder.go
type Recorder struct {
    Store *store.Store
    Actor string
}

// SetStatus swaps the task's status label to target. No-op when already there.
// Adds the target first, then removes every other <code>:status:* label, so a
// failure never leaves the task without a status.
// Returns the prior status value (bare, e.g. "open") or "" if untriaged.
func (r *Recorder) SetStatus(taskID, target string) (prior string, err error)
```

- Resolves the task's project code via `Store.GetTask` + the id prefix.
- Scans the task's labels for **all** matching `<code>:status:*` (a hand-edited task may carry several) and records `prior` as the first non-target one. The store returns labels sorted lexicographically (`internal/store/cache.go` `ORDER BY label`), so `prior` is the alphabetically-first non-target status — **not** necessarily the most recently set one.
- If the target is already present as the sole status, returns `prior == target` and does nothing (zero store calls; the log must not advance).
- Adds `<code>:status:<target>` via `TaskLabelAdd` **first** (skipped when already present), then removes every other matching label (one `TaskLabelRemove` per match). On a well-formed single-status task this is one add and one remove.
- `prior` is the bare value removed (e.g. `"open"`), or `""` if the task was untriaged. The CLI uses it for the `<id>: status <prior> -> <target>` line. When the task had multiple status labels (hand-edited), `prior` is the first non-target removed; the swap line reports the transition from that value.

The four scrum verbs are thin wrappers:

```go
func (r *Recorder) Start(taskID string) (string, error)    { return r.SetStatus(taskID, StatusInProgress) }
func (r *Recorder) Open(taskID string) (string, error)     { return r.SetStatus(taskID, StatusOpen) }
func (r *Recorder) Block(taskID string) (string, error)    { return r.SetStatus(taskID, StatusBlocked) }
func (r *Recorder) Complete(taskID string) (string, error) { return r.SetStatus(taskID, StatusDone) }
```

### Reporter — read-only status

```go
// internal/workflow/reporter.go
type Reporter struct{ Store *store.Store }

// Status returns the task's status value, or "" when untriaged.
func (r *Reporter) Status(taskID string) (string, error)
```

Scans the task's labels for `<code>:status:*`; returns the bare value or `""`. Pure — never mutates; store byte-identical before and after (testable, like `contextmap`'s reporter contract).

### CLI wiring

A new `internal/cli/workflow.go` builds the `atm workflow` command tree. Each mutating verb: `resolveTaskID` -> `requireMutatingActor` -> `openStore` -> `Recorder{...}.Start/Open/...` -> print the swap line + JSON emit. The reporter: `resolveActor(true)` (defaults allowed) -> `openStore` -> `Reporter.Status` -> print the value or `untriaged`. `seed`: `requireMutatingActor` -> `openStore` -> `EnsureVocabulary`.

The existing `workflow.EnsureVocabulary` call sites (`cli/project.go:46`, `cli/label.go:164`, `tui/projects.go:243`, `tui/app.go:186`) become calls to the same `EnsureVocabulary` (now ensuring all three boards). The `workflow.BoardOpenTasks` reference in `tui/labels.go:259` (default selection) is unchanged — `open-tasks` stays the default SELECTED board; `backlog` and `in-progress-tasks` enter the ring as normal members.

### What stays out of core

The store gains nothing. `TaskLabelAdd` / `TaskLabelRemove` / `GetTask` / `LabelSeed` are the entire surface the capability uses. The swap is two store calls in the recorder (add, then remove); the "exactly one status" invariant is maintained by the capability, never enforced by the store. The recorder is not atomic — the store has no transactions — but add-before-remove bounds the worst case to a recoverable extra label rather than a lost status. A human using raw `atm task label add --label ATM:status:done` on an in-progress task still produces two status labels — the store permits it; only `workflow`'s verbs guarantee the swap. This is the paved-road-not-a-fence contract.

## Conventions update

`atm conventions` (and its JSON testdata) gains a workflow paragraph and a first-contact-sequence step, reflecting that the paved road now lives in a capability, not in agent habits. The "no state machine" wording about the **store** stays — the store remains neutral — but workflow itself is no longer "outside the store in agent prompts"; it's in `internal/workflow`, replaceable.

### New conventions text (the workflow paragraph)

> **Workflow verbs (status transitions).** Status transitions live in the `internal/workflow` capability, exposed as `atm workflow` verbs — `start` (in-progress), `open`, `block`, `complete` (done) — plus a read-only `status` reporter. Each verb swaps the task's `status:*` label (removes any existing one, adds the target), so exactly-one-status is an invariant the capability maintains. The store still enforces nothing: raw `atm task label add/remove --label <CODE>:status:<value>` works and a human may hand-assign, rename, or delete any status label. `internal/workflow` is a paved road, not a fence — a project can replace it with a different transition model. Three boards are ensured on project create / label seed / TUI use: `ATM:backlog` (`NOT status:*` — untriaged jottings), `ATM:open-tasks` (`status:open` — active work, the TUI default), `ATM:in-progress-tasks` (`status:in-progress`). In an older project where a board is absent, the expression fallback applies (`--label <CODE>:status:open` etc.).

### Edits to existing paragraphs

- **"What ATM is"** — keep "no state machine" as a description of the *store*, but soften "Workflow lives outside the store, in agent prompts and human habits" to: "Workflow lives in capabilities (`internal/workflow`), not in the store; the store only keeps the substrate legible. A capability is a paved road, not a fence — a project can replace it." The "no privileged namespaces" invariant is unaffected.
- **First-contact sequence** — gains a step: "`atm workflow status <task-id>` / `atm workflow start <task-id>` — the paved road for status transitions; prefer these over raw `task label add/remove --label status:*`." Placed after the existing `atm task list --label <CODE>:open-tasks` step.
- **"How to search"** — the `open-tasks` fallback line stays; a parallel line names `backlog` (`--label <CODE>:backlog` returns untriaged tasks; in an older project, `--expr 'NOT status:*'` is equivalent).

### JSON testdata

The `internal/cli/conventions.go` string map updates the matching keys (`what_atm_is`, first-contact array) and adds a `workflow_verbs` key. Golden testdata updates accordingly (the existing golden-test pattern for conventions).

## Testing

Three test layers, mirroring `internal/contextmap` and the existing conventions golden tests.

### `internal/workflow`

- `EnsureVocabulary` is idempotent; creates all three boards with the right expr/desc in a fresh project; does not overwrite a human-curated description; works without `atm label seed` (self-bootstrapping).
- `Recorder.SetStatus`:
  - Swaps an existing status to a new one (prior returned, exactly one `status:*` label after).
  - No-op when already at the target (prior == target, no mutation, no log entry).
  - Sets a status on an untriaged task (prior == "", target added).
  - On a task carrying a non-status label (e.g. `priority:high`), only the status changes; the other label survives.
  - On a task hand-edited to carry multiple `status:*` labels (e.g. `status:open` + `status:done`), `SetStatus` removes **all** of them and adds the target, leaving exactly one status label.
  - Resolves the project from the task id prefix; rejects an unknown task id.
- `Reporter.Status`:
  - Returns the value for a task with a status; returns `""` for an untriaged task; returns `""` (not an error) for a task with only non-status labels.
  - **Purity**: store byte-identical before and after `Status` runs (read the event log, run, re-read, compare) — the same contract `contextmap`'s reporter test enforces.
- The four scrum verbs (`Start` / `Open` / `Block` / `Complete`) map to the right target status (table-driven).

### `internal/cli` (`atm workflow`)

- Each verb resolves `--task` / `--id` (and legacy), requires `--actor` (errors without it), swaps the status, prints the `<id>: status <prior> -> <value>` line, and emits valid JSON under `--output json`.
- `atm workflow status <id>` prints the value or `untriaged`; read-only (no log mutation).
- `atm workflow seed --project <CODE>` ensures all three boards; re-running is a no-op; works on a project that never ran `atm label seed`.
- `atm project create` leaves the new project with all three boards; `atm label seed` ensures them on an existing project.
- Conventions text + JSON reference the workflow verbs and the three boards; golden testdata updated.
- Determinism / lexicographic test (if one exists for boards) updated to account for the two new ensured boards.

### `internal/tui`

- On project select, `EnsureVocabulary` ensures all three boards; `backlog` and `in-progress-tasks` appear as normal ring members (the ring is the existing `buildBoardRows` output; no new code needed beyond the ensure call already wiring all three).
- The default SELECTED board is still `open-tasks` (via `BoardOpenTasks`); the new boards do not displace it.
- A naked task (created with `a`, no labels) appears under the `backlog` board's task list but not under `open-tasks`. (Existing `focusUnlabeled` test at `tasks_test.go:204` is the substrate precedent; here we assert via the board ring.)
- Drilling the `backlog` thumbnail (a leaf board, expression `NOT status:*`) shows its label detail; the task list follows its focus.
- Existing boards-merge tests still pass (no ring/order regressions from the two new boards).

## Verification

Before declaring implementation complete, run:

```sh
make verify
```