package tui

import (
	"os"
	"os/exec"
	"slices"
	"strings"

	"atm/internal/agent"
	"atm/internal/core"
	"atm/internal/dispatch"

	tea "github.com/charmbracelet/bubbletea"
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

// checklistRow is one selectable row of the dialog's checklist multi-select.
// warnings carries the unmet-requires reasons: the row renders greyed with
// them but stays toggleable (warn never block, spec decision 4).
type checklistRow struct {
	name     string
	selected bool
	warnings []string
}

type dispatchModel struct {
	m             *Model
	active        bool
	personas      []*core.Persona
	personaCursor int
	project       string
	taskID        string
	taskTitle     string
	scope         dispatchScope
	agents        []agentOption
	cursor        int
	targets       []string
	targetCursor  int
	preview       string
	previewErr    string
	repos         []core.RepoConfig
	repoCursor    int
	// Checklist multi-select state, recomputed by loadOptions on open and on
	// every persona cycle. missing lists expected-but-absent shipped
	// checklists (warning rows, never selectable); optErr is the options
	// query failure rendered in place of the block.
	rows      []checklistRow
	missing   []string
	rowCursor int
	optErr    string
	// launchOverride is the per-dispatch launch mode override; "" means the
	// persona's own default. Never persisted (spec §5).
	launchOverride string
}

// selectedPersona returns the persona under the cursor, or nil.
func (d *dispatchModel) selectedPersona() *core.Persona {
	if d.personaCursor < 0 || d.personaCursor >= len(d.personas) {
		return nil
	}
	return d.personas[d.personaCursor]
}

func (d *dispatchModel) persona() string {
	if p := d.selectedPersona(); p != nil {
		return p.Name
	}
	return ""
}

// personaLaunch is the selected persona's own launch mode ("" when none).
func (d *dispatchModel) personaLaunch() string {
	if p := d.selectedPersona(); p != nil {
		return p.Launch
	}
	return ""
}

// effectiveLaunch is the launch mode this dispatch will use: the
// per-dispatch override when set, else the persona's own launch field.
func (d *dispatchModel) effectiveLaunch() string {
	if d.launchOverride != "" {
		return d.launchOverride
	}
	return d.personaLaunch()
}

// launchesTUI reports whether this dispatch routes to a fresh TUI (effective
// launch tui — admin's default among them). A TUI route ignores --project/
// --agent/--task/--capability/--checklist, so none of them ride its argv and
// agent readiness is irrelevant.
func (d *dispatchModel) launchesTUI() bool { return d.effectiveLaunch() == "tui" }

// nextLaunch cycles the per-dispatch launch override — "" (persona default)
// → prompt → hook → tui → "" — skipping the value that EQUALS the default:
// selecting it would be the default, not an override.
func nextLaunch(cur, def string) string {
	order := []string{"", "prompt", "hook", "tui"}
	i := slices.Index(order, cur)
	for {
		i = (i + 1) % len(order)
		if order[i] == def {
			continue
		}
		return order[i]
	}
}

// projectRequired reports whether the selected persona needs --project in its
// argv. Derived from the persona's project_optional spec; a tui-route
// dispatch is always project-optional because the TUI ignores --project.
func (d *dispatchModel) projectRequired() bool {
	p := d.selectedPersona()
	if p == nil {
		return false
	}
	if d.launchesTUI() {
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

func (d *dispatchModel) title() string {
	// A tui-route dispatch opens a fresh TUI that ignores --project, so
	// its title never carries a project scope (mirrors projectRequired).
	if d.launchesTUI() {
		return d.persona()
	}
	if d.taskID != "" {
		return d.taskID
	}
	if d.project != "" && d.persona() != "" {
		return d.project + " · " + d.persona()
	}
	return d.persona()
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

// bwInner returns the inner text width of the dispatch dialog box for the
// given terminal width, mirroring renderOverlay's box-width math so a long
// repo path truncates consistently with the task title.
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

// loadFor preselects the given default persona (falling back to the first
// store persona when it is not in the list — no name is special, spec
// decision 10), sets the context defaults, and refreshes the target preview
// — everything open does except flipping active. open calls it and then
// activates; the spotlight preview calls it alone, so previewing never
// activates the dialog. Dispatch logic never branches on how it was opened.
func (d *dispatchModel) loadFor(defaultPersona, project, taskID, taskTitle string, scope dispatchScope) {
	d.project, d.taskID, d.taskTitle, d.scope = project, taskID, taskTitle, scope
	d.personas = d.m.store.ListPersonas()
	d.personaCursor = 0
	for i, p := range d.personas {
		if p.Name == defaultPersona {
			d.personaCursor = i
			break
		}
	}
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
	d.loadOptions()
	if d.m.dispatcher == nil {
		d.previewErr = "dispatch unavailable in this build"
		return
	}
	d.refreshPreview()
}

// loadOptions recomputes the checklist rows and launch state for the
// selected persona — the reset-to-default semantics of spec §6: cycling the
// persona discards manual toggles and the launch override.
func (d *dispatchModel) loadOptions() {
	d.rows, d.missing, d.rowCursor, d.optErr, d.launchOverride = nil, nil, 0, "", ""
	p := d.selectedPersona()
	if p == nil || d.project == "" {
		return
	}
	opts, err := d.m.dispatchOptionsFn(p.Name, d.project, d.scope.Capability)
	if err != nil {
		d.optErr = err.Error()
		return
	}
	for _, r := range opts.Rows {
		d.rows = append(d.rows, checklistRow{name: r.Name, selected: r.Default, warnings: r.Warnings})
	}
	d.missing = opts.Missing
}

// open loads the dialog's fields for the given context, then activates it.
func (d *dispatchModel) open(defaultPersona, project, taskID, taskTitle string, scope dispatchScope) {
	d.loadFor(defaultPersona, project, taskID, taskTitle, scope)
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

func (d *dispatchModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		d.active = false
	case "p":
		if len(d.personas) > 0 {
			d.personaCursor = (d.personaCursor + 1) % len(d.personas)
			d.loadOptions()
		}
	case "left", "h":
		if d.cursor > 0 {
			d.cursor--
		}
	case "right", "l":
		if d.cursor < len(d.agents)-1 {
			d.cursor++
		}
	case "down", "j":
		if d.rowCursor < len(d.rows)-1 {
			d.rowCursor++
		}
	case "up", "k":
		if d.rowCursor > 0 {
			d.rowCursor--
		}
	case " ":
		if d.rowCursor >= 0 && d.rowCursor < len(d.rows) {
			d.rows[d.rowCursor].selected = !d.rows[d.rowCursor].selected
		}
	case "r":
		if len(d.repos) > 0 {
			d.repoCursor = (d.repoCursor + 1) % len(d.repos)
		}
	case "L":
		d.launchOverride = nextLaunch(d.launchOverride, d.personaLaunch())
	case "t":
		d.targetCursor = (d.targetCursor + 1) % len(d.targets)
		d.refreshPreview()
	case "enter":
		d.submit()
	}
	return nil
}

func (d *dispatchModel) submit() {
	if d.previewErr != "" {
		d.m.showToast("error: " + d.previewErr)
		return
	}
	p := d.selectedPersona()
	if p == nil {
		d.m.showToast("error: no personas available")
		return
	}
	if d.projectRequired() && d.project == "" {
		d.m.showToast("error: persona " + p.Name + " requires a project scope")
		return
	}
	if len(d.agents) == 0 {
		d.m.showToast("error: agent catalog is empty")
		return
	}
	a := d.agents[d.cursor]
	tui := d.launchesTUI()
	if !tui && !a.ready {
		d.m.showToast("error: agent " + a.name + " not ready: " + a.hint)
		return
	}
	argv := []string{"atm", "--persona", p.Name}
	// A known project rides along for every session-launching persona — a
	// project-optional persona still accepts --project, and a scoped session
	// needs it to render a project-bound context. A tui-launch persona
	// routes to a fresh TUI that ignores it.
	if d.project != "" && !tui {
		argv = append(argv, "--project", d.project)
	}
	if !tui {
		argv = append(argv, "--agent", a.name)
	}
	// --task rides only with --project: the CLI launcher rejects
	// "--task requires --project".
	if d.taskID != "" && d.project != "" && !tui {
		argv = append(argv, "--task", d.taskID)
	}
	if d.scope.Capability != "" && !tui {
		argv = append(argv, "--capability", d.scope.Capability)
	}
	// The explicit selection rides the argv so the spawned command is a
	// reproducible record (spec §6). A fully-empty selection emits nothing —
	// the launcher recomputes the same default; deselect-all therefore means
	// "default set", not "no checklists".
	if d.project != "" && !tui {
		for _, r := range d.rows {
			if r.selected {
				argv = append(argv, "--checklist", r.name)
			}
		}
	}
	if d.launchOverride != "" {
		argv = append(argv, "--launch", d.launchOverride)
	}
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
	d.m.showToast("dispatched " + p.Name + " → " + d.preview)
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

	// Box width mirrors capabilityModel.renderOverlay's computation; it is
	// computed before the content lines so the truncations below can use the
	// inner width.
	bw := d.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > d.m.width-4 {
		bw = d.m.width - 4
	}

	var b strings.Builder
	b.WriteString(d.previewBody(bw - 4))

	help := "[p]persona  [L]launch  [←/→]agent  [t]target"
	if d.project != "" {
		help = "[p]persona  [↑/↓]row  [space]toggle  [L]launch  [←/→]agent  [r]repo  [t]target"
	}
	b.WriteString("\n\n" + styles.KeyMenuDim.Render(help))
	b.WriteString("\n" + styles.KeyMenuDim.Render("[Enter]dispatch  [Esc]close"))

	bh := strings.Count(b.String(), "\n") + 3
	return titledBoxHeight(styles.DialogBody, bw, "Dispatch", b.String(), bh)
}

// previewBody renders the dialog's field summary (persona/task/scope/repo/
// agent/target and any warning) at content width w, without box chrome or
// the footer hint. renderOverlay wraps it; the spotlight preview renders it
// directly, so a preview can never show something the overlay does not.
func (d *dispatchModel) previewBody(w int) string {
	styles := d.m.styles
	var b strings.Builder
	if p := d.selectedPersona(); p != nil {
		b.WriteString("Persona: ‹ " + p.Name + " ›\n")
		b.WriteString(styles.FieldHint.Render("        "+fitLine(p.Description, w-6)) + "\n\n")
	}
	if d.taskID != "" {
		b.WriteString("Task:   " + d.taskID + "\n")
		b.WriteString(styles.FieldHint.Render("        "+fitLine(d.taskTitle, w-6)) + "\n\n")
	}
	if !d.scope.empty() {
		line := d.scope.Capability + " capability"
		if d.scope.Lane != "" {
			line += " · " + d.scope.Lane + " lane"
		}
		b.WriteString("Scope:  " + line + "\n\n")
	}
	if d.project != "" && !d.launchesTUI() {
		b.WriteString("Repo:   " + d.repoLabel() + "\n\n")
		b.WriteString(d.checklistBlock(w))
	}
	a := agentOption{name: "—"}
	if len(d.agents) > 0 {
		a = d.agents[d.cursor]
	}
	b.WriteString("Agent:  ‹ " + a.label() + " ›\n")
	if a.ready || d.launchesTUI() {
		b.WriteString(styles.Success.Render("        ready") + "\n\n")
	} else {
		b.WriteString(styles.Error.Render("        x "+a.hint) + "\n\n")
	}
	if d.persona() != "" {
		launch := "Launch: " + d.effectiveLaunch()
		if d.launchOverride != "" {
			launch += " (override)"
		}
		b.WriteString(launch + "\n")
	}
	if d.previewErr != "" {
		b.WriteString(styles.Error.Render("Target: x "+d.previewErr) + "\n")
	} else {
		b.WriteString("Target: " + d.targets[d.targetCursor] + " · " + d.preview + " \"" + d.title() + "\"\n")
	}
	if d.projectRequired() && d.project == "" {
		b.WriteString(styles.Error.Render("⚠ "+d.persona()+" requires a project scope") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// checklistBlock renders the multi-select: one row per project checklist
// (cursor marker, checkbox, name, greyed unmet-requires reasons), then one
// warning row per expected-but-absent shipped checklist. Empty when the
// project has no checklists and nothing is missing.
func (d *dispatchModel) checklistBlock(w int) string {
	styles := d.m.styles
	if d.optErr != "" {
		return styles.Error.Render(fitLine("Checklists: x "+d.optErr, w)) + "\n\n"
	}
	if len(d.rows) == 0 && len(d.missing) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Checklists:\n")
	for i, r := range d.rows {
		marker := "  "
		if i == d.rowCursor {
			marker = "> "
		}
		box := "[ ] "
		if r.selected {
			box = "[x] "
		}
		line := marker + box + r.name
		if len(r.warnings) > 0 {
			// The row already names the checklist — strip the shared
			// "checklist <name>: " prefix so the reasons fit the line.
			reasons := make([]string, len(r.warnings))
			for j, warn := range r.warnings {
				reasons[j] = strings.TrimPrefix(warn, "checklist "+r.name+": ")
			}
			b.WriteString(styles.FieldHint.Render(fitLine(line+"  "+strings.Join(reasons, "; "), w)) + "\n")
			continue
		}
		b.WriteString(fitLine(line, w) + "\n")
	}
	for _, name := range d.missing {
		b.WriteString(styles.Error.Render(fitLine("  ⚠ "+name+" — shipped seed not applied to this project", w)) + "\n")
	}
	b.WriteString("\n")
	return b.String()
}
