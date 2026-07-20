package tui

import (
	"fmt"

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
	c.m.boards.loadPins()
}
