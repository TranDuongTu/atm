// internal/cli/channel.go
package cli

import (
	"fmt"
	"os"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// newChannelCmd is the first-class channel noun. Channels are labelled tasks
// plus local wiring underneath (see the channel capability guide), but they
// are managed as channels: this group is the only sanctioned write path.
func newChannelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Manage the project's channels (repositories, Notion) — ledger identity, local wiring, status",
		Long: "A channel is how personas communicate: a repository, a Notion database. " +
			"The ledger holds identity, purpose, and address (synced; never credentials); " +
			"local wiring in config.json holds this machine's path or MCP server name " +
			"(not synced); secrets live only in agent-side tooling. `--output json` on " +
			"list/show is the agent endpoint.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newChannelAddCmd(st))
	cmd.AddCommand(newChannelListCmd(st))
	cmd.AddCommand(newChannelShowCmd(st))
	cmd.AddCommand(newChannelEditCmd(st))
	cmd.AddCommand(newChannelRemoveCmd(st))
	return cmd
}

// channelProject resolves the target project: --project flag, else ATM_PROJECT.
func channelProject(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("project")
	if p == "" {
		p = os.Getenv("ATM_PROJECT")
	}
	if p == "" {
		return "", fmt.Errorf("%w: --project is required (or ATM_PROJECT)", core.ErrUsage)
	}
	return p, nil
}

// requireChannelCapability gates the noun on the project's enabled set. A nil
// Capabilities list is a legacy project: all built-ins read as enabled. The
// capability name is a bare string literal — internal/cli may not import
// internal/capability/channel (tests/arch/imports_test.go), so it cannot
// reference channel.CapabilityName.
func requireChannelCapability(s core.Service, project string) error {
	p, err := s.GetProject(project)
	if err != nil {
		return err
	}
	if p.Capabilities == nil {
		return nil
	}
	for _, n := range p.Capabilities {
		if n == "channel" {
			return nil
		}
	}
	return fmt.Errorf("%w: capability \"channel\" is not enabled for project %s (enable with: atm project capability add --project %s --name channel)", core.ErrUsage, project, project)
}

// newChannelAddCmd authors a channel's ledger record (tier 1): identity,
// purpose, and address. No credential flag exists here or anywhere in this
// group — secrets never touch ATM.
func newChannelAddCmd(st *cliState) *cobra.Command {
	var typ, name, purpose, url, workspace, database, page string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Author a channel's ledger record (identity + purpose + address; synced, never credentials)",
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
			rec := core.ChannelRecord{Name: name, Type: typ, Purpose: purpose, Address: core.ChannelAddress{URL: url, Workspace: workspace, Database: database, Page: page}}
			tk, err := s.CreateChannel(project, rec, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name, "type": typ, "task": tk.ID}, func() {
				fmt.Fprintf(st.stdout(), "created channel %s (%s, task %s)\n", name, typ, tk.ID)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "unique channel handle")
	cmd.Flags().StringVar(&typ, "type", "", "channel type: repo|notion")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what this channel is for (the searchable narrative)")
	cmd.Flags().StringVar(&url, "url", "", "repo: remote URL")
	cmd.Flags().StringVar(&workspace, "workspace", "", "notion: workspace")
	cmd.Flags().StringVar(&database, "database", "", "notion: database id")
	cmd.Flags().StringVar(&page, "page", "", "notion: parent page id")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

// newChannelListCmd is the agent endpoint's list: the joined read (ledger +
// this machine's wiring + local probe) for every channel in the project.
func newChannelListCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the project's channels (ledger + this machine's wiring + local probe)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := channelProject(cmd)
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
			views, err := s.ProjectChannels(project)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), views)
			}
			for _, v := range views {
				fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", v.Name, v.Type, channelStatus(v))
			}
			return nil
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	return cmd
}

// newChannelShowCmd is the agent endpoint's single-channel read.
func newChannelShowCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show one channel: ledger record, this machine's wiring, and local probe",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := channelProject(cmd)
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
			v, err := s.GetChannelByName(project, name)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), v, func() {
				fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", v.Name, v.Type, channelStatus(*v))
				if v.Purpose != "" {
					fmt.Fprintf(st.stdout(), "purpose: %s\n", v.Purpose)
				}
				if addr := formatChannelAddress(v.Address); addr != "" {
					fmt.Fprintf(st.stdout(), "address: %s\n", addr)
				}
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "channel handle")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChannelEditCmd updates purpose and/or address. Both are optional and
// independent: cmd.Flags().Changed gates each so an untouched field is
// passed as nil, not a zero value — EditChannel would otherwise clear it.
func newChannelEditCmd(st *cliState) *cobra.Command {
	var name, purpose, url, workspace, database, page string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a channel's purpose and/or address",
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
			var purposePtr *string
			if cmd.Flags().Changed("purpose") {
				purposePtr = &purpose
			}
			var addrPtr *core.ChannelAddress
			if cmd.Flags().Changed("url") || cmd.Flags().Changed("workspace") || cmd.Flags().Changed("database") || cmd.Flags().Changed("page") {
				addrPtr = &core.ChannelAddress{URL: url, Workspace: workspace, Database: database, Page: page}
			}
			if err := s.EditChannel(project, name, purposePtr, addrPtr, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name}, func() {
				fmt.Fprintf(st.stdout(), "updated channel %s\n", name)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "channel handle")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what this channel is for (the searchable narrative)")
	cmd.Flags().StringVar(&url, "url", "", "repo: remote URL")
	cmd.Flags().StringVar(&workspace, "workspace", "", "notion: workspace")
	cmd.Flags().StringVar(&database, "database", "", "notion: database id")
	cmd.Flags().StringVar(&page, "page", "", "notion: parent page id")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChannelRemoveCmd removes a channel's ledger record and this machine's
// wiring for it, if any.
func newChannelRemoveCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a channel's ledger record and this machine's wiring",
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
			if err := s.RemoveChannel(project, name, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name}, func() {
				fmt.Fprintf(st.stdout(), "removed channel %s\n", name)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "channel handle")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// channelStatus is the text-mode status column: wired/unwired, plus how many
// verification stamps this machine's wiring carries.
func channelStatus(v core.ChannelView) string {
	if v.Wiring == nil {
		return "unwired"
	}
	if n := len(v.Wiring.Stamps); n > 0 {
		if n == 1 {
			return "wired (1 stamp)"
		}
		return fmt.Sprintf("wired (%d stamps)", n)
	}
	return "wired"
}

// formatChannelAddress renders the non-empty address fields as key=value
// pairs for text-mode `show`.
func formatChannelAddress(a core.ChannelAddress) string {
	out := ""
	add := func(k, v string) {
		if v == "" {
			return
		}
		if out != "" {
			out += " "
		}
		out += k + "=" + v
	}
	add("url", a.URL)
	add("workspace", a.Workspace)
	add("database", a.Database)
	add("page", a.Page)
	return out
}
