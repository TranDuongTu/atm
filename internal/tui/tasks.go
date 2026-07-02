package tui

import (
	"fmt"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

type tasksModel struct {
	m     *Model
	view  tView

	// list state (flat + grouped)
	rows      []taskRow
	groups    []taskGroup
	others    []taskRow
	cursor    int
	offset    int
	pageSize  int

	// filter / sort
	filter     string
	filterEdit string
	filterEditing bool
	sortMode   sortMode

	// detail
	detail taskDetailState
}

type tView int

const (
	tViewList tView = iota
	tViewDetail
)

type sortMode int

const (
	sortUpdatedDesc sortMode = iota
	sortUpdatedAsc
	sortIDAsc
)

func (s sortMode) String() string {
	switch s {
	case sortUpdatedDesc:
		return "updated-desc"
	case sortUpdatedAsc:
		return "updated-asc"
	case sortIDAsc:
		return "id-asc"
	}
	return "?"
}

type taskRow struct {
	id      string
	title   string
	labels  []string
	updated string
	task    *store.Task
}

type taskGroup struct {
	label string
	rows  []taskRow
	// collapsed controls group-header expand/collapse.
	collapsed bool
}

type taskDetailState struct {
	id      string
	task    *store.Task
	lines   []string
	offset  int
}

func newTasksModel(m *Model) tasksModel {
	return tasksModel{m: m, sortMode: sortUpdatedDesc}
}

func (t *tasksModel) SetSize(w, h int) {
	_ = w
	t.pageSize = h - 4 // header(1) + col-header(1) + separator(1) + footer(1)
	if t.pageSize < 1 {
		t.pageSize = 1
	}
}

func (t *tasksModel) refresh() {
	t.rows = nil
	t.groups = nil
	t.others = nil
	if t.m.projectScope == "" {
		t.clampCursor()
		return
	}
	filters := t.parseFilter()
	ts := t.m.store.ListTasks(store.QueryFilters{Project: t.m.projectScope, Labels: filters})
	ts = t.applySort(ts)
	wildcards := wildcardTokens(filters)
	if len(wildcards) > 0 {
		// Grouped view.
		groups, others := t.m.store.GroupTasks(store.QueryFilters{Project: t.m.projectScope, Labels: filters})
		for _, g := range groups {
			rows := make([]taskRow, 0, len(g.Tasks))
			for _, tk := range g.Tasks {
				rows = append(rows, t.toRow(tk))
			}
			t.groups = append(t.groups, taskGroup{label: g.Label, rows: rows})
		}
		for _, tk := range others {
			t.others = append(t.others, t.toRow(tk))
		}
	} else {
		for _, tk := range ts {
			t.rows = append(t.rows, t.toRow(tk))
		}
	}
	t.clampCursor()
}

func (t *tasksModel) toRow(tk *store.Task) taskRow {
	return taskRow{
		id:      tk.ID,
		title:   tk.Title,
		labels:  tk.Labels,
		updated: relTime(tk.UpdatedAt, store.Now()),
		task:    tk,
	}
}

func (t *tasksModel) applySort(ts []*store.Task) []*store.Task {
	out := make([]*store.Task, len(ts))
	copy(out, ts)
	switch t.sortMode {
	case sortUpdatedDesc:
		// stable: most recent first
		// Use insertion-stable by index after a manual compare.
		for i := 1; i < len(out); i++ {
			for j := i; j > 0; j-- {
				if out[j].UpdatedAt.After(out[j-1].UpdatedAt) {
					out[j], out[j-1] = out[j-1], out[j]
				}
			}
		}
	case sortUpdatedAsc:
		for i := 1; i < len(out); i++ {
			for j := i; j > 0; j-- {
				if out[j].UpdatedAt.Before(out[j-1].UpdatedAt) {
					out[j], out[j-1] = out[j-1], out[j]
				}
			}
		}
	case sortIDAsc:
		// store already returns id-asc; no-op
	}
	return out
}

// parseFilter splits the filter string on spaces; tokens ending `:*` are
// wildcards (facets), others are exact restrictors.
func (t *tasksModel) parseFilter() []string {
	s := strings.TrimSpace(t.filter)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func (t *tasksModel) hasWildcard() bool {
	for _, tok := range t.parseFilter() {
		if isWildcardTUI(tok) {
			return true
		}
	}
	return false
}

func isWildcardTUI(l string) bool { return strings.HasSuffix(l, ":*") }

func wildcardTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if isWildcardTUI(l) {
			out = append(out, l)
		}
	}
	return out
}

