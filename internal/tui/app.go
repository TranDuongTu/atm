package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"atm/internal/capability"
	"atm/internal/core"
	"atm/internal/version"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type workspacePane int

const (
	paneProjects workspacePane = iota
	paneTasks
)

const numPanes = 2

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
	formTaskLabelAdd      // task detail: add label
	formTaskLabelRemove   // task detail: remove label
	formProjectSetName    // project detail: set name
	formCommentAdd        // task detail: add comment
	formPersonaCreate     // Projects pane / overlay: add persona
	formBoardEditor       // Boards pane: new/edit a board (live-validated expr)
	formNamespaceDescribe // Boards pane: edit a namespace descriptor (description-only)
)

// confirmAction identifies what a confirm overlay is for.
type confirmAction int

const (
	confirmNone confirmAction = iota
	confirmRemoveProject
	confirmRemoveTask
	confirmDropIndex
)

// Model is the root Bubble Tea model for the v2 TUI: a persistent two-pane
// workspace (Projects, Tasks), a help overlay, and a status line.
type Model struct {
	store    core.Service
	storeSet bool
	// storeStats is the status bar's store summary, refreshed by refreshAll
	// so View never touches the filesystem.
	storeStats core.StoreStats
	actor      string
	km         keymap
	// reg is the capability registry the composition root injected; nil-safe.
	reg *capability.Registry

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

	// actorsOverlay, when true, renders the persona activity list/detail as a
	// centered modal over the workspace (opened by P in the Projects pane).
	actorsOverlay bool

	projects   projectsModel
	tasks      tasksModel
	boards     boardsModel
	capability capabilityModel
	actors     actorsModel
	help       helpModel

	// dispatch is the composition-root-injected dispatch port (the
	// *dispatch.Service facade). nil disables dispatch with a clear error.
	dispatcher     Dispatcher
	agentOptionsFn func() []agentOption
	dispatchDlg    dispatchModel

	form *Form

	formKind formAction
	// formPayload carries context for the form (e.g. which label is being
	// removed, which task is being edited).
	formPayload string

	// boardEditor holds the live-validation state for the Boards pane
	// [n]ew/[e]dit form. Non-nil only while formKind == formBoardEditor.
	boardEd *boardEditor

	confirm        confirmAction
	confirmMsg     string
	confirmArg     string
	confirmPayload string

	toastMsg string

	lastRefreshAt time.Time

	// plugins holds registered plugins in registration order. No plugins are
	// registered yet; the indexer (Task 4) will be the first.
	plugins []plugin
	// pluginOverlay is the index into plugins of the currently open plugin
	// overlay, or -1 when none is open. Initialized lazily; zero value (0) is
	// unused until the first plugin is registered.
	pluginOverlay int
	// pluginPrefixActive is set transiently after the `g` leader key is
	// pressed; the next key resolves it to either opening a registered
	// plugin's overlay (if it matches an OverlayKey) or clearing the flag.
	pluginPrefixActive bool
	// supervisor debounces per-plugin Reset calls (3-strikes/30s).
	supervisor *pluginSupervisor
	// indexer is the lazy-init model behind the indexer plugin; populated by
	// indexerPlugin.model on first use.
	indexer *indexerModel
}

// NewModelOpts are the inputs to NewModel.
type NewModelOpts struct {
	Service    core.Service
	Actor      string
	Registry   *capability.Registry
	Dispatcher Dispatcher
}

