package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"atm/internal/activity"
	"atm/internal/core"
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

	// capCursor indexes into the registry's Names() for the [c]/[space]
	// capability cursor on the project detail view.
	capCursor int

	// history toggle on project detail.
	showHistory bool

	// logsOffset is the Recent Events feed's viewport offset (list view,
	// ATM-793b19 revision 2): shift+arrows drive it directly, with no
	// subfocus mode — see events_feed.go's scrollEventsFeed. It indexes the
	// newest-first feed; render clamps it into a local copy only (View must
	// stay pure), so out-of-range values persist harmlessly until the next
	// key handler that moves it.
	logsOffset int

	// personaFocus is the inline "activity by persona" navigation mode
	// (replaces the former P overlay). P toggles it; ctrl+up/down moves the
	// persona cursor; ctrl+right drills into the hovered persona's
	// agents/models/actions breakdown; ctrl+left/Esc returns; D dispatches.
	personaFocus   bool
	personaCursor  int
	personaDrilled bool
	personaGroups  []activity.Group
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
	project   *core.Project
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

// projectPaneSplitHeights allocates the list view's vertical space three
// ways: project list ~30%, recent-events feed ~35%, summary the rest. An
// events slot under 4 rows is not worth rendering: 4 is the minimum that
// still leaves 2 content lines under 2 lines of frame — top/bottom border in
// the boxed form, the caption line plus a padding line in the compact form
// (see summaryChartsBoxed and renderEventsFeed) — below that it collapses to
// 0 and the pre-feed 30/70 list/summary split is restored.
func projectPaneSplitHeights(total int) (int, int, int) {
	if total <= 0 {
		return 0, 0, 0
	}
	if total == 1 {
		return 1, 0, 0
	}
	listH := total * 30 / 100
	if listH < 1 {
		listH = 1
	}
	eventsH := total * 35 / 100
	if eventsH < 4 {
		eventsH = 0
	}
	summaryH := total - listH - eventsH
	if summaryH < 1 {
		summaryH = 1
		listH = total - summaryH - eventsH
		if listH < 1 {
			listH = 1
			eventsH = 0
			summaryH = total - listH
		}
	}
	return listH, eventsH, summaryH
}

func computeStripDays(width int) int {
	const gap = 1
	const maxDays = 14
	const minDays = 7
	const minCellW = 9 // widest label "Yesterday"
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

func activityStripeDayCounts(entries []core.LogEntry, days int) []activityStripeDay {
	return activityStripeDayCountsEnding(entries, days, core.Now())
}

func activityStripeDayCountsEnding(entries []core.LogEntry, days int, end time.Time) []activityStripeDay {
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
}

func (p *projectsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "P":
		p.togglePersonaFocus()
		return nil
	}
	// Persona-focus navigation takes precedence over list/detail keys.
	if p.personaFocus {
		return p.handlePersonaFocusKey(k)
	}
	switch p.view {
	case pViewList:
		return p.handleListKey(k)
	case pViewDetail:
		return p.handleDetailKey(k)
	}
	return nil
}

// togglePersonaFocus activates/deactivates the inline persona-activity
// navigation mode (the former P overlay, now driven by ctrl+arrows). It
// reloads the persona groups from the current project's event log and
// clamps the cursor.
func (p *projectsModel) togglePersonaFocus() {
	if p.m.projectScope == "" {
		p.m.showToast("select a project first")
		return
	}
	if p.personaFocus {
		p.personaFocus, p.personaDrilled = false, false
		return
	}
	p.refreshPersonaGroups()
	p.personaFocus = true
	p.personaDrilled = false
}

func (p *projectsModel) refreshPersonaGroups() {
	p.personaGroups = nil
	p.personaCursor = 0
	code := p.m.projectScope
	if code == "" {
		return
	}
	entries, err := p.m.store.ReadLogCached(code)
	if err != nil {
		return
	}
	p.personaGroups = activity.Aggregate(activity.Build(entries), "persona")
}

