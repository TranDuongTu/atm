# ATM TUI Visual Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove banner-style `sectionDivider` clutter from list panes and detail
pages, give list panes a real tabular shape, and consolidate every detail
page's key hints into the status line instead of an in-body `Actions` block.

**Architecture:** Pure rendering changes inside `internal/tui`. No store, CLI,
or navigation-model changes. A new `sectionCaption` helper (short, scoped rule)
replaces `sectionDivider` (full-width banner) at detail-page sub-section
boundaries; list panes drop the banner outright and, where content is
naturally flat (Projects, Tasks-flat), grow a real column header + single
rule + paging footer. `tasksModel.statusHint()` gains overlay-awareness so the
status line stays correct while a comment/history overlay is open.

**Tech Stack:** Go, Bubble Tea (`github.com/charmbracelet/bubbletea`),
Lip Gloss (`github.com/charmbracelet/lipgloss`).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-05-tui-visual-polish-design.md` — every
  task below implements one section of it; do not deviate from the approved
  mockups without stopping to check with the user.
- No change to store API, CLI, mutation behavior, filtering/sorting
  semantics, or focus/navigation model (`docs/superpowers/specs/2026-07-04-tui-three-pane-workspace-design.md` stays in force).
- Do not touch `internal/tui/help.go` — help-overlay dividers are explicitly
  out of scope.
- Do not touch `internal/tui/form.go` line 259's hint text or the confirm
  overlay's hint at `app.go:721` — those are overlay-local, not detail-page
  Actions blocks, and are out of scope.
- `sectionDivider` (styles.go) must keep its exact current signature — it
  stays in use by `help.go`. Do not rename or delete it.
- Run `make verify` before the final commit (last step of Task 10).

---

## File Structure

- Modify `internal/tui/styles.go`: add `sectionCaption` helper.
- Modify `internal/tui/projects.go`: list caption/footer, detail sections,
  Project Summary caption.
- Modify `internal/tui/tasks.go`: list caption/table/footer (flat mode),
  banner removal (grouped mode), detail sections, `statusHint` overlay
  awareness.
- Modify `internal/tui/labels.go`: list banner removal, detail sections.
- Modify `internal/tui/comments.go`: overlay section captions, drop trailing
  hint lines.
- Modify `internal/tui/app_test.go`, `internal/tui/labels_test.go`,
  `internal/tui/comments_test.go`: update assertions for the above.

## Task 1: `sectionCaption` helper

**Files:**
- Modify: `internal/tui/styles.go`
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Produces: `sectionCaption(styles Styles, width int, title string) string` —
  returns two newline-joined lines: the bold/colored `title` and, on the next
  line, a rule of dashes exactly as wide as `title` (not the pane width). Every
  later task that touches a detail-page sub-section calls this.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/app_test.go` (anywhere among the other standalone helper
tests, e.g. right after `TestDashboardContentUsesPaneWidthWithoutCentering`):

```go
func TestSectionCaptionRuleScopedToTitleWidth(t *testing.T) {
	m := newTestModel(t)
	out := sectionCaption(m.styles, 40, "FACTS")
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("sectionCaption produced %d lines, want 2\n%q", len(lines), out)
	}
	if !strings.Contains(lines[0], "FACTS") {
		t.Fatalf("line 0 = %q, want it to contain FACTS", lines[0])
	}
	// The rule must be exactly as wide as the title ("FACTS" = 5 dashes), not
	// the full 40-column pane width.
	dashCount := strings.Count(lines[1], "─")
	if dashCount != len("FACTS") {
		t.Fatalf("rule has %d dashes, want %d\n%q", dashCount, len("FACTS"), lines[1])
	}
	if lipgloss.Width(lines[1]) >= 40 {
		t.Fatalf("rule line width = %d, want it short (title-scoped), not pane-width\n%q", lipgloss.Width(lines[1]), lines[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestSectionCaptionRuleScopedToTitleWidth -v`
