package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"atm/internal/developing"
	"atm/internal/store"

	"github.com/spf13/cobra"
)

type developingOpts struct {
	Project string
	Actor   string
	DryRun  bool
}

func newDevelopingCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "developing",
		Short: "Launch an agent with ATM developing context",
	}
	cmd.AddCommand(newDevelopingPluginCmd(st))
	for _, name := range []string{"opencode", "codex", "claude"} {
		cmd.AddCommand(newDevelopingAgentCmd(st, name))
	}
	return cmd
}

func newDevelopingPluginCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage ATM developing agent plugins",
	}
	cmd.AddCommand(newDevelopingPluginStatusCmd(st))
	cmd.AddCommand(newDevelopingPluginInstallCmd(st))
	return cmd
}

func newDevelopingPluginStatusCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "status [all|opencode|codex|claude]",
		Short: "Show ATM developing plugin install status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "all"
			if len(args) > 0 {
				target = args[0]
			}
			agents, err := developingPluginAgents(target)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			plugins := make([]developing.Status, 0, len(agents))
			for _, agent := range agents {
				plugins = append(plugins, developing.PluginStatus(agent, home))
			}
			return st.emit(st.stdout(), map[string]any{"plugins": plugins}, func() {
				for _, plugin := range plugins {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", plugin.Agent, plugin.State, plugin.Path)
				}
			})
		},
	}
}

func newDevelopingPluginInstallCmd(st *cliState) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install [all|opencode|codex|claude]",
		Short: "Install ATM developing plugin assets",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "all"
			if len(args) > 0 {
				target = args[0]
			}
			agents, err := developingPluginAgents(target)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			installed := make([]developing.InstallResult, 0, len(agents))
			for _, agent := range agents {
				res, err := developing.InstallPlugin(agent, home, dryRun)
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

func developingPluginAgents(target string) ([]string, error) {
	all := []string{"opencode", "codex", "claude"}
	if target == "" || target == "all" {
		return all, nil
	}
	for _, agent := range all {
		if target == agent {
			return []string{agent}, nil
		}
	}
	return nil, fmt.Errorf("%w: unknown developing plugin agent %q", ErrUsage, target)
}

func newDevelopingAgentCmd(st *cliState, agent string) *cobra.Command {
	var opts developingOpts
	cmd := &cobra.Command{
		Use:   agent,
		Short: "Launch " + agent + " with ATM developing context",
		RunE: func(cmd *cobra.Command, args []string) error {
			l, ok := developing.LauncherFor(agent)
			if !ok {
				return fmt.Errorf("%w: unknown developing agent %q", ErrUsage, agent)
			}
			opts.Actor = defaultDevelopingActor(l.Name(), st, opts.Actor)
			return runDeveloping(st, l, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default <agent>-dev)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func defaultDevelopingActor(agent string, st *cliState, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if st.flags.actor != "" {
		return st.flags.actor
	}
	return agent + "-dev"
}

func runDeveloping(st *cliState, l developing.Launcher, opts developingOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	p, err := s.GetProject(opts.Project)
	if err != nil {
		return fmt.Errorf("%w: project %s not found; create it first:\n  atm project create --code %s --name \"...\"",
			ErrNotFound, opts.Project, opts.Project)
	}

	atmBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve atm binary: %w", err)
	}

	runID := newRunID(opts.Project)
	contextPath := filepath.Join(s.StorePath(), "developing", runID+".md")
	if err := os.MkdirAll(filepath.Dir(contextPath), 0o755); err != nil {
		return fmt.Errorf("create developing dir: %w", err)
	}

	rendered := developing.RenderContext(developing.ContextData{
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

	argv := l.BuildArgv()
	envMap := developingEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath)
	env := developingEnv(opts.Project, atmBin, opts.Actor, runID, contextPath)
	if err := emitDevelopingHeader(st, l.Name(), opts.Project, runID, contextPath, argv, envMap); err != nil {
		return err
	}
	if opts.DryRun {
		return nil
	}

	exitCode, runErr := runDevelopingChild(l, argv, env)
	if err := emitDevelopingTail(st, l.Name(), opts.Project, runID, contextPath, exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

func developingEnv(project, atmBin, actor, runID, contextPath string) []string {
	env := os.Environ()
	for key, value := range developingEnvValues(project, atmBin, actor, runID, contextPath) {
		env = append(env, key+"="+value)
	}
	return env
}

func developingEnvValues(project, atmBin, actor, runID, contextPath string) map[string]string {
	return map[string]string{
		"ATM_ROLE":         "developing",
		"ATM_PROJECT":      project,
		"ATM_BIN":          atmBin,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_CONTEXT_FILE": contextPath,
	}
}

func runDevelopingChild(l developing.Launcher, argv []string, env []string) (int, error) {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return 0, fmt.Errorf("%s not found on PATH; install: %s", l.Name(), l.NotFoundHint())
	}
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = env
	err = cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), err
		}
		return 1, err
	}
	return 0, nil
}

func emitDevelopingHeader(st *cliState, launcherName, project, runID, contextPath string, argv []string, env map[string]string) error {
	return st.emit(st.stdout(), map[string]any{
		"run_id":       runID,
		"project":      project,
		"agent":        launcherName,
		"context_path": contextPath,
		"argv":         argv,
		"env":          env,
	}, func() {
		fmt.Fprintf(st.stdout(), "developing %s  run=%s  agent=%s\n", project, runID, launcherName)
		fmt.Fprintf(st.stdout(), "  context:  %s\n", contextPath)
		fmt.Fprintf(st.stdout(), "  launching: %s\n", strings.Join(argv, " "))
	})
}

func emitDevelopingTail(st *cliState, launcherName, project, runID, contextPath string, agentExit int) error {
	return st.emit(st.stdout(), map[string]any{
		"run_id":       runID,
		"project":      project,
		"agent":        launcherName,
		"context_path": contextPath,
		"agent_exit":   agentExit,
	}, func() {
		fmt.Fprintf(st.stdout(), "developing %s  run=%s\n", project, runID)
		fmt.Fprintf(st.stdout(), "  context: %s\n", contextPath)
		fmt.Fprintf(st.stdout(), "%s exited %d\n", launcherName, agentExit)
	})
}
