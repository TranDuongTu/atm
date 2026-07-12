package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"atm/internal/activity"
	"atm/internal/store"
	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// projectsModel owns the Projects pane state: list, detail, cursor, selection.
type projectsModel struct {
	m             *Model
	width         int
	contentHeight int
	list          []projRow
	view          pView
	cursor        int
	detail        detailState

	// history toggle on project detail.
	showHistory bool
}

type pView int

const (
	pViewList pView = iota
	pViewDetail
)

type projRow struct {
	code     string
	name     string
	tasks    int
	labels   int
	updated  string // relative
	updatedT int64  // unix for sort (unused; store pre-sorts by code)
}

type detailState struct {
	code      string
	project   *store.Project
	lines     []string // rendered detail lines (for scroll)
	offset    int
	historyOn bool
}

type activityStripeDay struct {
	day   string
	count int
}

func newProjectsModel(m *Model) projectsModel {
	return projectsModel{m: m}
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

func activityStripeDayCounts(entries []store.LogEntry, days int) []activityStripeDay {
	return activityStripeDayCountsEnding(entries, days, store.Now())
}

func activityStripeDayCountsEnding(entries []store.LogEntry, days int, end time.Time) []activityStripeDay {
	if days <= 0 {
		return nil
	}
	counts := map[string]int{}
	for _, e := range entries {
		if e.At.IsZero() {
			continue
		}
		day := e.At.UTC().Format("2006-01-02")
		counts[day]++
	}
	end = end.UTC()
	start := end.AddDate(0, 0, -(days - 1))
	out := make([]activityStripeDay, 0, days)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		out = append(out, activityStripeDay{day: key, count: counts[key]})
	}
	return out
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

func (p *projectsModel) SetSize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	p.width = w
	p.contentHeight = h
	p.detail.offset = 0
}

func (p *projectsModel) refresh() {
	ps := p.m.store.ListProjects()
	p.list = make([]projRow, 0, len(ps))
	for _, pr := range ps {
		tasks := len(listTaskIDs(p.m.store, pr.Code))
		labels := len(p.m.store.LabelList(pr.Code, ""))
		p.list = append(p.list, projRow{
			code:    pr.Code,
			name:    pr.Name,
			tasks:   tasks,
			labels:  labels,
			updated: relTime(pr.UpdatedAt, store.Now()),
		})
	}
	// store pre-sorts by code-asc; keep that (fixed sort per mockup).
	if p.cursor >= len(p.list) && len(p.list) > 0 {
		p.cursor = len(p.list) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *projectsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "P":
		return p.openActorsOverlay()
	case "p":
		return p.m.openPersonaCreateForm()
	}
	switch p.view {
	case pViewList:
		return p.handleListKey(k)
	case pViewDetail:
		return p.handleDetailKey(k)
	}
	return nil
}

func (p *projectsModel) handleListKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		if p.cursor < len(p.list)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case "g":
		p.cursor = 0
	case "]":
		listH, _ := projectPaneSplitHeights(p.contentHeight)
		p.cursor += p.listPageSize(listH)
		if p.cursor > len(p.list)-1 {
			p.cursor = len(p.list) - 1
		}
		if p.cursor < 0 {
			p.cursor = 0
		}
	case "[":
		listH, _ := projectPaneSplitHeights(p.contentHeight)
		p.cursor -= p.listPageSize(listH)
		if p.cursor < 0 {
			p.cursor = 0
		}
	case "enter", "e":
		if r, ok := p.selected(); ok {
			p.openDetail(r.code)
		}
	case "s":
		if r, ok := p.selected(); ok {
			p.m.projectScope = r.code
			// D15: auto-start the indexer for the newly-selected project
			// (starts the watcher if config present; opens the overlay to
			// configure if not). resetIndexer on the old project is handled
			// inside autoStartIndexer's contract — the caller sets the new
			// projectScope first, then autoStart refreshes against it. The
			// old watcher, if any, is stopped here. autoStartIndexer returns
			// the pluginTickCmd from startIndexer; returning it here lets the
			// Bubble Tea runtime schedule the pluginTickMsg that drains
			// im.msgCh — discarding it (ATM-0077) leaves the dock stuck on
			// "running" with an empty log pane.
			if p.m.indexer != nil {
				resetIndexer(p.m)
			}
			cmd := autoStartIndexer(p.m, r.code)
			p.m.tasks.refresh()
			p.m.labels.refresh()
			return cmd
		}
	case "a":
		p.openCreateForm()
	case "x":
		if r, ok := p.selected(); ok {
			return p.requestRemoveProject(r.code)
		}
	}
	return nil
}

