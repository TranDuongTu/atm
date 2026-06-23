package cli

import (
	"fmt"

	"atm/internal/store"
	"atm/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICmd(st *cliState) *cobra.Command {
	var actor string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the Bubble Tea TUI (thin client over the store)",
		RunE: func(cmd *cobra.Command, args []string) error {
			a := actor
			if a == "" {
				a = st.flags.actor
			}
			root := store.ResolveStorePath(st.flags.store)
			if err := tui.Run(root, a); err != nil {
				return fmt.Errorf("tui: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "", "actor id (overrides --actor/ATM_ACTOR for the session)")
	return cmd
}
