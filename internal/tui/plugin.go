package tui

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// plugin is the small, real plugin registry interface that the indexer (and
// future right-side status-bar plugins) implement. It is intentionally minimal
// so "the indexer is the first plugin" is a true statement and the next
// plugin is one struct away.
type plugin interface {
	ID() string
	Icon() string
	OverlayKey() string
	DockLabel(state any) string
	DockColor(state any, s Styles) lipgloss.Style
	State(m *Model) any
	Open(m *Model)
	Close(m *Model)
	Reset(m *Model)
	HandleKey(k tea.KeyMsg, m *Model) tea.Cmd
	Render(m *Model) string
}

// pluginSupervisor wraps a plugin's Reset calls with a 3-strikes debounce
// window: when 3 errors land within the configured window (30s by default) it
// signals that the plugin should be reset, and clears the per-plugin error
// window so a fresh error starts a new window. The store-side exponential
// backoff (per-delta) stays; the supervisor is for "this plugin is clearly
// broken, give the user a clean slate."
type pluginSupervisor struct {
	mu     sync.Mutex
	window time.Duration
	errors map[string][]time.Time
}

// newPluginSupervisor returns a supervisor with a 30s sliding error window.
func newPluginSupervisor() *pluginSupervisor {
	return &pluginSupervisor{window: 30 * time.Second, errors: map[string][]time.Time{}}
}

// recordError records an error for the given plugin and reports whether the
// 3-strikes threshold has been reached within the sliding window. When it
// returns true, the per-plugin error window is cleared so a fresh error
// starts a new window; the caller is responsible for invoking plugin.Reset.
func (sv *pluginSupervisor) recordError(p plugin) (shouldReset bool) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	id := p.ID()
	now := time.Now()
	cutoff := now.Add(-sv.window)
	var recent []time.Time
	for _, t := range sv.errors[id] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	sv.errors[id] = recent
	if len(recent) >= 3 {
		sv.errors[id] = nil
		return true
	}
	return false
}

// clear drops the per-plugin error window so the next error starts a fresh
// 3-strike count.
func (sv *pluginSupervisor) clear(p plugin) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	delete(sv.errors, p.ID())
}

// dockSegments returns one rendered dock string per registered plugin, in
// registration order. It is the right-aligned join target for the status line.
//
// Per D14 the dock is always visible — even with no project selected — so the
// user discovers the plugin keybinds from the first launch. In that case the
// indexer state is `off` and the segment reads `⌬ off  g1`.
//
// Per D12 each segment is `<state-colored label>  <muted g<OverlayKey> hint>`
// so the keybind to open the plugin's overlay is readable right next to the
// icon. When no plugins are registered it returns nil.
func dockSegments(m *Model) []string {
	if len(m.plugins) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.plugins))
	for _, p := range m.plugins {
		st := p.State(m)
		label := p.DockLabel(st)
		style := p.DockColor(st, m.styles)
		hint := m.styles.KeyMenuDim.Render("g" + p.OverlayKey())
		out = append(out, style.Render(label)+"  "+hint)
	}
	return out
}
