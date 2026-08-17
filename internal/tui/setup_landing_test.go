package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/developing"
	"atm/internal/setup"

	tea "github.com/charmbracelet/bubbletea"
)

// newTestModelEmptyStore builds a Model against a fresh temp-dir store with
// no projects, then explicitly applies the empty-store landing rule — the
// same call Run makes right after NewModel in a real launch (see run.go).
// The rule is NOT folded into NewModel itself: NewModel is also what every
// other internal/tui test uses to build a bare fixture for one pane, and
// none of those seed a project just to see their own pane render normally;
// auto-opening the wizard inside NewModel would hijack every one of them.
func newTestModelEmptyStore(t *testing.T) *Model {
	t.Helper()
	m := newTestModelWithActor(t, testActor)
	m.applyLandingRule()
	return m
}

// An empty store has nothing to show and nothing to press — the wizard IS
// the TUI there — once.
func TestEmptyStoreLandsOnSetup(t *testing.T) {
	m := newTestModelEmptyStore(t)
	if !m.setup.active {
		t.Fatal("a store with no projects lands on the setup view")
	}
}

func TestStoreWithProjectsDoesNotAutoOpen(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme") // has at least one project
	m.applyLandingRule()
	if m.setup.active {
		t.Fatal("the wizard never auto-opens once projects exist")
	}
}

func TestNudgeAppearsOnlyWhileSomethingIsUnready(t *testing.T) {
	m := newTestModel(t)
	m.setup.model = setup.Model{Agents: []setup.AgentRow{
		{Agent: "claude", Binary: setup.FactPresent, Plugin: setup.FactAbsent},
	}}
	if !strings.Contains(m.renderStatusLine(), "setup") {
		t.Fatal("an unready agent must raise the nudge")
	}
	m.setup.model = setup.Model{Agents: []setup.AgentRow{
		{Agent: "claude", Binary: setup.FactPresent, Plugin: setup.FactPresent},
	}}
	if strings.Contains(m.renderStatusLine(), "setup") {
		t.Fatal("nothing unready means no nudge at all")
	}
}

// The case the single-agent fixture above could never expose: ATM cannot
// install a harness, so a machine that deliberately runs only claude glyphs ○
// for codex and opencode forever. Nudging on that made the warning permanent
// and undismissable — so the rule is what the user can act on, not what is
// merely not ●.
func TestNudgeIgnoresHarnessesTheUserDoesNotUse(t *testing.T) {
	ready := setup.AgentRow{Agent: "claude", Binary: setup.FactPresent, Plugin: setup.FactPresent}
	notInstalled := func(name string) setup.AgentRow {
		return setup.AgentRow{Agent: name, Binary: setup.FactAbsent, Plugin: setup.FactAbsent}
	}
	withDefault := func(row setup.AgentRow) setup.AgentRow {
		row.IsDefault, row.DefaultVia = true, "native"
		return row
	}
	unready := setup.AgentRow{Agent: "claude", Binary: setup.FactPresent, Plugin: setup.FactAbsent}

	for _, tc := range []struct {
		name   string
		agents []setup.AgentRow
		want   bool
	}{
		{
			name:   "no selection, one harness ready and two not installed",
			agents: []setup.AgentRow{ready, notInstalled("codex"), notInstalled("opencode")},
			want:   false,
		},
		{
			name:   "no selection, nothing ready anywhere",
			agents: []setup.AgentRow{unready, notInstalled("codex"), notInstalled("opencode")},
			want:   true,
		},
		{
			name:   "the selected default is ready; the other two are not installed",
			agents: []setup.AgentRow{withDefault(ready), notInstalled("codex"), notInstalled("opencode")},
			want:   false,
		},
		{
			name:   "the selected default is unready even though another agent is ready",
			agents: []setup.AgentRow{withDefault(unready), {Agent: "codex", Binary: setup.FactPresent, Plugin: setup.FactPresent}},
			want:   true,
		},
		{
			name:   "no rows at all: nothing probed yet is not a warning",
			agents: nil,
			want:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			m.setup.model = setup.Model{Agents: tc.agents}
			got := strings.Contains(m.renderStatusLine(), "setup")
			if got != tc.want {
				t.Fatalf("nudge = %v, want %v", got, tc.want)
			}
		})
	}
}

