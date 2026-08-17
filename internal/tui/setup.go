package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"atm/internal/agent"
	"atm/internal/core"
	atmsetup "atm/internal/setup"
	"atm/internal/version"
	"atm/skills"

	tea "github.com/charmbracelet/bubbletea"
)

// setupSection is one section of the wizard. The names carry the `setup`
// prefix because this package already spends sectionActions/sectionViews/
// sectionReference on the spotlight's menu registry, and the two lists must
// never blur into one another in a single package namespace.
type setupSection int

const (
	setupSectionAgents setupSection = iota
	setupSectionChannels
	setupSectionPersonas
)

func (s setupSection) String() string {
	switch s {
	case setupSectionChannels:
		return "channels"
	case setupSectionPersonas:
		return "personas"
	default:
		return "agents"
	}
}

// setupProbeTimeout bounds each individual tier-2 subprocess (one
// `--version`, one `mcp list`). It mirrors internal/cli's probeTimeout: an
// unresponsive harness must not pin a goroutine forever, and here it must
// not leave the wizard reporting "probing" for the rest of the session.
const setupProbeTimeout = 10 * time.Second

// setupModel is the setup & readiness wizard's state machine: the probed
// snapshot, which section has focus, and whether the user has drilled into
// it. Structurally it is a sibling of channelsModel/personasModel, with one
// deliberate difference — it REPLACES the workspace instead of layering over
// it (see View), because it is a full-screen mode, not a modal. That is also
// why it must appear in workspaceIdle().
//
// The model is built in two tiers. reload() is tier 1: store reads and PATH
// lookups only, so the wizard is on screen immediately. probeCmd() is tier
// 2: one `--version` and one `mcp list` per agent, 1.6-3s each, run off the
// render path and folded in when setupProbedMsg lands.
type setupModel struct {
	m      *Model
	active bool

	// model is the snapshot every frame renders. Render is pure formatting
	// over it — nothing below View may read the store or PATH.
	model   atmsetup.Model
	section setupSection
	cursor  int
	drilled bool

	// run is the tier-2 subprocess runner, injected so a test can prove the
	// open + render path never spawns one.
	run atmsetup.RunFunc

	// probing is true from the moment tier 2 is fired until its message
	// lands. The cells it fills are not missing, they are merely not in yet,
	// and the view has to be able to say which.
	probing bool

	// versions/servers/states cache the last tier-2 answers. A reload (after
	// an action writes, or after the project scope changes) rebuilds tier 1
	// from scratch, and without this cache it would blank the async columns
	// until the next probe returned.
	versions map[string]string
	servers  map[string][]atmsetup.MCPServer
	states   map[string]atmsetup.Fact

	// loadErr records a failed store read from the last reload. A read
	// failure is reported, never rendered as an absent fact.
	loadErr string
}

// setupRun is the production tier-2 runner: run the harness's own verb and
// return its stdout (mirrors internal/cli's execRun).
func setupRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// setupProbedMsg carries the async tier home. It is a whole-tier snapshot
// rather than one message per agent so the columns swap atomically: a
// half-applied tier would leave some rows probed and some still pending with
// nothing in the model to tell them apart.
type setupProbedMsg struct {
	versions map[string]string
	servers  map[string][]atmsetup.MCPServer
	states   map[string]atmsetup.Fact
}

// open snapshots tier 1 and returns the command that runs tier 2. It never
// spawns a subprocess itself — everything on this path is a store read or a
// PATH lookup — so the wizard is on screen before the first `--version` call
// has even been made.
func (s *setupModel) open() tea.Cmd {
	s.active, s.section, s.cursor, s.drilled = true, setupSectionAgents, 0, false
	return s.refresh()
}

// refresh re-reads tier 1 and re-fires tier 2. It is what `r` does, and what
// open() does on the way in, so a manual refresh and a fresh open can never
// produce differently-built models.
func (s *setupModel) refresh() tea.Cmd {
	s.reload()
	s.probing = true
	return s.probeCmd()
}

