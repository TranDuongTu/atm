package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// activateEntryByKey finds the menu entry for key, drills into its owning
// group if it has one, and activates it — the same tree-walk walkTo does for
// a label (spotlight_test.go), just keyed on the replay key instead: a
// registry-row invariant test only has the key to go on, not the label.
func activateEntryByKey(t *testing.T, m *Model, key string) {
	t.Helper()
	for i := range menuEntries {
		e := &menuEntries[i]
		if e.hidden || e.key != key {
			continue
		}
		walkTo(t, m, e.label)
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		return
	}
	t.Fatalf("no menu entry with key %q", key)
}

func TestSetupRegistryRowSatisfiesInvariants(t *testing.T) {
	var found bool
	for _, e := range menuEntries {
		if e.key != "W" {
			continue
		}
		found = true
		if e.group != groupNone {
			t.Fatalf("the wizard is a root-level global, got group %v", e.group)
		}
		if lipgloss.Width(e.icon) != 1 {
			t.Fatalf("icon %q measures %d, must be 1", e.icon, lipgloss.Width(e.icon))
		}
		if e.needsProject {
			t.Fatal("the wizard is global; it must show with no project selected")
		}
	}
	if !found {
		t.Fatal("no W row in menuEntries")
	}
}

func TestActivateSetupLeavesItOpenAndSpotlightClosed(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	activateEntryByKey(t, m, "W")
	if !m.setup.active {
		t.Fatal("activation must leave the setup view open")
	}
	if m.spotlight.open {
		t.Fatal("the spotlight must not reopen over the setup view")
	}
}

func TestSetupRenderNarrowDropsColumnsRightToLeft(t *testing.T) {
	m := newTestModel(t)
	m.setup.open()
	wide := m.setup.render(120, 30)
	narrow := m.setup.render(46, 30)
	if !strings.Contains(wide, "MODEL") {
		t.Fatal("wide render should carry the MODEL column")
	}
	if strings.Contains(narrow, "MODEL") {
		t.Fatal("narrow render should have dropped MODEL")
	}
	if !strings.Contains(narrow, "claude") {
		t.Fatal("the agent name must never be dropped")
	}
}

func TestSetupRenderShowsEllipsisBeforeAsyncTierLands(t *testing.T) {
	m := newTestModel(t)
	m.setup.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("harness 4.5.6\n"), nil
	}
	cmd := m.setup.open() // async has not resolved
	pending := agentVerCell(t, m.setup.render(120, 30), "claude")
	if pending != "…" {
		t.Fatalf("claude's VER cell = %q before the probe landed, want …", pending)
	}

	m.Update(cmd())
	landed := agentVerCell(t, m.setup.render(120, 30), "claude")
	if landed == pending {
		t.Fatalf("VER cell is %q both before and after the probe landed; this assertion cannot fail", landed)
	}
	if landed != "4.5.6" {
		t.Fatalf("claude's VER cell = %q after the probe landed, want 4.5.6", landed)
	}
}

// agentVerCell pulls one agent row's VER cell out of a render. The header
// carries its own "probing…" ellipsis, so asserting on the frame as a whole
// says nothing about the cells — this reads the cell the async tier fills.
func agentVerCell(t *testing.T, frame, agent string) string {
	t.Helper()
	for _, line := range strings.Split(frame, "\n") {
		fields := strings.Fields(stripANSI(line))
		// A row is "<glyph> <agent> <ver> …"; the header line's first field is
		// "AGENT", never a glyph.
		for i, f := range fields {
			if f != agent || i == 0 || i+1 >= len(fields) {
				continue
			}
			return fields[i+1]
		}
	}
	t.Fatalf("no %s row in the frame:\n%s", agent, frame)
	return ""
}

// Enter has to open something. The MCP server list is the reason this level
// exists: apply() computes it and `atm setup status` prints it, but the
// AGENTS table has no column for it, so without the drill the wizard cannot
// answer "which servers does this harness have" at all.
func TestSetupDrillShowsTheMCPServersNoColumnCarries(t *testing.T) {
	m := newTestModel(t)
	m.setup.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			return []byte("harness 1.0.0\n"), nil
		}
		if name == "claude" {
			return []byte("notion: https://mcp.notion.com/mcp (HTTP) - ✓ Connected\n"), nil
		}
		return nil, errProbeUnavailable
	}
	cmd := m.setup.open()
	m.Update(cmd())
	focusAgent(t, m, "claude")

	if top := m.setup.render(120, 40); strings.Contains(top, "notion") {
		t.Fatal("the top level has no MCP column; it must not claim one")
	}
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	detail := stripANSI(m.setup.render(120, 40))
	if !strings.Contains(detail, "DETAIL") {
		t.Fatalf("enter must open a detail body, got:\n%s", detail)
	}
	if !strings.Contains(detail, "notion") {
		t.Fatalf("the drill must list the agent's MCP servers, got:\n%s", detail)
	}
	if !strings.Contains(detail, "connected") {
		t.Fatalf("the drill must report each server's connected state, got:\n%s", detail)
	}
	if !strings.Contains(detail, "1.0.0") {
		t.Fatalf("the drill must report the probed version, got:\n%s", detail)
	}
}

