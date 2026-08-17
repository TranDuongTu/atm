package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	atmsetup "atm/internal/setup"
)

// setupMaxWidth caps the wizard's box. It replaces the workspace rather than
// overlaying it, but it is a dialog, not a pane: stretched across a wide
// terminal the eye has to travel from a glyph on the far left to a count on
// the far right, and the whole point is reading a row at a glance. Capped and
// centred, it reads as the one thing being asked about.
const setupMaxWidth = 100

// setupBoxWidth is the width the wizard LAYS OUT at — the column-drop ladder
// measures against this, not the terminal, so the table drops columns to fit
// the box the user actually sees.
func setupBoxWidth(width int) int {
	if width < setupMaxWidth {
		return width
	}
	return setupMaxWidth
}

// Column widths for the AGENTS table. Fixed rather than measured against
// content so the header and every data row — whatever setupColumns dropped —
// land in the same positions.
const (
	setupColAgentW     = 10
	setupColVerW       = 8
	setupColPluginW    = 8
	setupColLaunchersW = 14
	setupColModelW     = 12
	setupColChannelsW  = 8
)

// setupColumns decides which agent-table columns fit. The order is the drop
// order: the rightmost column goes first. AGENT and the status glyph are
// never droppable — a row the user cannot identify is worse than no table.
func setupColumns(width int) (ver, plugin, launchers, model, channels bool) {
	switch {
	case width >= 96:
		return true, true, true, true, true
	case width >= 78:
		return true, true, true, true, false
	case width >= 62:
		return true, true, true, false, false
	case width >= 48:
		return true, true, false, false, false
	default:
		return false, false, false, false, false
	}
}

// asyncCell renders a tier-2 fact: … while the probe is in flight, so a
// pending value never reads as a known-empty one.
func asyncCell(value string, landed bool) string {
	if !landed {
		return "…"
	}
	if value == "" {
		return "—"
	}
	return value
}

// render draws the wizard in place of the workspace: a header line (ATM/
// ollama versions, a probing indicator), the AGENTS table, then — with a
// project selected — CHANNELS and PERSONAS. Pure formatting over s.model; it
// reads nothing and spawns nothing, so it is safe to call every frame.
func (s *setupModel) render(termWidth, height int) string {
	styles := s.m.styles
	width := setupBoxWidth(termWidth)
	var b strings.Builder

	head := fmt.Sprintf("atm %s · ollama %s", s.model.ATMVersion, s.model.Ollama)
	if s.probing {
		head += " · probing…"
	}
	b.WriteString(styles.Muted.Render(fitLine(head, width-4)) + "\n")
	if s.loadErr != "" {
		b.WriteString(styles.Error.Render(fitLine(s.loadErr, width-4)) + "\n")
	}

	if s.drilled {
		b.WriteString(s.drill(width))
	} else {
		b.WriteString(s.agentsSection(width))
		if ps := s.model.Project; ps != nil {
			b.WriteString(s.channelsSection(ps, width))
			b.WriteString(s.personasSection(ps, width))
		} else if setupAnyReady(s.model.Agents) {
			// The wizard hands off; it never creates projects itself. Once at
			// least one agent can dispatch there is nothing left for THIS view
			// to do — pointing at the Projects pane is more useful than an
			// empty CHANNELS/PERSONAS section that can't exist without one.
			//
			// FIRST project only when the store genuinely has none. This
			// branch is also reached whenever no project is SELECTED, which on
			// a populated store is the common case — telling someone with five
			// projects to create their first one is simply false.
			hand := "  ready — press [Esc], then [s] on Projects to select one"
			if len(s.m.projects.list) == 0 {
				hand = "  ready — press [Esc], then [a] on Projects to create your first project"
			}
			b.WriteString("\n" + styles.Muted.Render(fitLine(hand, width-4)) + "\n")
		}
	}

	// Two footer lines, deliberately unalike. The top one is what this view
	// can DO for the focused section — the reason the wizard exists — so it
	// carries the bold accent. The bottom one is navigation, identical in
	// every section and every other overlay, so it stays dim and recedes.
	// Same-weight lines read as one run-on list of keys where nothing looks
	// more worth pressing than anything else.
	//
	// The action text belongs to the ladder itself (setup_actions.go) so a key
	// can never be advertised here without being bound there.
	b.WriteString("\n" + styles.KeyMenu.Render(fitLine(s.actionHints(), width-4)) + "\n")
	b.WriteString(styles.KeyMenuDim.Render(setupNavHints(s.drilled)))

	// Sized to its content and centred on both axes, the way every overlay in
	// this TUI sits. Stretching the box to the full content height left the
	// sections pinned to the top of a mostly-empty frame; a dialog that is as
	// tall as it needs to be, floating in the middle, is the same shape the
	// user already knows from the channels and personas overlays.
	//
	// It only falls back to a height-clamped box when the content genuinely
	// does not fit, because titledBoxHeight truncates from the BOTTOM — the
	// navigation footer is the first thing lost, so pay that cost only when a
	// short terminal forces it.
	box := titledBox(styles.PaneActive, width, s.title(), b.String())
	if height > 0 && lipgloss.Height(box) > height {
		box = titledBoxHeight(styles.PaneActive, width, s.title(), b.String(), height)
	}
	return lipgloss.Place(termWidth, height, lipgloss.Center, lipgloss.Center, box)
}