Expected: FAIL with `undefined: sectionCaption`

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/styles.go`, add this function right after `sectionDivider`
(after line 309, before the `relTime` doc comment):

```go
// sectionCaption renders a detail-page sub-section header as a bold/colored
// label followed by a rule scoped to the label's own width (not the pane
// width). Unlike sectionDivider (a full-width banner used for list-pane and
// page-level headers), this is deliberately lightweight so multiple
// sub-sections on one detail page don't compete for visual weight.
func sectionCaption(styles Styles, width int, title string) string {
	label := dashboardLine(width, styles.HeaderLabel.Render(title))
	rule := dashboardLine(width, styles.HeaderLabel.Render(repeat("─", lipgloss.Width(title))))
	return label + "\n" + rule
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestSectionCaptionRuleScopedToTitleWidth -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/styles.go internal/tui/app_test.go
git commit -m "tui: add sectionCaption helper for detail sub-sections"
```

## Task 2: Projects list — drop Overview banner, add real paging footer

**Files:**
- Modify: `internal/tui/projects.go:381-423` (`renderListRows`)
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: existing `dashboardLine`, `dashboardContentWidth`, `repeat`,
  `truncateRunes`, `p.m.styles.RowCursor`/`GutterSelect`/`Muted`/`HeaderLabel`.
- Produces: no new exported behavior; `renderListRows` still returns a string
  sized to `maxRows` lines via the caller's `padToHeight`.

- [ ] **Step 1: Update the tests to the new expected output**

In `internal/tui/app_test.go`, edit `TestProjectsListPopulated` (around line
563): delete the `─ Overview ─` check and the divider/summary alignment block,
keep everything else, and add a footer check:

```go
func TestProjectsListPopulated(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 35)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "SCY", "Scylla")
	v := m.View()
	body := m.projects.View()
	if strings.HasPrefix(body, "Projects\n") {
		t.Fatalf("projects body repeats tab title\n--- body ---\n%s", body)
	}
	mustNotContain(t, body, "─ Overview ─")
	mustContain(t, body, "total projects: 2")
	mustContain(t, body, "selected: none")
	mustContain(t, body, "showing 1-2 of 2")
	for _, col := range []string{"CODE", "NAME"} {
		mustContain(t, v, col)
	}
	mustContain(t, v, "ATM")
	mustContain(t, v, "Acme Task Manager")
	mustContain(t, v, "SCY")
	// Select ATM via [s]; the gutter marker appears on the ATM row.
	// cursor starts at 0 (ATM, since code-asc sort).
	if m.projects.cursor != 0 {
		t.Errorf("cursor = %d want 0", m.projects.cursor)
	}
	update(t, m, "s")
	if m.projectScope != "ATM" {
		t.Errorf("after s: projectScope = %q want ATM", m.projectScope)
	}
	selected := m.View()
	mustContain(t, m.projects.View(), "selected: ATM")
	mustContain(t, selected, "▸")
}
```

Edit `TestProjectsListRendersSummaryRegionBelowList` (around line 603):

```go
func TestProjectsListRendersSummaryRegionBelowList(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	body := m.projects.View()
	mustNotContain(t, body, "─ Overview ─")
	mustContain(t, body, "total projects: 1")
	mustContain(t, body, "Project Summary")
	mustContain(t, body, "select a project to see summaries")
	captionIdx := strings.Index(body, "total projects: 1")
	summaryIdx := strings.Index(body, "Project Summary")
	if captionIdx < 0 || summaryIdx < 0 || summaryIdx <= captionIdx {
		t.Fatalf("summary should render below the list caption\n--- body ---\n%s", body)
	}
}
```

Edit `TestProjectsListOverflowSentinelRendersWithinHeight` (around line 632):
keep the whole test identical except the single overflow-text assertion,
which changes from:

```go
	mustContain(t, body, "more projects")
```

to:

```go
	mustContain(t, body, "showing ")
```

Edit `TestProjectSummaryRendersOnShortTerminalWithoutPanic` (around line 845):
remove the `mustContain(t, body, "Overview")` line (that word no longer
appears anywhere in the Projects list); keep `mustContain(t, body, "Project
Summary")` and the rest of the test unchanged.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestProjectsListPopulated|TestProjectsListRendersSummaryRegionBelowList|TestProjectsListOverflowSentinelRendersWithinHeight|TestProjectSummaryRendersOnShortTerminalWithoutPanic' -v`
Expected: FAIL — `mustContain` on `"showing 1-2 of 2"` / `"showing "` not
found yet; `mustNotContain` on `─ Overview ─` still finding it.

- [ ] **Step 3: Implement**

Replace `internal/tui/projects.go:381-423` (`renderListRows`) with:

```go
func (p *projectsModel) renderListRows(maxRows int) string {
	var b strings.Builder
	selected := p.m.projectScope
	if selected == "" {
		selected = "none"
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("total projects: %d   selected: %s", len(p.list), selected)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.HeaderLabel.Render(fmt.Sprintf("%-6s %-30s %6s %7s %10s", "CODE", "NAME", "TASKS", "LABELS", "UPDATED"))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, repeat("─", dashboardContentWidth(p.width))))

	availableRows := maxRows - 4 // caption + header + rule + footer
	if availableRows < 0 {
		availableRows = 0
	}
	end := len(p.list)
	if end > availableRows {
		end = availableRows
	}
	for i := 0; i < end; i++ {
		r := p.list[i]
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
	if end == 0 {
		fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.Muted.Render("showing 0-0 of 0")))
	} else {
		fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.Muted.Render(fmt.Sprintf("showing 1-%d of %d", end, len(p.list)))))
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestProjectsListPopulated|TestProjectsListRendersSummaryRegionBelowList|TestProjectsListOverflowSentinelRendersWithinHeight|TestProjectSummaryRendersOnShortTerminalWithoutPanic|TestProjectPaneSplitHeights|TestProjectsViewUsesThirtySeventySplit' -v`
Expected: PASS (the last two are included because they read line positions
into the same function's output — verify they still pass unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/projects.go internal/tui/app_test.go
git commit -m "tui: drop Overview banner from Projects list, add paging footer"
```

## Task 3: Projects detail — sectionCaption, drop Actions, plain Project Summary caption

**Files:**
- Modify: `internal/tui/projects.go:299-341` (`renderDetail`),
  `internal/tui/projects.go:425-426` (`renderSummary` first line)
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: `sectionCaption` from Task 1.
- Produces: no new exported behavior.

- [ ] **Step 1: Update the tests to the new expected output**

Edit `TestProjectDetailDashboardSections` (around line 677) in
`internal/tui/app_test.go`:

```go
func TestProjectDetailDashboardSections(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 50)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "enter")
	v := m.projects.View()
	mustContain(t, v, "Project ATM")
	mustContain(t, v, "FACTS")
	mustContain(t, v, "code")
	mustContain(t, v, "tasks")
	mustNotContain(t, v, "Actions")
	hint := m.projects.statusHint()
	mustContain(t, hint, "[N]name")
	mustContain(t, hint, "[H]history")
	mustContain(t, hint, "[x]remove")
}
```

Edit `TestProjectDetailHistoryToggle` (around line 1108): change the
mixed-case assertion to the new all-caps caption. The `mustNotContain` line
stays as-is (already uppercase); only this line changes:

```go
	mustContain(t, v, "History")
