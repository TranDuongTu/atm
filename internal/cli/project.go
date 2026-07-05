package cli

import (
	"fmt"
	"os"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newProjectCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Project commands",
	}
	cmd.AddCommand(newProjectCreateCmd(st))
	cmd.AddCommand(newProjectListCmd(st))
	cmd.AddCommand(newProjectShowCmd(st))
	cmd.AddCommand(newProjectSetNameCmd(st))
	cmd.AddCommand(newProjectRemoveCmd(st))
	return cmd
}

func newProjectCreateCmd(st *cliState) *cobra.Command {
	var code, name string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a project (minimal: code + name)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.CreateProject(code, name, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p, nil)}, func() {
				fmt.Fprintf(os.Stdout, "created project %s\n", p.Code)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code (^[A-Z]{3,6}$)")
	cmd.Flags().StringVar(&name, "name", "", "project name")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newProjectListCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			ps := s.ListProjects()
			return st.emit(st.stdout(), map[string]any{"projects": projectsToJSON(ps)}, func() {
				fmt.Fprint(os.Stdout, renderProjectListText(projectsToJSON(ps)))
			})
		},
	}
	return cmd
}

func newProjectShowCmd(st *cliState) *cobra.Command {
	var code string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(code)
			if err != nil {
				return err
			}
			hv := s.History(p.Code, store.Subject{Kind: "project", Code: p.Code})
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p, hv)}, func() {
				fmt.Fprintln(os.Stdout, renderProjectText(projectToJSON(p, hv)))
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	_ = cmd.MarkFlagRequired("code")
	return cmd
}

func newProjectSetNameCmd(st *cliState) *cobra.Command {
	var code, name string
	cmd := &cobra.Command{
		Use:   "set-name",
		Short: "Rename a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetProjectName(code, name, actor); err != nil {
				return err
			}
			p, err := s.GetProject(code)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p, nil)}, func() {
				fmt.Fprintf(os.Stdout, "renamed project %s\n", p.Code)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "new project name")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newProjectRemoveCmd(st *cliState) *cobra.Command {
	var code string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a project (zero-task guard)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveProject(code, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": code}, func() {
				fmt.Fprintf(os.Stdout, "removed project %s\n", code)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	_ = cmd.MarkFlagRequired("code")
	return cmd
}