// setupNavHints is the navigation half of the footer. It differs by level for
// one reason: the top level offers [Enter]detail, and the drill offers the way
// back out. Every key it names works at that level — advertising one that
// silently does nothing is how a user concludes the arrows are broken.
func setupNavHints(drilled bool) string {
	if drilled {
		return "[Tab]section  [↑/↓]move  [r]refresh  [Esc]back"
	}
	return "[Tab]section  [↑/↓]move  [Enter]detail  [r]refresh  [Esc]close"
}

func (s *setupModel) agentsSection(width int) string {
	return "\n" + s.m.styles.HeaderLabel.Render("AGENTS") + "\n" + s.agentTable(width)
}

func (s *setupModel) channelsSection(ps *atmsetup.ProjectSetup, width int) string {
	styles := s.m.styles
	var b strings.Builder
	b.WriteString("\n" + styles.HeaderLabel.Render("CHANNELS · "+ps.Code) + "\n")
	if len(ps.Channels) == 0 {
		b.WriteString(styles.Muted.Render(fitLine("  no channels yet", width-4)) + "\n")
	}
	for i, ch := range ps.Channels {
		b.WriteString(s.row(setupSectionChannels, i, fmt.Sprintf("%s %-14s %-8s %s", ch.Glyph, ch.Name, ch.Type, ch.Note), width) + "\n")
	}
	return b.String()
}

func (s *setupModel) personasSection(ps *atmsetup.ProjectSetup, width int) string {
	styles := s.m.styles
	var b strings.Builder
	b.WriteString("\n" + styles.HeaderLabel.Render("PERSONAS · "+ps.Code) + "\n")
	switch {
	case s.checklistErr != "":
		// The capability's state could not be read, which is NOT the same as
		// the capability being off — and [e] would not fix it, so it is not
		// offered here.
		b.WriteString(styles.Error.Render(fitLine(
			"  cannot tell whether checklists are on for "+ps.Code+": "+s.checklistErr, width-4)) + "\n")
	case !ps.ChecklistCapEnabled:
		b.WriteString(styles.Muted.Render(fitLine(
			"  checklists are off for "+ps.Code+" — press [e] to enable the capability", width-4)) + "\n")
	}
	for i, p := range ps.Personas {
		b.WriteString(s.row(setupSectionPersonas, i, fmt.Sprintf("%-16s %d checklists · starters %d/%d",
			p.Persona, p.Checklists, p.StartersSeeded, p.StartersTotal), width) + "\n")
	}
	return b.String()
}

// drill renders the level Enter opens: the focused section's rows, and under
// them the detail for the row the cursor is on. Only the focused section is
// drawn — the box is height-clipped (titledBoxHeight), so carrying all three
// plus a body would push the footer off the bottom on a normal terminal — and
// keeping the rows means the cursor keys still have something to move, with
// the detail following them.
func (s *setupModel) drill(width int) string {
	styles := s.m.styles
	var b strings.Builder
	ps := s.model.Project
	switch {
	case s.section == setupSectionChannels && ps != nil:
		b.WriteString(s.channelsSection(ps, width))
	case s.section == setupSectionPersonas && ps != nil:
		b.WriteString(s.personasSection(ps, width))
	default:
		b.WriteString(s.agentsSection(width))
	}
	b.WriteString("\n" + styles.HeaderLabel.Render("DETAIL") + "\n")
	lines := s.detailLines()
	if len(lines) == 0 {
		// A section with nothing in it still has a detail: what is absent, and
		// what would end that.
		lines = []string{"nothing to detail yet — this section has no rows"}
	}
	for _, line := range lines {
		b.WriteString(styles.Body.Render(fitLine("  "+line, width-4)) + "\n")
	}
	return b.String()
}

