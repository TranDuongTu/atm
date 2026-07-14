package cli

import (
	"fmt"
	"os"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

// bindTaskIDFlags registers the canonical --task flag (bound to id) and the
// deprecated --id alias (bound to legacy) for a task-level subcommand. Call
// resolveTaskID inside RunE to fold a legacy --id value into id and emit a
// deprecation notice to stderr when the alias is used. The --id alias is kept
// hidden from --help so new callers discover --task.
func bindTaskIDFlags(cmd *cobra.Command, id *string, legacy *string) {
	cmd.Flags().StringVar(id, "task", "", "task id")
	cmd.Flags().StringVar(legacy, "id", "", "task id (deprecated alias for --task)")
	_ = cmd.Flags().MarkHidden("id")
}

// resolveTaskID folds the deprecated --id alias into the canonical --task
// value when --task was not supplied, returning an error if neither was given.
// depNotice is written to stderr when the alias carried the value.
func resolveTaskID(st *cliState, id, legacy string) (string, error) {
	if id != "" {
		if legacy != "" {
			fmt.Fprintf(st.stderr(), "warning: --id is deprecated for task subcommands; use --task. Ignoring --id in favor of --task.\n")
		}
		return id, nil
	}
	if legacy != "" {
		fmt.Fprintf(st.stderr(), "warning: --id is deprecated for task subcommands; use --task.\n")
		return legacy, nil
	}
	return "", fmt.Errorf("required flag(s) \"task\" not set")
}

func newTaskCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task commands",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newTaskCreateCmd(st))
	cmd.AddCommand(newTaskListCmd(st))
	cmd.AddCommand(newTaskShowCmd(st))
	cmd.AddCommand(newTaskSetTitleCmd(st))
	cmd.AddCommand(newTaskSetDescriptionCmd(st))
	cmd.AddCommand(newTaskLabelCmd(st))
	cmd.AddCommand(newTaskCommentCmd(st))
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
	var project, expr string
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
			if expr != "" {
				if _, err := store.ParseExpr(expr); err != nil {
					return err
				}
			}
			filters := store.QueryFilters{Project: project, Labels: labels, Expr: expr}
			if facets {
				groups, others, gerr := s.GroupTasksErr(filters)
				if gerr != nil {
					return gerr
				}
				f := jsonFacets{
					Groups: groupsToJSON(groups),
					Others: tasksToJSON(others),
				}
				return st.emit(st.stdout(), map[string]any{"groups": f.Groups, "others": f.Others}, func() {
					fmt.Fprint(os.Stdout, renderFacetsText(f))
				})
			}
			ts, err := s.ListTasksErr(filters)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"tasks": tasksToJSON(ts)}, func() {
				fmt.Fprint(os.Stdout, renderTaskListText(tasksToJSON(ts)))
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "filter by project code")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label filter (repeatable; full name or wildcard suffix e.g. ATM:status:*)")
	cmd.Flags().BoolVar(&facets, "facets", false, "group output by wildcard label facets")
	cmd.Flags().StringVar(&expr, "expr", "", "board expression filter (AND/OR/NOT/parens over bare label names)")
	return cmd
}

func newTaskShowCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := resolveTaskID(st, id, legacy)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.GetTask(taskID)
			if err != nil {
				return err
			}
			hv, err := s.HistoryE(t.ProjectCode, store.Subject{Kind: "task", ID: t.ID})
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, hv)}, func() {
				jt := taskToJSON(t, hv)
				fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", jt.ID, jt.Title, formatLabels(jt.Labels))
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newTaskSetTitleCmd(st *cliState) *cobra.Command {
	var id, legacy, title string
	cmd := &cobra.Command{
		Use:   "set-title",
		Short: "Set a task title",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := resolveTaskID(st, id, legacy)
			if err != nil {
				return err
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetTitle(taskID, title, actor); err != nil {
				return err
			}
			t, err := s.GetTask(taskID)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
				fmt.Fprintf(os.Stdout, "updated title %s\n", t.ID)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&title, "title", "", "new title")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newTaskSetDescriptionCmd(st *cliState) *cobra.Command {
	var id, legacy, description string
	cmd := &cobra.Command{
		Use:   "set-description",
		Short: "Set a task description",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := resolveTaskID(st, id, legacy)
			if err != nil {
				return err
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetDescription(taskID, description, actor); err != nil {
				return err
			}
			t, err := s.GetTask(taskID)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
				fmt.Fprintf(os.Stdout, "updated description %s\n", t.ID)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&description, "description", "", "new description")
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
	var id, legacy, label string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a label to a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := resolveTaskID(st, id, legacy)
			if err != nil {
				return err
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.TaskLabelAdd(taskID, label, actor); err != nil {
				return err
			}
			t, err := s.GetTask(taskID)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
				fmt.Fprintf(os.Stdout, "added label %s to %s\n", label, t.ID)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&label, "label", "", "label name")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newTaskLabelRemoveCmd(st *cliState) *cobra.Command {
	var id, legacy, label string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a label from a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := resolveTaskID(st, id, legacy)
			if err != nil {
				return err
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.TaskLabelRemove(taskID, label, actor); err != nil {
				return err
			}
			t, err := s.GetTask(taskID)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
				fmt.Fprintf(os.Stdout, "removed label %s from %s\n", label, t.ID)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&label, "label", "", "label name")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newTaskRemoveCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := resolveTaskID(st, id, legacy)
			if err != nil {
				return err
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveTask(taskID, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": taskID}, func() {
				fmt.Fprintf(os.Stdout, "removed task %s\n", taskID)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func groupsToJSON(gs []store.LabelGroup) []jsonLabelGroup {
	out := make([]jsonLabelGroup, 0, len(gs))
	for _, g := range gs {
		out = append(out, jsonLabelGroup{Label: g.Label, Tasks: tasksToJSON(g.Tasks)})
	}
	return out
}
