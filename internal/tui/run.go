package tui

import (
	"atm/internal/capability"
	"atm/internal/core"

	"github.com/charmbracelet/bubbletea"
)

// Run launches the Bubble Tea TUI over an already-opened store, with the
// given free-form actor id. The composition root (cmd/atm) resolves the
// store path and opens the concrete store; Run auto-inits it if absent,
// builds the root Model, and runs the program until the user quits. d is
// the dispatch port (the *dispatch.Service facade); nil disables dispatch
// with a clear error in the dialog.
func Run(svc core.Service, actor string, reg *capability.Registry, d Dispatcher) error {
	m, err := NewModel(NewModelOpts{Service: svc, Actor: actor, Registry: reg, Dispatcher: d})
	if err != nil {
		return err
	}
	// The empty-store landing rule: a store with no projects has nothing to
	// show and nothing to press, so a real launch lands on the setup wizard
	// instead of an empty two-pane workspace. Applied here rather than
	// inside NewModel because NewModel also builds every internal/tui unit
	// test's fixture, most of which never seed a project and must not have
	// the wizard sprung on them. m.initCmd carries the wizard's tier-2 probe
	// (a nil Cmd if a project already exists) to Init(), which the program
	// below runs once its event loop is up.
	m.initCmd = m.applyLandingRule()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
