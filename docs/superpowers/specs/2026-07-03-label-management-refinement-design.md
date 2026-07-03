# Label Management Refinement — Design Spec

**Status:** Approved
**Date:** 2026-07-03
**Supersedes (in part):** `2026-07-02-tasks-management-v2-design.md` Sections 3, 6, 7 (label surface, TUI Projects-tab labels pane, onboarding/conventions). The v2 spec's data model, store API, and non-label surfaces are unchanged; this spec refines only the label management UX, seeding, and conventions content.

## Driver

The v2 spec made labels the single dynamic substrate and put label add/remove on the Projects tab detail view, with task creation collecting only a title and description. In practice this created three gaps:

1. **Labels were managed in the wrong place.** The Projects tab is about project lifecycle (create, name, remove); label curation is a distinct, ongoing activity that deserves its own surface. Co-locating them made the project detail view carry a large labels section that crowded out project facts.
2. **Task creation couldn't label at creation time via the TUI.** The CLI already supported repeatable `--label`, but the TUI's task-create form collected only title + description, forcing a second step (`[b]` add label) per label. For agents and humans, labeling at creation is the natural moment.
3. **A fresh project started label-less.** The v2 spec's answer was "onboarding is seeding index tasks, which populates namespaces organically." But a fresh agent landing in an empty project had no labels to read — the very first `atm label list` returned nothing, undercutting the "read labels first" workflow. Default labels with descriptions should exist from the moment a project is created.

This spec closes all three: a dedicated Labels tab (scoped, editable), multi-label task creation, auto-seeding of a documented default label set on project create (plus an on-demand re-seed), and a rewritten conventions guide that adds an agent code-of-conduct and a "understand labels first" first-contact sequence.

## What changes and why

### 1. A dedicated Labels tab (TUI)

The TUI gains a fourth tab, **Labels**, between Tasks and Help. Tab order: `1 Projects · 2 Tasks · 3 Labels · 4 Help`. Labels sits between Tasks and Help because it is a management surface the human consults while steering tasks; Help stays last.

**Scope:** The Labels tab shows labels for the currently selected project (`Model.projectScope`). When no project is selected, it shows an empty state mirroring the Tasks tab pattern: `no project selected` / `press [s] in the Projects tab to scope this view`.

**List view (project selected):** rows grouped by namespace (alphabetical; unnamespaced tags under a `tags:` heading), each row showing `<suffix> (N tasks)  <description>`. A header line gives the column legend. Keys:
- `j/k/g` navigate.
- `[a]dd` opens a form with a single `name` field (suffix only — the project prefix is fixed and auto-prepended, matching the existing task-detail `[b]` flow). The suffix is validated against the existing `labelSuffixRe` (`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`). No description field here — descriptions are set via `[d]`. Auto-registers the label with empty description.
- `[d]escribe` opens a form with a `description` field pre-filled with the current description. Calls `store.LabelAdd(full, newDesc, actor)` — the upsert that deliberately overwrites the description. This is the only TUI path that sets/edits a label's description.
- `[l] remove` opens a form with a `name` field (suffix). Calls `store.LabelRemove`. Toast: `removed label <full> (retained usage: N)`.
- `[S]eed` applies the default seed set via `store.SeedLabels(code, actor)`. Idempotent — preserves existing descriptions. Toast: `seeded 17 labels into <CODE>`.
- `Enter` opens a label detail view (full name, description, usage count). `[Esc]` returns to the list.

**Status hint (list):** `[a]dd [d]esc [l]remove [S]eed [Enter]detail [?]keys`.
**Status hint (detail):** `[d]esc [l]remove [Esc]back`.

### 2. Projects tab loses its label management surface

The Projects tab detail view drops its entire LABELS section (the header, separator, namespace grouping, and per-label usage rows) and the `[L]`/`[l]` keys. The project detail renders PROJECT facts + HISTORY (toggled by `H`) only. The `labels    %d` count line in the facts block stays — it's a useful at-a-glance counter; the detail/edit lives in the Labels tab.

