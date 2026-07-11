package cli

import (
	"encoding/json"
	"fmt"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newVocabularyCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "vocabulary", Short: "Project vocabulary (ubiquitous language) commands"}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newVocabularyShowCmd(st))
	cmd.AddCommand(newVocabularyWriteCmd(st))
	return cmd
}

func newVocabularyShowCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a project's vocabulary (ubiquitous language)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			vocab, err := s.GetVocabulary(project)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"vocabulary": vocab}, func() {
				if vocab == nil || len(vocab.Terms) == 0 {
					fmt.Fprintln(st.stdout(), "no vocabulary yet")
					return
				}
				fmt.Fprintf(st.stdout(), "updated: %s  actor: %s\n", store.RFC3339UTC(vocab.UpdatedAt), vocab.Actor)
				for _, term := range vocab.Terms {
					fmt.Fprintf(st.stdout(), "%3d  %s\n", term.Weight, term.Term)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newVocabularyWriteCmd(st *cliState) *cobra.Command {
	var project, termsJSON string
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write a project's vocabulary (ubiquitous language)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			var terms []store.VocabularyTerm
			if err := json.Unmarshal([]byte(termsJSON), &terms); err != nil {
				return fmt.Errorf("%w: --terms must be JSON array of {term,weight}: %v", ErrUsage, err)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			v := &store.Vocabulary{Actor: actor, Terms: terms}
			if err := s.WriteVocabulary(project, v); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project":    project,
				"terms":      len(terms),
				"updated_at": store.RFC3339UTC(v.UpdatedAt),
			}, func() {
				fmt.Fprintf(st.stdout(), "wrote %d terms to vocabulary for %s\n", len(terms), project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&termsJSON, "terms", "", `JSON array of {"term":"...","weight":N}`)
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("terms")
	return cmd
}
