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
	paneLabels
	paneHelp
)

const numPanes = 4

// formAction identifies what a form overlay is collecting.
type formAction int

const (
	formNone formAction = iota
	formProjectCreate
	formLabelAdd      // Labels tab / task detail: add label (ATM: prefix fixed)
	formLabelRemove   // Labels tab: remove label (name-only + warning)
	formLabelDescribe // Labels tab: set description (upsert)
	formTaskCreate
	formTaskSetTitle
	formTaskSetDescription
	formTaskLabelAdd    // task detail: add label
	formTaskLabelRemove // task detail: remove label
	formProjectSetName  // project detail: set name
)

// confirmAction identifies what a confirm overlay is for.
type confirmAction int

const (
	confirmNone confirmAction = iota
	confirmRemoveProject
	confirmRemoveTask
)

// Model is the root Bubble Tea model for the v2 TUI: four tabs
// (Projects/Tasks/Labels/Help) sharing a single content area with a tab bar
// and a status line.
type Model struct {
	store    *store.Store
	storeSet bool
	actor    string
	km       keymap

	width, height   int
	contentHeight   int
	focused         workspacePane
	projectScope    string // selection (mockup "Selection model")
	quitting        bool
	keymapOverlayOn bool

	projects projectsModel
	tasks    tasksModel
	labels   labelsModel
	help     helpModel

	form *Form

	formKind formAction
	// formPayload carries context for the form (e.g. which label is being
	// removed, which task is being edited).
	formPayload string

	confirm    confirmAction
	confirmMsg string
	confirmArg string

	toastMsg string
}

// NewModelOpts are the inputs to NewModel.
type NewModelOpts struct {
	StorePath string
	Actor     string
}

// NewModel opens (and auto-inits if absent) the store and builds the root
// Model with all three panes initialized.
func NewModel(opts NewModelOpts) (*Model, error) {
	root := store.ResolveStorePath(opts.StorePath)
	s, err := store.Open(root)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(s.StorePath()); statErr != nil {
		if err := s.Init(""); err != nil {
			return nil, err
		}
	}
	actor := opts.Actor
	if actor == "" {
		actor = "default"
	}
	m := &Model{
		store:    s,
		storeSet: true,
		km:       defaultKeymap(),
		width:    100,
		height:   30,
		actor:    actor,
	}
	m.projects = newProjectsModel(m)
	m.tasks = newTasksModel(m)
	m.labels = newLabelsModel(m)
	m.help = newHelpModel(m)
	m.SetSize(m.width, m.height)
	m.refreshAll()
	return m, nil
}

// SetSize sets the terminal dimensions and propagates to sub-panes.
func (m *Model) SetSize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	m.width = w
	m.height = h
	// chrome: 1 tab bar + 1 status line + 2 separator lines = 4; content
	// height is what remains for the body.
	m.contentHeight = h - 4
	if m.contentHeight < 1 {
		m.contentHeight = 1
	}
	contentW := w
	if contentW < 1 {
		contentW = 1
	}
	m.projects.SetSize(contentW, m.contentHeight)
	m.tasks.SetSize(contentW, m.contentHeight)
	m.labels.SetSize(contentW, m.contentHeight)
	m.help.SetSize(contentW, m.contentHeight)
}

// refreshAll reloads all panes from the store. Called on launch and after
// every mutation.
func (m *Model) refreshAll() {
	m.projects.refresh()
	m.tasks.refresh()
	m.labels.refresh()
	m.help.refresh()
}

// actorOr returns the actor string for the status line. The actor is always
// set (defaults to "default" when none was provided at launch).
func (m *Model) actorOr() string {
	return m.actor
}

// canMutate reports whether mutating keys are active. Always true in v2: the
// actor defaults to "default" when the TUI is launched without --actor, so
// there is no actor-gated dead state. Kept as a stable predicate for callers.
func (m *Model) canMutate() bool { return true }

// Init is the Bubble Tea Init command.
func (m *Model) Init() tea.Cmd { return nil }

// Update routes messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

