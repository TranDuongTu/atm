# Labels Pane Namespace Filtering — Design Spec

**Status:** Approved from brainstorming session on ATM-0041.
**Scope:** Follow-on refinement for the Labels pane and Tasks pane filter in the
three-pane Bubble Tea TUI (`docs/superpowers/specs/2026-07-04-tui-three-pane-workspace-design.md`).

## Driver

Today, faceting the Tasks pane by a label namespace (e.g. grouping by every
`status:*` value) requires opening the Tasks filter editor (`/`) and manually
typing `status:*`. The Labels pane, visible in the same workspace, already
lists every namespace and its labels but has no connection to the Tasks
filter at all — you have to already know the namespace name and type it by
hand. There's also no one-key way to clear the Tasks filter once it's been
typed; the only path is opening the editor and backspacing everything out.

This spec lets the Labels pane drive Tasks filtering directly: selecting a
namespace facets the Tasks pane by it, and the Labels pane itself turns into a
"tasks by label" bar chart for that namespace (reusing the existing
activity-by-actor meter-bar chart style from
`docs/superpowers/specs/2026-07-04-tui-project-summary-charts-design.md`).
Selecting an individual label keeps today's behavior (opens label detail).

## Goals

- Let a namespace header in the Labels pane be selected with `Enter`, adding
  `"<ns>:*"` to the Tasks pane filter and switching the Labels pane to a bar
  chart of usage counts per label in that namespace.
- Selecting an already-active namespace again removes its facet from the
  Tasks filter and returns the Labels pane to the flat list.
- Add a one-key "clear filter" action to the Tasks pane so the accumulate
  model (Labels can add multiple facets over time) has a matching one-key
  reset.
- Keep individual label selection (`Enter` on a label row) unchanged: opens
  label detail.
- No store or CLI changes; this is TUI presentation and filter-string
  composition only.

## Non-Goals

- No change to Tasks filter syntax or wildcard/facet semantics
  (`internal/store/query.go`, `parseFilter`, `buildNestedGroups` are
  untouched).
- No exact-label (non-wildcard) filtering from the Labels pane in this
  iteration — only namespace-level wildcard facets. Selecting a specific
  label still opens detail, not a filter action.
- No multi-chart display — the Labels pane charts exactly one namespace at a
  time, even if the Tasks filter holds multiple wildcard facets.
- No automatic chart activation from manually-typed filter text — typing
  `status:*` directly into the Tasks filter groups Tasks as it does today,
  but does not by itself switch the Labels pane into chart view.

## Data Model

`labelsModel` (`internal/tui/labels.go`) gains a unified entry list, replacing
today's row-only cursor:

```go
type labelEntryKind int

const (
    entryHeaderNS labelEntryKind = iota
    entryHeaderTags
    entryRow
)

type labelEntry struct {
    kind labelEntryKind
    ns   string   // namespace name, valid for entryHeaderNS
    row  labelRow // valid for entryRow
}
```

`refresh()` builds `l.entries []labelEntry` once per refresh: namespace
headers in alphabetical order followed by their rows, then the `tags:` header
followed by unnamespaced rows — the same grouping/order `renderList()`
computes today, hoisted so navigation and rendering share one source. `l.rows`
remains as the underlying data (usage counts, descriptions); `l.cursor` now
indexes `l.entries`, not `l.rows`.

A new field `l.chartNS string` holds the active chart namespace (empty means
flat list view). It is set/cleared only by the `Enter`-on-header toggle
action described below — it is not derived from the Tasks filter on every
render, except for one self-healing check: if `l.chartNS != ""` but
`"<chartNS>:*"` is no longer present in `l.m.tasks.filter`, the pane renders
the flat list instead (covers the Tasks filter being edited or cleared out
from under an active chart).

`labelsModel` already holds `m *Model`, and `Model` already holds `tasks
tasksModel` as a plain field, so the toggle action reads and writes
`l.m.tasks.filter` directly and calls `l.m.tasks.refresh()`. No event bus or
message passing is introduced.

## Interaction

**Labels pane, list view (`handleListKey`):**

- `j/k/g/[/]` — unchanged behavior, now stepping over `l.entries` (headers
  included) instead of `l.rows`.
- `enter` — context-sensitive on the entry under the cursor:
  - `entryHeaderNS`: toggle that namespace's facet. If `"<ns>:*"` is not in
    the Tasks filter, append it and set `l.chartNS = ns`. If it is already
    present, remove it from the Tasks filter and clear `l.chartNS` (falls
    back to list view). Either branch calls `l.m.tasks.refresh()` so Tasks
    re-groups immediately.
  - `entryHeaderTags`: no-op — bare tags have no namespace to facet on.
  - `entryRow`: unchanged — opens label detail.
