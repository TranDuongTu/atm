package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"atm/internal/capability"
	"atm/internal/core"
)

// unmanagedCapability is the pseudo-capability of the capability view: the
// labels no enabled capability owns, browsed via the label drill-down. It is
// always selectable and never appears in Project.Capabilities.
const unmanagedCapability = "unmanaged"

// capEntry is one row of the [C] switcher overlay.
type capEntry struct {
	name      string
	summary   string
	enabled   bool
	unmanaged bool
	count     string // "6 boards" / "5 labels · 12 tasks"
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

func (c *capabilityModel) unmanagedCurrent() bool { return c.current == unmanagedCapability }

// refresh rebuilds the switcher entries and re-resolves current. It MUST run
// before boardsModel.refresh in refreshAll — the ring is scoped to current.
func (c *capabilityModel) refresh() {
	c.entries = nil
	scope := c.m.projectScope
	if scope == "" {
		c.current = ""
		return
	}
	enabled := map[string]bool{}
	names := c.m.regFor(scope).Names()
	for _, n := range names {
		enabled[n] = true
	}
	boardsPer := map[string]int{}
	for _, e := range c.m.reg.Exposed(scope) {
		boardsPer[e.Owner]++
	}
	for _, d := range c.m.reg.Describe() {
		n := boardsPer[d.Name]
		c.entries = append(c.entries, capEntry{
			name:    d.Name,
			summary: d.Summary,
			enabled: enabled[d.Name],
			count:   fmt.Sprintf("%d %s", n, pluralBoards(n)),
		})
	}
	un, _ := c.m.regFor(scope).Unmanaged(c.m.store, scope)
	c.entries = append(c.entries, capEntry{
		name:      unmanagedCapability,
		summary:   "labels no enabled capability owns",
		unmanaged: true,
		count: fmt.Sprintf("%d %s · %d %s",
			len(un), pluralLabels(len(un)),
			c.m.countTasksCarrying(scope, capability.NewLabelSet(un)), pluralTasks(c.m.countTasksCarrying(scope, capability.NewLabelSet(un)))),
	})
	c.current = c.resolveCurrent(names)
	if c.cursor >= len(c.entries) {
		c.cursor = len(c.entries) - 1
	}
	if c.cursor < 0 {
		c.cursor = 0
	}
}

func pluralBoards(n int) string {
	if n == 1 {
		return "board"
	}
	return "boards"
}

func pluralLabels(n int) string {
	if n == 1 {
		return "label"
	}
	return "labels"
}

// resolveCurrent applies the resolution rule: the in-session current if still
// valid, else the persisted boards.capability if valid, else the first
// enabled capability, else unmanaged. Never writes back — only switchTo
// persists.
func (c *capabilityModel) resolveCurrent(enabledNames []string) string {
	valid := func(v string) bool {
		if v == unmanagedCapability {
			return true
		}
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
	return unmanagedCapability
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
	c.m.boards.resetDrill()
	c.m.boards.selected = ""
	c.m.boards.refresh()
	c.m.boards.selectDefault()
	// loadPins already runs at the end of boards.refresh (both managed and
	// unmanaged branches), so an explicit call here is redundant.
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
		if !e.enabled && !e.unmanaged {
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
		if e.unmanaged {
			return nil
		}
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
//	▶ ● workflow     status verbs and boards · 6 boards
func (c *capabilityModel) renderOverlay() string {
	styles := c.m.styles
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
		st := styles.Body
		switch {
		case e.unmanaged:
			state = "— "
		case !e.enabled:
			state = "○ "
			st = styles.Muted
		}
		name := fmt.Sprintf("%-*s", nameW, e.name)
		detail := e.summary
		if e.count != "" {
			detail += "  ·  " + e.count
		}
		line := marker + state + name + "  " + detail
		if i == c.cursor {
			line = styles.RowCursor.Render(line)
		} else {
			line = st.Render(line)
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	body.WriteString("\n")
	body.WriteString(styles.KeyMenuDim.Render("[↑/↓]move  [Enter]switch  [space]enable/disable  [Esc]close"))

	bw := c.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > c.m.width-4 {
		bw = c.m.width - 4
	}
	bh := len(c.entries) + 5
	return titledBoxHeight(styles.DialogBody, bw, "Capabilities", body.String(), bh)
}
