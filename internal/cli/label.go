package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newLabelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Label registry commands",
	}
	cmd.AddCommand(newLabelAddCmd(st))
	cmd.AddCommand(newLabelRemoveCmd(st))
	cmd.AddCommand(newLabelListCmd(st))
	cmd.AddCommand(newLabelShowCmd(st))
	return cmd
}

func newLabelAddCmd(st *cliState) *cobra.Command {
	var name, description string
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
			if err := s.LabelAdd(name, description, actor); err != nil {
				return err
			}
			l, err := s.LabelShow(name)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"label": labelToJSON(l)}, func() {
				fmt.Fprintf(os.Stdout, "added label %s\n", l.Name)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "label name (e.g. ATM:type:bug)")
	cmd.Flags().StringVar(&description, "description", "", "label description")
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
				fmt.Fprintf(os.Stdout, "removed label %s (retained usage: %d)\n", name, res.RetainedUsage)
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
			s, err := st.openStore()
			if err != nil {
				return err
			}
			ls := s.LabelList(project, namespace)
			return st.emit(st.stdout(), map[string]any{"labels": labelsToJSON(ls)}, func() {
				fmt.Fprint(os.Stdout, renderLabelListText(labelsToJSON(ls)))
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
				if l.Description == "" {
					fmt.Fprintln(os.Stdout, l.Name)
				} else {
					fmt.Fprintf(os.Stdout, "%s\t%s\n", l.Name, l.Description)
				}
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "label name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}