// detailLines is the drill's body for the row under the cursor. It is pure
// formatting over facts the model ALREADY holds — no probe of its own, so the
// drill can never report something the table above it does not.
func (s *setupModel) detailLines() []string {
	switch s.section {
	case setupSectionChannels:
		ch, ok := s.currentChannel()
		if !ok {
			return nil
		}
		return s.channelDetail(ch)
	case setupSectionPersonas:
		p, ok := s.currentPersona()
		if !ok {
			return nil
		}
		return setupPersonaDetail(p)
	default:
		if s.cursor < 0 || s.cursor >= len(s.model.Agents) {
			return nil
		}
		return s.agentDetail(s.model.Agents[s.cursor])
	}
}

// agentDetail is the AGENTS drill body. Its reason to exist is the MCP server
// list: apply() computes it and `atm setup status` prints it, but no column in
// the table is wide enough to carry it, so this is the only place the wizard
// can answer "which servers does this harness actually have".
func (s *setupModel) agentDetail(row atmsetup.AgentRow) []string {
	landed := !s.probing
	out := []string{
		"version    " + asyncCell(row.Version, landed),
		"binary     " + row.Binary.String() + "     plugin  " + row.Plugin.String(),
		"launchers  " + setupLaunchersCell(row),
	}
	if missing := setupMissingFacts(row); missing != "" {
		out = append(out, "missing    "+missing)
	}
	out = append(out, "mcp        "+asyncCell(row.MCPState.String(), landed))
	switch {
	case !landed:
		out = append(out, "  the server list is still probing…")
	case row.MCPState != atmsetup.FactPresent:
		// The cardinal rule at the finest grain the wizard has: a list ATM
		// never managed to read is not a list of no servers.
		out = append(out, "  the server list is unknown — press [r] to probe again")
	case len(s.servers[row.Agent]) == 0:
		out = append(out, "  no servers configured — press [a] to add this project's servers")
	default:
		for _, sv := range s.servers[row.Agent] {
			out = append(out, fmt.Sprintf("  %-24s %s", sv.Name, setupConnectedWord(sv.Connected)))
		}
	}
	return out
}

// setupConnectedWord says what a server's health fact IS. An unknown health —
// codex's list reports configuration but not health — must never read as
// disconnected: offering [l] is honest, claiming the server is down is not.
func setupConnectedWord(f atmsetup.Fact) string {
	switch f {
	case atmsetup.FactPresent:
		return "connected"
	case atmsetup.FactAbsent:
		return "not connected — press [l] to authorize"
	default:
		return "health unknown"
	}
}

// setupMissingFacts names what stands between this row and ●. Absent and
// unknown are kept apart even though Glyph() grades them the same, because
// the fix is not the same: one is a thing to install, the other a probe to
// re-run.
func setupMissingFacts(row atmsetup.AgentRow) string {
	var parts []string
	for _, f := range []struct {
		name string
		fact atmsetup.Fact
	}{
		{"binary", row.Binary},
		{"plugin", row.Plugin},
	} {
		switch f.fact {
		case atmsetup.FactAbsent:
			parts = append(parts, f.name+" absent")
		case atmsetup.FactUnknown:
			parts = append(parts, f.name+" unknown")
		}
	}
	return strings.Join(parts, ", ")
}

// channelDetail is the CHANNELS drill body: the per-agent coverage map, which
// the table can only summarise as one glyph.
func (s *setupModel) channelDetail(ch atmsetup.ChannelRow) []string {
	out := []string{
		"type       " + ch.Type,
		"status     " + ch.Glyph + " " + ch.Note,
	}
	if ch.MCPServer != "" {
		out = append(out, "server     "+ch.MCPServer)
	} else {
		out = append(out, "server     — reached by path on this machine, not by an MCP server")
	}
	out = append(out, "coverage")
	// Ordered by the agent rows rather than by map iteration: a detail that
	// reshuffles between frames is unreadable.
	for _, row := range s.model.Agents {
		out = append(out, fmt.Sprintf("  %-24s %s", row.Agent, setupCoverageWord(ch.PerAgent[row.Agent])))
	}
	return out
}

// setupCoverageWord reports one agent's coverage of one channel, keeping the
// third state: an agent whose mcp probe could not answer does not cover this
// channel and does not fail to — ATM does not know.
func setupCoverageWord(f atmsetup.Fact) string {
	switch f {
	case atmsetup.FactPresent:
		return "covered"
	case atmsetup.FactAbsent:
		return "not covered"
	default:
		return "unknown"
	}
}

