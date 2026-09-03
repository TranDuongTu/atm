// internal/cli/channel.go
package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

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
	cmd.AddCommand(newChannelEndpointCmd(st))
	cmd.AddCommand(newChannelEditCmd(st))
	cmd.AddCommand(newChannelRemoveCmd(st))
	cmd.AddCommand(newChannelWireCmd(st))
	cmd.AddCommand(newChannelStampCmd(st))
	cmd.AddCommand(newChannelMigrateCmd(st))
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
	var typ, name, purpose, url, workspace, database, page, channelID string
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
			rec := core.ChannelRecord{Name: name, Type: typ, Purpose: purpose, Address: core.ChannelAddress{URL: url, Workspace: workspace, Database: database, Page: page, ChannelID: channelID}}
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
	cmd.Flags().StringVar(&typ, "type", "", "channel type: repo|notion|slack")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what this channel is for (the searchable narrative)")
	cmd.Flags().StringVar(&url, "url", "", "repo: remote URL")
	cmd.Flags().StringVar(&workspace, "workspace", "", "notion: workspace; slack: workspace domain slug (the \"acme\" of acme.slack.com)")
	cmd.Flags().StringVar(&database, "database", "", "notion: database id")
	cmd.Flags().StringVar(&page, "page", "", "notion: parent page id")
	cmd.Flags().StringVar(&channelID, "channel-id", "", "slack: channel id (C0123ABC) or #handle")
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
			now := core.Now()
			for _, v := range views {
				fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", v.Name, v.Type, channelStatus(v, now))
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
				fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", v.Name, v.Type, channelStatus(*v, core.Now()))
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
// independent: cmd.Flags().Changed gates the purpose pointer, so an untouched
// purpose is passed as nil rather than a zero value that would clear it.
// Address is NOT a per-field pointer — EditChannel takes the whole struct and
// stores exactly what it is given — so an address edit reads the current
// record first and overlays only the flags the user actually named. Building
// the struct from the flag variables alone would silently clear every sibling
// field (a --database edit dropping --workspace), and the address lives
// nowhere else.
func newChannelEditCmd(st *cliState) *cobra.Command {
	var name, purpose, url, workspace, database, page, channelID string
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
			if cmd.Flags().Changed("url") || cmd.Flags().Changed("workspace") || cmd.Flags().Changed("database") || cmd.Flags().Changed("page") || cmd.Flags().Changed("channel-id") {
				cur, err := s.GetChannelByName(project, name)
				if err != nil {
					return err
				}
				next := cur.Address
				for _, f := range []struct {
					flag string
					dst  *string
					src  string
				}{
					{"url", &next.URL, url},
					{"workspace", &next.Workspace, workspace},
					{"database", &next.Database, database},
					{"page", &next.Page, page},
					{"channel-id", &next.ChannelID, channelID},
				} {
					if cmd.Flags().Changed(f.flag) {
						*f.dst = f.src
					}
				}
				addrPtr = &next
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
	cmd.Flags().StringVar(&workspace, "workspace", "", "notion: workspace; slack: workspace domain slug (the \"acme\" of acme.slack.com)")
	cmd.Flags().StringVar(&database, "database", "", "notion: database id")
	cmd.Flags().StringVar(&page, "page", "", "notion: parent page id")
	cmd.Flags().StringVar(&channelID, "channel-id", "", "slack: channel id (C0123ABC) or #handle")
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

// newChannelWireCmd records how THIS machine reaches a channel (tier 2):
// a local path, an MCP server name, or both — never a secret. At least one
// of the two is required, since wiring nothing is a no-op that looks like
// success.
func newChannelWireCmd(st *cliState) *cobra.Command {
	var name, path, mcpServer string
	cmd := &cobra.Command{
		Use:   "wire",
		Short: "Record this machine's local path and/or MCP server for a channel (never a secret)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" && mcpServer == "" {
				return fmt.Errorf("%w: at least one of --path or --mcp-server is required", core.ErrUsage)
			}
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
			if err := s.SetChannelWiring(project, name, path, mcpServer, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name, "path": path, "mcp_server": mcpServer}, func() {
				fmt.Fprintf(st.stdout(), "wired channel %s\n", name)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "channel handle")
	cmd.Flags().StringVar(&path, "path", "", "local path (repo channels)")
	cmd.Flags().StringVar(&mcpServer, "mcp-server", "", "MCP server name the agents on this machine reach the channel through (notion, slack, …)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChannelStampCmd records a verification stamp: an actor touched the
// channel's wiring and vouches for it. --note is required — an unexplained
// stamp is not worth much to the next reader.
func newChannelStampCmd(st *cliState) *cobra.Command {
	var name, note string
	cmd := &cobra.Command{
		Use:   "stamp",
		Short: "Record a verification stamp: this actor touched the channel and vouches for its wiring",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if note == "" {
				return fmt.Errorf("%w: --note is required", core.ErrUsage)
			}
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
			if err := s.AddChannelStamp(project, name, note, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name, "note": note}, func() {
				fmt.Fprintf(st.stdout(), "stamped channel %s\n", name)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "channel handle")
	cmd.Flags().StringVar(&note, "note", "", "verification note")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChannelMigrateCmd lifts every legacy repo dispatch target (config.json's
// `repos` list, written by the now-retired `atm project repo add`) into a
// repo channel: idempotent, since a prior run's channels are recognized and
// left alone. Three outcomes are reported: migrated handles got both a
// ledger record and this machine's wiring; unwired handles got a ledger
// record but their legacy path no longer exists on disk, so wiring is left
// to a concierge; skipped handles were left untouched in the legacy config
// because their name already belongs to a different-typed channel — nothing
// is lost, but nothing is migrated either.
func newChannelMigrateCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-repos",
		Short: "Lift legacy repo dispatch targets into repo channels (idempotent; concierge confirms purpose later)",
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
			migrated, unwired, skipped, err := s.MigrateReposToChannels(project, actor)
			if err != nil {
				return err
			}
			payload := map[string]any{
				"project":  project,
				"migrated": migrated,
				"unwired":  normalizeStrSlice(unwired),
				"skipped":  normalizeStrSlice(skipped),
			}
			return st.emit(st.stdout(), payload, func() {
				fmt.Fprintf(st.stdout(), "migrated %d repo(s) into channels; author each channel's purpose with `atm channel edit`\n", migrated)
				if len(unwired) > 0 {
					fmt.Fprintf(st.stdout(), "no longer on disk, so recorded WITHOUT local wiring: %s — re-wire with `atm channel wire`\n", strings.Join(unwired, ", "))
				}
				if len(skipped) > 0 {
					fmt.Fprintf(st.stdout(), "left untouched in the legacy config (name already used by a different channel type, nothing lost): %s\n", strings.Join(skipped, ", "))
				}
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	return cmd
}

// channelStatus is the text-mode status column. The rule itself lives in
// core.ChannelStatus so this surface and the TUI cannot disagree about the
// same record — computing it here from Wiring alone would print "wired" for a
// repo whose directory is gone, which is more than ATM knows and the opposite
// of what the overlay shows. Text mode takes the note and drops the glyph:
// this column is read by scripts as much as by people, and ●/◐/○ carries
// nothing the note does not already say.
func channelStatus(v core.ChannelView, now time.Time) string {
	_, note := core.ChannelStatus(v, now)
	return note
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
