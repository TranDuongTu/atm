package tui

import (
	"fmt"
	"strings"

	atmsetup "atm/internal/setup"
)

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
	b.WriteString(s.agentTable(width))

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
	} else if setupAnyReady(s.model.Agents) {
		// The wizard hands off; it never creates projects itself. Once at
		// least one agent can dispatch there is nothing left for THIS view
		// to do — pointing at project creation is more useful than an empty
		// CHANNELS/PERSONAS section that can't exist without a project.
		b.WriteString("\n" + styles.Muted.Render(fitLine(
			"  ready — press [Esc] then [a] on Projects to create your first project", width-4)) + "\n")
	}

	b.WriteString("\n" + styles.KeyMenuDim.Render("[Tab]section  [↑/↓]move  [Enter]detail  [r]refresh  [Esc]close"))
	return titledBoxHeight(styles.PaneActive, width, s.title(), b.String(), height)
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
