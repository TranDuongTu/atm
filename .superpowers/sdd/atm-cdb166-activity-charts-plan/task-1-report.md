# Task 1 Report: Bucketing and Windowed Aggregation

## Scope

Implemented the pure activity-chart helpers requested for ATM-cdb166 Task 1:

- Added `chartRangeSpec` and the five configured chart ranges.
- Added UTC-normalized, inclusive `chartWindow` calculation.
- Added oldest-first daily and weekly `activityBucketCounts` with optional persona filtering through `actor.Resolve`.
- Added windowed `aggregateWindow` population for counts, agents, models, and actions.
- Added relative day labels for day, week, month, and year-scale values.
- Added focused tests only; no `projects.go` integration was changed.

## TDD Evidence

### RED

Command:

```text
go test ./internal/tui/ -run 'TestActivityBucket|TestAggregateWindow|TestRelDayLabel' -v
```

Result: failed at compilation because the new helpers were not yet defined. The focused test file reported undefined symbols for `activityBucketCounts`, `chartRanges`, `aggregateWindow`, and `relDayLabel`.

### GREEN

Command:

```text
go test ./internal/tui/ -run 'TestActivityBucket|TestAggregateWindow|TestRelDayLabel' -v
```

Result:

```text
=== RUN   TestActivityBucketCountsDailyWindow
--- PASS: TestActivityBucketCountsDailyWindow (0.00s)
=== RUN   TestActivityBucketCountsFiltersPersonaAndPlacesWeeklyEntries
--- PASS: TestActivityBucketCountsFiltersPersonaAndPlacesWeeklyEntries (0.00s)
=== RUN   TestAggregateWindowCountsModelsForPersonaAndAll
--- PASS: TestAggregateWindowCountsModelsForPersonaAndAll (0.00s)
=== RUN   TestRelDayLabel
--- PASS: TestRelDayLabel (0.00s)
PASS
```

## Verification

- `go test ./internal/tui/`: passed.
- `make verify`: passed.
- Repository Go tests passed, including `internal/tui`.
- Script tests: `26 passed, 0 failed`.
- `git diff --check`: passed.

## Review

The change is limited to `internal/tui/activity_chart.go` and `internal/tui/activity_chart_test.go`, plus this report. Window boundaries are normalized to UTC midnight, and entries at both inclusive endpoints are covered by tests. Zero timestamps and invalid range dimensions are handled without producing counts.
