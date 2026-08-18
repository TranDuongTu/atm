package tui

import (
	"fmt"
	"regexp"
	"strings"

	"atm/internal/activity"
	"atm/internal/core"
	"atm/internal/tui/art"
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

	// logsOffset is the Recent Events feed's viewport offset (list view,
	// ATM-793b19 revision 2): shift+arrows drive it directly, with no
	// subfocus mode — see events_feed.go's scrollEventsFeed. It indexes the
	// newest-first feed; render clamps it into a local copy only (View must
	// stay pure), so out-of-range values persist harmlessly until the next
	// key handler that moves it.
	logsOffset int

	// Combined activity chart state. Rendering consumes the refresh-time
	// summary snapshot; only keys and project-scope writes mutate this state.
	chartPersona string
	chartRange   int
	chartFocused bool
	chartDrill   bool

	// Render snapshot for the summary pane and events feed, rebuilt by
	// refreshSummary (refreshAll and the project select/deselect handlers).
	// View reads ONLY these — the old per-frame GetProject + ListTasks +
	// ReadLogCached reads ran a freshness probe per call and made every
	// keystroke O(store) (ATM-4c476c). External appends surface when the
	// 10s refresh tick rebuilds the snapshot, same as every other pane.
	summaryProject *core.Project
	summaryTasks   []*core.Task
	summaryEntries []core.LogEntry
	summaryOK      bool
	feedOK         bool
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
	code    string
	project *core.Project
	lines   []string // rendered detail lines (for scroll)
	offset  int
}

func newProjectsModel(m *Model) projectsModel {
	return projectsModel{m: m}
}

// projectPaneSplitHeights allocates the list view's vertical space four
// ways, top to bottom: project list (fixed page of 5 rows = 9 lines with
// caption/header/rule/footer), background art (absorbs the spare height),
// recent-events feed (~35%, collapses under 4 — see the boxed/compact frame
// note on renderEventsFeed), and summary (~35%, keeps the bottom). An art
// slot under 3 lines is not worth drawing (art.MinH) and folds into summary,
// restoring the pre-art layout on short panes.
func projectPaneSplitHeights(total int) (listH, artH, eventsH, summaryH int) {
	if total <= 0 {
		return 0, 0, 0, 0
	}
	listH = 9 // caption + header + rule + 5 rows + footer
	if listH > total {
		return total, 0, 0, 0
	}
	remaining := total - listH
	eventsH = total * 35 / 100
	if eventsH > remaining {
		eventsH = remaining
	}
	if eventsH < 4 {
		eventsH = 0
	}
	remaining -= eventsH
	summaryH = total * 35 / 100
	if summaryH > remaining {
		summaryH = remaining
	}
	artH = remaining - summaryH
	if artH < 3 {
		summaryH += artH
		artH = 0
	}
	return listH, artH, eventsH, summaryH
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
			updated: relTime(pr.UpdatedAt, core.Now()),
		})
	}
	// store pre-sorts by code-asc; keep that (fixed sort per mockup).
	if p.cursor >= len(p.list) && len(p.list) > 0 {
		p.cursor = len(p.list) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.refreshSummary()
}

func (p *projectsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	if p.view == pViewList {
		if cmd, handled := p.handleChartKey(k); handled {
			return cmd
		}
	}
	switch p.view {
	case pViewList:
		return p.handleListKey(k)
	case pViewDetail:
		return p.handleDetailKey(k)
	}
	return nil
}

func (p *projectsModel) handleChartKey(k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "ctrl+left", "ctrl+right":
		p.focusChart()
		entries := carouselEntries(activity.Aggregate(activity.Build(p.summaryEntries), "persona"))
		direction := -1
		if k.String() == "ctrl+right" {
			direction = 1
		}
		p.chartPersona = carouselStep(entries, p.chartPersona, direction)
		return nil, true
	case "ctrl+up":
		p.focusChart()
		if p.chartRange < len(chartRanges)-1 {
			p.chartRange++
		}
		return nil, true
	case "ctrl+down":
		p.focusChart()
		if p.chartRange > 0 {
			p.chartRange--
		}
		return nil, true
	case "enter", "ctrl+j", "ctrl+enter":
		if k.String() == "enter" && !p.chartFocused {
			return nil, false
		}
		p.focusChart()
		p.chartDrill = true
		return nil, true
	case "esc":
		if p.chartDrill {
			p.chartDrill = false
			return nil, true
		}
	}
	return nil, false
}

