# ATM TUI Project Summary Charts — Design Spec

**Status:** Approved from user feedback on the three-pane workspace.
**Scope:** Follow-on refinement for the Projects pane in the v2 Bubble Tea TUI.

## Driver

The three-pane workspace currently gives the Projects pane the full height of
the left column. That makes Projects a stable anchor, but it leaves the lower
part of the pane underused once a human has selected the project they want to
inspect. The Projects pane should become both the project selector and a compact
project summary surface.

This refinement adds project-bound summary charts below the project list. It is
presentation and aggregation only: it does not change the store schema, CLI,
task filtering semantics, label semantics, mutation behavior, or workspace focus
model.

## Goals

- Keep the persistent three-pane workspace: Projects, Tasks, and Labels.
- Split the Projects pane body into a compact project list and summary charts.
- Put the summary region below the project list, taking roughly 70 percent of
  the Projects pane body height.
- Render summaries only when a project is selected.
- Add three summary chart areas:
  - task label usage by label namespace
  - project activity over time from project and task history
  - keyword bubbles placeholder for a future agent integration
- Keep the charts terminal-friendly, bounded, and non-interactive in this
  iteration.
- Keep implementation in the TUI layer using existing store reads.

## Non-Goals

- No store schema changes.
- No CLI changes.
- No new project or task entities.
- No persistent analytics cache.
- No mouse-driven charts.
- No interactive chart drill-down.
- No agent integration for keyword extraction in this iteration.
- No exact audit-reporting guarantees for chart numbers.

## Layout

The root workspace split remains the same: Projects on the left, Tasks above
Labels on the right. The Projects pane keeps its existing border title:
`[1] Projects`.

Inside the Projects pane body, list mode splits vertically:

```text
┌─ [1] Projects ───────────────────────┐
│ ─ Overview ─                         │  roughly 30%
│ total projects: 4   selected: ATM    │
│ CODE   NAME       TASKS LABELS       │
│ ▸ ATM  Agent Tasks   12     18       │
├──────────────────────────────────────┤
│ ─ Project Summary ─                  │  roughly 70%
│ Labels by namespace                  │
│ status      ███████  8               │
│ type        █████    5               │
│ priority    ███      3               │
│                                      │
│ Activity                             │
│ ░ ░ ▒ ▓ █ ░ ▒ ░ ░ ▓ ▒ ░             │
│                                      │
│ Keywords                             │
│ agent-generated keyword bubbles      │
│ pending                              │
└──────────────────────────────────────┘
```

The split should be approximate and bounded. On normal terminal heights, the
project list gets about 30 percent of the Projects pane body and the summary
region gets about 70 percent. On short terminals, both regions clamp to at least
one visible line where possible and rendering must not panic. The projects list
may show fewer rows than today because it is now a selector rather than the full
left-column content.

Project detail mode remains pane-local and keeps the full Projects pane body.
Opening project detail should not show the summary charts at the same time.

## Selection Behavior

The summary region is bound to `projectScope`, the same selected project used by
the Tasks and Labels panes.

- If no project is selected, the summary region renders a quiet empty state such
  as `select a project to see summaries`.
- Moving the cursor in the Projects list does not change summaries by itself.
- Pressing `s` selects the cursor project, refreshes Tasks and Labels, and also
  refreshes the summary chart data.
- Creating a project selects it as today; the summary region then renders for
  the new project.
- Removing the selected project clears `projectScope`; the summary region
  returns to the empty state.

## Chart 1: Labels By Namespace

The first chart summarizes task label usage by namespace for the selected
project. It does not render each concrete label as a separate slice.

For a label named `<CODE>:<namespace>:<value>`, the namespace is the middle
segment. For a tag label named `<CODE>:<tag>`, the namespace bucket is `tags`.
Counts are computed from labels actually attached to tasks in the selected
project, not from registry labels alone.

Counting is multi-membership:

- A task with `ATM:status:open` and `ATM:type:bug` contributes one count to
  `status` and one count to `type`.
