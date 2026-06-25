package tui

import (
	"fmt"
	"os"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type workspacePane int

const (
	paneProjects workspacePane = iota
	paneTasks
	paneSummary
	paneHelp
)

type focusMode int

const (
	focusList focusMode = iota
	focusFilter
	focusForm
	focusOverlay
)

type Model struct {
	store         *store.Store
	storeSet      bool
	actor         string
	width         int
	height        int
	contentHeight int
	leftWidth     int
	rightWidth    int
	focused       workspacePane
	km            keymap
	projectScope  string

	dash     *dashboardModel
	projects *projectsModel
	tasks    *tasksModel
	help     *helpModel

	filter   filterState
	toast    toastState
	overlay  overlayState
	form     formState
	startup  startupState
	quitting bool
}

type filterState struct {
	active bool
	value  string
}

type toastState struct {
	msg string
}

type overlayState struct {
	visible bool
	title   string
	lines   []string
}

type formState struct {
	active   bool
	kind     string
	form     *Form
	onSubmit func(*Form) tea.Cmd
}

type startupState struct {
	promptActor bool
	promptInit  bool
	actorInput  string
	pathInput   string
	mode        string
}

type refreshMsg struct{}

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

type NewModelOpts struct {
	StorePath string
	Actor     string
}

func NewModel(opts NewModelOpts) (*Model, error) {
	root := store.ResolveStorePath(opts.StorePath)
	s, err := store.Open(root)
	if err != nil {
		return nil, err
	}
	m := &Model{
		store:  s,
		km:     defaultKeymap(),
		width:  100,
		height: 30,
	}
	m.actor = opts.Actor
	m.dash = newDashboardModel(m)
	m.projects = newProjectsModel(m)
	m.tasks = newTasksModel(m)
	m.help = newHelpModel(m)
	m.SetSize(m.width, m.height)

	storeExists := storeExists(s)
	m.startup.promptInit = !storeExists
	if storeExists {
		m.storeSet = true
		if m.actor == "" {
			m.startup.promptActor = true
			m.startup.mode = "actor"
		} else {
			m.refreshAll()
		}
	}
	return m, nil
}

func storeExists(s *store.Store) bool {
	if s == nil {
		return false
	}
	if _, err := os.Stat(s.StorePath()); err != nil {
		return false
	}
	projectsDir := s.StorePath() + "/projects"
	if _, err := os.Stat(projectsDir); err != nil {
		return false
	}
	return true
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	// Reserve space for top (header) and bottom (footer) bordered
	// panes plus their surrounding borders. Each bordered pane occupies
	// (1 top border + content rows + 1 bottom border) lines.
	topH := topPaneHeight(m.width)
	bottomH := bottomPaneHeight(m.width)
	contentH := h - topH - bottomH
	if contentH < 2 {
		contentH = 2
	}
	m.contentHeight = contentH
	contentW := w
	if contentW < 20 {
		contentW = 20
	}
	leftW := contentW * 30 / 100
	if leftW < 24 {
		leftW = 24
	}
	if leftW > 44 {
		leftW = 44
	}
	if leftW > contentW-20 {
		leftW = contentW / 2
	}
	rightW := contentW - leftW
	if rightW < 20 {
		rightW = 20
	}
	m.leftWidth = leftW
	m.rightWidth = rightW
	m.dash.setSize(rightW, contentH)
	m.projects.setSize(rightW, contentH)
	m.tasks.setSize(rightW, contentH)
	m.help.setSize(rightW, contentH)
}

// topPaneHeight is the outer height (including borders) of the top pane for
// the current width. The header is two logical lines (title, location); with a
// rounded border that yields 1 + 2 + 1 = 4 lines.
func topPaneHeight(w int) int { return 4 }

// bottomPaneHeight is the outer height (including borders) of the bottom pane.
// The footer (status bar) is two logical lines (key menu + status line) ->
// 1 + 2 + 1 = 4 lines.
func bottomPaneHeight(w int) int { return 4 }

func (m *Model) refreshAll() {
	m.dash.refresh()
	m.projects.refresh()
	m.tasks.refresh()
	m.help.refresh()
}

func (m *Model) focusedPaneName() string {
	switch m.focused {
	case paneProjects:
		return "Projects"
	case paneTasks:
		return "Tasks"
	case paneSummary:
		return "Summary"
	case paneHelp:
		return "Help"
	default:
		return "Projects"
	}
}

func (m *Model) selectedProjectCode() string {
	return m.projectScope
}

func (m *Model) showToast(msg string) {
	m.toast.msg = msg
}

func (m *Model) hideToast() {
	m.toast.msg = ""
}

func (m *Model) showOverlay(title string, lines []string) {
	m.overlay.title = title
	m.overlay.lines = lines
	m.overlay.visible = true
}

func (m *Model) hideOverlay() {
	m.overlay.visible = false
}

func (m *Model) openForm(kind string, f *Form, onSubmit func(*Form) tea.Cmd) {
	m.form.kind = kind
	m.form.form = f
	m.form.onSubmit = onSubmit
	m.form.active = true
	f.SetWidth(m.width)
}

func (m *Model) closeForm() {
	m.form.active = false
	m.form.form = nil
	m.form.onSubmit = nil
}

func (m *Model) requireActor() bool {
	return m.actor != ""
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case errMsg:
		m.showToast(fmt.Sprintf("error: %v", msg.err))
		return m, nil
	case refreshMsg:
		m.refreshAll()
		return m, nil
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}

	if m.startup.promptInit || m.startup.promptActor {
		return m.handleStartup(msg)
	}

	if m.form.active {
		return m.handleFormKey(msg)
	}

	if m.overlay.visible {
		if msg.String() == m.km.escape || msg.String() == "q" || msg.String() == "enter" {
			m.hideOverlay()
			return m, nil
		}
		return m, nil
	}

	if m.filter.active {
		return m.handleFilterKey(msg)
	}

	key := msg.String()
	switch key {
	case m.km.quit:
		m.quitting = true
		return m, tea.Quit
	case m.km.refresh:
		m.refreshAll()
		m.showToast("refreshed")
		return m, nil
	case m.km.help:
		m.focused = paneHelp
		return m, nil
	case m.km.palette:
		m.showToast("command palette: not yet implemented")
		return m, nil
	case m.km.filter:
		m.filter.active = true
		m.filter.value = ""
		return m, nil
	case m.km.escape:
		// Clear any status message; otherwise let the active pane handle
		// Esc (e.g. exit a project/task detail view).
		if m.toast.msg != "" {
			m.hideToast()
			return m, nil
		}
	case "1":
		m.focused = paneProjects
		return m, nil
	case "2":
		m.focused = paneTasks
		return m, nil
	case "3":
		m.focused = paneSummary
		return m, nil
	case "4":
		m.focused = paneHelp
		return m, nil
	case " ":
		if m.focused == paneProjects {
			if code, ok := m.projects.selectedCode(); ok {
				if m.projectScope == code {
					m.projectScope = ""
					m.showToast("scope: all")
				} else {
					m.projectScope = code
					m.showToast("scope: " + code)
				}
				m.refreshAll()
			}
			return m, nil
		}
	}

	return m.dispatchPane(msg)
}