func (p *projectsModel) focusChart() {
	p.chartFocused = true
}

func (p *projectsModel) openPersonaActivity() {
	spec := chartRanges[0]
	if p.chartRange >= 0 && p.chartRange < len(chartRanges) {
		spec = chartRanges[p.chartRange]
	}
	entries := carouselEntries(activity.Aggregate(activity.Build(p.summaryEntries), "persona"))
	p.m.personaAct.openFor(carouselSelected(entries, p.chartPersona), spec, p.summaryEntries)
}

func (p *projectsModel) resetChart() {
	p.chartPersona = ""
	p.chartRange = 0
	p.chartFocused = false
	p.chartDrill = false
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
	case "shift+up", "shift+down":
		// Move the events feed viewport by one line — same modeless pattern
		// as the Tasks pane's thumbnail chart cursor (tasks_list.go).
		dir := -1
		if k.String() == "shift+down" {
			dir = 1
		}
		p.scrollEventsFeed(dir, 1)
	case "shift+left", "shift+right":
		// Page the events feed viewport.
		dir := -1
		if k.String() == "shift+right" {
			dir = 1
		}
		p.scrollEventsFeed(dir, p.eventsPageSize())
	case "]":
		listH, _, _, _ := projectPaneSplitHeights(p.contentHeight)
		p.cursor += p.listPageSize(listH)
		if p.cursor > len(p.list)-1 {
			p.cursor = len(p.list) - 1
		}
		if p.cursor < 0 {
			p.cursor = 0
		}
	case "[":
		listH, _, _, _ := projectPaneSplitHeights(p.contentHeight)
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
			// ATM-0082: a project switch is a clean break for the right
			// column. Reset the Tasks pane via its documented single
			// channel (setFocus) so view/detail/filter/focus/cursor/offset
			// all return to a fresh list, and the Boards pane back to L0.
			// Going through setFocus (rather than poking fields directly)
			// keeps the invariant that the Tasks pane never edits its own
			// filter; it also clears stale view/detail from a task the
			// user had open under the previous project.
			p.m.boards.reset()
			p.m.tasks.backToList()
			p.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
			p.m.capability.current = "" // re-resolve for the new project
			p.logsOffset = 0            // fresh project: viewport back to the newest event
			p.resetChart()
			if _, err := p.m.regFor(r.code).EnsureVocabulary(p.m.store, r.code, p.m.actor); err != nil {
				p.m.showToast("ensure workflow boards: " + err.Error())
			}
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
			p.m.capability.refresh()
			p.m.boards.refresh()
			p.m.boards.selectDefault()
			// tasks.refresh runs AFTER boards.selectDefault so that, when the
			// resolved capability is `unmanaged`, selectDefault has already
			// established focusUmbrellaIdle via enterUnmanagedBase — preventing
			// an unfiltered task sweep at idle (capability-view spec §4).
			p.m.tasks.refresh()
			p.m.boards.loadPins()
			// Status-bar counts and the summary/feed snapshot are
			// project-scoped, so they must follow the switch here — this
			// handler never runs a full refreshAll.
			p.m.refreshStoreStats()
			p.refreshSummary()
			return cmd
		}
	case "a":
		p.openCreateForm()
	case "A":
		p.m.toggleScopedArt()
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
	p.chartDrill = false
	p.detail = detailState{code: code, project: pr}
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
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("created   %s   by %s", core.RFC3339UTC(pr.CreatedAt), pr.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("updated   %s   by %s", core.RFC3339UTC(pr.UpdatedAt), pr.UpdatedBy)))

	b.WriteString("\n")
	b.WriteString(sectionCaption(p.m.styles, p.width, "CAPABILITIES"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, p.renderCapabilitiesLine(pr)))

	p.detail.lines = strings.Split(b.String(), "\n")
	p.clampDetail()
}

