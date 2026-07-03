package tui

import (
	"fmt"
	"regexp"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

// projectsModel owns the Projects tab state: list, detail, cursor, selection.
type projectsModel struct {
	m     *Model
	list  []projRow
	view  pView
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
	updatedT int64   // unix for sort (unused; store pre-sorts by code)
}

type detailState struct {
	code     string
	project  *store.Project
	lines    []string // rendered detail lines (for scroll)
	offset   int
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
	case "L":
		p.openLabelAddForm()
	case "l":
		p.openLabelRemoveForm()
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
	b.WriteString("PROJECT\n")
	b.WriteString(sepLine("─", 78, p.m.width, 2))
	b.WriteString("\n")
	fmt.Fprintf(&b, "code      %s\n", pr.Code)
	fmt.Fprintf(&b, "name      %s                                  [N] set name\n", pr.Name)
	fmt.Fprintf(&b, "tasks     %d\n", len(listTaskIDs(p.m.store, pr.Code)))
	fmt.Fprintf(&b, "labels    %d\n", len(p.m.store.LabelList(pr.Code, "")))
	fmt.Fprintf(&b, "created   %s   by %s\n", store.RFC3339UTC(pr.CreatedAt), pr.CreatedBy)
	fmt.Fprintf(&b, "updated   %s   by %s\n", store.RFC3339UTC(pr.UpdatedAt), pr.UpdatedBy)
	b.WriteString("\n")

	// LABELS grouped by namespace with usage counts.
	b.WriteString("LABELS\n")
	b.WriteString(sepLine("─", 78, p.m.width, 2))
	b.WriteString("\n")
	ls := p.m.store.LabelList(pr.Code, "")
	namespaces := p.m.store.Namespaces(pr.Code)
	// Group: namespaced first (alphabetical), then unnamespaced tags.
	byNS := map[string][]store.Label{}
	var tags []store.Label
	for _, l := range ls {
		rest := strings.TrimPrefix(l.Name, pr.Code+":")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			byNS[parts[0]] = append(byNS[parts[0]], l)
		} else {
			tags = append(tags, l)
		}
	}
	for _, ns := range namespaces {
		fmt.Fprintf(&b, "%s:\n", ns)
		for _, l := range byNS[ns] {
			count, _ := p.m.store.LabelUsage(pr.Code, l.Name)
			fmt.Fprintf(&b, "   %-45s (%d %s)\n", l.Name, count, pluralTasks(count))
			if l.Description != "" {
				fmt.Fprintf(&b, "       %s\n", l.Description)
			}
		}
		b.WriteString("\n")
	}
	if len(tags) > 0 {
		b.WriteString("tags:\n")
		for _, l := range tags {
			count, _ := p.m.store.LabelUsage(pr.Code, l.Name)
			fmt.Fprintf(&b, "   %-45s (%d %s)\n", l.Name, count, pluralTasks(count))
		}
	}

	if p.detail.historyOn {
		b.WriteString("\n")
		b.WriteString("HISTORY\n")
		b.WriteString(sepLine("─", 78, p.m.width, 2))
		b.WriteString("\n")
		for _, h := range pr.History {
			fmt.Fprintf(&b, " %-3s %s   %s     %s\n", h.ID, store.RFC3339UTC(h.At), h.Actor, h.Action)
			if len(h.Meta) > 0 {
				fmt.Fprintf(&b, "      meta: %s\n", metaJSON(h.Meta))
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
	// Header.
	b.WriteString(headerLabelStyle.Render(fmt.Sprintf("%-6s %-30s %6s %7s %10s", "CODE", "NAME", "TASKS", "LABELS", "UPDATED")))
	b.WriteString("\n")
	b.WriteString(sepLine("─", 78, p.m.width, 2))
	b.WriteString("\n")
	for i, r := range p.list {
		var gutter string
		if r.code == p.m.projectScope {
			gutter = gutterSelectStyle.Render("▸")
		} else {
			gutter = " "
		}
		// build cell line
		line := fmt.Sprintf(" %-5s %-30s %6d %7d %10s", r.code, truncateRunes(r.name, 30), r.tasks, r.labels, r.updated)
		if i == p.cursor {
			line = gutter + " " + rowCursorStyle.Render(line)
		} else {
			line = gutter + " " + line
		}
		b.WriteString(line)
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
		emptyHeadStyle.Render("no projects"),
		"",
		emptyTextStyle.Render(fmt.Sprintf("press %s to add a project, then seed", emptyKeyStyle.Render("[a]"))),
		emptyDimStyle.Render("index tasks (start-here, repo:, doc:)"),
		emptyDimStyle.Render("and label as you go"),
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
		return "[N]name [L]add label [l]remove label [H]history [x]remove [Esc]back"
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

// labelSuffixRe validates the suffix the user types in the label add/remove
// forms. The fixed "<CODE>:" prefix is prepended by the form submit handler
// (doLabelAdd/doLabelRemove build full = code + ":" + suffix), so the suffix
// itself is "<namespace>:<value>" or "<tag>" with NO leading colon. The mockup
// spec's full-label regex is ^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$; removing
// the leading "<CODE>:" yields this suffix regex.
var labelSuffixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`)

func (p *projectsModel) openLabelAddForm() {
	pr := p.detail.project
	if pr == nil {
		return
	}
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>, e.g. status:open")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>, e.g. status:open", Validator: validator},
		{Label: "desc", Required: false, Hint: "optional; preserved if already set"},
	}
	f := NewForm(fmt.Sprintf("Add label  %s:", pr.Code), fields)
	f.Title = fmt.Sprintf("Add label  %s:", pr.Code)
	p.m.form = f
	p.m.formKind = formLabelAdd
	p.m.formPayload = pr.Code
}

func (p *projectsModel) openLabelRemoveForm() {
	pr := p.detail.project
	if pr == nil {
		return
	}
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm(fmt.Sprintf("Remove label  %s:", pr.Code), fields)
	f.Title = fmt.Sprintf("Remove label  %s:", pr.Code)
	p.m.form = f
	p.m.formKind = formLabelRemove
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

func (m *Model) doLabelAdd(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	desc := vals["desc"]
	if err := m.store.LabelAdd(full, desc, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.projects.openDetail(code)
	return nil
}

func (m *Model) doLabelRemove(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	res, err := m.store.LabelRemove(full, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.showToast(fmt.Sprintf("removed label %s (retained usage: %d)", full, res.RetainedUsage))
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

func pluralTasks(n int) string {
	if n == 1 {
		return "task"
	}
	return "tasks"
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