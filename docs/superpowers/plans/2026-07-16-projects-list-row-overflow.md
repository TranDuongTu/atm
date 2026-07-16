# Projects List Row Overflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the TUI Projects pane data rows from overflowing the pane width and clipping the rightmost UPDATED column (`3m ago` → `3m ag`).

**Architecture:** One arithmetic change in `projectColumnWidths` (`internal/tui/projects.go`): size the flexible NAME column so the full data row — including the 2-char `gutter + " "` prefix — fits `p.width`. NAME absorbs shrinkage (floor lowered 20 → 8); UPDATED stays fixed at 10 and is never the column that gets clipped. No change to column order, fixed widths, or the header.

**Tech Stack:** Go 1.22+; `internal/tui` (Bubble Tea); `github.com/charmbracelet/lipgloss` for display-width math.

## Global Constraints

- Go 1.22+; verify with `make verify` (or `make build && make test`).
- No emojis in code or commits.
- Follow existing style in neighboring files (`internal/tui/projects.go`, `internal/tui/fixedslot_test.go`).
- Stable API surface; the TUI is a thin client over `internal/store`.
- The fixed column widths stay exactly: `codeW=6, tasksW=6, labelsW=7, updatedW=10`.
- Column order stays `CODE NAME TASKS LABELS UPDATED`.
- The header format string is unchanged.

---

## Task 1: Fix projectColumnWidths to account for the 2-char gutter prefix

**Files:**
- Modify: `internal/tui/projects.go:399-408` (`projectColumnWidths`)
- Test: `internal/tui/fixedslot_test.go` (append new tests, mirroring `TestTaskColumnWidthsSizesIdToLongestID`)

**Interfaces:**
- Consumes: `p.width` (the projects pane content width, set by `SetSize`).
- Produces: `projectColumnWidths() (codeW, tasksW, labelsW, updatedW, nameW int)` — same signature, same fixed widths, but `nameW` now sized so the full data row (with the 2-char `gutter + " "` prefix) fits `p.width`; NAME floor lowered from 20 to 8.

### Context for the implementer

`renderListRows` (`internal/tui/projects.go:422`) builds each data row as:

```go
line := fmt.Sprintf(" %-*s %-*s %*d %*d %*s", codeW, r.code, nameW, truncateRunes(r.name, nameW), tasksW, r.tasks, labelsW, r.labels, updatedW, r.updated)
if i == p.cursor {
    line = gutter + " " + p.m.styles.RowCursor.Render(line)
} else {
    line = gutter + " " + line
}
fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, line))
```

The format string contributes 1 leading space + 4 inter-column spaces = 5 chars of overhead. The `gutter + " "` prefix contributes 2 more. So the full data row width is `codeW + nameW + tasksW + labelsW + updatedW + 7`. For the row to fit `p.width`:

```
nameW = p.width - codeW - tasksW - labelsW - updatedW - 7
```

The current code subtracts only 5 (it ignores the 2-char gutter prefix), so each data row is `p.width + 2` wide and `dashboardLine`→`fitLine` truncates from the right, clipping UPDATED.

The header row uses the same `nameW` but has no gutter prefix, so its width is `fixed + nameW + 5 = p.width - 2`. It already fits and is right-padded by the pane box; columns stay aligned with the data rows because both share the same fixed widths and the same 1-char format leading space.

The tasks pane had the identical bug and was fixed the same way — see `TestTaskColumnWidthsSizesIdToLongestID` in `internal/tui/fixedslot_test.go:54`. Mirror that test.

- [ ] **Step 1: Write the failing width test**

Append to `internal/tui/fixedslot_test.go`:

```go
// TestProjectColumnWidthsFitPaneWithGutterPrefix verifies the data row —
// including the 2-char "gutter + space" prefix renderListRows prepends —
// fits p.width so the rightmost UPDATED column is never clipped ("3m ago"
// must not become "3m ag"). NAME is the flexible column and absorbs the
// gutter overhead; UPDATED stays fixed at 10.
func TestProjectColumnWidthsFitPaneWithGutterPrefix(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 60
	codeW, tasksW, labelsW, updatedW, nameW := p.projectColumnWidths()
	if codeW != 6 || tasksW != 6 || labelsW != 7 || updatedW != 10 {
		t.Errorf("fixed widths = %d/%d/%d/%d, want 6/6/7/10", codeW, tasksW, labelsW, updatedW)
	}
	// Full data row = fixed + nameW + 5 (format overhead) + 2 (gutter+space).
	rowW := codeW + tasksW + labelsW + updatedW + nameW + 5 + 2
	if rowW > p.width {
		t.Errorf("data row width = %d, exceeds pane width %d (UPDATED would clip)", rowW, p.width)
	}
}

// TestProjectColumnWidthsNameAbsorbsShrinkage verifies NAME is the flexible
// column: at a wider pane it grows, and it never forces the row to overflow.
func TestProjectColumnWidthsNameAbsorbsShrinkage(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 100
	_, _, _, _, nameW100 := p.projectColumnWidths()
	p.width = 50
	_, _, _, _, nameW50 := p.projectColumnWidths()
	if nameW100 <= nameW50 {
		t.Errorf("nameW at width 100 = %d, not greater than nameW at width 50 = %d", nameW100, nameW50)
	}
}

// TestProjectColumnWidthsNameFloorIsEight verifies the NAME floor is 8 (lowered
// from 20) so NAME keeps absorbing shrinkage at narrow panes instead of
// forcing the row to overflow and clip UPDATED.
func TestProjectColumnWidthsNameFloorIsEight(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 30 // below the floor: nameW would go negative without the clamp
	_, _, _, _, nameW := p.projectColumnWidths()
	if nameW != 8 {
		t.Errorf("nameW floor = %d, want 8", nameW)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestProjectColumnWidths' -v`