func (t *tasksModel) clampCursor() {
	if t.cursor < 0 {
		t.cursor = 0
	}
	// For grouped view, the cursor indexes into a flattened list of
	// (group header, group rows, others header, others rows). We compute that
	// lazily in render; clamp to total line count.
	total := t.flatLineCount()
	if t.cursor >= total {
		t.cursor = total - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

func (t *tasksModel) handleKey(k tea.KeyMsg) tea.Cmd {
	// Filter editing takes priority (header is the input).
	if t.filterEditing {
		return t.handleFilterEditKey(k)
	}
	switch t.view {
	case tViewList:
		return t.handleListKey(k)
	case tViewDetail:
		return t.handleDetailKey(k)
	}
	return nil
}

func (t *tasksModel) handleFilterEditKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "enter":
		t.filter = t.filterEdit
		t.filterEditing = false
		t.cursor = 0
		t.refresh()
		return nil
	case "esc":
		t.cancelFilterEdit()
		return nil
	case "backspace":
		if len(t.filterEdit) > 0 {
			t.filterEdit = t.filterEdit[:len(t.filterEdit)-1]
		}
		return nil
	case " ":
		t.filterEdit += " "
		return nil
	}
	if k.Type == tea.KeyRunes {
		t.filterEdit += string(k.Runes)
	}
	return nil
}

func (t *tasksModel) cancelFilterEdit() {
	t.filterEditing = false
	t.filterEdit = ""
}

func (t *tasksModel) handleListKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		t.cursorDown()
	case "k", "up":
		t.cursorUp()
	case "g":
		t.cursor = 0
		t.offset = 0
	case "/":
		if t.m.projectScope == "" {
			return nil
		}
		t.filterEditing = true
		t.filterEdit = t.filter
	case "s":
		// cycle sort
		t.sortMode = (t.sortMode + 1) % 3
		t.refresh()
	case "a":
		if !t.m.canMutate() {
			return nil
		}
		if t.m.projectScope == "" {
			return nil
		}
		t.openCreateForm()
	case "enter":
		if t.hasWildcard() {
			return t.toggleGroupAtCursor()
		}
		return t.openDetailAtCursor()
	}
	return nil
}

func (t *tasksModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		t.detail.offset++
		t.clampDetail()
	case "k", "up":
		if t.detail.offset > 0 {
			t.detail.offset--
		}
	case "g":
		t.detail.offset = 0
	case "pgdown", " ":
		t.detail.offset += t.m.contentHeight / 2
		t.clampDetail()
	case "pgup":
		if t.detail.offset > t.m.contentHeight/2 {
			t.detail.offset -= t.m.contentHeight / 2
		} else {
			t.detail.offset = 0
		}
	case "e":
		if !t.m.canMutate() {
			return nil
		}
		t.openTitleForm()
	case "d":
		if !t.m.canMutate() {
			return nil
		}
		t.openDescriptionForm()
	case "b":
		if !t.m.canMutate() {
			return nil
		}
		t.openLabelAddForm()
	case "B":
		if !t.m.canMutate() {
			return nil
		}
		t.openLabelRemoveForm()
	case "x":
		if !t.m.canMutate() {
			return nil
		}
		return t.requestRemoveTask()
	}
	return nil
}

// --- cursor navigation ---

func (t *tasksModel) cursorDown() {
	total := t.flatLineCount()
	if t.cursor < total-1 {
		t.cursor++
	}
}

func (t *tasksModel) cursorUp() {
	if t.cursor > 0 {
		t.cursor--
	}
}

// flatLineCount returns the number of logical lines (headers + rows) the
// list view presents — used for cursor bounds and paging.
func (t *tasksModel) flatLineCount() int {
	if t.hasWildcard() {
		n := 0
		for _, g := range t.groups {
			n++ // header
			if !g.collapsed {
				n += len(g.rows)
			}
		}
		n++ // (no matching labels) header
		n += len(t.others)
		return n
	}
	return len(t.rows)
}

