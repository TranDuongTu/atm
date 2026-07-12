# Activity Stripe: Proportional Bands + Adaptive Day Window — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the activity stripe bar chart span full box width with proportional bar widths driven by real activity counts, and adaptively show 7-14 days based on available width.

**Architecture:** Modify `renderActivityStripeCanvas` to compute bar widths from count/maxCount ratios instead of fixed `cellW`, remove the `cellW > 10` cap, use uniform █ fill glyph, and change axis labels to a centered date range. Add `computeStripDays` to derive day count from available width and wire it into `renderActivityStripeChart`.

**Tech Stack:** Go 1.22+, Bubble Tea TUI, `github.com/NimbleMarkets/ntcharts/canvas`, lipgloss

## Global Constraints

- Go 1.22+
- Follow existing code style in `internal/tui/projects.go`
- `make verify` passes before declaring done

---

### Task 1: Add `computeStripDays` and wire it into `renderActivityStripeChart`

**Files:**
- Modify: `internal/tui/projects.go:556-562`

**Interfaces:**
- Produces: `func computeStripDays(width int) int` — returns day count clamped to [7, 14] based on width, assuming min 3 cells per bar + 1 gap

- [ ] **Step 1: Add `computeStripDays` function**

Add this function to `projects.go`, directly above `activityStripeDayCounts` (before line 87):

```go
func computeStripDays(width int) int {
	const minCellW = 3
	const gap = 1
	const maxDays = 14
	const minDays = 7
	if width < 1 {
		return minDays
	}
	days := (width + gap) / (minCellW + gap)
	if days < minDays {
		return minDays
	}
	if days > maxDays {
		return maxDays
	}
	return days
}
```

- [ ] **Step 2: Wire `computeStripDays` into `renderActivityStripeChart`**

Replace (lines 556-562):

```go
func (p *projectsModel) renderActivityStripeChart(entries []store.LogEntry, bodyHeight int) string {
	days := activityStripeDayCounts(entries, 7)
	if len(days) == 0 {
		return p.m.styles.Muted.Render("no activity yet")
	}
	return renderActivityStripeCanvas(days, chartBoxInnerWidth(p.width), bodyHeight)
}
```

With:

```go
func (p *projectsModel) renderActivityStripeChart(entries []store.LogEntry, bodyHeight int) string {
	innerW := chartBoxInnerWidth(p.width)
	numDays := computeStripDays(innerW)
	days := activityStripeDayCounts(entries, numDays)
	if len(days) == 0 {
		return p.m.styles.Muted.Render("no activity yet")
	}
	return renderActivityStripeCanvas(days, innerW, bodyHeight)
}
```

- [ ] **Step 3: Run existing tests to confirm no regressions yet**

```bash
go test ./internal/tui/ -run "TestRenderActivityStripeCanvas|TestActivityStripeDayCounts|TestRenderActivityStripeIncludes" -v
```

Expected: All pass (existing behavior preserved since `computeStripDays` returns 7 at current test widths).

- [ ] **Step 4: Commit**

```bash
git add internal/tui/projects.go
git commit -m "feat: add computeStripDays for adaptive activity stripe day window"
```

---

### Task 2: Proportional bar widths in `renderActivityStripeCanvas`

**Files:**
- Modify: `internal/tui/projects.go:588-627` (the canvas rendering function)

**Interfaces:**
- Consumes: `[]activityStripeDay` with `day` (string, YYYY-MM-DD) and `count` (int) fields
- Produces: `string` — canvas-rendered activity stripe with proportional bars

- [ ] **Step 1: Rewrite `renderActivityStripeCanvas` with proportional widths and uniform █ glyph**

Replace lines 588-627:

