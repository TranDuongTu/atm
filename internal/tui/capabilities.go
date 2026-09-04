package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"atm/internal/capability"
	"atm/internal/core"
)

// capEntry is one row of the [C] switcher overlay.
type capEntry struct {
	name    string
	summary string
	enabled bool
}

// capabilityModel owns the capability concern of pane [2]: which capabilities
// are listed and enabled, which is current, the [C] switcher overlay, and the
// persistence of the selection (config.json -> boards.capability). The ring
// (boardsModel) and the header (tasksModel) read from it; it never renders
// their surfaces.
type capabilityModel struct {
	m       *Model
	current string
	entries []capEntry
	open    bool
	cursor  int
}

func newCapabilityModel(m *Model) capabilityModel { return capabilityModel{m: m} }

// refresh rebuilds the switcher entries and re-resolves current. It MUST run
// before lanesModel.refresh in refreshAll — the lanes are scoped to current.
//
// Only FLOW capabilities are listed. Pane [2] is one flow capability × one
// lane, so a registry capability has no lanes to show. Disabled flows stay
// listed so the overlay can still enable one.
func (c *capabilityModel) refresh() {
	c.entries = nil
	scope := c.m.projectScope
	if scope == "" {
		c.current = ""
		return
	}
	names := c.flowNames(c.m.regFor(scope))
	enabled := map[string]bool{}
	for _, n := range names {
		enabled[n] = true
	}
	listed := map[string]bool{}
	for _, n := range c.flowNames(c.m.reg) {
		listed[n] = true
	}
	for _, d := range c.m.reg.Describe() {
		if !listed[d.Name] {
			continue
		}
		c.entries = append(c.entries, capEntry{
			name:    d.Name,
			summary: d.Summary,
			enabled: enabled[d.Name],
		})
	}
	c.current = c.resolveCurrent(names)
	if c.cursor >= len(c.entries) {
		c.cursor = len(c.entries) - 1
	}
	if c.cursor < 0 {
		c.cursor = 0
	}
}

// flowNames is the registry's flow capabilities by name, in registration
// order — which is also the order the fallback picks "first" from.
func (c *capabilityModel) flowNames(reg *capability.Registry) []string {
	var out []string
	for _, f := range reg.Flows() {
		out = append(out, f.Name())
	}
	return out
}

// resolveCurrent applies the resolution rule: the in-session current if still
// valid, else the persisted boards.capability if valid, else the first
// enabled flow, else "". Valid means "an enabled FLOW" — a project that
// persisted a capability which is no longer one (a deleted capability, or a
// registry capability) falls back silently rather than showing a pane with
// no lanes. Never writes back — only switchTo persists.
func (c *capabilityModel) resolveCurrent(enabledNames []string) string {
	valid := func(v string) bool {
		for _, n := range enabledNames {
			if n == v {
				return true
			}
		}
		return false
	}
	if c.current != "" && valid(c.current) {
		return c.current
	}
	if cfg, err := c.m.store.GetBoardsConfig(c.m.projectScope); err == nil && cfg != nil && cfg.Capability != "" && valid(cfg.Capability) {
		return cfg.Capability
	}
	if len(enabledNames) > 0 {
		return enabledNames[0]
	}
	return ""
}

// switchTo makes name the current capability: persist (best-effort — the
// in-memory switch survives a failed write), rebuild the ring for the new
// scope, and reset the task focus through the boards channel.
func (c *capabilityModel) switchTo(name string) {
	scope := c.m.projectScope
	if scope == "" {
		return
	}
	c.open = false
	// A completed switch (even a no-op re-selection of the current
	// capability) lands on the workspace rather than reopening the spotlight
	// over it (see completeAction).
	c.m.completeAction()
	if name == c.current {
		return
	}
	c.current = name
	cfg, err := c.m.store.GetBoardsConfig(scope)
	if err != nil || cfg == nil {
		cfg = &core.BoardsConfig{}
	}
	cfg.Capability = name
	if err := c.m.store.SetProjectBoards(scope, cfg, c.m.actor); err != nil {
		c.m.showToast("save capability: " + err.Error())
	}
	c.m.lanes.refresh()
	c.m.lanes.selectDefault()
	c.m.momentum.refresh()
}

