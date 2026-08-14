package tui

import (
	"fmt"
	"strings"
	"time"

	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// channelsModel is the read-only channels overlay: list every channel with
// its status, enter for detail. The one action is dispatching a concierge
// session to fix what the status shows; all writes go through `atm channel`.
type channelsModel struct {
	m       *Model
	open    bool
	cursor  int
	project string
	entries []core.ChannelView
	loadErr string
	detail  bool
	offset  int
	// loadedAt is the clock the status glyphs are computed against. It is
	// sampled once in openOverlay (with the entries) rather than per frame so
	// renderOverlay stays pure formatting over a snapshot, like every other
	// pane in this package.
	loadedAt time.Time
}

func (c *channelsModel) openOverlay(project string) {
	c.project = project
	c.entries, c.loadErr = nil, ""
	if views, err := c.m.store.ProjectChannels(project); err != nil {
		c.loadErr = err.Error()
	} else {
		c.entries = views
	}
	c.loadedAt = time.Now()
	c.open, c.detail, c.offset = true, false, 0
	if c.cursor >= len(c.entries) {
		c.cursor = 0
	}
}

func (c *channelsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "E":
		if c.detail {
			c.detail = false
			return nil
		}
		c.open = false
	case "j", "down":
		if c.detail {
			c.offset++
		} else if c.cursor < len(c.entries)-1 {
			c.cursor++
		}
	case "k", "up":
		if c.detail {
			if c.offset > 0 {
				c.offset--
			}
		} else if c.cursor > 0 {
			c.cursor--
		}
	case "g":
		// Top of the current view. In detail mode that means the top of THIS
		// channel's body — resetting the cursor there would swap the pane to
		// another channel's detail, since renderDetail reads entries[cursor]
		// live rather than from a snapshot taken on Enter.
		c.offset = 0
		if !c.detail {
			c.cursor = 0
		}
	case "enter":
		if !c.detail && len(c.entries) > 0 {
			c.detail, c.offset = true, 0
		}
	case "c":
		// The overlay's empty-state body explains the dead end, but `c` must
		// not dispatch anyway: a scoped session with no --project would render
		// a literal `project <CODE>` placeholder. Refuse with a toast and keep
		// the overlay open so the user can act on the hint.
		if c.project == "" {
			c.m.showToast("select a project first")
			return nil
		}
		c.open = false
		c.m.dispatchDlg.open("concierge", c.project, "", "", "channel")
	}
	return nil
}

// renderOverlay draws the channel list, or the scrolled detail of the
// selected channel. Box shape and cursor styling mirror
// personasModel.renderOverlay.
func (c *channelsModel) renderOverlay() string {
	styles := c.m.styles
	bw := c.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > c.m.width-4 {
		bw = c.m.width - 4
	}

	if c.loadErr != "" {
		var body strings.Builder
		body.WriteString(fitLine("read channels: "+c.loadErr, bw-4) + "\n")
		body.WriteString("\n" + styles.KeyMenuDim.Render("[c]dispatch concierge  [Esc]close"))
		return titledBoxHeight(styles.DialogBody, bw, c.title(), body.String(), 6)
	}

	if c.detail && c.cursor < len(c.entries) {
		return c.renderDetail(bw)
	}

	if len(c.entries) == 0 {
		var body strings.Builder
		msg := "no channels yet — add one with `atm channel add --project " + c.project + " --name <handle> --type repo`"
		if c.project == "" {
			msg = "no project selected — select one in the Projects pane first"
		}
		body.WriteString(fitLine(msg, bw-4) + "\n")
		body.WriteString("\n" + styles.KeyMenuDim.Render("[c]dispatch concierge  [Esc]close"))
		return titledBoxHeight(styles.DialogBody, bw, c.title(), body.String(), 6)
	}

	nameW := 10
	for _, v := range c.entries {
		if len(v.Name) > nameW {
			nameW = len(v.Name)
		}
	}
	var body strings.Builder
	for i, v := range c.entries {
		glyph, note := core.ChannelStatus(v, c.loadedAt)
		line := fmt.Sprintf("%s %-*s %-7s %s", glyph, nameW, v.Name, v.Type, note)
		line = fitLine(line, bw-4)
		if i == c.cursor {
			line = styles.RowCursor.Render(line)
		} else {
			line = styles.Body.Render(line)
		}
		body.WriteString(line + "\n")
	}
	body.WriteString("\n" + styles.KeyMenuDim.Render("[↑/↓]move  [Enter]detail  [c]dispatch concierge  [Esc]close"))
	return titledBoxHeight(styles.DialogBody, bw, c.title(), body.String(), len(c.entries)+5)
}

func (c *channelsModel) title() string {
	if c.project == "" {
		return "Channels"
	}
	return "Channels · " + c.project
}

