# Task 1 Report: Add Pure Summary Data Helpers

## Summary

Implemented the pure summary helper functions required for the project summary charts work:

- `namespaceCount`
- `projectPaneSplitHeights(total int) (int, int)`
- `labelNamespaceCounts(tasks []*store.Task) []namespaceCount`
- `activityDayCounts(project *store.Project, tasks []*store.Task) map[string]int`
- `activityDensityGlyph(count int) string`

I also added focused tests in `internal/tui/app_test.go` covering split heights, label namespace aggregation, activity day aggregation, and density glyph mapping.

## RED

Added the tests first and ran the exact focused command from the brief:

```sh
go test ./internal/tui -run 'TestProjectPaneSplitHeights|TestLabelNamespaceCounts|TestActivityDayCountsIncludesProjectAndTaskHistory' -count=1
```

Expected failure was confirmed:

```text
undefined: projectPaneSplitHeights
undefined: labelNamespaceCounts
undefined: namespaceCount
undefined: activityDayCounts
undefined: activityDensityGlyph
```

## GREEN

Implemented the helpers in `internal/tui/projects.go` and reran the same focused test command. It passed, then I ran the broader package test:

```sh
go test ./internal/tui -count=1
```

That also passed.

## Files Changed

- `internal/tui/app_test.go`
- `internal/tui/projects.go`

## Tests Run

- `go test ./internal/tui -run 'TestProjectPaneSplitHeights|TestLabelNamespaceCounts|TestActivityDayCountsIncludesProjectAndTaskHistory' -count=1`
- `go test ./internal/tui -count=1`

## Self-Review

- The helpers are pure and isolated from rendering, as required.
- The label namespace logic follows the brief's exact fallback behavior for non-namespaced labels.
- The activity aggregation includes project history and task history, and normalizes timestamps to UTC date keys.
- I added a small nil-task guard in `activityDayCounts` to avoid a panic on unexpected inputs; it does not affect the specified test cases.

## Commit

- `4e66e1c` - `feat: add project summary chart helpers`
