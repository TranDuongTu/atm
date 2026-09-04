package cli

import (
	"fmt"
	"strings"

	"atm/internal/compose"

	"github.com/spf13/cobra"
)

// newDispatchCmd is the named form of a dispatch (plan §3.7): you name an
// ACTION and everything else derives — the persona from its suits, the mode
// from its frontmatter, the eligible tasks from its targets expression.
//
// It shares launchSession with the root flags rather than reimplementing the
// binding, so the two spellings cannot drift apart. What it adds over the
// root flags is a NAME (a dispatch is a thing you do, not a modifier on the
// bare command) and --dry-run.
func newDispatchCmd(st *cliState) *cobra.Command {
	var opts sessionOpts
	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Dispatch an action: a session running one checklist",
		Long: "Dispatch an action — one checklist — as a session.\n\n" +
			"The persona comes from the checklist's suits, the mode from its own\n" +
			"frontmatter, and the eligible tasks from its targets expression;\n" +
			"--persona and --mode override those. Unmet requirements warn on\n" +
			"stderr and never block the launch.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Checklist == "" {
				return fmt.Errorf("%w: dispatch needs an action: --checklist <name>", ErrUsage)
			}
			opts.ExtraArgs = args
			return st.launchSession(opts)
		},
	}
	cmd.Flags().StringVar(&opts.Checklist, "checklist", "", "the ACTION to dispatch (required)")
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the session works on")
	cmd.Flags().StringVar(&opts.Task, "task", "", "the task to dispatch on (required by a task-target action)")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "override the persona the action's suits name")
	cmd.Flags().StringVar(&opts.Mode, "mode", "", "override the action's mode: eager|interactive")
	cmd.Flags().StringVar(&opts.Capability, "capability", "", "scope the session to one enabled capability")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "override the selected agent for this launch (see `atm agents list`)")
	cmd.Flags().StringVar(&opts.Launch, "launch", "", "override the persona's launch vehicle: prompt|hook|tui")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "bind and render the dispatch, then report it instead of launching")
	_ = cmd.MarkFlagRequired("checklist")
	return cmd
}

// emitDispatchPlan reports a bound-but-unlaunched dispatch. It prints what
// the binding RESOLVED TO — the derived persona, the mode, the vehicle, the
// argv — rather than the flags that were passed, because the whole question
// a dry run answers is what those flags turned into.
func emitDispatchPlan(st *cliState, code, agent string, plan *compose.Plan) error {
	return st.emit(st.stdout(), map[string]any{
		"project":      code,
		"checklist":    plan.Checklist,
		"persona":      plan.Persona,
		"mode":         plan.Mode,
		"vehicle":      plan.Vehicle,
		"task":         plan.EnvValues["ATM_TASK"],
		"agent":        agent,
		"context_path": plan.ContextPath,
		"argv":         plan.Argv,
		"warnings":     plan.Warnings,
		"env":          plan.EnvValues,
	}, func() {
		fmt.Fprintf(st.stdout(), "would dispatch %s on %s\n", plan.Checklist, code)
		fmt.Fprintf(st.stdout(), "  persona:  %s\n", plan.Persona)
		fmt.Fprintf(st.stdout(), "  mode:     %s\n", plan.Mode)
		fmt.Fprintf(st.stdout(), "  vehicle:  %s (%s)\n", plan.Vehicle, agent)
		if task := plan.EnvValues["ATM_TASK"]; task != "" {
			fmt.Fprintf(st.stdout(), "  task:     %s\n", task)
		}
		fmt.Fprintf(st.stdout(), "  context:  %s\n", plan.ContextPath)
		fmt.Fprintf(st.stdout(), "  argv:     %s\n", strings.Join(plan.Argv, " "))
	})
}