```

to:

```go
	mustContain(t, v, "HISTORY")
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestProjectDetailDashboardSections|TestProjectDetailHistoryToggle' -v`
Expected: FAIL — `FACTS`/`HISTORY` not found (still `─ Facts ─`/`History` in
the render), `mustNotContain(v, "Actions")` fails (still present).

- [ ] **Step 3: Implement**

Replace `internal/tui/projects.go:299-341` (`renderDetail`) with:

```go
// renderDetail (re)builds the scrollable lines for the project detail view.
func (p *projectsModel) renderDetail() {
	var b strings.Builder
	pr := p.detail.project
	if pr == nil {
		return
	}
	fmt.Fprintf(&b, "Project %s\n", pr.Code)
	b.WriteString(sepLine("─", 78, p.width, 2))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", p.m.styles.Muted.Render(pr.Name))
	b.WriteString("\n")
	b.WriteString(sectionCaption(p.m.styles, p.width, "FACTS"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("code      %s", pr.Code)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("name      %s", pr.Name)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("tasks     %d", len(listTaskIDs(p.m.store, pr.Code)))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("labels    %d", len(p.m.store.LabelList(pr.Code, "")))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("created   %s   by %s", store.RFC3339UTC(pr.CreatedAt), pr.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("updated   %s   by %s", store.RFC3339UTC(pr.UpdatedAt), pr.UpdatedBy)))

	if p.detail.historyOn {
		b.WriteString("\n")
		b.WriteString(sectionCaption(p.m.styles, p.width, "HISTORY"))
		b.WriteString("\n")
		hv := p.m.store.History(p.detail.code, store.Subject{Kind: "project", Code: p.detail.code})
		if len(hv) == 0 {
			b.WriteString(dashboardLine(p.width, " (no history)"))
			b.WriteString("\n")
		} else {
			for _, e := range hv {
				fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("[%d] %s %s %s", e.Seq, store.RFC3339UTC(e.At), e.Actor, e.Action)))
			}
		}
	}

	p.detail.lines = strings.Split(b.String(), "\n")
	p.clampDetail()
}
```

(This deletes the `Actions` `sectionDivider` + hint line entirely, and swaps
`sectionDivider(..., "Facts")` / `sectionDivider(..., "History")` for
`sectionCaption(..., "FACTS")` / `sectionCaption(..., "HISTORY")`.)

In `renderSummary` (line 425-426), change:

```go
	lines := []string{sectionDivider(p.m.styles, p.width, "Project Summary")}
```

to:

```go
	lines := []string{dashboardLine(p.width, p.m.styles.HeaderLabel.Render("Project Summary"))}
```

This keeps the exact same line count (1 line) so none of the height-budget
branches later in `renderSummary` (`remaining == 1/2/3`, `remaining >= 9`,
etc.) shift — only the banner's dash-fill is removed, replaced by a plain
bold caption.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestProjectDetailDashboardSections|TestProjectDetailHistoryToggle|TestProjectDetailDoesNotRenderSummaryCharts|TestSelectedProjectSummaryRendersActivityInCompactPane|TestProjectSummaryTinyHeightStillRendersActivity|TestProjectSummaryClearsWhenSelectedProjectRemoved|TestProjectSummaryRendersOnShortTerminalWithoutPanic|TestProjectsViewUsesThirtySeventySplit' -v`
Expected: PASS (the activity/height tests are included to confirm the
line-count-preserving change to `renderSummary` didn't shift any chart
budgets).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/projects.go internal/tui/app_test.go
git commit -m "tui: sectionCaption for project detail, drop Actions block"
```

## Task 4: Tasks list (flat mode) — real single-line table + footer

**Files:**
- Modify: `internal/tui/tasks.go:102-115` (`SetSize`),
  `internal/tui/tasks.go:761-829` (`renderList`, `renderFlatList`)
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: `taskRow{id, title, labels, updated}` (unchanged, defined at
  `tasks.go:69-75`).
- Produces: new helper `func (t *tasksModel) taskColumnWidths() (idW, labelsW, updatedW, titleW int)`,
  used only within `tasks.go`.

- [ ] **Step 1: Update the tests to the new expected output**

Edit `TestTasksFlatListEmptyFilter` (around line 1138) in
`internal/tui/app_test.go`:

```go
func TestTasksFlatListEmptyFilter(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "task one", "ATM:status:open")
	update(t, m, "s") // select ATM
	update(t, m, "2") // focus Tasks pane
	v := m.View()
	body := m.tasks.View()
	if strings.HasPrefix(body, "Tasks\n") {
		t.Fatalf("tasks body repeats tab title\n--- body ---\n%s", body)
	}
	mustNotContain(t, body, "─ Overview ─")
	mustContain(t, body, "ID")
	mustContain(t, body, "TITLE")
	mustContain(t, body, "LABELS")
	mustContain(t, body, "UPDATED")
	mustContain(t, v, "PROJECT: ATM")
	mustContain(t, v, "FILTER: (none)")
	mustContain(t, v, "SORT: updated-desc")
	mustContain(t, v, "task one")
	mustContain(t, v, "ATM-0001")
	mustContain(t, v, "ATM:status:open")
	mustContain(t, v, "showing 1-1 of 1")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestTasksFlatListEmptyFilter -v`
Expected: FAIL — `─ Overview ─` still present, `ID`/`TITLE` column headers
missing, `"showing 1-1 of 1"` missing.

- [ ] **Step 3: Implement**

Replace `internal/tui/tasks.go:102-115` (`SetSize`) with:

```go
func (t *tasksModel) SetSize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	t.width = w
	t.contentHeight = h
	t.pageSize = h - 6 // header line + blank + column header + rule + footer + margin
	if t.pageSize < 1 {
		t.pageSize = 1
	}
}
```

Replace `internal/tui/tasks.go:761-829` (`renderList` through the end of
`renderFlatList`) with:

```go
func (t *tasksModel) renderList() string {
	var b strings.Builder
	b.WriteString(dashboardLine(t.width, t.m.styles.HeaderLine.Render(t.headerLine())))
	b.WriteString("\n")
	b.WriteString("\n")

	if t.m.projectScope == "" {
		t.renderEmptyState(&b, []string{
			t.m.styles.EmptyHead.Render("no project selected"),
			"",
			t.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects pane to scope this view", t.m.styles.EmptyKey.Render("[s]"))),
		})
		return padToHeight(b.String(), t.contentHeight)
	}

	if t.hasWildcard() {
		t.renderGroupedList(&b)
	} else {
		t.renderFlatList(&b)
	}
	return padToHeight(b.String(), t.contentHeight)
}