// NewModel builds the root Model over an opened store (auto-initing the
// store directory if absent) with all its sub-models initialized.
func NewModel(opts NewModelOpts) (*Model, error) {
	s := opts.Service
	if _, statErr := os.Stat(s.StorePath()); statErr != nil {
		if err := s.Init(""); err != nil {
			return nil, err
		}
	}
	actor := opts.Actor
	switch {
	case actor == "":
		actor = "admin@tui:unset"
	case !strings.Contains(actor, "@"):
		actor = actor + "@tui:unset"
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
		reg:       opts.Registry,
	}
	m.projects = newProjectsModel(m)
	m.tasks = newTasksModel(m)
	m.boards = newBoardsModel(m)
	m.capability = newCapabilityModel(m)
	m.actors = newActorsModel(m)
	m.help = newHelpModel(m)
	m.dispatcher = opts.Dispatcher
	m.agentOptionsFn = agentOptions
	m.dispatchDlg.m = m
	m.plugins = []plugin{newIndexerPlugin()}
	m.pluginOverlay = -1
	m.supervisor = newPluginSupervisor()
	m.SetSize(m.width, m.height)
	m.refreshAll()
	// Defensive: NewModel never sets projectScope before launch (a fresh
	// launch always starts with no project selected), so this is a no-op in
	// practice — the real entry point is the project-select handler in
	// projects.go. Kept in case a future caller constructs a Model with a
	// pre-populated projectScope.
	if m.projectScope != "" {
		if _, err := m.regFor(m.projectScope).EnsureVocabulary(m.store, m.projectScope, m.actor); err != nil {
			m.showToast("ensure workflow boards: " + err.Error())
		}
		m.boards.selectDefault()
	}
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
	m.projects.SetSize(innerPaneWidth(leftW), innerPaneHeight(m.contentHeight))
	m.tasks.SetSize(innerPaneWidth(rightW), innerPaneHeight(m.contentHeight))
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

// actorsOverlayBoxSize returns the outer dimensions of the centered modal that
// hosts the P overlay. It mirrors helpBoxSize's ~80% sizing so the persona list
// and detail breakdowns stay readable.
func (m *Model) actorsOverlayBoxSize() (int, int) {
	bw, bh := m.helpBoxSize()
	return bw, bh
}

// sizeActorsToOverlay sizes the actors model to the overlay box's inner area.
// titledBoxHeight draws a 1-cell border + title row + bottom row, so inner =
// (bw-2) x (bh-2).
func (m *Model) sizeActorsToOverlay() {
	bw, bh := m.actorsOverlayBoxSize()
	innerW := bw - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := bh - 2
	if innerH < 1 {
		innerH = 1
	}
	m.actors.SetSize(innerW, innerH)
}

// renderActorsOverlay renders the persona activity list/detail as a centered
// modal box (the P overlay) sized like the help overlay.
func (m *Model) renderActorsOverlay() string {
	bw, bh := m.actorsOverlayBoxSize()
	return titledBoxHeight(m.styles.DialogBody, bw, "Activity by persona", m.actors.View(), bh)
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
	m.capability.refresh()
	m.projects.refresh()
	m.tasks.refresh()
	m.boards.refresh()
	m.help.refresh()
	m.refreshStoreStats()
	m.lastRefreshAt = core.Now()
}

// refreshStoreStats reloads the status bar's event-log summary for the
// current project scope (store-wide when nothing is selected). Selecting a
// project must call this directly: that handler deliberately avoids a full
// refreshAll, so without it the bar would keep the previous project's
// numbers until the next refresh tick. A read failure is never worth
// blanking the bar — keep the previous value and let the next refresh
// correct it.
func (m *Model) refreshStoreStats() {
	if st, err := m.store.StoreStats(m.projectScope); err == nil {
		m.storeStats = st
	}
}

// actorOr returns the actor string for the status line. The actor is always
// set (defaults to "admin@tui:unset" when none was provided at launch).
func (m *Model) actorOr() string {
	return m.actor
}

// regFor narrows the registry to the project's enabled set, degrading to
// the full registry when the project cannot be read (never blank a pane
// over a read failure).
func (m *Model) regFor(code string) *capability.Registry {
	p, err := m.store.GetProject(code)
	if err != nil {
		return m.reg
	}
	return m.reg.For(p)
}

func (m *Model) cycleTheme() {
	m.themeName = nextThemeName(m.themeName)
	m.styles = buildStyles(m.themeName)
}

// canMutate reports whether mutating keys are active. Always true in v2: the
// actor defaults to "admin@tui:unset" when the TUI is launched without
// --actor, so there is no actor-gated dead state. Kept as a stable predicate
// for callers.
func (m *Model) canMutate() bool { return true }

// Init is the Bubble Tea Init command. It schedules the periodic refresh
// tick that re-runs refreshAll so external mutations (CLI writes in another
// process) surface in the TUI without a manual key. The tick is cheap: with
// the O(1) LastLogSeq staleness check, refreshAll skips rebuilds when the
// cache is fresh.
func (m *Model) Init() tea.Cmd { return refreshTickCmd() }

// refreshTickMsg is the periodic message that triggers a refreshAll to pick
// up external mutations (a CLI invocation in another process appending to
// log.jsonl + cache.db). The TUI's own mutations already call refreshAll
// synchronously; this tick only matters for changes originating outside the
// running TUI.
type refreshTickMsg struct{}

// refreshTickInterval is how often the TUI polls for external mutations.
// 10s keeps background refreshes visible without continuously rewriting the
// status bar; the indicator reports stale sync only after a missed grace
// window.
const refreshTickInterval = 10 * time.Second

func refreshTickCmd() tea.Cmd {
	return tea.Tick(refreshTickInterval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// Update routes messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case refreshTickMsg:
		// Pick up external mutations (CLI in another process). refreshAll
		// is cheap with the O(1) LastLogSeq staleness check; a no-op tick
		// against a fresh cache skips rebuilds. Preserve cursor position
		// so the user's selection isn't disturbed by a background tick.
		m.refreshAll()
		return m, refreshTickCmd()
	case pluginTickMsg:
		im := m.indexer
		if im == nil {
			return m, nil
		}
		for {
			select {
			case msg := <-im.msgCh:
				applyIndexerMsg(m, msg)
			default:
				goto drained
			}
		}
	drained:
		if m.pluginOverlay != -1 || im.cancel != nil {
			return m, pluginTickCmd()
		}
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case reindexResultMsg:
		im := m.indexer
		if im == nil {
			return m, nil
		}
		if msg.err != "" {
			im.logs = append(im.logs, "index error: "+msg.err)
			m.showToast("reindex error: " + msg.err)
		} else {
			im.logs = append(im.logs, fmt.Sprintf("indexed %d (model=%s); index at log_seq %d", msg.indexed, msg.model, msg.logSeq))
			im.refreshStatus()
		}
		if len(im.logs) > 1000 {
			im.logs = im.logs[len(im.logs)-1000:]
		}
		return m, nil
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

	// Actors overlay (P) consumes navigation + p (add persona) + Esc until closed.
	if m.actorsOverlay {
		switch k.String() {
		case "esc":
			if m.actors.detail {
				m.actors.handleKey(k)
				return nil
			}
			m.actorsOverlay = false
			return nil
		case "p":
			return m.openPersonaCreateForm()
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
		return m.actors.handleKey(k)
	}

	// Plugin overlay consumes keys until closed (Esc). T/?/C still work so the
	// global help/theme shortcuts remain reachable while a plugin overlay is
	// open (mirrors the actors-overlay behavior above).
	if m.pluginOverlay != -1 {
		switch k.String() {
		case "esc":
			m.plugins[m.pluginOverlay].Close(m)
			m.pluginOverlay = -1
			return nil
		case "T":
			m.cycleTheme()
			return nil
		case "?":
			m.openHelp(helpKeys)
			return nil
		case "C":
			m.openHelp(helpConventions)
			return nil
		}
		return m.plugins[m.pluginOverlay].HandleKey(k, m)
	}

	// Capabilities switcher consumes keys until closed (Esc/C). T still
	// cycles the theme, mirroring the other overlays.
	if m.capability.open {
		return m.capability.handleKey(k)
	}

	// Dispatch dialog consumes keys until closed (Esc).
	if m.dispatchDlg.kind != dispatchNone {
		return m.dispatchDlg.handleKey(k)
	}

	// `q` quits the app when no overlay/form/confirm is active (mirrors the
	// common TUI convention; ctrl+c also quits anywhere).
	if k.String() == "q" {
		if m.indexer != nil {
			resetIndexer(m)
		}
		m.quitting = true
		return tea.Quit
	}

	// `g` is a leader key: the key pressed immediately after `g` opens a
	// registered plugin's overlay if it matches an OverlayKey, otherwise the
	// prefix flag clears and the key falls through to normal pane routing.
	// This resolution runs BEFORE the pane-focus switch below so that a
	// plugin OverlayKey that collides with a pane-focus key (e.g. "1") is
	// claimed by the pending prefix rather than switching panes. If `g` was
	// just pressed, the case below sets the flag and returns, so the next
	// key enters this block.
	if m.pluginPrefixActive {
		m.pluginPrefixActive = false
		for i, p := range m.plugins {
			if k.String() == p.OverlayKey() {
				// D14: the overlay opens even with no project selected — it
				// shows the `off` state + "(none — press [e] to configure)".
				// No "select a project first" toast; the user can configure
				// after selecting a project.
				m.pluginOverlay = i
				p.Open(m)
				return nil
			}
		}
		return nil
	}

	// Tab switching works in list/detail panes (not inside form/confirm).
	switch k.String() {
	case "1":
		m.focused = paneProjects
		return nil
	case "2":
		m.focused = paneTasks
		return nil
	case "?":
		m.openHelp(helpKeys)
		return nil
	case "C":
		if m.focused == paneTasks && m.projectScope != "" {
			m.capability.openOverlay()
			return nil
		}
		m.openHelp(helpConventions)
		return nil
	case "D":
		if m.focused == paneProjects {
			if row, ok := m.projects.selected(); ok {
				m.dispatchDlg.open(dispatchManager, row.code, "", "")
			}
			return nil
		}
		if m.focused == paneTasks {
			if r, ok := m.tasks.selectedRow(); ok {
				project := m.projectScope
				if r.task != nil && r.task.ProjectCode != "" {
					project = r.task.ProjectCode
				}
				if project == "" {
					m.showToast("error: no project scope for dispatch")
					return nil
				}
				m.dispatchDlg.open(dispatchDeveloper, project, r.id, r.title)
			}
			return nil
		}
	case "T":
		m.cycleTheme()
		return nil
	case "g":
		m.pluginPrefixActive = true
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
		}
		// No detail to leave: ignore.
		return nil
	}

	switch m.focused {
	case paneProjects:
		return m.projects.handleKey(k)
	case paneTasks:
		return m.tasks.handleKey(k)
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
		m.confirmPayload = ""
	}
	return nil
}

// closeForm dismisses the active form without performing its action.
func (m *Model) closeForm() {
	m.form = nil
	m.formKind = formNone
	m.formPayload = ""
	m.boardEd = nil
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
	case formPersonaCreate:
		return m.doPersonaCreate(vals)
	case formBoardEditor:
		return m.doBoardEdit(vals)
	case formNamespaceDescribe:
		return m.doNamespaceDescribe(vals)
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

func (m *Model) doPersonaCreate(vals map[string]string) tea.Cmd {
	name := vals["name"]
	desc := vals["description"]
	_, err := m.store.CreatePersona(name, "", desc, m.actor)
	if err != nil {
		if core.IsConflict(err) {
			m.showToast(fmt.Sprintf("persona %s already exists", name))
		} else {
			m.showToast("error: " + err.Error())
		}
		return nil
	}
	m.showToast(fmt.Sprintf("created persona %s", name))
	m.actors.refresh()
	m.refreshAll()
	return nil
}

// showToast records a transient toast message shown inline in the status
// line. The toast is cleared on the next key press (any key) so the TUI
// never locks the user out of the workspace behind a notification screen.
func (m *Model) showToast(msg string) {
	m.toastMsg = msg
}

// openPersonaCreateForm opens the New persona form (name + description only).
// The prompt is left empty; the user sets it later via CLI --prompt-file.
func (m *Model) openPersonaCreateForm() tea.Cmd {
	nameValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		return core.ValidatePersonaName(value)
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "lowercase slug, e.g. staff-engineer", Validator: nameValidator},
		{Label: "description", Hint: "one-line summary (optional)"},
	}
	f := NewForm("New persona", fields)
	m.form = f
	m.formKind = formPersonaCreate
	return nil
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
	if m.actorsOverlay {
		out = m.placeOverlay(out, m.renderActorsOverlay())
	}
	if m.pluginOverlay != -1 {
		out = m.placeOverlay(out, m.plugins[m.pluginOverlay].Render(m))
	}
	if m.capability.open {
		out = m.placeOverlay(out, m.capability.renderOverlay())
	}
	if m.dispatchDlg.kind != dispatchNone {
		out = m.placeOverlay(out, m.dispatchDlg.renderOverlay())
	}
	// Toasts render inline in the status line (see renderStatusLine), not as
	// a full-screen overlay, so the workspace stays interactive underneath.
	return out
}

func (m *Model) renderWorkspace() string {
	leftW, rightW := splitWorkspaceWidths(m.width)
	projects := m.renderPane(paneProjects, leftW, m.contentHeight, "[1] Projects", m.projects.View())
	tasks := m.renderPane(paneTasks, rightW, m.contentHeight, "[2] Tasks", m.tasks.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, projects, tasks)
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
	}
	return ""
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
	// The counts are scoped to the selected project; naming it keeps the
	// numbers from reading as store-wide totals that mysteriously moved.
	scope := ""
	if m.projectScope != "" {
		scope = m.projectScope + " "
	}
	parts = append(parts, m.styles.StatusLabel.Render("⛃ "+m.storeStats.Version)+
		m.styles.Status.Render(fmt.Sprintf(" · %s%d events · %s", scope, m.storeStats.EventCount, formatSize(m.storeStats.SizeBytes))))
	// Panes with nothing pane-specific to say now return "" — the global
	// key cluster on the right covers what their hint used to repeat.
	if hint := m.statusHint(); hint != "" {
		parts = append(parts, m.styles.KeyMenu.Render(hint))
	}
	if m.toastMsg != "" {
		parts = append(parts, m.styles.Toast.Render(m.toastMsg))
	}
	left := strings.Join(parts, "  ")
	rightSegments := dockSegments(m)
	rightSegments = append(rightSegments,
		m.styles.KeyMenu.Render("[?]help [C]conv [T]theme"),
		m.styles.KeyMenuDim.Render("atm "+version.Version),
		m.refreshRecencySegment())
	right := strings.Join(rightSegments, "  ")
	used := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	need := used + 2 + rightW
	gap := 2
	if need < m.width {
		gap = m.width - used - rightW
	}
	if gap < 1 {
		gap = 1
	}
	line := left + spaces(gap) + right
	if lw := lipgloss.Width(line); lw < m.width {
		line += spaces(m.width - lw)
	}
	return line
}

