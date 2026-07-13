package cli

import (
	"fmt"
	"sort"

	"atm/internal/seed"

	"github.com/spf13/cobra"
)

func newLabelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Label registry commands",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newLabelAddCmd(st))
	cmd.AddCommand(newLabelRemoveCmd(st))
	cmd.AddCommand(newLabelListCmd(st))
	cmd.AddCommand(newLabelShowCmd(st))
	cmd.AddCommand(newLabelSeedCmd(st))
	return cmd
}

func newLabelAddCmd(st *cliState) *cobra.Command {
	var name, description, expr string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add or update a label (upsert; auto-registers)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.LabelAdd(name, description, expr, actor); err != nil {
				return err
			}
			l, err := s.LabelShow(name)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"label": labelToJSON(l)}, func() {
				fmt.Fprintf(st.stdout(), "added label %s\n", l.Name)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "label name (e.g. ATM:type:bug)")
	cmd.Flags().StringVar(&description, "description", "", "label description")
	cmd.Flags().StringVar(&expr, "expr", "",
		"board expression over labels (AND/OR/NOT/parens), e.g. "+
			"'status:open AND (priority:high OR priority:critical)'. "+
			"A label with an expression is a board: its membership is computed, "+
			"and it cannot be assigned to a task.")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newLabelRemoveCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a label (reports retained_usage)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			res, err := s.LabelRemove(name, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"retained_usage": res.RetainedUsage}, func() {
				fmt.Fprintf(st.stdout(), "removed label %s (retained usage: %d)\n", name, res.RetainedUsage)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "label name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newLabelListCmd(st *cliState) *cobra.Command {
	var project, namespace string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List labels (optionally filtered by project and/or namespace)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if namespace != "" && project == "" {
				return fmt.Errorf("%w: --namespace requires --project", ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			ls := s.LabelList(project, namespace)
			return st.emit(st.stdout(), map[string]any{"labels": labelsToJSON(ls)}, func() {
				fmt.Fprint(st.stdout(), renderLabelListText(labelsToJSON(ls)))
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "filter by project code prefix")
	cmd.Flags().StringVar(&namespace, "namespace", "", "filter by namespace (requires --project)")
	return cmd
}

func newLabelShowCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a label",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			l, err := s.LabelShow(name)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"label": labelToJSON(l)}, func() {
				switch {
				case l.Expr != "":
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", l.Name, l.Description, l.Expr)
				case l.Description != "":
					fmt.Fprintf(st.stdout(), "%s\t%s\n", l.Name, l.Description)
				default:
					fmt.Fprintln(st.stdout(), l.Name)
				}
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "label name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newLabelSeedCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Apply the default seed labels to a project (idempotent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SeedLabels(project, actor); err != nil {
				return err
			}
			names := make([]string, 0, len(seed.Labels))
			for _, l := range seed.Labels {
				names = append(names, project+":"+l.Suffix)
			}
			sort.Strings(names)
			return st.emit(st.stdout(), map[string]any{
				"project": project,
				"seeded":  len(seed.Labels),
				"labels":  names,
			}, func() {
				fmt.Fprintf(st.stdout(), "seeded %d labels into %s\n", len(seed.Labels), project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code to seed")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
