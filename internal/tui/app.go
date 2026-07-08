package tui

import (
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
	paneActors
)

const numPanes = 4

// helpOverlayKind identifies which read-only reference overlay is open.
type helpOverlayKind int

const (
	helpNone        helpOverlayKind = iota
	helpKeys                        // `?` — CLI/TUI parity + global keymap
	helpConventions                 // `C` — full conventions text
)

// formAction identifies what a form overlay is collecting.
type formAction int

const (
	formNone formAction = iota
	formProjectCreate
	formLabelAdd      // Labels pane / task detail: add label (ATM: prefix fixed)
	formLabelRemove   // Labels pane: remove label (name-only + warning)
	formLabelDescribe // Labels pane: set description (upsert)
	formTaskCreate
	formTaskSetTitle
	formTaskSetDescription
	formTaskLabelAdd    // task detail: add label
	formTaskLabelRemove // task detail: remove label
	formProjectSetName  // project detail: set name
	formCommentAdd      // task detail: add comment
)

// confirmAction identifies what a confirm overlay is for.
type confirmAction int

const (
	confirmNone confirmAction = iota
	confirmRemoveProject
	confirmRemoveTask
)

// Model is the root Bubble Tea model for the v2 TUI: a persistent three-pane
// workspace (Projects, Tasks, Labels), a help overlay, and a status line.
type Model struct {
	store    *store.Store
	storeSet bool
	actor    string
	km       keymap

	themeName ThemeName
	styles    Styles

	width, height int
	contentHeight int
	focused       workspacePane
	projectScope  string // selection (mockup "Selection model")
	quitting      bool
	// helpOverlay tracks which read-only reference overlay (if any) is open.
	// It is a clean full-body replacement over the workspace (the workspace
	// does not show through), unlike forms/confirms which layer on top.
	helpOverlay helpOverlayKind

	projects projectsModel
	tasks    tasksModel
	labels   labelsModel
	actors   actorsModel
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
	themeName := defaultThemeName()
	m := &Model{
		store:     s,
		storeSet:  true,
		km:        defaultKeymap(),
		width:     100,
		height:    30,
		actor:     actor,
		themeName: themeName,
		styles:    buildStyles(themeName),
	}
	m.projects = newProjectsModel(m)
	m.tasks = newTasksModel(m)
	m.labels = newLabelsModel(m)
	m.actors = newActorsModel(m)
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
	// chrome: 1 status line. The remaining height belongs to the workspace.
	m.contentHeight = h - 1
	if m.contentHeight < 1 {
		m.contentHeight = 1
	}
	leftW, rightW := splitWorkspaceWidths(w)
	tasksH, labelsH := splitRightColumnHeights(m.contentHeight)
	m.projects.SetSize(innerPaneWidth(leftW), innerPaneHeight(m.contentHeight))
	m.tasks.SetSize(innerPaneWidth(rightW), innerPaneHeight(tasksH))
	m.labels.SetSize(innerPaneWidth(rightW), innerPaneHeight(labelsH))
	m.actors.SetSize(innerPaneWidth(m.width), innerPaneHeight(m.contentHeight))
	if m.helpOverlay != helpNone {
		bw, bh := m.helpBoxSize()
		m.help.SetSize(bw, bh)
	} else {
		m.help.SetSize(w, m.contentHeight)
	}
	m.help.refresh()
}

// helpBoxSize returns the outer dimensions of the centered modal that hosts
// the ?/C reference overlay. It is intentionally larger than the form dialog
// (~80% of the workspace) so the parity table and conventions text remain
// readable, while still leaving workspace visible above and below the modal
// and a small lateral margin on either side.
func (m *Model) helpBoxSize() (int, int) {
	const pct = 80
	bw := m.width * pct / 100
	// Keep at least 95 cols so the 93-wide parity table fits inside the
	// border; only go wider (80% of terminal) when the terminal is large.
	if bw < 95 {
		bw = 95
	}
	if bw > m.width-4 {
		bw = m.width - 4
	}
	if bw < 1 {
		bw = 1
	}
	bh := m.contentHeight * pct / 100
	if bh > m.contentHeight-2 {
		bh = m.contentHeight - 2
	}
	if bh < 10 {
		bh = m.contentHeight
	}
	if bh < 1 {
		bh = 1
	}
	return bw, bh
}