func (p *projectsModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		p.detail.offset++
		p.clampDetail()
	case "k", "up":
		if p.detail.offset > 0 {
			p.detail.offset--
		}
	case "g":
		p.detail.offset = 0
	case "n":
		p.openSetNameForm()
	case "H":
		p.detail.historyOn = !p.detail.historyOn
		p.renderDetail()
	case "x":
		return p.requestRemoveProject(p.detail.code)
	}
	return nil
}

func (p *projectsModel) selected() (projRow, bool) {
	if p.cursor < 0 || p.cursor >= len(p.list) {
		return projRow{}, false
	}
	return p.list[p.cursor], true
}

func (p *projectsModel) openDetail(code string) {
	pr, err := p.m.store.GetProject(code)
	if err != nil {
		p.m.showToast("error: " + err.Error())
		return
	}
	p.detail = detailState{code: code, project: pr, historyOn: false}
	p.view = pViewDetail
	p.renderDetail()
}

func (p *projectsModel) backToList() {
	p.view = pViewList
	p.detail = detailState{}
}

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

func (p *projectsModel) clampDetail() {
	maxOff := len(p.detail.lines) - p.contentHeight
	if maxOff < 0 {
		maxOff = 0
	}
	if p.detail.offset > maxOff {
		p.detail.offset = maxOff
	}
	if p.detail.offset < 0 {
		p.detail.offset = 0
	}
}

func (p *projectsModel) View() string {
	switch p.view {
	case pViewList:
		return p.renderList()
	case pViewDetail:
		return p.renderDetailView()
	}
	return ""
}

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

// projectColumnWidths returns fixed widths for CODE/TASKS/LABELS/UPDATED and a
// flexible NAME width that absorbs the remaining pane width.
func (p *projectsModel) projectColumnWidths() (codeW, tasksW, labelsW, updatedW, nameW int) {
	codeW, tasksW, labelsW, updatedW = 6, 6, 7, 10
	nameW = p.width - codeW - tasksW - labelsW - updatedW - 5
	if nameW < 20 {
		nameW = 20
	}
	return
}

// listPageSize returns the number of project rows that fit in the list
// section at the given section height, after the caption/header/rule/footer
// overhead. Shared by rendering (the visible window) and the "[" / "]" page
// jump so both agree on what a "page" is.
func (p *projectsModel) listPageSize(maxRows int) int {
	availableRows := maxRows - 4 // caption + header + rule + footer
	if availableRows < 1 {
		availableRows = 1
	}
	return availableRows
}

