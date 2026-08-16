package tui

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"atm/internal/capability"
	"atm/internal/core"
	"atm/internal/tui/art"
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
// workspace (Projects, Tasks), the \ spotlight overlay, and a status line.
type Model struct {
	store    core.Service
	storeSet bool
	// storeStats is the status bar's store summary, refreshed by refreshAll
	// so View never touches the filesystem.
	storeStats core.StoreStats
	actor      string
	// reg is the capability registry the composition root injected; nil-safe.
	reg *capability.Registry

	themeName ThemeName
	styles    Styles

	width, height int
	contentHeight int
	focused       workspacePane
	projectScope  string // selection (mockup "Selection model")
	quitting      bool

	projects   projectsModel
	tasks      tasksModel
	boards     boardsModel
	capability capabilityModel

	// dispatch is the composition-root-injected dispatch port (the
	// *dispatch.Service facade). nil disables dispatch with a clear error.
	dispatcher     Dispatcher
	agentOptionsFn func() []agentOption
	dispatchDlg    dispatchModel
	personasOv     personasModel
	channelsOv     channelsModel
	spotlight      spotlightModel
	// spotlightReturn is the spotlight row to restore when a spotlight-spawned
	// overlay, form, or confirm is dismissed, or -1 when nothing is pending. It
	// is set by spotlightModel.activate for kindDialog entries, consumed by
	// handleKey's wrapper, and cleared by a successful submit or confirm (which
	// land on the workspace instead).
	spotlightReturn int

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

	// artPhase is the background-art animation clock, advanced by artTickMsg
	// only while the plain workspace is visible; artOn caches each listed
	// project's config.json art_on flag and artPair caches the pinned
	// two-theme pair (both refreshed by refreshAll) so View never touches
	// the filesystem.
	artPhase int
	artOn    map[string]bool
	artPair  map[string][]string
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
	m.dispatcher = opts.Dispatcher
	m.agentOptionsFn = agentOptions
	m.dispatchDlg.m = m
	m.personasOv.m = m
	m.channelsOv.m = m
	m.spotlight = spotlightModel{m: m}
	m.spotlightReturn = -1
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
	// The spotlight's preview is wrapped to menuBoxWidth() at the row that was
	// hovered; without this, a resize while it's open leaves stale wrapping
	// behind (invisible with a one-line summary, wrong once the preview holds
	// multi-line reference/overlay/form content).
	if m.spotlight.open {
		m.spotlight.refreshPreview()
	}
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
	// Refresh the art-on cache alongside the project list so renderers
	// never read config.json during View.
	on := make(map[string]bool, len(m.projects.list))
	pairs := make(map[string][]string, len(m.projects.list))
	for _, r := range m.projects.list {
		if cfg, err := m.store.GetProjectConfig(r.code); err == nil && cfg != nil {
			if cfg.ArtOn {
				on[r.code] = true
			}
			if len(cfg.ArtPair) > 0 {
				pairs[r.code] = cfg.ArtPair
			}
		}
	}
	m.artOn = on
	m.artPair = pairs
	m.tasks.refresh()
	m.boards.refresh()
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

// workspaceIdle reports whether the plain two-pane workspace is what View
// shows — no overlay, form, confirm, plugin, capability switcher, dispatch
// dialog, personas overlay, or channels overlay layered over it (see View's
// overlay chain).
// Art animates only then; anything covering the workspace freezes the phase
// clock.
func (m *Model) workspaceIdle() bool {
	return !m.spotlight.open &&
		!(m.form != nil && m.form.Active) &&
		m.confirm == confirmNone &&
		m.pluginOverlay == -1 &&
		!m.capability.open &&
		!m.dispatchDlg.active &&
		!m.personasOv.open &&
		!m.channelsOv.open
}

// completeAction clears a pending spotlight return: spec decision 5 requires
// a successful submit or confirm to land on the workspace rather than
// reopening the spotlight. Call this at every point where a spotlight-spawned
// form, confirm, dialog, or overlay finishes its action successfully — as
// opposed to being merely dismissed (Esc), which is meant to return to the
// spotlight. Four call sites (submitForm, handleConfirmKey,
// dispatchModel.submit, capabilityModel.switchTo) route through this one
// helper so a future completion point is never the one that forgets it.
func (m *Model) completeAction() {
	m.spotlightReturn = -1
}

// Init is the Bubble Tea Init command. It schedules the periodic refresh
// tick that re-runs refreshAll so external mutations (CLI writes in another
// process) surface in the TUI without a manual key. The tick is cheap: with
// the O(1) LastLogSeq staleness check, refreshAll skips rebuilds when the
// cache is fresh. It also starts the background-art animation tick.
func (m *Model) Init() tea.Cmd { return tea.Batch(refreshTickCmd(), artTickCmd()) }

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

// artTickMsg advances the background-art animation. The tick always
// reschedules (cheap no-op off-workspace) but the phase only advances while
// the plain workspace is visible, so art freezes under overlays and forms.
type artTickMsg struct{}

const artTickInterval = 600 * time.Millisecond

func artTickCmd() tea.Cmd {
	return tea.Tick(artTickInterval, func(time.Time) tea.Msg { return artTickMsg{} })
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
	case artTickMsg:
		if m.workspaceIdle() {
			m.artPhase++
		}
		return m, artTickCmd()
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
			return m, pluginTickCmd(pluginTickInterval(m))
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

// handleKey dispatches a key, then resolves any pending spotlight return: if
// the spotlight spawned the overlay/form/confirm that was just dismissed, it
// reopens where the user left it. Centralizing it here means the six close
// paths (each overlay's esc, closeForm, the confirm dismissal) need no
// spotlight awareness.
//
// spotlightModel.activate also replays prelude/key segments through this
// same handleKey, so this wrapper runs on every replayed segment too — but
// harmlessly: activate sets spotlightReturn only after its replay loop
// finishes, so the return is still -1 during replay and the check below
// cannot fire on any of the nested calls for the replayed segments. It DOES
// fire, though, on the outer call: activate() itself runs beneath this same
// handleKey (the user's Enter keypress that triggered it), so by the time that
// outer call's dispatchKey returns, spotlightReturn is freshly set and the
// check below fires immediately — the reopen happens within the same
// keystroke that activated the entry, not on the user's next keypress. This
// is why a kindDialog entry whose replay leaves nothing workspaceIdle()
// gates on (e.g. an in-pane view rather than a real overlay/form/confirm)
// looks inert: the spotlight reopens over it before the user sees anything.
func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	cmd := m.dispatchKey(k)
	if m.spotlightReturn >= 0 && m.workspaceIdle() {
		row := m.spotlightReturn
		m.spotlightReturn = -1
		m.spotlight.openAt(row)
	}
	return cmd
}

// dispatchKey is the former handleKey body: overlay/form/confirm routing
// first, then pane routing.
func (m *Model) dispatchKey(k tea.KeyMsg) tea.Cmd {
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

	// Spotlight overlay consumes keys until closed. T still cycles the theme.
	if m.spotlight.open {
		if k.String() == "T" {
			m.cycleTheme()
			return nil
		}
		return m.spotlight.handleKey(k)
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

	// Dispatch dialog consumes keys until closed (Esc). Checked before the
	// actors overlay because dispatch can be opened from within the actors
	// detail view and must take over key routing immediately.
	if m.dispatchDlg.active {
		return m.dispatchDlg.handleKey(k)
	}

	// Plugin overlay consumes keys until closed (Esc). T/\ still work so the
	// global theme shortcut and the spotlight remain reachable while a plugin
	// overlay is open.
	if m.pluginOverlay != -1 {
		switch k.String() {
		case "esc":
			m.plugins[m.pluginOverlay].Close(m)
			m.pluginOverlay = -1
			return nil
		case "T":
			m.cycleTheme()
			return nil
		case "\\":
			// Close the plugin overlay first so the spotlight's replayed keys
			// target the workspace — leaving it open would swallow the replay
			// into the still-open plugin overlay.
			m.plugins[m.pluginOverlay].Close(m)
			m.pluginOverlay = -1
			m.spotlight.openSpotlight()
			return nil
		}
		return m.plugins[m.pluginOverlay].HandleKey(k, m)
	}

	// Capabilities switcher consumes keys until closed (Esc/C). T still
	// cycles the theme, mirroring the other overlays.
	if m.capability.open {
		return m.capability.handleKey(k)
	}

	// Personas overlay (read-only) consumes keys until closed (Esc).
	if m.personasOv.open {
		return m.personasOv.handleKey(k)
	}

	// Channels overlay (read-only) consumes keys until closed (Esc) or until
	// `c` hands off to the dispatch dialog.
	if m.channelsOv.open {
		return m.channelsOv.handleKey(k)
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
	case "\\":
		m.spotlight.openSpotlight()
		return nil
	case "C":
		if m.focused == paneTasks && m.projectScope != "" {
			m.capability.openOverlay()
		}
		return nil
	case "D":
		m.openDispatch()
		return nil
	case "V":
		m.personasOv.openOverlay()
		return nil
	case "E":
		// Read-only channel status. In the Projects pane this resolves to the
		// highlighted row, exactly as D does (see openDispatch), so the two
		// bindings never disagree about the project the user is looking at —
		// `c` inside the overlay hands that same project to the dispatch
		// dialog. Elsewhere it stays on m.projectScope: the overlay is a
		// project-wide status view and must not chase the task cursor.
		m.channelsOv.openOverlay(m.overlayProject())
		return nil
	case "T":
		m.cycleTheme()
		return nil
	case "g":
		m.pluginPrefixActive = true
		return nil
	}

	// Esc at pane level: back from detail to list, or cancel task filter.
	// If a per-detail overlay (comment peek) is open, defer to the pane's
	// overlay Esc handler so Esc returns to the detail rather than leaping
	// out to the list and leaving the overlay state stale. Persona-chart
	// drill-in Esc (back from detail) is handled by the projects pane's own
	// key handler.
	if k.String() == "esc" {
		if m.focused == paneProjects && m.projects.personaDrilled {
			return m.projects.handleKey(k)
		}
		if m.focused == paneProjects && m.projects.view == pViewDetail {
			m.projects.backToList()
			return nil
		}
		if m.focused == paneTasks {
			if m.tasks.view == tViewDetail {
				if m.tasks.commentOverlay.id != "" {
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

// overlayProject is the project a project-wide overlay should open on. It
// mirrors the project openDispatch resolves — the highlighted Projects row
// when that pane is focused and not drilled into the persona chart (the
// drill-in has no row of its own, so dispatch keeps the scope there too) —
// and falls back to the current scope everywhere else.
func (m *Model) overlayProject() string {
	if m.focused == paneProjects && !(m.projects.personaDrilled && m.projects.personaCursor < len(m.projects.personaGroups)) {
		if row, ok := m.projects.selected(); ok {
			return row.code
		}
	}
	return m.projectScope
}

// openDispatch opens the universal dispatch dialog, resolving the current
// pane/selection into persona, project, and task defaults. Context never
// changes dispatch logic — it only preselects. With no selection the dialog
// still opens, defaulting to concierge (the one built-in usable without a
// project).
func (m *Model) openDispatch() {
	persona, project, taskID, taskTitle := m.dispatchDefaults()
	m.dispatchDlg.open(persona, project, taskID, taskTitle, "")
}

// dispatchDefaults resolves the persona/project/task defaults openDispatch
// preselects from the current pane and selection. Extracted so the
// spotlight preview can compute the same defaults — without opening the
// dialog — and never show something the real D key would not.
func (m *Model) dispatchDefaults() (persona, project, taskID, taskTitle string) {
	persona, project = "concierge", m.projectScope
	switch {
	case m.focused == paneProjects && m.projects.personaDrilled && m.projects.personaCursor < len(m.projects.personaGroups):
		persona = m.projects.personaGroups[m.projects.personaCursor].Key
	case m.focused == paneProjects:
		if row, ok := m.projects.selected(); ok {
			persona, project = "manager", row.code
		}
	case m.focused == paneTasks:
		if r, ok := m.tasks.selectedRow(); ok {
			persona, taskID, taskTitle = "developer", r.id, r.title
			if r.task != nil && r.task.ProjectCode != "" {
				project = r.task.ProjectCode
			}
		}
	}
	return
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
		m.completeAction()
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
	m.completeAction()
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
	m.refreshAll()
	return nil
}

// showToast records a transient toast message shown inline in the status
// line. The toast is cleared on the next key press (any key) so the TUI
// never locks the user out of the workspace behind a notification screen.
func (m *Model) showToast(msg string) {
	m.toastMsg = msg
}

// toggleScopedArt flips the on/off flag for the currently scoped project,
// persists it to the project config, and refreshes the in-memory cache. When
// turning art ON it re-rolls a fresh random two-theme pair and pins it; when
// turning it OFF it clears the pinned pair. It is a no-op (with a toast hint)
// when no project is scoped. Bound to the `A` key in the projects and tasks
// panes.
func (m *Model) toggleScopedArt() {
	code := m.projectScope
	if code == "" {
		m.showToast("select a project first (s) to toggle art")
		return
	}
	next := !m.artOn[code]
	var pair []string
	if next {
		rolled := art.RollPair(rand.New(rand.NewSource(time.Now().UnixNano())))
		pair = []string{rolled[0].Name(), rolled[1].Name()}
	}
	if err := m.store.SetProjectArtOn(code, next, pair, m.actor); err != nil {
		m.showToast("art: " + err.Error())
		return
	}
	if m.artOn == nil {
		m.artOn = map[string]bool{}
	}
	if m.artPair == nil {
		m.artPair = map[string][]string{}
	}
	m.artOn[code] = next
	if next {
		m.artPair[code] = pair
		m.showToast("art: on")
	} else {
		delete(m.artPair, code)
		m.showToast("art: off")
	}
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
	f.SetWidth(FormWidth(m.width))
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

	// Overlay layers (menu, form, confirm) render on top of the body via
	// placeOverlay: the workspace stays visible on the rows above and below
	// each modal, while the modal's own rows are blank-filled either side
	// (see overlayLineAt) so underlying pane borders do not leak through.
	//
	// KEEP IN SYNC WITH workspaceIdle(): the eight gates below are exactly the
	// states in which View renders something over the plain workspace, and
	// workspaceIdle() is their negation (it gates the background-art animation
	// tick). Adding an overlay here without adding it to workspaceIdle() would
	// let art animate underneath the new overlay.
	out := b.String()
	if m.spotlight.open {
		out = m.placeOverlay(out, m.spotlight.renderOverlay())
	}
	if m.form != nil && m.form.Active {
		out = m.placeOverlay(out, m.form.View(m.styles))
	}
	if m.confirm != confirmNone {
		out = m.placeOverlay(out, m.renderConfirm())
	}
	if m.pluginOverlay != -1 {
		out = m.placeOverlay(out, m.plugins[m.pluginOverlay].Render(m))
	}
	if m.capability.open {
		out = m.placeOverlay(out, m.capability.renderOverlay())
	}
	if m.dispatchDlg.active {
		out = m.placeOverlay(out, m.dispatchDlg.renderOverlay())
	}
	if m.personasOv.open {
		out = m.placeOverlay(out, m.personasOv.renderOverlay())
	}
	if m.channelsOv.open {
		out = m.placeOverlay(out, m.channelsOv.renderOverlay())
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
	if m.toastMsg != "" {
		parts = append(parts, m.styles.Toast.Render(m.toastMsg))
	}
	left := strings.Join(parts, "  ")
	rightSegments := dockSegments(m)
	rightSegments = append(rightSegments,
		m.styles.KeyMenu.Render("[\\]spotlight"),
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