// handleKey dispatches a key based on overlay/form/confirm state first, then
// by active pane.
func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	// Global quit works everywhere.
	switch k.String() {
	case "ctrl+c":
		m.quitting = true
		return tea.Quit
	}

	// Keymap overlay (?) toggles anywhere and consumes the key.
	if m.keymapOverlayOn {
		if k.String() == "?" || k.String() == "esc" {
			m.keymapOverlayOn = false
		}
		return nil
	}

	// Confirm overlay consumes all keys until resolved.
	if m.confirm != confirmNone {
		return m.handleConfirmKey(k)
	}

	// Form overlay consumes all keys until closed.
	if m.form != nil && m.form.Active {
		return m.handleFormKey(k)
	}

	// `q` quits the app when no overlay/form/confirm is active (mirrors the
	// common TUI convention; ctrl+c also quits anywhere).
	if k.String() == "q" {
		m.quitting = true
		return tea.Quit
	}

	// Tab switching works in list/detail panes (not inside form/confirm).
	switch k.String() {
	case "1":
		m.focused = paneProjects
		return nil
	case "2":
		m.focused = paneTasks
		return nil
	case "3":
		m.focused = paneLabels
		return nil
	case "4":
		m.focused = paneHelp
		return nil
	case "?":
		m.keymapOverlayOn = true
		return nil
	}

	// Esc at pane level: back from detail to list, or cancel task filter.
	if k.String() == "esc" {
		if m.focused == paneProjects && m.projects.view == pViewDetail {
			m.projects.backToList()
			return nil
		}
		if m.focused == paneTasks {
			if m.tasks.view == tViewDetail {
				m.tasks.backToList()
				return nil
			}
			if m.tasks.filterEditing {
				m.tasks.cancelFilterEdit()
				return nil
			}
		}
		if m.focused == paneLabels && m.labels.view == lViewDetail {
			m.labels.view = lViewList
			return nil
		}
		// No detail to leave: ignore.
		return nil
	}

	switch m.focused {
	case paneProjects:
		return m.projects.handleKey(k)
	case paneTasks:
		return m.tasks.handleKey(k)
	case paneLabels:
		return m.labels.handleKey(k)
	case paneHelp:
		return m.help.handleKey(k)
	}
	return nil
}

// handleFormKey routes a key into the active form, then handles submit/cancel
// outcomes.
func (m *Model) handleFormKey(k tea.KeyMsg) tea.Cmd {
	m.form.Update(k)
	if m.form.Cancel {
		m.closeForm()
		return nil
	}
	if m.form.Done {
		return m.submitForm()
	}
	return nil
}

// handleConfirmKey routes a key into the active confirm overlay.
func (m *Model) handleConfirmKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "enter", "y":
		return m.confirmYes()
	case "esc", "n", "q":
		m.confirm = confirmNone
	}
	return nil
}

// closeForm dismisses the active form without performing its action.
func (m *Model) closeForm() {
	m.form = nil
	m.formKind = formNone
	m.formPayload = ""
}

// submitForm performs the action bound to the active form.
func (m *Model) submitForm() tea.Cmd {
	defer m.closeForm()
	vals := m.form.Values()
	switch m.formKind {
	case formProjectCreate:
		return m.doProjectCreate(vals)
	case formProjectSetName:
		return m.doProjectSetName(vals)
	case formLabelAdd:
		return m.doLabelAdd(vals)
	case formLabelRemove:
		return m.doLabelRemove(vals)
	case formLabelDescribe:
		return m.doLabelDescribe(vals)
	case formTaskCreate:
		return m.doTaskCreate(vals)
	case formTaskSetTitle:
		return m.doTaskSetTitle(vals)
	case formTaskSetDescription:
		return m.doTaskSetDescription(vals)
	case formTaskLabelAdd:
		return m.doTaskLabelAdd(vals)
	case formTaskLabelRemove:
		return m.doTaskLabelRemove(vals)
	}
	return nil
}

// showToast records a transient toast message. Toasts clear on the next
// non-form/confirm key.
func (m *Model) showToast(msg string) {
	m.toastMsg = msg
}

// View renders the full screen: tab bar, body, status line, plus any active
// overlay/form/keymap-overlay.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")
	b.WriteString(repeat("─", m.width))
	b.WriteString("\n")
	b.WriteString(m.renderBody())
	b.WriteString("\n")
	b.WriteString(repeat("─", m.width))
	b.WriteString("\n")
	b.WriteString(m.renderStatusLine())

	// Overlay layers (form, confirm, keymap) render on top of the body via
	// lipgloss Place. We re-render the body+chrome then place the overlay.
	out := b.String()
	if m.form != nil && m.form.Active {
		out = m.placeOverlay(out, m.form.View())
	}
	if m.confirm != confirmNone {
		out = m.placeOverlay(out, m.renderConfirm())
	}
	if m.keymapOverlayOn {
		out = m.placeOverlay(out, m.renderKeymapOverlay())
	}
	if m.toastMsg != "" {
		out = m.placeToast(out, toastStyle.Render(" "+m.toastMsg+" "))
	}
	return out
}

