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
	Integration string
	Persona     string
	Planning    bool
	Grooming    bool
	Tracking    bool
	Asking      bool
	Glossary    bool
	Onboarding  bool
	ExtraArgs   []string
}

type managerAction string

const (
	managerActionPlanning   managerAction = "planning"
	managerActionGrooming   managerAction = "grooming"
	managerActionTracking   managerAction = "tracking"
	managerActionAsking     managerAction = "asking"
	managerActionGlossary   managerAction = "glossary"
	managerActionOnboarding managerAction = "onboarding"
)

func newManageCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Launch an ATM manager session",
	}
	for _, name := range []string{"opencode", "codex", "claude"} {
		cmd.AddCommand(newManageAgentCmd(st, name))
	}
	cmd.AddCommand(newManageOllamaCmd(st))
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

func newManageAgentCmd(st *cliState, agent string) *cobra.Command {
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
			opts.Integration = ""
			return runManager(st, l, agent, "", opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; defaults actor to <persona>@<agent>:unset")
	bindManagerActionFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newManageOllamaCmd(st *cliState) *cobra.Command {
	var opts managerOpts
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Launch ollama-backed agent with ATM manager context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ExtraArgs = args
			l := manager.OllamaLauncher{Integration: opts.Integration}
			return runManager(st, l, "ollama", opts.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; defaults actor to <persona>@ollama:unset")
	bindManagerActionFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}

func bindManagerActionFlags(cmd *cobra.Command, opts *managerOpts) {
	cmd.Flags().BoolVar(&opts.Planning, "planning", false, "review backlog readiness, blocked work, and in-flight work")
	cmd.Flags().BoolVar(&opts.Grooming, "grooming", false, "prioritize and shape the backlog")
	cmd.Flags().BoolVar(&opts.Tracking, "tracking", false, "curate progress, decisions, questions, and handoffs")
	cmd.Flags().BoolVar(&opts.Asking, "asking", false, "answer project questions grounded in ledger IDs")
	cmd.Flags().BoolVar(&opts.Glossary, "glossary", false, "maintain shared project language")
	cmd.Flags().BoolVar(&opts.Onboarding, "onboarding", false, "learn a repo/project and organize it for later agents")
}

func validateManagerAction(opts managerOpts) (managerAction, error) {
	selected := []managerAction{}
	if opts.Planning {
		selected = append(selected, managerActionPlanning)
	}
	if opts.Grooming {
		selected = append(selected, managerActionGrooming)
	}
	if opts.Tracking {
		selected = append(selected, managerActionTracking)
	}
	if opts.Asking {
		selected = append(selected, managerActionAsking)
	}
	if opts.Glossary {
		selected = append(selected, managerActionGlossary)
	}
	if opts.Onboarding {
		selected = append(selected, managerActionOnboarding)
	}
	if len(selected) != 1 {
		return "", fmt.Errorf("%w: choose exactly one manager action: --planning, --grooming, --tracking, --asking, --glossary, or --onboarding", ErrUsage)
	}
	return selected[0], nil
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
				data.ATMBin = atmBinPath()
				data.Name = opts.Project // fallback when the project isn't in the store
				if s, err := st.openStore(); err == nil {
					if p, err := s.GetProject(opts.Project); err == nil {
						data.Name = p.Name
					}
				}
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

	action, err := validateManagerAction(opts)
	if err != nil {
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
		Code:               p.Code,
		Name:               p.Name,
		ATMBin:             atmBin,
		Actor:              actor,
		RunID:              runID,
		Timestamp:          store.RFC3339UTC(time.Now().UTC()),
		Persona:            effectivePersona,
		PersonaPrompt:      mp.Prompt,
		PersonaDescription: mp.Description,
		Action:             string(action),
	})
	if err := os.WriteFile(contextPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	var base []string
	onboarding := action == managerActionOnboarding
	if onboarding {
		base = l.BuildArgvOnboard(contextPath)
	} else {
		base = l.BuildArgv()
	}
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
	envValues := managerEnvValues(opts.Project, atmBin, actor, runID, contextPath, onboarding, effectivePersona, string(action))
	env := assembleEnv(envValues)
	if onboarding {
		setTmuxWindowLabel(os.Stdout, tmuxLabelOnboarding)
	}
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

func managerEnvValues(project, atmBin, actor, runID, contextPath string, onboard bool, persona string, action string) map[string]string {
	m := map[string]string{
		"ATM_ROLE":           "manager",
		"ATM_PROJECT":        project,
		"ATM_BIN":            atmBin,
		"ATM_ACTOR":          actor,
		"ATM_RUN_ID":         runID,
		"ATM_CONTEXT_FILE":   contextPath,
		"ATM_PERSONA":        persona,
		"ATM_MANAGER_ACTION": action,
	}
	if onboard {
		m["ATM_ONBOARD"] = "1"
	}
	return m
}

func atmBinPath() string {
	bin, err := os.Executable()
	if err != nil {
		return "atm"
	}
	return bin
}
