# TUI Project Summary Charts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add project-bound summary charts below the Projects list in the ATM TUI.

**Architecture:** Keep the feature inside `internal/tui`: pure helper functions compute summary chart data from existing `store.Project` and `store.Task` values, and `projectsModel` renders that data below the compact project list. The root three-pane workspace, store schema, CLI, focus model, and mutation behavior remain unchanged.

**Tech Stack:** Go 1.22+, Bubble Tea model code, lipgloss rendering helpers already present in `internal/tui`, existing `internal/store` APIs.

## Global Constraints

- Use the approved spec: `docs/superpowers/specs/2026-07-04-tui-project-summary-charts-design.md`.
- No store schema changes.
- No CLI changes.
- No new project or task entities.
- No persistent analytics cache.
- No mouse-driven charts.
- No interactive chart drill-down.
- No agent integration for keyword extraction in this iteration.
- No exact audit-reporting guarantees for chart numbers.
- Render summaries only when a project is selected.
- Put the summary region below the project list, taking roughly 70 percent of the Projects pane body height.
- Keep charts terminal-friendly, bounded, and non-interactive.
- Run `make verify` before declaring done.

---

## File Structure

- Modify `internal/tui/projects.go`: add summary data helpers, internal height split helper, and summary rendering inside `projectsModel.renderList`.
- Modify `internal/tui/app_test.go`: add focused TUI tests near the existing Projects pane tests.
- No changes to `internal/store`.
- No changes to `internal/cli`.
- No new dependencies.

---

### Task 1: Add Pure Summary Data Helpers

**Files:**
- Modify: `internal/tui/projects.go`
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: `store.Task`, `store.Project`, existing `store.HistoryEntry`.
- Produces:
  - `type namespaceCount struct { namespace string; count int }`
  - `func projectPaneSplitHeights(total int) (listH int, summaryH int)`
  - `func labelNamespaceCounts(tasks []*store.Task) []namespaceCount`
  - `func activityDayCounts(project *store.Project, tasks []*store.Task) map[string]int`
  - `func activityDensityGlyph(count int) string`

- [ ] **Step 1: Write failing tests for split, label counts, and activity counts**

Add these tests to `internal/tui/app_test.go` near the existing Projects tests:

```go
func TestProjectPaneSplitHeights(t *testing.T) {
	listH, summaryH := projectPaneSplitHeights(30)
	if listH != 9 || summaryH != 21 {
		t.Fatalf("projectPaneSplitHeights(30) = (%d,%d), want (9,21)", listH, summaryH)
	}
	listH, summaryH = projectPaneSplitHeights(3)
	if listH < 1 || summaryH < 1 || listH+summaryH != 3 {
		t.Fatalf("projectPaneSplitHeights(3) = (%d,%d), want positive heights summing to 3", listH, summaryH)
	}
	listH, summaryH = projectPaneSplitHeights(1)
	if listH != 1 || summaryH != 0 {
		t.Fatalf("projectPaneSplitHeights(1) = (%d,%d), want (1,0)", listH, summaryH)
	}
}

func TestLabelNamespaceCounts(t *testing.T) {
	tasks := []*store.Task{
		{Labels: []string{"ATM:status:open", "ATM:type:bug", "ATM:priority:high", "ATM:urgent"}},
		{Labels: []string{"ATM:status:done", "ATM:type:bug", "ATM:type:refactor"}},
		{Labels: []string{"ATM:context:agent"}},
	}
	got := labelNamespaceCounts(tasks)
	want := []namespaceCount{
		{namespace: "type", count: 3},
		{namespace: "status", count: 2},
		{namespace: "context", count: 1},
		{namespace: "priority", count: 1},
		{namespace: "tags", count: 1},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("labelNamespaceCounts() = %#v, want %#v", got, want)
	}
}

func TestActivityDayCountsIncludesProjectAndTaskHistory(t *testing.T) {
	mustTime := func(s string) time.Time {
		t.Helper()
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("time.Parse(%q): %v", s, err)
		}
		return ts
	}
	day1 := mustTime("2026-07-01T10:00:00Z")
	day2 := mustTime("2026-07-02T10:00:00Z")
	project := &store.Project{
		History: []store.HistoryEntry{
			{ID: "h1", Action: "created", Actor: "claude", At: day1},
			{ID: "h2", Action: "name-changed", Actor: "claude", At: day2},
		},
	}
	tasks := []*store.Task{
		{History: []store.HistoryEntry{
			{ID: "h1", Action: "created", Actor: "claude", At: day1},
			{ID: "h2", Action: "label-added", Actor: "claude", At: day2},
		}},
		{History: []store.HistoryEntry{
			{ID: "h1", Action: "created", Actor: "claude", At: day2},
		}},
	}
	got := activityDayCounts(project, tasks)
	want := map[string]int{
		"2026-07-01": 2,
		"2026-07-02": 3,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("activityDayCounts() = %#v, want %#v", got, want)
	}
	if activityDensityGlyph(0) != "·" || activityDensityGlyph(1) != "░" || activityDensityGlyph(3) != "▒" || activityDensityGlyph(6) != "▓" || activityDensityGlyph(10) != "█" {
		t.Fatalf("activityDensityGlyph returned unexpected density marks")
	}
}
```