// renderCapabilitiesLine renders the read-only "capabilities: [x]/[ ] name
// ..." line for the project detail view. A legacy project (nil
// Capabilities) reads as "all enabled" (Registry.For's own contract), so
// every registered name shows [x] with a trailing "(default)" marker
// distinguishing it from an explicit all-enabled project. Capability
// management (enabling/disabling) lives in the C overlay, not here.
func (p *projectsModel) renderCapabilitiesLine(pr *core.Project) string {
	names := p.m.reg.Names()
	enabled := map[string]bool{}
	explicit := pr.Capabilities != nil
	if explicit {
		for _, n := range pr.Capabilities {
			enabled[n] = true
		}
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		mark := "[ ]"
		if !explicit || enabled[n] {
			mark = "[x]"
		}
		parts = append(parts, fmt.Sprintf("%s %s", mark, n))
	}
	suffix := ""
	if !explicit {
		suffix = "  (default)"
	}
	return "capabilities: " + strings.Join(parts, "  ") + suffix
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
	listH, artH, eventsH, summaryH := projectPaneSplitHeights(p.contentHeight)
	if p.m.projectScope == "" {
		// With no project selected, the feed would render nothing but the
		// same "select a project" placeholder the summary section already
		// shows right below it — a doubled message on the fresh-launch
		// screen. Fold the events rows back into the summary, the same way
		// projectPaneSplitHeights already collapses eventsH to 0 (and grows
		// summaryH to match) when the slot is too small to render at all.
		eventsH, summaryH = 0, summaryH+eventsH
	}
	// Decided once, here, and handed to renderEventsFeed rather than let it
	// infer its own frame: renderList already computes both eventsH and
	// summaryH, so it is the one place that can make the feed and the
	// summary charts agree on which visual language the pane speaks at this
	// height (ATM-793b19 revision-2 review, I1).
	boxed := summaryChartsBoxed(summaryH)
	var parts []string
	if listH > 0 {
		parts = append(parts, padToHeight(p.renderListRows(listH), listH))
	}
	if artH > 0 {
		parts = append(parts, padToHeight(p.renderArt(artH), artH))
	}
	if eventsH > 0 {
		parts = append(parts, padToHeight(p.renderEventsFeed(eventsH, boxed), eventsH))
	}
	if summaryH > 0 {
		parts = append(parts, padToHeight(p.renderSummary(summaryH), summaryH))
	}
	return padToHeight(strings.Join(parts, "\n"), p.contentHeight)
}

// renderArt draws the background art for the pane's scoped project: blank
// when no project is selected or art is off for it. Falls back to blank lines
// (padToHeight in the caller) when the region or project list can't support
// art.
func (p *projectsModel) renderArt(height int) string {
	if len(p.list) == 0 {
		return ""
	}
	code := p.m.projectScope
	if code == "" || !p.m.artOn[code] {
		return ""
	}
	theme := art.EffectivePair(p.m.artPair[code], code)[0]
	lines := art.Render(theme, p.width, height, art.Seed(code), p.m.artPhase,
		p.m.styles.ArtBase, p.m.styles.ArtAccent)
	if lines == nil {
		return ""
	}
	return strings.Join(lines, "\n")
}

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

// listPageSize returns the project rows per page: fixed at 5 (the list
// section is sized for exactly 5 by projectPaneSplitHeights), degrading
// only when the whole pane is shorter than the fixed list section. Shared
// by rendering and the "[" / "]" page jump so both agree on a page.
func (p *projectsModel) listPageSize(maxRows int) int {
	availableRows := maxRows - 4 // caption + header + rule + footer
	if availableRows < 1 {
		availableRows = 1
	}
	if availableRows > 5 {
		availableRows = 5
	}
	return availableRows
}

func (p *projectsModel) renderListRows(maxRows int) string {
	var b strings.Builder
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
		b.WriteString(dashboardFooter(p.width, p.m.styles.Muted.Render("showing 0-0 of 0")))
	} else {
		b.WriteString(dashboardFooter(p.width, p.m.styles.Muted.Render(fmt.Sprintf("showing %d-%d of %d", start+1, end, len(p.list)))))
	}
	return b.String()
}

