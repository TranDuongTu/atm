package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"atm/internal/agent"

	"github.com/spf13/cobra"
)

func newAgentsCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Inspect and switch the host agent used by atm dev / atm manage",
	}
	cmd.AddCommand(newAgentsListCmd(st))
	cmd.AddCommand(newAgentsSelectCmd(st))
	cmd.AddCommand(newAgentsArgsCmd(st))
	return cmd
}

type agentRow struct {
	Name     string `json:"name"`
	Launch   string `json:"launch"`
	Status   string `json:"status"`
	Args     string `json:"args,omitempty"`
	Selected bool   `json:"selected"`
}

func newAgentsListCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List supported agents with live readiness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cfg, err := s.GetAgentsConfig()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			rows := make([]agentRow, 0, len(agent.Catalog()))
			for _, e := range agent.Catalog() {
				rows = append(rows, agentRow{
					Name:     e.Name,
					Launch:   strings.Join(e.Base(), " "),
					Status:   agent.Status(e, home, exec.LookPath).String(),
					Args:     strings.Join(cfg.Args[e.Name], " "),
					Selected: cfg.Selected == e.Name,
				})
			}
			return st.emit(st.stdout(), map[string]any{"agents": rows}, func() {
				for _, r := range rows {
					marker := ""
					if r.Selected {
						marker = "  *selected"
					}
					fmt.Fprintf(st.stdout(), "%-16s  %-26s  %-24s  %s%s\n",
						r.Name, r.Launch, r.Status, r.Args, marker)
				}
			})
		},
	}
}

func newAgentsSelectCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "select <name>",
		Short: "Set the default agent for atm dev / atm manage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			e, ok := agent.Lookup(name)
			if !ok {
				return fmt.Errorf("%w: unknown agent %q (see `atm agents list`)", ErrUsage, name)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetSelectedAgent(name, "admin@cli:unset"); err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			r := agent.Status(e, home, exec.LookPath)
			if !r.Ready() {
				fmt.Fprintf(st.stderr(), "warning: %s is not ready (%s)\n", name, r.String())
			}
			return st.emit(st.stdout(), map[string]any{"selected": name}, func() {
				fmt.Fprintf(st.stdout(), "selected %s\n", name)
			})
		},
	}
}

func newAgentsArgsCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "args <name> [-- <args...>]",
		Short: "Get or set an agent's default passthrough args",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if _, ok := agent.Lookup(name); !ok {
				return fmt.Errorf("%w: unknown agent %q (see `atm agents list`)", ErrUsage, name)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			// Only the name -> print current args. Extra tokens -> set them.
			if len(args) == 1 {
				cfg, err := s.GetAgentsConfig()
				if err != nil {
					return err
				}
				cur := cfg.Args[name]
				return st.emit(st.stdout(), map[string]any{"name": name, "args": cur}, func() {
					fmt.Fprintln(st.stdout(), strings.Join(cur, " "))
				})
			}
			set := args[1:]
			if err := s.SetAgentArgs(name, set, "admin@cli:unset"); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"name": name, "args": set}, func() {
				fmt.Fprintf(st.stdout(), "set args for %s: %s\n", name, strings.Join(set, " "))
			})
		},
	}
}