func (p *projectsModel) renderListRows(maxRows int) string {
	var b strings.Builder
	selected := p.m.projectScope
	if selected == "" {
		selected = "none"
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("total projects: %d   selected: %s", len(p.list), selected)))
	codeW, tasksW, labelsW, updatedW, nameW := p.projectColumnWidths()
	header := fmt.Sprintf(" %-*s %-*s %*s %*s %*s", codeW, "CODE", nameW, "NAME", tasksW, "TASKS", labelsW, "LABELS", updatedW, "UPDATED")
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.HeaderLabel.Render(header)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, repeat("─", dashboardContentWidth(p.width))))

	pageSize := p.listPageSize(maxRows)
	start, end := windowLines(len(p.list), p.cursor, pageSize)
	for i := start; i < end; i++ {
		r := p.list[i]
		var gutter string
		if r.code == p.m.projectScope {
			gutter = p.m.styles.GutterSelect.Render("▸")
		} else {
			gutter = " "
		}
		line := fmt.Sprintf(" %-*s %-*s %*d %*d %*s", codeW, r.code, nameW, truncateRunes(r.name, nameW), tasksW, r.tasks, labelsW, r.labels, updatedW, r.updated)
		if i == p.cursor {
			line = gutter + " " + p.m.styles.RowCursor.Render(line)
		} else {
			line = gutter + " " + line
		}
		fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, line))
	}
	if end == start {
		fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.Muted.Render("showing 0-0 of 0")))
	} else {
		fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.m.styles.Muted.Render(fmt.Sprintf("showing %d-%d of %d", start+1, end, len(p.list)))))
	}
	return b.String()
}

