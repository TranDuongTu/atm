package tui

import (
	"fmt"
	"strings"

	"atm/internal/agent"
	"atm/internal/compose"
	"atm/internal/profile"

	tea "github.com/charmbracelet/bubbletea"
)

// profilesModel is the read-only Profiles overlay: the TUI twin of `atm
// profile status`. It answers "is this project set up, for the agent I am
// about to dispatch on" — the applied profiles with their sync counts, then
// one row per action with the rung it reaches per configured agent.
//
// It absorbs the setup wizard's is-this-set-up role, driven by real readiness
// data instead of a hand-maintained checklist of things to look at. Every fix
// is a named CLI command or a [d]/[v] dispatch through the normal dialog, so
// the overlay writes nothing and view_purity_test holds.
type profilesModel struct {
	m       *Model
	open    bool
	cursor  int
	project string
	loadErr string
	// readiness is THE computation. Nothing here re-derives a judgment it
	// already makes: a second opinion in a status surface is how a status
	// surface starts lying.
	readiness *profile.Readiness
	agents    []string
	// expanded shows the selected action's reason chain — every warning with
	// the command that answers it.
	expanded bool
	offset   int
}

// loadFor snapshots the project's readiness. Like the channels overlay it
// takes the PROBED reading: this surface is opened deliberately to answer
// whether the project is usable, and a repo path that has vanished is part
// of that answer.
func (p *profilesModel) loadFor(project string) {
	p.project, p.readiness, p.agents, p.loadErr = project, nil, nil, ""
	if project == "" {
		return
	}
	if cfg, err := p.m.store.GetAgentsConfig(); err == nil {
		p.agents = agent.AttestingAgents(cfg)
	}
	r, err := compose.ReadinessFor(p.m.store, project, p.agents)
	if err != nil {
		p.loadErr = err.Error()
		return
	}
	p.readiness = r
	if p.cursor >= len(p.actions()) {
		p.cursor = 0
	}
}

func (p *profilesModel) actions() []profile.ActionReadiness {
	if p.readiness == nil {
		return nil
	}
	return p.readiness.Actions
}

// selected is the action row under the cursor, or nil.
func (p *profilesModel) selected() *profile.ActionReadiness {
	acts := p.actions()
	if p.cursor < 0 || p.cursor >= len(acts) {
		return nil
	}
	return &acts[p.cursor]
}

func (p *profilesModel) openOverlay(project string) {
	p.loadFor(project)
	p.open, p.expanded, p.offset = true, false, 0
}

func (p *profilesModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "P":
		if p.expanded {
			p.expanded = false
			return nil
		}
		p.open = false
	case "j", "down":
		if p.expanded {
			p.offset++
		} else if p.cursor < len(p.actions())-1 {
			p.cursor++
		}
	case "k", "up":
		if p.expanded {
			if p.offset > 0 {
				p.offset--
			}
		} else if p.cursor > 0 {
			p.cursor--
		}
	case "g":
		p.offset = 0
		if !p.expanded {
			p.cursor = 0
		}
	case "enter":
		if !p.expanded && len(p.actions()) > 0 {
			p.expanded, p.offset = true, 0
		}
	case "d":
		// Dispatch THIS action. The overlay does not fix anything itself;
		// it hands the dispatch to the dialog, which is the one place a
		// session is bound.
		a := p.selected()
		if a == nil || p.project == "" {
			return nil
		}
		p.open = false
		p.m.dispatchDlg.openOnAction(p.project, a.Name)
	case "v":
		// Prefill attest, the same fix-it the channels overlay offers.
		if p.project == "" {
			return nil
		}
		p.open = false
		p.m.dispatchDlg.openOnAction(p.project, attestActionName)
	}
	return nil
}

// attestActionName is the action every profile is expected to ship for
// verifying its channels on the current agent.
const attestActionName = "attest"

func (p *profilesModel) title() string {
	if p.project == "" {
		return "Profiles"
	}
	return "Profiles · " + p.project
}

func (p *profilesModel) renderOverlay() string {
	styles := p.m.styles
	bw := p.m.width * 70 / 100
	if bw < 72 {
		bw = 72
	}
	if bw > p.m.width-4 {
		bw = p.m.width - 4
	}
	var body strings.Builder
	body.WriteString(p.previewBody(bw-4) + "\n")
	switch {
	case p.expanded:
		body.WriteString("\n" + styles.KeyMenuDim.Render("[j/k]scroll  [d]dispatch  [v]attest  [Esc]back"))
	default:
		body.WriteString("\n" + styles.KeyMenuDim.Render("[↑/↓]move  [Enter]why  [d]dispatch  [v]attest  [Esc]close"))
	}
	h := len(p.actions()) + len(p.appliedLines()) + 7
	if p.expanded {
		h = p.m.height - 8
		if h < 10 {
			h = 10
		}
	}
	return titledBoxHeight(styles.DialogBody, bw, p.title(), body.String(), h)
}

