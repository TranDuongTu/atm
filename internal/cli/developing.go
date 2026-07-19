package cli

import (
	"fmt"
	"time"

	"atm/internal/core"
	"atm/internal/developing"

	"github.com/spf13/cobra"
)

type developingOpts struct {
	Project     string
	Integration string
	Persona     string
	Agent       string
	DefaultArgs []string
	ExtraArgs   []string
}

func newDevCmd(st *cliState) *cobra.Command {
	var opts developingOpts
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Launch the selected agent with ATM developer context",
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
			l, ok := devLauncherFor(e)
			if !ok {
				return fmt.Errorf("%w: unknown developer agent %q", ErrUsage, e.Launcher)
			}
			opts.ExtraArgs = args
			opts.Integration = e.Integration
			opts.DefaultArgs = defArgs
			return runDeveloping(st, l, e.Launcher, e.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; injects its prompt and defaults actor to <persona>@<agent>:unset")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "override the selected agent for this launch (see `atm agents list`)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func runDeveloping(st *cliState, l developing.Launcher, agent, integration string, opts developingOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	p, err := ensureProjectForLaunch(s, opts.Project)
	if err != nil {
		return err
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
	personaDescription := pp.Description
	actor := effectivePersona + "@" + l.Name() + ":unset"

	if _, err := st.lookPath("atm"); err != nil {
		return fmt.Errorf("%w: atm is not on PATH; the developing/manager prompt assumes `atm` resolves on PATH. Either add the directory containing the `atm` binary to PATH, or invoke atm from a shell where it resolves.", ErrUsage)
	}

	now := time.Now().UTC()
	runID := newRunID(opts.Project)
	timestamp := core.RFC3339UTC(now)
	contextPath := contextCachePath(s.StorePath(), p.Code, "dev", effectivePersona, "", "")

	rendered := developing.RenderContext(developing.ContextData{
		Code:               p.Code,
		Name:               p.Name,
		Actor:              actor,
		Persona:            effectivePersona,
		PersonaPrompt:      personaPrompt,
		PersonaDescription: personaDescription,
	})
	if err := writeContextIfDiff(contextPath, []byte(rendered)); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	base := l.BuildArgv()
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
	envValues := developingEnvValues(opts.Project, actor, runID, contextPath, l.Name(), effectivePersona, timestamp)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "developing", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}

	exitCode, runErr := st.runChild(l.Name(), argv, env, l.NotFoundHint())
	if err := emitLaunchTail(st, "developing", opts.Project, runID, contextPath, l.Name(), exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

func developingEnvValues(project, actor, runID, contextPath, agent, persona, timestamp string) map[string]string {
	m := map[string]string{
		"ATM_ROLE":         "developing",
		"ATM_PROJECT":      project,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_TIMESTAMP":    timestamp,
		"ATM_CONTEXT_FILE": contextPath,
		"ATM_AGENT":        agent,
	}
	if persona != "" {
		m["ATM_PERSONA"] = persona
	}
	return m
}
