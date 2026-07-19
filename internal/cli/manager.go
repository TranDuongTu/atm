package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"atm/internal/core"
	"atm/internal/manager"

	"github.com/spf13/cobra"
)

type managerOpts struct {
	Project     string
	Integration string
	Persona     string
	Agent       string
	DefaultArgs []string
	Action      string
	Capability  string
	ExtraArgs   []string
}

func newManageCmd(st *cliState) *cobra.Command {
	var opts managerOpts
	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Launch the selected agent with ATM manager context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cfg, err := s.GetAgentsConfig()
			if err != nil {
				return err
			}
			e, defArgs, err := resolveEntry(opts.Agent, cfg)
			if err != nil {
				return err
			}
			l, ok := manageLauncherFor(e)
			if !ok {
				return fmt.Errorf("%w: unknown manager agent %q", ErrUsage, e.Launcher)
			}
			opts.ExtraArgs = args
			opts.Integration = e.Integration
			opts.DefaultArgs = defArgs
			return runManager(st, l, e.Launcher, e.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; defaults actor to <persona>@<agent>:unset")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "override the selected agent for this launch (see `atm agents list`)")
	bindManagerActionFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("project")
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

func bindManagerActionFlags(cmd *cobra.Command, opts *managerOpts) {
	cmd.Flags().StringVar(&opts.Action, "action", "autopilot", "manager action: brief (interview the human to set up each capability), autopilot (autonomously maintain each capability's territory), ask (read-only standby for questions)")
	cmd.Flags().StringVar(&opts.Capability, "capability", "", "scope the action to one enabled capability (default: all enabled)")
}

// validateManagerAction checks the semantic-agnostic action vocabulary and
// the optional capability scope. The scope is validated against the FULL
// registry first (typo -> registered list), then the enabled set (known but
// disabled -> how to enable it).
func validateManagerAction(action, capabilityName string, enabled, registered []string) error {
	switch action {
	case "brief", "autopilot", "ask":
	default:
		return fmt.Errorf("%w: unknown manager action %q (available: brief, autopilot, ask)", ErrUsage, action)
	}
	if capabilityName == "" {
		return nil
	}
	if !slices.Contains(registered, capabilityName) {
		return fmt.Errorf("%w: unknown capability %q (registered: %s)", ErrUsage, capabilityName, strings.Join(registered, ", "))
	}
	if !slices.Contains(enabled, capabilityName) {
		return fmt.Errorf("%w: capability %q is not enabled for project; run `atm project capability add --project <CODE> --name %s` first", ErrUsage, capabilityName, capabilityName)
	}
	return nil
}

func newManageContextCmd(st *cliState) *cobra.Command {
	var opts struct {
		Project string
		Actor   string
	}
	cmd := &cobra.Command{
		Use:    "manage-context",
		Short:  "Print the ATM manager system prompt to stdout",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data := manager.ContextData{
				Code:  opts.Project,
				Actor: opts.Actor,
			}
			if opts.Project != "" {
				data.Name = opts.Project // fallback when the project isn't in the store
				if s, err := st.openStore(); err == nil {
					if p, err := s.GetProject(opts.Project); err == nil {
						data.Name = p.Name
					}
				}
			}
			rendered := manager.RenderContext(data)
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
	p, err := ensureProjectForLaunch(s, opts.Project)
	if err != nil {
		return err
	}

	if err := validateManagerAction(opts.Action, opts.Capability, st.registry.Names(), st.fullRegistry.Names()); err != nil {
		return err
	}
	effectivePersona := opts.Persona
	if effectivePersona == "" {
		effectivePersona = "manager"
	}
	mp, err := s.GetPersona(effectivePersona)
	if err != nil {
		return err
	}
	actor := effectivePersona + "@" + l.Name() + ":unset"

	if _, err := st.lookPath("atm"); err != nil {
		return fmt.Errorf("%w: atm is not on PATH; the developing/manager prompt assumes `atm` resolves on PATH. Either add the directory containing the `atm` binary to PATH, or invoke atm from a shell where it resolves.", ErrUsage)
	}

	now := time.Now().UTC()
	runID := newRunID(opts.Project)
	timestamp := core.RFC3339UTC(now)
	contextPath := contextCachePath(s.StorePath(), p.Code, "manage", effectivePersona, opts.Action, opts.Capability)

	rendered := manager.RenderContext(manager.ContextData{
		Code:               p.Code,
		Name:               p.Name,
		Actor:              actor,
		Persona:            effectivePersona,
		PersonaPrompt:      mp.Prompt,
		PersonaDescription: mp.Description,
		Action:             opts.Action,
		Capability:         opts.Capability,
	})
	if err := writeContextIfDiff(contextPath, []byte(rendered)); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	base := l.BuildArgvManage(contextPath)
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
	envValues := managerEnvValues(opts.Project, actor, runID, contextPath, effectivePersona, opts.Action, opts.Capability, timestamp)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "manager", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}

	exitCode, runErr := st.runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, "manager", opts.Project, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

func managerEnvValues(project, actor, runID, contextPath, persona, action, capability, timestamp string) map[string]string {
	return map[string]string{
		"ATM_ROLE":               "manager",
		"ATM_PROJECT":            project,
		"ATM_ACTOR":              actor,
		"ATM_RUN_ID":             runID,
		"ATM_TIMESTAMP":          timestamp,
		"ATM_CONTEXT_FILE":       contextPath,
		"ATM_PERSONA":            persona,
		"ATM_MANAGER_ACTION":     action,
		"ATM_MANAGER_CAPABILITY": capability,
	}
}
