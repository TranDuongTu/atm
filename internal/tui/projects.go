package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
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

type namespaceCount struct {
	namespace string
	count     int
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
		if tk == nil {
			continue
		}
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
	case "enter", "e":
		if r, ok := p.selected(); ok {
			p.openDetail(r.code)
		}
	case "s":
		if r, ok := p.selected(); ok {
			p.m.projectScope = r.code
			p.m.tasks.refresh()
			p.m.labels.refresh()
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
	b.WriteString(sectionDivider(p.m.styles, p.width, "Facts"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("code      %s", pr.Code)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("name      %s", pr.Name)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("tasks     %d", len(listTaskIDs(p.m.store, pr.Code)))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("labels    %d", len(p.m.store.LabelList(pr.Code, "")))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("created   %s   by %s", store.RFC3339UTC(pr.CreatedAt), pr.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("updated   %s   by %s", store.RFC3339UTC(pr.UpdatedAt), pr.UpdatedBy)))
	b.WriteString("\n")
	b.WriteString(sectionDivider(p.m.styles, p.width, "Actions"))
	b.WriteString("\n")
	b.WriteString(dashboardLine(p.width, p.m.styles.KeyMenuDim.Render("[N] set name   [H] history   [x] remove   [Esc] back")))
	b.WriteString("\n")

	if p.detail.historyOn {
		b.WriteString("\n")
		b.WriteString(sectionDivider(p.m.styles, p.width, "History"))
		b.WriteString("\n")
		for _, h := range pr.History {
			fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf(" %-3s %s   %s     %s", h.ID, store.RFC3339UTC(h.At), h.Actor, h.Action)))
			if len(h.Meta) > 0 {
				fmt.Fprintf(&b, "%s\n", dashboardLine(p.width, fmt.Sprintf("      meta: %s", metaJSON(h.Meta))))
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
	overflow := len(p.list) > availableRows
	if overflow && availableRows > 0 {
		availableRows--
	}
	for i, r := range p.list {
		if i >= availableRows {
			remaining := len(p.list) - i
			if remaining > 0 && overflow && availableRows > 0 {
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
			return "[a]add [?]keys"
		}
		return "[a]dd [s]elect [Enter]detail [x]remove [?]keys"
	case pViewDetail:
		return "[N]name [H]history [x]remove [Esc]back"
	}
	return "[?]keys"
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
