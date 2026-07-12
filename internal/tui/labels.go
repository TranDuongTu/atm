package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"atm/internal/seed"
	"atm/internal/store"

	"github.com/charmbracelet/bubbletea"
)

// pluralUses returns "use"/"uses" for the given count — neutral over tasks
// and comments, since LabelUsage counts both kinds of entities.
func pluralUses(n int) string {
	if n == 1 {
		return "use"
	}
	return "uses"
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
	rows          []labelRow // all project labels (flat, used to build tables/charts)
	nsRows        []nsRow    // L0 table rows
	level         lLevel
	ns            string // active namespace at chart/detail (real ns name; "" when bareTags)
	bareTags      bool   // active pseudo-namespace is the bare-tags bucket
	cursor        int    // indexes the current level's row slice
	offset        int
	pageSize      int
	detail        labelDetailState
}

type lLevel int

const (
	lLevelTable  lLevel = iota // L0 namespace table
	lLevelChart                // L1 per-namespace chart
	lLevelDetail               // L2 label detail (or unset/none leaf)
)

type labelRow struct {
	suffix      string
	full        string
	description string
	usage       int
}

// nsRow is one row of the L0 namespace table. For real namespaces key is the
// namespace name; the synthetic rows set bareTags or none instead.
type nsRow struct {
	key      string
	display  string
	tasks    int
	labels   int
	bareTags bool
	none     bool
}

type labelDetailState struct {
	row  labelRow
	leaf string // "" for a real label; "unset" or "none" for the synthetic leaves
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
	l.pageSize = h - 2 // table header + trailing line
	if l.pageSize < 1 {
		l.pageSize = 1
	}
}