// setupPersonaDetail is the PERSONAS drill body: which shipped starters this
// persona is missing, and which have been edited since they were seeded.
func setupPersonaDetail(p atmsetup.PersonaRow) []string {
	out := []string{
		fmt.Sprintf("checklists %d · %d steps", p.Checklists, p.Steps),
		fmt.Sprintf("starters   %d of %d seeded", p.StartersSeeded, p.StartersTotal),
	}
	if len(p.MissingStarters) > 0 {
		out = append(out, "missing    "+strings.Join(p.MissingStarters, ", ")+"  — press [s] to author them")
	}
	if len(p.Customised) > 0 {
		// Informational, never actionable: a seeded starter is MEANT to be
		// edited (see setup.BuildPersonas).
		out = append(out, "customised "+strings.Join(p.Customised, ", "))
	}
	return out
}

// agentTable renders the AGENTS section's header and rows. setupColumns picks
// which optional columns fit at width; every tier-2 cell (VER, CHANNELS) goes
// through asyncCell so a probe still in flight reads as "…", never a guess or
// a blank that could be mistaken for a landed, empty answer.
func (s *setupModel) agentTable(width int) string {
	ver, plugin, launchers, model, channels := setupColumns(width)
	landed := !s.probing

	var b strings.Builder
	b.WriteString(s.m.styles.Muted.Render(fitLine("  "+setupAgentColumns(
		fmt.Sprintf("  %-*s", setupColAgentW, "AGENT"),
		ver, plugin, launchers, model, channels,
		"VER", "PLUGIN", "LAUNCHERS", "MODEL", "CHANNELS",
	), width-4)) + "\n")
	for i, row := range s.model.Agents {
		text := setupAgentColumns(
			fmt.Sprintf("%s %-*s", row.Glyph(), setupColAgentW, row.Agent),
			ver, plugin, launchers, model, channels,
			asyncCell(row.Version, landed),
			row.Plugin.String(),
			setupLaunchersCell(row),
			setupModelCell(row),
			setupChannelsCell(row, s.model.Project != nil, landed),
		)
		b.WriteString(s.row(setupSectionAgents, i, text, width) + "\n")
	}
	return b.String()
}

// setupAgentColumns joins one AGENT-table line's cells. identity (the
// glyph+name cell, or the header's own label) is always present; the rest
// are included only when setupColumns says the width allows it, so a header
// built from the same flags as its data rows can never drift out of
// alignment with the drop ladder.
func setupAgentColumns(identity string, ver, plugin, launchers, model, channels bool, verCell, pluginCell, launchersCell, modelCell, channelsCell string) string {
	cells := []string{identity}
	if ver {
		cells = append(cells, fmt.Sprintf("%-*s", setupColVerW, verCell))
	}
	if plugin {
		cells = append(cells, fmt.Sprintf("%-*s", setupColPluginW, pluginCell))
	}
	if launchers {
		cells = append(cells, fmt.Sprintf("%-*s", setupColLaunchersW, launchersCell))
	}
	if model {
		cells = append(cells, fmt.Sprintf("%-*s", setupColModelW, modelCell))
	}
	if channels {
		cells = append(cells, fmt.Sprintf("%-*s", setupColChannelsW, channelsCell))
	}
	return strings.Join(cells, " ")
}

// setupLaunchersCell shows which launch paths this agent can use — the
// native binary, ollama, or both — with * marking whichever one the current
// selection uses. Both facts are tier-1 (PATH lookups done at open/refresh),
// so this cell never waits on the async probe.
func setupLaunchersCell(row atmsetup.AgentRow) string {
	var parts []string
	if row.NativeOK == atmsetup.FactPresent {
		parts = append(parts, "native")
	}
	if row.OllamaOK == atmsetup.FactPresent {
		parts = append(parts, "ollama")
	}
	if len(parts) == 0 {
		return "none"
	}
	cell := strings.Join(parts, "+")
	if row.IsDefault {
		cell += "*"
	}
	return cell
}

// setupModelCell shows the model recorded for this agent's selection. This is
// a tier-1 fact (read from agents.json, not probed), so an absent selection
// reads as — rather than going through asyncCell's pending state.
func setupModelCell(row atmsetup.AgentRow) string {
	if row.Model == "" {
		return "—"
	}
	return row.Model
}

// setupChannelsCell shows this agent's channel coverage for the selected
// project. A channel only counts as covered once its server's tier-2 MCP
// state is known (see setup.Fill), so — unlike PLUGIN and LAUNCHERS — this
// cell genuinely depends on the async tier and goes through asyncCell too.
// With no project selected there is nothing to cover, which reads as — since
// that is a fact (no project), not a pending probe.
func setupChannelsCell(row atmsetup.AgentRow, hasProject, landed bool) string {
	if !hasProject {
		return "—"
	}
	return asyncCell(fmt.Sprintf("%d/%d", row.ChannelsOK, row.ChannelsAll), landed)
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