func (m *Model) renderTabBar() string {
	names := []string{"Projects", "Tasks", "Labels", "Help"}
	var parts []string
	for i, n := range names {
		label := fmt.Sprintf("%d  %s", i+1, n)
		if workspacePane(i) == m.focused {
			parts = append(parts, activeTabStyle.Render(label))
		} else {
			parts = append(parts, inactiveTabStyle.Render(label))
		}
	}
	bar := strings.Join(parts, "  ")
	// Right-fill to width.
	if lw := lipgloss.Width(bar); lw < m.width {
		bar += spaces(m.width - lw)
	}
	return bar
}

func (m *Model) renderBody() string {
	switch m.focused {
	case paneProjects:
		return m.projects.View()
	case paneTasks:
		return m.tasks.View()
	case paneLabels:
		return m.labels.View()
	case paneHelp:
		return m.help.View()
	}
	return ""
}

// statusHint returns the tab-specific keymap hint for the status line.
func (m *Model) statusHint() string {
	switch m.focused {
	case paneProjects:
		return m.projects.statusHint()
	case paneTasks:
		return m.tasks.statusHint()
	case paneLabels:
		return m.labels.statusHint()
	case paneHelp:
		return "[1/2/3/4]tabs [?]keys"
	}
	return "[?]keys"
}

func (m *Model) renderStatusLine() string {
	var parts []string
	parts = append(parts, statusLabelStyle.Render("STORE: ")+statusStyle.Render(shortenPath(m.store.StorePath(), 40)))
	if m.projectScope != "" {
		parts = append(parts, statusLabelStyle.Render("SELECTED: ")+statusStyle.Render(m.projectScope))
	}
	hint := m.statusHint()
	parts = append(parts, keyMenuStyle.Render(hint))
	actor := "actor: " + m.actorOr()
	// Right-align the actor segment.
	left := strings.Join(parts, "  ")
	used := lipgloss.Width(left)
	actorW := lipgloss.Width(actor)
	need := used + 2 + actorW // 2 spaces gap
	gap := 2
	if need < m.width {
		gap = m.width - used - actorW
	}
	if gap < 1 {
		gap = 1
	}
	line := left + spaces(gap) + statusStyle.Render(actor)
	if lw := lipgloss.Width(line); lw < m.width {
		line += spaces(m.width - lw)
	}
	return line
}

// shortenPath trims a long path to fit maxW columns, keeping the tail.
func shortenPath(p string, maxW int) string {
	if len(p) <= maxW {
		return p
	}
	if maxW <= 3 {
		return "..." + p[len(p)-(maxW-3):]
	}
	return "..." + p[len(p)-(maxW-3):]
}

// placeOverlay centers `overlay` over `base` (top-half vertical, centered
// horizontal). The base is kept visible underneath (no opaque backdrop fill —
// the form's own border frames it).
func (m *Model) placeOverlay(base, overlay string) string {
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// placeToast puts the toast near the bottom, above the status line.
func (m *Model) placeToast(base, toast string) string {
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Bottom,
		toast,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// renderConfirm renders the destructive-action confirm overlay.
func (m *Model) renderConfirm() string {
	var b strings.Builder
	b.WriteString(dialogTitleStyle.Render(m.confirmMsg))
	b.WriteString("\n")
	b.WriteString(repeat("-", min(len(m.confirmMsg)+2, m.width-4)))
	b.WriteString("\n\n")
	b.WriteString(amberStyle.Render(m.confirmArg))
	b.WriteString("\n\n")
	b.WriteString(keyMenuDimStyle.Render("[Enter] confirm   [Esc] cancel"))
	return dialogStyle.Render(b.String())
}

// renderKeymapOverlay renders a compact version of the global keymap.
func (m *Model) renderKeymapOverlay() string {
	var b strings.Builder
	b.WriteString(dialogTitleStyle.Render("Keymap"))
	b.WriteString("\n")
	b.WriteString(repeat("-", 10))
	b.WriteString("\n\n")
	for _, r := range keymapRows {
		fmt.Fprintf(&b, "%-12s %s\n", r.Key, r.Projects)
	}
	b.WriteString("\n")
	b.WriteString(keyMenuDimStyle.Render("[?] or [Esc] to close"))
	return dialogStyle.Render(b.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
