package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newInquiryCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inquiry",
		Short: "Inquiry-log management (R7 driving hook for future eval)",
	}
	cmd.AddCommand(newInquiryAddCmd(st))
	return cmd
}

func newInquiryAddCmd(st *cliState) *cobra.Command {
	var project, query, cited string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Append an inquiry (query + cited hit IDs) to inquiry-log.jsonl",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			citedIDs := []string{}
			if cited != "" {
				citedIDs = strings.Split(cited, ",")
			}
			if err := s.AppendInquiry(project, query, citedIDs); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project": project, "query": query, "cited_ids": citedIDs, "actor": actor,
			}, func() {
				fmt.Fprintf(st.stdout(), "appended inquiry to %s: %s\n", project, query)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&query, "query", "", "the inquiry question")
	cmd.Flags().StringVar(&cited, "cited", "", "comma-separated cited hit IDs")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("query")
	return cmd
}