// handlePersonaFocusKey routes the inline persona-activity navigation keys.
// ctrl+up/down move the cursor; ctrl+right drills in; ctrl+left/Esc back out;
// D dispatches the hovered persona.
func (p *projectsModel) handlePersonaFocusKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+up", "k":
		if p.personaCursor > 0 {
			p.personaCursor--
		}
	case "ctrl+down", "j":
		if p.personaCursor < len(p.personaGroups)-1 {
			p.personaCursor++
		}
	case "ctrl+right":
		if p.personaCursor < len(p.personaGroups) {
			p.personaDrilled = true
		}
	case "ctrl+left", "esc":
		if p.personaDrilled {
			p.personaDrilled = false
		} else {
			p.personaFocus = false
		}
	case "d":
		if p.personaDrilled && p.personaCursor < len(p.personaGroups) {
			return p.openDispatchForPersona(p.personaGroups[p.personaCursor].Key)
		}
	case "P":
		p.personaFocus, p.personaDrilled = false, false
	}
	return nil
}

// openDispatchForPersona maps a persona name to its dispatch kind and opens
// the dispatch dialog over the current project scope (empty for
// project-optional personas). Falls back to manager for unknown personas.
func (p *projectsModel) openDispatchForPersona(persona string) tea.Cmd {
	m := p.m
	project := m.projectScope
	switch persona {
	case "developer":
		m.dispatchDlg.open(dispatchDeveloper, project, "", "")
	case "concierge":
		m.dispatchDlg.open(dispatchConcierge, "", "", "")
	case "admin":
		m.dispatchDlg.open(dispatchAdmin, "", "", "")
	default:
		m.dispatchDlg.open(dispatchManager, project, "", "")
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
		listH, _, _ := projectPaneSplitHeights(p.contentHeight)
		p.cursor += p.listPageSize(listH)
		if p.cursor > len(p.list)-1 {
			p.cursor = len(p.list) - 1
		}
		if p.cursor < 0 {
			p.cursor = 0
		}
	case "[":
		listH, _, _ := projectPaneSplitHeights(p.contentHeight)
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
			// Status-bar counts are project-scoped, so they must follow the
			// switch here — this handler never runs a full refreshAll.
			p.m.refreshStoreStats()
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
	case "c":
		names := p.m.reg.Names()
		if len(names) > 0 {
			p.capCursor = (p.capCursor + 1) % len(names)
			p.renderDetail()
		}
	case " ":
		p.toggleCapability()
	}
	return nil
}

// toggleCapability flips the enabled state of the capability under the
// detail view's capability cursor (set by the "c" key). A legacy (nil
// Capabilities) project reads as "all enabled" per Registry.For; disabling
// one of its capabilities must first make that reading EXPLICIT — before the
// Disable call, every OTHER registered name is enabled so the stored set
// becomes "all but this one", matching what the (default) view already
// implied. Errors are swallowed (mirrors the plan's other detail mutations,
// e.g. requestRemoveProject's guard toast pattern is the exception, not the
// rule) since a failed toggle simply leaves the view unchanged on refresh.
func (p *projectsModel) toggleCapability() {
	names := p.m.reg.Names()
	if len(names) == 0 {
		return
	}
	name := names[p.capCursor%len(names)]
	code := p.detail.code
	proj, err := p.m.store.GetProject(code)
	if err != nil {
		return
	}
	isEnabled := proj.Capabilities == nil // legacy: everything enabled
	for _, n := range proj.Capabilities {
		if n == name {
			isEnabled = true
		}
	}
	if isEnabled {
		if proj.Capabilities == nil {
			for _, n := range names {
				if n != name {
					_ = p.m.store.EnableProjectCapability(code, n, p.m.actor)
				}
			}
		}
		_ = p.m.store.DisableProjectCapability(code, name, p.m.actor)
	} else {
		_ = p.m.store.EnableProjectCapability(code, name, p.m.actor)
	}
	// Refresh the detail view's cached project + rendered lines so the
	// toggle is visible immediately (mirrors the H history-toggle refresh
	// above: mutate, then re-render from the freshly read project).
	if pr, err := p.m.store.GetProject(code); err == nil {
		p.detail.project = pr
	}
	p.renderDetail()
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
	p.capCursor = 0
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

	if p.detail.historyOn {
		b.WriteString("\n")
		b.WriteString(sectionCaption(p.m.styles, p.width, "HISTORY"))
		b.WriteString("\n")
		hv := p.m.store.History(p.detail.code, core.Subject{Kind: "project", Code: p.detail.code})
		if len(hv) == 0 {
			b.WriteString(dashboardLine(p.width, " (no history)"))
			b.WriteString("\n")
		} else {
			for _, e := range hv {
				fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("[%d] %s %s %s", e.Seq, core.RFC3339UTC(e.At), e.Actor, e.Action)))
			}
		}
	}

	p.detail.lines = strings.Split(b.String(), "\n")
	p.clampDetail()
}

