package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"atm/internal/developing"
	"atm/internal/store"

	"github.com/spf13/cobra"
)

type developingOpts struct {
	Project     string
	Actor       string
	Integration string
	Persona     string
	DryRun      bool
	ExtraArgs   []string
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
	cmd.AddCommand(newDevelopingOllamaCmd(st))
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
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, ok := developing.LauncherFor(agent)
			if !ok {
				return fmt.Errorf("%w: unknown developing agent %q", ErrUsage, agent)
			}
			opts.ExtraArgs = args
			opts.Integration = ""
			return runDeveloping(st, l, agent, "", opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default <agent>-dev)")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; injects its prompt and defaults actor to <persona>@<agent>:unset")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newDevelopingOllamaCmd(st *cliState) *cobra.Command {
	var opts developingOpts
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Launch ollama-backed agent with ATM developing context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ExtraArgs = args
			l := developing.OllamaLauncher{Integration: opts.Integration}
			return runDeveloping(st, l, "ollama", opts.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into ATM commands (default <persona>@<agent>:unset)")
	cmd.Flags().StringVar(&opts.Integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; injects its prompt and defaults actor to <persona>@<agent>:unset")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render context + print argv/env; do not launch")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}

func runDeveloping(st *cliState, l developing.Launcher, agent, integration string, opts developingOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	p, err := s.GetProject(opts.Project)
	if err != nil {
		return fmt.Errorf("%w: project %s not found; create it first:\n  atm project create --code %s --name \"...\"",
			ErrNotFound, opts.Project, opts.Project)
	}

	effectivePersona := opts.Persona
	if effectivePersona == "" {
		effectivePersona = "developer"
	}
	pp, err := s.GetPersona(effectivePersona)
	if err != nil {
		return err // unregistered --persona fails fast
	}
	personaPrompt := pp.Prompt
	if opts.Actor == "" {
		opts.Actor = effectivePersona + "@" + l.Name() + ":unset"
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
		Code:          p.Code,
		Name:          p.Name,
		ATMBin:        atmBin,
		Actor:         opts.Actor,
		RunID:         runID,
		Timestamp:     store.RFC3339UTC(time.Now().UTC()),
		Persona:       effectivePersona,
		PersonaPrompt: personaPrompt,
	})
	if err := os.WriteFile(contextPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	base := l.BuildArgv()
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
	envValues := developingEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath, l.Name(), opts.Persona)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "developing", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}
	if opts.DryRun {
		return nil
	}

	exitCode, runErr := runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, "developing", opts.Project, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

func developingEnvValues(project, atmBin, actor, runID, contextPath, agent, persona string) map[string]string {
	m := map[string]string{
		"ATM_ROLE":         "developing",
		"ATM_PROJECT":      project,
		"ATM_BIN":          atmBin,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_CONTEXT_FILE": contextPath,
		"ATM_AGENT":        agent,
	}
	if persona != "" {
		m["ATM_PERSONA"] = persona
	}
	return m
}
