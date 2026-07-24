package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"atm/internal/agent"
	"atm/internal/core"
	"atm/internal/session"
	"atm/skills"

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

// resolvePersonaSpec resolves a persona name to its spec: built-ins come from
// the skills package, custom personas are parsed from their stored markdown
// document. A custom persona that fails to parse is a usage error (the store
// accepted the markdown; the prompt format is what makes it a persona).
func resolvePersonaSpec(s core.Service, name string) (skills.PersonaSpec, error) {
	if spec, ok := skills.Persona(name); ok {
		return spec, nil
	}
	doc, err := s.PersonaDoc(name)
	if err != nil {
		return skills.PersonaSpec{}, err
	}
	spec, err := skills.ParsePersona(name, []byte(doc))
	if err != nil {
		return skills.PersonaSpec{}, fmt.Errorf("%w: stored persona %q: %v", ErrUsage, name, err)
	}
	return spec, nil
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

// launchSession renders the persona's session prompt, writes it to the cache
// file, emits the launch header, execs the host agent, and emits the tail.
// It is the single launch path for every persona (developer/manager/custom).
func (st *cliState) launchSession(opts sessionOpts) error {
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
	l, ok := sessionLauncherFor(e)
	if !ok {
		return fmt.Errorf("%w: unknown agent %q", ErrUsage, e.Launcher)
	}
	opts.Integration = e.Integration
	opts.DefaultArgs = defArgs

	spec, err := resolvePersonaSpec(s, opts.Persona)
	if err != nil {
		return err
	}

	var code, projName string
	if opts.Project == "" {
		if !spec.ProjectOptional {
			return fmt.Errorf("%w: --project is required for persona %q", ErrUsage, spec.Name)
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
	enabled := st.registry.Names()
	if opts.Project != "" && st.fullRegistry != nil {
		if p, err := s.GetProject(code); err == nil {
			enabled = st.fullRegistry.For(p).Names()
		}
	}
	if err := validateCapabilityScope(opts.Capability, enabled, st.fullRegistry.Names()); err != nil {
		return err
	}

	actor := spec.Name + "@" + l.Name() + ":unset"

	if _, err := st.lookPath("atm"); err != nil {
		return fmt.Errorf("%w: atm is not on PATH; the session prompt assumes `atm` resolves on PATH. Either add the directory containing the `atm` binary to PATH, or invoke atm from a shell where it resolves.", ErrUsage)
	}

	personality, err := s.GetPersonality(spec.Name)
	if err != nil {
		return err
	}
	personaPrompt := buildPersonaPrompt(spec, personality, code, projName, actor, opts.Task)

	now := time.Now().UTC()
	runCode := code
	if runCode == "" {
		runCode = "atm"
	}
	runID := newRunID(runCode)
	timestamp := core.RFC3339UTC(now)
	contextPath := contextCachePath(s.StorePath(), code, spec.Name, opts.Task)

	rendered := session.RenderContext(session.ContextData{
		Code: code, Name: projName, Actor: actor,
		TaskID:        opts.Task,
		PersonaPrompt: personaPrompt,
	})
	if err := writeContextIfDiff(contextPath, []byte(rendered)); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	var base []string
	role := spec.Name
	if spec.Launch == "hook" {
		base = l.BuildArgv()
		role = "developing"
	} else {
		base = l.BuildArgvPrompt(contextPath)
	}
	envArgs := agentEnvArgs(e.Launcher, e.Integration)
	argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
	envValues := sessionEnvValues(code, actor, runID, contextPath, l.Name(), spec.Name, role, opts.Capability, opts.Task, timestamp)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, spec.Name, code, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}

	exitCode, runErr := st.runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, spec.Name, code, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

// sessionEnvValues builds the env map handed to the host agent. ATM_ROLE is
// "developing" for launch:hook personas (back-compat with installed
// session-start hooks that gate on it) and the persona name otherwise.
// ATM_CAPABILITY is omitted when empty (the host agent can distinguish
// "no capability" from "named capability").
func sessionEnvValues(project, actor, runID, contextPath, agentName, persona, role, capability, task, timestamp string) map[string]string {
	m := map[string]string{
		"ATM_ROLE":         role,
		"ATM_PROJECT":      project,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_TIMESTAMP":    timestamp,
		"ATM_CONTEXT_FILE": contextPath,
		"ATM_AGENT":        agentName,
		"ATM_PERSONA":      persona,
	}
	if capability != "" {
		m["ATM_CAPABILITY"] = capability
	}
	if task != "" {
		m["ATM_TASK"] = task
	}
	return m
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
// `manage-context` alias: resolve the persona, look up the project name (or
// leave the placeholder), and emit the rendered context wrapper.
func renderSessionContext(st *cliState, persona, project, actor, capability, task string) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	spec, err := resolvePersonaSpec(s, persona)
	if err != nil {
		return err
	}
	personality, err := s.GetPersonality(spec.Name)
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
	data := session.ContextData{
		Code: project, Name: projName, Actor: actor,
		TaskID:        task,
		PersonaPrompt: buildPersonaPrompt(spec, personality, project, projName, actor, task),
	}
	rendered := session.RenderContext(data)
	return st.emit(st.stdout(), map[string]any{"persona": spec.Name, "context": rendered}, func() {
		fmt.Fprint(st.stdout(), rendered)
	})
}

// buildPersonaPrompt renders a persona's prompt text with context params
// substituted. The result is injected into the session context template.
func buildPersonaPrompt(spec skills.PersonaSpec, personality, code, name, actor, taskID string) string {
	sub := func(s string) string {
		r := strings.NewReplacer(
			"<CODE>", code,
			"<PROJECT_NAME>", name,
			"<ACTOR>", actor,
			"<TASK_ID>", taskID,
		)
		return r.Replace(s)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Persona: %s\n\n%s\n\n", spec.Name, spec.Description)
	b.WriteString(sub(spec.CorePrompt))
	if personality == "" {
		personality = spec.Personality
	}
	if personality != "" {
		fmt.Fprintf(&b, "\n### Personality\n\n%s\n", sub(personality))
	}
	b.WriteString("\nYou are operating as this persona. Hold to its principles throughout the session, alongside repo instructions and the working routine below.\n")
	return b.String()
}