func (p *projectsModel) renderSummary(height int) string {
	lines := []string{dashboardLine(p.width, p.m.styles.HeaderLabel.Render("Project Summary"))}
	if p.m.projectScope == "" {
		lines = append(lines, dashboardLine(p.width, p.m.styles.Muted.Render("select a project to see summaries")))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	project, tasks, entries, vocab, ok := p.projectSummaryData()
	if !ok {
		lines = append(lines, dashboardLine(p.width, p.m.styles.Muted.Render("selected project could not be loaded")))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	lines = append(lines, dashboardLine(p.width, fmt.Sprintf("project: %s   tasks: %d", project.Code, len(tasks))))

	remaining := height - len(lines)
	if remaining <= 0 {
		return padToHeight(strings.Join(lines, "\n"), height)
	}

	if remaining >= 9 {
		actorH, stripeH, bubblesH := chartBoxHeights(remaining)
		lines = append(lines, p.renderPersonaActivityChart(entries, actorH)...)
		lines = append(lines, strings.Split(p.renderChartBox("activity stripe", p.renderActivityStripeChart(entries, stripeH-2), stripeH), "\n")...)
		lines = append(lines, strings.Split(p.renderUbiquitousLanguageChart(vocab, bubblesH), "\n")...)
		return padToHeight(strings.Join(lines, "\n"), height)
	}

	if remaining == 3 {
		// persona chart + a one-line stripe row.
		lines = append(lines, p.renderPersonaActivityChart(entries, 2)...)
		lines = append(lines, dashboardLine(p.width, fmt.Sprintf("activity stripe %s", p.renderActivityStripeChart(entries, 2))))
		return padToHeight(strings.Join(lines, "\n"), height)
	}

	actorMax := remaining
	if remaining > 6 {
		actorMax = remaining - 4
	} else if remaining > 3 {
		actorMax = remaining - 2
	}
	if actorMax > 0 {
		lines = append(lines, p.renderPersonaActivityChart(entries, actorMax)...)
	}
	if height-len(lines) >= 2 {
		lines = append(lines,
			dashboardLine(p.width, "activity stripe"),
			dashboardLine(p.width, p.renderActivityStripeChart(entries, 2)),
		)
	}
	return padToHeight(strings.Join(lines, "\n"), height)
}

func chartBoxHeights(total int) (int, int, int) {
	if total < 9 {
		return total, 0, 0
	}
	actor := total / 3
	stripe := total / 3
	bubbles := total - actor - stripe
	if actor < 3 {
		actor = 3
	}
	if stripe < 3 {
		stripe = 3
	}
	if bubbles < 3 {
		bubbles = 3
	}
	return actor, stripe, bubbles
}

func (p *projectsModel) renderPersonaActivityChart(entries []store.LogEntry, maxLines int) []string {
	if maxLines <= 0 {
		return nil
	}
	// 1 line genuinely cannot fit a bar row alongside a label, so the
	// expand hint is the only legible content there. 2-3 lines render a
	// compact title + bar rows (no border). From 4 up the bordered chart
	// box takes over (its title lives in the top border, so we do not
	// prepend one ourselves).
	if maxLines == 1 {
		return []string{dashboardLine(p.width, "activity by persona  [P]expand")}
	}
	groups := activity.Aggregate(activity.Build(entries), "persona")
	if len(groups) == 0 {
		body := p.m.styles.Muted.Render("no activity yet")
		if maxLines < 4 {
			return []string{
				dashboardLine(p.width, "activity by persona  [P]expand"),
				dashboardLine(p.width, body),
			}[:maxLines]
		}
		return strings.Split(p.renderChartBox("activity by persona  [P]expand", body, maxLines), "\n")
	}
	nameW := longestPersonaKeyWidth(groups)
	meterW := chartBoxInnerWidth(p.width) - nameW - 10
	if meterW < 10 {
		meterW = 10
	}
	total := 0
	for _, g := range groups {
		total += g.Count
	}
	barRow := func(g activity.Group) string {
		percent := 0
		if total > 0 {
			percent = (g.Count*100 + total/2) / total
		}
		return fmt.Sprintf("%-*s %s %3d%% %3d", nameW, g.Key, meterBar(percent, meterW), percent, g.Count)
	}
	if maxLines < 4 {
		// Compact: title row + as many bar rows as fit.
		rows := []string{dashboardLine(p.width, "activity by persona  [P]expand")}
		cap := maxLines - 1
		if len(groups) > cap {
			groups = groups[:cap]
		}
		for _, g := range groups {
			rows = append(rows, dashboardLine(p.width, barRow(g)))
		}
		return rows
	}
	// Boxed: renderChartBox draws the title in the border; emit bar rows
	// only, capped to the box's inner height (maxLines - 2).
	cap := maxLines - 2
	if len(groups) > cap {
		groups = groups[:cap]
	}
	var body []string
	for _, g := range groups {
		body = append(body, barRow(g))
	}
	return strings.Split(p.renderChartBox("activity by persona  [P]expand", strings.Join(body, "\n"), maxLines), "\n")
}

func longestPersonaKeyWidth(groups []activity.Group) int {
	width := 0
	for _, g := range groups {
		if w := lipgloss.Width(g.Key); w > width {
			width = w
		}
	}
	return width
}

func (p *projectsModel) renderActivityStripeChart(entries []store.LogEntry, bodyHeight int) string {
	innerW := chartBoxInnerWidth(p.width)
	numDays := computeStripDays(innerW)
	days := activityStripeDayCounts(entries, numDays)
	if len(days) == 0 {
		return p.m.styles.Muted.Render("no activity yet")
	}
	return renderActivityStripeCanvas(days, innerW, bodyHeight)
}

func renderActivityStripe(days []activityStripeDay) string {
	if len(days) == 0 {
		return ""
	}
	var b strings.Builder
	for _, day := range days {
		b.WriteString(activityDensityGlyph(day.count))
	}
	return b.String()
}

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

func computeProportionalBars(days []activityStripeDay, width, gap int) ([]int, int) {
	numDays := len(days)
	totalBarWidth := width - (numDays-1)*gap
	if totalBarWidth < numDays {
		totalBarWidth = numDays
	}

	totalCount := 0
	for _, day := range days {
		totalCount += day.count
	}

	barWidths := make([]int, numDays)
	if totalCount == 0 {
		for i := range barWidths {
			barWidths[i] = 1
		}
	} else {
		scaledTotal := 0
		for i, day := range days {
			if day.count == 0 {
				barWidths[i] = 0
			} else {
				bw := day.count * totalBarWidth / totalCount
				if bw < 1 {
					bw = 1
				}
				barWidths[i] = bw
			}
			scaledTotal += barWidths[i]
		}
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

func activityCanvasStyle(count int) lipgloss.Style {
	switch {
	case count <= 0:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	case count <= 2:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	case count <= 5:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	}
}

func renderUbiquitousLanguageCanvas(width int, height int, terms []store.VocabularyTerm) string {
	if width < 18 {
		width = 18
	}
	if height < 3 {
		height = 3
	}
	c := canvas.New(width, height)
	colors := []lipgloss.Color{"39", "214", "82", "171", "203", "117"}
	sorted := make([]store.VocabularyTerm, len(terms))
	copy(sorted, terms)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Weight != sorted[j].Weight {
			return sorted[i].Weight > sorted[j].Weight
		}
		return sorted[i].Term < sorted[j].Term
	})
	for i, term := range sorted {
		if i >= 12 {
			break
		}
		col := colors[i%len(colors)]
		style := lipgloss.NewStyle().Foreground(col).Bold(term.Weight >= 7)
		x := (i * 13) % width
		y := i % height
		c.SetStringWithStyle(canvas.Point{X: x, Y: y}, term.Term, style)
	}
	return c.View()
}

func (p *projectsModel) renderUbiquitousLanguageChart(vocab *store.Vocabulary, maxLines int) string {
	if maxLines < 3 {
		return dashboardLine(p.width, "Ubiquitous Language")
	}
	innerW := chartBoxInnerWidth(p.width)
	innerH := maxLines - 2
	var body string
	if vocab == nil || len(vocab.Terms) == 0 {
		body = p.m.styles.Muted.Render("no vocabulary yet — manager has not computed it")
	} else {
		body = renderUbiquitousLanguageCanvas(innerW, innerH, vocab.Terms)
	}
	return p.renderChartBox("Ubiquitous Language", body, maxLines)
}

func chartBoxWidth(width int) int {
	if width <= 8 {
		return width
	}
	w := width * 96 / 100
	if w < 18 {
		w = width
	}
	if w > width {
		w = width
	}
	return w
}

func chartBoxInnerWidth(width int) int {
	w := chartBoxWidth(width) - 2
	if w < 1 {
		return 1
	}
	return w
}

func (p *projectsModel) renderChartBox(title, body string, maxLines int) string {
	boxW := chartBoxWidth(p.width)
	if boxW < 3 || maxLines < 3 {
		return dashboardLine(p.width, title)
	}
	innerW := boxW - 2
	bodyLines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(bodyLines) == 1 && bodyLines[0] == "" {
		bodyLines = []string{""}
	}
	innerH := maxLines - 2
	if len(bodyLines) > innerH {
		bodyLines = bodyLines[:innerH]
	}
	topPad := 0
	if len(bodyLines) < innerH {
		topPad = (innerH - len(bodyLines)) / 2
	}
	for i := 0; i < topPad; i++ {
		bodyLines = append([]string{""}, bodyLines...)
	}
	for len(bodyLines) < innerH {
		bodyLines = append(bodyLines, "")
	}
	border := p.m.styles.Muted
	content := p.m.styles.Body
	label := " " + title + " "
	if lipgloss.Width(label) > innerW {
		label = fitLine(label, innerW)
	}
	topFill := innerW - lipgloss.Width(label)
	if topFill < 0 {
		topFill = 0
	}
	top := border.Render("╭" + label + repeat("─", topFill) + "╮")
	bottom := border.Render("╰" + repeat("─", innerW) + "╯")
	out := []string{top}
	for _, line := range bodyLines {
		fit := fitLine(line, innerW)
		leftPad := 0
		if lipgloss.Width(fit) < innerW {
			leftPad = (innerW - lipgloss.Width(fit)) / 2
		}
		fit = spaces(leftPad) + fit
		pad := innerW - lipgloss.Width(fit)
		if pad < 0 {
			pad = 0
		}
		out = append(out, border.Render("│")+content.Render(fit)+spaces(pad)+border.Render("│"))
	}
	out = append(out, bottom)
	prefix := spaces((p.width - boxW) / 2)
	for i := range out {
		out[i] = dashboardLine(p.width, prefix+out[i])
	}
	return strings.Join(out, "\n")
}

func meterBar(percent int, width int) string {
	if width <= 0 {
		return ""
	}
	filled := (percent*width + 99) / 100
	if percent <= 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return repeat("█", filled) + repeat("░", width-filled)
}

func (p *projectsModel) projectSummaryData() (*store.Project, []*store.Task, []store.LogEntry, *store.Vocabulary, bool) {
	code := p.m.projectScope
	if code == "" {
		return nil, nil, nil, nil, false
	}
	project, err := p.m.store.GetProject(code)
	if err != nil {
		return nil, nil, nil, nil, false
	}
	tasks := p.m.store.ListTasks(store.QueryFilters{Project: code})
	entries, err := p.m.store.ReadLogCached(code)
	if err != nil && !store.IsIntegrity(err) {
		return nil, nil, nil, nil, false
	}
	vocab, _ := p.m.store.GetVocabulary(code)
	return project, tasks, entries, vocab, true
}

// renderEmpty renders the empty-store landing (mockup Screen 1): a heading
// and the first-run guidance, each line center-aligned within the dashboard
// content area (so lines stay centered regardless of width, not wrapped).
// The [a] action key is highlighted to draw the eye.
func (p *projectsModel) renderEmpty() string {
	lines := []string{
		p.m.styles.EmptyHead.Render("no projects"),
		"",
		p.m.styles.EmptyText.Render(fmt.Sprintf("press %s to add a project, then seed", p.m.styles.EmptyKey.Render("[a]"))),
		p.m.styles.EmptyDim.Render("index tasks (start-here, repo:, doc:)"),
		p.m.styles.EmptyDim.Render("and label as you go"),
	}
	return padToHeight(centerLinesBoth(lines, p.width, p.contentHeight), p.contentHeight)
}

func (p *projectsModel) renderDetailView() string {
	end := p.detail.offset + p.contentHeight
	if end > len(p.detail.lines) {
		end = len(p.detail.lines)
	}
	var b strings.Builder
	for i := p.detail.offset; i < end; i++ {
		b.WriteString(p.detail.lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), p.contentHeight)
}

func (p *projectsModel) statusHint() string {
	switch p.view {
	case pViewList:
		if len(p.list) == 0 {
			return "[a]add [p]ersona [?]keys"
		}
		return "[a]dd [s]elect [Enter]detail [x]remove [P]ersona [p]new [?]keys"
	case pViewDetail:
		return "[N]ame [H]istory [x]remove [P]ersona [p]new [Esc]back"
	}
	return "[?]keys"
}

// --- form openers ---

var codeRe = regexp.MustCompile(`^[A-Z]{3,6}$`)

// openActorsOverlay opens the P overlay showing persona activity for the
// current project scope. Tosts and does nothing if no project is selected.
func (p *projectsModel) openActorsOverlay() tea.Cmd {
	if p.m.projectScope == "" {
		p.m.showToast("select a project first")
		return nil
	}
	p.m.actorsOverlay = true
	p.m.actors.refresh()
	p.m.sizeActorsToOverlay()
	return nil
}

func (p *projectsModel) openCreateForm() {
	codeValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !codeRe.MatchString(value) {
			return fmt.Errorf("code must be 3-6 uppercase letters")
		}
		return nil
	}
	fields := []formField{
		{Label: "code", Required: true, Hint: "3-6 uppercase letters, e.g. ATM", Validator: codeValidator},
		{Label: "name", Required: true, Hint: "project display name"},
	}
	f := NewForm("New project", fields)
	p.m.form = f
	p.m.formKind = formProjectCreate
}

func (p *projectsModel) openSetNameForm() {
	pr := p.detail.project
	if pr == nil {
		return
	}
	fields := []formField{
		{Label: "name", Required: true, Value: pr.Name, Hint: "new project display name"},
	}
	f := NewForm("Set project name", fields)
	p.m.form = f
	p.m.formKind = formProjectSetName
	p.m.formPayload = pr.Code
}

// --- mutations ---

func (p *projectsModel) requestRemoveProject(code string) tea.Cmd {
	// Pre-check: tasks present -> refuse (store guard), else ask confirm.
	if n := len(listTaskIDs(p.m.store, code)); n > 0 {
		p.m.showToast(fmt.Sprintf("3 conflict: project has %d tasks — remove tasks first", n))
		return nil
	}
	p.m.confirm = confirmRemoveProject
	p.m.confirmMsg = fmt.Sprintf("Remove project %s?", code)
	p.m.confirmArg = "History is lost. Registry labels are unaffected.\n[Enter] confirm   [Esc] cancel"
	p.m.confirmArg = "History is lost. Registry labels are unaffected."
	return nil
}

// doProjectCreate handles submit of the create form.
func (m *Model) doProjectCreate(vals map[string]string) tea.Cmd {
	code := vals["code"]
	name := vals["name"]
	if _, err := m.store.CreateProject(code, name, m.actor); err != nil {
		if store.IsConflict(err) {
			m.showToast(fmt.Sprintf("4 conflict: code %s exists", code))
		} else {
			m.showToast("error: " + err.Error())
		}
		return nil
	}
	m.projectScope = code
	m.refreshAll()
	return nil
}

func (m *Model) doProjectSetName(vals map[string]string) tea.Cmd {
	code := m.formPayload
	name := vals["name"]
	if err := m.store.SetProjectName(code, name, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.projects.openDetail(code)
	return nil
}

func (m *Model) confirmYes() tea.Cmd {
	switch m.confirm {
	case confirmRemoveProject:
		code := m.projects.detail.code
		if m.projects.view != pViewDetail {
			// removing from list: use cursor row
			if r, ok := m.projects.selected(); ok {
				code = r.code
			}
		}
		err := m.store.RemoveProject(code, m.actor)
		m.confirm = confirmNone
		if err != nil {
			m.showToast("error: " + err.Error())
			return nil
		}
		if m.projectScope == code {
			m.projectScope = ""
			if m.indexer != nil {
				resetIndexer(m)
			}
		}
		m.projects.backToList()
		m.refreshAll()
		return nil
	case confirmRemoveTask:
		id := m.tasks.detail.id
		err := m.store.RemoveTask(id, m.actor)
		m.confirm = confirmNone
		if err != nil {
			m.showToast("error: " + err.Error())
			return nil
		}
		m.tasks.backToList()
		m.refreshAll()
		return nil
	case confirmDropIndex:
		model := m.confirmPayload
		if err := m.store.DropVectors(m.projectScope, model); err != nil {
			m.showToast("error: " + err.Error())
		} else {
			m.showToast(fmt.Sprintf("dropped vector index %s/%s", m.projectScope, model))
		}
		if m.indexer != nil {
			m.indexer.refreshStatus()
		}
		m.confirm = confirmNone
		m.confirmPayload = ""
		return nil
	}
	m.confirm = confirmNone
	return nil
}

// metaJSON renders a history meta map as a stable JSON-ish single line.
func metaJSON(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteString("{")
	first := true
	for k, v := range m {
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "%q:%v", k, v)
	}
	b.WriteString("}")
	return b.String()
}

// padToHeight right-pads the string with blank lines so it fills `h` lines.
func padToHeight(s string, h int) string {
	lines := strings.Split(s, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// listTaskIDs returns the per-project task IDs via the exported store query
// API (the store's own listTaskIDs is unexported).
func listTaskIDs(s *store.Store, code string) []string {
	ts := s.ListTasks(store.QueryFilters{Project: code})
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}
