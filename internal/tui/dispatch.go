package tui

import (
	"os"
	"os/exec"
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
	name  string
	ready bool
	hint  string
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
type dispatchModel struct {
	m             *Model
	active        bool
	personas      []*core.Persona
	personaCursor int
	project       string
	taskID        string
	taskTitle     string
	capability    string
	agents        []agentOption
	cursor        int
	targets       []string
	targetCursor  int
	preview       string
	previewErr    string
	repos         []core.RepoConfig
	repoCursor    int
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

// projectRequired reports whether the selected persona needs --project in its
// argv. Derived from the persona's project_optional spec; admin is always
// project-optional because --persona admin routes to a fresh TUI that ignores
// --project.
func (d *dispatchModel) projectRequired() bool {
	p := d.selectedPersona()
	if p == nil {
		return false
	}
	if p.Name == "admin" {
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
	// admin routes to a fresh TUI that ignores --project, so its title never
	// carries a project scope (mirrors projectRequired's admin special-case).
	if d.persona() == "admin" {
		return "admin"
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

// loadFor preselects the given default persona (falling back to concierge
// when it is not in the store list), sets the context defaults, and
// refreshes the target preview — everything open does except flipping
// active. open calls it and then activates; the spotlight preview calls it
// alone, so previewing never activates the dialog. Dispatch logic never
// branches on how it was opened.
func (d *dispatchModel) loadFor(defaultPersona, project, taskID, taskTitle, capability string) {
	d.project, d.taskID, d.taskTitle, d.capability = project, taskID, taskTitle, capability
	d.personas = d.m.store.ListPersonas()
	d.personaCursor = 0
	for i, p := range d.personas {
		if p.Name == defaultPersona {
			d.personaCursor = i
			break
		}
	}
	if d.persona() != defaultPersona {
		// default not found: preselect concierge (project-optional, always
		// dispatchable) so the dialog always opens with a usable persona.
		for i, p := range d.personas {
			if p.Name == "concierge" {
				d.personaCursor = i
				break
			}
		}
	}
	d.agents = d.m.agentOptionsFn()
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
	if d.m.dispatcher == nil {
		d.previewErr = "dispatch unavailable in this build"
		return
	}
	d.refreshPreview()
}

// open loads the dialog's fields for the given context, then activates it.
func (d *dispatchModel) open(defaultPersona, project, taskID, taskTitle, capability string) {
	d.loadFor(defaultPersona, project, taskID, taskTitle, capability)
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
		if len(d.repos) > 0 {
			d.repoCursor = (d.repoCursor + 1) % len(d.repos)
		}
	case "up", "k":
		if len(d.repos) > 0 {
			d.repoCursor = (d.repoCursor - 1 + len(d.repos)) % len(d.repos)
		}
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
	if p.Name != "admin" && !a.ready {
		d.m.showToast("error: agent " + a.name + " not ready: " + a.hint)
		return
	}
	argv := []string{"atm", "--persona", p.Name}
	// A known project rides along for every non-admin persona — a
	// project-optional persona still accepts --project, and a scoped session
	// (e.g. concierge from the channels overlay) needs it to render a
	// project-bound context. admin routes to a fresh TUI that ignores it.
	if d.project != "" && p.Name != "admin" {
		argv = append(argv, "--project", d.project)
	}
	if p.Name != "admin" {
		argv = append(argv, "--agent", a.name)
	}
	// --task rides only with --project: the CLI launcher rejects
	// "--task requires --project".
	if d.taskID != "" && d.project != "" && p.Name != "admin" {
		argv = append(argv, "--task", d.taskID)
	}
	if d.capability != "" && p.Name != "admin" {
		argv = append(argv, "--capability", d.capability)
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

	help := "[p]persona  [←/→]agent  [t]target  [Enter]dispatch  [Esc]close"
	if d.project != "" {
		help = "[p]persona  [←/→]agent  [↑/↓]repo  [t]target  [Enter]dispatch  [Esc]close"
	}
	b.WriteString("\n\n" + styles.KeyMenuDim.Render(help))

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
	if d.capability != "" {
		b.WriteString("Scope:  " + d.capability + " capability\n\n")
	}
	if d.project != "" {
		b.WriteString("Repo:   " + d.repoLabel() + "\n\n")
	}
	a := agentOption{name: "—"}
	if len(d.agents) > 0 {
		a = d.agents[d.cursor]
	}
	b.WriteString("Agent:  ‹ " + a.name + " ›\n")
	if a.ready || d.persona() == "admin" {
		b.WriteString(styles.Success.Render("        ready") + "\n\n")
	} else {
		b.WriteString(styles.Error.Render("        x "+a.hint) + "\n\n")
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
