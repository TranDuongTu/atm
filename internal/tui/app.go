package tui

import (
	"fmt"
	"os"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	tabDashboard = 0
	tabProjects  = 1
	tabTasks     = 2
	tabActors    = 3
	tabHelp      = 4
)

type focusMode int

const (
	focusList focusMode = iota
	focusFilter
	focusForm
	focusOverlay
)

type Model struct {
	store    *store.Store
	storeSet bool
	actor    string
	width    int
	height   int
	tab      int
	km       keymap

	dash     *dashboardModel
	projects *projectsModel
	tasks    *tasksModel
	actors   *actorsModel
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
	msg     string
	visible bool
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
	m.actors = newActorsModel(m)
	m.help = newHelpModel(m)

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
	// Reserve space for top (header+tabbar) and bottom (footer) bordered
	// panes plus their surrounding borders. Each bordered pane occupies
	// (1 top border + content rows + 1 bottom border) lines.
	topH := topPaneHeight(m.width)
	bottomH := bottomPaneHeight(m.width)
	contentH := h - topH - bottomH
	if contentH < 2 {
		contentH = 2
	}
	contentW := w
	if contentW < 20 {
		contentW = 20
	}
	m.dash.setSize(contentW, contentH)
	m.projects.setSize(contentW, contentH)
	m.tasks.setSize(contentW, contentH)
	m.actors.setSize(contentW, contentH)
	m.help.setSize(contentW, contentH)
}

// topPaneHeight is the outer height (including borders) of the top pane for
// the current width. The header is one logical line; the tab bar is another.
// With a rounded border that yields 1 + 2 + 1 = 4 lines.
func topPaneHeight(w int) int { return 4 }

// bottomPaneHeight is the outer height (including borders) of the bottom pane.
// The footer is one logical line -> 1 + 2 = 3 lines.
func bottomPaneHeight(w int) int { return 3 }

func (m *Model) refreshAll() {
	m.dash.refresh()
	m.projects.refresh()
	m.tasks.refresh()
	m.actors.refresh()
	m.help.refresh()
}

func (m *Model) showToast(msg string) {
	m.toast.msg = msg
	m.toast.visible = true
}

func (m *Model) hideToast() {
	m.toast.visible = false
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
		m.tab = tabHelp
		return m, nil
	case m.km.palette:
		m.showToast("command palette: not yet implemented")
		return m, nil
	case m.km.filter:
		m.filter.active = true
		m.filter.value = ""
		return m, nil
	case m.km.escape:
		m.hideToast()
		return m, nil
	case "1":
		m.tab = tabDashboard
		return m, nil
	case "2":
		m.tab = tabProjects
		return m, nil
	case "3":
		m.tab = tabTasks
		return m, nil
	case "4":
		m.tab = tabActors
		return m, nil
	case "5":
		m.tab = tabHelp
		return m, nil
	case "tab":
		m.tab = (m.tab + 1) % 5
		return m, nil
	case "shift+tab":
		m.tab = (m.tab + 4) % 5
		return m, nil
	}

	return m.dispatchTab(msg)
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
		m.showToast("cancelled")
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
	switch m.tab {
	case tabDashboard:
		m.dash.setFilter(v)
	case tabProjects:
		m.projects.setFilter(v)
	case tabTasks:
		m.tasks.setFilter(v)
	case tabActors:
		m.actors.setFilter(v)
	}
}

func (m *Model) dispatchTab(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabDashboard:
		return m.dash.update(msg)
	case tabProjects:
		return m.projects.update(msg)
	case tabTasks:
		return m.tasks.update(msg)
	case tabActors:
		return m.actors.update(msg)
	case tabHelp:
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

	top := box(topStyle, w, m.renderHeader()+"\n"+m.renderTabBar())
	content := box(contentStyle, w, m.renderContent())
	bottom := box(bottomStyle, w, m.renderFooter())

	var b strings.Builder
	b.WriteString(top)
	b.WriteString("\n")
	b.WriteString(content)
	b.WriteString("\n")
	b.WriteString(bottom)
	if m.form.active && m.form.form != nil {
		b.WriteString("\n")
		b.WriteString(m.form.form.View())
	}
	if m.overlay.visible {
		b.WriteString("\n")
		b.WriteString(m.renderOverlayBox())
	}
	if m.toast.visible && m.toast.msg != "" {
		b.WriteString("\n[ " + m.toast.msg + " ]")
	}
	return b.String()
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
	storeInd := m.store.StorePath()
	actor := m.actor
	if actor == "" {
		actor = "(unset)"
	}
	return fmt.Sprintf("atm  %s  actor: %s  [r]efresh [q]uit", storeInd, actor)
}

func (m *Model) renderTabBar() string {
	tabs := []string{"1 Dashboard", "2 Projects", "3 Tasks", "4 Actors", "5 Help"}
	var parts []string
	for i, t := range tabs {
		if i == m.tab {
			parts = append(parts, activeTabStyle.Render(t))
		} else {
			parts = append(parts, inactiveTabStyle.Render(t))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m *Model) renderContent() string {
	switch m.tab {
	case tabDashboard:
		return m.dash.view()
	case tabProjects:
		return m.projects.view()
	case tabTasks:
		return m.tasks.view()
	case tabActors:
		return m.actors.view()
	case tabHelp:
		return m.help.view()
	}
	return ""
}

func (m *Model) renderFooter() string {
	actor := m.actor
	if actor == "" {
		actor = "(unset)"
	}
	hint := m.footerHint()
	return fmt.Sprintf("actor: %s | store: %s | %s", actor, m.store.StorePath(), hint)
}

func (m *Model) footerHint() string {
	switch m.tab {
	case tabDashboard:
		return "a:approve r:reject R:resolve"
	case tabProjects:
		return "a:add e:edit Enter:open L/l:labels T:type R/r:repos"
	case tabTasks:
		return "a:add Enter:open n:next c:claim u:unclaim s:status"
	case tabActors:
		return "Enter:show"
	case tabHelp:
		return "scroll: j/k"
	}
	return ""
}

func (m *Model) renderOverlayBox() string {
	var b strings.Builder
	w := m.width - 4
	if w < 20 {
		w = 20
	}
	border := strings.Repeat("-", w)
	b.WriteString("+" + border + "+\n")
	title := " " + m.overlay.title + " "
	pad := w - len(title)
	if pad < 0 {
		pad = 0
	}
	b.WriteString("|" + title + strings.Repeat(" ", pad) + "|\n")
	b.WriteString("+" + border + "+\n")
	for _, l := range m.overlay.lines {
		if len(l) > w {
			l = l[:w]
		}
		p := w - len(l)
		if p < 0 {
			p = 0
		}
		b.WriteString("|" + l + strings.Repeat(" ", p) + "|\n")
	}
	b.WriteString("+" + border + "+")
	return b.String()
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