func (t *tasksModel) openDetailAtCursor() tea.Cmd {
	if !t.hasWildcard() {
		if t.cursor >= 0 && t.cursor < len(t.rows) {
			return t.openDetail(t.rows[t.cursor].id)
		}
	}
	return nil
}

func (t *tasksModel) toggleGroupAtCursor() tea.Cmd {
	if !t.hasWildcard() {
		return nil
	}
	idx := 0
	for gi := range t.groups {
		if idx == t.cursor {
			t.groups[gi].collapsed = !t.groups[gi].collapsed
			return nil
		}
		idx++ // header
		if !t.groups[gi].collapsed {
			idx += len(t.groups[gi].rows)
		}
	}
	// (no matching labels) header is not collapsible.
	return nil
}

func (t *tasksModel) openDetail(id string) tea.Cmd {
	tk, err := t.m.store.GetTask(id)
	if err != nil {
		t.m.showToast("error: " + err.Error())
		return nil
	}
	t.detail = taskDetailState{id: id, task: tk}
	t.view = tViewDetail
	t.renderDetail()
	return nil
}

func (t *tasksModel) backToList() {
	t.view = tViewList
	t.detail = taskDetailState{}
}

func (t *tasksModel) renderDetail() {
	var b strings.Builder
	tk := t.detail.task
	if tk == nil {
		return
	}
	b.WriteString("TASK\n")
	b.WriteString(sepLine("─", 78, t.m.width, 2))
	b.WriteString("\n")
	fmt.Fprintf(&b, "id           %s\n", tk.ID)
	fmt.Fprintf(&b, "project      %s\n", tk.ProjectCode)
	fmt.Fprintf(&b, "title        %s                            [e] edit\n", tk.Title)
	if tk.Description == "" {
		b.WriteString("description  (none)                                    [d] edit\n")
	} else {
		for i, line := range strings.Split(tk.Description, "\n") {
			if i == 0 {
				fmt.Fprintf(&b, "description  %s                            [d] edit\n", line)
			} else {
				fmt.Fprintf(&b, "             %s\n", line)
			}
		}
	}
	fmt.Fprintf(&b, "created      %s   by %s\n", store.RFC3339UTC(tk.CreatedAt), tk.CreatedBy)
	fmt.Fprintf(&b, "updated      %s   by %s\n", store.RFC3339UTC(tk.UpdatedAt), tk.UpdatedBy)
	b.WriteString("\n")

	b.WriteString("LABELS\n")
	b.WriteString(sepLine("─", 78, t.m.width, 2))
	b.WriteString("\n")
	if len(tk.Labels) == 0 {
		b.WriteString(" (no labels)\n")
	} else {
		chips := strings.Join(tk.Labels, "   ")
		b.WriteString(" " + chips + "\n")
	}
	b.WriteString("                                      [b] add label   [B] remove label\n")
	b.WriteString("\n")

	b.WriteString("HISTORY\n")
	b.WriteString(sepLine("─", 78, t.m.width, 2))
	b.WriteString("\n")
	for _, h := range tk.History {
		fmt.Fprintf(&b, " %-3s %s   %s     %s\n", h.ID, store.RFC3339UTC(h.At), h.Actor, h.Action)
		if len(h.Meta) > 0 {
			fmt.Fprintf(&b, "      meta: %s\n", metaJSON(h.Meta))
		}
	}
	t.detail.lines = strings.Split(b.String(), "\n")
	t.clampDetail()
}

func (t *tasksModel) clampDetail() {
	maxOff := len(t.detail.lines) - t.m.contentHeight
	if maxOff < 0 {
		maxOff = 0
	}
	if t.detail.offset > maxOff {
		t.detail.offset = maxOff
	}
	if t.detail.offset < 0 {
		t.detail.offset = 0
	}
}

// --- view ---

func (t *tasksModel) View() string {
	switch t.view {
	case tViewList:
		return t.renderList()
	case tViewDetail:
		return t.renderDetailView()
	}
	return ""
}