If `internal/tui/app_test.go` does not already import `time`, add it to the import block:

```go
import (
	"fmt"
	"strings"
	"testing"
	"time"

	"atm/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```sh
go test ./internal/tui -run 'TestProjectPaneSplitHeights|TestLabelNamespaceCounts|TestActivityDayCountsIncludesProjectAndTaskHistory' -count=1
```

Expected: FAIL with undefined identifiers such as `projectPaneSplitHeights`, `labelNamespaceCounts`, `namespaceCount`, `activityDayCounts`, or `activityDensityGlyph`.

- [ ] **Step 3: Implement the pure helpers**

In `internal/tui/projects.go`, update imports and add helpers near `projRow` or before `renderList`:

```go
import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)
```

Add:

```go
type namespaceCount struct {
	namespace string
	count     int
}

func projectPaneSplitHeights(total int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if total == 1 {
		return 1, 0
	}
	listH := total * 30 / 100
	if listH < 1 {
		listH = 1
	}
	summaryH := total - listH
	if summaryH < 1 {
		summaryH = 1
		listH = total - summaryH
		if listH < 1 {
			listH = 1
			summaryH = 0
		}
	}
	return listH, summaryH
}

func labelNamespaceCounts(tasks []*store.Task) []namespaceCount {
	counts := map[string]int{}
	for _, tk := range tasks {
		for _, label := range tk.Labels {
			parts := strings.Split(label, ":")
			ns := "tags"
			if len(parts) >= 3 {
				ns = parts[1]
			}
			counts[ns]++
		}
	}
	out := make([]namespaceCount, 0, len(counts))
	for ns, count := range counts {
		out = append(out, namespaceCount{namespace: ns, count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count == out[j].count {
			return out[i].namespace < out[j].namespace
		}
		return out[i].count > out[j].count
	})
	return out
}

func activityDayCounts(project *store.Project, tasks []*store.Task) map[string]int {
	counts := map[string]int{}
	if project != nil {
		for _, h := range project.History {
			counts[h.At.UTC().Format("2006-01-02")]++
		}
	}
	for _, tk := range tasks {
		for _, h := range tk.History {
			counts[h.At.UTC().Format("2006-01-02")]++
		}
	}
	return counts
}

func activityDensityGlyph(count int) string {
	switch {
	case count <= 0:
		return "·"
	case count <= 2:
		return "░"
	case count <= 5:
		return "▒"
	case count <= 9:
		return "▓"
	default:
		return "█"
	}
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run:

```sh
go test ./internal/tui -run 'TestProjectPaneSplitHeights|TestLabelNamespaceCounts|TestActivityDayCountsIncludesProjectAndTaskHistory' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```sh
git add internal/tui/projects.go internal/tui/app_test.go
git commit -m "feat: add project summary chart helpers"
```

---

### Task 2: Render Summary Region Below Compact Projects List

**Files:**
- Modify: `internal/tui/projects.go`
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes from Task 1: `projectPaneSplitHeights`.
- Produces:
  - `func (p *projectsModel) renderListRows(maxRows int) string`
  - `func (p *projectsModel) renderSummary(height int) string`
  - `func (p *projectsModel) projectSummaryData() (*store.Project, []*store.Task, bool)`

- [ ] **Step 1: Write failing rendering tests**

Add these tests near `TestProjectsListDashboard` in `internal/tui/app_test.go`:

```go
func TestProjectsListRendersSummaryRegionBelowList(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	body := m.projects.View()
	mustContain(t, body, "─ Overview ─")
	mustContain(t, body, "─ Project Summary ─")
	mustContain(t, body, "select a project to see summaries")
	overviewIdx := strings.Index(body, "─ Overview ─")
	summaryIdx := strings.Index(body, "─ Project Summary ─")
	if overviewIdx < 0 || summaryIdx < 0 || summaryIdx <= overviewIdx {
		t.Fatalf("summary should render below overview\n--- body ---\n%s", body)
	}
}

func TestProjectsListSummaryUsesSelectedProjectNotCursor(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "SCY", "Scylla")
	update(t, m, "s")
	update(t, m, "j")
	body := m.projects.View()
	mustContain(t, body, "project: ATM")
	if m.projectScope != "ATM" {
		t.Fatalf("projectScope = %q want ATM", m.projectScope)
	}
}

func TestProjectDetailDoesNotRenderSummaryCharts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	update(t, m, "enter")
	body := m.projects.View()
	mustContain(t, body, "Project ATM")
	mustNotContain(t, body, "Project Summary")
	mustNotContain(t, body, "Labels by namespace")
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```sh
go test ./internal/tui -run 'TestProjectsListRendersSummaryRegionBelowList|TestProjectsListSummaryUsesSelectedProjectNotCursor|TestProjectDetailDoesNotRenderSummaryCharts' -count=1
```

Expected: FAIL because `Project Summary` is not rendered yet.

- [ ] **Step 3: Refactor `renderList` into compact list plus summary region**

Replace `projectsModel.renderList` in `internal/tui/projects.go` with:

```go
func (p *projectsModel) renderList() string {
	if len(p.list) == 0 {
		return p.renderEmpty()
	}
	listH, summaryH := projectPaneSplitHeights(p.contentHeight)
	var parts []string
	if listH > 0 {
		parts = append(parts, padToHeight(p.renderListRows(listH), listH))
	}
	if summaryH > 0 {
		parts = append(parts, padToHeight(p.renderSummary(summaryH), summaryH))
	}
	return padToHeight(strings.Join(parts, "\n"), p.contentHeight)
}
```

Add `renderListRows` below it:

```go
func (p *projectsModel) renderListRows(maxRows int) string {
	var b strings.Builder
	selected := p.m.projectScope
	if selected == "" {
		selected = "none"
	}
	fmt.Fprintf(&b, "%s\n", sectionDivider(p.m.styles, p.width, "Overview"))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("total projects: %d   selected: %s", len(p.list), selected)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.HeaderLabel.Render(fmt.Sprintf("%-6s %-30s %6s %7s %10s", "CODE", "NAME", "TASKS", "LABELS", "UPDATED"))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, repeat("─", dashboardContentWidth(p.width))))

	availableRows := maxRows - 4
	if availableRows < 0 {
		availableRows = 0
	}
	for i, r := range p.list {
		if i >= availableRows {
			remaining := len(p.list) - i
			if remaining > 0 && availableRows > 0 {
				fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.Muted.Render(fmt.Sprintf("... %d more projects", remaining))))
			}
			break
		}
		var gutter string
		if r.code == p.m.projectScope {
			gutter = p.m.styles.GutterSelect.Render("▸")
		} else {
			gutter = " "
		}
		line := fmt.Sprintf(" %-5s %-30s %6d %7d %10s", r.code, truncateRunes(r.name, 30), r.tasks, r.labels, r.updated)
		if i == p.cursor {
			line = gutter + " " + p.m.styles.RowCursor.Render(line)
		} else {
			line = gutter + " " + line
		}
		fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, line))
	}
	return b.String()
}
```

Add initial `renderSummary` and `projectSummaryData`:

```go
func (p *projectsModel) renderSummary(height int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", sectionDivider(p.m.styles, p.width, "Project Summary"))
	if p.m.projectScope == "" {
		fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.Muted.Render("select a project to see summaries")))
		return padToHeight(b.String(), height)
	}
	project, tasks, ok := p.projectSummaryData()
	if !ok {
		fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.Muted.Render("selected project could not be loaded")))
		return padToHeight(b.String(), height)
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("project: %s   tasks: %d", project.Code, len(tasks))))
	return padToHeight(b.String(), height)
}

func (p *projectsModel) projectSummaryData() (*store.Project, []*store.Task, bool) {
	code := p.m.projectScope
	if code == "" {
		return nil, nil, false
	}
	project, err := p.m.store.GetProject(code)
	if err != nil {
		return nil, nil, false
	}
	tasks := p.m.store.ListTasks(store.QueryFilters{Project: code})
	return project, tasks, true
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run:

```sh
go test ./internal/tui -run 'TestProjectsListRendersSummaryRegionBelowList|TestProjectsListSummaryUsesSelectedProjectNotCursor|TestProjectDetailDoesNotRenderSummaryCharts|TestProjectsListDashboard|TestProjectsListCursorVsSelectionIndependent' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```sh
git add internal/tui/projects.go internal/tui/app_test.go
git commit -m "feat: split projects pane summary region"
```

---

### Task 3: Render Label Namespace and Activity Charts

**Files:**
- Modify: `internal/tui/projects.go`
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes from Task 1: `labelNamespaceCounts`, `activityDayCounts`, `activityDensityGlyph`.
- Produces:
  - `func (p *projectsModel) renderLabelNamespaceChart(tasks []*store.Task, maxLines int) []string`
  - `func (p *projectsModel) renderActivityChart(project *store.Project, tasks []*store.Task, width int) string`
  - `func renderActivityDensity(counts map[string]int, width int) string`

- [ ] **Step 1: Write failing chart rendering tests**

Add these tests to `internal/tui/app_test.go`:

```go
func TestSelectedProjectSummaryRendersCharts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(140, 48)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "bug one", "ATM:status:open", "ATM:type:bug", "ATM:urgent")
	seedTask(t, m, "ATM", "bug two", "ATM:status:open", "ATM:type:bug")
	update(t, m, "s")
	body := m.projects.View()
	mustContain(t, body, "Labels by namespace")
	mustContain(t, body, "status")
	mustContain(t, body, "type")
	mustContain(t, body, "tags")
	mustContain(t, body, "Activity")
	mustContain(t, body, "Keywords")
	mustContain(t, body, "agent-generated keyword bubbles pending")
}

func TestRenderActivityDensityDeterministic(t *testing.T) {
	counts := map[string]int{
		"2026-07-01": 1,
		"2026-07-02": 3,
		"2026-07-03": 10,
	}
	got := renderActivityDensity(counts, 10)
	want := "░▒█"
	if got != want {
		t.Fatalf("renderActivityDensity() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```sh
go test ./internal/tui -run 'TestSelectedProjectSummaryRendersCharts|TestRenderActivityDensityDeterministic' -count=1
```

Expected: FAIL because chart sections and `renderActivityDensity` are not implemented.

- [ ] **Step 3: Implement chart rendering helpers**

Add these functions to `internal/tui/projects.go`:

```go
func (p *projectsModel) renderLabelNamespaceChart(tasks []*store.Task, maxLines int) []string {
	if maxLines <= 0 {
		return nil
	}
	counts := labelNamespaceCounts(tasks)
	lines := []string{dashboardLine(p.width, p.m.styles.HeaderLabel.Render("Labels by namespace"))}
	if len(counts) == 0 {
		return append(lines, dashboardLine(p.width, p.m.styles.Muted.Render("no task labels yet")))
	}
	maxCount := counts[0].count
	for i, nc := range counts {
		if i >= maxLines-1 {
			break
		}
		barW := 10
		if p.width < 34 {
			barW = 5
		}
		fill := 1
		if maxCount > 0 {
			fill = nc.count * barW / maxCount
			if fill < 1 {
				fill = 1
			}
		}
		bar := repeat("█", fill)
		if fill < barW {
			bar += repeat("░", barW-fill)
		}
		line := fmt.Sprintf("%-10s %s %d", truncateRunes(nc.namespace, 10), bar, nc.count)
		lines = append(lines, dashboardLine(p.width, line))
	}
	return lines
}

func (p *projectsModel) renderActivityChart(project *store.Project, tasks []*store.Task, width int) string {
	counts := activityDayCounts(project, tasks)
	if len(counts) == 0 {
		return p.m.styles.Muted.Render("no activity yet")
	}
	return renderActivityDensity(counts, width)
}

func renderActivityDensity(counts map[string]int, width int) string {
	if len(counts) == 0 || width <= 0 {
		return ""
	}
	days := make([]string, 0, len(counts))
	for day := range counts {
		days = append(days, day)
	}
	sort.Strings(days)
	if len(days) > width {
		days = days[len(days)-width:]
	}
	var b strings.Builder
	for _, day := range days {
		b.WriteString(activityDensityGlyph(counts[day]))
	}
	return b.String()
}
```

Update `renderSummary` so the selected-project branch renders all chart sections:

```go
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("project: %s   tasks: %d", project.Code, len(tasks))))
	b.WriteString("\n")
	for _, line := range p.renderLabelNamespaceChart(tasks, 5) {
		fmt.Fprintf(&b, "%s\n", line)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.HeaderLabel.Render("Activity")))
	activityWidth := dashboardContentWidth(p.width)
	if activityWidth > 42 {
		activityWidth = 42
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.renderActivityChart(project, tasks, activityWidth)))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.HeaderLabel.Render("Keywords")))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.Muted.Render("agent-generated keyword bubbles pending")))
	return padToHeight(b.String(), height)