// previewBody renders the overlay's content at width w without box chrome,
// so the spotlight preview and the overlay cannot show different things.
func (p *profilesModel) previewBody(w int) string {
	styles := p.m.styles
	if p.project == "" {
		return fitLine("no project selected — select one in the Projects pane first", w)
	}
	if p.loadErr != "" {
		return fitLine("read readiness: "+p.loadErr, w)
	}
	if p.readiness == nil {
		return fitLine("no readiness for this project", w)
	}
	if p.expanded {
		return p.reasonChain(w)
	}

	var b strings.Builder
	for _, line := range p.appliedLines() {
		b.WriteString(fitLine(line, w) + "\n")
	}
	b.WriteString("\n")
	if len(p.agents) == 0 {
		b.WriteString(styles.Error.Render(fitLine("no agent configured on this machine — `atm agents select <name>`", w)) + "\n")
	}
	if len(p.actions()) == 0 {
		b.WriteString(fitLine("no actions — apply a profile with `atm profile apply --project "+p.project+" --name <profile>`", w))
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString(fitLine(p.tableHeader(), w) + "\n")
	for i, a := range p.actions() {
		line := fitLine(p.actionRow(a), w)
		if i == p.cursor {
			line = styles.RowCursor.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// appliedLines renders each applied profile with its sync counts. The
// profiles are DERIVED from record origins (see profile.ProfileSync), so
// this cannot claim a profile is applied that no record came from.
func (p *profilesModel) appliedLines() []string {
	if p.readiness == nil || len(p.readiness.Profiles) == 0 {
		return []string{"no profile applied — records are this project's own"}
	}
	out := make([]string, 0, len(p.readiness.Profiles))
	for _, s := range p.readiness.Profiles {
		parts := []string{fmt.Sprintf("%d in sync", s.InSync)}
		if s.Modified > 0 {
			parts = append(parts, fmt.Sprintf("%d modified", s.Modified))
		}
		if s.Missing > 0 {
			parts = append(parts, fmt.Sprintf("%d missing", s.Missing))
		}
		line := fmt.Sprintf("%-22s %s", s.Ref, strings.Join(parts, " · "))
		switch {
		case !s.Available:
			line += "  (not installed here)"
		case s.Latest != "" && s.Latest != s.Ref:
			line += "  (" + s.Latest + " available)"
		}
		out = append(out, line)
	}
	return out
}

// tableHeader names the agent columns. The rung a row reports is
// agent-relative below "wired", which is why the columns exist at all.
func (p *profilesModel) tableHeader() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-16s %-10s", "action", "persona")
	for _, a := range p.agents {
		fmt.Fprintf(&b, " %-12s", a)
	}
	return b.String()
}

func (p *profilesModel) actionRow(a profile.ActionReadiness) string {
	var b strings.Builder
	persona := a.Persona
	if persona == "" {
		persona = "—"
	}
	fmt.Fprintf(&b, "%-16s %-10s", a.Name, persona)
	if len(p.agents) == 0 {
		fmt.Fprintf(&b, " %s", a.Rung[""])
		return b.String()
	}
	for _, ag := range p.agents {
		fmt.Fprintf(&b, " %-12s", rungCell(a.Rung[ag]))
	}
	return b.String()
}

// rungCell renders one rung. "attested" is the top of the ladder, so it
// reads as ready; anything below names the rung it stopped at, because the
// rung IS the diagnosis.
func rungCell(rung string) string {
	if rung == profile.RungAttested {
		return "ready"
	}
	if rung == "" {
		return "—"
	}
	return rung
}

// reasonChain is the [enter] view: every warning holding this action back,
// bottom rung first, each with the command that answers it. It is the whole
// point of the overlay — a rung name says WHERE an action stopped, and the
// chain says what to type.
func (p *profilesModel) reasonChain(w int) string {
	a := p.selected()
	if a == nil {
		return fitLine("no action selected", w)
	}
	var lines []string
	lines = append(lines, "action   "+a.Name)
	if a.Persona != "" {
		lines = append(lines, "persona  "+a.Persona)
	}
	if len(a.Channels) > 0 {
		lines = append(lines, "channels "+strings.Join(a.Channels, ", "))
	}
	agents := p.agents
	if len(agents) == 0 {
		agents = []string{""}
	}
	for _, ag := range agents {
		lines = append(lines, "")
		label := ag
		if label == "" {
			label = "(no agent configured)"
		}
		lines = append(lines, label+": "+rungCell(a.Rung[ag]))
		ws := a.Warnings[ag]
		if len(ws) == 0 {
			lines = append(lines, "  nothing is holding this back")
			continue
		}
		for _, warn := range ws {
			lines = append(lines, "  "+warn.Rung+": "+warn.Text)
			if warn.Command != "" {
				lines = append(lines, "    "+warn.Command)
			}
		}
	}

	var wrapped []string
	for _, ln := range lines {
		wrapped = append(wrapped, wrapDetailLine(ln, w)...)
	}
	height := p.m.height - 12
	if height < 6 {
		height = 6
	}
	if p.offset > len(wrapped)-1 {
		p.offset = len(wrapped) - 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
	end := p.offset + height
	if end > len(wrapped) {
		end = len(wrapped)
	}
	var b strings.Builder
	for _, ln := range wrapped[p.offset:end] {
		b.WriteString(fitLine(ln, w) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// DegradedActions counts the actions that fall short of attested for the
// given agent — the number the header glyph reports.
func DegradedActions(r *profile.Readiness, agent string) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, a := range r.Actions {
		if a.Rung[agent] != profile.RungAttested {
			n++
		}
	}
	return n
}
