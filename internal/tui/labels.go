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
	entries       []labelEntry
	chartNS       string // active "tasks by label" chart namespace ("" = list view)
	cursor        int
	offset        int
	pageSize      int
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

type labelEntryKind int

const (
	entryHeaderNS labelEntryKind = iota
	entryHeaderTags
	entryRow
)

// labelEntry is one navigable line in the Labels list: a namespace header, the
// tags header, or a label row. The cursor indexes the entries slice so headers
// are selectable (Enter on a namespace header facets the Tasks pane).
type labelEntry struct {
	kind labelEntryKind
	ns   string   // set for entryHeaderNS
	row  labelRow // set for entryRow
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
	l.pageSize = h - 3 // caption + blank + footer
	if l.pageSize < 1 {
		l.pageSize = 1
	}
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
	l.rebuildEntries()
	if l.cursor >= len(l.entries) && len(l.entries) > 0 {
		l.cursor = len(l.entries) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// rebuildEntries flattens l.rows into the navigable entries list: namespace
// headers (alphabetical) each followed by their rows, then a tags header with
// unnamespaced rows. Mirrors the grouping renderList uses.
func (l *labelsModel) rebuildEntries() {
	l.entries = nil
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
	for _, ns := range nsOrder {
		l.entries = append(l.entries, labelEntry{kind: entryHeaderNS, ns: ns})
		for _, r := range byNS[ns] {
			l.entries = append(l.entries, labelEntry{kind: entryRow, row: r})
		}
	}
	if len(tags) > 0 {
		l.entries = append(l.entries, labelEntry{kind: entryHeaderTags})
		for _, r := range tags {
			l.entries = append(l.entries, labelEntry{kind: entryRow, row: r})
		}
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
	if l.activeChartNS() != "" {
		if k.String() == "esc" {
			l.chartNS = ""
		}
		return nil
	}
	switch k.String() {
	case "j", "down":
		if l.cursor < len(l.entries)-1 {
			l.cursor++
		}
	case "k", "up":
		if l.cursor > 0 {
			l.cursor--
		}
	case "g":
		l.cursor = 0
	case "]":
		l.cursor += l.pageSize
		if l.cursor > len(l.entries)-1 {
			l.cursor = len(l.entries) - 1
		}
		if l.cursor < 0 {
			l.cursor = 0
		}
	case "[":
		l.cursor -= l.pageSize
		if l.cursor < 0 {
			l.cursor = 0
		}
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
		if l.cursor < 0 || l.cursor >= len(l.entries) {
			return nil
		}
		e := l.entries[l.cursor]
		switch e.kind {
		case entryHeaderNS:
			if l.m.projectScope == "" {
				return nil
			}
			l.toggleNamespaceFacet(e.ns)
		case entryHeaderTags:
			// no-op: bare tags have no namespace to facet on.
		case entryRow:
			l.detail = labelDetailState{row: e.row}
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
	if l.cursor < 0 || l.cursor >= len(l.entries) {
		return labelRow{}, false
	}
	e := l.entries[l.cursor]
	if e.kind != entryRow {
		return labelRow{}, false
	}
	return e.row, true
}

// activeChartNS returns the namespace currently charted, self-healing to "" if
// its facet token is no longer present in the Tasks filter (e.g. the user
// edited or cleared the filter directly).
func (l *labelsModel) activeChartNS() string {
	if l.chartNS == "" {
		return ""
	}
	if !filterHasToken(l.m.tasks.filter, facetToken(l.m.projectScope, l.chartNS)) {
		l.chartNS = ""
	}
	return l.chartNS
}

// toggleNamespaceFacet toggles the ns wildcard facet in the Tasks filter and
// the Labels chart view. If the facet is present it is removed and the chart
// closes; otherwise it is added and the chart opens for ns. Refreshes the
// Tasks pane so grouping updates immediately.
func (l *labelsModel) toggleNamespaceFacet(ns string) {
	token := facetToken(l.m.projectScope, ns)
	if filterHasToken(l.m.tasks.filter, token) {
		l.m.tasks.filter = filterRemoveToken(l.m.tasks.filter, token)
		l.chartNS = ""
	} else {
		l.m.tasks.filter = filterAddToken(l.m.tasks.filter, token)
		l.chartNS = ns
	}
	l.m.tasks.cursor = 0
	l.m.tasks.refresh()
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
	if l.view == lViewList && l.activeChartNS() != "" {
		return l.renderChart()
	}
	switch l.view {
	case lViewList:
		return l.renderList()
	case lViewDetail:
		return l.renderDetail()
	}
	return ""
}

func (l *labelsModel) renderList() string {
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
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("project: %s   total labels: %d", l.m.projectScope, len(l.rows))))
	b.WriteString("\n")

	// Build one display line per entry; the cursor indexes l.entries directly,
	// so headers are highlightable cursor stops. lineRowOrd tracks the 1-based
	// label-row ordinal for each line (-1 for headers) to render the footer.
	var bodyLines []string
	var lineRowOrd []int
	cursorLine := 0
	rowOrd := 0
	for i, e := range l.entries {
		switch e.kind {
		case entryHeaderNS:
			line := l.m.styles.NamespaceHeader.Render(e.ns + ":")
			if i == l.cursor {
				line = l.m.styles.RowCursor.Render(e.ns + ":")
				cursorLine = len(bodyLines)
			}
			bodyLines = append(bodyLines, dashboardLine(l.width, line))
			lineRowOrd = append(lineRowOrd, -1)
		case entryHeaderTags:
			line := l.m.styles.NamespaceHeader.Render("tags:")
			if i == l.cursor {
				line = l.m.styles.RowCursor.Render("tags:")
				cursorLine = len(bodyLines)
			}
			bodyLines = append(bodyLines, dashboardLine(l.width, line))
			lineRowOrd = append(lineRowOrd, -1)
		case entryRow:
			rowOrd++
			r := e.row
			desc := r.description
			if desc == "" {
				desc = l.m.styles.Warning.Render("needs description")
			}
			line := fmt.Sprintf(" %-30s %5d %-5s  %s", r.full, r.usage, pluralTasks(r.usage), desc)
			if i == l.cursor {
				line = " " + l.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
				cursorLine = len(bodyLines)
			} else {
				line = " " + line
			}
			bodyLines = append(bodyLines, dashboardLine(l.width, line))
			lineRowOrd = append(lineRowOrd, rowOrd)
		}
	}

	start, end := windowLines(len(bodyLines), cursorLine, l.pageSize)
	for i := start; i < end; i++ {
		b.WriteString(bodyLines[i])
		b.WriteString("\n")
	}
	firstOrd, lastOrd := -1, -1
	for i := start; i < end; i++ {
		if lineRowOrd[i] < 0 {
			continue
		}
		if firstOrd == -1 {
			firstOrd = lineRowOrd[i]
		}
		lastOrd = lineRowOrd[i]
	}
	if firstOrd == -1 {
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("showing 0-0 of "+fmt.Sprint(len(l.rows)))))
	} else {
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render(fmt.Sprintf("showing %d-%d of %d", firstOrd, lastOrd, len(l.rows)))))
	}
	return padToHeight(b.String(), l.contentHeight)
}