// codex's `mcp list --json` reports configuration but not health, so its
// servers land with FactUnknown. The drill must say so — reporting an unknown
// health as "not connected" is the manufactured negative the whole tri-state
// exists to prevent.
func TestSetupDrillReportsUnknownServerHealthAsUnknown(t *testing.T) {
	m := newTestModel(t)
	m.setup.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			return []byte("harness 1.0.0\n"), nil
		}
		if name == "codex" {
			return []byte(`[{"name":"notion"}]`), nil
		}
		return nil, errProbeUnavailable
	}
	cmd := m.setup.open()
	m.Update(cmd())
	focusAgent(t, m, "codex")
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	detail := stripANSI(m.setup.render(120, 40))
	if !strings.Contains(detail, "notion") {
		t.Fatalf("codex's configured server must be listed, got:\n%s", detail)
	}
	if !strings.Contains(detail, "health unknown") {
		t.Fatalf("an unknown health must read as unknown, got:\n%s", detail)
	}
	if strings.Contains(detail, "not connected") {
		t.Fatalf("an unknown health must never read as disconnected, got:\n%s", detail)
	}
}

// A probe that could not answer leaves the list unknown, and the drill must
// not present that as "this agent has no servers".
func TestSetupDrillSaysUnknownRatherThanEmptyWhenTheProbeFailed(t *testing.T) {
	m := newTestModel(t)
	m.setup.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			return []byte("harness 1.0.0\n"), nil
		}
		return nil, errProbeUnavailable
	}
	cmd := m.setup.open()
	m.Update(cmd())
	focusAgent(t, m, "claude")
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	detail := stripANSI(m.setup.render(120, 40))
	if !strings.Contains(detail, "unknown") {
		t.Fatalf("an unanswerable probe leaves the server list unknown, got:\n%s", detail)
	}
	if strings.Contains(detail, "no servers configured") {
		t.Fatalf("unknown is not empty, got:\n%s", detail)
	}
}

// The drill keeps the focused section's rows on screen, so the movement keys
// the footer advertises must still move. They silently stopped working before
// — which reads as the arrow keys having broken.
func TestSetupDrillKeepsCursorMovementWorking(t *testing.T) {
	m := newTestModel(t)
	m.setup.open()
	if len(m.setup.model.Agents) < 2 {
		t.Fatalf("need at least two harnesses to move between, got %d", len(m.setup.model.Agents))
	}
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.setup.cursor != 1 {
		t.Fatalf("cursor = %d after j inside the drill; movement must not go quiet", m.setup.cursor)
	}
	detail := stripANSI(m.setup.render(120, 40))
	if !strings.Contains(detail, m.setup.model.Agents[1].Agent) {
		t.Fatalf("the detail must follow the cursor, got:\n%s", detail)
	}
	if !strings.Contains(detail, "[Esc]back") {
		t.Fatalf("the drill's footer must name the way back out, got:\n%s", detail)
	}
}

// The CHANNELS drill is where a channel's per-agent coverage lives: the table
// row can only carry one glyph for the whole channel.
func TestSetupChannelDrillShowsPerAgentCoverage(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedWiredRepoChannel(t, m, "ATM", "atm-repo")
	m.projectScope = "ATM"
	m.setup.open()
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyTab}) // -> CHANNELS
	m.setup.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	detail := stripANSI(m.setup.render(120, 40))
	if !strings.Contains(detail, "coverage") {
		t.Fatalf("the channel drill must show the per-agent coverage map, got:\n%s", detail)
	}
	for _, row := range m.setup.model.Agents {
		if !strings.Contains(detail, row.Agent) {
			t.Fatalf("coverage must name every agent; %s is missing from:\n%s", row.Agent, detail)
		}
	}
	if !strings.Contains(detail, "covered") {
		t.Fatalf("a wired repo channel is covered for every agent, got:\n%s", detail)
	}
}
