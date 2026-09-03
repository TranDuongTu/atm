// internal/cli/channel_endpoint.go
package cli

import (
	"fmt"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// newChannelEndpointCmd is the endpoint noun: a channel is a place content
// flows, not a single address, so it can be reached through several media
// at once. Content lands in the HOME endpoint and a one-line reference goes
// to every BROADCAST; reads scan them all.
func newChannelEndpointCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "endpoint",
		Short: "Manage the media a channel is reached through",
		Long: "A channel can be reached several ways at once — a Notion database that " +
			"holds the documents and a Slack channel that is told when one lands. " +
			"Each endpoint carries a medium, an address shaped for it, and a role: " +
			"home (content lands here; at most one) or broadcast (receives a " +
			"reference). No credential flag exists here or anywhere in this group — " +
			"secrets never touch ATM.",
	}
	cmd.AddCommand(newChannelEndpointAddCmd(st))
	cmd.AddCommand(newChannelEndpointRemoveCmd(st))
	return cmd
}

func newChannelEndpointAddCmd(st *cliState) *cobra.Command {
	var name, typ, role, url, workspace, database, page, channelID string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add or correct the channel's endpoint for one medium",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := channelProject(cmd)
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
			if err := requireChannelCapability(s, project); err != nil {
				return err
			}
			ep := core.ChannelEndpoint{Type: typ, Role: role, Address: core.ChannelAddress{
				URL: url, Workspace: workspace, Database: database, Page: page, ChannelID: channelID}}
			if err := s.AddChannelEndpoint(project, name, ep, actor); err != nil {
				return err
			}
			rec, err := s.GetChannelByName(project, name)
			if err != nil {
				return err
			}
			got, _ := rec.Endpoint(typ)
			return st.emit(st.stdout(), map[string]any{"project": project, "channel": name, "endpoint": got}, func() {
				fmt.Fprintf(st.stdout(), "channel %s: %s endpoint is now %s\n", name, got.Type, got.Role)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "channel handle")
	cmd.Flags().StringVar(&typ, "type", "", "endpoint medium: repo|notion|slack")
	cmd.Flags().StringVar(&role, "role", "", "home (content lands here) or broadcast (gets a reference); default follows the medium")
	cmd.Flags().StringVar(&url, "url", "", "repo: remote URL")
	cmd.Flags().StringVar(&workspace, "workspace", "", "notion: workspace; slack: workspace domain slug (the \"acme\" of acme.slack.com)")
	cmd.Flags().StringVar(&database, "database", "", "notion: database id")
	cmd.Flags().StringVar(&page, "page", "", "notion: parent page id")
	cmd.Flags().StringVar(&channelID, "channel-id", "", "slack: channel id (C0123ABC) or #handle")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func newChannelEndpointRemoveCmd(st *cliState) *cobra.Command {
	var name, typ string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Drop the channel's endpoint for one medium",
		Long: "The channel record survives with no endpoints: a handle with a purpose " +
			"and no address is a legitimate expectation waiting to be addressed, " +
			"which is exactly what applying a profile creates.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := channelProject(cmd)
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
			if err := requireChannelCapability(s, project); err != nil {
				return err
			}
			if err := s.RemoveChannelEndpoint(project, name, typ, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "channel": name, "removed": typ}, func() {
				fmt.Fprintf(st.stdout(), "channel %s: removed the %s endpoint\n", name, typ)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "channel handle")
	cmd.Flags().StringVar(&typ, "type", "", "endpoint medium: repo|notion|slack")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}
