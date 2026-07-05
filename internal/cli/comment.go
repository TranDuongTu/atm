package cli

import (
	"fmt"
	"os"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newTaskCommentCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Task comment commands",
	}
	cmd.AddCommand(newCommentAddCmd(st))
	cmd.AddCommand(newCommentListCmd(st))
	cmd.AddCommand(newCommentShowCmd(st))
	cmd.AddCommand(newCommentSetBodyCmd(st))
	cmd.AddCommand(newCommentLabelCmd(st))
	cmd.AddCommand(newCommentRemoveCmd(st))
	return cmd
}

func newCommentAddCmd(st *cliState) *cobra.Command {
	var task, body, replyTo string
	var labels []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a comment to a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			c, err := s.CreateComment(task, body, labels, replyTo, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, nil)}, func() {
				fmt.Fprintf(os.Stdout, "created comment %s\n", c.ID)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringVar(&body, "body", "", "comment body (free-form prose)")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "comment label (repeatable; full name e.g. ATM:comment:open-question)")
	cmd.Flags().StringVar(&replyTo, "reply-to", "", "optional comment id this replies to (same task)")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}

func newCommentListCmd(st *cliState) *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List comments on a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cs, err := s.ListComments(task)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comments": commentsToJSON(cs)}, func() {
				fmt.Fprint(os.Stdout, renderCommentListText(commentsToJSON(cs)))
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newCommentShowCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			c, err := s.GetComment(id)
			if err != nil {
				return err
			}
			code, _, _, _ := store.ParseCommentID(id)
			hv := s.History(code, store.Subject{Kind: "comment", ID: c.ID})
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, hv)}, func() {
				fmt.Fprint(os.Stdout, renderCommentText(commentToJSON(c, hv)))
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newCommentSetBodyCmd(st *cliState) *cobra.Command {
	var id, body string
	cmd := &cobra.Command{
		Use:   "set-body",
		Short: "Set a comment body",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetCommentBody(id, body, actor); err != nil {
				return err
			}
			c, err := s.GetComment(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, nil)}, func() {
				fmt.Fprintf(os.Stdout, "updated body %s\n", c.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	cmd.Flags().StringVar(&body, "body", "", "new body")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}

func newCommentLabelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Comment label commands",
	}
	cmd.AddCommand(newCommentLabelAddCmd(st))
	cmd.AddCommand(newCommentLabelRemoveCmd(st))
	return cmd
}

func newCommentLabelAddCmd(st *cliState) *cobra.Command {
	var id, label string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a label to a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.CommentLabelAdd(id, label, actor); err != nil {
				return err
			}
			c, err := s.GetComment(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, nil)}, func() {
				fmt.Fprintf(os.Stdout, "added label %s to %s\n", label, c.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	cmd.Flags().StringVar(&label, "label", "", "label name")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newCommentLabelRemoveCmd(st *cliState) *cobra.Command {
	var id, label string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a label from a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.CommentLabelRemove(id, label, actor); err != nil {
				return err
			}
			c, err := s.GetComment(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, nil)}, func() {
				fmt.Fprintf(os.Stdout, "removed label %s from %s\n", label, c.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	cmd.Flags().StringVar(&label, "label", "", "label name")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newCommentRemoveCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveComment(id, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": id}, func() {
				fmt.Fprintf(os.Stdout, "removed comment %s\n", id)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
