package tui

import (
	"atm/internal/core"

	"github.com/charmbracelet/bubbletea"
)

// Run launches the Bubble Tea TUI over an already-opened store, with the
// given free-form actor id. The composition root (cmd/atm) resolves the
// store path and opens the concrete store; Run auto-inits it if absent,
// builds the root Model, and runs the program until the user quits.
func Run(svc core.Service, actor string) error {
	m, err := NewModel(NewModelOpts{Service: svc, Actor: actor})
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
