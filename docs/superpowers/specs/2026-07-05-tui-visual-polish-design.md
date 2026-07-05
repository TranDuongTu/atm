# ATM TUI Visual Polish — Design Spec

**Status:** Approved from user feedback on divider clutter and non-table-like
list panes.
**Scope:** Rendering-only refinement to list panes, detail pages, and the
key-hint surface across Projects, Tasks, and Labels. No store, CLI, or
navigation-model changes.

## Driver

The current list panes and detail pages lean on `sectionDivider` — a
full-pane-width `──── Title ────` banner — for every section: `Overview`,
`Facts`, `Labels`, `Comments`, `Actions`, `History`, `Groups`, `Namespaces`,
`Project Summary`. Every section gets the same heavy visual weight, so:

- List panes read as stacked text blocks, not tables — there's no consistent
  column alignment or a natural "header, then rows, then summary/paging"
  shape.
- Detail pages (task/project/label) are wall-to-wall dividers with no visual
  hierarchy between sections.
- Every detail page repeats its available key hints in an `Actions` section,
  even though the status line's `statusHint()` already renders context-aware
  key hints for the focused pane — the same information exists twice, in two
  different visual conventions.

This spec removes the banner-style dividers, gives list panes an actual
tabular shape where the content is naturally flat, and consolidates all
"what can I press" hints into the status line.

## Non-Goals

- No change to pane layout, focus model, or the three-pane workspace
  (`docs/superpowers/specs/2026-07-04-tui-three-pane-workspace-design.md`
  stays in force).
- No change to list/detail navigation — `Enter` still swaps a pane's list
  view for a full detail view in place (no accordion/expand-in-row; explicitly
  considered and rejected during design).
- No change to store API, CLI, mutation behavior, filtering/sorting semantics.
- No change to grouped/tree list structure (task label-grouping, label
  namespace-grouping stay trees, not converted to flat tables).

## 1. List Panes

Remove `sectionDivider` banners from all list views. Replace the banner +
"Overview" caption with a single plain caption line (no rule), and standardize
on **one thin rule directly under the column header row** where a header row
exists. Nothing else in a list view gets a full-width rule.

### Projects list

Already close to tabular. Drop the `Overview` banner; keep the existing
caption + header + single underline structure:

```
Projects · 3 total · selected: none

  CODE   NAME                    TASKS  LABELS  UPDATED
  ─────────────────────────────────────────────────────
▸ ATM    ATM Developer Tool         12       8  2h ago
  SCY    Scyllas Core                 5       3  1d ago

  showing 1-2 of 2
```

### Tasks list — flat mode (no wildcard label filter active)

Currently two lines per row (title line, then an indented meta line with id/
labels/updated) with no column header. Convert to one row per task with real
columns — `ID`, `TITLE` (truncated to fit), `LABELS` (truncated), `UPDATED` —
matching the Projects treatment:

```
Tasks · ATM · 12 total · filter: none

  ID       TITLE                          LABELS                UPDATED
  ──────────────────────────────────────────────────────────────────────
▸ ATM-2    Fix theme flicker on resize    type:bug status:open    2h ago
  ATM-1    Implement launcher config...   type:feature            3d ago

  showing 1-2 of 12
```

### Tasks list — grouped mode (wildcard label filter active) & Labels list
(grouped by namespace)

These are trees, not flat tables — a column header does not fit naturally
across nesting depths. Drop the banner only (`Groups`, `Namespaces`,
`Overview`); do not add a column header or rule. The existing bold
`GroupHeader` / `NamespaceHeader` styling already provides structure:

```
Tasks · ATM · grouped by status:*

▾ status:open (4)
    ATM-2  Fix theme flicker on resize        2h ago
    ATM-5  Add label seeding CLI              1d ago
▾ status:done (8)
  ...
```

```
Labels · ATM · 9 total

status:
  status:open      4 tasks
  status:done      8 tasks
type:
  type:bug         3 tasks
```

### Project Summary pane

Drop the `Project Summary` banner; replace with a plain caption line
(`project: ATM   tasks: 12`), consistent with the other panes. The chart/stat
content below is unaffected by this spec.

### Empty states

Unaffected — `centerLinesBoth`-based empty states (no project selected, no
tasks match filter, etc.) keep their current rendering; they do not use
`sectionDivider`.

## 2. Detail Pages (Task / Project / Label)

Keep the existing title line + full-width rule (`Task ATM-2` /
`──────────`) as the page-level header — this is the one full-width rule that
survives, scoped to the top of the page only.

Every sub-section below the title becomes: a bold caption (short label, no
banner fill), a rule scoped to the caption's own width (not the pane width),
the section's fields, then a blank line before the next section. No section
gets more visual weight than another.

