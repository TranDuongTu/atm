package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStoreCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Store inspection commands",
		Long: "Event-log administration for the machine-global store under $ATM_HOME. The " +
			"subcommands are log (inspect the event stream), upgrade (import a v1 project log " +
			"into side-by-side EventSource v2 storage), prune-v1 (retire an upgraded project's " +
			"frozen v1 log.jsonl, archived by default), and set-format (choose the store default " +
			"format that governs project birth and the legacy default).",
	}
	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the resolved store path",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
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