func (t *tasksModel) headerLine() string {
	proj := t.m.projectScope
	if proj == "" {
		proj = "(none)"
	}
	filt := t.filter
	if t.filterEditing {
		filt = t.filterEdit + "_"
	}
	if filt == "" {
		filt = "(none)"
	}
	if t.filterEditing && t.filterEdit == "" {
		filt = "_"
	}
	return fmt.Sprintf("PROJECT: %s    FILTER: %s    SORT: %s", proj, filt, t.sortMode)
}

func (t *tasksModel) renderList() string {
	var b strings.Builder
	b.WriteString(headerLineStyle.Render(t.headerLine()))
	b.WriteString("\n")

	if t.m.projectScope == "" {
		empty := "\nno project selected\n\npress [s] in the Projects tab to scope this view"
		b.WriteString(centerBlock(empty, t.m.width))
		return padToHeight(b.String(), t.m.contentHeight)
	}

	if t.hasWildcard() {
		t.renderGroupedList(&b)
	} else {
		t.renderFlatList(&b)
	}
	return padToHeight(b.String(), t.m.contentHeight)
}

func (t *tasksModel) renderFlatList(b *strings.Builder) {
	if len(t.rows) == 0 {
		empty := fmt.Sprintf("\nno tasks match this filter\n\nno task carries %s\n\n[/] to edit filter, or clear it to see all tasks",
			strings.Join(t.parseFilter(), " and "))
		b.WriteString(centerBlock(empty, t.m.width))
		return
	}
	// Column header.
	b.WriteString(headerLabelStyle.Render(fmt.Sprintf(" %-10s %-40s %-30s %10s", "ID", "TITLE", "LABELS", "UPDATED")))
	b.WriteString("\n")
	b.WriteString(sepLine("─", 78, t.m.width, 2))
	b.WriteString("\n")
	// Page window.
	start, end := t.pageWindow(len(t.rows))
	for i := start; i < end; i++ {
		r := t.rows[i]
		labels := "-"
		if len(r.labels) > 0 {
			labels = truncateRunes(strings.Join(r.labels, " "), 30)
		}
		line := fmt.Sprintf(" %-10s %-40s %-30s %10s", r.id, truncateRunes(r.title, 40), labels, r.updated)
		if i == t.cursor {
			line = " " + rowCursorStyle.Render(line)
		} else {
			line = " " + line
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf(" showing %d-%d of %d", start+1, end, len(t.rows)))
}

func (t *tasksModel) renderGroupedList(b *strings.Builder) {
	// Check the wildcard-yields-no-labels state.
	if len(t.groups) == 0 {
		b.WriteString(centerBlock("no labels match wildcard — add labels to tasks", t.m.width))
		b.WriteString("\n")
	}
	idx := 0
	for _, g := range t.groups {
		marker := "▾"
		if g.collapsed {
			marker = "▸"
		}
		header := groupHeaderStyle.Render(fmt.Sprintf("%s %s (%d)", marker, g.label, len(g.rows)))
		if idx == t.cursor {
			header = rowCursorStyle.Render(header)
		}
		b.WriteString(header)
		b.WriteString("\n")
		idx++
		if !g.collapsed {
			for _, r := range g.rows {
				// Grouped rows omit the LABELS column (group header is the axis).
				line := fmt.Sprintf("  %-10s %-50s %10s", r.id, truncateRunes(r.title, 50), r.updated)
				if idx == t.cursor {
					line = " " + rowCursorStyle.Render(strings.TrimPrefix(line, " "))
				}
				b.WriteString(line)
				b.WriteString("\n")
				idx++
			}
		}
	}
	// (no matching labels) bucket — always rendered, last.
	header := groupHeaderStyle.Render(fmt.Sprintf("▾ (no matching labels) (%d)", len(t.others)))
	if idx == t.cursor {
		header = rowCursorStyle.Render(header)
	}
	b.WriteString(header)
	b.WriteString("\n")
	idx++
	for _, r := range t.others {
		labels := "(no labels)"
		if len(r.labels) > 0 {
			labels = truncateRunes(strings.Join(r.labels, " "), 30)
		}
		line := fmt.Sprintf("  %-10s %-40s %-30s %10s", r.id, truncateRunes(r.title, 40), labels, r.updated)
		if idx == t.cursor {
			line = " " + rowCursorStyle.Render(strings.TrimPrefix(line, " "))
		}
		b.WriteString(line)
		b.WriteString("\n")
		idx++
	}
}

func (t *tasksModel) renderDetailView() string {
	end := t.detail.offset + t.m.contentHeight
	if end > len(t.detail.lines) {
		end = len(t.detail.lines)
	}
	var b strings.Builder
	for i := t.detail.offset; i < end; i++ {
		b.WriteString(t.detail.lines[i])
		b.WriteString("\n")
	}
	return b.String()
}

func (t *tasksModel) pageWindow(total int) (int, int) {
	start := 0
	// keep cursor in view
	if t.cursor >= start+t.pageSize {
		start = t.cursor - t.pageSize + 1
	}
	if t.cursor < start {
		start = t.cursor
	}
	if start < 0 {
		start = 0
	}
	end := start + t.pageSize
	if end > total {
		end = total
	}
	return start, end
}

func (t *tasksModel) statusHint() string {
	if t.m.projectScope == "" {
		return "[?]keys"
	}
	if t.view == tViewDetail {
		return "[e]title [d]desc [b]add label [B]remove label [x]remove [Esc]back"
	}
	hint := "[/]filter [s]sort [a]dd [Enter]detail [?]keys"
	if t.filterEditing {
		hint = "[Enter]apply [Esc]cancel"
	}
	return hint
}

// --- form openers ---

func (t *tasksModel) openCreateForm() {
	fields := []formField{
		{Label: "title", Required: true, Hint: "task title"},
		{Label: "description", Required: false, Hint: "optional; multi-line later"},
	}
	f := NewForm("New task  "+t.m.projectScope+":", fields)
	f.Title = "New task  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskCreate
}

func (t *tasksModel) openTitleForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	fields := []formField{
		{Label: "title", Required: true, Value: tk.Title, Hint: "new task title"},
	}
	f := NewForm("Edit title", fields)
	t.m.form = f
	t.m.formKind = formTaskSetTitle
}