func (m *Model) refreshRecencySegment() string {
	now := core.Now()
	if !m.lastRefreshAt.IsZero() && !now.Before(m.lastRefreshAt) && now.Sub(m.lastRefreshAt) <= 15*time.Second {
		return m.refreshRecencyStyleAt(now).Render("✓")
	}
	return m.refreshRecencyStyleAt(now).Render("↻ " + refreshAgeLabel(m.lastRefreshAt, now))
}

func (m *Model) refreshRecencyStyle() lipgloss.Style {
	return m.refreshRecencyStyleAt(core.Now())
}

func (m *Model) refreshRecencyStyleAt(now time.Time) lipgloss.Style {
	if !m.lastRefreshAt.IsZero() && !now.Before(m.lastRefreshAt) && now.Sub(m.lastRefreshAt) <= 10*time.Second {
		return m.styles.StatusOK
	}
	if !m.lastRefreshAt.IsZero() && !now.Before(m.lastRefreshAt) && now.Sub(m.lastRefreshAt) > 15*time.Second {
		return m.styles.Warning
	}
	return m.styles.Status
}

func refreshAgeLabel(last, now time.Time) string {
	if last.IsZero() {
		return "--"
	}
	if now.Before(last) {
		return "now"
	}
	age := now.Sub(last)
	switch {
	case age < time.Second:
		return "now"
	case age < time.Minute:
		return fmt.Sprintf("%ds ago", int(age.Seconds()))
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	}
}

