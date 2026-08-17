package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"atm/internal/agent"

	"github.com/spf13/cobra"
)

func newAgentsCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Inspect and switch the host agent used by atm --persona",
	}
	cmd.AddCommand(newAgentsListCmd(st))
	cmd.AddCommand(newAgentsSelectCmd(st))
	cmd.AddCommand(newAgentsArgsCmd(st))
	cmd.AddCommand(newAgentsModelsCmd(st))
	return cmd
}

// execRun is the production RunFunc: it runs the lister and returns stdout.
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func newAgentsModelsCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "models <name>",
		Short: "List the models the selection's launcher can serve",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sel, err := agent.ParseSelectionKey(args[0])
			if err != nil {
				return fmt.Errorf("%w: %v (see `atm agents list`)", ErrUsage, err)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			models, err := agent.ListModels(ctx, sel, execRun)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"name": args[0], "models": models}, func() {
				for _, m := range models {
					fmt.Fprintln(st.stdout(), m)
				}
			})
		},
	}
}

// agentRow is one SELECTION KEY (agent × launcher), which is the shape scripts
// already consume from JSON mode. Model is the key's configured model; empty
// means the harness's own default, which ATM does not know.
type agentRow struct {
	Name     string `json:"name"`
	Launcher string `json:"launcher"`
	Launch   string `json:"launch"`
	Status   string `json:"status"`
	Model    string `json:"model,omitempty"`
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
			ready := make(map[string]agent.Readiness, len(agent.Catalog()))
			for _, e := range agent.Catalog() {
				sel := e.Selection()
				r := agent.Status(e, home, exec.LookPath)
				ready[e.Name] = r
				rows = append(rows, agentRow{
					Name:     e.Name,
					Launcher: string(sel.Launcher),
					Launch:   strings.Join(e.Base(), " "),
					Status:   r.String(),
					Model:    cfg.Models[e.Name],
					Args:     strings.Join(cfg.Args[e.Name], " "),
					Selected: cfg.Selected == e.Name,
				})
			}
			return st.emit(st.stdout(), map[string]any{"agents": rows}, func() {
				writeAgentTable(st.stdout(), rows, ready)
			})
		},
	}
}

// writeAgentTable renders rows as agents × launchers: one line per harness,
// one column per launcher. The selection is a PAIR, so the `*` marker sits on
// a cell rather than a line, and each cell carries that key's model. Whether
// the ollama binary is installed is ONE global fact, printed once at the foot
// instead of repeated down an OLLAMA column.
func writeAgentTable(w io.Writer, rows []agentRow, ready map[string]agent.Readiness) {
	byKey := make(map[string]agentRow, len(rows))
	for _, r := range rows {
		byKey[r.Name] = r
	}
	const format = "%-10s  %-8s  %-24s  %-24s  %s"
	line := func(cells ...any) {
		fmt.Fprintln(w, strings.TrimRight(fmt.Sprintf(format, cells...), " "))
	}
	line("AGENT", "PLUGIN", "NATIVE", "OLLAMA", "ARGS")
	var ollamaBin bool
	for _, h := range agent.Harnesses() {
		nativeKey, ollamaKey := h.Name, "ollama:"+h.Name
		ollamaBin = !ready[ollamaKey].MissingBin
		plugin := "ok"
		if ready[nativeKey].MissingPlugin {
			plugin = "missing"
		}
		note := ""
		if ready[nativeKey].MissingBin {
			note = "no binary"
		}
		line(h.Name, plugin,
			agentCell(byKey[nativeKey], note),
			agentCell(byKey[ollamaKey], ""),
			strings.TrimSpace(byKey[nativeKey].Args+" "+byKey[ollamaKey].Args))
	}
	binState := "not installed"
	if ollamaBin {
		binState = "installed"
	}
	fmt.Fprintf(w, "\nollama binary: %s\n", binState)
	fmt.Fprintln(w, "* = selected;  - = the harness's own default model")
}

// agentCell renders one (agent, launcher) cell: the selection marker, the
// key's model, and any note about that cell's own readiness.
func agentCell(r agentRow, note string) string {
	cell := r.Model
	if cell == "" {
		cell = "-"
	}
	if r.Selected {
		cell = "* " + cell
	}
	if note != "" {
		cell += "  (" + note + ")"
	}
	return cell
}

func newAgentsSelectCmd(st *cliState) *cobra.Command {
	var model string
	cmd := &cobra.Command{
		Use:   "select <name> [--model M]",
		Short: "Set the default agent for atm --persona",
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
			// Changed, not model != "": `--model ""` is an instruction to
			// clear the model, not a silent no-op.
			if cmd.Flags().Changed("model") {
				if err := s.SetAgentModel(name, model, "admin@cli:unset"); err != nil {
					return err
				}
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			r := agent.Status(e, home, exec.LookPath)
			if !r.Ready() {
				fmt.Fprintf(st.stderr(), "warning: %s is not ready (%s)\n", name, r.String())
			}
			out := map[string]any{"selected": name}
			if cmd.Flags().Changed("model") {
				out["model"] = model
			}
			return st.emit(st.stdout(), out, func() {
				fmt.Fprintf(st.stdout(), "selected %s\n", name)
				if cmd.Flags().Changed("model") {
					if model == "" {
						fmt.Fprintf(st.stdout(), "model cleared (the harness picks its own default)\n")
					} else {
						fmt.Fprintf(st.stdout(), "model %s\n", model)
					}
				}
			})
		},
	}
	cmd.Flags().StringVar(&model, "model", "", "model for this selection (empty = the harness's own default)")
	return cmd
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
