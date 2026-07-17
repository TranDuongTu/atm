package cli

import (
	"fmt"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// newStoreMigrateCmds returns the store subcommands that move a project
// between storage formats: upgrade, prune-v1, set-format.
func newStoreMigrateCmds(st *cliState) []*cobra.Command {
	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade v1 project logs to side-by-side EventSource v2 storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openAdmin()
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			all, _ := cmd.Flags().GetBool("all")
			if all == (project != "") {
				return fmt.Errorf("%w: pass exactly one of --project or --all", core.ErrUsage)
			}
			if all {
				reps, err := s.UpgradeAllStorage()
				if err != nil {
					return err
				}
				if st.isJSON() {
					return writeJSON(st.stdout(), reps)
				}
				for _, r := range reps {
					if r.AlreadyV2 {
						// Retry after a partial failure: UpgradeAllToV2
						// skipped this project because it is already
						// v2-active (gating rule; see Task 4).
						fmt.Fprintf(st.stdout(), "skipped\t%s\t%s\talready v2-active\n", r.Project, r.Format)
						continue
					}
					fmt.Fprintf(st.stdout(), "upgraded\t%s\t%s\tevents=%d\n", r.Project, r.Format, r.Events)
				}
				// UpgradeAllToV2 flipped the store default: new projects
				// are born v2 from here on. Surface that.
				fmt.Fprintln(st.stdout(), "active format: v2")
				return nil
			}
			rep, err := s.UpgradeStorage(project)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), rep)
			}
			fmt.Fprintf(st.stdout(), "upgraded\t%s\t%s\tevents=%d\n", rep.Project, rep.Format, rep.Events)
			return nil
		},
	}
	upgradeCmd.Flags().String("project", "", "project code to upgrade")
	upgradeCmd.Flags().Bool("all", false, "upgrade all projects")

	pruneCmd := &cobra.Command{
		Use:   "prune-v1",
		Short: "Retire upgraded projects' frozen v1 log.jsonl (archive by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openAdmin()
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			all, _ := cmd.Flags().GetBool("all")
			del, _ := cmd.Flags().GetBool("delete")
			if all == (project != "") {
				return fmt.Errorf("%w: pass exactly one of --project or --all", core.ErrUsage)
			}
			codes := []string{project}
			if all {
				svc, err := st.openStore()
				if err != nil {
					return err
				}
				codes, err = svc.ProjectCodes()
				if err != nil {
					return err
				}
			}
			// A per-project error stops the loop but must not discard the
			// reports already collected: PruneProjectV1's archive/delete move
			// for those projects already happened on disk, so hiding them
			// behind a mid-batch failure would make a successful `--all` prune
			// look like it did nothing. Print what was collected so far, then
			// return the error.
			reps := make([]*core.PruneReport, 0, len(codes))
			var loopErr error
			for _, c := range codes {
				rep, err := s.PruneLegacy(c, del)
				if err != nil {
					loopErr = fmt.Errorf("project %q: %w", c, err)
					break
				}
				reps = append(reps, rep)
			}
			if st.isJSON() {
				if err := writeJSON(st.stdout(), reps); err != nil {
					return err
				}
				return loopErr
			}
			for _, r := range reps {
				switch {
				case r.Deleted:
					fmt.Fprintf(st.stdout(), "pruned\t%s\tdeleted\n", r.Project)
				case r.Pruned:
					fmt.Fprintf(st.stdout(), "pruned\t%s\tarchived %s\n", r.Project, r.Archived)
				default:
					fmt.Fprintf(st.stdout(), "skipped\t%s\t%s\n", r.Project, r.Reason)
				}
			}
			return loopErr
		},
	}
	pruneCmd.Flags().String("project", "", "project code to prune")
	pruneCmd.Flags().Bool("all", false, "prune all eligible projects")
	pruneCmd.Flags().Bool("delete", false, "delete log.jsonl instead of archiving it")

	setFormatCmd := &cobra.Command{
		Use:   "set-format",
		Short: "Set the store default format (governs project birth and the legacy default only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openAdmin()
			if err != nil {
				return err
			}
			format, _ := cmd.Flags().GetString("format")
			if err := s.SetStorageFormat(format); err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"active_format": format})
			}
			fmt.Fprintf(st.stdout(), "active format: %s\n", format)
			return nil
		},
	}
	setFormatCmd.Flags().String("format", "", "v1 or v2; v2 is refused while any project lacks an explicit format entry")
	return []*cobra.Command{upgradeCmd, pruneCmd, setFormatCmd}
}
