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
	Spawn(dispatch.Spec) error
}

type dispatchKind int

const (
	dispatchNone dispatchKind = iota
	dispatchManager
	dispatchDeveloper
	dispatchConcierge
	dispatchAdmin
)

// projectRequired reports whether the persona needs --project in its argv.
// concierge and admin launch without a project.
func (k dispatchKind) projectRequired() bool {
	return k == dispatchManager || k == dispatchDeveloper
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

// dispatchModel is the dispatch dialog overlay (pattern: capabilityModel).
type dispatchModel struct {
	m          *Model
	kind       dispatchKind
	project    string
	taskID     string
	taskTitle  string
	agents     []agentOption
	cursor     int
	preview    string
	previewErr string
	repos      []core.RepoConfig
	repoCursor int
}

func (d *dispatchModel) persona() string {
	switch d.kind {
	case dispatchDeveloper:
		return "developer"
	case dispatchConcierge:
		return "concierge"
	case dispatchAdmin:
		return "admin"
	}
	return "manager"
}

func (d *dispatchModel) title() string {
	t := d.persona()
	if d.kind.projectRequired() {
		t = d.project + " · " + d.persona()
	}
	if d.taskID != "" {
		t += " · " + d.taskID
	}
	return t
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

func (d *dispatchModel) open(kind dispatchKind, project, taskID, taskTitle string) {
	d.kind, d.project, d.taskID, d.taskTitle = kind, project, taskID, taskTitle
	d.agents = d.m.agentOptionsFn()
	d.cursor = 0
	for i, a := range d.agents { // preselect the first ready agent
		if a.ready {
			d.cursor = i
			break
		}
	}
	d.preview, d.previewErr = "", ""
	d.repos, d.repoCursor = nil, 0
	if kind == dispatchDeveloper && project != "" {
		if repos, err := d.m.store.ProjectRepos(project); err == nil {
			d.repos = repos
		}
	}
	if d.m.dispatcher == nil {
		d.previewErr = "dispatch unavailable in this build"
		return
	}
	if p, err := d.m.dispatcher.Preview(); err != nil {
		d.previewErr = err.Error()
	} else {
		d.preview = p
	}
}

func (d *dispatchModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		d.kind = dispatchNone
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
	if len(d.agents) == 0 {
		d.m.showToast("error: agent catalog is empty")
		return
	}
	a := d.agents[d.cursor]
	if !a.ready {
		d.m.showToast("error: agent " + a.name + " not ready: " + a.hint)
		return
	}
	argv := []string{"atm", "--persona", d.persona()}
	if d.kind.projectRequired() {
		argv = append(argv, "--project", d.project)
	}
	argv = append(argv, "--agent", a.name)
	if d.taskID != "" {
		argv = append(argv, "--task", d.taskID)
	}
	dir, err := os.Getwd()
	if err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	if len(d.repos) > 0 {
		dir = d.repos[d.repoCursor].Path
	}
	if err := d.m.dispatcher.Spawn(dispatch.Spec{Title: d.title(), Argv: argv, Dir: dir}); err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	d.m.showToast("dispatched " + d.persona() + " → " + d.preview)
	d.kind = dispatchNone
}

// renderOverlay draws the dialog. Box construction mirrors
// capabilityModel.renderOverlay (titledBoxHeight + styles.DialogBody) —
// reuse the same helpers and width conventions found there. The taskTitle
// echo line is truncated to the box's inner width with fitLine so a long
// title cannot widen the dialog.
func (d *dispatchModel) renderOverlay() string {
	styles := d.m.styles

	// Box width mirrors capabilityModel.renderOverlay's computation; it is
	// computed before the task lines so the taskTitle truncation below can
	// use the inner width.
	bw := d.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > d.m.width-4 {
		bw = d.m.width - 4
	}

	var b strings.Builder
	if d.kind == dispatchDeveloper {
		b.WriteString("Task:   " + d.taskID + "\n")
		b.WriteString(styles.FieldHint.Render("        "+fitLine(d.taskTitle, bw-10)) + "\n\n")
		b.WriteString("Repo:   " + d.repoLabel() + "\n\n")
	}
	a := agentOption{name: "—"}
	if len(d.agents) > 0 {
		a = d.agents[d.cursor]
	}
	b.WriteString("Agent:  ‹ " + a.name + " ›\n")
	if a.ready {
		b.WriteString(styles.Success.Render("        ready") + "\n\n")
	} else {
		b.WriteString(styles.Error.Render("        x "+a.hint) + "\n\n")
	}
	if d.previewErr != "" {
		b.WriteString(styles.Error.Render("Target: x "+d.previewErr) + "\n")
	} else {
		b.WriteString("Target: " + d.preview + " \"" + d.title() + "\"\n")
	}
	help := "[←/→]agent  [Enter]dispatch  [Esc]close"
	if d.kind == dispatchDeveloper {
		help = "[←/→]agent  [↑/↓]repo  [Enter]dispatch  [Esc]close"
	}
	b.WriteString("\n" + styles.KeyMenuDim.Render(help))

	bh := strings.Count(b.String(), "\n") + 3
	dialogTitle := "Dispatch " + d.persona()
	if d.kind.projectRequired() {
		dialogTitle += " — " + d.project
	}
	return titledBoxHeight(styles.DialogBody, bw, dialogTitle, b.String(), bh)
}