// renderEmptyState appends a vertically+horizontally centered empty-state
// block (each line center-aligned independently) into b. The block is
// centered within contentHeight-1 to account for the header line already
// written by the caller.
func (t *tasksModel) renderEmptyState(b *strings.Builder, lines []string) {
	b.WriteString(centerLinesBoth(lines, t.width, t.contentHeight-1))
}

// taskColumnWidths returns fixed widths for ID/LABELS/UPDATED and a flexible
// TITLE width that absorbs the remaining pane width. The format string used
// by both the header and data rows is " %-*s %-*s %-*s %*s" (leading space +
// 3 inter-column spaces = 4 extra columns of padding).
func (t *tasksModel) taskColumnWidths() (idW, labelsW, updatedW, titleW int) {
	idW, labelsW, updatedW = 9, 24, 9
	titleW = t.width - idW - labelsW - updatedW - 4
	if titleW < 16 {
		titleW = 16
	}
	return
}

func (t *tasksModel) renderFlatList(b *strings.Builder) {
	if len(t.rows) == 0 {
		filter := strings.Join(t.parseFilter(), " and ")
		t.renderEmptyState(b, []string{
			t.m.styles.EmptyHead.Render("no tasks match this filter"),
			"",
			t.m.styles.EmptyDim.Render(fmt.Sprintf("no task carries %s", filter)),
			"",
			t.m.styles.EmptyText.Render(fmt.Sprintf("%s to edit filter, or clear it to see all tasks", t.m.styles.EmptyKey.Render("[/]"))),
		})
		return
	}
	idW, labelsW, updatedW, titleW := t.taskColumnWidths()
	header := fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, "ID", titleW, "TITLE", labelsW, "LABELS", updatedW, "UPDATED")
	b.WriteString(dashboardLine(t.width, t.m.styles.HeaderLabel.Render(header)))
	b.WriteString("\n")
	b.WriteString(dashboardLine(t.width, repeat("─", dashboardContentWidth(t.width))))
	b.WriteString("\n")

	start, end := t.pageWindow(len(t.rows))
	for i := start; i < end; i++ {
		r := t.rows[i]
		labels := "-"
		if len(r.labels) > 0 {
			labels = strings.Join(r.labels, " ")
		}
		line := fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, r.id, titleW, truncateRunes(r.title, titleW), labelsW, truncateRunes(labels, labelsW), updatedW, r.updated)
		if i == t.cursor {
			line = " " + t.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
		}
		b.WriteString(dashboardLine(t.width, line))
		b.WriteString("\n")
	}
	b.WriteString(dashboardLine(t.width, fmt.Sprintf(" showing %d-%d of %d", start+1, end, len(t.rows))))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestTasksFlatListEmptyFilter|TestTasksFilterInlineEditing|TestTasksFlatListExactFilterRestricts|TestTasksPagingFooter' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tasks.go internal/tui/app_test.go
