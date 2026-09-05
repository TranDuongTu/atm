package tui

import (
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"atm/internal/agent"
	"atm/internal/compose"
	"atm/internal/core"
	"atm/internal/dispatch"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Dispatcher is the TUI-facing dispatch port; *dispatch.Service implements
// it. nil disables dispatch with a clear error in the dialog.
type Dispatcher interface {
	Preview() (string, error)
	PreviewTarget(string) (string, error)
	Spawn(dispatch.Spec) error
}

type agentOption struct {
	name string
	// model is the model configured for this selection key, empty when the
	// harness picks its own default. Filled from the store by loadFor, not by
	// agentOptions: readiness is a PATH fact, the model is a stored one.
	model string
	ready bool
	hint  string
}

// label is the option as the dialog shows it: the selection key, plus the
// model it will launch with when one is configured.
func (a agentOption) label() string {
	if a.model == "" {
		return a.name
	}
	return a.name + " · " + a.model
}

// agentOptions snapshots the catalog with readiness; swapped in tests via
// Model.agentOptionsFn.
func agentOptions() []agentOption {
	home, _ := os.UserHomeDir()
	var out []agentOption
	for _, e := range agent.Catalog() {
		r := agent.Status(e, home, exec.LookPath)
		out = append(out, agentOption{name: e.Name, ready: r.Ready(), hint: r.String()})
	}
	return out
}

// dispatchModel is the universal dispatch dialog overlay (pattern:
// capabilityModel). Persona is a selectable field cycling over every store
// persona; context only preselects persona/project/task defaults at open.
// dispatchScope is the working context a dispatch inherits from the pane it
// was opened from: which capability the user is looking through, and which of
// its lanes. Lane carries the DISPLAY name (Inbox/Pipeline/Out) — it is read
// by a human in the dialog, not parsed.
type dispatchScope struct {
	Capability string
	Lane       string
}

func (s dispatchScope) empty() bool { return s == dispatchScope{} }

// dispatchModel is the universal dispatch dialog. v3 is ACTION-FIRST: the
// user picks one action — a checklist — and the persona, the mode and the
// eligible tasks all derive from it, exactly as Compose derives them at
// launch. The dialog is therefore a view of the binding, never a second
// opinion about it.
//
// What v2 had and v3 does not: a persona cycler as the primary axis (the
// action names its persona), a checklist multi-select with checkboxes and
// row toggling (one dispatch runs one action), and a launch-override field
// (vehicle is launcher plumbing, and the user-facing half of it became the
// mode axis).
type dispatchModel struct {
	m      *Model
	active bool

	project   string
	taskTitle string
	scope     dispatchScope

	// Profile cycler. profiles are the record origins the actions carry,
	// plus a leading "all" that scopes to nothing; the action list filters
	// on the selection. Static when the project has only one.
	profiles      []string
	profileCursor int

	// The action list. actions holds every row; visible indexes the subset
	// the selected profile admits. optErr is the query failure rendered in
	// place of the list.
	actions      []compose.ActionRow
	visible      []int
	actionCursor int
	optErr       string

	// The task cycler, populated for a target: task action from its targets
	// expression. prefillTask is the task the dialog was opened on: it is
	// selected when the action admits it, and warned about when it does not.
	tasks       []*core.Task
	taskCursor  int
	prefillTask string

	// personaOverride and modeOverride are empty when the action's own
	// values stand. Neither is persisted (spec §5).
	personaOverride string
	modeOverride    string
	personas        []*core.Persona

	agents []agentOption
	cursor int

	targets      []string
	targetCursor int
	preview      string
	previewErr   string
	repos        []core.RepoConfig
	repoCursor   int
}

// action is the row under the cursor, or nil when the list is empty.
func (d *dispatchModel) action() *compose.ActionRow {
	if d.actionCursor < 0 || d.actionCursor >= len(d.visible) {
		return nil
	}
	i := d.visible[d.actionCursor]
	if i < 0 || i >= len(d.actions) {
		return nil
	}
	return &d.actions[i]
}

// persona is who this dispatch runs as: the override when set, else the
// action's own suits — the same precedence Compose applies.
func (d *dispatchModel) persona() string {
	if d.personaOverride != "" {
		return d.personaOverride
	}
	if a := d.action(); a != nil {
		return a.Persona
	}
	return ""
}

// personaSource labels where the persona came from, because a derived value
// and an overridden one look identical otherwise.
func (d *dispatchModel) personaSource() string {
	if d.personaOverride != "" {
		return "override"
	}
	if a := d.action(); a != nil && a.Persona != "" {
		return "from suits"
	}
	return "none — this action suits no persona"
}

// mode is the session's autonomy: the override when set, else the action's
// own. Empty action means the ad-hoc default.
func (d *dispatchModel) mode() string {
	if d.modeOverride != "" {
		return d.modeOverride
	}
	if a := d.action(); a != nil && a.Mode != "" {
		return a.Mode
	}
	return core.ChecklistModeEager
}

// modeSource annotates the Mode field the way §3.8 specifies: whether the
// value shown is the action's own or a deliberate departure from it.
func (d *dispatchModel) modeSource() string {
	if d.modeOverride == "" {
		return "checklist default"
	}
	def := core.ChecklistModeEager
	if a := d.action(); a != nil && a.Mode != "" {
		def = a.Mode
	}
	return "override (default: " + def + ")"
}

// nextMode cycles the launchable modes only. resident is in the vocabulary
// so the dialog can show it as coming, and Compose refuses it at bind time;
// cycling INTO it would offer the user a dispatch that cannot start.
func nextMode(cur string) string {
	if cur == core.ChecklistModeInteractive {
		return core.ChecklistModeEager
	}
	return core.ChecklistModeInteractive
}

// selectedTask is the task this dispatch runs on, or "" for a project-target
// action.
func (d *dispatchModel) selectedTask() *core.Task {
	if d.taskCursor < 0 || d.taskCursor >= len(d.tasks) {
		return nil
	}
	return d.tasks[d.taskCursor]
}

func (d *dispatchModel) taskID() string {
	if t := d.selectedTask(); t != nil {
		return t.ID
	}
	return ""
}

// needsTask reports whether the selected action operates on a task.
func (d *dispatchModel) needsTask() bool {
	a := d.action()
	return a != nil && a.Target == core.ChecklistTargetTask
}

// prefillIneligible reports the case §3.8 asks the dialog to warn about: the
// dialog was opened on a task the chosen action may not run on. The dispatch
// is still allowed — the launcher warns and proceeds — so this is a message,
// not a block.
func (d *dispatchModel) prefillIneligible() bool {
	if d.prefillTask == "" || !d.needsTask() {
		return false
	}
	return !slices.ContainsFunc(d.tasks, func(t *core.Task) bool { return t.ID == d.prefillTask })
}

// launchesTUI reports whether this dispatch routes to a fresh TUI. It is a
// property of the PERSONA's vehicle, not of the action.
func (d *dispatchModel) launchesTUI() bool {
	p := d.personaRecord(d.persona())
	return p != nil && compose.LaunchModeOf(*p) == "tui"
}

// personaRecord finds a persona by name in the dialog's list.
func (d *dispatchModel) personaRecord(name string) *core.Persona {
	for _, p := range d.personas {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// projectRequired reports whether this dispatch needs --project in its argv.
func (d *dispatchModel) projectRequired() bool {
	p := d.personaRecord(d.persona())
	if p == nil || d.launchesTUI() {
		return false
	}
	return !p.ProjectOptional
}

func (d *dispatchModel) target() string {
	if d.targetCursor == 0 {
		return ""
	}
	return d.targets[d.targetCursor]
}

// title is the spawned session's window title: the action and what it runs
// on, since that is what a human scanning their windows is looking for.
func (d *dispatchModel) title() string {
	if d.launchesTUI() {
		return d.persona()
	}
	a := d.action()
	if a == nil {
		return d.persona()
	}
	if id := d.taskID(); id != "" {
		return a.Name + ": " + id
	}
	if d.project != "" {
		return a.Name + ": " + d.project
	}
	return a.Name
}

// repoLabel renders the Repo: line's value: the selected repo's path, or
// "(cwd)" when no repos are recorded. Paths are truncated to the box's inner
// width with fitLine so a long path cannot widen the dialog.
func (d *dispatchModel) repoLabel() string {
	if len(d.repos) == 0 {
		return "‹ (cwd) ›"
	}
	r := d.repos[d.repoCursor]
	label := r.Path
	if r.Name != "" {
		label = r.Name + " · " + r.Path
	}
	return "‹ " + fitLine(label, bwInner(d.m.width)) + " ›"
}

// bwInner returns the inner text width of the dispatch dialog's width CAP
// for the given terminal width, mirroring renderOverlay's maxBW math. The
// box hugs its content below the cap, so a repo path truncated here can at
// most widen the dialog to the cap, consistently with the task title.
func bwInner(width int) int {
	bw := width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > width-4 {
		bw = width - 4
	}
	return bw - 4
}

// loadFor sets the dialog's context and loads the action list, preselecting
// the action that best fits where the user came from: the caller's default
// persona picks the first action that persona runs. Everything else — the
// task set, the mode, the warnings — derives from the selected action, so it
// is loaded by selectAction rather than here.
func (d *dispatchModel) loadFor(defaultPersona, project, taskID, taskTitle string, scope dispatchScope) {
	d.project, d.taskTitle, d.scope = project, taskTitle, scope
	d.prefillTask = taskID
	d.personas = d.dispatchPersonas(project)
	d.personaOverride, d.modeOverride = "", ""
	d.agents = d.m.agentOptionsFn()
	if cfg, err := d.m.store.GetAgentsConfig(); err == nil {
		for i := range d.agents {
			d.agents[i].model = cfg.Models[d.agents[i].name]
		}
	}
	d.cursor = 0
	for i, a := range d.agents { // preselect the first ready agent
		if a.ready {
			d.cursor = i
			break
		}
	}
	d.targets = []string{"auto", "herdr", "tmux", "terminal"}
	d.targetCursor = 0
	d.preview, d.previewErr = "", ""
	d.repos, d.repoCursor = nil, 0
	if project != "" {
		// RepoChannelTargets, not ProjectChannels: the dialog opens on a
		// keypress and must not shell out to git once per repo to draw a
		// picker. Status is the overlay's job.
		if targets, err := d.m.store.RepoChannelTargets(project); err == nil {
			d.repos = targets
		}
		if len(d.repos) == 0 { // legacy fallback until migrate-repos has run
			if repos, err := d.m.store.ProjectRepos(project); err == nil {
				d.repos = repos
			}
		}
	}
	d.loadActions(defaultPersona)
	if d.m.dispatcher == nil {
		d.previewErr = "dispatch unavailable in this build"
		return
	}
	d.refreshPreview()
}

// dispatchPersonas lists what the [p] override can select: the project's OWN
// records first (a persona is that project's operating identity, and the
// record wins the same name collision compose resolves), then the built-ins
// the binary carries. The machine-global custom store is pruned (plan §7),
// so this is the whole list — a persona outside the project goes into it
// with `atm persona set`.
func (d *dispatchModel) dispatchPersonas(project string) []*core.Persona {
	var out []*core.Persona
	seen := map[string]bool{}
	if project != "" {
		if recs, err := d.m.store.PersonaRecords(project); err == nil {
			for _, p := range recs {
				cp := p
				out = append(out, &cp)
				seen[p.Name] = true
			}
		}
	}
	for _, p := range d.m.store.ListPersonas() {
		if !seen[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

// loadActions fetches the action list for the CURRENT agent — warnings are
// agent-relative, so the list belongs to the agent as much as to the project
// — then builds the profile cycler from the origins found and selects the
// action matching defaultPersona.
func (d *dispatchModel) loadActions(defaultPersona string) {
	d.actions, d.visible, d.actionCursor, d.optErr = nil, nil, 0, ""
	d.tasks, d.taskCursor = nil, 0
	if d.project == "" || d.m.dispatchActionsFn == nil {
		return
	}
	rows, err := d.m.dispatchActionsFn(d.project, d.agentName())
	if err != nil {
		d.optErr = err.Error()
		return
	}
	d.actions = rows
	d.profiles = append([]string{allProfiles}, compose.AppliedProfiles(rows)...)
	if d.profileCursor >= len(d.profiles) {
		d.profileCursor = 0
	}
	d.applyProfileFilter()
	if defaultPersona != "" {
		for i, vi := range d.visible {
			if d.actions[vi].Persona == defaultPersona {
				d.actionCursor = i
				break
			}
		}
	}
	d.selectAction()
}

// allProfiles is the profile cycler's unscoped entry. §3.8 makes the cycler
// static when one profile is applied; with several, "all" is what lets the
// user see the whole roster at once.
const allProfiles = "all"

// applyProfileFilter recomputes the visible rows for the selected profile.
func (d *dispatchModel) applyProfileFilter() {
	d.visible = nil
	want := ""
	if d.profileCursor > 0 && d.profileCursor < len(d.profiles) {
		want = d.profiles[d.profileCursor]
	}
	for i, r := range d.actions {
		if want == "" || r.Origin == want {
			d.visible = append(d.visible, i)
		}
	}
	if d.actionCursor >= len(d.visible) {
		d.actionCursor = 0
	}
}

// selectAction reloads everything the CURSOR determines: the eligible task
// set and the per-dispatch overrides. §3.8 requires cursor movement to
// recompute the task set, persona, mode, warnings and preview live, and the
// overrides reset because they were chosen against the previous action —
// carrying an override across actions would silently apply a decision the
// user made about something else.
func (d *dispatchModel) selectAction() {
	d.personaOverride, d.modeOverride = "", ""
	d.tasks, d.taskCursor = nil, 0
	a := d.action()
	if a == nil || !d.needsTask() || d.m.eligibleTasksFn == nil {
		return
	}
	tasks, err := d.m.eligibleTasksFn(d.project, *a)
	if err != nil {
		d.optErr = err.Error()
		return
	}
	d.tasks = tasks
	for i, t := range d.tasks {
		if t.ID == d.prefillTask {
			d.taskCursor = i
			break
		}
	}
}

// agentName is the selection key of the agent under the cursor.
func (d *dispatchModel) agentName() string {
	if d.cursor < 0 || d.cursor >= len(d.agents) {
		return ""
	}
	return d.agents[d.cursor].name
}

// open loads the dialog's fields for the given context, then activates it.
func (d *dispatchModel) open(defaultPersona, project, taskID, taskTitle string, scope dispatchScope) {
	d.loadFor(defaultPersona, project, taskID, taskTitle, scope)
	d.active = true
}

// openOnAction opens the dialog with the cursor on a NAMED action — what a
// readiness surface's fix-it key does, since it knows exactly which action
// the user was looking at. An action that is not in the list leaves the
// cursor where loadFor put it: the dialog still opens, which teaches more
// than refusing to.
func (d *dispatchModel) openOnAction(project, action string) {
	d.loadFor("", project, "", "", dispatchScope{})
	for i, vi := range d.visible {
		if d.actions[vi].Name == action {
			d.actionCursor = i
			d.selectAction()
			break
		}
	}
	d.active = true
}

func (d *dispatchModel) refreshPreview() {
	d.preview, d.previewErr = "", ""
	target := ""
	if d.targetCursor > 0 {
		target = d.targets[d.targetCursor]
	}
	p, err := d.m.dispatcher.PreviewTarget(target)
	if err != nil {
		d.previewErr = err.Error()
	} else {
		d.preview = p
	}
}

// handleKey maps the v3 key plan (§3.8): j/k walks the ACTION list, h/l
// walks the eligible tasks, m cycles the mode, p overrides the persona, a
// cycles the agent, P scopes to a profile. Space and L are gone with the
// multi-select and the launch override they drove.
func (d *dispatchModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		d.active = false
	case "down", "j":
		if d.actionCursor < len(d.visible)-1 {
			d.actionCursor++
			d.selectAction()
		}
	case "up", "k":
		if d.actionCursor > 0 {
			d.actionCursor--
			d.selectAction()
		}
	case "left", "h":
		if len(d.tasks) > 0 {
			d.taskCursor = (d.taskCursor - 1 + len(d.tasks)) % len(d.tasks)
		}
	case "right", "l":
		if len(d.tasks) > 0 {
			d.taskCursor = (d.taskCursor + 1) % len(d.tasks)
		}
	case "a":
		if len(d.agents) > 0 {
			d.cursor = (d.cursor + 1) % len(d.agents)
			// Warnings are agent-relative, so the whole list is refetched
			// for the new agent.
			d.reloadForAgent()
		}
	case "m":
		next := nextMode(d.mode())
		d.modeOverride = next
		if a := d.action(); a != nil && next == a.Mode {
			d.modeOverride = "" // back to the action's own value
		}
	case "p":
		d.cyclePersona()
	case "P":
		if len(d.profiles) > 1 {
			d.profileCursor = (d.profileCursor + 1) % len(d.profiles)
			d.applyProfileFilter()
			d.selectAction()
		}
	case "r":
		if len(d.repos) > 0 {
			d.repoCursor = (d.repoCursor + 1) % len(d.repos)
		}
	case "t":
		d.targetCursor = (d.targetCursor + 1) % len(d.targets)
		d.refreshPreview()
	case "enter":
		d.submit()
	}
	return nil
}

// reloadForAgent refetches the action list for the newly selected agent and
// restores the cursor to the same action by NAME. Refetching is the point —
// the warnings are that agent's answer, not the project's — and restoring by
// name keeps the user where they were.
func (d *dispatchModel) reloadForAgent() {
	name := ""
	if a := d.action(); a != nil {
		name = a.Name
	}
	task := d.taskID()
	// The overrides are the USER's choices about this action, and the action
	// has not changed — only the agent has. selectAction drops them by
	// design (they were chosen against a different action), so they are
	// carried across this reload explicitly.
	persona, mode := d.personaOverride, d.modeOverride
	d.loadActions("")
	for i, vi := range d.visible {
		if d.actions[vi].Name == name {
			d.actionCursor = i
			break
		}
	}
	d.selectAction()
	d.personaOverride, d.modeOverride = persona, mode
	for i, t := range d.tasks {
		if t.ID == task {
			d.taskCursor = i
			break
		}
	}
}

// cyclePersona steps the [p] override through the project's personas. The
// action's own persona is reachable as "no override": the override is the
// exception, so returning to the derived value must be possible.
func (d *dispatchModel) cyclePersona() {
	if len(d.personas) == 0 {
		return
	}
	derived := ""
	if a := d.action(); a != nil {
		derived = a.Persona
	}
	cur := 0
	if d.personaOverride != "" {
		if i := slices.IndexFunc(d.personas, func(p *core.Persona) bool { return p.Name == d.personaOverride }); i >= 0 {
			cur = (i + 1) % len(d.personas)
		}
	} else if derived != "" {
		if i := slices.IndexFunc(d.personas, func(p *core.Persona) bool { return p.Name == derived }); i >= 0 {
			cur = (i + 1) % len(d.personas)
		}
	}
	if d.personas[cur].Name == derived {
		d.personaOverride = ""
		return
	}
	d.personaOverride = d.personas[cur].Name
}

// submit builds the argv for the chosen dispatch and spawns it. The argv is
// the v3 CLI form — `atm dispatch --checklist <action> ...` — so the spawned
// command is a reproducible record of exactly this dialog state (spec §6),
// and re-running it by hand does the same thing.
func (d *dispatchModel) submit() {
	if d.previewErr != "" {
		d.m.showToast("error: " + d.previewErr)
		return
	}
	a := d.action()
	if a == nil {
		d.m.showToast("error: no actions available for this project")
		return
	}
	persona := d.persona()
	if persona == "" {
		d.m.showToast("error: action " + a.Name + " suits no persona — pick one with [p]")
		return
	}
	if d.projectRequired() && d.project == "" {
		d.m.showToast("error: persona " + persona + " requires a project scope")
		return
	}
	if d.needsTask() && d.taskID() == "" {
		d.m.showToast("error: action " + a.Name + " runs on a task, and none is eligible")
		return
	}
	if len(d.agents) == 0 {
		d.m.showToast("error: agent catalog is empty")
		return
	}
	ag := d.agents[d.cursor]
	tui := d.launchesTUI()
	if !tui && !ag.ready {
		d.m.showToast("error: agent " + ag.name + " not ready: " + ag.hint)
		return
	}

	// A tui-vehicle persona opens a fresh TUI, which ignores project, agent,
	// task and capability alike — and has no action to run. Its argv is the
	// ad-hoc persona form, because that is literally what it does; naming an
	// action there would record a dispatch that never happens.
	if tui {
		argv := []string{"atm", "--persona", persona}
		d.spawn(argv, a.Name)
		return
	}

	argv := []string{"atm", "dispatch", "--checklist", a.Name}
	if d.project != "" {
		argv = append(argv, "--project", d.project)
	}
	if id := d.taskID(); id != "" && d.project != "" {
		argv = append(argv, "--task", id)
	}
	argv = append(argv, "--agent", ag.name)
	// The overrides ride only when they ARE overrides: an argv restating the
	// action's own persona and mode would record a decision the user never
	// made, and would go stale the moment the checklist changed.
	if d.personaOverride != "" {
		argv = append(argv, "--persona", d.personaOverride)
	}
	if d.modeOverride != "" {
		argv = append(argv, "--mode", d.modeOverride)
	}
	if d.scope.Capability != "" {
		argv = append(argv, "--capability", d.scope.Capability)
	}

	d.spawn(argv, a.Name)
}

// spawn hands the built argv to the dispatcher and closes the dialog.
func (d *dispatchModel) spawn(argv []string, label string) {
	dir, err := os.Getwd()
	if err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	if len(d.repos) > 0 {
		dir = d.repos[d.repoCursor].Path
	}
	if err := d.m.dispatcher.Spawn(dispatch.Spec{Title: d.title(), Argv: argv, Dir: dir, Target: d.target()}); err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	d.m.showToast("dispatched " + label + " → " + d.preview)
	d.active = false
	// A successful dispatch completes the action; land on the workspace
	// rather than reopening the spotlight over the toast (see completeAction).
	d.m.completeAction()
}

// renderOverlay draws the dialog. Box construction mirrors
// capabilityModel.renderOverlay (titledBoxHeight + styles.DialogBody) — reuse
// the same helpers and width conventions found there. The persona description,
// taskTitle, and repo path hints are truncated to the box's inner width with
// fitLine so a long value cannot widen the dialog.
func (d *dispatchModel) renderOverlay() string {
	styles := d.m.styles

	// maxBW is the cap — the formula the box used to be fixed at (mirrored
	// by bwInner for the repo-path truncation). The box now HUGS its
	// content: the body is measured untruncated first and the box takes the
	// widest line plus the layout's 4-column slack; only content wider than
	// the cap truncates, so a long value can at most widen the dialog to
	// the old fixed width, never past it.
	maxBW := d.m.width * 60 / 100
	if maxBW < 64 {
		maxBW = 64
	}
	if maxBW > d.m.width-4 {
		maxBW = d.m.width - 4
	}

	body := func(w int) string {
		var b strings.Builder
		b.WriteString(d.previewBody(w))
		// The footer advertises only keys that do something here: [h/l] with
		// no task cycler, or [P] with one profile, is an instruction to press
		// a key that is a no-op.
		help, help2 := "[j/k]action  [m]mode  [p]persona  [a]agent", "[t]target  [Enter]dispatch  [Esc]close"
		if d.needsTask() {
			help = "[j/k]action  [h/l]task  [m]mode  [p]persona  [a]agent"
		}
		if len(d.profiles) > 2 {
			help += "  [P]profile"
		}
		if d.project != "" && !d.launchesTUI() {
			help2 = "[r]repo  [t]target  [Enter]dispatch  [Esc]close"
		}
		b.WriteString("\n\n" + styles.KeyMenuDim.Render(help))
		b.WriteString("\n" + styles.KeyMenuDim.Render(help2))
		return b.String()
	}

	natural := body(maxBW * 2) // wide enough that only bwInner truncations apply
	bw := 0
	for _, line := range strings.Split(natural, "\n") {
		if w := lipgloss.Width(line) + 4; w > bw {
			bw = w
		}
	}
	if bw > maxBW {
		bw = maxBW
	}

	inner := body(bw - 4)
	bh := strings.Count(inner, "\n") + 3
	return titledBoxHeight(styles.DialogBody, bw, "Dispatch", inner, bh)
}

// previewBody renders the dialog's fields at content width w, without box
// chrome or the footer hint: the profile cycler, the ACTION list, the task
// cycler, the derived persona and mode, the repo, the agent, and the target.
// renderOverlay wraps it; the spotlight preview renders it directly, so a
// preview can never show something the overlay does not.
func (d *dispatchModel) previewBody(w int) string {
	styles := d.m.styles
	var b strings.Builder

	// The profile cycler is STATIC when the project has one profile: a
	// cycler over a single value is a control that does nothing.
	if len(d.profiles) > 2 {
		b.WriteString("Profile: ‹ " + d.profiles[d.profileCursor] + " ›\n\n")
	} else if len(d.profiles) == 2 {
		b.WriteString("Profile: " + d.profiles[1] + "\n\n")
	}

	b.WriteString(d.actionBlock(w))

	if d.needsTask() {
		b.WriteString(d.taskBlock(w))
	}

	persona := d.persona()
	if persona == "" {
		b.WriteString(styles.Error.Render("Persona: — · "+d.personaSource()) + "\n")
	} else {
		b.WriteString("Persona: " + persona + " · " + styles.FieldHint.Render(d.personaSource()) + "\n")
	}
	b.WriteString("Mode:    ‹ " + d.mode() + " › · " + styles.FieldHint.Render(d.modeSource()) + "\n")
	b.WriteString(styles.FieldHint.Render("         "+core.ChecklistModeResident+" (future)") + "\n\n")

	// The capability scope is not in §3.8's sketch, but it rides the argv and
	// narrows the rendered context, so a dialog that hides it hides part of
	// the dispatch it is about to start.
	if !d.scope.empty() {
		line := d.scope.Capability + " capability"
		if d.scope.Lane != "" {
			line += " · " + d.scope.Lane + " lane"
		}
		b.WriteString("Scope:   " + line + "\n\n")
	}

	if d.project != "" && !d.launchesTUI() {
		b.WriteString("Repo:    " + d.repoLabel() + "\n\n")
	}

	ag := agentOption{name: "—"}
	if len(d.agents) > 0 {
		ag = d.agents[d.cursor]
	}
	b.WriteString("Agent:   ‹ " + ag.label() + " ›\n")
	if ag.ready || d.launchesTUI() {
		b.WriteString(styles.Success.Render("         ready") + "\n")
	} else {
		b.WriteString(styles.Error.Render("         x "+ag.hint) + "\n")
	}

	// Warnings sit under the AGENT, because they are that agent's answer:
	// cycling the agent recomputes them (§3.10).
	if a := d.action(); a != nil {
		for _, warn := range a.Warnings {
			b.WriteString(styles.FieldHint.Render(fitLine("⚠ "+strings.TrimPrefix(warn, "checklist "+a.Name+": "), w)) + "\n")
		}
	}
	b.WriteString("\n")

	if d.previewErr != "" {
		b.WriteString(styles.Error.Render("Target:  x "+d.previewErr) + "\n")
	} else {
		b.WriteString("Target:  " + d.targets[d.targetCursor] + " · " + d.preview + " \"" + d.title() + "\"\n")
	}
	if d.projectRequired() && d.project == "" {
		b.WriteString(styles.Error.Render("⚠ "+persona+" requires a project scope") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// actionBlock renders the action list: one row per action with the persona
// it runs as and its purpose, greyed with its reasons when something is
// unmet. A greyed row stays selectable — warn never blocks (spec decision 4).
func (d *dispatchModel) actionBlock(w int) string {
	styles := d.m.styles
	if d.optErr != "" {
		return styles.Error.Render(fitLine("Action:  x "+d.optErr, w)) + "\n\n"
	}
	if len(d.visible) == 0 {
		return styles.FieldHint.Render("Action:  (this project has no checklists)") + "\n\n"
	}
	var b strings.Builder
	b.WriteString("Action:\n")
	for i, vi := range d.visible {
		a := d.actions[vi]
		marker := "  "
		if i == d.actionCursor {
			marker = "> "
		}
		persona := a.Persona
		if persona == "" {
			persona = "—"
		}
		line := marker + pad(a.Name, 15) + pad(persona, 11) + a.Purpose
		// A row with unmet requirements is GREYED and flagged; its reasons
		// are rendered in full under Agent for whichever row is selected.
		// Inlining them here truncated both the reason and the purpose to
		// uselessness — readiness texts are sentences, not the short labels
		// the mockup sketched — and the row is the wrong place to read a
		// sentence anyway.
		if len(a.Warnings) > 0 {
			b.WriteString(styles.FieldHint.Render(fitLine(line, w-2)+" ⚠") + "\n")
			continue
		}
		b.WriteString(fitLine(line, w) + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// taskBlock renders the task cycler for a target: task action — the selected
// task, how many are eligible, and the expression that decided. The count and
// the expression are shown because an empty or surprising list is a question
// about the expression, and the user cannot ask it without seeing it.
func (d *dispatchModel) taskBlock(w int) string {
	styles := d.m.styles
	var b strings.Builder
	if len(d.tasks) == 0 {
		b.WriteString(styles.Error.Render("Task:    ‹ none eligible ›") + "\n")
	} else {
		t := d.tasks[d.taskCursor]
		b.WriteString("Task:    ‹ " + t.ID + " ›  " + fitLine(t.Title, w-20) + "\n")
	}
	hint := plural(len(d.tasks), "eligible")
	if a := d.action(); a != nil && a.Targets != "" {
		hint += " · " + a.Targets
	}
	b.WriteString(styles.FieldHint.Render(fitLine("         "+hint, w)) + "\n")
	if d.prefillIneligible() {
		b.WriteString(styles.Error.Render(fitLine("         ⚠ "+d.prefillTask+" is not eligible for this action", w)) + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// pad right-pads to n columns so the action list reads as columns.
func pad(s string, n int) string {
	if len(s) >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-len(s))
}

// plural renders "N thing" / "N things" for the eligible-task count.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word
}