func (p *projectsModel) renderSummary(height int) string {
	if p.m.projectScope == "" {
		lines := []string{dashboardLine(p.width, p.m.styles.Muted.Render("select a project to see summaries"))}
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	project, tasks, entries, ok := p.projectSummaryData()
	if !ok {
		lines := []string{dashboardLine(p.width, p.m.styles.Muted.Render("selected project could not be loaded"))}
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	_ = project
	_ = tasks

	if height <= 0 {
		return padToHeight("", height)
	}
	return padToHeight(p.renderCombinedActivityChart(entries, height), height)
}

func summaryChartsBoxed(summaryH int) bool {
	return summaryH >= 11
}

func (p *projectsModel) renderCombinedActivityChart(entries []core.LogEntry, height int) string {
	groups := activity.Aggregate(activity.Build(entries), "persona")
	carousel := carouselEntries(groups)
	selected := carouselSelected(carousel, p.chartPersona)
	spec := chartRanges[0]
	if p.chartRange >= 0 && p.chartRange < len(chartRanges) {
		spec = chartRanges[p.chartRange]
	}
	title := "activity"
	if p.chartFocused {
		title = "\u25b8 " + title
	}
	now := core.Now()
	yMax := maxBucketCount(activityBucketCounts(entries, "", spec, now))

	if !summaryChartsBoxed(height) {
		lines := []string{dashboardLine(p.width, title), dashboardLine(p.width, renderCarouselCompact(carousel, selected, p.width, p.m.styles))}
		if len(groups) == 0 {
			lines = append(lines, dashboardLine(p.width, p.m.styles.Muted.Render("no activity yet")))
			lines = append(lines, dashboardLine(p.width, renderRangeLegend(spec, p.width, p.m.styles)))
			return strings.Join(lines, "\n")
		}
		if p.chartDrill {
			drillH := height - len(lines) - 1
			for _, line := range renderInlineActivityDrill(aggregateWindow(entries, selected, spec, now), selected, p.width, drillH, p.m.styles) {
				lines = append(lines, dashboardLine(p.width, line))
			}
			lines = append(lines, dashboardLine(p.width, renderRangeLegend(spec, p.width, p.m.styles)))
			return strings.Join(lines, "\n")
		}
		pulse := renderActivityPulseWithYMax(activityBucketCounts(entries, selected, spec, now), yMax, spec, p.width, height-len(lines)-1, now, p.m.styles.HeaderLabel, p.m.styles.Muted, p.m.styles.Muted)
		if pulse != "" {
			for _, line := range strings.Split(pulse, "\n") {
				lines = append(lines, dashboardLine(p.width, line))
			}
		}
		lines = append(lines, dashboardLine(p.width, renderRangeLegend(spec, p.width, p.m.styles)))
		return strings.Join(lines, "\n")
	}

	innerW := chartBoxInnerWidth(p.width)
	cards := personaCardEntries(entries, groups, spec, now)
	body := renderChartPersonaRows(cards, selected, p.chartFocused, innerW, height, p.m.styles)
	if len(groups) == 0 {
		body = append(body, p.m.styles.Muted.Render("no activity yet"))
	} else if p.chartDrill {
		drillH := height - 3 - len(body)
		if len(body) > 0 && drillH > 0 {
			body = append(body, "")
			drillH--
		}
		for _, line := range renderInlineActivityDrill(aggregateWindow(entries, selected, spec, now), selected, innerW, drillH, p.m.styles) {
			body = append(body, chartBodyLine(line, innerW))
		}
	} else {
		pulseH := height - 3 - len(body)
		if len(body) > 0 && pulseH > 0 {
			body = append(body, "")
			pulseH--
		}
		pulse := renderActivityPulseWithYMax(activityBucketCounts(entries, selected, spec, now), yMax, spec, innerW, pulseH, now, p.m.styles.HeaderLabel, p.m.styles.Muted, p.m.styles.Muted)
		if pulse != "" {
			for _, line := range strings.Split(pulse, "\n") {
				body = append(body, chartBodyLine(line, innerW))
			}
		}
	}
	for i := range body {
		body[i] = chartBodyLine(body[i], innerW)
	}
	body = append(body, chartBodyLine(renderRangeLegend(spec, innerW, p.m.styles), innerW))
	return p.renderChartBoxWithBorder(title, strings.Join(body, "\n"), height, p.m.styles.Muted)
}

func renderChartPersonaRows(cards []personaCardEntry, selected string, focused bool, width, chartHeight int, st Styles) []string {
	if chartHeight >= 16 {
		return renderPersonaCardRows(cards, selected, focused, width, st)
	}
	return renderPersonaMiniCardRows(cards, selected, focused, width, st)
}

func chartBodyLine(line string, width int) string {
	return padDisplay(fitLine(line, width), width)
}

func renderRangeLegend(spec chartRangeSpec, width int, st Styles) string {
	label := spec.label
	if label == "" {
		label = spec.key
	}
	return st.HeaderLabel.Render(fitLine("Range: "+label+"  [Ctrl+\u2191/\u2193]", width))
}

func renderInlineActivityDrill(group activity.Group, key string, width, height int, st Styles) []string {
	if height <= 0 {
		return nil
	}
	header := st.HeaderLabel.Render(fitLine(personaIcon(key)+" "+carouselName(key), width))
	events := activityEventsLabel(group.Count)
	models := inlineBreakdownLine("models", group.Models, width)
	agents := inlineBreakdownLine("agents", group.Agents, width)
	actions := inlineBreakdownLine("actions", group.Actions, width)
	back := st.KeyMenuDim.Render("[Esc] back")
	if height == 1 {
		return []string{back}
	}
	if height == 2 {
		return []string{header, back}
	}
	if height == 3 {
		return []string{header, fitLine(models+" | "+agents+" | "+actions, width), back}
	}
	if height == 4 {
		return []string{header, models, fitLine(agents+" | "+actions, width), back}
	}
	if height == 5 {
		return []string{header, models, agents, actions, back}
	}
	return []string{
		st.HeaderLabel.Render(fitLine(personaIcon(key)+" "+carouselName(key), width)),
		events,
		models,
		agents,
		actions,
		back,
	}
}

func activityEventsLabel(count int) string {
	if count == 1 {
		return "1 event"
	}
	return fmt.Sprintf("%d events", count)
}

func inlineBreakdownLine(caption string, counts map[string]int, width int) string {
	return fitLine(caption+"  "+topCountLabel(counts, 3, "no "+caption), width)
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
	return p.renderChartBoxWithBorder(title, body, maxLines, p.m.styles.Muted)
}

func (p *projectsModel) renderChartBoxWithBorder(title, body string, maxLines int, border lipgloss.Style) string {
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

// refreshSummary rebuilds the render snapshot behind projectSummaryData and
// readEventLog. Called wherever the summary's inputs change: refresh (the
// refreshAll path) and the select/deselect handlers, which bypass refreshAll.
func (p *projectsModel) refreshSummary() {
	p.summaryProject = nil
	p.summaryTasks = nil
	p.summaryEntries = nil
	p.summaryOK = false
	p.feedOK = false
	code := p.m.projectScope
	if code == "" {
		return
	}
	// Feed policy (former readEventLog): a v2 integrity failure still hands
	// back the recoverable prefix, any other read error rejects the feed.
	entries, err := p.m.store.ReadLogCached(code)
	if err == nil || core.IsIntegrity(err) {
		p.summaryEntries = entries
		p.feedOK = true
	}
	// Summary policy (former projectSummaryData): needs the project row, the
	// task list and a tolerable log read.
	project, perr := p.m.store.GetProject(code)
	if perr != nil || !p.feedOK {
		return
	}
	p.summaryProject = project
	p.summaryTasks = p.m.store.ListTasks(core.QueryFilters{Project: code})
	p.summaryOK = true
}

// projectSummaryData hands back the refresh-time snapshot; View must not
// touch the store (see refreshSummary).
func (p *projectsModel) projectSummaryData() (*core.Project, []*core.Task, []core.LogEntry, bool) {
	if !p.summaryOK {
		return nil, nil, nil, false
	}
	return p.summaryProject, p.summaryTasks, p.summaryEntries, true
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

// --- form openers ---

var codeRe = regexp.MustCompile(`^[A-Z]{3,6}$`)

// newProjectCreateForm builds the create-project form without installing it.
// The spotlight preview renders one to show the user what Enter will open.
func newProjectCreateForm(width int) *Form {
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
	f.SetWidth(FormWidth(width))
	return f
}

func (p *projectsModel) openCreateForm() {
	p.m.form = newProjectCreateForm(p.m.width)
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
	f.SetWidth(FormWidth(p.m.width))
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
		if core.IsConflict(err) {
			m.showToast(fmt.Sprintf("4 conflict: code %s exists", code))
		} else {
			m.showToast("error: " + err.Error())
		}
		return nil
	}
	m.projectScope = code
	// R2-3: logsOffset resets on every scope write, matching the other two
	// production scope-write sites (handleListKey's "s" and confirmYes's
	// project removal). A brand-new project has no prior viewport position
	// to strand, so this isn't user-visible today, but the invariant applies
	// uniformly regardless.
	m.projects.logsOffset = 0
	m.projects.resetChart()
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
			// The removed project's viewport position is meaningless for
			// whatever gets selected next.
			m.projects.logsOffset = 0
			m.projects.resetChart()
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
func listTaskIDs(s core.Service, code string) []string {
	ts := s.ListTasks(core.QueryFilters{Project: code})
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}