func (t *tasksModel) openDescriptionForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	fields := []formField{
		{Label: "description", Required: false, Value: tk.Description, Hint: "new description (empty clears)"},
	}
	f := NewForm("Edit description", fields)
	t.m.form = f
	t.m.formKind = formTaskSetDescription
}

func (t *tasksModel) openLabelAddForm() {
	tk := t.detail.task
	if tk == nil {
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
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm("Add label  "+t.m.projectScope+":", fields)
	f.Title = "Add label  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskLabelAdd
}

func (t *tasksModel) openLabelRemoveForm() {
	tk := t.detail.task
	if tk == nil {
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
	f := NewForm("Remove label  "+t.m.projectScope+":", fields)
	f.Title = "Remove label  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskLabelRemove
}

func (t *tasksModel) requestRemoveTask() tea.Cmd {
	t.m.confirm = confirmRemoveTask
	t.m.confirmMsg = fmt.Sprintf("Remove task %s?", t.detail.id)
	t.m.confirmArg = "History is lost. Registry labels are unaffected."
	return nil
}

// --- mutations ---

func (m *Model) doTaskCreate(vals map[string]string) tea.Cmd {
	title := vals["title"]
	desc := vals["description"]
	tk, err := m.store.CreateTask(m.projectScope, title, desc, nil, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	if tk != nil {
		m.tasks.openDetail(tk.ID)
	}
	return nil
}

func (m *Model) doTaskSetTitle(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	title := vals["title"]
	if err := m.store.SetTitle(id, title, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}

func (m *Model) doTaskSetDescription(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	desc := vals["description"]
	if err := m.store.SetDescription(id, desc, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}

func (m *Model) doTaskLabelAdd(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	suffix := vals["name"]
	full := m.projectScope + ":" + suffix
	if err := m.store.TaskLabelAdd(id, full, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}

func (m *Model) doTaskLabelRemove(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	suffix := vals["name"]
	full := m.projectScope + ":" + suffix
	if err := m.store.TaskLabelRemove(id, full, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}