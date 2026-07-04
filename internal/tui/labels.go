package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"atm/internal/seed"

	"github.com/charmbracelet/bubbletea"
)

// pluralTasks returns "task"/"tasks" for the given count.
func pluralTasks(n int) string {
	if n == 1 {
		return "task"
	}
	return "tasks"
}

// labelSuffixRe validates the suffix the user types in the label add/remove
// forms. The fixed "<CODE>:" prefix is prepended by the form submit handler,
// so the suffix is "<namespace>:<value>" or "<tag>" with NO leading colon.
var labelSuffixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`)

// openLabelAddForm opens the add-label form bound to the given project code.
// Used by the Labels pane.
func (m *Model) openLabelAddForm(code string) {
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
	}
	f := NewForm(fmt.Sprintf("Add label  %s:", code), fields)
	f.Title = fmt.Sprintf("Add label  %s:", code)
	m.form = f
	m.formKind = formLabelAdd
	m.formPayload = code
}

// openLabelRemoveForm opens the remove-label form bound to the given project code.
func (m *Model) openLabelRemoveForm(code string) {
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
	f := NewForm(fmt.Sprintf("Remove label  %s:", code), fields)
	f.Title = fmt.Sprintf("Remove label  %s:", code)
	m.form = f
	m.formKind = formLabelRemove
	m.formPayload = code
}

// doLabelAdd handles submit of the add-label form.
func (m *Model) doLabelAdd(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	if err := m.store.LabelAdd(full, "", m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	return nil
}

// doLabelRemove handles submit of the remove-label form.
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
	return nil
}

// --- Labels pane model ---

type labelsModel struct {
	m             *Model
	width         int
	contentHeight int
	rows          []labelRow
	cursor        int
	offset        int
	view          lView
	detail        labelDetailState
}

type lView int

const (
	lViewList lView = iota
	lViewDetail
)

type labelRow struct {
	suffix      string
	full        string
	description string
	usage       int
}

type labelDetailState struct {
	row labelRow
}

func newLabelsModel(m *Model) labelsModel {
	return labelsModel{m: m}
}

func (l *labelsModel) SetSize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	l.width = w
	l.contentHeight = h
}

func (l *labelsModel) refresh() {
	l.rows = nil
	if l.m.projectScope == "" {
		return
	}
	ls := l.m.store.LabelList(l.m.projectScope, "")
	for _, lab := range ls {
		usage, _ := l.m.store.LabelUsage(l.m.projectScope, lab.Name)
		suffix := strings.TrimPrefix(lab.Name, l.m.projectScope+":")
		l.rows = append(l.rows, labelRow{
			suffix:      suffix,
			full:        lab.Name,
			description: lab.Description,
			usage:       usage,
		})
	}
	if l.cursor >= len(l.rows) && len(l.rows) > 0 {
		l.cursor = len(l.rows) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

func (l *labelsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch l.view {
	case lViewList:
		return l.handleListKey(k)
	case lViewDetail:
		return l.handleDetailKey(k)
	}
	return nil
}

func (l *labelsModel) handleListKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		if l.cursor < len(l.rows)-1 {
			l.cursor++
		}
	case "k", "up":
		if l.cursor > 0 {
			l.cursor--
		}
	case "g":
		l.cursor = 0
	case "a":
		if l.m.projectScope == "" {
			return nil
		}
		l.m.openLabelAddForm(l.m.projectScope)
	case "d":
		if l.m.projectScope == "" {
			return nil
		}
		l.m.openLabelDescribeForm()
	case "l":
		if l.m.projectScope == "" {
			return nil
		}
		l.m.openLabelRemoveForm(l.m.projectScope)
	case "S":
		if l.m.projectScope == "" {
			return nil
		}
		return l.seedDefaults()
	case "enter":
		if r, ok := l.selected(); ok {
			l.detail = labelDetailState{row: r}
			l.view = lViewDetail
		}
	}
	return nil
}

func (l *labelsModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
	case "k", "up":
	case "d":
		l.m.openLabelDescribeFormFor(l.detail.row.suffix, l.detail.row.description)
	case "l":
		l.m.openLabelRemoveForm(l.m.projectScope)
	case "esc":
		l.view = lViewList
	}
	return nil
}

func (l *labelsModel) selected() (labelRow, bool) {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return labelRow{}, false
	}
	return l.rows[l.cursor], true
}

func (l *labelsModel) seedDefaults() tea.Cmd {
	if err := l.m.store.SeedLabels(l.m.projectScope, l.m.actor); err != nil {
		l.m.showToast("error: " + err.Error())
		return nil
	}
	l.m.showToast(fmt.Sprintf("seeded %d labels into %s", len(seed.Labels), l.m.projectScope))
	l.m.refreshAll()
	return nil
}

func (l *labelsModel) View() string {
	switch l.view {
	case lViewList:
		return l.renderList()
	case lViewDetail:
		return l.renderDetail()
	}
	return ""
}

func (l *labelsModel) renderList() string {
	var b strings.Builder
	if l.m.projectScope == "" {
		lines := []string{
			l.m.styles.EmptyHead.Render("no project selected"),
			"",
			l.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects pane to scope this view", l.m.styles.EmptyKey.Render("[s]"))),
		}
		return padToHeight(centerLinesBoth(lines, l.width, l.contentHeight), l.contentHeight)
	}
	if len(l.rows) == 0 {
		return padToHeight("no labels", l.contentHeight)
	}
	b.WriteString(sectionDivider(l.m.styles, l.width, "Overview"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("project: %s   total labels: %d", l.m.projectScope, len(l.rows))))
	b.WriteString("\n")
	b.WriteString(sectionDivider(l.m.styles, l.width, "Namespaces"))
	b.WriteString("\n")
	// Group by namespace.
	byNS := map[string][]labelRow{}
	var tags []labelRow
	var nsOrder []string
	seenNS := map[string]bool{}
	for _, r := range l.rows {
		parts := strings.SplitN(r.suffix, ":", 2)
		if len(parts) == 2 {
			if !seenNS[parts[0]] {
				seenNS[parts[0]] = true
				nsOrder = append(nsOrder, parts[0])
			}
			byNS[parts[0]] = append(byNS[parts[0]], r)
		} else {
			tags = append(tags, r)
		}
	}
	sort.Strings(nsOrder)
	rowIdx := 0
	for _, ns := range nsOrder {
		b.WriteString(dashboardLine(l.width, l.m.styles.NamespaceHeader.Render(ns+":")))
		b.WriteString("\n")
		for _, r := range byNS[ns] {
			desc := r.description
			if desc == "" {
				desc = l.m.styles.Warning.Render("needs description")
			}
			line := fmt.Sprintf(" %-30s %5d %-5s  %s", r.full, r.usage, pluralTasks(r.usage), desc)
			if rowIdx == l.cursor {
				line = " " + l.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
			} else {
				line = " " + line
			}
			b.WriteString(dashboardLine(l.width, line))
			b.WriteString("\n")
			rowIdx++
		}
	}
	if len(tags) > 0 {
		b.WriteString(dashboardLine(l.width, l.m.styles.NamespaceHeader.Render("tags:")))
		b.WriteString("\n")
		for _, r := range tags {
			desc := r.description
			if desc == "" {
				desc = l.m.styles.Warning.Render("needs description")
			}
			line := fmt.Sprintf(" %-30s %5d %-5s  %s", r.full, r.usage, pluralTasks(r.usage), desc)
			if rowIdx == l.cursor {
				line = " " + l.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
			} else {
				line = " " + line
			}
			b.WriteString(dashboardLine(l.width, line))
			b.WriteString("\n")
			rowIdx++
		}
	}
	return padToHeight(b.String(), l.contentHeight)
}

func (l *labelsModel) renderDetail() string {
	r := l.detail.row
	var b strings.Builder
	fmt.Fprintf(&b, "Label %s\n", r.full)
	b.WriteString(sepLine("─", 78, l.width, 2))
	b.WriteString("\n")
	b.WriteString(sectionDivider(l.m.styles, l.width, "Facts"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("name        %s", r.full)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("usage       %d %s", r.usage, pluralTasks(r.usage))))
	desc := r.description
	if desc == "" {
		desc = l.m.styles.Warning.Render("needs description")
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("description %s", desc)))
	b.WriteString("\n")
	b.WriteString(sectionDivider(l.m.styles, l.width, "Actions"))
	b.WriteString("\n")
	b.WriteString(dashboardLine(l.width, l.m.styles.KeyMenuDim.Render("[d] describe   [l] remove   [Esc] back")))
	return padToHeight(b.String(), l.contentHeight)
}

func (l *labelsModel) statusHint() string {
	if l.m.projectScope == "" {
		return "[?]keys"
	}
	if l.view == lViewDetail {
		return "[d]esc [l]remove [Esc]back"
	}
	return "[a]dd [d]esc [l]remove [S]eed [Enter]detail [?]keys"
}

// --- describe form (used by [d] in list and detail) ---

// openLabelDescribeForm opens a form with name + description fields. The
// user types the label suffix and a new description; submit calls LabelAdd
// (the upsert that overwrites the description).
func (m *Model) openLabelDescribeForm() {
	f := m.newLabelDescribeForm("", "")
	m.form = f
	m.formKind = formLabelDescribe
	m.formPayload = m.projectScope
}

// openLabelDescribeFormFor opens the describe form pre-filled with a known
// suffix and its current description (used from the label detail view).
func (m *Model) openLabelDescribeFormFor(suffix, currentDesc string) {
	f := m.newLabelDescribeForm(suffix, currentDesc)
	m.form = f
	m.formKind = formLabelDescribe
	m.formPayload = m.projectScope
}

func (m *Model) newLabelDescribeForm(suffix, desc string) *Form {
	nameValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Value: suffix, Hint: "<namespace>:<value> or <tag>", Validator: nameValidator},
		{Label: "description", Required: false, Value: desc, Hint: "new description (overwrites)"},
	}
	f := NewForm(fmt.Sprintf("Describe label  %s:", m.projectScope), fields)
	f.Title = fmt.Sprintf("Describe label  %s:", m.projectScope)
	return f
}

// doLabelDescribe handles submit of the describe-label form.
func (m *Model) doLabelDescribe(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	desc := vals["description"]
	if err := m.store.LabelAdd(full, desc, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	return nil
}
