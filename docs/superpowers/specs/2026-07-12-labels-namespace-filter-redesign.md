# Labels pane redesign: namespace-driven task filtering

Status: approved (brainstorming) — 2026-07-12
Supersedes the interaction model of docs/superpowers/specs/2026-07-07-labels-namespace-filter-design.md (ATM-0041). TUI-only. No store or CLI changes.

## Problem

Filtering tasks by label today requires ping-ponging between two panes. The Tasks pane owns the filter (typed via `/`, cleared via `c`), while the Labels pane owns the vocabulary. To filter by `status:done` a user reads the label in the Labels pane, then switches to the Tasks pane to type or toggle it. The Labels pane's flat list mixes namespace headers and label rows, and the wildcard facet does not restrict — selecting `status:*` still shows unstatused tasks in an `others` bucket, so "tasks with a status" and "tasks missing a status" are not cleanly separable. The result is that the two most common filtering intents (narrow to a namespace value; find tasks missing a namespace) are awkward.

## Goals

- Make the Labels pane the single filtering surface. The Tasks pane becomes selection-only: navigate, open, label, comment — but never type a filter.
- Turn the Labels pane into a three-level drill-down: namespace table (L0) -> per-namespace chart (L1) -> label detail (L2). Navigation alone drives the Tasks filter; there is no separate filter input to keep in sync.
- Allow exactly one namespace facet and, under it, one exact label (or one absence filter) active at a time. Moving up a level clears the filter that level introduced.
- Support the "missing namespace" intent: within a namespace's chart, an `(unset)` row filters the Tasks pane to tasks that carry no label in that namespace (e.g. a stub with `priority:high` but no `status:*`).
- Support the "no labels at all" intent: a top-level `(none)` row filters the Tasks pane to tasks with an empty label set.
- Give the Tasks pane 75% of the right column height and the Labels pane 25%.

## Non-Goals

- No multi-namespace or multi-label filtering exposed in the UI. The underlying filter string can still hold multiple tokens, but the Labels navigation only ever sets one namespace facet plus at most one exact/absence filter. (Deferred; not removed from the store.)
- No store or CLI changes. `QueryFilters` is unchanged; absence filtering is a Tasks-pane rendering concern over the `groups`/`others` the store already returns.
- No free-text label search in the pane. Navigation is by cursor only.
- The pre-existing stale-filter-on-project-switch bug (ATM-0082) is not fixed globally; this redesign resets the Labels pane and Tasks focus on project switch, which sidesteps it for this path.

## Interaction model

The Labels pane is a state machine with three levels. The Tasks pane is a read-only mirror of the Labels cursor: whatever the cursor implies is what the Tasks pane shows.

### Level 0 — namespace table (no active filter)

The pane renders a table, spanning the pane width, with one row per namespace plus two synthetic rows. There is no "project X / total Y labels" caption.

```
 NAMESPACE        TASKS      LABELS
 comment              0           4
 context             22           4
 priority            31           2
>status              40           5
 type                18           3
 tags                 3           2
 (none)               6           —
```

- Rows: each real namespace (alphabetical), then `tags` (bare/unnamespaced labels), then `(none)` (tasks with zero labels). The `tags` and `(none)` rows are omitted when they would be empty (no bare labels defined; no unlabeled tasks).
- `TASKS` = number of distinct tasks in the project carrying at least one label in this namespace (`TASKS` counts tasks only, never comments, so a namespace whose labels only ever land on comments — e.g. `comment` — shows `0`). For `tags`, distinct tasks carrying any bare label. For `(none)`, the count of zero-label tasks.
- `LABELS` = number of labels defined in the namespace. For `(none)`, `—` (it is a task bucket, not a namespace).
- Tasks pane at L0: all tasks, unfiltered, flat list.

Keys at L0:
- `j`/`k`/`g`/`[`/`]`: move the cursor over rows.
- `Enter` on a real namespace or `tags`: drill into its chart (L1).
- `Enter` on `(none)`: apply the unlabeled filter — the Tasks pane shows only zero-label tasks. This is a leaf (there is nothing to chart); the Labels pane shows a minimal detail panel ("N tasks with no labels"). `Esc` returns to the table and clears the filter.
- `a`: add a label to the project (project-level form, unchanged).
- `S`: seed default labels (unchanged).
- `Esc`: no-op (nothing above L0).

### Level 1 — namespace chart (namespace facet active)

Entered by `Enter` on a namespace (or `tags`) row. The Tasks pane facets by that namespace's wildcard and shows only tasks carrying the namespace, grouped by value; the `others` bucket is hidden.

