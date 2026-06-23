package cli

import (
	"fmt"
	"io"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newTaskCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task management commands",
	}
	cmd.AddCommand(newTaskCreateCmd(st))
	cmd.AddCommand(newTaskShowCmd(st))
	cmd.AddCommand(newTaskListCmd(st))
	cmd.AddCommand(newTaskSetStatusCmd(st))
	cmd.AddCommand(newTaskSetTitleCmd(st))
	cmd.AddCommand(newTaskSetDescriptionCmd(st))
	cmd.AddCommand(newTaskLabelCmd(st))
	cmd.AddCommand(newTaskLinkCmd(st))
	cmd.AddCommand(newTaskNextCmd(st))
	cmd.AddCommand(newTaskClaimCmd(st))
	cmd.AddCommand(newTaskUnclaimCmd(st))
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
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "created %s\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&title, "title", "", "task title")
	cmd.Flags().StringVar(&description, "description", "", "task description")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "task label (repeatable)")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newTaskShowCmd(st *cliState) *cobra.Command {
	var id string
	var withContext bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if withContext {
				res, err := s.ShowWithContext(id)
				if err != nil {
					return err
				}
				payload := map[string]any{
					"task": taskToJSON(res.Task),
					"context": map[string]any{
						"links_out":   edgesToJSON(res.Context.LinksOut),
						"links_in":    edgesToJSON(res.Context.LinksIn),
						"conventions": conventionsToJSON(res.Context.Conventions),
						"timeline":    timelineToJSON(res.Context.Timeline),
						"guide":       guideToJSON(res.Context.Guide),
					},
				}
				return st.emit(st.stdout(), payload, func() {
					renderTaskText(st.stdout(), res.Task)
					renderContextText(st.stdout(), res)
				})
			}
			t, err := s.GetTask(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				renderTaskText(st.stdout(), t)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().BoolVar(&withContext, "with-context", false, "include linked tasks, conventions, timeline, and the project guide")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newTaskListCmd(st *cliState) *cobra.Command {
	var project, status, assignee, claimant string
	var labels []string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			tasks := s.ListTasks(store.QueryFilters{
				Project:  project,
				Labels:   labels,
				Status:   status,
				Assignee: assignee,
				Claimant: claimant,
			})
			jt := make([]jsonTask, 0, len(tasks))
			for _, t := range tasks {
				jt = append(jt, taskToJSON(t))
			}
			return st.emit(st.stdout(), map[string]any{"tasks": jt}, func() {
				renderTaskListText(st.stdout(), tasks)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "filter by label (repeatable, AND)")
	cmd.Flags().StringVar(&status, "status", "", "filter by status")
	cmd.Flags().StringVar(&assignee, "assignee", "", "filter by followup assignee")
	cmd.Flags().StringVar(&claimant, "claimant", "", "filter by claim actor")
	return cmd
}

func newTaskSetStatusCmd(st *cliState) *cobra.Command {
	var id, status string
	cmd := &cobra.Command{
		Use:   "set-status",
		Short: "Set a task status",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetStatus(id, status, actor); err != nil {
				return err
			}
			t, err := s.GetTask(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s status -> %s\n", t.ID, t.Status)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&status, "status", "", "new status")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("status")
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
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s title -> %s\n", t.ID, t.Title)
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
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s description updated\n", t.ID)
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
	var id, label string
	add := &cobra.Command{
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
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s labels: %v\n", t.ID, t.Labels)
			})
		},
	}
	remove := &cobra.Command{
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
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s labels: %v\n", t.ID, t.Labels)
			})
		},
	}
	add.Flags().StringVar(&id, "id", "", "task id")
	add.Flags().StringVar(&label, "label", "", "label name")
	_ = add.MarkFlagRequired("id")
	_ = add.MarkFlagRequired("label")
	remove.Flags().StringVar(&id, "id", "", "task id")
	remove.Flags().StringVar(&label, "label", "", "label name")
	_ = remove.MarkFlagRequired("id")
	_ = remove.MarkFlagRequired("label")
	cmd.AddCommand(add, remove)
	return cmd
}

func conventionsToJSON(cs []store.Convention) []jsonConvention {
	out := make([]jsonConvention, 0, len(cs))
	for _, c := range cs {
		ml := c.MatchedLabels
		if ml == nil {
			ml = []string{}
		}
		out = append(out, jsonConvention{ID: c.ID, Title: c.Title, MatchedLabels: ml})
	}
	return out
}

func renderContextText(w io.Writer, res *store.ShowWithContextResult) {
	fmt.Fprintf(w, "context:\n")
	if len(res.Context.Conventions) > 0 {
		fmt.Fprintf(w, "  conventions:\n")
		for _, c := range res.Context.Conventions {
			fmt.Fprintf(w, "    %s %s [%v]\n", c.ID, c.Title, c.MatchedLabels)
		}
	}
	if res.Context.Guide != nil {
		fmt.Fprintf(w, "  guide: %d section(s)\n", len(res.Context.Guide.Sections))
	} else {
		fmt.Fprintf(w, "  guide: (none)\n")
	}
}