```go
func renderActivityStripeCanvas(days []activityStripeDay, width int, heights ...int) string {
	if len(days) == 0 || width <= 0 {
		return ""
	}
	height := 2
	if len(heights) > 0 && heights[0] > 0 {
		height = heights[0]
	}
	if height < 2 {
		height = 2
	}
	axisH := 1
	bodyH := height - axisH
	if bodyH < 1 {
		bodyH = 1
	}
	const gap = 1

	barWidths, canvasW := computeProportionalBars(days, width, gap)
	if canvasW < 1 {
		return ""
	}

	c := canvas.New(canvasW, height)
	x := 0
	for i, day := range days {
		bw := barWidths[i]
		if bw <= 0 {
			x += gap
			continue
		}
		style := activityCanvasStyle(day.count)
		for col := 0; col < bw; col++ {
			for row := 0; row < bodyH; row++ {
				c.SetRuneWithStyle(canvas.Point{X: x + col, Y: row}, '█', style)
			}
		}
		x += bw + gap
	}
	axis := activityStripeAxis(days, canvasW)
	c.SetStringWithStyle(canvas.Point{X: 0, Y: height - 1}, axis, lipgloss.NewStyle().Foreground(lipgloss.Color("244")))
	return c.View()
}
```

Then add the helper function `computeProportionalBars` right after `renderActivityStripeCanvas`:

```go
func computeProportionalBars(days []activityStripeDay, width, gap int) ([]int, int) {
	numDays := len(days)
	totalBarWidth := width - (numDays-1)*gap
	if totalBarWidth < numDays {
		totalBarWidth = numDays
	}

	maxCount := 0
	for _, day := range days {
		if day.count > maxCount {
			maxCount = day.count
		}
	}

	barWidths := make([]int, numDays)
	if maxCount == 0 {
		for i := range barWidths {
			barWidths[i] = 1
		}
	} else {
		scaledTotal := 0
		for i, day := range days {
			if day.count == 0 {
				barWidths[i] = 0
			} else {
				bw := day.count * totalBarWidth / maxCount
				if bw < 1 {
					bw = 1
				}
				barWidths[i] = bw
			}
			scaledTotal += barWidths[i]
		}
		// Distribute remainder left-to-right to non-zero bars
		remainder := totalBarWidth - scaledTotal
		for i := range barWidths {
			if remainder <= 0 {
				break
			}
			if days[i].count > 0 {
				barWidths[i]++
				remainder--
			}
		}
	}

	canvasW := 0
	for _, bw := range barWidths {
		canvasW += bw
	}
	canvasW += (numDays - 1) * gap
	return barWidths, canvasW
}
```

- [ ] **Step 2: Run tests to see which need updating**

```bash
go test ./internal/tui/ -run "TestRenderActivityStripeCanvas|TestRenderActivityStripe" -v
```

