package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"atm/internal/agent"
	"atm/internal/compose"
	"atm/internal/core"
	"atm/internal/profile"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// channelsModel is the read-only channels overlay: list every channel with
// its status, enter for detail. The one action is dispatching an ATTEST
// session to re-reach the endpoints the status shows; all writes go through
// `atm channel`.
type channelsModel struct {
	m       *Model
	open    bool
	cursor  int
	project string
	entries []core.ChannelView
	loadErr string
	detail  bool
	offset  int
	// readiness is THE computation (profile.ComputeReadiness), snapshotted
	// with the entries. Every judgment this overlay renders — attested,
	// stale, unwired — comes from here, so the overlay and `atm profile
	// status` cannot grade the same project differently. The channel VIEWS
	// are still read alongside it for record content the matrix does not
	// carry: purposes, addresses, and each stamp's note.
	readiness *profile.Readiness
	// agents are the attesting segments of this machine's configured agents
	// — the matrix columns.
	agents []string
	// loadedAt is the clock the status glyphs are computed against. It is
	// sampled once in openOverlay (with the entries) rather than per frame so
	// renderOverlay stays pure formatting over a snapshot, like every other
	// pane in this package.
	loadedAt time.Time
}

// dispatchDefaultPersona names the persona the overlay's attest dispatch
// preselects: the project's own manager record when it has one, else the
// manager every profile is expected to ship. No persona is special in the
// dialog itself — this is only the row the cursor opens on.
func (c *channelsModel) dispatchDefaultPersona() string {
	if recs, err := c.m.store.PersonaRecords(c.project); err == nil {
		for _, p := range recs {
			if p.Name == "manager" {
				return "manager"
			}
		}
	}
	return "manager"
}

// loadFor snapshots the project's channels and the clock the status glyphs
// are computed against. openOverlay calls it and then opens; the spotlight
// preview calls it alone, so previewing never opens the overlay.
func (c *channelsModel) loadFor(project string) {
	c.project = project
	c.entries, c.loadErr, c.readiness, c.agents = nil, "", nil, nil
	if views, err := c.m.store.ProjectChannels(project); err != nil {
		c.loadErr = err.Error()
	} else {
		c.entries = views
	}
	if project != "" {
		if cfg, err := c.m.store.GetAgentsConfig(); err == nil {
			c.agents = agent.AttestingAgents(cfg)
		}
		// The PROBED readiness, deliberately: unlike the dispatch dialog —
		// which opens on a keypress and must not shell out to git — this
		// overlay is opened on purpose to answer "can I reach these", and a
		// repo path that has vanished is exactly what it is being asked
		// about. The extra cost buys the answer the user came for.
		if r, err := compose.ReadinessFor(c.m.store, project, c.agents); err == nil {
			c.readiness = r
		}
	}
	c.loadedAt = time.Now()
	if c.cursor >= len(c.entries) {
		c.cursor = 0
	}
}

func (c *channelsModel) openOverlay(project string) {
	c.loadFor(project)
	c.open, c.detail, c.offset = true, false, 0
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
	case "v", "c":
		// [v] is the fix-it key §3.11 specifies; [c] is kept as the alias
		// that shipped with increment 7, so muscle memory built on it does
		// not break silently.
		//
		// It opens a prefilled ATTEST dispatch through the normal dialog:
		// reaching the endpoints this overlay grades is exactly what the
		// attest action does, and the dialog's agent cycler selects which
		// harness to attest. A scoped session with no --project would render
		// a literal `project <CODE>` placeholder, so refuse with a toast and
		// keep the overlay open.
		if c.project == "" {
			c.m.showToast("select a project first")
			return nil
		}
		c.open = false
		c.m.dispatchDlg.open(c.dispatchDefaultPersona(), c.project, "", "", dispatchScope{Capability: "channel"})
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
		body.WriteString(c.previewBody(bw-4) + "\n")
		body.WriteString("\n" + styles.KeyMenuDim.Render("[v]attest  [Esc]close"))
		return titledBoxHeight(styles.DialogBody, bw, c.title(), body.String(), 6)
	}

	if c.detail && c.cursor < len(c.entries) {
		return c.renderDetail(bw)
	}

	if len(c.entries) == 0 {
		var body strings.Builder
		body.WriteString(c.previewBody(bw-4) + "\n")
		body.WriteString("\n" + styles.KeyMenuDim.Render("[v]attest  [Esc]close"))
		return titledBoxHeight(styles.DialogBody, bw, c.title(), body.String(), 6)
	}

	var body strings.Builder
	body.WriteString(c.previewBody(bw-4) + "\n")
	body.WriteString("\n" + styles.KeyMenuDim.Render("[↑/↓]move  [Enter]detail  [v]attest  [Esc]close"))
	return titledBoxHeight(styles.DialogBody, bw, c.title(), body.String(), len(c.entries)+5)
}

