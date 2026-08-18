# Task 3 Report: Braille Pulse Line Renderer

## Scope

Implemented only the Task 3 renderer helpers and focused tests:

- `relXLabelFormatter(end time.Time) linechart.LabelFormatter`
- `renderActivityPulse(counts []int, spec chartRangeSpec, width, height int, end time.Time, graph, axis, label lipgloss.Style) string`

No `projects.go` integration was added.

## TDD Evidence

### RED

Added `TestRenderActivityPulse` and `TestRenderActivityPulseNilCountsReturnsEmpty` before production implementation. The required focused command failed because the helpers were absent:

```text
# atm/internal/tui [atm/internal/tui.test]
internal/tui/activity_chart_test.go:191:9: undefined: renderActivityPulse
internal/tui/activity_chart_test.go:219:9: undefined: renderActivityPulse
FAIL	atm/internal/tui [build failed]
```

### GREEN

After implementation, the focused command passed:

```text
=== RUN   TestRenderActivityPulse
--- PASS: TestRenderActivityPulse (0.00s)
=== RUN   TestRenderActivityPulseNilCountsReturnsEmpty
--- PASS: TestRenderActivityPulseNilCountsReturnsEmpty (0.00s)
PASS
ok  	atm/internal/tui	0.004s
```

## Implementation Notes

- Uses pinned ntcharts v0.5.1 `timeserieslinechart` and `DrawBraille`.
- Maps each count to `start + i*bucketDays`, where `start` comes from `chartWindow`.
- Uses a zero-to-maximum Y range and falls back to maximum `1` for all-zero data.
- Returns empty output for nil/empty counts, width below `12`, or height below `2`.
- Configures graph, axis, and label styles through ntcharts options.
- Adds one bucket-sized display padding after the data window so the terminal can show the final `Today` label when width allows; data points remain aligned to the original window.
- The renderer returns ntcharts output at the requested chart height.

## Verification

- `go test ./internal/tui/ -run TestRenderActivityPulse -v`: passed.
- `go test ./internal/tui/`: passed.
- `make verify`: passed.
  - `go build`: passed.
  - `go test ./... ./libs/eventsource/...`: passed; `internal/tui` passed in 175.057s.
  - `tests/scripts/runner.sh`: `26 passed, 0 failed`.
- `git diff --check`: passed.

## Self-Review

- Diff is limited to `internal/tui/activity_chart.go`, `internal/tui/activity_chart_test.go`, and this report.
- No new dependencies were introduced.
- No project view integration was added.
- Focused tests cover non-empty output, requested line count, `Today`, braille output, and nil counts.

## Fix Report: Real Chart Window Boundary

### Finding Addressed

Removed the future-bucket display extension from `renderActivityPulse`. The ntcharts time range now uses the `start` and `endDay` returned directly by `chartWindow`; the earlier implementation note about display padding is superseded by this correction.

Added regression coverage for the formatter at the exact chart-window end, plus empty non-nil counts and width/height guard cases.

### Changed Files

- `internal/tui/activity_chart.go`
- `internal/tui/activity_chart_test.go`
- This report file

### Verification

Commands:

```text
go test ./internal/tui/ -run TestRenderActivityPulse -v
go test ./internal/tui/ -run 'Test(RenderActivityPulse|RelXLabelFormatter)' -count=1 -v
```

Output:

```text
=== RUN   TestRenderActivityPulse
--- PASS: TestRenderActivityPulse (0.00s)
=== RUN   TestRenderActivityPulseNilCountsReturnsEmpty
--- PASS: TestRenderActivityPulseNilCountsReturnsEmpty (0.00s)
=== RUN   TestRenderActivityPulseEmptyAndTooSmallReturnsEmpty
--- PASS: TestRenderActivityPulseEmptyAndTooSmallReturnsEmpty (0.00s)
PASS
ok  	atm/internal/tui	0.003s
```

The uncached broader focused command also passed with the same renderer results plus `TestRelXLabelFormatterUsesChartWindowBoundary`; `git diff --check` passed.