```

- [ ] **Step 4: Run tests and verify they pass**

Run:

```sh
go test ./internal/tui -run 'TestSelectedProjectSummaryRendersCharts|TestRenderActivityDensityDeterministic|TestLabelNamespaceCounts|TestActivityDayCountsIncludesProjectAndTaskHistory' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```sh
git add internal/tui/projects.go internal/tui/app_test.go
git commit -m "feat: render project summary charts"
```

---

### Task 4: Verify Selection, Short-Terminal Behavior, and Regression Coverage

**Files:**
- Modify: `internal/tui/app_test.go`
- Modify only if tests reveal a bug: `internal/tui/projects.go`

**Interfaces:**
- Consumes from Tasks 1-3: rendered summary region and chart helper functions.
- Produces: regression tests proving the spec's edge cases.

- [ ] **Step 1: Write regression tests for removal and short sizes**

Add these tests to `internal/tui/app_test.go`:

```go
func TestProjectSummaryClearsWhenSelectedProjectRemoved(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	if m.projectScope != "ATM" {
		t.Fatalf("projectScope = %q want ATM", m.projectScope)
	}
	update(t, m, "x")
	update(t, m, "enter")
	if m.projectScope != "" {
		t.Fatalf("projectScope after removal = %q want empty", m.projectScope)
	}
	mustContain(t, m.projects.View(), "no projects")
}

func TestProjectSummaryRendersOnShortTerminalWithoutPanic(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(50, 8)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("View panicked on short terminal: %v", r)
		}
	}()
	_ = m.View()
}

func TestKeywordSummaryDoesNotOpenFormOrConfirm(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	body := m.projects.View()
	mustContain(t, body, "agent-generated keyword bubbles pending")
	if m.form != nil {
		t.Fatalf("keyword placeholder opened form")
	}
	if m.confirm != confirmNone {
		t.Fatalf("keyword placeholder opened confirm = %v", m.confirm)
	}
}
```

