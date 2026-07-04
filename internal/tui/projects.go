package tui

import (
	"fmt"
	"regexp"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

// projectsModel owns the Projects pane state: list, detail, cursor, selection.
type projectsModel struct {
	m      *Model
	list   []projRow
	view   pView
	cursor int
	detail detailState

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

func newProjectsModel(m *Model) projectsModel {
	return projectsModel{m: m}
}

func (p *projectsModel) SetSize(w, h int) {
	_ = w
	// detail scroll height
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
	b.WriteString(sepLine("─", 78, p.m.width, 2))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", p.m.styles.Muted.Render(pr.Name))
	b.WriteString("\n")
	b.WriteString(sectionDivider(p.m.styles, p.m.width, "Facts"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.m.width, fmt.Sprintf("code      %s", pr.Code)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.m.width, fmt.Sprintf("name      %s", pr.Name)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.m.width, fmt.Sprintf("tasks     %d", len(listTaskIDs(p.m.store, pr.Code)))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.m.width, fmt.Sprintf("labels    %d", len(p.m.store.LabelList(pr.Code, "")))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.m.width, fmt.Sprintf("created   %s   by %s", store.RFC3339UTC(pr.CreatedAt), pr.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.m.width, fmt.Sprintf("updated   %s   by %s", store.RFC3339UTC(pr.UpdatedAt), pr.UpdatedBy)))
	b.WriteString("\n")
	b.WriteString(sectionDivider(p.m.styles, p.m.width, "Actions"))
	b.WriteString("\n")
	b.WriteString(dashboardLine(p.m.width, p.m.styles.KeyMenuDim.Render("[N] set name   [H] history   [x] remove   [Esc] back")))
	b.WriteString("\n")

	if p.detail.historyOn {
		b.WriteString("\n")
		b.WriteString(sectionDivider(p.m.styles, p.m.width, "History"))
		b.WriteString("\n")
		for _, h := range pr.History {
			fmt.Fprintf(&b, "%s\n", dashboardLine(p.m.width, fmt.Sprintf(" %-3s %s   %s     %s", h.ID, store.RFC3339UTC(h.At), h.Actor, h.Action)))
			if len(h.Meta) > 0 {
				fmt.Fprintf(&b, "%s\n", dashboardLine(p.m.width, fmt.Sprintf("      meta: %s", metaJSON(h.Meta))))
			}
		}
	}

	p.detail.lines = strings.Split(b.String(), "\n")
	p.clampDetail()
}

func (p *projectsModel) clampDetail() {
	maxOff := len(p.detail.lines) - p.m.contentHeight
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
	var b strings.Builder
	if len(p.list) == 0 {
		return p.renderEmpty()
	}
	selected := p.m.projectScope
	if selected == "" {
		selected = "none"
	}
	b.WriteString(sectionDivider(p.m.styles, p.m.width, "Overview"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(p.m.width, fmt.Sprintf("total projects: %d   selected: %s", len(p.list), selected)))
	b.WriteString("\n")
	b.WriteString(dashboardLine(p.m.width, p.m.styles.HeaderLabel.Render(fmt.Sprintf("%-6s %-30s %6s %7s %10s", "CODE", "NAME", "TASKS", "LABELS", "UPDATED"))))
	b.WriteString("\n")
	b.WriteString(dashboardLine(p.m.width, repeat("─", dashboardContentWidth(p.m.width))))
	b.WriteString("\n")
	for i, r := range p.list {
		var gutter string
		if r.code == p.m.projectScope {
			gutter = p.m.styles.GutterSelect.Render("▸")
		} else {
			gutter = " "
		}
		// build cell line
		line := fmt.Sprintf(" %-5s %-30s %6d %7d %10s", r.code, truncateRunes(r.name, 30), r.tasks, r.labels, r.updated)
		if i == p.cursor {
			line = gutter + " " + p.m.styles.RowCursor.Render(line)
		} else {
			line = gutter + " " + line
		}
		b.WriteString(dashboardLine(p.m.width, line))
		b.WriteString("\n")
	}
	return padToHeight(b.String(), p.m.contentHeight)
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
	return padToHeight(centerLinesBoth(lines, p.m.width, p.m.contentHeight), p.m.contentHeight)
}

func (p *projectsModel) renderDetailView() string {
	end := p.detail.offset + p.m.contentHeight
	if end > len(p.detail.lines) {
		end = len(p.detail.lines)
	}
	var b strings.Builder
	for i := p.detail.offset; i < end; i++ {
		b.WriteString(p.detail.lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), p.m.contentHeight)
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