git commit -m "tui: render Tasks flat list as a real single-line table"
```

## Task 5: Tasks list (grouped mode) — drop Groups banner

**Files:**
- Modify: `internal/tui/tasks.go` (`renderGroupedList`, right after Task 4's
  edits — the function starts at what is now a few lines earlier than the
  original `831`; locate it by its `func (t *tasksModel) renderGroupedList`
  signature)
- Test: `internal/tui/app_test.go`

**Interfaces:** none new.

- [ ] **Step 1: Confirm today's banner text, before touching anything**

Run: `go test ./internal/tui/ -run TestTasksGroupedSingleWildcard -v`
Expected: PASS (the `mustContain(t, v, "Groups")` assertion matches today's
`sectionDivider(..., "Groups")` banner text).

- [ ] **Step 2: Remove the now-obsolete assertion**

In `internal/tui/app_test.go`, edit `TestTasksGroupedSingleWildcard` (around
line 1225): remove this line —

```go
	mustContain(t, v, "Groups")
```

— and keep every other assertion in that test unchanged.

- [ ] **Step 3: Implement**

In `renderGroupedList`, delete these two lines (the very first two statements
in the function body):

```go
	b.WriteString(sectionDivider(t.m.styles, t.width, "Groups"))
	b.WriteString("\n")
```

Leave everything else in the function (the `len(t.groups) == 0` empty-state
branch, the group-rendering loop, the `(no matching labels)` bucket) exactly
as-is.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestTasksGroupedSingleWildcard|TestTasksGroupedNestedWildcards|TestTasksGroupedNoMatchingLabelsBucket' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tasks.go internal/tui/app_test.go
git commit -m "tui: drop Groups banner from Tasks grouped list"
```

## Task 6: Tasks detail — sectionCaption, drop Actions

**Files:**
- Modify: `internal/tui/tasks.go:642-716` (`renderDetail`)
- Test: `internal/tui/app_test.go`, `internal/tui/comments_test.go`

**Interfaces:**
- Consumes: `sectionCaption` from Task 1.

- [ ] **Step 1: Update the tests to the new expected output**

Edit `TestTaskDetailFactsLabelsHistory` in `internal/tui/app_test.go` (around
line 1314). Replace the block from the `v := m.tasks.View()` after `enter`
through the `[H]` open (lines ~1330-1345) with:

```go
	v := m.tasks.View()
	mustContain(t, v, "Task ATM-0001")
	mustContain(t, v, "FACTS")
	mustContain(t, v, "id      ATM-0001")
	mustContain(t, v, "project ATM")
	mustContain(t, v, "title   Fix label reconciliation")
	mustContain(t, v, "LABELS")
	mustContain(t, v, "ATM:status:in-progress")
	mustContain(t, v, "ATM:type:bug")
	mustContain(t, v, "ATM:priority:high")
	mustNotContain(t, v, "Actions")
	hint := m.tasks.statusHint()
	mustContain(t, hint, "[e]title")
	mustContain(t, hint, "[b]add label")
	if strings.Contains(v, "task.created") {
		t.Fatalf("history must be hidden behind [H] overlay by default, found task.created:\n%s", v)
	}
```

Further down in the same test, the closing check (around line 1374-1377)
changes from:

```go
	v = t.m.tasks.View()
	mustContain(t, v, "Task ATM-0001")
	mustContain(t, v, "─ Facts ─")
}
```

to:

```go
	v = m.tasks.View()
	mustContain(t, v, "Task ATM-0001")
	mustContain(t, v, "FACTS")
}
```

Edit `TestTaskDetailRendersCommentsSection` in
`internal/tui/comments_test.go` (around line 10): replace the two hint checks
that read from the pane body with checks against `m.tasks.statusHint()`:

```go
func TestTaskDetailRendersCommentsSection(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "Agent Tasks Management", "claude")
	tk, _ := m.store.CreateTask("ATM", "Fix thing", "work on it", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "first comment body", []string{"ATM:comment:open-question"}, "", "agent")
	_, _ = m.store.CreateComment(tk.ID, "second reply", nil, "ATM-0001-c0001", "ttran")

	m.projectScope = "ATM"
	m.SetSize(240, 70)
	m.tasks.openDetail(tk.ID)
	view := m.tasks.View()
	if !strings.Contains(view, "Comments") {
		t.Fatalf("missing Comments section:\n%s", view)
	}
	if !strings.Contains(view, "agent") {
		t.Fatalf("missing first comment actor:\n%s", view)
	}
	if !strings.Contains(view, "ttran") {
		t.Fatalf("missing second comment actor:\n%s", view)
	}
	if !strings.Contains(view, "first comment body") {
		t.Fatalf("missing first comment body:\n%s", view)
	}
	if !strings.Contains(view, "second reply") {
		t.Fatalf("missing second comment body:\n%s", view)
	}
	hint := m.tasks.statusHint()
	if !strings.Contains(hint, "[M]comment") {
		t.Fatalf("missing [M] hint: %s", hint)
	}
	if !strings.Contains(hint, "[H]history") {
		t.Fatalf("missing [H] hint: %s", hint)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestTaskDetailFactsLabelsHistory|TestTaskDetailRendersCommentsSection' -v`
Expected: FAIL — `FACTS`/`LABELS` not found (still `─ Facts ─`/`─ Labels ─`),
`mustNotContain(v, "Actions")` fails.

- [ ] **Step 3: Implement**

Replace `internal/tui/tasks.go:642-716` (`renderDetail`) with:

