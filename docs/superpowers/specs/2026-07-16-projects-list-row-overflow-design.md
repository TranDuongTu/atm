# Projects List Row Overflow Design

**Task:** ATM-46f820

## Purpose

In the TUI Projects pane, data rows overflow the pane width by one column,
so `dashboardLine`/`fitLine` truncates from the right and clips the
rightmost UPDATED column. A relative timestamp like `3m ago` renders as
`3m ag`. The header row does not overflow because it uses a 1-char leading
space, while data rows use a 2-char gutter+space prefix.

## Root Cause

`renderListRows` (`internal/tui/projects.go`) builds each data row as:

```go
line := fmt.Sprintf(" %-*s %-*s %*d %*d %*s", codeW, ..., nameW, ..., tasksW, ..., labelsW, ..., updatedW, ...)
line = gutter + " " + ...Render(line)   // 2-char prefix
dashboardLine(p.width, line)            // fitLine truncates from the right
```

`projectColumnWidths` sizes NAME to absorb the remainder so that
`codeW + nameW + tasksW + labelsW + updatedW + 5 == p.width`. The `+5`
accounts for four inter-column spaces plus one leading space — i.e. a
1-char prefix. The actual rendered prefix is `gutter + " "` (2 chars),
so each data row is `p.width + 1` wide. `dashboardLine` → `fitLine`
truncates from the right and clips UPDATED, the last column. The header
row uses only the 1-char leading space from the format string, so it
never overflows; only data rows do.

## Design

Approach A: size the columns so the full data row — including the
2-char `gutter + " "` prefix — fits inside `p.width`. NAME remains the
flexible column and absorbs the difference; UPDATED stays fixed at 10 so
the relative timestamp is never the column that gets clipped.

### Change in `projectColumnWidths`

The data row is built as:

```go
line := fmt.Sprintf(" %-*s %-*s %*d %*d %*s", codeW, ..., nameW, ..., tasksW, ..., labelsW, ..., updatedW, ...)
line = gutter + " " + ...Render(line)
dashboardLine(p.width, line)
```

The format string contributes: 1 leading space + 4 inter-column spaces
= 5 chars of overhead. The `gutter + " "` prefix contributes 2 more.
So the full data row width is:

```
codeW + nameW + tasksW + labelsW + updatedW + 5 + 2
= fixed + nameW + 7
```

For the data row to fit `p.width`:

```go
nameW = p.width - codeW - tasksW - labelsW - updatedW - 7
```

Lower the NAME floor from 20 to 8 (sub-decision A2). This keeps NAME as
the column that always absorbs shrinkage: at narrow pane widths the
project name truncates with an ellipsis (`truncateRunes` already appends
`...`), while UPDATED stays intact at its fixed 10.

The header row uses the same `nameW` but has no gutter prefix, so its
width is `fixed + nameW + 5 = p.width - 2`. It fits comfortably and is
right-padded by the pane box; column left-edges and fixed widths are
unchanged, so header and data columns stay aligned. No change to the
header format string.

When `nameW` would be below the floor even at the pane's minimum width,
the row can still overflow — this is the same degenerate behavior as
today for extremely tiny terminals and is out of scope for this bug
fix. The floor of 8 means the data row overflows only when
`p.width < fixed + 8 + 7 = 29 + 15 = 44`. The Projects pane's own
minimum content width (from `splitWorkspaceWidths`'s left-pane minimum
of 24, minus borders) is well below where NAME hits the floor in normal
use, so the fix covers all realistic widths.

### What does not change

- Column order: CODE NAME TASKS LABELS UPDATED.
- Fixed column widths (CODE/TASKS/LABELS/UPDATED).
- The `gutter + " "` prefix on data rows.
- `dashboardLine`/`fitLine` truncation behavior.
- The "NAME is the flexible column" design.
- Header rendering.

## Error Handling

None beyond the existing degenerate overflow at extremely narrow
terminal widths, which is unchanged by this fix.

## Testing

Add TUI tests in `internal/tui/app_test.go`:

- A data row's UPDATED value renders in full (e.g. `3m ago`, not `3m
  ag`) at a realistic pane width. Seed a project, set a known
  updated-at, drive `renderListRows` and assert the full timestamp
  string is present.
- The header row still renders all column labels (CODE/NAME/TASKS/
  LABELS/UPDATED) — guards against accidentally shrinking the header.
- At a narrow pane width where NAME hits the floor of 8, the name is
  truncated with an ellipsis and UPDATED still renders in full.
- The existing `TestProjectsListPopulated` expectations (total
  projects, selected, showing range, gutter marker) still hold.

## Scope

One arithmetic change in `projectColumnWidths` (subtract 2 for the
prefix instead of 1, lower the NAME floor to 8) plus tests. No changes
to column order, fixed widths, header rendering, or `dashboardLine`.