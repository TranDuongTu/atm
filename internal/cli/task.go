package cli

import (
	"fmt"
	"os"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newTaskCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task commands",
	}
	cmd.AddCommand(newTaskCreateCmd(st))
	cmd.AddCommand(newTaskListCmd(st))
	cmd.AddCommand(newTaskShowCmd(st))
	cmd.AddCommand(newTaskSetTitleCmd(st))
	cmd.AddCommand(newTaskSetDescriptionCmd(st))
	cmd.AddCommand(newTaskLabelCmd(st))
	cmd.AddCommand(newTaskRemoveCmd(st))
	return cmd
}

func newTaskCreateCmd(st *cliState) *cobra.Command {
	var project, title, description string
	var labels []string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.CreateTask(project, title, description, labels, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
				fmt.Fprintf(os.Stdout, "created task %s\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&title, "title", "", "task title")
	cmd.Flags().StringVar(&description, "description", "", "task description")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label (repeatable; full name e.g. ATM:type:bug)")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newTaskListCmd(st *cliState) *cobra.Command {
	var project string
	var labels []string
	var facets bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks (optionally faceted by wildcard labels)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			filters := store.QueryFilters{Project: project, Labels: labels}
			if facets {
				groups, others := s.GroupTasks(filters)
				f := jsonFacets{
					Groups: groupsToJSON(groups),
					Others: tasksToJSON(others),
				}
				return st.emit(st.stdout(), map[string]any{"groups": f.Groups, "others": f.Others}, func() {
					fmt.Fprint(os.Stdout, renderFacetsText(f))
				})
			}
			ts := s.ListTasks(filters)
			return st.emit(st.stdout(), map[string]any{"tasks": tasksToJSON(ts)}, func() {
				fmt.Fprint(os.Stdout, renderTaskListText(tasksToJSON(ts)))
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "filter by project code")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label filter (repeatable; full name or wildcard suffix e.g. ATM:status:*)")
	cmd.Flags().BoolVar(&facets, "facets", false, "group output by wildcard label facets")
	return cmd
}

func newTaskShowCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.GetTask(id)
			if err != nil {
				return err
			}
			hv := s.History(t.ProjectCode, store.Subject{Kind: "task", ID: t.ID})
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, hv)}, func() {
				jt := taskToJSON(t, hv)
				fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", jt.ID, jt.Title, formatLabels(jt.Labels))
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newTaskSetTitleCmd(st *cliState) *cobra.Command {
	var id, title string
	cmd := &cobra.Command{
		Use:   "set-title",
		Short: "Set a task title",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetTitle(id, title, actor); err != nil {
				return err
			}
			t, err := s.GetTask(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
				fmt.Fprintf(os.Stdout, "updated title %s\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&title, "title", "", "new title")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newTaskSetDescriptionCmd(st *cliState) *cobra.Command {
	var id, description string
	cmd := &cobra.Command{
		Use:   "set-description",
		Short: "Set a task description",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetDescription(id, description, actor); err != nil {
				return err
			}
			t, err := s.GetTask(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
				fmt.Fprintf(os.Stdout, "updated description %s\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("description")
	return cmd
}

func newTaskLabelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Task label commands",
	}
	cmd.AddCommand(newTaskLabelAddCmd(st))
	cmd.AddCommand(newTaskLabelRemoveCmd(st))
	return cmd
}

func newTaskLabelAddCmd(st *cliState) *cobra.Command {
	var id, label string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a label to a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.TaskLabelAdd(id, label, actor); err != nil {
				return err
			}
			t, err := s.GetTask(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
				fmt.Fprintf(os.Stdout, "added label %s to %s\n", label, t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&label, "label", "", "label name")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newTaskLabelRemoveCmd(st *cliState) *cobra.Command {
	var id, label string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a label from a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.TaskLabelRemove(id, label, actor); err != nil {
				return err
			}
			t, err := s.GetTask(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
				fmt.Fprintf(os.Stdout, "removed label %s from %s\n", label, t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&label, "label", "", "label name")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newTaskRemoveCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveTask(id, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": id}, func() {
				fmt.Fprintf(os.Stdout, "removed task %s\n", id)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func groupsToJSON(gs []store.LabelGroup) []jsonLabelGroup {
	out := make([]jsonLabelGroup, 0, len(gs))
	for _, g := range gs {
		out = append(out, jsonLabelGroup{Label: g.Label, Tasks: tasksToJSON(g.Tasks)})
	}
	return out
}
