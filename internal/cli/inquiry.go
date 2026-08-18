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
	var project, query, cited, returned string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Append an inquiry (query + cited hit IDs) to inquiry-log.jsonl",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			returnedIDs := []string{}
			if returned != "" {
				returnedIDs = strings.Split(returned, ",")
			}
			citedIDs := []string{}
			if cited != "" {
				citedIDs = strings.Split(cited, ",")
			}
			// This CLI surface has no click-through -- that's the spotlight (ATM-f71b81).
			if err := s.AppendInquiry(project, query, returnedIDs, citedIDs, nil); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project": project, "query": query, "returned_ids": returnedIDs, "cited_ids": citedIDs,
			}, func() {
				fmt.Fprintf(st.stdout(), "appended inquiry to %s: %s\n", project, query)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&query, "query", "", "the inquiry question")
	cmd.Flags().StringVar(&cited, "cited", "", "comma-separated cited hit IDs")
	cmd.Flags().StringVar(&returned, "returned", "", "comma-separated IDs the search returned (the recall@k denominator)")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("query")
	return cmd
}
