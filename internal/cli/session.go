package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"atm/internal/agent"
	"atm/internal/capability"
	"atm/internal/compose"
	"atm/internal/core"
	"atm/internal/session"

	"github.com/spf13/cobra"
)

// sessionOpts carries a single launch's resolved flags. The root command
// populates it from its --persona / --project / --capability /
// --agent flags and the positional args (passthrough after cobra parsing);
// session-context fills a subset directly.
type sessionOpts struct {
	Persona     string
	Project     string
	Capability  string
	Agent       string
	Task        string
	Checklists  []string // explicit --checklist selection; nil = computed default
	Launch      string   // --launch override; "" = persona default
	Integration string
	DefaultArgs []string
	ExtraArgs   []string
}

// sessionLauncherFor maps a catalog entry to the unified session launcher.
// Ollama launches carry their integration; static launchers come from
// session.LauncherFor. Returns ok=false for unknown launchers.
func sessionLauncherFor(e agent.Entry) (session.Launcher, bool) {
	if e.Launcher == "ollama" {
		return session.OllamaLauncher{Integration: e.Integration}, true
	}
	return session.LauncherFor(e.Launcher)
}

// composeFor builds the Compose service over an opened store, injecting the
// registry-derived views so internal/compose stays registry-free.
func (st *cliState) composeFor(s core.Service) *compose.Service {
	return &compose.Service{
		Svc:                 s,
		EnabledCapabilities: func(code string) []string { return narrowedRegistry(st, s, code).Names() },
		CapabilitiesBlock:   func(code string) string { return composeCapabilitiesBlock(narrowedRegistry(st, s, code)) },
		ExpectedChecklists:  func(code string) []string { return narrowedRegistry(st, s, code).ChecklistSeedNames() },
	}
}