// renderDetail draws the selected channel's full record, wiring, stamps and
// probe, scrolled by offset (same shape as the personas prompt view).
func (c *channelsModel) renderDetail(bw int) string {
	styles := c.m.styles
	v := c.entries[c.cursor]
	var lines []string
	for _, ln := range channelDetailLines(v, c.project, c.loadedAt) {
		lines = append(lines, wrapDetailLine(ln, bw-4)...)
	}

	height := c.m.height - 8
	if height < 8 {
		height = 8
	}
	if c.offset > len(lines)-1 {
		c.offset = len(lines) - 1
	}
	if c.offset < 0 {
		c.offset = 0
	}
	end := c.offset + height - 3
	if end > len(lines) {
		end = len(lines)
	}
	var body strings.Builder
	for _, ln := range lines[c.offset:end] {
		body.WriteString(fitLine(ln, bw-4) + "\n")
	}
	body.WriteString("\n" + styles.KeyMenuDim.Render("[j/k]scroll  [c]dispatch concierge  [Esc]back"))
	return titledBoxHeight(styles.DialogBody, bw, "Channel: "+v.Name, body.String(), height)
}

// detailIndent is the continuation indent for wrapped detail rows: it aligns
// under the value column produced by the "%-10s " field format.
const detailIndent = "           "

// wrapDetailLine hard-wraps one detail row to w columns instead of truncating
// it. A wiring path or clone URL is the field a concierge acts on, so it must
// survive the box edge; wordwrap would leave these space-free tokens long.
func wrapDetailLine(line string, w int) []string {
	if w <= len(detailIndent)+8 || lipgloss.Width(line) <= w {
		return []string{line}
	}
	out := []string{fitLine(line, w)}
	cont := w - len(detailIndent)
	for start := w; start < lipgloss.Width(line); start += cont {
		out = append(out, detailIndent+fitLineFrom(line, start, cont))
	}
	return out
}

// channelDetailLines formats one channel's detail body: identity and status,
// then the tier-1 record (purpose, address), then this machine's tier-2
// wiring with its stamps, then the local probe. Pure formatting — the caller
// supplies the clock. project is carried in only so the unwired hint can name
// a command that actually runs: `atm channel wire` is cobra.NoArgs and needs
// --project and --name.
func channelDetailLines(v core.ChannelView, project string, now time.Time) []string {
	glyph, note := core.ChannelStatus(v, now)
	field := func(label, value string) string { return fmt.Sprintf("%-10s %s", label, value) }

	lines := []string{
		field("type", v.Type),
		field("status", glyph+" "+note),
	}
	if v.TaskID != "" {
		lines = append(lines, field("task", v.TaskID))
	}
	if v.Purpose != "" {
		for i, ln := range strings.Split(v.Purpose, "\n") {
			if i == 0 {
				lines = append(lines, field("purpose", ln))
			} else {
				lines = append(lines, field("", ln))
			}
		}
	}
	for _, f := range []struct{ label, value string }{
		{"url", v.Address.URL},
		{"workspace", v.Address.Workspace},
		{"database", v.Address.Database},
		{"page", v.Address.Page},
	} {
		if f.value != "" {
			lines = append(lines, field(f.label, f.value))
		}
	}

	lines = append(lines, "")
	if v.Wiring == nil {
		how := "--path <dir>"
		if v.Type == core.ChannelTypeNotion {
			how = "--mcp-server <name>"
		}
		lines = append(lines, field("wiring", "none on this machine — `atm channel wire --project "+project+" --name "+v.Name+" "+how+"`"))
		return lines
	}
	if v.Wiring.Path != "" {
		lines = append(lines, field("path", v.Wiring.Path))
	}
	if v.Wiring.MCPServer != "" {
		lines = append(lines, field("mcp", v.Wiring.MCPServer))
	}
	if len(v.Wiring.Stamps) == 0 {
		lines = append(lines, field("stamps", "none — never verified"))
	} else {
		lines = append(lines, field("stamps", ""))
		for _, s := range v.Wiring.Stamps {
			row := s.At + " · " + s.By
			if s.Note != "" {
				row += " · " + s.Note
			}
			lines = append(lines, "  "+row)
		}
	}
	if v.Probe != nil {
		lines = append(lines, "")
		lines = append(lines, field("probe", fmt.Sprintf("exists=%t  git=%t  dirty=%t", v.Probe.PathExists, v.Probe.IsGitRepo, v.Probe.Dirty)))
		lines = append(lines, field("", fmt.Sprintf("upstream=%t  ahead=%d  behind=%d", v.Probe.HasUpstream, v.Probe.Ahead, v.Probe.Behind)))
	}
	return lines
}