// reload rebuilds the tier-1 snapshot: the stored agents config, PATH
// lookups, plugin files on disk, and the selected project's channels and
// checklists. No subprocess, so it is safe to call from a key handler.
func (s *setupModel) reload() {
	s.loadErr = ""
	cfg, err := s.m.store.GetAgentsConfig()
	if err != nil {
		s.loadErr = "read agents config: " + err.Error()
	}
	home, _ := os.UserHomeDir()
	s.model = atmsetup.Instant(cfg, atmsetup.Probes{
		LookPath: exec.LookPath,
		Run:      s.run,
		Home:     home,
		Now:      core.Now,
	})
	s.model.ATMVersion = version.Version
	s.apply()
}

// apply folds the cached tier-2 answers into the tier-1 snapshot and rebuilds
// the project sections, which depend on both (a channel's per-agent coverage
// is an MCP fact). Both the reload path and the probe-landed path go through
// it, so the two can never assemble the model differently.
func (s *setupModel) apply() {
	for i := range s.model.Agents {
		row := &s.model.Agents[i]
		row.Version = s.versions[row.Agent]
		// A missing key is FactUnknown, which is exactly right: an agent the
		// probe has not answered for yet has an unknown MCP state, never an
		// absent one.
		row.MCPState = s.states[row.Agent]
		row.MCPServers = nil
		for _, sv := range s.servers[row.Agent] {
			row.MCPServers = append(row.MCPServers, sv.Name)
		}
	}
	s.model.Project = s.buildProject()
	atmsetup.Fill(&s.model, s.model.Project)
	s.clampCursor()
}

// applyProbed lands the async tier: it caches the answers so later reloads
// keep them, then rebuilds the model around them.
func (s *setupModel) applyProbed(msg setupProbedMsg) {
	s.versions, s.servers, s.states = msg.versions, msg.servers, msg.states
	s.probing = false
	s.apply()
}

// buildProject builds the project sections for the selected project, or nil
// when none is selected: with no project the wizard is honestly global, so
// the project sections are ABSENT rather than empty-with-a-hint.
func (s *setupModel) buildProject() *atmsetup.ProjectSetup {
	code := s.m.projectScope
	if code == "" {
		return nil
	}
	views, err := s.m.store.ProjectChannels(code)
	if err != nil {
		s.loadErr = "read channels: " + err.Error()
	}
	ps := atmsetup.BuildProject(code, views, s.servers, s.states, s.model.ProbedAt)
	ps.ChecklistCapEnabled = s.checklistEnabled(code)
	// A disabled capability is not an empty personas section: there is
	// nothing to account for until it is on, so the rows stay absent and the
	// renderer offers to enable it instead.
	if ps.ChecklistCapEnabled {
		records, err := s.m.store.ChecklistRecords(code)
		if err != nil {
			s.loadErr = "read checklists: " + err.Error()
		}
		ps.Personas = atmsetup.BuildPersonas(setupPersonaNames(s.m.store.ListPersonas()), records, skills.ChecklistSeeds())
	}
	return ps
}

// checklistEnabled mirrors internal/cli's requireChecklistCapability rule —
// a nil Capabilities list is a legacy project, where every built-in reads as
// enabled — so the wizard and `atm setup status` say the same thing about
// the same project. It reads the project record rather than the injected
// registry because the question is what the CLI would allow, not which
// capabilities this binary happens to have compiled in. A project that
// cannot be read reads as NOT enabled: claiming it is on would offer a fix
// ladder that the CLI would then refuse.
func (s *setupModel) checklistEnabled(code string) bool {
	p, err := s.m.store.GetProject(code)
	if err != nil {
		return false
	}
	if p.Capabilities == nil {
		return true
	}
	for _, n := range p.Capabilities {
		if n == "checklist" {
			return true
		}
	}
	return false
}

// setupPersonaNames flattens the persona catalog to the names BuildPersonas
// wants (the same shape internal/cli's personaNames produces).
func setupPersonaNames(ps []*core.Persona) []string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	return names
}