The `openLabelAddForm`/`openLabelRemoveForm`/`doLabelAdd`/`doLabelRemove` helpers and the `formLabelAdd`/`formLabelRemove` form kinds move to the Labels tab (`labels.go`). The `labelSuffixRe` validator is shared.

### 3. Task creation collects multiple labels (TUI)

The Tasks tab task-create form gains a third field:

```
labels   (optional) space-separated suffixes, e.g. 'status:open type:bug' (prefix auto-added)
```

The field is optional and validated per-token against `labelSuffixRe`. On submit, `doTaskCreate` splits the field on whitespace, builds full names (`projectScope + ":" + token`), and passes the slice to `store.CreateTask`, which auto-registers any not in the registry with empty description. The human/agent describes them later in the Labels tab. Empty field → no labels (unchanged behavior).

The CLI `task create --label` flag is unchanged (already repeatable, already auto-registers).

### 4. Default labels are seeded on project create and on demand

**New package `internal/seed`** holds the single source of truth for the default label set:

```go
package seed

type Label struct {
    Suffix      string
    Description string
}

var Labels = []Label{
    {"status:open",          "workflow state: open; task is not started or is being considered"},
    {"status:todo",          "workflow state: todo; task is queued for work"},
    {"status:in-progress",   "workflow state: in-progress; someone is actively working on this"},
    {"status:done",          "workflow state: done; task is complete"},
    {"status:blocked",       "workflow state: blocked; task cannot proceed pending something else"},
    {"status:review",        "workflow state: review; task is awaiting review/approval"},
    {"type:bug",             "task categorization: bug; a defect to fix"},
    {"type:feature",         "task categorization: feature; new functionality to add"},
    {"type:task",            "task categorization: task; general work item"},
    {"type:chore",           "task categorization: chore; maintenance, refactoring, tooling"},
    {"priority:high",        "optional prioritization: high"},
    {"priority:medium",      "optional prioritization: medium"},
    {"priority:low",         "optional prioritization: low"},
    {"context:documentation","the labeled task contains documentation about the project"},
    {"context:repository",   "the labeled task contains a pointer to a code repository"},
    {"context:agent",        "agent direction when navigating the project; read these to understand how to work in this project"},
    {"context:fixit",        "something on this task should be reviewed, updated, or altered"},
}
```

17 labels. Each carries a description so a fresh agent reading `atm label list --project <CODE>` sees meaningful text immediately. Templated namespaces (`repo:<name>`, `doc:<name>`, `claimed-by:<agent>`, `blocks:<ID>`, `related:<ID>`) are intentionally NOT seeded as concrete labels — they depend on project-specific values and are created on demand.

**Layering:** `internal/seed` holds only the data. `internal/store` imports `seed` and implements `SeedLabels(code, actor)` using the list. No seed→store import, no cycle.

**Store changes (`internal/store`):**