Expected: `TestRenderActivityStripeCanvasUsesMultiLineChart` will fail because it asserts on old glyphs (▅, etc.) and old axis labels (7d ago, Yesterday, Today). This is expected and will be fixed in Task 4.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/projects.go
git commit -m "feat: proportional bar widths with uniform fill in activity stripe"
```

---

### Task 3: Date range axis labels

**Files:**
- Modify: `internal/tui/projects.go:640-665`

**Interfaces:**
- Consumes: `days []activityStripeDay`, `width int`
- Produces: `string` — centered date range label like `"2026-06-28 — 2026-07-12"`

- [ ] **Step 1: Replace `activityStripeAxis` with date range version**

Replace lines 640-665:

```go
func activityStripeAxis(days []activityStripeDay, width int) string {
	if len(days) == 0 || width <= 0 {
		return ""
	}
	start := days[0].day
	end := days[len(days)-1].day
	label := start + " — " + end
	labelW := lipgloss.Width(label)
	if labelW > width {
		startShort := strings.ReplaceAll(start, "-", "")
		endShort := strings.ReplaceAll(end, "-", "")
		label = startShort + " — " + endShort
		labelW = lipgloss.Width(label)
		if labelW > width {
			label = label[:width]
			labelW = width
		}
	}
	line := []rune(repeat(" ", width))
	pos := (width - labelW) / 2
	if pos < 0 {
		pos = 0
	}
	for i, r := range label {
		if pos+i < len(line) {
			line[pos+i] = r
		}
	}
	return string(line)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/tui/projects.go
git commit -m "feat: date range axis labels for activity stripe"
```

---

### Task 4: Remove unused `activityCanvasRune` and `emptyBarRune` functions

**Files:**
- Modify: `internal/tui/projects.go:630-692`

**Interfaces:**
- Removes: `emptyBarRune`, `activityCanvasRune` (no longer called; `renderActivityStripeCanvas` now uses uniform █)

- [ ] **Step 1: Delete unused functions**

Remove the `emptyBarRune` function (lines 630-635):

```go
// REMOVE:
func emptyBarRune(count int) rune {
	if count <= 0 {
		return '▁'
	}
	return activityCanvasRune(count)
}
```

Remove the `activityCanvasRune` function (lines 681-692):

```go
// REMOVE:
func activityCanvasRune(count int) rune {
	switch {
	case count <= 0:
		return '·'
	case count <= 2:
		return '▂'
	case count <= 5:
		return '▅'
	default:
		return '█'
	}
}
```

- [ ] **Step 2: Compile-check**

```bash
go build ./internal/tui/
```

Expected: No compilation errors (unused functions removed).

- [ ] **Step 3: Commit**

```bash
git add internal/tui/projects.go
git commit -m "chore: remove unused activityCanvasRune and emptyBarRune"
```

---

### Task 5: Update tests for new behavior

**Files:**
- Modify: `internal/tui/app_test.go:1066-1091` (`TestRenderActivityStripeCanvasUsesMultiLineChart`)

**Interfaces:**
- Consumes: `renderActivityStripeCanvas` with new proportional bar and axis behavior

- [ ] **Step 1: Run all tests first to identify failures**

```bash
go test ./internal/tui/ -v 2>&1 | grep -E "FAIL|PASS"
```

- [ ] **Step 2: Update `TestRenderActivityStripeCanvasUsesMultiLineChart`**

Replace lines 1066-1091:

```go
func TestRenderActivityStripeCanvasUsesMultiLineChart(t *testing.T) {
	days := []activityStripeDay{
		{day: "2026-07-01", count: 1},
		{day: "2026-07-02", count: 3},
		{day: "2026-07-03", count: 10},
		{day: "2026-07-04", count: 0},
		{day: "2026-07-05", count: 0},
		{day: "2026-07-06", count: 0},
		{day: "2026-07-07", count: 0},
	}
	got := renderActivityStripeCanvas(days, 70)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("renderActivityStripeCanvas() should render a multi-line canvas, got %q", got)
	}
	mustContain(t, got, "█")
	mustContain(t, got, "2026-07-01")
	mustContain(t, got, "2026-07-07")
	if activityCanvasStyle(10).GetForeground() == nil {
		t.Fatalf("activityCanvasStyle should configure foreground color")
	}
	// Bar at index 2 (count 10) must be wider than bar at index 0 (count 1).
	barWidths, _ := computeProportionalBars(days, 70, 1)
	if barWidths[2] <= barWidths[0] {
		t.Fatalf("bar for count 10 (width=%d) should be wider than bar for count 1 (width=%d)",
			barWidths[2], barWidths[0])
	}
	// Day with count 0 should have 0 bar width.
	if barWidths[3] != 0 {
		t.Fatalf("bar for count 0 should have 0 width, got %d", barWidths[3])
	}
}
```

- [ ] **Step 3: Add test for `computeProportionalBars` all-zero case**

Add after the updated test:

```go
func TestComputeProportionalBarsAllZero(t *testing.T) {
	days := []activityStripeDay{
		{day: "2026-07-01", count: 0},
		{day: "2026-07-02", count: 0},
		{day: "2026-07-03", count: 0},
	}
	barWidths, canvasW := computeProportionalBars(days, 30, 1)
	for i, bw := range barWidths {
		if bw != 1 {
			t.Fatalf("all-zero bar[%d] = %d, want 1", i, bw)
		}
	}
	expectedW := 3*1 + 2*1 // 3 bars(1 each) + 2 gaps(1 each)
	if canvasW != expectedW {
		t.Fatalf("canvasW = %d, want %d", canvasW, expectedW)
	}
}

