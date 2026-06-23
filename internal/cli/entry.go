package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newTaskTodoCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "todo",
		Short: "Task todo commands",
	}
	cmd.AddCommand(newTaskTodoAddCmd(st))
	cmd.AddCommand(newTaskTodoToggleCmd(st))
	return cmd
}

func newTaskFollowupCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "followup",
		Short: "Task followup commands",
	}
	cmd.AddCommand(newTaskFollowupAddCmd(st))
	cmd.AddCommand(newTaskFollowupResolveCmd(st))
	return cmd
}

func newTaskDiscussionCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discussion",
		Short: "Task discussion commands",
	}
	cmd.AddCommand(newTaskDiscussionAddCmd(st))
	return cmd
}

func newTaskTimelineCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "Show a task's merged timeline (history + todo + followup + discussion)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			entries, err := s.TimelineList(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"id":      id,
				"entries": timelineToJSON(entries),
			}, func() {
				fmt.Fprintf(st.stdout(), "%s timeline:\n", id)
				for _, e := range entries {
					fmt.Fprintf(st.stdout(), "  %s %s\n", e.Kind, e.ID)
				}
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newTaskTodoAddCmd(st *cliState) *cobra.Command {
	var id, text string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a todo to a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.TodoAdd(id, text, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s todo added\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&text, "text", "", "todo text")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("text")
	return cmd
}

func newTaskTodoToggleCmd(st *cliState) *cobra.Command {
	var id, todoID string
	cmd := &cobra.Command{
		Use:   "toggle",
		Short: "Toggle a todo on a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.TodoToggle(id, todoID, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s todo %s toggled\n", t.ID, todoID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&todoID, "todo", "", "todo id")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("todo")
	return cmd
}

func newTaskFollowupAddCmd(st *cliState) *cobra.Command {
	var id, text, assignee, dueRaw string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a followup to a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			var due *time.Time
			if dueRaw != "" {
				tv, perr := time.Parse(time.RFC3339, dueRaw)
				if perr != nil {
					return fmt.Errorf("%w: --due must be RFC 3339: %v", ErrUsage, perr)
				}
				due = &tv
			}
			t, err := s.FollowupAdd(id, text, assignee, actor, due)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s followup added\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&text, "text", "", "followup text")
	cmd.Flags().StringVar(&assignee, "assignee", "", "followup assignee (defaults to author)")
	cmd.Flags().StringVar(&dueRaw, "due", "", "followup due date (RFC 3339)")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("text")
	return cmd
}

func newTaskFollowupResolveCmd(st *cliState) *cobra.Command {
	var id, followupID string
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve a followup on a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.FollowupResolve(id, followupID, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s followup %s resolved\n", t.ID, followupID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&followupID, "followup", "", "followup id")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("followup")
	return cmd
}

func newTaskDiscussionAddCmd(st *cliState) *cobra.Command {
	var id, text string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a discussion entry to a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			t, err := s.DiscussionAdd(id, text, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(st.stdout(), "%s discussion added\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&text, "text", "", "discussion text")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("text")
	return cmd
}