// validateCapabilityScope checks the optional --capability against the full
// registry first (typo → registered list), then the project's enabled set
// (known but disabled → how to enable it). Empty capability means "all
// enabled" and is always valid.
func validateCapabilityScope(capabilityName string, enabled, registered []string) error {
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

// launchSession binds the session via the Compose service, writes the
// rendered context to its cache file, emits the launch header, execs the
// host agent, and emits the tail. It is the single launch path for every
// persona (developer/manager/custom); launch:tui personas route to the
// interactive TUI instead — data-driven, no name checks.
func (st *cliState) launchSession(opts sessionOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	csvc := st.composeFor(s)
	persona, err := csvc.ResolvePersona(opts.Project, opts.Persona)
	if err != nil {
		return err
	}
	// Routing reads the EFFECTIVE launch mode — the --launch override when
	// set, else the persona's default — validated before any route so a bad
	// value fails even for tui personas.
	if opts.Launch != "" && opts.Launch != "prompt" && opts.Launch != "hook" && opts.Launch != "tui" {
		return fmt.Errorf("%w: --launch must be prompt, hook, or tui, got %q", ErrUsage, opts.Launch)
	}
	mode := compose.LaunchModeOf(persona)
	if opts.Launch != "" {
		mode = opts.Launch
	}
	if mode == "tui" {
		// The TUI ignores --project/--agent/--task/--capability, exactly as
		// the former admin route did; positional args still reject.
		if len(opts.ExtraArgs) > 0 {
			return fmt.Errorf("%w: unknown command %q", ErrUsage, opts.ExtraArgs[0])
		}
		return st.launchTUI()
	}
	cfg, err := s.GetAgentsConfig()
	if err != nil {
		return err
	}
	e, defArgs, err := resolveEntry(opts.Agent, cfg)
	if err != nil {
		return err
	}
	l, ok := sessionLauncherFor(e)
	if !ok {
		return fmt.Errorf("%w: unknown agent %q", ErrUsage, e.Launcher)
	}

	var code, projName string
	if opts.Project == "" {
		if !persona.ProjectOptional {
			return fmt.Errorf("%w: --project is required for persona %q", ErrUsage, persona.Name)
		}
	} else {
		p, err := ensureProjectForLaunch(s, opts.Project)
		if err != nil {
			return err
		}
		code, projName = p.Code, p.Name
	}

	if opts.Task != "" {
		if code == "" {
			return fmt.Errorf("%w: --task requires --project", ErrUsage)
		}
		t, err := s.GetTask(opts.Task)
		if err != nil {
			return err
		}
		if t.ProjectCode != code {
			return fmt.Errorf("%w: task %s belongs to project %s, not %s", ErrUsage, t.ID, t.ProjectCode, code)
		}
		opts.Task = t.ID
	}

	// Validate --capability against the project's enabled set AFTER the project
	// is resolved: st.registry may be un-narrowed when the project was just
	// auto-created by ensureProjectForLaunch (mountRegistry degraded to the
	// full registry). Recompute enabled from the resolved project so the
	// "not enabled for project" branch is reachable on that path.
	enabled := narrowedRegistry(st, s, code).Names()
	if err := validateCapabilityScope(opts.Capability, enabled, st.fullRegistry.Names()); err != nil {
		return err
	}

	if _, err := st.lookPath("atm"); err != nil {
		return fmt.Errorf("%w: atm is not on PATH; the session prompt assumes `atm` resolves on PATH. Either add the directory containing the `atm` binary to PATH, or invoke atm from a shell where it resolves.", ErrUsage)
	}

	sel := e.Selection()
	sel.Model = cfg.Models[sel.Key()]
	runCode := code
	if runCode == "" {
		runCode = "atm"
	}
	plan, err := csvc.Compose(compose.Request{
		Persona:     persona.Name,
		Code:        code,
		ProjName:    projName,
		Task:        opts.Task,
		Capability:  opts.Capability,
		Checklists:  opts.Checklists,
		Launch:      opts.Launch,
		Launcher:    l,
		Model:       sel.Model,
		DefaultArgs: defArgs,
		EnvArgs:     agentEnvArgs(e.Launcher, e.Integration),
		ExtraArgs:   opts.ExtraArgs,
		RunID:       newRunID(runCode),
		Timestamp:   core.RFC3339UTC(time.Now().UTC()),
	})
	if err != nil {
		return err
	}
	if err := writeContextIfDiff(plan.ContextPath, []byte(plan.ContextText)); err != nil {
		return fmt.Errorf("write context file %s: %w", plan.ContextPath, err)
	}
	for _, w := range plan.Warnings {
		fmt.Fprintln(st.stderr(), "warning: "+w)
	}

	env := assembleEnv(plan.EnvValues)
	runID := plan.EnvValues["ATM_RUN_ID"]
	if err := emitLaunchHeader(st, persona.Name, code, runID, plan.ContextPath, l.Name(), plan.Argv, plan.EnvValues); err != nil {
		return err
	}

	exitCode, runErr := st.runChild(l.Name(), plan.Argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, persona.Name, code, runID, plan.ContextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

// newSessionContextCmd renders a persona's session prompt to stdout. Hidden
// plumbing: thin-pointer subagent plugins call it at dispatch to render the
// prompt without launching a host agent.
func newSessionContextCmd(st *cliState) *cobra.Command {
	var opts struct {
		Persona    string
		Project    string
		Actor      string
		Capability string
		Task       string
	}
	cmd := &cobra.Command{
		Use:    "session-context",
		Short:  "Print a persona's rendered session prompt to stdout",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderSessionContext(st, opts.Persona, opts.Project, opts.Actor, opts.Capability, opts.Task)
		},
	}
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name")
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project code (optional; when absent, placeholders are left for env-driven use)")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id (optional)")
	cmd.Flags().StringVar(&opts.Capability, "capability", "", "scope to one capability")
	cmd.Flags().StringVar(&opts.Task, "task", "", "assign the session a task (rendered into the prompt; not validated here)")
	_ = cmd.MarkFlagRequired("persona")
	return cmd
}

// newManageContextCmd is the legacy alias installed thin-pointer manager
// plugins call; it renders the manager persona's prompt. Hidden.
func newManageContextCmd(st *cliState) *cobra.Command {
	var opts struct {
		Project string
		Actor   string
	}
	cmd := &cobra.Command{
		Use:    "manage-context",
		Short:  "Print the ATM manager system prompt to stdout (alias of session-context --persona manager)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderSessionContext(st, "manager", opts.Project, opts.Actor, "", "")
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project code")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id")
	return cmd
}

// renderSessionContext is the shared render path for `session-context` and the
// `manage-context` alias: a context-only Compose (no launcher, no argv) with
// the same default checklist set a real launch would carry.
func renderSessionContext(st *cliState, persona, project, actor, capability, task string) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	var projName string
	if project != "" {
		projName = project
		if p, err := s.GetProject(project); err == nil {
			projName = p.Name
		}
	}
	plan, err := st.composeFor(s).Compose(compose.Request{
		Persona:    persona,
		Code:       project,
		ProjName:   projName,
		Task:       task,
		Capability: capability,
		Actor:      actor,
	})
	if err != nil {
		return err
	}
	return st.emit(st.stdout(), map[string]any{"persona": persona, "context": plan.ContextText}, func() {
		fmt.Fprint(st.stdout(), plan.ContextText)
	})
}

// narrowedRegistry resolves the registry narrowed to the project's enabled
// set, falling back to st.registry when the project cannot be read (the same
// recompute launchSession already does for --capability validation).
func narrowedRegistry(st *cliState, s core.Service, code string) *capability.Registry {
	if code != "" && st.fullRegistry != nil {
		if p, err := s.GetProject(code); err == nil {
			return st.fullRegistry.For(p)
		}
	}
	return st.registry
}

// composeCapabilitiesBlock renders the ## Capabilities section from the
// project's enabled capabilities. Every word about a capability comes from
// its own markdown (brief, falling back to description) — the template and
// this function stay capability-name-free.
func composeCapabilitiesBlock(reg *capability.Registry) string {
	descs := reg.Describe()
	if len(descs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Capabilities\n\nThis project has these capabilities enabled. Each line is that capability's own brief — run `atm capability <name> guide` before relying on one.\n\n")
	for _, d := range descs {
		fmt.Fprintf(&b, "- **%s** — %s\n", d.Name, d.Brief)
	}
	return strings.TrimRight(b.String(), "\n")
}