- `a`/`d`/`l`/`S` — unchanged; they don't depend on cursor position today and
  still don't.
- `esc` — new: only meaningful when `l.chartNS != ""`. Closes the chart view
  back to the flat list. Does **not** touch the Tasks filter — chart
  visibility and filter membership are decoupled, so leaving chart view
  doesn't lose the facet.

While `l.chartNS != ""`, the pane is a static chart view: `j/k/g/[/]` and
`enter` are inert (there is no list to navigate — the entries list still
exists underneath but isn't rendered). `esc` is the only active key. To chart
a different namespace, `esc` back to the flat list first (cursor position is
preserved), move to the new namespace header, then `enter`. This keeps chart
mode a single static render rather than a hybrid chart+navigable-list state.

**Tasks pane, list view (`handleListKey`):** new key `c` clears the Tasks
filter to empty in one press and refreshes. This is what makes the
"facets accumulate" model workable — Labels can keep adding `ns:*` tokens
across multiple namespaces, and Tasks gets a single dedicated reset.

**Multiple active facets:** the Tasks filter can hold more than one `ns:*`
token at once (existing multi-wildcard nesting is unchanged). The Labels pane
only ever charts the most-recently-toggled-on namespace; toggling a different
namespace on updates `l.chartNS` to the new one without removing the earlier
namespace's token from the Tasks filter.

## Chart Rendering

Reuses the existing "activity by actor" chart machinery from
`internal/tui/projects.go` — `meterBar(percent, width)` and
`renderChartBox(title, body, maxLines)` — since the shape is identical: a
name, a horizontal bar, a percentage, and a count. Rows come from the
already-loaded `l.rows` data for the active namespace (the `usage` field
`refresh()` already populates via `store.LabelUsage`). No new store reads are
introduced, and the counts mean the same thing they mean in today's flat list:
project-wide label usage, independent of whatever the Tasks pane filter
currently shows.

```
[3] Labels — chart: status
 project: ATM   namespace: status

 status:open          █████████████  62%  18
 status:in-progress   ████████       31%   9
 status:done          ██              7%   2

[Esc] back to list
```

A namespace with zero usage across all its labels still renders the chart
(all-zero bars) rather than an empty state — the namespace exists, it's just
unused.

## Status Hints, Help, Keymap

- Labels list hint: `[a]dd [d]esc [l]remove [S]eed [Enter]select/detail
  [Esc]back [?]keys`.
- Tasks list hint gains `[c]lear`.
- `internal/tui/keymap.go` and the Keys help overlay (`help.go`) get new rows
  for Labels `Enter` (namespace select vs. label detail) and Tasks `c`
  (clear filter).

## Error Handling

- No project selected: Labels pane keeps today's empty state; namespace
  toggling requires a scoped project, same guard already used by
  `a`/`d`/`l`/`S`.
- Switching project scope while a chart is active: `refresh()` rebuilds
  `l.entries`/`l.rows` for the new project. If the new project's Tasks
  filter no longer contains `"<chartNS>:*"` (a fresh project scope resets the
  Tasks filter already), the self-healing check drops back to list view.
- Manually typing a `ns:*` token into the Tasks filter does not auto-activate
  the chart (see Non-Goals); only editing the filter to *remove* a
  currently-charted namespace triggers the self-heal back to list view.
- Rendering must not panic on narrow/short terminals — the chart box reuses
  the existing `renderChartBox`/`chartBoxInnerWidth` clamping used by the
  Projects pane charts.

## Testing

`internal/tui/labels_test.go`:

- `TestLabelsEntriesIncludeNamespaceHeaders` — entries list interleaves
  header and row entries in the expected order.
- `TestLabelsEnterOnNamespaceTogglesFacetAndChart` — `Enter` on a namespace
  header adds `ns:*` to the Tasks filter and switches to chart view; `Enter`
  again removes it and returns to list view.
- `TestLabelsEnterOnTagsHeaderIsNoop`.
- `TestLabelsChartShowsUsageBars` — chart view renders the expected
  bar/percent/count per label in the namespace.
- `TestLabelsChartSelfHealsWhenFilterEditedAway` — clearing the Tasks filter
  out from under an active chart falls back to list view on next render.
- `TestLabelsEscClosesChartWithoutClearingFilter`.
- Update `TestLabelsListScrollsWithCursor` and
  `TestLabelsBracketKeysPageThroughList` — expected cursor positions now
  account for header entries consuming cursor slots.

`internal/tui/tasks_test.go`:

- `TestTasksClearFilterKey` — `c` in Tasks list view resets `t.filter` to
  empty and refreshes.

Tests should assert stable text, model state, and helper outputs, consistent
with this project's existing avoidance of brittle full-screen ANSI snapshot
tests.

## Verification

Before declaring implementation complete, run:

```sh
make verify
```