// installFakeAgentsOnPath writes stub "claude"/"opencode"/"codex" executables
// into a fresh bin dir and puts it on PATH, so exec.LookPath finds all three
// deterministically regardless of what is actually installed on the machine
// running the test. codex's stub has to do more than just exist: InstallPlugin
// "codex" shells out to `codex plugin add ... --json`
// (developing.installCodexRegistration -> runCodexPluginAdd), so the stub
// fakes that command's real side effect — populating the plugin cache
// codexPluginCached checks for. This mirrors internal/developing's own
// installFakeCodex test helper (plugin_install_test.go) exactly, since it
// is unexported and this package cannot import it.
func installFakeAgentsOnPath(t *testing.T, home string) {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	for _, name := range []string{"claude", "opencode"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	codexScript := `#!/bin/sh
mkdir -p "$HOME/.codex/plugins/cache/atm-local/atm-developing/0.1.0/.codex-plugin"
printf '{"name":"atm-developing"}\n' > "$HOME/.codex/plugins/cache/atm-local/atm-developing/0.1.0/.codex-plugin/plugin.json"
`
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte(codexScript), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// newTestModelWithFullyReadyAgents builds a model whose store has a project
// (so the landing rule stays out of the way) and whose three real harnesses
// (agent.Harnesses(): claude, codex, opencode) are ALL genuinely ready —
// found on PATH and fully plugin-installed via developing.InstallPlugin, the
// same production path `atm agents plugin install` uses — under a fresh,
// isolated $HOME. It exists to prove the nudge stays silent from a real
// probe, not a fixture that skips the readiness computation.
func newTestModelWithFullyReadyAgents(t *testing.T) *Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	installFakeAgentsOnPath(t, home)
	for _, agent := range []string{"claude", "opencode", "codex"} {
		if _, err := developing.InstallPlugin(agent, home, false); err != nil {
			t.Fatalf("InstallPlugin %s: %v", agent, err)
		}
	}
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	return m
}

// TestNudgeFiresFromANormalSessionWithoutOpeningTheWizard closes the gap
// review round 1 found: TestNudgeAppearsOnlyWhileSomethingIsUnready above
// hand-assigns m.setup.model, which is precisely what production never
// does — refreshAll's tier-1 reload is what has to populate it (see
// refreshAll's comment in app.go). This builds both halves the way a real
// session does: a project exists, the wizard is never opened, and the nudge
// has to reflect a REAL probe (a fresh, plugin-less $HOME for "unready";
// developing.InstallPlugin under an isolated $HOME for "ready" — see
// newTestModelWithFullyReadyAgents), not a value the test wrote in by hand.
func TestNudgeFiresFromANormalSessionWithoutOpeningTheWizard(t *testing.T) {
	// Unready: a fresh, empty $HOME has no agent plugin installed for any
	// harness, so Glyph() cannot be ● for any row — regardless of whether
	// this machine's real PATH happens to have claude/codex/opencode on it.
	t.Setenv("HOME", t.TempDir())
	unready := newTestModel(t)
	seedProject(t, unready, "ATM", "Acme")
	if unready.setup.active {
		t.Fatal("precondition: the wizard must never have been opened")
	}
	if !strings.Contains(unready.renderStatusLine(), "setup") {
		t.Fatal("refreshAll must populate setup facts even though the wizard was never opened, so a real unready agent raises the nudge")
	}

	ready := newTestModelWithFullyReadyAgents(t)
	if ready.setup.active {
		t.Fatal("precondition: the wizard must never have been opened")
	}
	if strings.Contains(ready.renderStatusLine(), "setup") {
		t.Fatal("every agent genuinely ready, from a real probe, must mean no nudge — not just an unpopulated model reading as ready")
	}
}

// The wizard hands off; it never creates projects.
func TestFooterPointsAtProjectCreationOnceAnAgentIsReady(t *testing.T) {
	m := newTestModelEmptyStore(t)
	m.setup.model = setup.Model{Agents: []setup.AgentRow{
		{Agent: "claude", Binary: setup.FactPresent, Plugin: setup.FactPresent},
	}}
	if !strings.Contains(m.setup.render(100, 30), "first project") {
		t.Fatal("once an agent is ready the footer points at creating a project")
	}
}

// TestOpenSetupViaKeyReachesHandleKey exercises the real key-routing path
// end to end: dispatchKey's "W" case opens the wizard (rather than a test
// calling m.setup.open() directly), and a follow-up key reaches
// setupModel.handleKey through the same m.handleKey entry point a real
// keypress would use. Every other setup test drives setupModel.handleKey
// directly; this one proves the wiring between the two actually holds.
func TestOpenSetupViaKeyReachesHandleKey(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme") // NewModel itself never auto-opens (see newTestModelEmptyStore)
	m.projectScope = "ATM"           // gives Tab a second section to land on
	if m.setup.active {
		t.Fatal("precondition: wizard must start closed")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	if !m.setup.active {
		t.Fatal("the W key must open the setup wizard through the real dispatch path")
	}
	if m.setup.section != setupSectionAgents {
		t.Fatalf("opens on agents, got %v", m.setup.section)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.setup.section != setupSectionChannels {
		t.Fatalf("Tab must reach setupModel.handleKey through m.handleKey, not just m.setup.handleKey directly; section = %v", m.setup.section)
	}
}
