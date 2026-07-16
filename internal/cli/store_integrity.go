package cli

import (
	"fmt"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

// newStoreIntegrityCmds returns the store subcommands that inspect the log and
// reconcile it against the cache: log, verify, rebuild.
func newStoreIntegrityCmds(st *cliState) []*cobra.Command {
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
	return []*cobra.Command{logCmd, verifyCmd, rebuildCmd}
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