```go
func (t *tasksModel) renderDetail() {
	var b strings.Builder
	tk := t.detail.task
	if tk == nil {
		return
	}
	fmt.Fprintf(&b, "Task %s\n", tk.ID)
	b.WriteString(sepLine("─", 78, t.width, 2))
	b.WriteString("\n")
	b.WriteString(t.m.styles.Muted.Render(tk.Title))
	b.WriteString("\n\n")
	b.WriteString(sectionCaption(t.m.styles, t.width, "FACTS"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("id      %s", tk.ID)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("project %s", tk.ProjectCode)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("title   %s", tk.Title)))
	if tk.Description == "" {
		b.WriteString(dashboardLine(t.width, "description (none)"))
		b.WriteString("\n")
	} else {
		for i, line := range strings.Split(tk.Description, "\n") {
			if i == 0 {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("description %s", line)))
			} else {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("            %s", line)))
			}
		}
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("created %s   by %s", store.RFC3339UTC(tk.CreatedAt), tk.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("updated %s   by %s", store.RFC3339UTC(tk.UpdatedAt), tk.UpdatedBy)))
	b.WriteString("\n")

	b.WriteString(sectionCaption(t.m.styles, t.width, "LABELS"))
	b.WriteString("\n")
	if len(tk.Labels) == 0 {
		b.WriteString(dashboardLine(t.width, " (no labels)"))
		b.WriteString("\n")
	} else {
		chips := renderLabelChips(t.m.styles, tk.Labels, t.width-2)
		b.WriteString(dashboardLine(t.width, " "+chips))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	b.WriteString(sectionCaption(t.m.styles, t.width, "COMMENTS"))
	b.WriteString("\n")
	cs, _ := t.m.store.ListComments(tk.ID)
	if len(cs) == 0 {
		b.WriteString(dashboardLine(t.width, " (no comments)"))
		b.WriteString("\n")
	} else {
		for _, c := range cs {
			labels := "(no labels)"
			if len(c.Labels) > 0 {
				labels = strings.Join(c.Labels, " ")
			}
			fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf(" %s   %s   %s", c.CreatedBy, relTime(c.CreatedAt, store.Now()), truncateRunes(labels, 36))))
			bodyLines := strings.Split(c.Body, "\n")
			maxLines := 6
			for i := 0; i < len(bodyLines) && i < maxLines; i++ {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("     %s", bodyLines[i])))
			}
			if len(bodyLines) > maxLines {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, "     …"))
			}
		}
	}
	t.detail.lines = strings.Split(b.String(), "\n")
	t.clampDetail()
}
```

(This deletes the `Actions` `sectionDivider` + hint line entirely, and swaps
`sectionDivider(..., "Facts"/"Labels"/"Comments")` for
`sectionCaption(..., "FACTS"/"LABELS"/"COMMENTS")`. Note the trailing blank
line after the Comments section that used to precede Actions is also
removed since Actions is gone — the function now ends right after the last
comment content.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestTaskDetailFactsLabelsHistory|TestTaskDetailRendersCommentsSection|TestTaskDetailLabelsRenderAsChips|TestTaskDetailHidesHistoryInline' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tasks.go internal/tui/app_test.go internal/tui/comments_test.go
git commit -m "tui: sectionCaption for task detail, drop Actions block"
```

## Task 7: Labels list — drop Overview + Namespaces banners

**Files:**
- Modify: `internal/tui/labels.go:260-337` (`renderList`)
- Test: `internal/tui/labels_test.go`

**Interfaces:** none new.

- [ ] **Step 1: Update the test to the new expected output**

Edit `TestLabelsTabListSeededLabels` in `internal/tui/labels_test.go` (around
line 24):

```go
func TestLabelsTabListSeededLabels(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 80)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // select ATM
	update(t, m, "3") // Labels pane
	v := m.labels.View()
	body := m.labels.View()
	if strings.HasPrefix(body, "Labels\n") {
		t.Fatalf("labels body repeats tab title\n--- body ---\n%s", body)
	}
	mustNotContain(t, body, "─ Overview ─")
	mustContain(t, v, "total labels: 18")
	mustNotContain(t, v, "─ Namespaces ─")
	// Namespace headings for seeded namespaces.
	mustContain(t, v, "context:")
	mustContain(t, v, "status:")
	mustContain(t, v, "type:")
	mustContain(t, v, "priority:")
	// A seeded label's description is rendered.
	mustContain(t, v, "workflow state: open")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestLabelsTabListSeededLabels -v`
Expected: FAIL — `mustNotContain` finds `─ Overview ─` and `─ Namespaces ─`
still present.

- [ ] **Step 3: Implement**

In `internal/tui/labels.go`, inside `renderList` (starting at line 260),
delete these four lines (the `sectionDivider` calls and their trailing
`"\n"` writes for `"Overview"` and `"Namespaces"`):

```go
	b.WriteString(sectionDivider(l.m.styles, l.width, "Overview"))
	b.WriteString("\n")
```

and

```go
	b.WriteString(sectionDivider(l.m.styles, l.width, "Namespaces"))
	b.WriteString("\n")
```

Leave the caption line (`fmt.Fprintf(&b, "%s\n", dashboardLine(l.width,
fmt.Sprintf("project: %s   total labels: %d", ...)))`), the blank line after
it, the namespace-grouping loop, and the `tags:` bucket exactly as they are.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestLabelsTabListSeededLabels|TestLabelsTabCallsOutMissingDescriptions|TestLabelsTabEmptyStateNoProject' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go
git commit -m "tui: drop Overview/Namespaces banners from Labels list"
```

## Task 8: Labels detail — sectionCaption, drop Actions

**Files:**
- Modify: `internal/tui/labels.go:339-359` (`renderDetail`)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: `sectionCaption` from Task 1.

- [ ] **Step 1: Update the test to the new expected output**

Edit `TestLabelDetailDashboardSections` in `internal/tui/labels_test.go`
(around line 59):

```go
func TestLabelDetailDashboardSections(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	update(t, m, "enter")
	v := m.View()
	mustContain(t, v, "Label ")
	mustContain(t, v, "FACTS")
	mustContain(t, v, "usage")
	mustContain(t, v, "description")
	mustNotContain(t, v, "Actions")
	hint := m.labels.statusHint()
	if hint != "[d]esc [l]remove [Esc]back" {
		t.Fatalf("labels detail statusHint = %q want [d]esc [l]remove [Esc]back", hint)
	}
	mustContain(t, v, "[d]esc")
	mustContain(t, v, "[l]remove")
	mustContain(t, v, "[Esc]back")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestLabelDetailDashboardSections -v`
Expected: FAIL — `FACTS` not found (still `─ Facts ─`), `mustNotContain(v,
"Actions")` fails (the Actions divider text `─ Actions ─` contains
"Actions").

- [ ] **Step 3: Implement**

Replace `internal/tui/labels.go:339-359` (`renderDetail`) with:

```go
func (l *labelsModel) renderDetail() string {
	r := l.detail.row
	var b strings.Builder
	fmt.Fprintf(&b, "Label %s\n", r.full)
	b.WriteString(sepLine("─", 78, l.width, 2))
	b.WriteString("\n")
	b.WriteString(sectionCaption(l.m.styles, l.width, "FACTS"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("name        %s", r.full)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("usage       %d %s", r.usage, pluralTasks(r.usage))))
	desc := r.description
	if desc == "" {
		desc = l.m.styles.Warning.Render("needs description")
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("description %s", desc)))
	return padToHeight(b.String(), l.contentHeight)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestLabelDetailDashboardSections -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go
git commit -m "tui: sectionCaption for label detail, drop Actions block"
```

## Task 9: Comment/history overlays — sectionCaption, drop trailing hint lines

**Files:**
- Modify: `internal/tui/comments.go:32-74` (`commentOverlayModel.render`),
  `internal/tui/comments.go:143-160` (`historyOverlayModel.render`)
- Test: `internal/tui/comments_test.go`

**Interfaces:**
- Consumes: `sectionCaption` from Task 1.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/comments_test.go`:

```go
func TestCommentOverlayHasNoTrailingHintLine(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "the body text", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("enter"))
	view := m.tasks.commentOverlay.view(m)
	mustContain(t, view, "BODY")
	mustNotContain(t, view, "[Esc] back")
	mustNotContain(t, view, "[H] history")
}