func (l *labelsModel) refresh() {
	l.rows = nil
	l.nsRows = nil
	if l.m.projectScope == "" {
		return
	}
	scope := l.m.projectScope
	ls := l.m.store.LabelList(scope, "")
	usage, _ := l.m.store.LabelUsageGrouped(scope)
	for _, lab := range ls {
		l.rows = append(l.rows, labelRow{
			suffix:      strings.TrimPrefix(lab.Name, scope+":"),
			full:        lab.Name,
			description: lab.Description,
			usage:       usage[lab.Name],
		})
	}
	l.nsRows = l.buildNamespaceRows()
	if l.cursor >= len(l.nsRows) && len(l.nsRows) > 0 {
		l.cursor = len(l.nsRows) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// buildNamespaceRows aggregates the project's labels and tasks into the L0
// table: one row per real namespace (alphabetical), then a "tags" row for
// bare labels, then a "(none)" row for zero-label tasks. Synthetic rows are
// omitted when empty. TASKS counts distinct tasks; LABELS counts labels.
func (l *labelsModel) buildNamespaceRows() []nsRow {
	scope := l.m.projectScope
	labelCount := map[string]int{}
	bareLabelCount := 0
	var nsOrder []string
	seenNS := map[string]bool{}
	for _, r := range l.rows {
		parts := strings.SplitN(r.suffix, ":", 2)
		if len(parts) == 2 {
			ns := parts[0]
			if !seenNS[ns] {
				seenNS[ns] = true
				nsOrder = append(nsOrder, ns)
			}
			labelCount[ns]++
		} else {
			bareLabelCount++
		}
	}
	sort.Strings(nsOrder)

	nsTaskCount := map[string]int{}
	bareTaskCount := 0
	noneTaskCount := 0
	for _, tk := range l.m.store.ListTasks(store.QueryFilters{Project: scope}) {
		if len(tk.Labels) == 0 {
			noneTaskCount++
			continue
		}
		seen := map[string]bool{}
		hasBare := false
		for _, full := range tk.Labels {
			suffix := strings.TrimPrefix(full, scope+":")
			parts := strings.SplitN(suffix, ":", 2)
			if len(parts) == 2 {
				if !seen[parts[0]] {
					seen[parts[0]] = true
					nsTaskCount[parts[0]]++
				}
			} else {
				hasBare = true
			}
		}
		if hasBare {
			bareTaskCount++
		}
	}

	var out []nsRow
	for _, ns := range nsOrder {
		out = append(out, nsRow{key: ns, display: ns, tasks: nsTaskCount[ns], labels: labelCount[ns]})
	}
	if bareLabelCount > 0 {
		out = append(out, nsRow{display: "tags", tasks: bareTaskCount, labels: bareLabelCount, bareTags: true})
	}
	if noneTaskCount > 0 {
		out = append(out, nsRow{display: "(none)", tasks: noneTaskCount, none: true})
	}
	return out
}

// enterTable returns the pane to L0 and clears the Tasks-pane focus so the
// Tasks pane shows all tasks. cursor is preserved so Esc lands where the user
// drilled from.
func (l *labelsModel) enterTable() {
	l.level = lLevelTable
	l.ns = ""
	l.bareTags = false
	l.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
}

// enterChart drills into a namespace row's chart and focuses the Tasks pane on
// tasks carrying that namespace. cursor is reset to the top of the chart.
func (l *labelsModel) enterChart(r nsRow) {
	l.level = lLevelChart
	l.ns = r.key
	l.bareTags = r.bareTags
	l.cursor = 0
	l.offset = 0
	if r.bareTags {
		l.m.tasks.setFocus(taskFocus{mode: focusPresent, bareTags: true}, "")
		return
	}
	l.m.tasks.setFocus(taskFocus{mode: focusPresent, ns: r.key}, facetToken(l.m.projectScope, r.key))
}

// enterNoneLeaf filters the Tasks pane to zero-label tasks. It is a leaf: the
// Labels pane shows a minimal detail and Esc returns to the table.
func (l *labelsModel) enterNoneLeaf() {
	l.level = lLevelDetail
	l.detail = labelDetailState{leaf: "none"}
	l.m.tasks.setFocus(taskFocus{mode: focusUnlabeled}, "")
}

// reset returns the pane to L0 and clears Tasks focus. Called on project switch
// so no stale filter survives.
func (l *labelsModel) reset() {
	l.level = lLevelTable
	l.ns = ""
	l.bareTags = false
	l.cursor = 0
	l.offset = 0
	l.detail = labelDetailState{}
}

func (l *labelsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch l.level {
	case lLevelTable:
		return l.handleTableKey(k)
	case lLevelChart:
		return l.handleChartKey(k)
	case lLevelDetail:
		return l.handleDetailKey(k)
	}
	return nil
}

func (l *labelsModel) handleTableKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		if l.cursor < len(l.nsRows)-1 {
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
		if l.cursor > len(l.nsRows)-1 {
			l.cursor = len(l.nsRows) - 1
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
	case "S":
		if l.m.projectScope == "" {
			return nil
		}
		return l.seedDefaults()
	case "enter":
		if l.cursor < 0 || l.cursor >= len(l.nsRows) {
			return nil
		}
		r := l.nsRows[l.cursor]
		if r.none {
			l.enterNoneLeaf()
			return nil
		}
		l.enterChart(r)
	}
	return nil
}

// handleChartKey is expanded in Task 4 (cursor over label rows, Enter -> detail,
// (unset) row). For now it only handles Esc back to the table.
func (l *labelsModel) handleChartKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		l.enterTable()
	}
	return nil
}

func (l *labelsModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "d":
		if l.detail.leaf == "" {
			l.m.openLabelDescribeFormFor(l.detail.row.suffix, l.detail.row.description)
		}
	case "l":
		if l.detail.leaf == "" {
			l.m.openLabelRemoveForm(l.m.projectScope)
		}
	case "esc":
		// A real label detail and the (unset) leaf sit above the chart; the
		// (none) leaf sits above the table.
		if l.detail.leaf == "none" {
			l.enterTable()
			return nil
		}
		l.reenterChart()
	}
	return nil
}