// renderCapabilitiesLine renders the "capabilities: [x]/[ ] name ..." line
// for the project detail view. A legacy project (nil Capabilities) reads as
// "all enabled" (Registry.For's own contract), so every registered name
// shows [x] with a trailing "(default)" marker distinguishing it from an
// explicit all-enabled project. The name under the capability cursor (set by
// the "c" key, toggled by " ") is highlighted with the same RowCursor style
// the list view uses for its cursor row.
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
	for i, n := range names {
		mark := "[ ]"
		if !explicit || enabled[n] {
			mark = "[x]"
		}
		cell := fmt.Sprintf("%s %s", mark, n)
		if i == p.capCursor {
			cell = p.m.styles.RowCursor.Render(cell)
		}
		parts = append(parts, cell)
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
	listH, eventsH, summaryH := projectPaneSplitHeights(p.contentHeight)
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
	if eventsH > 0 {
		parts = append(parts, padToHeight(p.renderEventsFeed(eventsH, boxed), eventsH))
	}
	if summaryH > 0 {
		parts = append(parts, padToHeight(p.renderSummary(summaryH), summaryH))
	}
	return padToHeight(strings.Join(parts, "\n"), p.contentHeight)
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

// listPageSize returns the number of project rows that fit in the list
// section at the given section height, after the header/rule/footer
// overhead (header + rule + footer divider + footer line = 4). Shared by
// rendering (the visible window) and the "[" / "]" page jump so both agree
// on what a "page" is.
func (p *projectsModel) listPageSize(maxRows int) int {
	availableRows := maxRows - 4 // header + rule + footer divider + footer
	if availableRows < 1 {
		availableRows = 1
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

	remaining := height
	if remaining <= 0 {
		return padToHeight("", height)
	}

	var lines []string
	if remaining >= 6 {
		actorH, stripeH := chartBoxHeights(remaining)
		lines = append(lines, p.renderPersonaActivityChart(entries, actorH)...)
		lines = append(lines, strings.Split(p.renderChartBox("activity stripe", p.renderActivityStripeChart(entries, stripeH-2), stripeH), "\n")...)
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

func chartBoxHeights(total int) (int, int) {
	if total < 6 {
		return total, 0
	}
	actor := total / 2
	stripe := total - actor
	if actor < 3 {
		actor = 3
	}
	if stripe < 3 {
		stripe = 3
	}
	return actor, stripe
}

// summaryChartsBoxed reports whether renderSummary will draw its persona
// chart as a bordered box at the given summary-section height, without
// duplicating renderSummary's own rendering: it repeats only the arithmetic
// that decides the frame — no fixed header lines above the charts now (the
// "Project Summary" heading was removed), so the full summaryH is available,
// then renderPersonaActivityChart's own boxed-from-4-lines-up rule applied
// to the actor split chartBoxHeights returns. renderEventsFeed keys its own
// compact-vs-boxed choice off this (ATM-793b19 revision-2 review, I1) so the
// feed and the persona chart never disagree about which visual language the
// pane is speaking, even though the activity stripe chart below it can still
// box on its own at a smaller height than the persona chart does — that
// pre-existing wrinkle inside the summary section is unchanged here; this
// keys off the persona chart specifically because that is the section the
// feed is being compared against.
func summaryChartsBoxed(summaryH int) bool {
	remaining := summaryH
	if remaining < 6 {
		return false
	}
	actorH, _ := chartBoxHeights(remaining)
	return actorH >= 4
}

func (p *projectsModel) renderPersonaActivityChart(entries []core.LogEntry, maxLines int) []string {
	if maxLines <= 0 {
		return nil
	}
	// Drilled-in detail view: render the hovered persona's breakdown inside
	// the chart box with a bold centered dispatch action at the top.
	if p.personaFocus && p.personaDrilled && p.personaCursor < len(p.personaGroups) {
		return p.renderPersonaDetailChart(p.personaGroups[p.personaCursor], maxLines)
	}

	// 1 line genuinely cannot fit a bar row alongside a label, so the
	// expand hint is the only legible content there. 2-3 lines render a
	// compact title + bar rows (no border). From 4 up the bordered chart
	// box takes over (its title lives in the top border, so we do not
	// prepend one ourselves).
	hint := "activity by persona  [P]focus"
	if p.personaFocus {
		hint = "activity by persona  [ctrl+↑/↓]move [ctrl+→]drill [P]close"
	}
	if maxLines == 1 {
		return []string{dashboardLine(p.width, hint)}
	}
	groups := activity.Aggregate(activity.Build(entries), "persona")
	if p.personaFocus {
		groups = p.personaGroups
	}
	if len(groups) == 0 {
		body := p.m.styles.Muted.Render("no activity yet")
		if maxLines < 4 {
			return []string{
				dashboardLine(p.width, hint),
				dashboardLine(p.width, body),
			}[:maxLines]
		}
		return strings.Split(p.renderChartBox(hint, body, maxLines), "\n")
	}
	nameW := longestPersonaKeyWidth(groups)
	meterW := chartBoxInnerWidth(p.width) - nameW - 12
	if meterW < 10 {
		meterW = 10
	}
	total := 0
	for _, g := range groups {
		total += g.Count
	}
	barRow := func(g activity.Group, idx int) string {
		percent := 0
		if total > 0 {
			percent = (g.Count*100 + total/2) / total
		}
		marker := " "
		if p.personaFocus && idx == p.personaCursor {
			marker = "▸"
		}
		return fmt.Sprintf("%s%-*s %s %3d%% %4d", marker, nameW, g.Key, meterBar(percent, meterW), percent, g.Count)
	}
	if maxLines < 4 {
		// Compact: title row + as many bar rows as fit.
		rows := []string{dashboardLine(p.width, hint)}
		cap := maxLines - 1
		if len(groups) > cap {
			groups = groups[:cap]
		}
		for i, g := range groups {
			line := barRow(g, i)
			if p.personaFocus && i == p.personaCursor {
				line = p.m.styles.RowCursor.Render(line)
			}
			rows = append(rows, dashboardLine(p.width, line))
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
	for i, g := range groups {
		line := barRow(g, i)
		if p.personaFocus && i == p.personaCursor {
			line = p.m.styles.RowCursor.Render(line)
		}
		body = append(body, line)
	}
	return strings.Split(p.renderChartBox(hint, strings.Join(body, "\n"), maxLines), "\n")
}

// renderPersonaDetailChart renders the drilled-in persona breakdown inside
// the chart box. A bold centered dispatch action sits at the top of the box
// body; below it the agents/models/actions breakdown bars render as in the
// former overlay detail view.
func (p *projectsModel) renderPersonaDetailChart(g activity.Group, maxLines int) []string {
	hint := fmt.Sprintf("persona: %s  [ctrl+←]back [D]dispatch", g.Key)
	if maxLines < 4 {
		// Too short to box: just the hint.
		return []string{dashboardLine(p.width, hint)}[:maxLines]
	}
	// Shared label width across all three breakdowns so bars align.
	nameW := 0
	for _, counts := range []map[string]int{g.Agents, g.Models, g.Actions} {
		for k := range counts {
			if w := lipgloss.Width(k); w > nameW {
				nameW = w
			}
		}
	}
	innerW := chartBoxInnerWidth(p.width)
	meterW := innerW - nameW - 8
	if meterW < 8 {
		meterW = 8
	}
	// Bold centered dispatch action at the top of the box body.
	action := fmt.Sprintf("[D] dispatch %s", g.Key)
	bodyLines := []string{centerLine(action, innerW, p.m.styles.HeaderLabel), ""}
	writeBreakdownLine := func(title string, counts map[string]int) []string {
		var out []string
		out = append(out, p.m.styles.Muted.Render(title))
		if len(counts) == 0 {
			out = append(out, "  (none)")
			return out
		}
		var rows []kvRow
		t := 0
		for k, v := range counts {
			rows = append(rows, kvRow{k, v})
			t += v
		}
		sortKV(rows)
		for _, r := range rows {
			percent := 0
			if t > 0 {
				percent = (r.v*100 + t/2) / t
			}
			out = append(out, fmt.Sprintf("  %-*s %s %4d", nameW, r.k, meterBar(percent, meterW), r.v))
		}
		return out
	}
	bodyLines = append(bodyLines, writeBreakdownLine("agents", g.Agents)...)
	bodyLines = append(bodyLines, writeBreakdownLine("models", g.Models)...)
	bodyLines = append(bodyLines, writeBreakdownLine("actions", g.Actions)...)
	// Cap to the box's inner height (maxLines - 2).
	cap := maxLines - 2
	if len(bodyLines) > cap {
		bodyLines = bodyLines[:cap]
	}
	return strings.Split(p.renderChartBox(hint, strings.Join(bodyLines, "\n"), maxLines), "\n")
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

func (p *projectsModel) renderActivityStripeChart(entries []core.LogEntry, bodyHeight int) string {
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
	cellW := (width - (len(days)-1)*gap) / len(days)
	if cellW < 1 {
		cellW = 1
	}
	canvasW := cellW*len(days) + (len(days)-1)*gap

	maxCount := 0
	for _, day := range days {
		if day.count > maxCount {
			maxCount = day.count
		}
	}

	c := canvas.New(canvasW, height)
	for i, day := range days {
		x0 := i * (cellW + gap)
		barH := bodyH
		if maxCount > 0 {
			if day.count > 0 {
				barH = day.count * bodyH / maxCount
				if barH < 1 {
					barH = 1
				}
			} else {
				barH = 1
			}
		}
		fill := densityFillRune(day.count)
		style := activityCanvasStyle(day.count)
		emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
		for col := 0; col < cellW; col++ {
			for row := 0; row < bodyH; row++ {
				if row >= bodyH-barH {
					c.SetRuneWithStyle(canvas.Point{X: x0 + col, Y: row}, fill, style)
				} else {
					c.SetRuneWithStyle(canvas.Point{X: x0 + col, Y: row}, '·', emptyStyle)
				}
			}
		}
	}
	axis := activityStripeAxis(days, canvasW, cellW, gap)
	c.SetStringWithStyle(canvas.Point{X: 0, Y: height - 1}, axis, lipgloss.NewStyle().Foreground(lipgloss.Color("244")))
	return c.View()
}

func activityStripeAxis(days []activityStripeDay, width, cellW, gap int) string {
	if len(days) == 0 || width <= 0 {
		return ""
	}
	n := len(days)
	line := []rune(repeat(" ", width))
	putLabel := func(label string, colIdx int) {
		labelRunes := []rune(label)
		colStart := colIdx * (cellW + gap)
		pos := colStart + (cellW-len(labelRunes))/2
		if pos < 0 {
			pos = 0
		}
		for i, r := range labelRunes {
			if pos+i < len(line) {
				line[pos+i] = r
			}
		}
	}
	if n >= 14 {
		putLabel("14d ago", 0)
		putLabel("7d ago", n-8)
	} else {
		putLabel("7d ago", 0)
	}
	if n >= 2 {
		putLabel("Yesterday", n-2)
		putLabel("Today", n-1)
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

func densityFillRune(count int) rune {
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

func (p *projectsModel) projectSummaryData() (*core.Project, []*core.Task, []core.LogEntry, bool) {
	code := p.m.projectScope
	if code == "" {
		return nil, nil, nil, false
	}
	project, err := p.m.store.GetProject(code)
	if err != nil {
		return nil, nil, nil, false
	}
	tasks := p.m.store.ListTasks(core.QueryFilters{Project: code})
	entries, err := p.m.store.ReadLogCached(code)
	if err != nil && !core.IsIntegrity(err) {
		return nil, nil, nil, false
	}
	return project, tasks, entries, true
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
	if p.personaFocus {
		if p.personaDrilled {
			return "[D]dispatch [Ctrl+←]back [P]close"
		}
		return "[Ctrl+↑/↓]move [Ctrl+→]drill [P]close"
	}
	switch p.view {
	case pViewList:
		if len(p.list) == 0 {
			return "[a]dd [P]ersona focus"
		}
		return "[a]dd [s]elect [Enter]detail [x]remove [P]ersona focus"
	case pViewDetail:
		return "[N]ame [H]istory [c]apability [space]toggle [x]remove [P]ersona focus [Esc]back"
	}
	return ""
}

// --- form openers ---

var codeRe = regexp.MustCompile(`^[A-Z]{3,6}$`)

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
