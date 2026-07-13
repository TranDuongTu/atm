package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"atm/internal/seed"
	"atm/internal/store"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pluralUses returns "use"/"uses" for the given count — neutral over tasks
// and comments, since LabelUsage counts both kinds of entities.
func pluralUses(n int) string {
	if n == 1 {
		return "use"
	}
	return "uses"
}

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
	if err := m.store.LabelAdd(full, "", "", m.actor); err != nil {
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

// --- Boards pane model ---

// boardRow is one row of the Boards pane: a computed label. Boards and
// namespaces render identically on purpose — the user should not have to
// know which is which. The only difference is that a namespace can expand.
type boardRow struct {
	Name             string // display name: "next-sprint" or "status"
	FullName         string // "ATM:next-sprint" or "ATM:status:*"
	Description      string
	Expr             string // empty for a namespace
	Count            int
	Expandable       bool // true for namespaces (they have members)
	NeedsDescription bool // renders the warning mark — conventions rule 6
	Broken           bool // expression invalid or cyclic -> render as broken
}

type boardsModel struct {
	m             *Model
	width         int
	contentHeight int
	rows          []boardRow // L0 flat list of computed labels (boards + namespaces)
	memberRows    []labelRow // all project labels (flat, used to build charts/detail)
	level         lLevel
	ns            string // active namespace at chart/detail
	cursor        int    // indexes the current level's row slice
	tableCursor   int    // L0 cursor saved before entering a chart
	offset        int
	pageSize      int
	detail        labelDetailState
}

type lLevel int

const (
	lLevelTable  lLevel = iota // L0 flat boards list
	lLevelChart                // L1 per-namespace chart
	lLevelDetail               // L2 label detail (or unset leaf)
)

type labelRow struct {
	suffix      string
	full        string
	description string
	usage       int
}

type labelDetailState struct {
	row  labelRow
	leaf string // "" for a real label; "unset" for the synthetic leaf
}

type chartRow struct {
	full  string
	count int
	unset bool
}

func newBoardsModel(m *Model) boardsModel {
	return boardsModel{m: m}
}

func (b *boardsModel) SetSize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	b.width = w
	b.contentHeight = h
	b.pageSize = h - 2 // table header + trailing line
	if b.pageSize < 1 {
		b.pageSize = 1
	}
}

func (b *boardsModel) refresh() {
	var selectedChart chartRow
	restoreChart := false
	if b.level == lLevelChart {
		rows := b.chartRows()
		if b.cursor >= 0 && b.cursor < len(rows) {
			selectedChart = rows[b.cursor]
			restoreChart = true
		}
	}
	b.rows = nil
	b.memberRows = nil
	if b.m.projectScope == "" {
		return
	}
	scope := b.m.projectScope
	ls := b.m.store.LabelList(scope, "")
	usage, _ := b.m.store.LabelUsageGrouped(scope)
	for _, lab := range ls {
		b.memberRows = append(b.memberRows, labelRow{
			suffix:      strings.TrimPrefix(lab.Name, scope+":"),
			full:        lab.Name,
			description: lab.Description,
			usage:       usage[lab.Name],
		})
	}
	b.rows = b.buildBoardRows(ls)
	if restoreChart && b.restoreChartCursor(selectedChart) {
		return
	}
	b.clampCursor()
}

