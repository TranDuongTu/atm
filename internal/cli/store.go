package cli

import (
	"fmt"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newStoreCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Store inspection commands",
	}
	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the resolved store path",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := store.ResolveStorePath(st.flags.store)
			s, err := store.Open(root, st.storeOpts...)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"store": s.StorePath()})
			}
			fmt.Fprintln(st.stdout(), s.StorePath())
			return nil
		},
	}
	cmd.AddCommand(pathCmd)

	for _, sub := range newStoreIntegrityCmds(st) {
		cmd.AddCommand(sub)
	}
	for _, sub := range newStoreMigrateCmds(st) {
		cmd.AddCommand(sub)
	}
	for _, sub := range newStoreSyncCmds(st) {
		cmd.AddCommand(sub)
	}

	return cmd
}
