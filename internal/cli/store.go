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

	logCmd := &cobra.Command{
		Use:   "log <CODE>",
		Short: "Stream the project's audit log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			from, _ := cmd.Flags().GetInt("from")
			to, _ := cmd.Flags().GetInt("to")
			// A project's truth is events.v2.jsonl; --from/--to filter the
			// DISPLAY ordinal.
			events, err := s.ReadV2LogForDisplay(args[0])
			if err != nil {
				return err
			}
			filtered := make([]store.V2LogView, 0, len(events))
			for _, e := range events {
				if from != 0 && e.Ordinal < from {
					continue
				}
				if to != 0 && e.Ordinal > to {
					continue
				}
				filtered = append(filtered, e)
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), filtered)
			}
			for _, e := range filtered {
				fmt.Fprintf(st.stdout(), "%d\t%s\t%s\t%s\t%s\t%s\n", e.Ordinal, store.RFC3339UTC(e.At), e.Actor, e.Action, e.Subject, e.ID)
			}
			return nil
		},
	}
	logCmd.Flags().Int("from", 0, "start seq (inclusive, 0 = start)")
	logCmd.Flags().Int("to", 0, "end seq (inclusive, 0 = end)")
	cmd.AddCommand(logCmd)

	verifyCmd := &cobra.Command{
		Use:   "verify [CODE]",
		Short: "Replay logs against caches and report divergence",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			repair, _ := cmd.Flags().GetBool("repair")
			if len(args) == 1 {
				r, err := s.VerifyProject(args[0])
				if err != nil {
					return err
				}
				if repair && r.Diverged {
					_, _ = s.Rebuild()
					r2, _ := s.VerifyProject(args[0])
					r = r2
				}
				if err := st.emitVerify(r); err != nil {
					return err
				}
				return reportIntegrityError([]store.VerifyReport{*r})
			}
			reports, err := s.Verify()
			if err != nil {
				return err
			}
			if repair {
				for _, r := range reports {
					if r.Diverged {
						_, _ = s.Rebuild()
						break
					}
				}
				reports, _ = s.Verify()
			}
			if err := st.emitVerifyAll(reports); err != nil {
				return err
			}
			return reportIntegrityError(reports)
		},
	}
	verifyCmd.Flags().Bool("repair", false, "regenerate caches from logs on divergence")
	cmd.AddCommand(verifyCmd)

	rebuildCmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Regenerate all cache files from the logs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			rep, err := s.Rebuild()
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), rep)
			}
			fmt.Fprintf(st.stdout(), "rebuilt: projects=%d tasks=%d labels=%d\n", rep.Projects, rep.Tasks, rep.Labels)
			return nil
		},
	}
	cmd.AddCommand(rebuildCmd)

	upgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade v1 project logs to side-by-side EventSource v2 storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			all, _ := cmd.Flags().GetBool("all")
			if all == (project != "") {
				return fmt.Errorf("%w: pass exactly one of --project or --all", store.ErrUsage)
			}
			if all {
				reps, err := s.UpgradeAllToV2()
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
			rep, err := s.UpgradeProjectToV2(project)
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
	cmd.AddCommand(upgradeCmd)

	pruneCmd := &cobra.Command{
		Use:   "prune-v1",
		Short: "Retire upgraded projects' frozen v1 log.jsonl (archive by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			all, _ := cmd.Flags().GetBool("all")
			del, _ := cmd.Flags().GetBool("delete")
			if all == (project != "") {
				return fmt.Errorf("%w: pass exactly one of --project or --all", store.ErrUsage)
			}
			codes := []string{project}
			if all {
				codes, err = s.ProjectCodes()
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
			reps := make([]*store.PruneReport, 0, len(codes))
			var loopErr error
			for _, c := range codes {
				rep, err := s.PruneProjectV1(c, del)
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
	cmd.AddCommand(pruneCmd)

	setFormatCmd := &cobra.Command{
		Use:   "set-format",
		Short: "Set the store default format (governs project birth and the legacy default only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			format, _ := cmd.Flags().GetString("format")
			if err := s.SetActiveFormat(store.StoreFormat(format)); err != nil {
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
	cmd.AddCommand(setFormatCmd)

	return cmd
}

func (st *cliState) emitVerify(r *store.VerifyReport) error {
	if st.isJSON() {
		return writeJSON(st.stdout(), r)
	}
	fmt.Fprintf(st.stdout(), "project: %s\nformat: %s\nlog_entries: %d\nlog_ok: %t\ntruncated: %d\ndiverged: %t\n", r.Project, r.Format, r.LogEntries, r.LogOK, r.Truncated, r.Diverged)
	for _, c := range r.Caches {
		fmt.Fprintf(st.stdout(), "  %s\t%s:%s\tcache=%d last=%d\n", c.Status, c.Kind, c.ID, c.CacheLogSeq, c.LastEventSeq)
	}
	return nil
}

func (st *cliState) emitVerifyAll(rs []store.VerifyReport) error {
	if st.isJSON() {
		return writeJSON(st.stdout(), rs)
	}
	for _, r := range rs {
		if err := st.emitVerify(&r); err != nil {
			return err
		}
	}
	return nil
}

func reportIntegrityError(reports []store.VerifyReport) error {
	for _, r := range reports {
		if r.Diverged || !r.LogOK {
			return fmt.Errorf("%w: project %s has integrity issues (diverged=%v, log_ok=%v)", store.ErrIntegrity, r.Project, r.Diverged, r.LogOK)
		}
	}
	return nil
}
