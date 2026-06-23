package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTaskNextCmd(st *cliState) *cobra.Command {
	var project string
	var claim bool
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Return the next claimable task for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(claim)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, guide, err := s.Next(project, claim, actor)
			if err != nil {
				return err
			}
			var taskJSON any
			if t != nil {
				taskJSON = taskToJSON(t)
			}
			return st.emit(st.stdout(), map[string]any{
				"task":  taskJSON,
				"guide": guideToJSON(guide),
			}, func() {
				renderNextText(st.stdout(), t, guide)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().BoolVar(&claim, "claim", false, "claim the returned task atomically")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newTaskClaimCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "claim",
		Short: "Claim a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.Claim(id, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s claimed by %s\n", t.ID, actor)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newTaskUnclaimCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "unclaim",
		Short: "Release a task claim",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.Unclaim(id, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s unclaimed\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
