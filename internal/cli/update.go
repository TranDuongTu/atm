package cli

import (
	"context"
	"fmt"

	"atm/internal/update"

	"github.com/spf13/cobra"
)

type updateRunner func(context.Context, update.Options) (update.Result, error)

func newUpdateCmd(st *cliState) *cobra.Command {
	var versionPin string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the atm binary in place",
		RunE: func(cmd *cobra.Command, args []string) error {
			run := st.runUpdate
			if run == nil {
				run = update.Run
			}
			res, err := run(cmd.Context(), update.Options{Version: versionPin})
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]update.Result{"update": res})
			}
			if !res.Updated {
				fmt.Fprintf(st.stdout(), "atm is already up to date: %s\n", res.NewVersion)
				return nil
			}
			fmt.Fprintf(st.stdout(), "updated atm %s -> %s\n", res.OldVersion, res.NewVersion)
			return nil
		},
	}
	cmd.Flags().StringVar(&versionPin, "version", "", "release tag to install (default latest)")
	return cmd
}
