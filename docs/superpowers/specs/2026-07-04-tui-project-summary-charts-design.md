# ATM TUI Project Summary Charts — Design Spec

**Status:** Approved from user feedback on the three-pane workspace; revised
2026-07-05 after the audit-log/event-sourcing merge.
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
- Render chart content inside large centered boxes that occupy the summary
  region's remaining height.
- Add two summary chart areas:
  - activities by actor from the selected project's audit log
  - one-week project activity stripe from the selected project's audit log
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
│ ╭ activity by actor ───────────────╮ │
│ │ claude   ███████████████ 62% 18 │ │
│ │ codex    ████████        31%  9 │ │
│ │ others   ██               7%  2 │ │
│ ╰──────────────────────────────────╯ │
│                                      │
│ ╭ activity stripe ─────────────────╮ │
│ │ ▁▁▁▁ ▂▂▂▂ ▅▅▅▅ ▁▁▁▁ ▁▁▁▁ ████ ▁▁▁▁ │ │
│ │ 7d ago                  Yesterday Today │ │
│ ╰──────────────────────────────────╯ │
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

## Chart 1: Activities By Actor

The first chart summarizes audit-log activity by actor for the selected
project. Activity is defined as the number of project log events carried by
that actor. Every `store.LogEntry` returned by `Store.ReadLog(<CODE>)`
contributes one activity to its `Actor`.

Rows are sorted by activity count descending, then actor name ascending for
ties. The chart renders at most 10 actor rows. If more than 10 actors are
present, the final row is `others`, aggregating all actors after the first 9.

Each row should feel like a compact btop CPU meter: actor name, horizontal bar,
percentage of total activity, and raw count. It renders inside a large centered
box titled `activity by actor`; the title is quieter and less prominent than a
section divider. The box is approximately 95 percent of the Projects pane
width, uses a dim gray border, and centers chart content inside the available
box space. Actor names are displayed in full. Bars use color through Lip Gloss
styling.

If the selected project has no log entries or no actor-bearing entries, the
chart renders an empty state instead of blank space.

## Chart 2: Activity Stripe

The second chart shows selected-project activity intensity across one week.
It uses all audit-log events for the selected project, across all actors, and
renders inside a large centered box titled `activity stripe`.

Events are bucketed by UTC date. The stripe renders seven distinct calendar day
bars ending today, so the rightmost bar is always `Today` even when today has no
activity. The x-axis labels only the first, sixth, and seventh bars:
`7d ago`, `Yesterday`, and `Today`; the middle unlabeled bars are intentionally
omitted to reduce clutter. More events produce a stronger density mark for that
day; zero-activity days render as an empty/low bar rather than disappearing. The
renderer uses
`github.com/NimbleMarkets/ntcharts/canvas` so the stripe is a small chart block
rather than a raw one-line glyph string. Example glyph progression: `▁`, `▂`,
`▅`, `█`.

The chart is a visual density indicator, not an exact audit report. It should
communicate whether the selected project has been quiet, steady, or recently
active.

If there are no log entries, the chart renders seven empty/low bars ending
today rather than a blank chart.

## Data Flow

The implementation should stay in `internal/tui`.

The Projects model can own the summary rendering directly. Helper functions
should compute chart data from existing store data:

- selected project code from `Model.projectScope`
- tasks from `Store.ListTasks(store.QueryFilters{Project: code})`
- project metadata from `Store.GetProject(code)`
- project activity events from `Store.ReadLog(code)`
- chart drawing primitives from `github.com/NimbleMarkets/ntcharts/canvas`

No new store methods are required unless implementation reveals a small
read-only helper that clearly belongs in `internal/store`. The default direction
is to keep this as TUI aggregation.

Pure helper functions are preferred for:

- splitting the Projects pane list and summary heights
- aggregating audit-log entries by actor
- collecting and bucketing audit-log entries into the one-week stripe
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

The two chart boxes should consume all remaining summary height after the
summary title and project context lines. On normal terminal heights, the space
is split across actor activity and the activity stripe. On very short
terminals, rendering may gracefully degrade to compact labels/inline charts.

The Projects status hint does not need new keys because charts are
non-interactive. Existing list keys remain valid.

## Error Handling

Rendering must not panic on narrow or short terminals. If there is not enough
height to show both charts, the summary region should render as many chart
lines as fit in priority order:

1. chart section title / selected project context
2. activities by actor chart
3. activity stripe chart

If a selected project cannot be loaded, the summary region should render a
short error-like empty state and avoid interrupting the rest of the TUI render.
Normal store mutation errors should continue to use existing toasts and forms.

## Testing

Implementation must add or update tests for:

- Projects list mode renders a project summary region below the project list.
- The Projects pane internal height split is approximately 30 percent list and
  70 percent summary on normal terminal sizes.
- No project selected renders the summary empty state.
- Selecting a project renders both chart sections.
- Activity-by-actor aggregation counts project log events by actor.
- Activity-by-actor aggregation sorts by count and folds extra actors into
  `others` when more than 10 actors are present.
- Activity stripe renders exactly seven bars ending today and labels only
  `7d ago`, `Yesterday`, and `Today`.
- Activity density rendering is deterministic for fixed bucket counts.
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