// renderChart renders the "tasks by label" bar chart for the active namespace:
// one meter row per label carrying that namespace. Percentages are each
// label's share of total usage within the namespace (matching the approved
// mockup: shares sum to ~100%); counts are absolute project-wide usage.
func (l *labelsModel) renderChart() string {
	ns := l.chartNS
	var rows []labelRow
	total := 0
	for _, e := range l.entries {
		if e.kind == entryRow && strings.HasPrefix(e.row.suffix, ns+":") {
			rows = append(rows, e.row)
			total += e.row.usage
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("chart: %s", ns)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, l.m.styles.Muted.Render(fmt.Sprintf("project: %s   namespace: %s", l.m.projectScope, ns))))
	b.WriteString("\n")

	if len(rows) == 0 {
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("no labels in this namespace")))
		b.WriteString("\n")
	} else {
		nameW := 0
		for _, r := range rows {
			if w := len(r.full); w > nameW {
				nameW = w
			}
		}
		meterW := l.width - nameW - 16
		if meterW < 10 {
			meterW = 10
		}
		for _, r := range rows {
			percent := 0
			if total > 0 {
				percent = (r.usage*100 + total/2) / total
			}
			line := fmt.Sprintf(" %-*s %s %3d%% %4d", nameW, r.full, meterBar(percent, meterW), percent, r.usage)
			b.WriteString(dashboardLine(l.width, line))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back to list")))
	return padToHeight(b.String(), l.contentHeight)
}

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

func (l *labelsModel) statusHint() string {
	if l.m.projectScope == "" {
		return "[?]keys"
	}
	if l.activeChartNS() != "" {
		return "[Esc]back to list"
	}
	if l.view == lViewDetail {
		return "[d]esc [l]remove [Esc]back"
	}
	return "[a]dd [d]esc [l]remove [S]eed [Enter]select/detail [Esc]back [?]keys"
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
