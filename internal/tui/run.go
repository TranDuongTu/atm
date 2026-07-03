package tui

import (
	"github.com/charmbracelet/bubbletea"
)

// Run launches the Bubble Tea TUI against the store at storePath, with the
// given free-form actor id. It auto-inits the store if absent, builds the
// root Model, and runs the program until the user quits.
func Run(storePath, actor string) error {
	m, err := NewModel(NewModelOpts{StorePath: storePath, Actor: actor})
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