func TestHistoryOverlayHasNoTrailingHintLine(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	m.projectScope = "ATM"
	m.SetSize(120, 70)
	m.tasks.openDetail(tk.ID)
	m.tasks.handleDetailKey(keyMsg("H"))
	view := m.tasks.historyOverlay.view(m)
	mustContain(t, view, "task.created")
	mustNotContain(t, view, "[Esc] back")
}
```

(`mustContain`/`mustNotContain` are defined in `app_test.go`, same package —
no import needed.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCommentOverlayHasNoTrailingHintLine|TestHistoryOverlayHasNoTrailingHintLine' -v`
Expected: FAIL — `mustContain(view, "BODY")` fails (still `─ Body ─`), and the
`mustNotContain` hint checks fail (trailing hint lines still present).

- [ ] **Step 3: Implement**

Replace `internal/tui/comments.go:32-74` (`commentOverlayModel.render`) with:

```go
func (co *commentOverlayModel) render(m *Model) {
	var b strings.Builder
	c := co.comment
	if c == nil {
		return
	}
	fmt.Fprintf(&b, "Comment %s\n", c.ID)
	b.WriteString(sepLine("─", 78, m.tasks.width, 2))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("id       %s", c.ID)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("task     %s", c.TaskID)))
	if c.ReplyTo != "" {
		fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("reply-to %s", c.ReplyTo)))
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("actor    %s", c.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("created  %s", store.RFC3339UTC(c.CreatedAt))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("updated  %s by %s", store.RFC3339UTC(c.UpdatedAt), c.UpdatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("labels   %s", formatLabelsTUI(c.Labels))))
	b.WriteString("\n")
	b.WriteString(sectionCaption(m.styles, m.tasks.width, "BODY"))
	b.WriteString("\n")
	for _, line := range strings.Split(c.Body, "\n") {
		fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, line))
	}
	if co.historyOpen {
		b.WriteString("\n")
		b.WriteString(sectionCaption(m.styles, m.tasks.width, "HISTORY"))
		b.WriteString("\n")
		code, _, _, _ := store.ParseCommentID(c.ID)
		hv := m.store.History(code, store.Subject{Kind: "comment", ID: c.ID})
		if len(hv) == 0 {
			b.WriteString(dashboardLine(m.tasks.width, " (no history)"))
			b.WriteString("\n")
		} else {
			for _, e := range hv {
				fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("[%d] %s %s %s", e.Seq, store.RFC3339UTC(e.At), e.Actor, e.Action)))
			}
		}
	}
	co.lines = strings.Split(b.String(), "\n")
}
```

