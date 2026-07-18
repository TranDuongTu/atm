package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"atm/internal/capability"
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
	Curate      bool
	Recall      bool
	Mapping     bool
	Onboarding  bool
	ExtraArgs   []string
}

// managerCoreActions are the manager's irreducible substrate duties. They
// exist for every project; capability actions come from the registry.
var managerCoreActions = []string{"curate", "recall"}

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
	cmd.Flags().StringVar(&opts.Action, "action", "", "manager action: curate, recall, or a capability-contributed action (see `atm manage --project <CODE> --help`)")
	cmd.Flags().BoolVar(&opts.Curate, "curate", false, "review backlog, triage, track handoffs, and maintain vocabulary (default)")
	cmd.Flags().BoolVar(&opts.Recall, "recall", false, "read-only synthesis grounded in ledger IDs; does not mutate")

	// Deprecated aliases (never hard-break a flag on a stable CLI surface,
	// ATM-0113): --mapping predates capability-contributed actions;
	// --onboarding predates --mapping.
	cmd.Flags().BoolVar(&opts.Mapping, "mapping", false, "")
	_ = cmd.Flags().MarkDeprecated("mapping", "use --action mapping")
	_ = cmd.Flags().MarkHidden("mapping")
	cmd.Flags().BoolVar(&opts.Onboarding, "onboarding", false, "")
	_ = cmd.Flags().MarkDeprecated("onboarding", "use --action mapping")
	_ = cmd.Flags().MarkHidden("onboarding")
}

// validateManagerAction resolves the session's action: the core duties
// (curate, recall) always exist; capability actions must be contributed by
// an enabled capability (`available` comes from the mount-narrowed
// registry, so a disabled capability's action is simply absent).
func validateManagerAction(opts managerOpts, available []capability.ManagerAction) (string, *capability.ManagerAction, error) {
	var selected []string
	if opts.Curate {
		selected = append(selected, "curate")
	}
	if opts.Recall {
		selected = append(selected, "recall")
	}
	if opts.Mapping || opts.Onboarding {
		selected = append(selected, "mapping")
	}
	if opts.Action != "" {
		selected = append(selected, opts.Action)
	}
	if len(selected) > 1 {
		return "", nil, fmt.Errorf("%w: choose one manager action", ErrUsage)
	}
	if len(selected) == 0 {
		return "curate", nil, nil
	}
	name := selected[0]
	for _, core := range managerCoreActions {
		if name == core {
			return name, nil, nil
		}
	}
	for i := range available {
		if available[i].Name == name {
			return name, &available[i], nil
		}
	}
	names := append([]string{}, managerCoreActions...)
	for _, a := range available {
		names = append(names, a.Name)
	}
	return "", nil, fmt.Errorf("%w: unknown manager action %q (available: %s)", ErrUsage, name, strings.Join(names, ", "))
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
			available := st.registry.ManagerActions(st)
			capActions := make([]manager.CapabilityAction, 0, len(available))
			for _, a := range available {
				capActions = append(capActions, manager.CapabilityAction{Name: a.Name, Summary: a.Summary, Command: a.Command})
			}
			data := manager.ContextData{
				Code:              opts.Project,
				Actor:             opts.Actor,
				CapabilityActions: capActions,
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
	p, err := ensureProjectForLaunch(s, opts.Project)
	if err != nil {
		return err
	}

	available := st.registry.ManagerActions(st)
	action, entry, err := validateManagerAction(opts, available)
	if err != nil {
		return err
	}
	capActions := make([]manager.CapabilityAction, 0, len(available))
	for _, a := range available {
		capActions = append(capActions, manager.CapabilityAction{Name: a.Name, Summary: a.Summary, Command: a.Command})
	}
	consult := ""
	if entry != nil {
		consult = entry.Command
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
		Timestamp:          core.RFC3339UTC(time.Now().UTC()),
		Persona:            effectivePersona,
		PersonaPrompt:      mp.Prompt,
		PersonaDescription: mp.Description,
		Action:             action,
		CapabilityActions:  capActions,
		ActionConsult:      consult,
	})
	if err := os.WriteFile(contextPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	var base []string
	// The onboard argv flavor is a launcher nuance historically tied to the
	// mapping action; not generalized to other capability actions (YAGNI).
	onboarding := action == "mapping"
	if onboarding {
		base = l.BuildArgvOnboard(contextPath)
	} else {
		base = l.BuildArgvManage(contextPath)
	}
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
	envValues := managerEnvValues(opts.Project, atmBin, actor, runID, contextPath, onboarding, effectivePersona, action)
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
