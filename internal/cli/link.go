package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTaskLinkCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Task link commands",
	}
	cmd.AddCommand(newTaskLinkAddCmd(st))
	cmd.AddCommand(newTaskLinkRemoveCmd(st))
	cmd.AddCommand(newTaskLinkListCmd(st))
	return cmd
}

func newTaskLinkAddCmd(st *cliState) *cobra.Command {
	var id, linkType, target string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a link from a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.LinkAdd(id, linkType, target, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"id": id, "link": jsonLink{Type: linkType, Target: target}}, func() {
				fmt.Fprintf(st.stdout(), "%s link %s -> %s added\n", id, linkType, target)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&linkType, "type", "", "link type")
	cmd.Flags().StringVar(&target, "target", "", "target task id")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

func newTaskLinkRemoveCmd(st *cliState) *cobra.Command {
	var id, linkType, target string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a link from a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.LinkRemove(id, linkType, target, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"id": id, "link": jsonLink{Type: linkType, Target: target}}, func() {
				fmt.Fprintf(st.stdout(), "%s link %s -> %s removed\n", id, linkType, target)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	cmd.Flags().StringVar(&linkType, "type", "", "link type")
	cmd.Flags().StringVar(&target, "target", "", "target task id")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

func newTaskLinkListCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List links for a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			res, err := s.LinkList(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"id":        id,
				"links_out": edgesToJSON(res.Out),
				"links_in":  edgesToJSON(res.In),
				"stale":     edgesToJSON(res.Stale),
			}, func() {
				fmt.Fprintf(st.stdout(), "%s out:\n", id)
				for _, e := range res.Out {
					fmt.Fprintf(st.stdout(), "  %s -> %s\n", e.Link.Type, e.Link.Target)
				}
				fmt.Fprintf(st.stdout(), "%s in:\n", id)
				for _, e := range res.In {
					fmt.Fprintf(st.stdout(), "  %s <- %s\n", e.Link.Type, e.Link.Target)
				}
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "task id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