// previewBody renders the channel list rows at content width w, without box
// chrome or the footer hint. renderOverlay wraps it; the spotlight preview
// renders it directly, so a preview can never show something the overlay
// does not.
func (c *channelsModel) previewBody(w int) string {
	if c.loadErr != "" {
		return fitLine("read channels: "+c.loadErr, w)
	}
	if len(c.entries) == 0 {
		msg := "no channels yet — add one with `atm channel add --project " + c.project + " --name <handle> --type repo`"
		if c.project == "" {
			msg = "no project selected — select one in the Projects pane first"
		}
		return fitLine(msg, w)
	}
	var body strings.Builder
	for i, line := range c.matrixRows(w) {
		if i == c.cursor {
			line = c.m.styles.RowCursor.Render(line)
		}
		body.WriteString(line + "\n")
	}
	if sum := c.summaryLine(w); sum != "" {
		body.WriteString("\n" + sum + "\n")
	}
	return strings.TrimRight(body.String(), "\n")
}

// matrixRows renders one line per channel: its name and type, then what each
// configured agent's worst endpoint attestation is. A channel with no
// endpoint gets the missing-endpoint row instead, naming the profile that
// expected it — "nothing can land here" is a different problem from "nobody
// has verified it lately", and reading the same as an unattested row would
// hide it.
func (c *channelsModel) matrixRows(w int) []string {
	rollups := map[string]profile.ChannelRollup{}
	if c.readiness != nil {
		for _, r := range profile.RollupByChannel(c.readiness.Matrix, c.agents) {
			rollups[r.Channel] = r
		}
	}
	nameW := 10
	for _, v := range c.entries {
		if len(v.Name) > nameW {
			nameW = len(v.Name)
		}
	}
	out := make([]string, 0, len(c.entries))
	for _, v := range c.entries {
		r, ok := rollups[v.Name]
		if !ok || r.Endpoints == 0 {
			out = append(out, fitLine(fmt.Sprintf("%s %-*s %-7s %s", "○", nameW, v.Name, v.Type, missingEndpointNote(v)), w))
			continue
		}
		out = append(out, fitLine(fmt.Sprintf("%s %-*s %-7s %s", c.rollupGlyph(r), nameW, v.Name, v.Type, c.agentCells(r)), w))
	}
	return out
}

// missingEndpointNote says what is missing and who expected it, short enough
// to survive the list row — the PROFILE is the part that must not truncate,
// since "why does this exist with nowhere to reach" is what it answers.
func missingEndpointNote(v core.ChannelView) string {
	if o, err := core.ParseOrigin(v.Origin); err == nil && o.Kind == core.OriginProfile {
		return "no endpoint — expected by " + v.Origin
	}
	return "no endpoint — nothing can land here"
}

// agentCells renders the per-agent rollup: "claude: attested  codex: 21d".
// An endpoint stuck below wiring is reported as the blocker instead, because
// no agent can attest what this machine cannot reach — grading the agent
// there would blame it for a wiring gap.
func (c *channelsModel) agentCells(r profile.ChannelRollup) string {
	if r.Unaddressed > 0 {
		return fmt.Sprintf("%d of %d endpoints have no address", r.Unaddressed, r.Endpoints)
	}
	if r.Unwired > 0 {
		return fmt.Sprintf("%d of %d endpoints unwired on this machine", r.Unwired, r.Endpoints)
	}
	if len(c.agents) == 0 {
		return "no agent configured — nothing can attest"
	}
	cells := make([]string, 0, len(c.agents))
	for _, a := range c.agents {
		cells = append(cells, a+": "+attestCell(r.Agents[a]))
	}
	return strings.Join(cells, "  ")
}

// rollupGlyph grades a channel from its ROLLUP rather than from
// core.ChannelStatus. ChannelStatus answers the pre-endpoint question — is
// this one wiring good — so on a multi-endpoint channel it can report a
// healthy glyph beside a row that says an endpoint is unwired. The glyph and
// the text it sits next to have to be the same judgment.
func (c *channelsModel) rollupGlyph(r profile.ChannelRollup) string {
	if r.Unaddressed > 0 || r.Unwired > 0 {
		return "○"
	}
	worst := ""
	for _, a := range c.agents {
		switch r.Agents[a].State {
		case profile.AttestNone:
			return "◐"
		case profile.AttestStale:
			worst = "◐"
		}
	}
	if worst != "" {
		return worst
	}
	if len(c.agents) == 0 {
		return "◐"
	}
	return "●"
}

// attestCell is one agent's answer in as few columns as it can be said.
func attestCell(a profile.Attestation) string {
	switch a.State {
	case profile.AttestFresh:
		return "ok"
	case profile.AttestStale:
		return fmt.Sprintf("%dd", a.Days)
	default:
		return "never"
	}
}

