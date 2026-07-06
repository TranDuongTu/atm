package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"atm/internal/manager"
	"atm/internal/store"

	"github.com/spf13/cobra"
)

type managerOpts struct {
	Project     string
	Actor       string
	Integration string
	DryRun      bool
	ExtraArgs   []string
}

func newManagerCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manager",
		Short: "Launch an ATM manager session or render manager context",
	}
	cmd.AddCommand(newManagerPluginCmd(st))
	cmd.AddCommand(newManagerRenderContextCmd(st))
	for _, name := range []string{"opencode", "codex", "claude"} {
		cmd.AddCommand(newManagerAgentCmd(st, name))
	}
	cmd.AddCommand(newManagerOllamaCmd(st))
	return cmd
}

func newManagerPluginCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage ATM manager subagent definitions",
	}
	cmd.AddCommand(newManagerPluginStatusCmd(st))
	cmd.AddCommand(newManagerPluginInstallCmd(st))
	return cmd
}

func newManagerPluginStatusCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "status [all|opencode|codex|claude]",
		Short: "Show ATM manager plugin install status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "all"
			if len(args) > 0 {
				target = args[0]
			}
			agents, err := managerPluginAgents(target)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			plugins := make([]manager.Status, 0, len(agents))
			for _, agent := range agents {
				plugins = append(plugins, manager.PluginStatus(agent, home))
			}
			return st.emit(st.stdout(), map[string]any{"plugins": plugins}, func() {
				for _, plugin := range plugins {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", plugin.Agent, plugin.State, plugin.Path)
				}
			})
		},
	}
}

func newManagerPluginInstallCmd(st *cliState) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install [all|opencode|codex|claude]",
		Short: "Install ATM manager subagent definitions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "all"
			if len(args) > 0 {
				target = args[0]
			}
			agents, err := managerPluginAgents(target)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			installed := make([]manager.InstallResult, 0, len(agents))
			for _, agent := range agents {
				res, err := manager.InstallPlugin(agent, home, dryRun)
				if err != nil {
					return err
				}
				installed = append(installed, res)
			}
			return st.emit(st.stdout(), map[string]any{"installed": installed}, func() {
				for _, res := range installed {
					mode := "installed"
					if res.DryRun {
						mode = "would install"
					}
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", res.Agent, mode, res.Path)
				}
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print files that would be written without modifying user config")
	return cmd
}

func managerPluginAgents(target string) ([]string, error) {
	all := []string{"opencode", "codex", "claude"}
	if target == "" || target == "all" {
		return all, nil
	}
	for _, agent := range all {
		if target == agent {
			return []string{agent}, nil
		}
	}
	return nil, fmt.Errorf("%w: unknown manager plugin agent %q", ErrUsage, target)
}

func newManagerAgentCmd(st *cliState, agent string) *cobra.Command {
	var opts managerOpts
	cmd := &cobra.Command{
		Use:   agent,
		Short: "Launch " + agent + " with ATM manager context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, ok := manager.LauncherFor(agent)
			if !ok {
				return fmt.Errorf("%w: unknown manager agent %q", ErrUsage, agent)
			}
			opts.ExtraArgs = args
			opts.Actor = defaultManagerActor(l.Name(), st, opts.Actor)
			opts.Integration = ""
			return runManager(st, l, agent, "", opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default <agent>-manager)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newManagerOllamaCmd(st *cliState) *cobra.Command {
	var opts managerOpts
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Launch ollama-backed agent with ATM manager context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ExtraArgs = args
			opts.Actor = defaultManagerActor("ollama", st, opts.Actor)
			l := manager.OllamaLauncher{Integration: opts.Integration}
			return runManager(st, l, "ollama", opts.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default ollama-manager)")
	cmd.Flags().StringVar(&opts.Integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}

func defaultManagerActor(agent string, st *cliState, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if st.flags.actor != "" {
		return st.flags.actor
	}
	return agent + "-manager"
}

func newManagerRenderContextCmd(st *cliState) *cobra.Command {
	var opts struct {
		Project string
		Actor   string
	}
	cmd := &cobra.Command{
		Use:   "render-context",
		Short: "Print the ATM manager system prompt to stdout",
		RunE: func(cmd *cobra.Command, args []string) error {
			data := manager.ContextData{
				Code:  opts.Project,
				Actor: opts.Actor,
				RunID: "RENDER",
			}
			if opts.Project != "" {
				data.ATMBin = atmBinPath()
			}
			rendered := manager.RenderContext(data)
			// Text mode: print raw markdown. JSON mode: wrap in an envelope.
			return st.emit(st.stdout(), map[string]any{"context": rendered}, func() {
				fmt.Fprint(st.stdout(), rendered)
			})
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project code (optional; when absent, placeholders are left for env-driven use)")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id (optional)")
	return cmd
}

func runManager(st *cliState, l manager.Launcher, agent, integration string, opts managerOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	p, err := s.GetProject(opts.Project)
	if err != nil {
		return fmt.Errorf("%w: project %s not found; create it first:\n  atm project create --code %s --name \"...\"",
			ErrNotFound, opts.Project, opts.Project)
	}

	if home, err := os.UserHomeDir(); err == nil {
		if ps := manager.PluginStatus(l.Name(), home); ps.State != "installed" {
			fmt.Fprintf(st.stderr(), "warning: manager plugin %s for %s; subagent dispatch unavailable until 'atm manager plugin install %s'\n", ps.State, l.Name(), l.Name())
		}
	}

	atmBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve atm binary: %w", err)
	}

	runID := newRunID(opts.Project)
	contextPath := filepath.Join(s.StorePath(), "manager", runID+".md")
	if err := os.MkdirAll(filepath.Dir(contextPath), 0o755); err != nil {
		return fmt.Errorf("create manager dir: %w", err)
	}

	rendered := manager.RenderContext(manager.ContextData{
		Code:      p.Code,
		Name:      p.Name,
		ATMBin:    atmBin,
		Actor:     opts.Actor,
		RunID:     runID,
		Timestamp: store.RFC3339UTC(time.Now().UTC()),
	})
	if err := os.WriteFile(contextPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	base := l.BuildArgv()
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
	envValues := managerEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "manager", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}
	if opts.DryRun {
		return nil
	}

	exitCode, runErr := runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, "manager", opts.Project, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

func managerEnvValues(project, atmBin, actor, runID, contextPath string) map[string]string {
	return map[string]string{
		"ATM_ROLE":         "manager",
		"ATM_PROJECT":      project,
		"ATM_BIN":          atmBin,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_CONTEXT_FILE": contextPath,
	}
}

func atmBinPath() string {
	bin, err := os.Executable()
	if err != nil {
		return "atm"
	}
	return bin
}
