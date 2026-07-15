package cli

import (
	"fmt"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func renderSubject(su store.Subject) string {
	switch su.Kind {
	case "project":
		return su.Code
	case "task":
		return su.ID
	case "label":
		return su.Name
	}
	return su.Kind
}

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
			s, err := store.Open(root)
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
			// A v2-active project's truth is events.v2.jsonl; its log.jsonl is
			// either frozen at the cutover point (upgraded) or absent entirely
			// (born v2), so the v1 renderer would show a stale — or empty — log.
			// --from/--to filter the DISPLAY ordinal, the v2 counterpart of the
			// v1 seq.
			if f, ferr := s.ProjectFormatForCLI(args[0]); ferr == nil && f == store.StoreFormatV2 {
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
			}
			entries, err := s.ReadLog(args[0])
			if err != nil && !store.IsIntegrity(err) {
				return err
			}
			filtered := make([]store.LogEntry, 0, len(entries))
			for _, e := range entries {
				if from != 0 && e.Seq < from {
					continue
				}
				if to != 0 && e.Seq > to {
					continue
				}
				filtered = append(filtered, e)
			}
			entries = filtered
			if st.isJSON() {
				if err != nil {
					return err
				}
				return writeJSON(st.stdout(), entries)
			}
			for _, e := range entries {
				fmt.Fprintf(st.stdout(), "%d\t%s\t%s\t%s\t%s\n", e.Seq, store.RFC3339UTC(e.At), e.Actor, e.Action, renderSubject(e.Subject))
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
