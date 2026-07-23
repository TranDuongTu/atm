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
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
