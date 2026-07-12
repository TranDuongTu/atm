# Activity Stripe: Proportional Bands + Adaptive Day Window

## Summary

The activity stripe bar chart in the projects dashboard summary currently uses fixed-width bars capped at 10 cells, with glyph-based count encoding (·, ▂, ▅, █). This wastes horizontal space on wide screens and constrains information density. Two improvements are made: (1) proportional bar widths driven by real activity counts so a high-activity day looks visually distinct from a low one; (2) an adaptive day window that expands from 7 up to 14 days as terminal width allows.

## Motivation

- The `cellW > 10` cap limits the stripe to ~76 columns of canvas regardless of pane width.
- Equal-width bars with glyph encoding are space-inefficient and less readable than a real histogram.
- On a large display, seeing 2 weeks of activity history is more useful than 1 week with wasted whitespace.

## Design

### 1. Proportional Bar Widths

Each day's bar width is proportional to its activity count relative to the maximum across the visible window.

```
totalBarWidth = canvasWidth - (numDays - 1) * gap
barWidth[i]   = round(count[i] / maxCount * totalBarWidth)
```

- Non-zero days get at least 1 cell.
- Zero days get 0 bar width (just the gap).
- If all counts are zero, every day gets 1 cell (to keep the stripe visible).
- Remainder pixels from rounding are distributed left-to-right across bars so the total exactly fills the canvas.
- The `cellW > 10` cap is removed.
- Fill glyph is uniform (█) for all bars — no count-based glyph encoding.
- The 4-color encoding is kept: dim gray (238) for zero, blue (39) for 1-2, green (82) for 3-5, orange (214) for 6+. This gives quick visual scanning while bar width carries the precise magnitude.

### 2. Adaptive Day Window (7-14 days)

The number of visible days adapts to available width:

```
minCellW  = 3                              // minimum recognisable bar width
maxDays   = min(14, max(7, (width + gap) / (minCellW + gap)))
```

- Minimum 7 days ensures the chart always has a meaningful horizon.
- Maximum 14 days prevents excessive density on very wide screens.
- On very narrow terminals where 7 days × 3 cells won't fit, fall back to 7 days at 1 cell per bar.
- `activityStripeDayCounts` already accepts a variable `n` parameter — just pass the computed value instead of the hardcoded 7.

### 3. Axis Labels

Replace `"7d ago" / "Yesterday" / "Today"` with a date range:

```
"<earliest-date> — <latest-date>"
```

Example: `"Jun 28 — Jul 12"`. If the label exceeds the canvas width, abbreviate to `"Jun28 — Jul12"`.

## Edge Cases

| Case | Behavior |
|------|----------|
| All zero counts | Equal 1-cell bars with dim gray · glyph |
| Narrow terminal (< 7×3 cells) | 7 days at min 1 cell per bar |
| Fewer than N days of log data | Existing zero-fill in `activityStripeDayCounts` handles this |
| Rounding remainder | Distributed left-to-right across bars |
| Date label overflow | Abbreviate dates (e.g. `Jun28 — Jul12`) |

## Files

- `internal/tui/projects.go` — `renderActivityStripeCanvas`, `renderActivityStripeChart`, `activityStripeDayCounts`, axis rendering
- `internal/tui/projects_test.go` — golden tests for new behavior

## Verification

- `make verify` (build + test + lint)
- TUI manual testing at 80-col and 200-col terminal widths
