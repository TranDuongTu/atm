# ATM TUI Dashboard Layout Refinement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the TUI feel more dashboard-like while switching the default theme to graphite and removing atm-dark.

**Architecture:** Keep the existing root `Model` theme ownership and pane renderers, then update theme helpers and rendering strings to create dashboard sections with existing Lip Gloss helpers. No store or command behavior changes.

**Tech Stack:** Go 1.22+, Bubble Tea, Lip Gloss, existing `internal/tui` tests, `make verify`.

## Global Constraints

- Themes are runtime-only and write nothing to `$ATM_HOME`.
- Built-in theme order is `graphite -> light -> mono -> graphite`.
- Default theme is `graphite`.
- `atm-dark` is not a built-in theme.
- No store changes.
- No label or task behavior changes.
- No new TUI tabs, panes, or data model concepts.
- No CLI flags or config commands for themes.
- Final completion requires `make verify`.

---

## File Structure

- Modify `internal/tui/theme.go`: remove `atm-dark`, default to `graphite`, update unknown fallback and cycle order.
- Modify `internal/tui/app_test.go`: update theme tests and add dashboard rendering assertions for Projects, Tasks, detail, and Help.
- Modify `internal/tui/labels_test.go`: add namespace and missing-description assertions.
- Modify `internal/tui/projects.go`: render Projects list and detail as dashboard sections.
- Modify `internal/tui/tasks.go`: render Tasks list, grouped blocks, and task detail as dashboard sections.
- Modify `internal/tui/labels.go`: render namespace groups, missing descriptions, and label detail as dashboard sections.
- Modify `internal/tui/help.go`: render Help as titled reference sections with formatted conventions text.
- Modify `internal/tui/styles.go`: add a shared imprinted section divider helper.

## Tasks

### Task 1: Theme Defaults

- [x] Write failing tests asserting default `graphite`, cycle order `graphite -> light -> mono -> graphite`, unknown fallback to `graphite`, and default status line `theme: graphite`.
- [x] Run `go test ./internal/tui -run 'Test(DefaultTheme|NextThemeNameWraps|ThemeCycleKeyUpdatesThemeAndStatus)' -count=1` and confirm failure.
- [x] Update `internal/tui/theme.go` to remove `themeATMDark`, set `themeOrder` to graphite/light/mono, and default/fallback to graphite.
- [x] Update existing theme input tests expecting text-entry `T` to leave the theme at graphite.
- [x] Re-run the focused theme tests and confirm they pass.

### Task 2: Dashboard List Panes

- [x] Write failing tests for Projects dashboard title/summary, Tasks dashboard header/title-primary rows, grouped task block headers, Labels namespace sections, and missing-description callout.
- [x] Run `go test ./internal/tui -run 'Test(ProjectsListDashboard|TasksListDashboard|TasksGroupedDashboard|LabelsTab)' -count=1` and confirm failure for new expectations.
- [x] Update `projects.go`, `tasks.go`, and `labels.go` list renderers to use titled dashboard sections and stable textual section labels.
- [x] Re-run the focused list tests and confirm they pass.

### Task 3: Dashboard Detail and Help Panes

- [x] Write failing tests for Project/Task/Label detail section titles and Help reference section titles.
- [x] Run `go test ./internal/tui -run 'Test(ProjectDetailDashboard|TaskDetail|LabelDetailDashboard|HelpTab)' -count=1` and confirm failure for new expectations.
- [x] Update detail renderers in `projects.go`, `tasks.go`, `labels.go`, and `help.go` to render title, facts, labels/history/actions, and reference sections.
- [x] Re-run focused detail/help tests and confirm they pass.

### Task 4: Verification

- [x] Run `go test ./internal/tui ./internal/store -count=1`.
- [x] Run `make verify`.
- [x] Review `git diff` for accidental behavior or store changes.

### Task 5: Section Dividers and Help Formatting

- [x] Write failing tests asserting Projects, Tasks, and Labels list bodies do not start with redundant tab titles.
- [x] Write failing tests asserting section titles render through imprinted dividers such as `Overview`, `Facts`, `Actions`, and `Conventions`.
- [x] Write failing tests asserting Help conventions content does not expose raw markdown heading markers like `## Suggested seed namespaces`.
- [x] Run `go test ./internal/tui -run 'Test(ProjectsListPopulated|TasksFlatListEmptyFilter|LabelsTabListSeededLabels|HelpTabConventions)' -count=1` and confirm failure for the new expectations.
- [x] Add a shared divider helper in `internal/tui/styles.go`.
- [x] Update Projects, Tasks, Labels, and Help renderers to use section dividers and remove redundant list-pane body titles.
- [x] Re-run the focused tests and confirm they pass.
- [x] Run `go test ./internal/tui ./internal/store -count=1`.
- [x] Run `make verify`.

### Task 6: Centered Wider Section Content

- [x] Write failing tests asserting wide terminal section dividers are centered and wider than 78 columns.
- [x] Write failing tests asserting section content begins at the same centered column as its divider.
- [x] Run `go test ./internal/tui -run 'Test(SectionDivider|ProjectsListPopulated|HelpTabParityTable)' -count=1` and confirm failure.
- [x] Add shared content-width and centered-line helpers in `internal/tui/styles.go`.
- [x] Update Projects, Tasks, Labels, and Help list/detail renderers to render divider and section content through the same centered column.
- [x] Re-run focused tests and confirm they pass.
- [x] Run `go test ./internal/tui ./internal/store -count=1`.
- [x] Run `make verify`.