func TestComputeProportionalBarsExact(t *testing.T) {
	days := []activityStripeDay{
		{day: "2026-07-01", count: 1},
		{day: "2026-07-02", count: 1},
		{day: "2026-07-03", count: 1},
	}
	barWidths, canvasW := computeProportionalBars(days, 12, 1)
	// 2 gaps of 1 each = 10 cells for bars. 3 equal bars => 3,3,3 => total 9.
	// Remainder 1 distributed to first non-zero bar (index 0).
	if barWidths[0] != 4 || barWidths[1] != 3 || barWidths[2] != 3 {
		t.Fatalf("barWidths = %v, want [4 3 3]", barWidths)
	}
	expectedW := 4 + 3 + 3 + 2 // bars + gaps
	if canvasW != expectedW {
		t.Fatalf("canvasW = %d, want %d", canvasW, expectedW)
	}
}

func TestComputeProportionalBarsMixed(t *testing.T) {
	days := []activityStripeDay{
		{day: "2026-07-01", count: 10},
		{day: "2026-07-02", count: 5},
		{day: "2026-07-03", count: 0},
	}
	// 100 cells for totalBarWidth with gap=1: 100 - 2 = 98
	barWidths, _ := computeProportionalBars(days, 100, 1)
	if barWidths[2] != 0 {
		t.Fatalf("zero-count bar should have 0 width, got %d", barWidths[2])
	}
	if barWidths[0] <= barWidths[1] {
		t.Fatalf("count 10 bar (%d) should be wider than count 5 bar (%d)", barWidths[0], barWidths[1])
	}
	// Should be roughly 2:1 ratio: count 10 vs count 5.
	ratio := float64(barWidths[0]) / float64(barWidths[1])
	if ratio < 1.5 || ratio > 3.0 {
		t.Fatalf("expected 2:1 ratio approx, got bars %v (ratio %.2f)", barWidths, ratio)
	}
}

func TestComputeStripDaysRange(t *testing.T) {
	tests := []struct {
		width    int
		wantDays int
	}{
		{width: 10, wantDays: 7},
		{width: 27, wantDays: 7},  // (27+1)/(3+1) = 7
		{width: 31, wantDays: 8},  // (31+1)/4 = 8
		{width: 59, wantDays: 14}, // (59+1)/4 = 15, capped at 14
		{width: 200, wantDays: 14},
	}
	for _, tc := range tests {
		got := computeStripDays(tc.width)
		if got != tc.wantDays {
			t.Errorf("computeStripDays(%d) = %d, want %d", tc.width, got, tc.wantDays)
		}
	}
}

func TestActivityStripeAxisDateRange(t *testing.T) {
	days := []activityStripeDay{
		{day: "2026-07-01", count: 1},
		{day: "2026-07-07", count: 2},
	}
	got := activityStripeAxis(days, 30)
	mustContain(t, got, "2026-07-01")
	mustContain(t, got, "2026-07-07")
	mustContain(t, got, "—")
}
```

- [ ] **Step 4: Add test for adaptive day count in chart rendering**

Add after the axis test:

```go
func TestRenderActivityStripeChartAdaptiveDays(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 48)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "t1", "ATM:status:open")
	update(t, m, "s")
	body := m.projects.View()
	mustContain(t, body, "activity stripe")
	// At 200 cols, should have adaptive day count. The stripe should
	// contain more than 7 bars.
	// Check that the date range appears somewhere in the output.
	mustContain(t, body, "—")
}
```

- [ ] **Step 5: Run all stripe-related tests**

```bash
go test ./internal/tui/ -run "TestRenderActivityStripe|TestComputeProportionalBars|TestComputeStripDays|TestActivityStripeAxis|TestRenderActivityStripeChart" -v
```

Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app_test.go
git commit -m "test: update activity stripe tests for proportional bars and adaptive days"
```

---

### Task 6: Final verification

- [ ] **Step 1: Run full test suite**

```bash
go test ./internal/... -v 2>&1 | tail -20
```

Expected: All tests PASS.

- [ ] **Step 2: Run `make verify`**

```bash
make verify
```

Expected: Build + test + lint all pass.

- [ ] **Step 3: Update ATM task**

Dispatch `atm-manager` to set `ATM-0103` status to `ATM:status:in-review` and add a comment summarizing what was done.

- [ ] **Step 4: Commit any final changes if needed**

```bash
git status
```