```
Task ATM-2
──────────────────────────────────────────
Fix theme flicker on resize

FACTS
─────
  id       ATM-2
  project  ATM
  title    Fix theme flicker on resize
  desc     (none)
  created  2026-06-30T10:12:00Z by tu
  updated  2h ago by tu

LABELS
──────
  type:bug   status:open

COMMENTS
────────
  tu   2h ago   (no labels)
       looks good, ship it
```

Project detail (`FACTS`, then `HISTORY` when `[H]` toggles it on) and label
detail (`FACTS`) follow the same pattern. The conditional `History` section in
project detail keeps its existing toggle behavior (`p.detail.historyOn`) — it
is real content, not a key-hint list, so it gets the same FACTS-style
treatment as any other section, not special-cased.

**The `Actions` section is removed entirely from all three detail pages.**
Those key hints already exist in the status line via each model's
`statusHint()` and are not duplicated in the page body anymore.

The comment-detail overlay and history overlay (`comments.go`) get the same
treatment: drop their inline trailing key-hint line (see §3).

## 3. Status Line — Actions Consolidation

`Model.statusHint()` (app.go) dispatches on the focused pane; each pane's own
`statusHint()` already dispatches on that pane's list/detail view and returns
the correct key hints for the three main detail pages. Removing the inline
`Actions` block from `tasksModel.renderDetail`, `projectsModel.renderDetail`,
and `labelsModel.renderDetail` requires no new logic there — the status line
already carries the same information.

The one gap: `tasksModel.statusHint()` does not know about
`commentOverlay`/`historyOverlay` state, so opening either overlay today would
leave the status line showing the stale task-detail hint instead of the
overlay's actual keys. Fix by checking overlay state first, before the
existing `tViewDetail` branch:

```go
func (t *tasksModel) statusHint() string {
    if t.commentOverlay.id != "" {
        return "[H]istory   [Esc]back"
    }
    if t.historyOverlay.active {
        return "[Esc]back"
    }
    if t.m.projectScope == "" {
        return "[?]keys"
    }
    if t.view == tViewDetail {
        return "[e]title [d]desc [b]add label [B]remove label [M]comment [H]history [x]remove [Esc]back"
    }
    ...
}
```

Drop the trailing `dashboardLine(...KeyMenuDim.Render(...))` hint line from
both `commentOverlayModel.render` and `historyOverlayModel.render` in
comments.go — their body content ends at the last content line (comment body,
or the last history entry / `(no history)`).

Net effect: the status line is the single source of truth for "what can I
press right now," in every pane, view, and overlay.

## Rendering Responsibilities / Helpers

- `sectionDivider` (styles.go) becomes unused by list panes and by the
  `Actions`/`Overview`/`Groups`/`Namespaces`/`Project Summary` banners. It is
  still used for the new short, caption-scoped rule under each detail-page
  sub-section — reused as-is if its signature already supports a
  narrower-than-pane-width rule, otherwise a small new helper
  (e.g. `captionRule(styles, title string)`) replaces it for that one purpose.
  Remove `sectionDivider` entirely only if nothing still calls it after this
  change.
- `sepLine` (styles.go) keeps its one remaining use: the full-width rule under
  each detail page's title line.
- New: a shared column-table renderer (header row + single underline rule +
  aligned data rows + summary caption + paging footer) usable by both the
  Projects list and the Tasks flat list, to avoid duplicating column-width/
  truncation logic between the two.

## Testing

Implementation must update or add tests for:

- list panes no longer render `sectionDivider`-style banner text
  (`Overview`, `Groups`, `Namespaces`, `Project Summary`) but do render their
  plain caption line
- Projects list and Tasks flat list render a column header row followed by
  exactly one underline rule, then data rows, then a summary/paging line
- Tasks grouped list and Labels list render group/namespace headers with no
  banner and no column header
- detail pages (task/project/label) no longer render an `Actions` section
- detail pages render each sub-section as a bold caption + short rule (not a
  full-width rule), except the page-level title rule which stays full-width
- `tasksModel.statusHint()` returns the comment-overlay hint when
  `commentOverlay.id != ""`, the history-overlay hint when
  `historyOverlay.active`, and falls through to existing behavior otherwise
- comment overlay and history overlay render bodies with no trailing
  `KeyMenuDim` hint line
- existing list/detail/overlay navigation, filtering, sorting, and mutation
  behavior is unchanged (no regressions in `tasks_test.go`, `labels_test.go`,
  `comments_test.go`, `app_test.go`)

Tests should assert stable text and small rendering invariants, not brittle
full-screen ANSI snapshots (consistent with the existing test suite's
convention).

## Verification

Before declaring implementation complete, run:

```sh
make verify
```