Expected: FAIL — `TestProjectColumnWidthsFitPaneWithGutterPrefix` fails because the current `nameW = p.width - 29 - 5` makes the data row `p.width + 2` wide; `TestProjectColumnWidthsNameFloorIsEight` fails because the current floor is 20, not 8. (`TestProjectColumnWidthsNameAbsorbsShrinkage` may already pass — that is fine.)

- [ ] **Step 3: Write the failing row-render test**

Append to `internal/tui/fixedslot_test.go`:

```go
// TestProjectListDataRowRendersFullUpdatedColumn verifies a rendered data row
// keeps the full UPDATED value ("3m ago", not "3m ag") at a realistic pane
// width. The row is formatted exactly as renderListRows does, including the
// "gutter + space" prefix, and must fit the pane width.
func TestProjectListDataRowRendersFullUpdatedColumn(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 60
	p.list = []projRow{
		{code: "ATM", name: "Acme Task Manager", tasks: 3, labels: 5, updated: "3m ago"},
	}
	codeW, tasksW, labelsW, updatedW, nameW := p.projectColumnWidths()
	r := p.list[0]
	gutter := " "
	line := fmt.Sprintf(" %-*s %-*s %*d %*d %*s", codeW, r.code, nameW, truncateRunes(r.name, nameW), tasksW, r.tasks, labelsW, r.labels, updatedW, r.updated)
	full := gutter + " " + line
	if w := lipgloss.Width(full); w > p.width {
		t.Errorf("data row width = %d, exceeds pane width %d (UPDATED would clip): %q", w, p.width, full)
	}
	if !strings.Contains(full, "3m ago") {
		t.Errorf("data row = %q, want the full UPDATED value \"3m ago\" (not clipped)", full)
	}
}

// TestProjectListDataRowRendersFullUpdatedColumnAtNarrowPane verifies the
// UPDATED value stays intact even when the pane is narrow enough to push NAME
// to its floor — NAME truncates with an ellipsis, UPDATED does not clip.
func TestProjectListDataRowRendersFullUpdatedColumnAtNarrowPane(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.width = 44 // the smallest width at which the row still fits with nameW=8
	p.list = []projRow{
		{code: "ATM", name: "Acme Task Manager", tasks: 3, labels: 5, updated: "3m ago"},
	}
	codeW, tasksW, labelsW, updatedW, nameW := p.projectColumnWidths()
	if nameW != 8 {
		t.Fatalf("nameW = %d, want 8 (floor) at p.width=44", nameW)
	}
	r := p.list[0]
	gutter := " "
	line := fmt.Sprintf(" %-*s %-*s %*d %*d %*s", codeW, r.code, nameW, truncateRunes(r.name, nameW), tasksW, r.tasks, labelsW, r.labels, updatedW, r.updated)
	full := gutter + " " + line
	if w := lipgloss.Width(full); w > p.width {
		t.Errorf("narrow data row width = %d, exceeds pane width %d: %q", w, p.width, full)
	}
	if !strings.Contains(full, "3m ago") {
		t.Errorf("narrow data row = %q, want the full UPDATED value \"3m ago\"", full)
	}
	if !strings.Contains(line, "...") {
		t.Errorf("narrow data row = %q, want NAME truncated with ellipsis", line)
	}
}
```

- [ ] **Step 4: Run the new tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestProjectListDataRow' -v`
Expected: FAIL — `TestProjectListDataRowRendersFullUpdatedColumn` fails because the data row is `p.width + 2` wide and `3m ago` is clipped to `3m ag`.

- [ ] **Step 5: Implement the fix**

Edit `internal/tui/projects.go:399-408`. Replace the body of `projectColumnWidths`:

```go
// projectColumnWidths returns fixed widths for CODE/TASKS/LABELS/UPDATED and a
// flexible NAME width that absorbs the remaining pane width. The data rows
// render with a 2-char "gutter + space" prefix (renderListRows) plus the 5
// chars of overhead inside the format string (1 leading space + 4 inter-column
// spaces), so NAME is sized to leave room for 7 chars of overhead — keeping
// the full row, including UPDATED, inside p.width. UPDATED stays fixed at 10
// so the relative timestamp is never the column that gets clipped; NAME is
// the flexible column and truncates with an ellipsis when the pane is narrow.
func (p *projectsModel) projectColumnWidths() (codeW, tasksW, labelsW, updatedW, nameW int) {
	codeW, tasksW, labelsW, updatedW = 6, 6, 7, 10
	nameW = p.width - codeW - tasksW - labelsW - updatedW - 7
	if nameW < 8 {
		nameW = 8
	}
	return
}
```

- [ ] **Step 6: Run the new tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestProjectColumnWidths|TestProjectListDataRow' -v`
Expected: PASS — all five new tests pass.

- [ ] **Step 7: Run the full TUI suite to check for regressions**

Run: `go test ./internal/tui/ -v`
Expected: PASS — in particular `TestProjectsListPopulated`, `TestProjectsListRendersSummaryRegionBelowList`, `TestProjectsListScrollsWithCursor`, and `TestProjectsBracketKeysPageThroughList` still pass. If any existing test asserts a specific NAME width that depended on the old `-5`/floor-20 arithmetic, update it to assert against the new widths (the columns must still fit the pane — that is the invariant the tests should guard).

- [ ] **Step 8: Run the repository verification gate**

Run: `make verify`
Expected: PASS (build + test + lint). Fix any lint findings in the changed files.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/projects.go internal/tui/fixedslot_test.go
git commit -m "ATM-46f820: fix Projects list row overflow clipping UPDATED column"
```