- A task with two labels in the same namespace contributes two counts to that
  namespace. This intentionally surfaces high label density and inconsistent
  duplicate namespace use rather than hiding it.

The chart should render as a compact text bar or pie-like summary. Exact visual
encoding is implementation detail, but it must show namespace names and counts
and fit within the Projects pane width.

If the selected project has no tasks or no task labels, the chart renders an
empty state instead of blank space.

## Chart 2: Activity Over Time

The second chart shows selected-project activity over time in a style similar to
a GitHub contribution chart. It is a visual density indicator, not an exact
audit report.

Input events include:

- every history entry on the selected project
- every history entry on every task in the selected project

Events are bucketed by date. The display may choose daily or coarser buckets
depending on available width; the requirement is that more events produce a
visibly stronger density mark for that bucket. Example glyph progression:
`·`, `░`, `▒`, `▓`, `█`.

The chart should be deterministic for a fixed store state. It does not need to
show exact numeric labels for every bucket, and it does not need to perfectly
match GitHub's week grid. It should communicate whether the selected project
has been quiet, steady, or recently active.

If there are no history entries, the chart renders an empty state.

## Chart 3: Keyword Bubbles Placeholder

The third chart is a placeholder for future agent-generated project keywords.
It should render only when a project is selected and should make the deferred
state explicit, for example:

```text
Keywords
agent-generated keyword bubbles pending
```

No agent is invoked. No keyword extraction runs locally. No cache file or store
field is added.

A future design may add an agent integration that digests current project data
and returns keyword bubbles. This spec only reserves the visual slot and keeps
the user-facing expectation visible.

## Data Flow

The implementation should stay in `internal/tui`.

The Projects model can own the summary rendering directly. Helper functions
should compute chart data from existing store data:

- selected project code from `Model.projectScope`
- tasks from `Store.ListTasks(store.QueryFilters{Project: code})`
- project history from `Store.GetProject(code)`
- task labels and task history from the returned tasks

No new store methods are required unless implementation reveals a small
read-only helper that clearly belongs in `internal/store`. The default direction
is to keep this as TUI aggregation.

Pure helper functions are preferred for:

- splitting the Projects pane list and summary heights
- aggregating label namespace counts
- collecting and bucketing activity history
- rendering density glyphs from bucket counts

This keeps chart behavior testable without relying on full-screen ANSI
snapshots.

## Rendering Responsibilities

The root `Model` continues to own outer workspace splits and pane borders.
`projectsModel` owns the internal split between the project list and project
summary region in list mode.

The summary region should use existing style primitives where practical:
section dividers, muted text, dashboard lines, truncation helpers, and stable
height padding. It should not introduce a new visual theme or dominate the
right-side Tasks and Labels panes.

The Projects status hint does not need new keys because charts are
non-interactive. Existing list keys remain valid.

## Error Handling

Rendering must not panic on narrow or short terminals. If there is not enough
height to show all three charts, the summary region should render as many chart
lines as fit in priority order:

1. chart section title / selected project context
2. label namespace chart
3. activity chart
4. keyword placeholder

If a selected project cannot be loaded, the summary region should render a
short error-like empty state and avoid interrupting the rest of the TUI render.
Normal store mutation errors should continue to use existing toasts and forms.

## Testing

Implementation must add or update tests for:

- Projects list mode renders a project summary region below the project list.
- The Projects pane internal height split is approximately 30 percent list and
  70 percent summary on normal terminal sizes.
- No project selected renders the summary empty state.
- Selecting a project renders all three chart sections.
- Label namespace aggregation counts task labels by namespace, not by concrete
  label value.
- Tag labels are counted under `tags`.
- Activity aggregation includes both project history and task history.
- Activity density rendering is deterministic for fixed bucket counts.
- Keyword chart renders as a placeholder and does not invoke an agent.
- Project detail mode uses the full Projects pane body and does not render
  summary charts.
- Narrow and short terminal sizes render without panic.

Tests should assert stable text, model state, and helper outputs. Avoid brittle
full-screen ANSI snapshots.

## Verification

Before declaring implementation complete, run:

```sh
make verify
```
