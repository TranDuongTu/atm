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
	cmd.AddCommand(newProjectSetNameCmd(st))
	cmd.AddCommand(newProjectSetTypeAxisCmd(st))
	cmd.AddCommand(newProjectRepoCmd(st))
	cmd.AddCommand(newProjectLabelCmd(st))
	cmd.AddCommand(newProjectGuideCmd(st))
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
	remove := &cobra.Command{
		Use:   "remove",
		Short: "Soft-remove a label from a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			res, err := s.LabelRemove(code, label, actor)
			if err != nil {
				return err
			}
			retained := 0
			if res != nil {
				retained = res.RetainedUsage
			}
			ls := s.LabelList(code)
			return st.emit(st.stdout(), map[string]any{
				"labels":         labelsToJSON(ls),
				"retained_usage": retained,
			}, func() {
				fmt.Fprintf(os.Stdout, "removed label %s from %s (retained_usage: %d)\n", label, code, retained)
			})
		},
	}
	remove.Flags().StringVar(&code, "code", "", "project code")
	remove.Flags().StringVar(&label, "label", "", "label name")
	_ = remove.MarkFlagRequired("code")
	_ = remove.MarkFlagRequired("label")
	cmd.AddCommand(list, add, remove)
	return cmd
}

func newProjectSetNameCmd(st *cliState) *cobra.Command {
	var code, name string
	cmd := &cobra.Command{
		Use:   "set-name",
		Short: "Set a project's name",
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
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p)}, func() {
				fmt.Fprintf(os.Stdout, "%s name -> %s\n", p.Code, p.Name)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "project name")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newProjectSetTypeAxisCmd(st *cliState) *cobra.Command {
	var code, namespace string
	cmd := &cobra.Command{
		Use:   "set-type-axis",
		Short: "Set a project's type axis namespace",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetTypeAxis(code, namespace, actor); err != nil {
				return err
			}
			p, err := s.GetProject(code)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p)}, func() {
				fmt.Fprintf(os.Stdout, "%s type_axis -> %s\n", p.Code, p.TypeAxis)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	cmd.Flags().StringVar(&namespace, "namespace", "", "type axis namespace")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("namespace")
	return cmd
}

func newProjectRepoCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Project repo path commands",
	}
	var code, path string
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a repo path to a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RepoAdd(code, path, actor); err != nil {
				return err
			}
			p, err := s.GetProject(code)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p)}, func() {
				fmt.Fprintf(os.Stdout, "%s repo add %s\n", p.Code, path)
			})
		},
	}
	add.Flags().StringVar(&code, "code", "", "project code")
	add.Flags().StringVar(&path, "path", "", "repo path")
	_ = add.MarkFlagRequired("code")
	_ = add.MarkFlagRequired("path")
	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove a repo path from a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RepoRemove(code, path, actor); err != nil {
				return err
			}
			p, err := s.GetProject(code)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p)}, func() {
				fmt.Fprintf(os.Stdout, "%s repo remove %s\n", p.Code, path)
			})
		},
	}
	remove.Flags().StringVar(&code, "code", "", "project code")
	remove.Flags().StringVar(&path, "path", "", "repo path")
	_ = remove.MarkFlagRequired("code")
	_ = remove.MarkFlagRequired("path")
	cmd.AddCommand(add, remove)
	return cmd
}
