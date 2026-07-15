package cli

import (
	"fmt"

	"atm/internal/activity"

	"github.com/spf13/cobra"
)

func newActivityCmd(st *cliState) *cobra.Command {
	var project, groupBy string
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Aggregate actor activity for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch groupBy {
			case "persona", "agent", "model":
			default:
				return fmt.Errorf("%w: --group-by must be persona|agent|model", ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return err
			}
			// ReadLogCached dispatches on the project's effective format and
			// serves the v2 event file as compatibility entries; activity.Build
			// is unchanged. It already applies v1's lenient posture to a
			// malformed v1 tail internally, so any error reaching here is worth
			// surfacing: a corrupt v2 event file yields only its recoverable
			// prefix, and reporting that truncated view as if complete would be
			// worse than failing.
			entries, err := s.ReadLogCached(project)
			if err != nil {
				return err
			}
			groups := activity.Aggregate(activity.Build(entries), groupBy)
			return st.emit(st.stdout(), map[string]any{"groups": groups}, func() {
				for _, g := range groups {
					fmt.Fprintf(st.stdout(), "%s\t%d\n", g.Key, g.Count)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&groupBy, "group-by", "persona", "persona|agent|model")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