// openOverlay opens the [C] switcher with the cursor on the current
// capability's row (the last one the user selected), not the top.
func (c *capabilityModel) openOverlay() {
	c.refresh()
	c.open = true
	c.cursor = 0
	for i, e := range c.entries {
		if e.name == c.current {
			c.cursor = i
			break
		}
	}
}

// handleKey consumes every key while the overlay is open. Enter switches
// (enabling first when the row is disabled — one-stroke happy path); space
// toggles enable/disable without switching; Esc/C closes.
func (c *capabilityModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "C":
		c.open = false
	case "j", "down":
		if c.cursor < len(c.entries)-1 {
			c.cursor++
		}
	case "k", "up":
		if c.cursor > 0 {
			c.cursor--
		}
	case "T":
		c.m.cycleTheme()
	case "enter":
		if c.cursor < 0 || c.cursor >= len(c.entries) {
			return nil
		}
		e := c.entries[c.cursor]
		if !e.enabled {
			if err := c.m.store.EnableProjectCapability(c.m.projectScope, e.name, c.m.actor); err != nil {
				c.m.showToast("enable " + e.name + ": " + err.Error())
				return nil
			}
			c.m.refreshAll()
		}
		c.switchTo(e.name)
	case " ":
		if c.cursor < 0 || c.cursor >= len(c.entries) {
			return nil
		}
		e := c.entries[c.cursor]
		if e.enabled {
			if err := c.m.store.DisableProjectCapability(c.m.projectScope, e.name, c.m.actor); err != nil {
				c.m.showToast("disable " + e.name + ": " + err.Error())
				return nil
			}
			if c.current == e.name {
				c.current = ""
			}
		} else {
			if err := c.m.store.EnableProjectCapability(c.m.projectScope, e.name, c.m.actor); err != nil {
				c.m.showToast("enable " + e.name + ": " + err.Error())
				return nil
			}
		}
		cursor := c.cursor
		c.m.refreshAll()
		c.cursor = cursor
		if c.cursor >= len(c.entries) {
			c.cursor = len(c.entries) - 1
		}
	}
	return nil
}

// renderOverlay draws the centered switcher modal. Row shape:
//
//	▶ ● scrum        the build flow · 3 lanes
func (c *capabilityModel) renderOverlay() string {
	styles := c.m.styles
	bw := c.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > c.m.width-4 {
		bw = c.m.width - 4
	}

	var body strings.Builder
	if pb := c.previewBody(bw - 4); pb != "" {
		body.WriteString(pb + "\n")
	}
	body.WriteString("\n")
	body.WriteString(styles.KeyMenuDim.Render("[↑/↓]move  [Enter]switch  [space]enable/disable  [Esc]close"))

	bh := len(c.entries) + 5
	return titledBoxHeight(styles.DialogBody, bw, "Capabilities", body.String(), bh)
}

// previewBody renders the capability switcher rows, without box chrome or
// the footer hint. renderOverlay wraps it; the spotlight preview renders it
// directly, so a preview can never show something the overlay does not. w is
// unused: the existing rows were never clipped to the box width, and this
// extraction preserves that (unchanged) behavior rather than introducing a
// new truncation.
func (c *capabilityModel) previewBody(w int) string {
	nameW := 12
	for _, e := range c.entries {
		if len(e.name) > nameW {
			nameW = len(e.name)
		}
	}
	var body strings.Builder
	for i, e := range c.entries {
		marker := "  "
		if e.name == c.current {
			marker = "▶ "
		}
		state := "● "
		st := c.m.styles.Body
		if !e.enabled {
			state = "○ "
			st = c.m.styles.Muted
		}
		name := fmt.Sprintf("%-*s", nameW, e.name)
		line := marker + state + name + "  " + e.summary
		if i == c.cursor {
			line = c.m.styles.RowCursor.Render(line)
		} else {
			line = st.Render(line)
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	return strings.TrimRight(body.String(), "\n")
}