// openHelp activates the requested reference overlay and re-sizes the help
// content to the centered modal box. closeHelp dismisses it.
func (m *Model) openHelp(kind helpOverlayKind) {
	m.helpOverlay = kind
	m.help.mode = kind
	bw, bh := m.helpBoxSize()
	m.help.SetSize(bw, bh)
	m.help.refresh()
}

func (m *Model) closeHelp() {
	m.helpOverlay = helpNone
}

func splitWorkspaceWidths(width int) (int, int) {
	if width < 2 {
		return width, 0
	}
	left := width * 40 / 100
	if left < 24 && width >= 48 {
		left = 24
	}
	if left > width-20 && width >= 40 {
		left = width - 20
	}
	right := width - left
	return left, right
}

func splitRightColumnHeights(height int) (int, int) {
	if height < 2 {
		return height, 0
	}
	top := height / 2
	bottom := height - top
	return top, bottom
}

func innerPaneWidth(width int) int {
	if width <= 2 {
		return 1
	}
	return width - 2
}

func innerPaneHeight(height int) int {
	if height <= 2 {
		return 1
	}
	return height - 2
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

func (m *Model) cycleTheme() {
	m.themeName = nextThemeName(m.themeName)
	m.styles = buildStyles(m.themeName)
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

	// Transient toast: clear on the next key so the user is never locked
	// behind a notification. A toast set by an action dispatched later in
	// this same call (e.g. submitForm -> showToast) survives because it is
	// assigned after this point, then renders until the next key.
	m.toastMsg = ""

	// Help overlay (? / C) toggles anywhere and consumes the key.
	if m.helpOverlay != helpNone {
		if k.String() == "T" {
			m.cycleTheme()
			m.help.refresh()
			return nil
		}
		// `?` and `C` toggle their own overlay; Esc closes; the other
		// reference key switches which overlay is shown.
		switch k.String() {
		case "?":
			if m.helpOverlay == helpKeys {
				m.closeHelp()
			} else {
				m.openHelp(helpKeys)
			}
			return nil
		case "C":
			if m.helpOverlay == helpConventions {
				m.closeHelp()
			} else {
				m.openHelp(helpConventions)
			}
			return nil
		case "esc":
			m.closeHelp()
			return nil
		}
		return m.help.handleKey(k)
	}

	// Confirm overlay consumes all keys until resolved.
	if m.confirm != confirmNone {
		if k.String() == "T" {
			m.cycleTheme()
			return nil
		}
		return m.handleConfirmKey(k)
	}

	// Form overlay consumes all keys until closed.
	if m.form != nil && m.form.Active {
		return m.handleFormKey(k)
	}

	if m.focused == paneTasks && m.tasks.filterEditing {
		return m.tasks.handleKey(k)
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
		m.focused = paneActors
		m.actors.refresh()
		return nil
	case "?":
		m.openHelp(helpKeys)
		return nil
	case "C":
		m.openHelp(helpConventions)
		return nil
	case "T":
		m.cycleTheme()
		return nil
	}

	// Esc at pane level: back from detail to list, or cancel task filter.
	// If a per-detail overlay (comment peek or history) is open, defer to
	// the pane's overlay Esc handler so Esc returns to the detail rather
	// than leaping out to the list and leaving the overlay state stale.
	if k.String() == "esc" {
		if m.focused == paneProjects && m.projects.view == pViewDetail {
			m.projects.backToList()
			return nil
		}
		if m.focused == paneTasks {
			if m.tasks.view == tViewDetail {
				if m.tasks.commentOverlay.id != "" || m.tasks.historyOverlay.active {
					return m.tasks.handleKey(k)
				}
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
		if m.focused == paneLabels && m.labels.chartNS != "" {
			m.labels.chartNS = ""
			return nil
		}
		if m.focused == paneActors && m.actors.detail {
			m.actors.detail = false
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
	case paneActors:
		return m.actors.handleKey(k)
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
	case formCommentAdd:
		return m.doCommentAdd(vals)
	}
	return nil
}

func (m *Model) doCommentAdd(vals map[string]string) tea.Cmd {
	taskID := m.tasks.detail.id
	body := vals["body"]
	var labels []string
	for _, tok := range strings.Fields(vals["labels"]) {
		labels = append(labels, m.projectScope+":"+tok)
	}
	replyTo := vals["reply-to"]
	_, err := m.store.CreateComment(taskID, body, labels, replyTo, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(taskID)
	return nil
}

// showToast records a transient toast message shown inline in the status
// line. The toast is cleared on the next key press (any key) so the TUI
// never locks the user out of the workspace behind a notification screen.
func (m *Model) showToast(msg string) {
	m.toastMsg = msg
}

// View renders the full screen: workspace, status line, plus any active
// overlay/form/help overlay.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.renderWorkspace())
	b.WriteString("\n")
	b.WriteString(m.renderStatusLine())

	// Overlay layers (help, form, confirm) render on top of the body via
	// placeOverlay: the workspace stays visible on the rows above and below
	// each modal, while the modal's own rows are blank-filled either side
	// (see overlayLineAt) so underlying pane borders do not leak through.
	out := b.String()
	if m.helpOverlay != helpNone {
		out = m.placeOverlay(out, m.renderHelpOverlay())
	}
	if m.form != nil && m.form.Active {
		out = m.placeOverlay(out, m.form.View(m.styles))
	}
	if m.confirm != confirmNone {
		out = m.placeOverlay(out, m.renderConfirm())
	}
	// Toasts render inline in the status line (see renderStatusLine), not as
	// a full-screen overlay, so the workspace stays interactive underneath.
	return out
}

func (m *Model) renderWorkspace() string {
	if m.focused == paneActors {
		return m.renderPane(paneActors, m.width, m.contentHeight, "[4] Actors", m.actors.View())
	}
	leftW, rightW := splitWorkspaceWidths(m.width)
	tasksH, labelsH := splitRightColumnHeights(m.contentHeight)

	projects := m.renderPane(paneProjects, leftW, m.contentHeight, "[1] Projects", m.projects.View())
	tasks := m.renderPane(paneTasks, rightW, tasksH, "[2] Tasks", m.tasks.View())
	labels := m.renderPane(paneLabels, rightW, labelsH, "[3] Labels", m.labels.View())

	right := lipgloss.JoinVertical(lipgloss.Left, tasks, labels)
	return lipgloss.JoinHorizontal(lipgloss.Top, projects, right)
}

func (m *Model) renderPane(pane workspacePane, width int, height int, title string, body string) string {
	style := m.styles.PaneInactive
	if m.focused == pane {
		style = m.styles.PaneActive
	}
	return titledBoxHeight(style, width, title, body, height)
}

// statusHint returns the focused-pane keymap hint for the status line.
func (m *Model) statusHint() string {
	switch m.focused {
	case paneProjects:
		return m.projects.statusHint()
	case paneTasks:
		return m.tasks.statusHint()
	case paneLabels:
		return m.labels.statusHint()
	case paneActors:
		return m.actors.statusHint()
	}
	return "[?]keys [C]conventions"
}

// renderHelpOverlay renders the active reference overlay as a centered,
// larger-than-form modal box (see helpBoxSize), placed on top of the
// workspace via placeOverlay. We use DialogBody (no Border/Padding) so
// titledBoxHeight's manual border chars are the only frame — Dialog would
// double-frame the content.
func (m *Model) renderHelpOverlay() string {
	title := "Help - Keys"
	if m.helpOverlay == helpConventions {
		title = "Help - Conventions"
	}
	bw, bh := m.helpBoxSize()
	return titledBoxHeight(m.styles.DialogBody, bw, title, m.help.View(), bh)
}

func (m *Model) renderStatusLine() string {
	var parts []string
	parts = append(parts, m.styles.StatusLabel.Render("STORE: ")+m.styles.Status.Render(shortenPath(m.store.StorePath(), 40)))
	if m.projectScope != "" {
		parts = append(parts, m.styles.StatusLabel.Render("SELECTED: ")+m.styles.Status.Render(m.projectScope))
	}
	parts = append(parts, m.styles.StatusLabel.Render("theme: ")+m.styles.Status.Render(string(m.themeName)))
	hint := m.statusHint()
	parts = append(parts, m.styles.KeyMenu.Render(hint))
	if m.toastMsg != "" {
		parts = append(parts, m.styles.Toast.Render(m.toastMsg))
	}
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
	line := left + spaces(gap) + m.styles.Status.Render(actor)
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
// horizontal). The entire backdrop is dimmed with a `░` shade (OverlayBackdrop
// style) — every row the modal does not occupy is replaced with a full-width
// dim row, and the columns either side of the modal on its own rows get the
// same dim fill. The overlay's own border frames the modal content. This gives
// the documented "modal on a dimmed workspace" look: the workspace shapes
// are still readable through the shade, but the modal reads unambiguously as
// the focused surface.
func (m *Model) placeOverlay(base, overlay string) string {
	return m.overlayLines(base, overlay, m.width, m.height)
}

func (m *Model) overlayLines(base, overlay string, width, height int) string {
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, spaces(width))
	}
	if len(baseLines) > height {
		baseLines = baseLines[:height]
	}

	overlayRows := strings.Split(overlay, "\n")
	overlayH := len(overlayRows)
	overlayW := 0
	for _, line := range overlayRows {
		if w := lipgloss.Width(line); w > overlayW {
			overlayW = w
		}
	}
	x := (width - overlayW) / 2
	if x < 0 {
		x = 0
	}
	y := (height - overlayH) / 2
	if y < 0 {
		y = 0
	}
	fullBackdrop := m.styles.OverlayBackdrop.Render(strings.Repeat("░", width))
	for i := range baseLines {
		// Rows outside the modal rectangle get the full dim backdrop.
		if i < y || i >= y+overlayH {
			baseLines[i] = fullBackdrop
			continue
		}
		baseLines[i] = m.overlayLineAt(overlayRows[i-y], x, width)
	}
	return strings.Join(baseLines, "\n")
}

// overlayLineAt composes a single modal row: the modal line sits at column x,
// columns either side are filled with the dim `░` shade (OverlayBackdrop).
// Dimming (rather than blanking) the side columns avoids the "modal-stripe
// over a bright workspace" look while still covering the pane borders that
// previously leaked through and read as shifted-to-the-right.
func (m *Model) overlayLineAt(overlayLine string, x, width int) string {
	maxW := width - x
	if maxW < 0 {
		maxW = 0
	}
	trimmed := fitLine(overlayLine, maxW)
	ow := lipgloss.Width(trimmed)
	backdrop := m.styles.OverlayBackdrop.Render(strings.Repeat("░", x))
	suffixW := width - x - ow
	if suffixW < 0 {
		suffixW = 0
	}
	suffix := m.styles.OverlayBackdrop.Render(strings.Repeat("░", suffixW))
	line := backdrop + trimmed + suffix
	if lw := lipgloss.Width(line); lw < width {
		line += spaces(width - lw)
	}
	return line
}

// renderConfirm renders the destructive-action confirm overlay.
func (m *Model) renderConfirm() string {
	var b strings.Builder
	b.WriteString(m.styles.DialogTitle.Render(m.confirmMsg))
	b.WriteString("\n")
	b.WriteString(repeat("-", min(len(m.confirmMsg)+2, m.width-4)))
	b.WriteString("\n\n")
	b.WriteString(m.styles.Warning.Render(m.confirmArg))
	b.WriteString("\n\n")
	b.WriteString(m.styles.KeyMenuDim.Render("[Enter] confirm   [Esc] cancel"))
	return m.styles.Dialog.Render(b.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