```
 status  ·  40 tasks
 status:open  ████████░░  25
>status:done  ████░░░░░░  10
 status:todo  ██░░░░░░░░   5
 (unset)      █░░░░░░░░░   6
```

- One bar row per label in the namespace, plus a trailing `(unset)` row. Bars and counts are TASK counts. For a real namespace they come from `GroupTasks(Project, [<CODE>:<ns>:*])` (each label's task list length; `(unset)` = length of `others`), so every row reconciles with the Tasks pane. For the `tags` pseudo-namespace, where no single wildcard can select bare labels, the counts are computed locally: per bare-tag task count, and `(unset)` = tasks with no bare tag. The bar length is each row's share of the namespace's task total (label task counts plus the unset count).
- The `(unset)` row is omitted when no task lacks the namespace.
- Tasks pane at L1: for a real namespace, grouped by the namespace with `others` hidden (focus present). For `tags`, a flat list of tasks carrying any bare tag (focus present + bareTags).

Keys at L1:
- `j`/`k`/`g`/`[`/`]`: move the cursor over rows, including `(unset)`. The chart scrolls when rows exceed the pane height.
- `Enter` on a label row: open its detail (L2) and set the Tasks filter to that exact label.
- `Enter` on `(unset)`: filter the Tasks pane to tasks lacking the namespace (Tasks focus = namespace-absent). The Labels pane shows a minimal detail panel ("N tasks with no <ns>"). Treated as an L2 leaf for Esc purposes.
- `d`: describe the label under the cursor (no-op on `(unset)`).
- `l`: remove the label under the cursor (no-op on `(unset)`).
- `Esc`: return to L0 and clear the namespace facet (Tasks pane back to all tasks).

### Level 2 — label detail (exact label active)

Entered by `Enter` on a label row at L1. The Labels pane shows the label's detail (name, usage, description) as today. The Tasks pane shows a flat list of the tasks carrying that exact label: the filter holds only the exact restricting token and focus is `focusOff`, so there is no grouping and no header — just "the tasks with this label." This is the same flat presentation for a real-namespace label (`status:done`) and a bare tag (`urgent`).

Keys at L2:
- `d`: describe this label.
- `l`: remove this label.
- `Esc`: return to L1; the Labels pane re-enters the chart, which re-applies L1's Tasks-pane state (see "State application" below).

The `(unset)` and `(none)` leaves reuse the L2 slot: they show a minimal detail panel and `Esc` steps back one level (to L1 or L0 respectively), clearing their filter. `d`/`l` are no-ops on them.

### State application

Rather than incrementally add and remove filter tokens, each Labels level sets the *entire* Tasks-pane state (filter string + focus) when it is entered, then calls `tasks.refresh()`. Navigation (Enter down, Esc up) simply moves between levels, and the destination level re-applies its state from scratch. This makes stale filters structurally impossible — there is no accumulation to leak — and keeps the invariant "at most one namespace facet plus at most one exact/absence filter" automatic. The per-level state:

- L0 table: filter `""`, focus `focusOff` (all tasks).
- L1 chart on real namespace `ns`: filter `<CODE>:<ns>:*`, focus `focusPresent{ns}`.
- L1 chart on `tags`: filter `""`, focus `focusPresent{bareTags:true}`.
- L2 detail on label `full`: filter `full` (exact), focus `focusOff`.
- `(unset)` leaf under `ns`: filter `<CODE>:<ns>:*`, focus `focusAbsent{ns}`; under `tags`: filter `""`, focus `focusAbsent{bareTags:true}`.
- `(none)` leaf: filter `""`, focus `focusUnlabeled`.

### Esc ladder summary

L2 (label detail) --Esc--> L1 (chart) — re-applies L1 state.
L1 (chart) / `(unset)` leaf --Esc--> L0 (table) — re-applies L0 state (all tasks).
L0 `(none)` leaf --Esc--> L0 (table).
L0 (table) --Esc--> no-op.

## Tasks pane changes

- Height: the right column splits 75/25 (Tasks/Labels) instead of 50/50. This is `splitRightColumnHeights` in internal/tui/app.go (currently `top := height / 2`).
- Remove the `/` (edit filter) and `c` (clear filter) keys and the editable filter input/display.
- Add a read-only focus caption so the user can see why the list is scoped, e.g. `focus: status`, `focus: status:done`, `focus: no status`, `focus: bare tags`, `focus: unlabeled`, or nothing at L0.
- Introduce a Tasks-pane focus value set by Labels navigation:

```
type taskFocus struct {
    mode     taskFocusMode
    ns       string // namespace for present/absent on a real namespace
    bareTags bool   // present/absent apply to bare (unnamespaced) labels instead of ns
}
type taskFocusMode int
const (
    focusOff       taskFocusMode = iota // render whatever t.filter yields (L0 all tasks; L2 exact-label flat list)
    focusPresent                        // tasks carrying the namespace (real ns: grouped via GroupTasks; bareTags: flat predicate)
    focusAbsent                         // tasks NOT carrying the namespace (real ns: GroupTasks others; bareTags: flat predicate)
    focusUnlabeled                      // tasks with zero labels
)
```

- `refresh()` branches on `focus.mode`:
  - `focusOff`: existing behavior — `ListTasks(parseFilter())`, grouped if the filter has a wildcard else flat. Covers L0 (empty filter -> all tasks flat) and L2 (exact token -> flat filtered list).
  - `focusPresent`, real namespace: `GroupTasks(Project, parseFilter())` where the filter is `<CODE>:<ns>:*`; render the `groups` only (hide `others`).
  - `focusPresent`, `bareTags`: flat list of tasks where `taskHasBareTag(t)` is true (predicate over `ListTasks(Project)`).
  - `focusAbsent`, real namespace: `GroupTasks(Project, [<CODE>:<ns>:*])`; render the `others` bucket as a flat list.
  - `focusAbsent`, `bareTags`: flat list of tasks where `taskHasBareTag(t)` is false.
  - `focusUnlabeled`: flat list of tasks where `len(t.Labels) == 0`.
- `taskHasBareTag(t)` is true when the task carries at least one label whose suffix (after the `<CODE>:` prefix) contains no `:` — i.e. an unnamespaced tag. It is a trivial predicate, not a new store query.
- Selecting a task, opening its detail, labeling, commenting, sorting: all unchanged.

## Labels pane changes

- Replace the flat entries list with the L0 namespace table plus the synthetic `tags` and `(none)` rows. Compute `TASKS` counts by iterating the project's tasks (available via `m.store.ListTasks`); `LABELS` counts from the label list.
- The chart gains a cursor and a trailing `(unset)` row; counts come from `GroupTasks` for the active namespace so the chart and Tasks pane agree.
- The three levels replace the current `chartNS`/`lViewDetail` ad-hoc state with an explicit level enum. `Enter`/`Esc` are owned by the Labels pane's `handleKey`; the Esc-interception special cases in internal/tui/app.go (labels detail at ~615, labels chart at ~619) are removed and folded into the level state machine.
- The `i` (inspect) key is removed; `Enter` opens detail.
- On project switch, reset the Labels pane to L0 and clear the Tasks focus and filter.

## Testing

Table-driven Go tests in internal/tui (following labels_test.go / tasks_test.go patterns):

- Count derivation: `TASKS`/`LABELS` per namespace, `tags` and `(none)` rows appear only when non-empty, correct counts for a fixture with mixed labeled/unlabeled tasks and a task with a namespace label but no status.
- Chart counts: per-label task counts and `(unset)` count match `GroupTasks`; `(unset)` omitted when every task has the namespace.
- Focus mapping: each `taskFocus` value renders the correct subset (present -> groups only, absent -> others only, unlabeled -> zero-label tasks, off -> all/exact).
- Enter/Esc ladder: L0->L1->L2 sets the expected filter tokens and focus; each Esc clears exactly the level's filter and restores the parent's Tasks view; the one-namespace-one-label invariant holds after any navigation sequence.
- Synthetic leaves: `Enter` on `(none)` and `(unset)` set `focusUnlabeled`/`focusNamespaceAbsent`; `Esc` steps back and clears; `d`/`l` are no-ops on them.
- Removed keys: `/` and `c` in the Tasks pane are no-ops; `i` in the Labels pane is a no-op.
- Edge cases: no project selected, empty namespace, project switch resets to L0 and clears focus.

`make verify` (build + all tests + scripts-test) is the completion gate.

## Files touched

- internal/tui/app.go — 75/25 split in `splitRightColumnHeights`; remove Labels Esc-interception special cases.
- internal/tui/tasks.go — remove `/`/`c` and filter input; add `taskFocus`, focus-aware rendering, read-only focus caption.
- internal/tui/labels.go — namespace table (L0), cursor chart with `(unset)` (L1), detail/leaf (L2), level state machine, synthetic-row counts, remove `i`.
- internal/tui/keymap.go and internal/tui/help.go — update Labels/Tasks key documentation.
- internal/tui/*_test.go — the tests above.