- `CreateProject` calls `s.SeedLabels(code, actor)` after the project file is committed (outside `CreateProject`'s `WithLock` block; `SeedLabels` takes its own project lock). A fresh project has all 17 default labels with descriptions the moment it exists.
- New `LabelSeed(name, description, actor) error`: upserts a label but **only sets the description when the label is newly created**. Existing labels keep their descriptions — this preserves human edits on re-seed. (Contrast with `LabelAdd`, which overwrites the description when the new one is non-empty and differs — `LabelAdd` remains the CLI `label add --desc` and TUI `[d]` path that deliberately overwrites.)
- New `SeedLabels(code, actor) error`: iterates `seed.Labels`, calls `LabelSeed(code+":"+l.Suffix, l.Description, actor)`. Idempotent.

**CLI:** new `atm label seed --project <CODE>` subcommand. Required `--project`; `--actor` via `resolveActor(true)`. Calls `s.SeedLabels(code, actor)`.
- Text output: `seeded 17 labels into <CODE>`.
- JSON output: `{"project":"ATM","seeded":17,"labels":["ATM:status:open",...]}` — lists the full names applied, for agent consumption.
- Exit 0 on success; standard error codes (2 usage, 3 not-found) on failure.

**TUI:** Labels tab `[S]` key calls `store.SeedLabels(code, actor)`. Toast: `seeded 17 labels into <CODE>`. Refresh.

### 5. Conventions rewritten (agent code-of-conduct + "understand labels first")

`internal/cli/conventions.go` `conventionsText` and `conventionsStructured()` are rewritten.

**Seed-namespace table (updated):**

| Namespace | Examples | Purpose |
|-----------|----------|---------|
| `status:` | open, todo, in-progress, done, blocked, review | workflow states — labels only, no state machine |
| `type:` | bug, feature, task, chore | task categorization |
| `priority:` | high, medium, low | optional prioritization |
| `context:documentation` | ATM:context:documentation | the labeled task contains documentation about the project |
| `context:repository` | ATM:context:repository | the labeled task contains a pointer to a code repository |
| `context:agent` | ATM:context:agent | agent direction when navigating the project |
| `context:fixit` | ATM:context:fixit | something on this task should be reviewed, updated, or altered |
| `repo:<name>`, `doc:<name>` | ATM:repo:atm, ATM:doc:architecture | index tasks pointing at repos/docs — created on demand, not seeded |
| `claimed-by:<agent>` | ATM:claimed-by:claude | who's working on what — last-writer-wins, no conflict detection |
| `blocks:<ID>`, `related:<ID>` | ATM:blocks:ATM-0002 | task relationships via labels — created on demand, not seeded |

**New section: Agent code-of-conduct (label hygiene)**

Agents working in an ATM project follow these rules to keep the label substrate legible for humans and other agents:

1. **Read before you write.** Run `atm label list --project <CODE>` and read every label's description before introducing any new label. The existing labels are the project's vocabulary; reuse them whenever one fits your intent.
2. **Default setup is the baseline.** The seeded labels (status, type, priority, context) cover the common cases. Prefer them. Do not reinvent `status:open` as `state:open` or `wf:open`.
3. **Invent only when nothing fits.** If no existing label captures your intent, you may create a new one — agents are free to self-organize. But before you do, ask yourself: would a human reviewing the Labels tab understand why this label exists?
4. **State the intention in the label description.** When you create a new label, also call `atm label add --name <CODE>:<ns>:<value> --description "<one sentence: why this label exists>"`. The description is the intention record. A label with no description is a flag for human review: "agent introduced this but didn't explain why."
5. **One label, one meaning.** Don't use the same label string to mean different things across tasks. If your intent diverges from an existing label's description, create a new label with a distinct name and a description that distinguishes it.
6. **Humans reconcile.** The Labels tab is the human's review surface. If you see labels that overlap, contradict, or lack descriptions, edit or remove them there. Agents follow the rules above; humans curate.

**Updated agent first-contact sequence:**

1. `atm conventions` — read this guide, including the code-of-conduct.
2. `atm label list --project <CODE>` — **read every label's description first** to understand the project's vocabulary before exploring tasks. Labels are the project's language; knowing them makes every task query meaningful.
3. `task list --project <CODE> --label <CODE>:context:agent` — get agent directions for working in this project.
4. `task list --project <CODE> --label <CODE>:context:repository` / `:context:documentation` — discover repository pointers and documentation.
5. `task list --project <CODE> --label <CODE>:status:open` — get open work.

**First-time human sequence (updated):** unchanged structure (atm tui → create project → create seed index tasks and work tasks, labeling as you go), with the note that project create now auto-seeds the 17 default labels with descriptions, so the Labels tab is populated from the start and the human curates from there.

**Notes section:** mentions `atm label seed --project <CODE>` / Labels tab `[S]` for re-applying defaults after an upgrade (idempotent; preserves edited descriptions).

**`conventionsStructured()` JSON:** adds `code_of_conduct` (array of rule strings), updates `namespaces`, updates `agent_first_contact_sequence`, adds `seeded_labels` (array of `{suffix, description}`) so agents can programmatically read the seed set.

### 6. Help tab + keymap updated

The Help tab parity table and keymap table reflect the new surface:
- `atm label add/remove` rows point to the Labels tab (not Projects detail).
- `atm label seed --project` → `Labels tab [S]` row added.
- `atm task create --project --title [--label]` row notes labels can be supplied at create time.
- The `L/l` Projects-detail keymap rows are removed; Labels-tab rows (`a`/`d`/`l`/`S`/`Enter`) are added.

## Data model

Unchanged from the v2 spec. No new fields on `Project`, `Task`, `Label`, or `HistoryEntry`. The seed set is a code constant, not stored data. Seeding writes ordinary `Label` entries into the existing `labels.json`.

## Store API surface

Additions only (no removals, no signature changes to existing methods):

```go
// LabelSeed upserts a label but only sets the description when the label is
// newly created. Existing labels keep their descriptions (preserves human
// edits on re-seed). Used by SeedLabels.
func (s *Store) LabelSeed(name, description, actor string) error

// SeedLabels applies the default seed labels (internal/seed.Labels) to the
// project. Idempotent; preserves existing descriptions. Called by
// CreateProject and by the CLI/TUI on-demand seed path.
func (s *Store) SeedLabels(code, actor string) error
```

`CreateProject` gains an internal seeding step (no signature change): after the project file is committed and the tasks dir created, it calls `s.SeedLabels(code, actor)`. If seeding fails, the project is already created; the error is returned and the user can re-seed via `atm label seed`. Seeding occurs outside `CreateProject`'s `WithLock` block; `SeedLabels` takes its own project lock.

All other store methods are unchanged.

## CLI surface

```
atm label seed  --project <CODE> [--actor <id>]              # idempotent; (re)applies default labels
```

All other commands unchanged. `atm label add --name --description` keeps its `--description` flag (the CLI path for setting descriptions; the Labels tab `[d]` is its TUI mirror). `atm task create --project --title [--label]... [--description] [--actor]` unchanged (already repeatable).

## TUI surface

Four tabs: **Projects**, **Tasks**, **Labels**, **Help**. `numPanes` 3→4; `paneLabels` between `paneTasks` and `paneHelp`. Tab bar: `1 Projects  2 Tasks  3 Labels  4 Help`. Key `3` → Labels, `4` → Help.

**New file `internal/tui/labels.go`** — the Labels tab model (list + detail), forms (add/describe/remove), and the seed key handler. Reuses `labelSuffixRe`, the `Form` machinery, and the existing `formLabelAdd`/`formLabelRemove` form kinds (repurposed; `formPayload` = project code). New `formLabelDescribe` form kind for the `[d]` edit-description form.

**`internal/tui/projects.go`** — `handleDetailKey` loses `case "L"` and `case "l"`; `renderDetail` loses the entire LABELS block; `statusHint` detail drops the label keys. The `openLabelAddForm`/`openLabelRemoveForm`/`doLabelAdd`/`doLabelRemove` helpers are removed from `projects.go` (moved to `labels.go`).

**`internal/tui/tasks.go`** — `openCreateForm` gains the `labels` field; `doTaskCreate` parses it into a slice and passes it to `store.CreateTask`.

**`internal/tui/keymap.go`** — `L/l` Projects-detail rows removed; Labels-tab rows added (`a` add, `d` describe, `l` remove, `S` seed, `Enter` detail).

**`internal/tui/help.go`** — parity table and keymap table updated.

## Testing, verification & rollout

**New tests:**

- `internal/seed/seed_test.go`:
  - `TestLabelsNonEmpty` — the list has ≥1 entry.
  - `TestLabelsAllValidSuffixes` — each suffix matches `^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`.
  - `TestLabelsNoDuplicates` — no two entries share a suffix.
  - `TestLabelsDescriptionsNonEmpty` — every entry has a non-empty description (the code-of-conduct requires it; this test guards the rule).
- `internal/store/project_test.go`:
  - `TestCreateProjectSeedsLabels` — after `CreateProject("ATM", ...)`, `LabelList("ATM","")` returns all 17 seed labels, each with its expected description.
  - `TestSeedLabelsIdempotentPreservesDescriptions` — seed a project, edit one label's description via `LabelAdd`, call `SeedLabels` again, assert the edited description is preserved and the other 16 are unchanged.
- `internal/store/label_test.go`:
  - `TestLabelSeedPreservesExistingDescription` — `LabelAdd("ATM:type:bug","custom")` then `LabelSeed("ATM:type:bug","seed desc")` → description stays "custom".
  - `TestLabelSeedSetsDescriptionOnCreate` — `LabelSeed("ATM:new:x","desc")` on a fresh label → description is "desc".
- `internal/cli/label_test.go`:
  - `TestLabelSeedCommand` — text output `seeded 17 labels into ATM`; JSON output has `project`, `seeded: 17`, `labels` array of 17 full names.
- `internal/cli/conventions_test.go`:
  - Golden files `conventions-text` and `conventions-json` regenerated.
  - New assertions: text contains "Agent code-of-conduct", "read every label's description first"; JSON contains `code_of_conduct` and `seeded_labels` keys.
- `internal/tui/labels_test.go` (new):
  - `TestLabelsTabEmptyState` — no project selected → empty-state text rendered.
  - `TestLabelsTabList` — seed a project → 17 rows grouped by namespace; each row shows suffix, usage (0), description.
  - `TestLabelsTabAddLabel` — `[a]` form, type `patch:urgent`, submit → row appears in the `patch:` namespace group.
  - `TestLabelsTabDescribeLabel` — `[d]` form on an existing label, change description → row updates.
  - `TestLabelsTabRemoveLabel` — `[l]` form, remove a label → row gone; toast shows retained usage.
  - `TestLabelsTabSeedKey` — remove one seed label, press `[S]` → it returns with the seed description (not overwriting any edits to the other 16).
  - `TestLabelsTabDetail` — `Enter` on a row → detail view shows full name + description + usage.
- `internal/tui/tasks_test.go`:
  - `TestTaskCreateWithLabels` — form with `labels` field = "status:open type:bug" → task created with both labels; both auto-registered in the registry.
- `internal/tui/app_test.go` (or `projects_test.go`):
  - `TestProjectDetailNoLabelKeys` — pressing `L`/`l` in project detail is a no-op (no form opens).
  - `TestProjectDetailNoLabelsSection` — view snapshot assert the detail render contains no `LABELS` header.
- `internal/tui/help_test.go` (if present) or `app_test.go`:
  - Help tab view snapshot asserts the updated parity table and keymap.

**Updated golden files:** `conventions-text`, `conventions-json`, help tab snapshot, parity table.

**Verification gate:** `make verify` (runs `make build && make test`). No new make targets.

**Rollout:** single-PR incremental commits, no data migration:
1. Add `internal/seed` + tests.
2. Add `store.LabelSeed`/`SeedLabels` + tests; wire `CreateProject` seeding + tests.
3. Rewrite `cli/conventions.go` + regenerate goldens; add `cli label seed` + tests.
4. Add `tui/labels.go`; wire 4-tab shell; update `projects.go` (drop labels section); update `tasks.go` (create form labels field); update `keymap.go`/`help.go`; add TUI tests.
5. `make verify` green before each commit.

**Out of scope:**
- Store-level enforcement of the code-of-conduct (the rules are agent-driven and human-curated, not store-enforced — consistent with v2's "no intrinsic workflow knowledge" principle).
- Backfilling existing projects on upgrade (users run `atm label seed --project <CODE>` or the `[S]` key on demand).
- Migrating v1 projects (v2 has no migration; unchanged).