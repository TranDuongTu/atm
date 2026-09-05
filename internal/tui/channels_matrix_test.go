package tui

import (
	"strings"
	"testing"

	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
)

// seedMatrixProject builds a project covering every state the matrix shows:
// an attested-by-one-agent repo, a partly-unwired multi-endpoint channel, and
// a profile-expected channel with NO endpoint at all.
func seedMatrixProject(t *testing.T, m *Model) string {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	dir := t.TempDir()

	if _, err := m.store.CreateChannel("ATM", core.ChannelRecord{
		Name: "design", Type: core.ChannelTypeNotion, Purpose: "specs and plans",
		Endpoints: []core.ChannelEndpoint{
			{Type: core.ChannelTypeNotion, Role: "home", Address: core.ChannelAddress{Workspace: "acme", Database: "db-1"}},
			{Type: core.ChannelTypeSlack, Role: "broadcast", Address: core.ChannelAddress{ChannelID: "C123"}},
		},
		Origin: "scrumban@1.0.0",
	}, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateChannel("ATM", core.ChannelRecord{
		Name: "code", Type: core.ChannelTypeRepo, Purpose: "product source",
		Endpoints: []core.ChannelEndpoint{{Type: core.ChannelTypeRepo, Role: "home", Address: core.ChannelAddress{URL: "git@example.com:acme/code.git"}}},
		Origin:    "scrumban@1.0.0",
	}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetChannelWiring("ATM", "code", core.ChannelTypeRepo, dir, "", testActor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetChannelWiring("ATM", "design", core.ChannelTypeNotion, "", "notion", testActor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.AddChannelStamp("ATM", "code", core.ChannelTypeRepo, core.StampKindProbe, "listed the remote", "manager@claude:opus-5"); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetAgentModel("claude", "opus-5", testActor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetAgentModel("codex", "gpt", testActor); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	return dir
}

// TestChannelsMatrixRendersPerAgentColumns: the list is the endpoint × agent
// matrix. Each channel says what every CONFIGURED agent's worst endpoint
// attestation is, because "is this reachable" has a different answer per
// harness.
func TestChannelsMatrixRendersPerAgentColumns(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	view := m.channelsOv.renderOverlay()
	if !strings.Contains(view, "claude: ok") {
		t.Errorf("the attesting agent's column must read ok:\n%s", view)
	}
	if !strings.Contains(view, "codex: never") {
		t.Errorf("an agent with no stamp must read never:\n%s", view)
	}
}

// An endpoint below wiring is reported as the BLOCKER, not as an agent
// failure: no agent can attest what this machine cannot reach, so grading
// the agent there would blame it for a wiring gap the human has to fix.
func TestChannelsMatrixReportsWiringGapsInsteadOfBlamingAgents(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	view := m.channelsOv.renderOverlay()
	if !strings.Contains(view, "1 of 2 endpoints unwired") {
		t.Fatalf("the partly-wired channel must name the gap:\n%s", view)
	}
	// design's row is the wiring gap, so it must not also claim an agent
	// never attested it.
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "design") && strings.Contains(line, "never") {
			t.Errorf("a wiring gap must not be reported as an agent failure:\n%s", line)
		}
	}
}

// TestChannelsMatrixGlyphAgreesWithItsRow: core.ChannelStatus answers the
// pre-endpoint question, so on a multi-endpoint channel it can report healthy
// beside a row that says an endpoint is unwired. The glyph is derived from
// the rollup so the two cannot disagree.
func TestChannelsMatrixGlyphAgreesWithItsRow(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	for _, line := range strings.Split(m.channelsOv.renderOverlay(), "\n") {
		if !strings.Contains(line, "design") {
			continue
		}
		if !strings.Contains(line, "○") {
			t.Fatalf("a channel with an unwired endpoint must not carry a healthy glyph:\n%s", line)
		}
		return
	}
	t.Fatal("design row not rendered")
}

// TestChannelsMatrixSummaryAggregates: a reader who is not going to walk
// every row still learns whether anything is wrong.
func TestChannelsMatrixSummaryAggregates(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	view := m.channelsOv.renderOverlay()
	for _, want := range []string{"unwired here", "codex never attested"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary missing %q:\n%s", want, view)
		}
	}
}

// TestChannelsMatrixFlagsAChannelWithNoEndpoint: a profile-applied channel
// that nobody has addressed yet has nowhere for anything to land. That is a
// different problem from "nobody verified it lately", and the row names the
// profile that expected it.
func TestChannelsMatrixFlagsAChannelWithNoEndpoint(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	if _, err := m.store.CreateChannel("ATM", core.ChannelRecord{
		Name: "standup", Purpose: "the daily heartbeat", Origin: "scrumban@1.0.0",
	}, testActor); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	view := m.channelsOv.renderOverlay()
	if !strings.Contains(view, "no endpoint") {
		t.Fatalf("a channel with nowhere to reach must say so:\n%s", view)
	}
	if !strings.Contains(view, "expected by scrumban@1.0.0") {
		t.Fatalf("the row must name the profile that expected it:\n%s", view)
	}
}

// TestChannelsDetailGroupsStampsByAgent: attestation is per (endpoint ×
// machine × agent), so a flat stamp list cannot answer "has codex ever
// reached this" — the question the matrix column asks.
func TestChannelsDetailGroupsStampsByAgent(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // code sorts first
	detail := m.channelsOv.renderOverlay()
	for _, want := range []string{"endpoint", "claude", "listed the remote", core.StampKindProbe, "codex", "never attested"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail missing %q:\n%s", want, detail)
		}
	}
}

// TestChannelsDetailKeepsStampsFromUnconfiguredAgents: the matrix COLUMNS are
// the configured agents, but a stamp is evidence someone reached the
// endpoint. Dropping an agent from agents.json must not erase that.
func TestChannelsDetailKeepsStampsFromUnconfiguredAgents(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	if err := m.store.AddChannelStamp("ATM", "code", core.ChannelTypeRepo, core.StampKindUse, "pushed a branch", "developer@opencode:glm"); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	detail := m.channelsOv.renderOverlay()
	if !strings.Contains(detail, "opencode") || !strings.Contains(detail, "pushed a branch") {
		t.Fatalf("a stamp from an unconfigured agent must still be shown:\n%s", detail)
	}
}

// TestChannelsDetailListsEveryEndpointIncludingUnwiredOnes: the v1 detail
// skipped endpoints with no wiring and no stamp, hiding exactly the gap it
// was supposed to report.
func TestChannelsDetailListsEveryEndpointIncludingUnwiredOnes(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // design
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	detail := m.channelsOv.renderOverlay()
	if !strings.Contains(detail, "slack") {
		t.Fatalf("the unwired endpoint must be listed:\n%s", detail)
	}
	if !strings.Contains(detail, "atm channel wire") {
		t.Fatalf("an unwired endpoint must name the command that fixes it:\n%s", detail)
	}
}

// TestChannelsAttestKeyOpensAPrefilledDispatch: [v] is §3.11's fix-it key.
// The overlay stays READ-ONLY — the fix goes through the normal dispatch
// dialog, where the agent cycler picks which harness to attest.
func TestChannelsAttestKeyOpensAPrefilledDispatch(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	seedAttestAction(t, m)
	m.agentOptionsFn = testAgents
	m.dispatcher = &fakeDispatcher{preview: "window"}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if m.channelsOv.open {
		t.Fatal("v must close the overlay")
	}
	if !m.dispatchDlg.active {
		t.Fatal("v must open the dispatch dialog")
	}
	if got := m.dispatchDlg.action(); got == nil || got.Name != "attest" {
		t.Fatalf("dialog action = %v, want the attest action prefilled", got)
	}
}
