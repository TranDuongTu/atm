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
	m.openLabelRemoveFormFor(code, "")
}

// openLabelRemoveFormFor opens the remove-label form with a known suffix.
// Used when the Labels pane has a real label under the cursor.
func (m *Model) openLabelRemoveFormFor(code, suffix string) {
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
		{Label: "name", Required: true, Value: suffix, Hint: "<namespace>:<value> or <tag>", Validator: validator},
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
	tableCursor   int    // L0 cursor saved before entering a chart
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

type chartRow struct {
	full  string
	count int
	unset bool
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
	l.cursor = l.tableCursor
	if l.cursor >= len(l.nsRows) {
		l.cursor = len(l.nsRows) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
}

// enterChart drills into a namespace row's chart and focuses the Tasks pane on
// tasks carrying that namespace. cursor is reset to the top of the chart.
func (l *labelsModel) enterChart(r nsRow) {
	l.tableCursor = l.cursor
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

// chartRows returns the active namespace's per-label task counts plus a
// trailing (unset) row for tasks lacking the namespace. Real namespaces use
// GroupTasks; the tags pseudo-namespace is counted locally since no single
// wildcard selects bare labels.
func (l *labelsModel) chartRows() []chartRow {
	scope := l.m.projectScope
	var rows []chartRow
	unset := 0
	if l.bareTags {
		bareCount := map[string]int{}
		for _, tk := range l.m.store.ListTasks(store.QueryFilters{Project: scope}) {
			if taskHasBareTag(scope, tk) {
				for _, full := range tk.Labels {
					if !strings.Contains(strings.TrimPrefix(full, scope+":"), ":") {
						bareCount[full]++
					}
				}
			} else {
				unset++
			}
		}
		for _, r := range l.rows {
			if !strings.Contains(r.suffix, ":") {
				rows = append(rows, chartRow{full: r.full, count: bareCount[r.full]})
			}
		}
	} else {
		groups, others := l.m.store.GroupTasks(store.QueryFilters{Project: scope, Labels: []string{facetToken(scope, l.ns)}})
		counts := map[string]int{}
		for _, g := range groups {
			counts[g.Label] = len(g.Tasks)
		}
		for _, r := range l.rows {
			if strings.HasPrefix(r.suffix, l.ns+":") {
				rows = append(rows, chartRow{full: r.full, count: counts[r.full]})
			}
		}
		unset = len(others)
	}
	if unset > 0 {
		rows = append(rows, chartRow{full: "(unset)", count: unset, unset: true})
	}
	return rows
}

// enterDetail opens a label's detail (L2) and focuses the Tasks pane on that
// exact label as a flat list.
func (l *labelsModel) enterDetail(r labelRow) {
	l.level = lLevelDetail
	l.detail = labelDetailState{row: r}
	l.m.tasks.setFocus(taskFocus{mode: focusOff}, r.full)
}

// enterUnsetLeaf focuses the Tasks pane on tasks lacking the active namespace.
func (l *labelsModel) enterUnsetLeaf() {
	l.level = lLevelDetail
	l.detail = labelDetailState{leaf: "unset"}
	if l.bareTags {
		l.m.tasks.setFocus(taskFocus{mode: focusAbsent, bareTags: true}, "")
		return
	}
	l.m.tasks.setFocus(taskFocus{mode: focusAbsent, ns: l.ns}, facetToken(l.m.projectScope, l.ns))
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
	l.tableCursor = 0
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

func (l *labelsModel) handleChartKey(k tea.KeyMsg) tea.Cmd {
	rows := l.chartRows()
	switch k.String() {
	case "j", "down":
		if l.cursor < len(rows)-1 {
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
		if l.cursor > len(rows)-1 {
			l.cursor = len(rows) - 1
		}
		if l.cursor < 0 {
			l.cursor = 0
		}
	case "[":
		l.cursor -= l.pageSize
		if l.cursor < 0 {
			l.cursor = 0
		}
	case "d":
		if r, ok := l.chartLabelRow(); ok {
			l.m.openLabelDescribeFormFor(r.suffix, r.description)
		}
	case "l":
		if r, ok := l.chartLabelRow(); ok {
			l.m.openLabelRemoveFormFor(l.m.projectScope, r.suffix)
		}
	case "enter":
		if l.cursor < 0 || l.cursor >= len(rows) {
			return nil
		}
		if rows[l.cursor].unset {
			l.enterUnsetLeaf()
			return nil
		}
		if r, ok := l.chartLabelRow(); ok {
			l.enterDetail(r)
		}
	case "esc":
		l.enterTable()
	}
	return nil
}

// chartLabelRow returns the labelRow under the chart cursor, or ok=false when
// the cursor is on the (unset) row or out of range.
func (l *labelsModel) chartLabelRow() (labelRow, bool) {
	rows := l.chartRows()
	if l.cursor < 0 || l.cursor >= len(rows) || rows[l.cursor].unset {
		return labelRow{}, false
	}
	full := rows[l.cursor].full
	for _, r := range l.rows {
		if r.full == full {
			return r, true
		}
	}
	return labelRow{}, false
}

func (l *labelsModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "d":
		if l.detail.leaf == "" {
			l.m.openLabelDescribeFormFor(l.detail.row.suffix, l.detail.row.description)
		}
	case "l":
		if l.detail.leaf == "" {
			l.m.openLabelRemoveFormFor(l.m.projectScope, l.detail.row.suffix)
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
	tableCursor := l.tableCursor
	l.enterChart(r)
	l.tableCursor = tableCursor
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

func (l *labelsModel) renderChart() string {
	title := l.ns
	if l.bareTags {
		title = "tags"
	}
	rows := l.chartRows()
	barTotal := 0
	for _, r := range rows {
		barTotal += r.count
	}

	var b strings.Builder
	b.WriteString(dashboardLine(l.width, fmt.Sprintf("%s  ·  %d tasks", title, l.activeNamespaceTaskCount())))
	b.WriteString("\n")

	nameW := 0
	for _, r := range rows {
		if w := len(r.full); w > nameW {
			nameW = w
		}
	}
	if nameW < 8 {
		nameW = 8
	}
	meterW := l.width - nameW - 14
	if meterW < 10 {
		meterW = 10
	}

	var lines []string
	for i, r := range rows {
		percent := 0
		if barTotal > 0 {
			percent = (r.count*100 + barTotal/2) / barTotal
		}
		line := fmt.Sprintf(" %-*s %s %4d", nameW, r.full, meterBar(percent, meterW), r.count)
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
	b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Enter]inspect  [Esc]back")))
	return padToHeight(b.String(), l.contentHeight)
}

func (l *labelsModel) renderDetail() string {
	var b strings.Builder
	switch l.detail.leaf {
	case "none":
		b.WriteString(dashboardLine(l.width, fmt.Sprintf("%d tasks with no labels", l.syntheticLeafTaskCount())))
		b.WriteString("\n")
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back to namespaces")))
		return padToHeight(b.String(), l.contentHeight)
	case "unset":
		ns := l.ns
		if l.bareTags {
			ns = "bare tag"
		}
		b.WriteString(dashboardLine(l.width, fmt.Sprintf("%d tasks with no %s", l.syntheticLeafTaskCount(), ns)))
		b.WriteString("\n")
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back to chart")))
		return padToHeight(b.String(), l.contentHeight)
	}
	r := l.detail.row
	b.WriteString(dashboardLine(l.width, fmt.Sprintf("name        %s", r.full)))
	b.WriteString("\n")
	b.WriteString(dashboardLine(l.width, fmt.Sprintf("usage       %d %s", r.usage, pluralUses(r.usage))))
	b.WriteString("\n")
	desc := r.description
	if desc == "" {
		desc = l.m.styles.Warning.Render("needs description")
	}
	b.WriteString(dashboardLine(l.width, fmt.Sprintf("description %s", desc)))
	return padToHeight(b.String(), l.contentHeight)
}

// activeNamespaceTaskCount counts distinct project tasks carrying the active
// namespace or bare-tag bucket. Chart rows intentionally retain membership
// counts, so their sum is not suitable for the headline.
func (l *labelsModel) activeNamespaceTaskCount() int {
	scope := l.m.projectScope
	count := 0
	for _, tk := range l.m.store.ListTasks(store.QueryFilters{Project: scope}) {
		if l.bareTags {
			if taskHasBareTag(scope, tk) {
				count++
			}
			continue
		}
		for _, full := range tk.Labels {
			if strings.HasPrefix(full, scope+":"+l.ns+":") {
				count++
				break
			}
		}
	}
	return count
}

func (l *labelsModel) syntheticLeafTaskCount() int {
	count := 0
	for _, tk := range l.m.store.ListTasks(store.QueryFilters{Project: l.m.projectScope}) {
		switch l.detail.leaf {
		case "none":
			if len(tk.Labels) == 0 {
				count++
			}
		case "unset":
			if l.bareTags {
				if !taskHasBareTag(l.m.projectScope, tk) {
					count++
				}
				continue
			}
			hasNamespace := false
			for _, full := range tk.Labels {
				if strings.HasPrefix(full, l.m.projectScope+":"+l.ns+":") {
					hasNamespace = true
					break
				}
			}
			if !hasNamespace {
				count++
			}
		}
	}
	return count
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