// reenterChart re-applies the L1 chart state for the active namespace. Used by
// Esc from a label detail or the (unset) leaf.
func (l *labelsModel) reenterChart() {
	r := nsRow{key: l.ns, bareTags: l.bareTags, display: l.ns}
	if l.bareTags {
		r.display = "tags"
	}
	l.enterChart(r)
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
	if l.m.projectScope == "" {
		lines := []string{
			l.m.styles.EmptyHead.Render("no project selected"),
			"",
			l.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects pane to scope this view", l.m.styles.EmptyKey.Render("[s]"))),
		}
		return padToHeight(centerLinesBoth(lines, l.width, l.contentHeight), l.contentHeight)
	}
	switch l.level {
	case lLevelChart:
		return l.renderChart()
	case lLevelDetail:
		return l.renderDetail()
	default:
		return l.renderTable()
	}
}

func (l *labelsModel) renderTable() string {
	if len(l.nsRows) == 0 {
		return padToHeight("no labels", l.contentHeight)
	}
	var b strings.Builder
	header := fmt.Sprintf(" %-20s %8s %8s", "NAMESPACE", "TASKS", "LABELS")
	b.WriteString(dashboardLine(l.width, l.m.styles.HeaderLabel.Render(header)))
	b.WriteString("\n")

	var lines []string
	for i, r := range l.nsRows {
		tasks := fmt.Sprintf("%d", r.tasks)
		labels := fmt.Sprintf("%d", r.labels)
		if r.none {
			labels = "-"
		}
		line := fmt.Sprintf(" %-20s %8s %8s", r.display, tasks, labels)
		if i == l.cursor {
			line = " " + l.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
		}
		lines = append(lines, dashboardLine(l.width, line))
	}
	start, end := windowLines(len(lines), l.cursor, l.pageSize)
	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), l.contentHeight)
}

// renderChart is the basic (non-cursor) chart; Task 4 replaces it with a
// cursor-navigable chart carrying an (unset) row.
func (l *labelsModel) renderChart() string {
	var b strings.Builder
	title := l.ns
	if l.bareTags {
		title = "tags"
	}
	b.WriteString(dashboardLine(l.width, fmt.Sprintf("chart: %s", title)))
	b.WriteString("\n")
	for _, r := range l.rows {
		if l.labelInActiveNamespace(r) {
			b.WriteString(dashboardLine(l.width, fmt.Sprintf(" %-30s %5d", r.full, r.usage)))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back")))
	return padToHeight(b.String(), l.contentHeight)
}

// labelInActiveNamespace reports whether label row r belongs to the active
// chart namespace (real namespace prefix, or a bare label when bareTags).
func (l *labelsModel) labelInActiveNamespace(r labelRow) bool {
	if l.bareTags {
		return !strings.Contains(r.suffix, ":")
	}
	return strings.HasPrefix(r.suffix, l.ns+":")
}

func (l *labelsModel) renderDetail() string {
	var b strings.Builder
	switch l.detail.leaf {
	case "none":
		b.WriteString(dashboardLine(l.width, "tasks with no labels"))
		b.WriteString("\n")
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back to namespaces")))
		return padToHeight(b.String(), l.contentHeight)
	case "unset":
		ns := l.ns
		if l.bareTags {
			ns = "bare tag"
		}
		b.WriteString(dashboardLine(l.width, fmt.Sprintf("tasks with no %s", ns)))
		b.WriteString("\n")
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back to chart")))
		return padToHeight(b.String(), l.contentHeight)
	}
	r := l.detail.row
	fmt.Fprintf(&b, "Label %s\n", r.full)
	b.WriteString(sepLine("─", 78, l.width, 2))
	b.WriteString("\n")
	b.WriteString(sectionCaption(l.m.styles, l.width, "FACTS"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("name        %s", r.full)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("usage       %d %s", r.usage, pluralUses(r.usage))))
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
	switch l.level {
	case lLevelChart:
		return "[Enter]inspect [d]esc [l]remove [Esc]back"
	case lLevelDetail:
		if l.detail.leaf != "" {
			return "[Esc]back"
		}
		return "[d]esc [l]remove [Esc]back"
	default:
		return "[Enter]open [a]dd [S]eed [?]keys"
	}
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
