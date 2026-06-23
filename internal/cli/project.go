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
		Short: "Project management commands",
	}
	cmd.AddCommand(newProjectCreateCmd(st))
	cmd.AddCommand(newProjectListCmd(st))
	cmd.AddCommand(newProjectShowCmd(st))
	cmd.AddCommand(newProjectLabelCmd(st))
	return cmd
}

func newProjectCreateCmd(st *cliState) *cobra.Command {
	var code, name, typeAxis string
	var labels []string
	var repoPaths []string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			ls := make([]store.Label, 0, len(labels))
			for _, l := range labels {
				ls = append(ls, store.Label{Name: l})
			}
			p, err := s.CreateProject(code, name, typeAxis, ls, repoPaths, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p)}, func() {
				fmt.Fprintf(os.Stdout, "created project %s\n", p.Code)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "project name")
	cmd.Flags().StringVar(&typeAxis, "type-axis", "", "type axis namespace")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label to seed (repeatable)")
	cmd.Flags().StringArrayVar(&repoPaths, "repo-path", nil, "repo path (repeatable)")
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
			out := make([]jsonProject, 0, len(ps))
			for _, p := range ps {
				out = append(out, projectToJSON(p))
			}
			return st.emit(st.stdout(), map[string]any{"projects": out}, func() {
				for _, p := range ps {
					fmt.Fprintf(os.Stdout, "%s  %s\n", p.Code, p.Name)
				}
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
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p)}, func() {
				fmt.Fprintf(os.Stdout, "%s  %s  (type_axis=%s)\n", p.Code, p.Name, p.TypeAxis)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	_ = cmd.MarkFlagRequired("code")
	return cmd
}

func newProjectLabelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Project label commands",
	}
	var code, label, description string
	list := &cobra.Command{
		Use:   "list",
		Short: "List project labels",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			ls := s.LabelList(code)
			return st.emit(st.stdout(), map[string]any{"labels": labelsToJSON(ls)}, func() {
				for _, l := range ls {
					fmt.Fprintf(os.Stdout, "%s\n", l.Name)
				}
			})
		},
	}
	list.Flags().StringVar(&code, "code", "", "project code")
	_ = list.MarkFlagRequired("code")
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a label to a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.LabelAdd(code, label, description, actor); err != nil {
				return err
			}
			ls := s.LabelList(code)
			return st.emit(st.stdout(), map[string]any{"labels": labelsToJSON(ls)}, func() {
				fmt.Fprintf(os.Stdout, "added label %s to %s\n", label, code)
			})
		},
	}
	add.Flags().StringVar(&code, "code", "", "project code")
	add.Flags().StringVar(&label, "label", "", "label name")
	add.Flags().StringVar(&description, "description", "", "label description")
	_ = add.MarkFlagRequired("code")
	_ = add.MarkFlagRequired("label")
	cmd.AddCommand(list, add)
	return cmd
}