// probeCmd runs tier 2 off the render path. It closes over the agent names
// and the runner ONLY — never over s — because Bubble Tea runs a Cmd on its
// own goroutine, where touching the model would race with Update.
func (s *setupModel) probeCmd() tea.Cmd {
	agents := make([]string, 0, len(s.model.Agents))
	for _, row := range s.model.Agents {
		agents = append(agents, row.Agent)
	}
	run := s.run
	return func() tea.Msg {
		msg := setupProbedMsg{
			versions: make(map[string]string, len(agents)),
			servers:  make(map[string][]atmsetup.MCPServer, len(agents)),
			states:   make(map[string]atmsetup.Fact, len(agents)),
		}
		for _, a := range agents {
			vctx, vcancel := context.WithTimeout(context.Background(), setupProbeTimeout)
			msg.versions[a] = atmsetup.ProbeVersion(vctx, a, run)
			vcancel()
			mctx, mcancel := context.WithTimeout(context.Background(), setupProbeTimeout)
			servers, state := atmsetup.ProbeMCP(mctx, a, run)
			mcancel()
			msg.servers[a], msg.states[a] = servers, state
		}
		return msg
	}
}

// sections is the reachable sections, in Tab order. The project sections are
// absent — not empty — when no project is selected, so Tab can never walk
// into a section that has nothing to say.
func (s *setupModel) sections() []setupSection {
	if s.model.Project == nil {
		return []setupSection{setupSectionAgents}
	}
	return []setupSection{setupSectionAgents, setupSectionChannels, setupSectionPersonas}
}

// rowCount is how many rows the focused section lists; it bounds the cursor.
func (s *setupModel) rowCount() int {
	switch s.section {
	case setupSectionChannels:
		if s.model.Project == nil {
			return 0
		}
		return len(s.model.Project.Channels)
	case setupSectionPersonas:
		if s.model.Project == nil {
			return 0
		}
		return len(s.model.Project.Personas)
	default:
		return len(s.model.Agents)
	}
}

// clampCursor keeps the cursor inside the focused section after a reload
// shrinks it (a channel removed by a CLI in another terminal, or the project
// scope changing under the wizard).
func (s *setupModel) clampCursor() {
	if s.cursor >= s.rowCount() {
		s.cursor = 0
	}
	// The section itself can vanish the same way — deselecting the project
	// takes the project sections with it.
	for _, sec := range s.sections() {
		if sec == s.section {
			return
		}
	}
	s.section, s.cursor, s.drilled = setupSectionAgents, 0, false
}

// handleKey drives the state machine: Tab cycles the sections that EXIST,
// j/k move within one, Enter drills and Esc peels — first the drill, then
// the view itself, so Esc never closes a wizard the user was reading a
// detail in. `r` re-runs both tiers.
func (s *setupModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		if s.drilled {
			s.drilled = false
			return nil
		}
		s.active = false
	case "tab", "shift+tab":
		secs := s.sections()
		idx := 0
		for i, sec := range secs {
			if sec == s.section {
				idx = i
			}
		}
		if k.String() == "shift+tab" {
			idx += len(secs) - 1
		} else {
			idx++
		}
		// Changing section peels the drill: a detail belongs to the section
		// it was opened in, and carrying it across would show one section's
		// body under another's heading.
		s.section, s.cursor, s.drilled = secs[idx%len(secs)], 0, false
	case "j", "down":
		if !s.drilled && s.cursor < s.rowCount()-1 {
			s.cursor++
		}
	case "k", "up":
		if !s.drilled && s.cursor > 0 {
			s.cursor--
		}
	case "g":
		if !s.drilled {
			s.cursor = 0
		}
	case "enter":
		// The drill is a level of the VIEW, not a property of a row: a
		// section with nothing in it still has a detail to show (what is
		// missing, and what would fix it), which is precisely the state the
		// user opened the wizard in.
		s.drilled = true
	case "r":
		return s.refresh()
	}
	return nil
}