// summaryLine aggregates the whole matrix, so a reader who is not going to
// walk every row still learns whether anything is wrong.
func (c *channelsModel) summaryLine(w int) string {
	if c.readiness == nil {
		return ""
	}
	sum := profile.SummarizeMatrix(c.readiness.Matrix, c.agents)
	var parts []string
	if sum.Unaddressed > 0 {
		parts = append(parts, fmt.Sprintf("%d endpoint(s) unaddressed", sum.Unaddressed))
	}
	if sum.Unwired > 0 {
		parts = append(parts, fmt.Sprintf("%d unwired here", sum.Unwired))
	}
	for _, a := range sortedKeys(sum.NeverAttested) {
		parts = append(parts, fmt.Sprintf("%s never attested %d", a, sum.NeverAttested[a]))
	}
	for _, a := range sortedKeys(sum.Stale) {
		parts = append(parts, fmt.Sprintf("%s stale %d", a, sum.Stale[a]))
	}
	if len(parts) == 0 {
		return c.m.styles.Success.Render(fitLine(fmt.Sprintf("all %d endpoint(s) attested", sum.Endpoints), w))
	}
	return c.m.styles.FieldHint.Render(fitLine(strings.Join(parts, " · "), w))
}

// sortedKeys keeps the summary's agent order stable across renders.
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v > 0 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
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
	for _, ln := range channelDetailLines(v, c.project, c.agents, c.loadedAt) {
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
	body.WriteString("\n" + styles.KeyMenuDim.Render("[j/k]scroll  [v]attest  [Esc]back"))
	return titledBoxHeight(styles.DialogBody, bw, "Channel: "+v.Name, body.String(), height)
}

// detailAgents is the configured roster plus any agent that has stamped this
// endpoint, in a stable order: configured first (matrix order), then the
// extras alphabetically.
func detailAgents(configured []string, byAgent map[string]core.VerificationStamp) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(configured)+len(byAgent))
	for _, a := range configured {
		seen[a] = true
		out = append(out, a)
	}
	var extra []string
	for a, st := range byAgent {
		if a == "" || seen[a] || st.At == "" {
			continue
		}
		extra = append(extra, a)
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// detailIndent is the continuation indent for wrapped detail rows: it aligns
// under the value column produced by the "%-10s " field format.
const detailIndent = "           "

// wrapDetailLine hard-wraps one detail row to w columns instead of truncating
// it. A wiring path or clone URL is the field an acting session needs whole,
// so it must survive the box edge; wordwrap would leave these space-free
// tokens long.
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
func channelDetailLines(v core.ChannelView, project string, agents []string, now time.Time) []string {
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
	if len(v.Endpoints) == 0 {
		lines = append(lines, field("endpoints", missingEndpointNote(v)))
		lines = append(lines, field("", "`atm channel endpoint add --project "+project+" --name "+v.Name+" --type <type> ...`"))
		return lines
	}
	// Wiring and attestation are PER ENDPOINT. Every endpoint is listed —
	// including ones with no wiring and no stamp, which the old view skipped
	// entirely and so hid the gap it was supposed to report.
	for _, ep := range v.Endpoints {
		w := v.EndpointWiring(ep.Type)
		label := ep.Type
		if len(v.Endpoints) > 1 {
			label = ep.Type + " (" + ep.Role + ")"
		}
		lines = append(lines, field("endpoint", label))
		switch {
		case w.Path != "":
			lines = append(lines, field("  path", w.Path))
		case w.MCPServer != "":
			lines = append(lines, field("  mcp", w.MCPServer))
		default:
			how := "--path <dir>"
			if ep.Type != core.ChannelTypeRepo {
				how = "--mcp-server <name>"
			}
			lines = append(lines, field("  wiring", "none on this machine — `atm channel wire --project "+project+" --name "+v.Name+" --type "+ep.Type+" "+how+"`"))
		}
		// Stamps grouped BY AGENT, because attestation is per (endpoint ×
		// machine × agent): a flat list of stamps cannot answer "has codex
		// ever reached this", which is the question the matrix column asks.
		byAgent := core.AgentStamps(w.Stamps)
		// The matrix COLUMNS are the configured agents (§3.10 — not every
		// conceivable one). The DETAIL also lists any agent that has
		// actually stamped this endpoint, configured or not: a stamp is
		// evidence someone reached it, and dropping an agent from
		// agents.json must not erase that from the record.
		rows := detailAgents(agents, byAgent)
		if len(rows) == 0 {
			lines = append(lines, field("  attested", "no agent configured on this machine"))
			continue
		}
		for _, a := range rows {
			st, ok := byAgent[a]
			if !ok || st.At == "" {
				lines = append(lines, field("  "+a, "never attested"))
				continue
			}
			kind := st.Kind
			if kind == "" {
				kind = core.StampKindUse
			}
			row := st.At + " · " + kind
			if days, ok := core.StampAgeDays(st, now); ok {
				row = fmt.Sprintf("%s · %dd ago · %s", st.At, days, kind)
			}
			if st.By != "" {
				row += " · " + st.By
			}
			if st.Note != "" {
				row += " · " + st.Note
			}
			lines = append(lines, field("  "+a, row))
		}
	}
	if v.Probe != nil {
		lines = append(lines, "")
		lines = append(lines, field("probe", fmt.Sprintf("exists=%t  git=%t  dirty=%t", v.Probe.PathExists, v.Probe.IsGitRepo, v.Probe.Dirty)))
		lines = append(lines, field("", fmt.Sprintf("upstream=%t  ahead=%d  behind=%d", v.Probe.HasUpstream, v.Probe.Ahead, v.Probe.Behind)))
	}
	return lines
}