// buildBoardRows constructs the flat L0 list: every board (a label with an
// Expr) and every emergent namespace (derived from stored label names,
// joined to its ATM:<ns>:* descriptor when one exists). Boards and
// namespaces are intermixed and sorted by display name; the result is a
// single flat list of computed labels.
func (b *boardsModel) buildBoardRows(ls []store.Label) []boardRow {
	scope := b.m.projectScope
	byName := map[string]store.Label{}
	for _, l := range ls {
		byName[l.Name] = l
	}
	var out []boardRow
	seen := map[string]bool{}
	// Boards: every label with a non-empty Expr.
	for _, l := range ls {
		if l.Expr == "" {
			continue
		}
		name := strings.TrimPrefix(l.Name, scope+":")
		full := l.Name
		count, broken := b.boardCount(full)
		seen[name] = true
		out = append(out, boardRow{
			Name:        name,
			FullName:    full,
			Description: l.Description,
			Expr:        l.Expr,
			Count:       count,
			Expandable:  false,
			Broken:      broken,
		})
	}
	// Emergent namespaces: derived from stored label names (any label of
	// the form <scope>:<ns>:<value> introduces namespace <ns>), plus any
	// namespace descriptor label (<scope>:<ns>:*) even with no members.
	nsOrder := []string{}
	nsSeen := map[string]bool{}
	for _, l := range ls {
		suffix := strings.TrimPrefix(l.Name, scope+":")
		if store.IsNamespaceName(l.Name) {
			ns := strings.TrimSuffix(suffix, ":*")
			if !nsSeen[ns] {
				nsSeen[ns] = true
				nsOrder = append(nsOrder, ns)
			}
			continue
		}
		parts := strings.SplitN(suffix, ":", 2)
		if len(parts) == 2 {
			ns := parts[0]
			if !nsSeen[ns] {
				nsSeen[ns] = true
				nsOrder = append(nsOrder, ns)
			}
		}
	}
	sort.Strings(nsOrder)
	for _, ns := range nsOrder {
		if seen[ns] {
			continue
		}
		descFull := scope + ":" + ns + ":*"
		descLabel, hasDesc := byName[descFull]
		desc := ""
		needsDesc := false
		if hasDesc {
			desc = descLabel.Description
			if desc == "" {
				needsDesc = true
			}
		} else {
			needsDesc = true
		}
		out = append(out, boardRow{
			Name:             ns,
			FullName:         descFull,
			Description:      desc,
			Expr:             "",
			Count:            b.namespaceTaskCount(ns),
			Expandable:       true,
			NeedsDescription: needsDesc,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// boardCount returns the task count for a board (label with an Expr) and
// whether the expression is broken (invalid or cyclic). Uses GroupTasksErr
// via a single-label query because ListTasks swallows expression errors
// and would conflate a broken board with an empty one.
func (b *boardsModel) boardCount(full string) (int, bool) {
	_, others, err := b.m.store.GroupTasksErr(store.QueryFilters{
		Project: b.m.projectScope,
		Labels:  []string{full},
	})
	if err != nil {
		return 0, true
	}
	return len(others), false
}

// namespaceTaskCount counts distinct tasks carrying the namespace, the same
// surface the old L0 namespace table showed.
func (b *boardsModel) namespaceTaskCount(ns string) int {
	scope := b.m.projectScope
	count := 0
	for _, tk := range b.m.store.ListTasks(store.QueryFilters{Project: scope}) {
		for _, full := range tk.Labels {
			if strings.HasPrefix(full, scope+":"+ns+":") {
				count++
				break
			}
		}
	}
	return count
}

func (b *boardsModel) restoreChartCursor(selected chartRow) bool {
	rows := b.chartRows()
	for i, r := range rows {
		if selected.unset && r.unset {
			b.cursor = i
			return true
		}
		if !selected.unset && !r.unset && r.full == selected.full {
			b.cursor = i
			return true
		}
	}
	return false
}

func (b *boardsModel) clampCursor() {
	max := len(b.rows) - 1
	if b.level == lLevelChart {
		max = len(b.chartRows()) - 1
	}
	if max < 0 {
		b.cursor = 0
		return
	}
	if b.cursor > max {
		b.cursor = max
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
}

// enterTable returns the pane to L0 and clears the Tasks-pane focus so the
// Tasks pane shows all tasks. cursor is preserved so Esc lands where the user
// drilled from.
func (b *boardsModel) enterTable() {
	b.level = lLevelTable
	b.ns = ""
	b.cursor = b.tableCursor
	if b.cursor >= len(b.rows) {
		b.cursor = len(b.rows) - 1
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
	b.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
}

// enterChart drills into a namespace row's chart and focuses the Tasks pane on
// tasks carrying that namespace. cursor is reset to the top of the chart.
func (b *boardsModel) enterChart(ns string) {
	b.tableCursor = b.cursor
	b.level = lLevelChart
	b.ns = ns
	b.cursor = 0
	b.offset = 0
	b.m.tasks.setFocus(taskFocus{mode: focusPresent, ns: ns}, facetToken(b.m.projectScope, ns))
}

// enterBoard filters the Tasks pane to a board's computed membership. A board
// is a label with an Expr; passing its FullName as a QueryFilters label token
// evaluates the expression through the resolver (Task 4). No new query path.
func (b *boardsModel) enterBoard(r boardRow) {
	b.tableCursor = b.cursor
	b.m.tasks.setFocus(taskFocus{mode: focusOff}, r.FullName)
}

// chartRows returns the active namespace's per-label task counts plus a
// trailing (unset) row for tasks lacking the namespace.
func (b *boardsModel) chartRows() []chartRow {
	scope := b.m.projectScope
	var rows []chartRow
	unset := 0
	groups, others := b.m.store.GroupTasks(store.QueryFilters{Project: scope, Labels: []string{facetToken(scope, b.ns)}})
	counts := map[string]int{}
	for _, g := range groups {
		counts[g.Label] = len(g.Tasks)
	}
	for _, r := range b.memberRows {
		if strings.HasPrefix(r.suffix, b.ns+":") {
			rows = append(rows, chartRow{full: r.full, count: counts[r.full]})
		}
	}
	unset = len(others)
	if unset > 0 {
		rows = append(rows, chartRow{full: "(unset)", count: unset, unset: true})
	}
	return rows
}

// enterDetail opens a label's detail (L2) and focuses the Tasks pane on that
// exact label as a flat list.
func (b *boardsModel) enterDetail(r labelRow) {
	b.level = lLevelDetail
	b.detail = labelDetailState{row: r}
	b.m.tasks.setFocus(taskFocus{mode: focusOff}, r.full)
}

// enterUnsetLeaf focuses the Tasks pane on tasks lacking the active namespace.
func (b *boardsModel) enterUnsetLeaf() {
	b.level = lLevelDetail
	b.detail = labelDetailState{leaf: "unset"}
	b.m.tasks.setFocus(taskFocus{mode: focusAbsent, ns: b.ns}, facetToken(b.m.projectScope, b.ns))
}

// reset returns the pane to L0 and clears Tasks focus. Called on project switch
// so no stale filter survives.
func (b *boardsModel) reset() {
	b.level = lLevelTable
	b.ns = ""
	b.cursor = 0
	b.tableCursor = 0
	b.offset = 0
	b.detail = labelDetailState{}
}

func (b *boardsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch b.level {
	case lLevelTable:
		return b.handleTableKey(k)
	case lLevelChart:
		return b.handleChartKey(k)
	case lLevelDetail:
		return b.handleDetailKey(k)
	}
	return nil
}

func (b *boardsModel) handleTableKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		if b.cursor < len(b.rows)-1 {
			b.cursor++
		}
	case "k", "up":
		if b.cursor > 0 {
			b.cursor--
		}
	case "g":
		b.cursor = 0
	case "]":
		b.cursor += b.pageSize
		if b.cursor > len(b.rows)-1 {
			b.cursor = len(b.rows) - 1
		}
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "[":
		b.cursor -= b.pageSize
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "a":
		if b.m.projectScope == "" {
			return nil
		}
		b.m.openLabelAddForm(b.m.projectScope)
	case "S":
		if b.m.projectScope == "" {
			return nil
		}
		return b.seedDefaults()
	case "enter":
		if b.cursor < 0 || b.cursor >= len(b.rows) {
			return nil
		}
		r := b.rows[b.cursor]
		if r.Expandable {
			b.enterChart(r.Name)
			return nil
		}
		b.enterBoard(r)
	}
	return nil
}

func (b *boardsModel) handleChartKey(k tea.KeyMsg) tea.Cmd {
	rows := b.chartRows()
	switch k.String() {
	case "j", "down":
		if b.cursor < len(rows)-1 {
			b.cursor++
		}
	case "k", "up":
		if b.cursor > 0 {
			b.cursor--
		}
	case "g":
		b.cursor = 0
	case "]":
		b.cursor += b.pageSize
		if b.cursor > len(rows)-1 {
			b.cursor = len(rows) - 1
		}
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "[":
		b.cursor -= b.pageSize
		if b.cursor < 0 {
			b.cursor = 0
		}
	case "d":
		if r, ok := b.chartLabelRow(); ok {
			b.m.openLabelDescribeFormFor(r.suffix, r.description)
		}
	case "l":
		if r, ok := b.chartLabelRow(); ok {
			b.m.openLabelRemoveFormFor(b.m.projectScope, r.suffix)
		}
	case "enter":
		if b.cursor < 0 || b.cursor >= len(rows) {
			return nil
		}
		if rows[b.cursor].unset {
			b.enterUnsetLeaf()
			return nil
		}
		if r, ok := b.chartLabelRow(); ok {
			b.enterDetail(r)
		}
	case "esc":
		b.enterTable()
	}
	return nil
}

// chartLabelRow returns the labelRow under the chart cursor, or ok=false when
// the cursor is on the (unset) row or out of range.
func (b *boardsModel) chartLabelRow() (labelRow, bool) {
	rows := b.chartRows()
	if b.cursor < 0 || b.cursor >= len(rows) || rows[b.cursor].unset {
		return labelRow{}, false
	}
	full := rows[b.cursor].full
	for _, r := range b.memberRows {
		if r.full == full {
			return r, true
		}
	}
	return labelRow{}, false
}

func (b *boardsModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "d":
		if b.detail.leaf == "" {
			b.m.openLabelDescribeFormFor(b.detail.row.suffix, b.detail.row.description)
		}
	case "l":
		if b.detail.leaf == "" {
			b.m.openLabelRemoveFormFor(b.m.projectScope, b.detail.row.suffix)
		}
	case "esc":
		// A real label detail and the (unset) leaf sit above the chart.
		b.reenterChart()
	}
	return nil
}

// reenterChart re-applies the L1 chart state for the active namespace. Used by
// Esc from a label detail or the (unset) leaf.
func (b *boardsModel) reenterChart() {
	tableCursor := b.tableCursor
	b.enterChart(b.ns)
	b.tableCursor = tableCursor
}

func (b *boardsModel) seedDefaults() tea.Cmd {
	if err := b.m.store.SeedLabels(b.m.projectScope, b.m.actor); err != nil {
		b.m.showToast("error: " + err.Error())
		return nil
	}
	b.m.showToast(fmt.Sprintf("seeded %d labels into %s", len(seed.Labels), b.m.projectScope))
	b.m.refreshAll()
	return nil
}

func (b *boardsModel) View() string {
	if b.m.projectScope == "" {
		lines := []string{
			b.m.styles.EmptyHead.Render("no project selected"),
			"",
			b.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects pane to scope this view", b.m.styles.EmptyKey.Render("[s]"))),
		}
		return padToHeight(centerLinesBoth(lines, b.width, b.contentHeight), b.contentHeight)
	}
	switch b.level {
	case lLevelChart:
		return b.renderChart()
	case lLevelDetail:
		return b.renderDetail()
	default:
		return b.renderTable()
	}
}

func (b *boardsModel) renderTable() string {
	if len(b.rows) == 0 {
		return padToHeight("no boards", b.contentHeight)
	}
	var sb strings.Builder
	header := boardTableLine(b.width, "BOARD", "COUNT")
	sb.WriteString(dashboardLine(b.width, b.m.styles.HeaderLabel.Render(header)))
	sb.WriteString("\n")

	var lines []string
	for i, r := range b.rows {
		name := r.Name
		if r.NeedsDescription {
			name = name + " " + b.m.styles.Warning.Render("⚠")
		}
		if r.Broken {
			name = name + " " + b.m.styles.Warning.Render("⚠ broken")
		}
		count := fmt.Sprintf("%d", r.Count)
		if r.Broken {
			count = "-"
		}
		line := boardTableLine(b.width, name, count)
		if i == b.cursor {
			line = " " + b.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
		}
		lines = append(lines, dashboardLine(b.width, line))
	}
	start, end := windowLines(len(lines), b.cursor, b.pageSize)
	for i := start; i < end; i++ {
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}
	return padToHeight(sb.String(), b.contentHeight)
}

// boardTableLine renders one row of the flat Boards list: a name column and
// an 8-wide count column. The name column absorbs the warning/broken
// markers so they stay visible alongside the count.
func boardTableLine(width int, name, count string) string {
	nameW := width - 10 // leading space + 8-wide count column + separator.
	if nameW < 8 {
		nameW = 8
	}
	return fitLine(fmt.Sprintf(" %-*s %8s", nameW, name, count), width)
}

func (b *boardsModel) renderChart() string {
	title := b.ns
	rows := b.chartRows()
	barTotal := 0
	for _, r := range rows {
		barTotal += r.count
	}

	var sb strings.Builder
	sb.WriteString(dashboardLine(b.width, fmt.Sprintf("%s  ·  %d tasks", title, b.activeNamespaceTaskCount())))
	sb.WriteString("\n")

	nameW := 0
	for _, r := range rows {
		if w := len(r.full); w > nameW {
			nameW = w
		}
	}
	if nameW < 8 {
		nameW = 8
	}
	meterW := b.width - nameW - 14
	if meterW < 10 {
		meterW = 10
	}

	var lines []string
	for i, r := range rows {
		percent := 0
		if barTotal > 0 {
			percent = (r.count*100 + barTotal/2) / barTotal
		}
		name := fmt.Sprintf("%-*s", nameW, r.full)
		if i == b.cursor {
			name = b.m.styles.RowCursor.Render(r.full) + spaces(nameW-lipgloss.Width(r.full))
		}
		line := fmt.Sprintf(" %s %s %4d", name, meterBar(percent, meterW), r.count)
		lines = append(lines, dashboardLine(b.width, line))
	}
	start, end := windowLines(len(lines), b.cursor, b.pageSize)
	for i := start; i < end; i++ {
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}
	sb.WriteString(dashboardLine(b.width, b.m.styles.Muted.Render("[Enter]inspect  [Esc]back")))
	return padToHeight(sb.String(), b.contentHeight)
}

func (b *boardsModel) renderDetail() string {
	var sb strings.Builder
	switch b.detail.leaf {
	case "unset":
		count := b.syntheticLeafTaskCount()
		sb.WriteString(dashboardLine(b.width, fmt.Sprintf("%d %s with no %s", count, pluralTasks(count), b.ns)))
		sb.WriteString("\n")
		sb.WriteString(dashboardLine(b.width, b.m.styles.Muted.Render("[Esc] back to chart")))
		return padToHeight(sb.String(), b.contentHeight)
	}
	r := b.detail.row
	sb.WriteString(dashboardLine(b.width, fmt.Sprintf("name        %s", r.full)))
	sb.WriteString("\n")
	sb.WriteString(dashboardLine(b.width, fmt.Sprintf("usage       %d %s", r.usage, pluralUses(r.usage))))
	sb.WriteString("\n")
	desc := r.description
	if desc == "" {
		desc = b.m.styles.Warning.Render("needs description")
	}
	sb.WriteString(dashboardLine(b.width, fmt.Sprintf("description %s", desc)))
	return padToHeight(sb.String(), b.contentHeight)
}

// activeNamespaceTaskCount counts distinct project tasks carrying the active
// namespace. Chart rows intentionally retain membership counts, so their
// sum is not suitable for the headline.
func (b *boardsModel) activeNamespaceTaskCount() int {
	scope := b.m.projectScope
	count := 0
	for _, tk := range b.m.store.ListTasks(store.QueryFilters{Project: scope}) {
		for _, full := range tk.Labels {
			if strings.HasPrefix(full, scope+":"+b.ns+":") {
				count++
				break
			}
		}
	}
	return count
}

func (b *boardsModel) syntheticLeafTaskCount() int {
	count := 0
	for _, tk := range b.m.store.ListTasks(store.QueryFilters{Project: b.m.projectScope}) {
		if b.detail.leaf != "unset" {
			continue
		}
		hasNamespace := false
		for _, full := range tk.Labels {
			if strings.HasPrefix(full, b.m.projectScope+":"+b.ns+":") {
				hasNamespace = true
				break
			}
		}
		if !hasNamespace {
			count++
		}
	}
	return count
}

func (b *boardsModel) statusHint() string {
	if b.m.projectScope == "" {
		return "[?]keys"
	}
	switch b.level {
	case lLevelChart:
		return "[Enter]inspect [d]esc [l]remove [Esc]back"
	case lLevelDetail:
		if b.detail.leaf != "" {
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
	if err := m.store.LabelAdd(full, desc, "", m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	return nil
}