// setModelFor records the model for an agent. It re-reads agents.json first
// — a CLI in another terminal can have changed the selection since the
// wizard was opened, and a fix that wrote the whole config back from this
// model's snapshot would silently revert it. The store's SetAgentModel is
// itself read-modify-write; the re-read here is for the KEY, which depends
// on the launcher the selection currently uses.
func (s *setupModel) setModelFor(agentName, model string) {
	cfg, err := s.m.store.GetAgentsConfig()
	if err != nil {
		s.m.showToast("read agents config: " + err.Error())
		return
	}
	key := agentName
	if sel, perr := agent.ParseSelectionKey(cfg.Selected); perr == nil && sel.Agent == agentName {
		key = sel.Key()
	}
	if err := s.m.store.SetAgentModel(key, model, s.m.actor); err != nil {
		s.m.showToast("set model: " + err.Error())
		return
	}
	// Re-read rather than patch the row: what the wizard shows next frame is
	// what the store now holds, including anything else that changed with it.
	s.reload()
	s.m.showToast(fmt.Sprintf("%s model: %s", key, model))
}

// render draws the wizard in place of the workspace.
//
// It is deliberately minimal: Task 9 owns the real renderer (the column-drop
// ladder, the MODEL column, and the async cells) in its own file. What is
// here is the section skeleton the state machine needs to be visible — the
// headings, each row's identity, and the offer to enable a disabled
// checklist capability. Pure formatting over s.model; it reads nothing.
func (s *setupModel) render(width, height int) string {
	styles := s.m.styles
	var b strings.Builder

	head := fmt.Sprintf("atm %s · ollama %s", s.model.ATMVersion, s.model.Ollama)
	if s.probing {
		head += " · probing…"
	}
	b.WriteString(styles.Muted.Render(fitLine(head, width-4)) + "\n")
	if s.loadErr != "" {
		b.WriteString(styles.Error.Render(fitLine(s.loadErr, width-4)) + "\n")
	}

	b.WriteString("\n" + styles.HeaderLabel.Render("AGENTS") + "\n")
	for i, row := range s.model.Agents {
		b.WriteString(s.row(setupSectionAgents, i, fmt.Sprintf("%s %-10s", row.Glyph(), row.Agent), width) + "\n")
	}

	if ps := s.model.Project; ps != nil {
		b.WriteString("\n" + styles.HeaderLabel.Render("CHANNELS · "+ps.Code) + "\n")
		if len(ps.Channels) == 0 {
			b.WriteString(styles.Muted.Render(fitLine("  no channels yet", width-4)) + "\n")
		}
		for i, ch := range ps.Channels {
			b.WriteString(s.row(setupSectionChannels, i, fmt.Sprintf("%s %-14s %-8s %s", ch.Glyph, ch.Name, ch.Type, ch.Note), width) + "\n")
		}

		b.WriteString("\n" + styles.HeaderLabel.Render("PERSONAS · "+ps.Code) + "\n")
		if !ps.ChecklistCapEnabled {
			b.WriteString(styles.Muted.Render(fitLine(
				"  checklists are off for "+ps.Code+" — press [e] to enable the capability", width-4)) + "\n")
		}
		for i, p := range ps.Personas {
			b.WriteString(s.row(setupSectionPersonas, i, fmt.Sprintf("%-16s %d checklists · starters %d/%d",
				p.Persona, p.Checklists, p.StartersSeeded, p.StartersTotal), width) + "\n")
		}
	}

	b.WriteString("\n" + styles.KeyMenuDim.Render("[Tab]section  [↑/↓]move  [Enter]detail  [r]refresh  [Esc]close"))
	return titledBoxHeight(styles.PaneActive, width, s.title(), b.String(), height)
}

// row formats one section row, highlighting it only when its own section has
// focus — the cursor is per-section, so two sections must never both look
// selected.
func (s *setupModel) row(sec setupSection, i int, text string, width int) string {
	line := fitLine("  "+text, width-4)
	if s.section == sec && s.cursor == i {
		return s.m.styles.RowCursor.Render(line)
	}
	return s.m.styles.Body.Render(line)
}

func (s *setupModel) title() string {
	if s.drilled {
		return "Setup · " + s.section.String()
	}
	return "Setup"
}