- [ ] **Step 2: Run focused regression tests**

Run:

```sh
go test ./internal/tui -run 'TestProjectSummaryClearsWhenSelectedProjectRemoved|TestProjectSummaryRendersOnShortTerminalWithoutPanic|TestKeywordSummaryDoesNotOpenFormOrConfirm' -count=1
```

Expected: PASS. If `TestProjectSummaryClearsWhenSelectedProjectRemoved` fails because seeded default labels prevent project removal only when tasks exist, adjust only the test setup, not store behavior.

- [ ] **Step 3: Run the full TUI package tests**

Run:

```sh
go test ./internal/tui -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

```sh
git add internal/tui/app_test.go internal/tui/projects.go
git commit -m "test: cover project summary chart edge cases"
```

---

### Task 5: Final Verification

**Files:**
- No planned code changes.
- If verification fails, fix the smallest relevant file and rerun the failed command before continuing.

**Interfaces:**
- Consumes all prior tasks.
- Produces a verified development branch.

- [ ] **Step 1: Run repository verification**

Run:

```sh
make verify
```

Expected: PASS.

- [ ] **Step 2: Check git status**

Run:

```sh
git status --short
```

Expected: clean working tree after committed task changes, or only intentionally untracked local scratch files outside the implementation.

- [ ] **Step 3: Summarize verification evidence**

Record the final `make verify` result and the commits created during execution in the implementation handoff or final response.
