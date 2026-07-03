# ATM TUI Dashboard Layout Refinement — Design Spec

**Status:** Approved from user feedback after the TUI theme refresh.
**Scope:** Follow-on visual layout refinement for the v2 Bubble Tea TUI.

## Driver

The runtime theme refresh added semantic styles and theme switching, but the
result still reads too much like plain text with different colors. The next
iteration should make the TUI feel like an operational dashboard: clearer pane
boundaries, stronger hierarchy, more breathing room, and sections that can be
scanned quickly.

This refinement is presentation-only. It must not change ATM's store API,
label substrate, task filtering semantics, keyboard navigation, or mutation
behavior.

## Goals

- Remove the `atm-dark` theme.
- Make `graphite` the default theme.
- Cycle themes in this order: `graphite -> light -> mono -> graphite`.
- Keep theme switching runtime-only and non-persistent.
- Redesign list, detail, label, and help panes for dashboard-like hierarchy.
- Preserve existing keyboard workflows, tab model, filters, sorting, forms,
  confirms, overlays, and data behavior.

## Non-goals

- No persisted theme setting.
- No store changes.
- No CLI flags or config commands.
- No new TUI tabs or data concepts.
- No mouse-driven redesign.
- No full-screen ANSI snapshot tests.

## Theme Model

The root model continues to own `ThemeName` and `Styles`. The active theme is
process-local only. `atm-dark` is removed from built-in theme names and from
the cycle order.

If an unknown theme name is encountered internally, the TUI falls back to the
default `graphite` theme.

## Dashboard Layout

The shared app chrome remains:

- tab bar
- separator
- body
- separator
- status line

Pane bodies should use imprinted section dividers such as
`──── Overview ────` where that improves visual separation. The layout may
still be a single column; this is a terminal TUI, not a mouse dashboard. The
important change is that each pane has clear section titles and grouped content
instead of long undifferentiated text blocks.

Section dividers and their content should share a centered content column. On
wide terminals this column should be wider than the prior 78-column divider so
tables and Help text breathe without spanning the whole screen.

The body of a tab must not repeat the active tab title for Projects, Tasks, or
Labels. The tab bar already carries those titles. Entity detail titles remain
useful and should stay, such as `Project ATM`, `Task ATM-0001`, and
`Label ATM:status:open`.

## Projects Pane

The Projects list should render as a dashboard section:

- summary line with total projects and selected project
- spaced table with stable columns for code, name, tasks, labels, updated
- selected project marker remains distinct from cursor highlight

Project detail should use:

- main title containing project code and name
- facts section for code, task count, label count, created, updated
- local actions section for `[N] set name`, `[H] history`, `[x] remove`
- history section only when enabled

## Tasks Pane

The Tasks list should have a dashboard header that shows:

- project scope
- filter
- sort mode

Flat task rows should make title the primary scanning field and metadata
secondary. Rows may remain fixed-width for terminal stability, but the order
should emphasize task identity and title before labels and timestamps.

Grouped task views should render groups as clearer blocks:

- group headers include expand/collapse marker and task count
- nested groups are visibly indented
- leaf task rows use a consistent secondary metadata format
- the `(no matching labels)` bucket remains visible and non-collapsible

Task detail should use:

- main title containing task ID and title
- facts section for project, description, created, updated
- labels section with chips when width allows
- history section
- local actions section for edit, label, and remove keys

## Labels Pane

The Labels list should group labels by namespace as dashboard sections:

- namespace headings are visually distinct
- usage counts are aligned and easy to scan
- labels without descriptions are called out as needing description
- tag labels still appear in a `tags` group

Label detail should use:

- main title containing the label name
- facts section for usage and description
- local actions section for describe, remove, and back

## Help Pane

Help should remain text-first but become a scannable reference:

- separate titled sections for CLI/TUI parity, global keymap, and conventions
- section spacing that prevents the tab from reading as one wall of text
- conventions content rendered as curated terminal reference text rather than
  raw markdown-looking prose
- markdown headings in the conventions source displayed as imprinted dividers
- numbered or bulleted guidance displayed with aligned indentation and quiet
  secondary styling

The keymap content must continue to document `[T]theme`.

## Error Handling

Rendering must not panic at narrow terminal widths. Existing width guards,
padding helpers, and truncation helpers should continue to protect render
paths. If bordered sections cannot fit comfortably, they should still render
bounded content rather than panic.

## Testing

Implementation must add or update tests for:

- default theme is `graphite`
- theme cycle order is `graphite -> light -> mono -> graphite`
- unknown theme fallback is `graphite`
- status line shows `theme: graphite` by default when width allows
- Projects, Tasks, and Labels list bodies do not repeat tab titles
- section dividers render imprinted titles such as `Overview`, `Facts`, and
  `Actions`
- section divider width and section text width match inside a centered content
  column on wide terminals
- Projects list includes a dashboard summary
- Tasks list includes a dashboard header and title-primary row content
- grouped Tasks view includes visually distinct group block headers
- Task detail includes facts, labels, history, and actions sections
- Labels list includes namespace sections and calls out missing descriptions
- Help includes scannable reference section titles and formatted conventions
  text rather than raw markdown heading markers

Tests should assert stable text and state, not complete ANSI snapshots.

## Verification

Before declaring implementation complete, run:

```sh
make verify
```