func (m *Model) handleStartup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.startup.promptInit {
		switch key {
		case "i", "I":
			if err := m.store.Init(""); err != nil {
				return m, func() tea.Msg { return errMsg{err} }
			}
			m.storeSet = true
			m.startup.promptInit = false
			if m.actor == "" {
				m.startup.promptActor = true
				m.startup.mode = "actor"
			} else {
				m.refreshAll()
			}
			return m, nil
		case "q", "Q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}
	if m.startup.promptActor {
		switch key {
		case "enter":
			actor := m.startup.actorInput
			if err := store.ValidateActorID(actor); err != nil {
				m.showToast(fmt.Sprintf("2 usage: %v", err))
				return m, nil
			}
			m.actor = actor
			m.startup.promptActor = false
			m.startup.actorInput = ""
			m.refreshAll()
			return m, nil
		case "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "backspace":
			if len(m.startup.actorInput) > 0 {
				m.startup.actorInput = m.startup.actorInput[:len(m.startup.actorInput)-1]
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes {
				m.startup.actorInput += string(msg.Runes)
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f, _ := m.form.form.Update(msg)
	m.form.form = f
	if f.Done {
		onSubmit := m.form.onSubmit
		m.closeForm()
		if onSubmit != nil {
			return m, onSubmit(f)
		}
		return m, nil
	}
	if f.Cancel {
		m.closeForm()
	}
	return m, nil
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.filter.active = false
		m.filter.value = ""
		m.applyFilter("")
		return m, nil
	case "enter":
		m.filter.active = false
		m.applyFilter(m.filter.value)
		return m, nil
	case "backspace":
		if len(m.filter.value) > 0 {
			m.filter.value = m.filter.value[:len(m.filter.value)-1]
			m.applyFilter(m.filter.value)
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.filter.value += string(msg.Runes)
			m.applyFilter(m.filter.value)
		}
		return m, nil
	}
}

func (m *Model) applyFilter(v string) {
	switch m.focused {
	case paneProjects:
		m.projects.setFilter(v)
	case paneTasks:
		m.tasks.setFilter(v)
	case paneSummary:
		m.dash.setFilter(v)
	}
}

func (m *Model) dispatchPane(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.focused {
	case paneProjects:
		return m.projects.update(msg)
	case paneTasks:
		return m.tasks.update(msg)
	case paneSummary:
		return m.dash.update(msg)
	case paneHelp:
		return m.help.update(msg)
	}
	return m, nil
}

func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.startup.promptInit {
		return m.renderStartupInit()
	}
	if m.startup.promptActor {
		return m.renderStartupActor()
	}

	w := m.width
	if w < 20 {
		w = 20
	}
	h := m.height

	// Fullscreen stack: title bar (header), content, status bar.
	top := box(topStyle, w, m.renderHeader())
	content := m.renderContent()
	bottom := box(bottomStyle, w, m.renderFooter())

	// Stretch content so the layout fills the terminal; lipgloss.Place then
	// drops the floating status bar at the absolute bottom.
	stack := top + "\n" + content
	out := lipgloss.Place(w, h, lipgloss.Left, lipgloss.Top, stack)

	// Floating status bar at the bottom.
	out = placeSafe(out, bottom, w, h, lipgloss.Center, lipgloss.Bottom)

	if m.form.active && m.form.form != nil {
		out = placeSafe(out, m.form.form.View(), w, h, lipgloss.Center, lipgloss.Center)
	}
	if m.overlay.visible {
		out = placeSafe(out, m.renderOverlayBox(), w, h, lipgloss.Center, lipgloss.Center)
	}
	return out
}

// placeSafe composites `overlay` onto `base` at the given position. Each
// overlay line is placed onto its target row using lipgloss.PlaceHorizontal
// (ANSI-safe), so styled content is never split mid-sequence. Base rows that
// are entirely covered by the overlay are replaced; partially covered rows
// keep their non-overlapping edges.
func placeSafe(base, overlay string, w, h int, hPos, vPos lipgloss.Position) string {
	olHeight := strings.Count(overlay, "\n") + 1
	olWidth := maxLineWidth(overlay)
	var x, y int
	switch hPos {
	case lipgloss.Right:
		x = w - olWidth
	default:
		x = (w - olWidth) / 2
	}
	switch vPos {
	case lipgloss.Top:
		y = 0
	case lipgloss.Bottom:
		y = h - olHeight
	default:
		y = (h - olHeight) / 2
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	baseLines := strings.Split(base, "\n")
	for len(baseLines) < h {
		baseLines = append(baseLines, strings.Repeat(" ", w))
	}
	if len(baseLines) > h {
		baseLines = baseLines[:h]
	}

	ovLines := strings.Split(overlay, "\n")
	for i, ol := range ovLines {
		yy := y + i
		if yy < 0 || yy >= len(baseLines) {
			continue
		}
		// Build the overlay row inside a w-wide line: left pad + overlay + right pad.
		// lipgloss.PlaceHorizontal handles the styling safely and pads with spaces.
		placed := lipgloss.PlaceHorizontal(w, hPos, ol)
		baseLines[yy] = placed
	}
	return strings.Join(baseLines, "\n")
}

func maxLineWidth(s string) int {
	maxw := 0
	for _, l := range strings.Split(s, "\n") {
		if w := lipgloss.Width(l); w > maxw {
			maxw = w
		}
	}
	return maxw
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Model) renderStartupInit() string {
	var b strings.Builder
	b.WriteString("atm\n")
	b.WriteString(strings.Repeat("-", m.width))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("  No store found at %s\n\n", m.store.StorePath()))
	b.WriteString("  [I]nit here   [Q]uit\n\n")
	b.WriteString(strings.Repeat("-", m.width))
	return b.String()
}

func (m *Model) renderStartupActor() string {
	var b strings.Builder
	b.WriteString("atm\n")
	b.WriteString(strings.Repeat("-", m.width))
	b.WriteString("\n\n")
	b.WriteString("  Actor id required (e.g. human:alice, agent:claude-1)\n\n")
	b.WriteString("  actor: " + m.startup.actorInput + "_\n\n")
	b.WriteString("  [Enter] continue   [Esc] quit\n\n")
	b.WriteString(strings.Repeat("-", m.width))
	return b.String()
}

func (m *Model) renderHeader() string {
	title := titleStyle.Render(" Agents Tasks Management - ATM ")
	scope := "all"
	if m.projectScope != "" {
		scope = m.projectScope
	}
	loc := locationStyle.Render(fmt.Sprintf(" scope: %s  actor: %s  store: %s  r refresh  q quit ", scope, m.actorString(), m.store.StorePath()))
	// Stack the two lines, each center-aligned across the full width.
	innerW := m.width - 2
	if innerW < 1 {
		innerW = 1
	}
	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.PlaceHorizontal(innerW, lipgloss.Center, title),
		lipgloss.PlaceHorizontal(innerW, lipgloss.Center, loc),
	)
}

func (m *Model) actorString() string {
	if m.actor == "" {
		return "(unset)"
	}
	return m.actor
}

func (m *Model) renderContent() string {
	left := lipgloss.NewStyle().Width(m.leftWidth).Render(m.renderLeftColumn())
	right := lipgloss.NewStyle().Width(m.rightWidth).Render(limitBlockHeight(m.renderRightColumn(), m.contentHeight))
	content := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return limitBlockHeight(content, m.contentHeight)
}

func (m *Model) renderLeftColumn() string {
	heights := splitHeights(m.contentHeight, 4)
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderPane("[1] - Projects", m.renderLeftProjects(), m.leftWidth, heights[0], m.focused == paneProjects),
		m.renderPane("[2] - Tasks", m.renderLeftTasks(), m.leftWidth, heights[1], m.focused == paneTasks),
		m.renderPane("[3] - Summary", m.renderLeftSummary(), m.leftWidth, heights[2], m.focused == paneSummary),
		m.renderPane("[4] - Help", m.renderLeftHelp(), m.leftWidth, heights[3], m.focused == paneHelp),
	)
}

func (m *Model) renderLeftProjects() string {
	var b strings.Builder
	for i, pr := range m.projects.filtered() {
		if i >= 4 {
			break
		}
		cursor := " "
		if m.focused == paneProjects && i == m.projects.cursor {
			cursor = ">"
		}
		scope := " "
		if m.projectScope == pr.Code {
			scope = "*"
		}
		b.WriteString(fmt.Sprintf("  %s%s %-12s\n", cursor, scope, pr.Code))
	}
	return b.String()
}

func (m *Model) renderLeftTasks() string {
	var b strings.Builder
	for i, tk := range m.tasks.filtered() {
		if i >= 5 {
			break
		}
		cursor := " "
		if m.focused == paneTasks && i == m.tasks.cursor {
			cursor = ">"
		}
		title := tk.Title
		if len(title) > 18 {
			title = title[:18]
		}
		b.WriteString(fmt.Sprintf("  %s %-10s %s\n", cursor, tk.ID, title))
	}
	return b.String()
}

func (m *Model) renderLeftSummary() string {
	var b strings.Builder
	filters := store.QueryFilters{Project: m.projectScope}
	tasks := m.store.ListTasks(filters)
	review := 0
	for _, tk := range tasks {
		if tk.Status == "review" {
			review++
		}
	}
	scope := "all projects"
	if m.projectScope != "" {
		scope = m.projectScope
	}
	b.WriteString(fmt.Sprintf("%s: open %d review %d\n", scope, len(tasks), review))
	return b.String()
}

func (m *Model) renderLeftHelp() string {
	return "keys, forms, parity\n"
}

func (m *Model) renderRightColumn() string {
	switch m.focused {
	case paneProjects:
		return m.projects.rightView()
	case paneTasks:
		return m.tasks.rightView()
	case paneSummary:
		return m.dash.view()
	case paneHelp:
		return m.help.view()
	}
	return ""
}

func (m *Model) renderPane(title, body string, width int, height int, active bool) string {
	style := paneStyle
	if active {
		style = activePaneStyle
	}
	return titledBoxHeight(style, width, title, body, height)
}

func splitHeights(total, n int) []int {
	if n <= 0 {
		return nil
	}
	heights := make([]int, n)
	base := total / n
	rem := total % n
	for i := range heights {
		heights[i] = base
		if i < rem {
			heights[i]++
		}
		if heights[i] < 3 {
			heights[i] = 3
		}
	}
	return heights
}

func limitBlockHeight(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderFooter() string {
	actor := m.actor
	if actor == "" {
		actor = "(unset)"
	}
	status := fmt.Sprintf("actor: %s | store: %s", actor, m.store.StorePath())
	hint := m.footerHint()
	innerW := m.width - 2
	if innerW < 1 {
		innerW = 1
	}
	// Two centered lines: the contextual key menu, then the status line.
	menu := keyMenuStyle.Render(" " + hint + " ")
	menuLine := lipgloss.PlaceHorizontal(innerW, lipgloss.Center, menu)
	// Status line: show a transient status message when present, otherwise the
	// actor/store info.
	var statusText string
	if m.toast.msg != "" {
		statusText = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true).Render(" " + m.toast.msg + " ")
	} else {
		statusText = keyMenuDimStyle.Render(" " + status + " ")
	}
	statusLine := lipgloss.PlaceHorizontal(innerW, lipgloss.Center, statusText)
	return lipgloss.JoinVertical(lipgloss.Left, menuLine, statusLine)
}

func (m *Model) footerHint() string {
	switch m.focused {
	case paneProjects:
		return "a:add e:edit Enter:open L/l:labels T:type R/r:repos"
	case paneTasks:
		return "a:add Enter:open n:next c:claim u:unclaim s:status"
	case paneSummary:
		return "a:approve r:reject R:resolve"
	case paneHelp:
		return "scroll: j/k"
	}
	return ""
}

func (m *Model) renderOverlayBox() string {
	var b strings.Builder
	w := m.width - 6
	if w < 30 {
		w = 30
	}
	b.WriteString(dialogTitleStyle.Render(m.overlay.title))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", w))
	b.WriteString("\n")
	for _, l := range m.overlay.lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(buttonInactiveStyle.Render("[ Close ]"))
	return dialogStyle.Render(b.String())
}

func Run(storePath, actor string) error {
	m, err := NewModel(NewModelOpts{StorePath: storePath, Actor: actor})
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
