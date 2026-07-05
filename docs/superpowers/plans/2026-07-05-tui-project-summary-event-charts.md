# TUI Project Summary Event Charts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the old project summary chart data with event-sourced activity charts driven by the selected project's audit log.

**Architecture:** Keep aggregation and rendering in `internal/tui/projects.go`. Use `Store.ReadLog(code)` as the activity source, keep helper functions pure, and keep the Projects pane layout unchanged.

**Tech Stack:** Go 1.22+, Bubble Tea TUI, existing `internal/store` audit log API.

## Global Constraints

- Run `make verify` before declaring done.
- Do not add store schema changes or CLI changes.
- Keep charts terminal-friendly, bounded, non-interactive, and resilient on narrow or short terminals.
- No emojis in code or commits.

---

### Task 1: Event Activity Aggregation

**Files:**
- Modify: `internal/tui/app_test.go`
- Modify: `internal/tui/projects.go`

**Interfaces:**
- Consumes: `[]store.LogEntry`
- Produces: `actorActivityRows(entries []store.LogEntry, limit int) []actorActivityRow`
- Produces: `activityStripeDayCounts(entries []store.LogEntry, days int) []activityStripeDay`

- [ ] **Step 1: Write failing tests** for actor counts, actor sorting, `others` folding, and one-week stripe bucketing in `internal/tui/app_test.go`.
- [ ] **Step 2: Run tests** with `go test ./internal/tui -run 'TestActorActivityRows|TestActivityStripe'` and confirm compile/test failures reference missing helpers or old behavior.
- [ ] **Step 3: Implement helpers** in `internal/tui/projects.go` using `store.LogEntry`.
- [ ] **Step 4: Run tests** with the same command and confirm they pass.

### Task 2: Summary Rendering

**Files:**
- Modify: `internal/tui/app_test.go`
- Modify: `internal/tui/projects.go`

**Interfaces:**
- Consumes: `projectSummaryData() (*store.Project, []*store.Task, []store.LogEntry, bool)`
- Produces: centered chart boxes titled `activity by actor`, `activity stripe`, and `bubbles`.

- [ ] **Step 1: Update failing render tests** so selected project summaries assert the event-sourced chart titles and no longer assert `Labels pie`.
- [ ] **Step 2: Run tests** with `go test ./internal/tui -run 'TestSelectedProjectSummary|TestProjectSummary|TestKeywordSummary|TestProjectDetailDoesNotRenderSummaryCharts'`.
- [ ] **Step 3: Update rendering** to call `Store.ReadLog`, render actor meters first, render the seven-day stripe second, and keep the bubbles placeholder third.
- [ ] **Step 4: Run the same tests** and confirm they pass.

### Task 3: Full Verification

**Files:**
- Modify: `docs/superpowers/specs/2026-07-04-tui-project-summary-charts-design.md`
- Create: `docs/superpowers/plans/2026-07-05-tui-project-summary-event-charts.md`

**Interfaces:**
- Consumes: repository verification target `make verify`
- Produces: clean verification result

- [ ] **Step 1: Run focused TUI tests** with `go test ./internal/tui`.
- [ ] **Step 2: Run repository verification** with `make verify`.
- [ ] **Step 3: Inspect `git diff --check` and `git status --short`** to confirm no whitespace errors and only intended files changed.

### Task 4: Boxed Charts and ntcharts Rendering

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/tui/app_test.go`
- Modify: `internal/tui/projects.go`
- Modify: `docs/superpowers/specs/2026-07-04-tui-project-summary-charts-design.md`

**Interfaces:**
- Consumes: `[]store.LogEntry`
- Produces: centered chart boxes with quiet lowercase titles.
- Produces: `renderActivityStripeCanvas(days []activityStripeDay, width int) string`
- Produces: `renderSampleBubbleCanvas(width int) string`

- [ ] **Step 1: Write failing tests** for centered chart boxes, quiet lowercase titles, ntcharts-backed stripe output, and sample bubble placeholders.
- [ ] **Step 2: Run focused tests** with `go test ./internal/tui -run 'TestProjectSummaryChartBoxes|TestRenderActivityStripe|TestRenderSampleBubble'` and confirm failures.
- [ ] **Step 3: Add `github.com/NimbleMarkets/ntcharts v0.5.1`** and render the stripe/bubbles through `ntcharts/canvas`.
- [ ] **Step 4: Run focused tests** with the same command and confirm they pass.
- [ ] **Step 5: Run `make verify`** before declaring the refinement complete.
