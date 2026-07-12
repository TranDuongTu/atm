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
		Code:               p.Code,
		Name:               p.Name,
		ATMBin:             atmBin,
		Actor:              actor,
		RunID:              runID,
		Timestamp:          store.RFC3339UTC(time.Now().UTC()),
		Persona:            effectivePersona,
		PersonaPrompt:      personaPrompt,
		PersonaDescription: personaDescription,
	})
	if err := os.WriteFile(contextPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write context file %s: %w", contextPath, err)
	}

	base := l.BuildArgv()
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
	envValues := developingEnvValues(opts.Project, atmBin, actor, runID, contextPath, l.Name(), effectivePersona)
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
