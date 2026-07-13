# TUI Refresh Recency Indicator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a rightmost refresh freshness indicator showing `✓` while fresh and `↻ <age> ago` when stale.

**Architecture:** Store the last refresh completion time on the root TUI model. Stamp it in `refreshAll()` and render it after plugin dock segments so it is the rightmost status-line item.

**Tech Stack:** Go 1.22+, Bubble Tea, Lip Gloss, existing `internal/tui` test helpers.

## Global Constraints

- Keep the TUI API surface stable.
- Do not add a database or watcher dependency.
- Use the existing periodic `refreshTickMsg -> refreshAll()` path with a 10-second interval.
- Keep the indicator compact enough for the persistent status line.
- No emojis in code or commits; the refresh symbol `↻` and check symbol `✓` are the requested status icons.

---

### Task 1: Refresh Timestamp Model State

**Files:**
- Modify: `internal/tui/app.go`
- Test: `internal/tui/refresh_tick_test.go`

**Interfaces:**
- Produces: `Model.lastRefreshAt time.Time`
- Produces: `refreshAgeLabel(last, now time.Time) string`
- Produces: `Model.refreshRecencyStyle() lipgloss.Style`
- Produces: `Model.refreshRecencySegment() string`

- [ ] **Step 1: Write the failing tests**

Add tests that assert `refreshAll()` stamps `lastRefreshAt`, the refresh tick interval is 10 seconds, the age formatter includes `ago` for stale values, the fresh segment is check-only, the stale segment shows `↻ <age> ago`, and the status line places the indicator rightmost.

- [ ] **Step 2: Run focused tests to verify failure**

Run: `go test ./internal/tui -run 'TestRefreshAllRecordsLastRefreshTime|TestRefreshTickIntervalIsTenSeconds|TestRefreshAgeLabel|TestRefreshRecencySegmentFreshShowsCheckOnly|TestRefreshRecencySegmentStaleShowsAge|TestStatusLineShowsRefreshRecencyRightmost'`

Expected: FAIL before implementation because the model has no refresh timestamp/style helpers or the interval/status behavior is still wrong.

- [ ] **Step 3: Implement minimal production code**

Add `lastRefreshAt time.Time` to `Model`, set it at the end of `refreshAll()`, change `refreshTickInterval` to `10 * time.Second`, add `refreshAgeLabel`, add `refreshRecencyStyle`, and append `m.refreshRecencySegment()` after plugin dock segments.

- [ ] **Step 4: Run focused tests to verify pass**

Run: `go test ./internal/tui -run 'TestRefreshAllRecordsLastRefreshTime|TestRefreshTickIntervalIsTenSeconds|TestRefreshAgeLabel|TestRefreshRecencySegmentFreshShowsCheckOnly|TestRefreshRecencySegmentStaleShowsAge|TestStatusLineShowsRefreshRecencyRightmost'`

Expected: PASS.

- [ ] **Step 5: Run full verification**

Run: `go test ./internal/tui`

Expected: PASS.

Run: `make verify`

Expected: PASS.