Replace `internal/tui/comments.go:143-160` (`historyOverlayModel.render`)
with:

```go
func (ho *historyOverlayModel) render(m *Model, code, taskID string) {
	var b strings.Builder
	fmt.Fprintf(&b, "History  %s\n", taskID)
	b.WriteString(sepLine("─", 78, m.tasks.width, 2))
	b.WriteString("\n")
	hv := m.store.History(code, store.Subject{Kind: "task", ID: taskID})
	if len(hv) == 0 {
		b.WriteString(dashboardLine(m.tasks.width, " (no history)"))
		b.WriteString("\n")
	} else {
		for _, e := range hv {
			fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("[%d] %s %s %s", e.Seq, store.RFC3339UTC(e.At), e.Actor, e.Action)))
		}
	}
	ho.lines = strings.Split(b.String(), "\n")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestCommentOverlayHasNoTrailingHintLine|TestHistoryOverlayHasNoTrailingHintLine|TestCommentOverlayShowsIDAndBody|TestCommentOverlayIsReadOnly|TestTaskDetailHKeyOpensHistoryOverlay' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/comments.go internal/tui/comments_test.go
git commit -m "tui: sectionCaption for overlays, drop trailing hint lines"
```

## Task 10: `tasksModel.statusHint()` overlay-awareness

**Files:**
- Modify: `internal/tui/tasks.go:986-998` (`statusHint`)
- Test: `internal/tui/comments_test.go`

**Interfaces:**
- Consumes: `t.commentOverlay.id` (string, empty when closed),
  `t.historyOverlay.active` (bool) — both already fields on `tasksModel` per
  `tasks.go:38-39`.
- Produces: `statusHint()` now returns `"[H]istory   [Esc]back"` while the
  comment overlay is open and `"[Esc]back"` while the history overlay is
  open, before falling through to its existing branches.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/comments_test.go`:

```go
func TestStatusHintReflectsOverlayState(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "body", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)

	base := m.tasks.statusHint()
	if !strings.Contains(base, "[e]title") {
		t.Fatalf("base detail hint = %q, want task-detail hint", base)
	}

	m.tasks.handleDetailKey(keyMsg("enter")) // open comment overlay
	if m.tasks.commentOverlay.id == "" {
		t.Fatal("expected comment overlay open")
	}
	if got := m.tasks.statusHint(); got != "[H]istory   [Esc]back" {
		t.Errorf("statusHint with comment overlay open = %q want [H]istory   [Esc]back", got)
	}
	m.tasks.handleCommentOverlayKey(keyMsg("esc"))

	m.tasks.handleDetailKey(keyMsg("H")) // open history overlay
	if !m.tasks.historyOverlay.active {
		t.Fatal("expected history overlay active")
	}
	if got := m.tasks.statusHint(); got != "[Esc]back" {
		t.Errorf("statusHint with history overlay open = %q want [Esc]back", got)
	}
	m.tasks.handleHistoryOverlayKey(keyMsg("esc"))

	if got := m.tasks.statusHint(); got != base {
		t.Errorf("statusHint after closing overlays = %q want %q", got, base)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestStatusHintReflectsOverlayState -v`
Expected: FAIL — `statusHint with comment overlay open` returns the
task-detail hint instead of `"[H]istory   [Esc]back"`.

- [ ] **Step 3: Implement**

Replace `internal/tui/tasks.go:986-998` (`statusHint`) with:

```go
func (t *tasksModel) statusHint() string {
	if t.commentOverlay.id != "" {
		return "[H]istory   [Esc]back"
	}
	if t.historyOverlay.active {
		return "[Esc]back"
	}
	if t.m.projectScope == "" {
		return "[?]keys"
	}
	if t.view == tViewDetail {
		return "[e]title [d]desc [b]add label [B]remove label [M]comment [H]history [x]remove [Esc]back"
	}
	hint := "[/]filter [s]sort [a]dd [Enter]detail [?]keys"
	if t.filterEditing {
		hint = "[Enter]apply [Esc]cancel"
	}
	return hint
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestStatusHintReflectsOverlayState -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tasks.go internal/tui/comments_test.go
git commit -m "tui: statusHint reflects comment/history overlay state"
```

## Task 11: Full-suite verification

**Files:** none (verification only).

- [ ] **Step 1: Run the full TUI test package**

Run: `go test ./internal/tui/... -v`
Expected: PASS, all tests green. If anything unrelated to the tasks above
fails, investigate before proceeding — do not silence or skip a failing
test.

- [ ] **Step 2: Confirm no stray `sectionDivider` calls remain outside help.go**

Run: `grep -rn "sectionDivider" internal/tui/*.go`
Expected output: only matches in `internal/tui/styles.go` (the function
definition) and `internal/tui/help.go` (unchanged, out of scope). If any
match appears in `tasks.go`, `projects.go`, `labels.go`, or `comments.go`,
one of the earlier tasks was missed — go back and fix it.

- [ ] **Step 3: Run the project's full verification suite**

Run: `make verify`
Expected: PASS (build, vet, full test suite per this repo's Makefile target).

- [ ] **Step 4: Commit** (only if `make verify` required any fix-up changes;
skip if steps 1-3 were all clean)

```bash
git add -A
git commit -m "tui: fix up visual-polish verification findings"
```