// formatSize renders a byte count for the status bar: whole KB under 1 MiB
// (a flat "0.0 MB" for small stores reads as broken), one-decimal MB above.
func formatSize(b int64) string {
	const mb = 1 << 20
	if b < mb {
		return fmt.Sprintf("%d KB", b/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
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

// countTasksCarrying counts distinct project tasks carrying at least one
// label the set owns. Runs on refresh, never per frame.
func (m *Model) countTasksCarrying(scope string, set capability.LabelSet) int {
	count := 0
	for _, tk := range m.store.ListTasks(core.QueryFilters{Project: scope}) {
		for _, full := range tk.Labels {
			if set.Contains(full) {
				count++
				break
			}
		}
	}
	return count
}

// capabilityTaskCount is the header's capability-owned total: tasks carrying
// at least one label the named capability owns (ownership rule shared with
// Registry.Unmanaged via LabelSet). For unmanaged it counts tasks carrying
// any unmanaged label. Deliberate: workflow's count reflects the paved road
// (status:/priority:-labeled tasks), not its all-tasks board match.
func (m *Model) capabilityTaskCount(capName string) int {
	scope := m.projectScope
	if scope == "" || capName == "" {
		return 0
	}
	if capName == unmanagedCapability {
		un, _ := m.regFor(scope).Unmanaged(m.store, scope)
		return m.countTasksCarrying(scope, capability.NewLabelSet(un))
	}
	return m.countTasksCarrying(scope, capability.NewLabelSet(m.reg.OwnedLabels(scope, capName)))
}
