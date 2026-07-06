package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"atm/internal/onboard"
	"atm/internal/store"

	"github.com/spf13/cobra"
)

type onboardingOpts struct {
	Project       string
	Actor         string
	PromptVersion string
	DryRun        bool
}

func newOnboardingCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "onboarding",
		Short: "Launch a non-interactive agent to seed an existing project with context",
	}
	cmd.AddCommand(newOnboardingOpencodeCmd(st))
	cmd.AddCommand(newOnboardingOllamaCmd(st))
	return cmd
}

func newOnboardingOpencodeCmd(st *cliState) *cobra.Command {
	var opts onboardingOpts
	cmd := &cobra.Command{
		Use:   "opencode",
		Short: "Onboard via opencode run --auto",
		RunE: func(cmd *cobra.Command, args []string) error {
			l := onboard.OpencodeLauncher{}
			opts.Actor = defaultActor(l.Name(), st, opts.Actor)
			return runOnboarding(st, l, opts)
		},
	}
	addOnboardingFlags(cmd, &opts)
	return cmd
}

func newOnboardingOllamaCmd(st *cliState) *cobra.Command {
	var opts onboardingOpts
	var integration string
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Onboard via ollama launch <integration> -- run --auto",
		RunE: func(cmd *cobra.Command, args []string) error {
			l := onboard.OllamaLauncher{Integration: integration}
			opts.Actor = defaultActor(l.Name(), st, opts.Actor)
			return runOnboarding(st, l, opts)
		},
	}
	addOnboardingFlags(cmd, &opts)
	cmd.Flags().StringVar(&integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}

func addOnboardingFlags(cmd *cobra.Command, opts *onboardingOpts) {
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to onboard into (must pre-exist)")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into history (default <launcher>-onboard)")
	cmd.Flags().StringVar(&opts.PromptVersion, "prompt-version", "", "embedded prompt version (default latest)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render + write prompt + print argv; do not launch")
	_ = cmd.MarkFlagRequired("project")
}

// defaultActor returns the explicit actor if set, the global --actor/ATM_ACTOR
// if set, or "<launcher>-onboard" as the final fallback.
func defaultActor(launcherName string, st *cliState, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if st.flags.actor != "" {
		return st.flags.actor
	}
	return launcherName + "-onboard"
}

// runOnboarding validates the project, snapshots existing tasks, renders the
// prompt, writes it to $ATM_HOME/onboarding/<run-id>.md, prints the header,
// and (unless --dry-run) execs the launcher as a child. It prints the tail
// summary after the child exits.
func runOnboarding(st *cliState, l onboard.Launcher, opts onboardingOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	p, err := s.GetProject(opts.Project)
	if err != nil {
		return fmt.Errorf("%w: project %s not found; create it first:\n  atm project create --code %s --name \"...\"",
			ErrNotFound, opts.Project, opts.Project)
	}

	version := opts.PromptVersion
	if version == "" {
		version = onboard.Latest
	}

	existing := s.ListTasks(store.QueryFilters{Project: opts.Project})
	snapshot := renderExistingTasksTable(existing)

	atmBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve atm binary: %w", err)
	}

	runID := newRunID(opts.Project)
	title := fmt.Sprintf("ATM onboarding: %s (%s)", opts.Project, runID)
	promptPath := filepath.Join(s.StorePath(), "onboarding", runID+".md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		return fmt.Errorf("create onboarding dir: %w", err)
	}

	rendered, err := onboard.Render(version, onboard.Data{
		Code:          p.Code,
		Name:          p.Name,
		ATMBin:        atmBin,
		Actor:         opts.Actor,
		RunID:         runID,
		Timestamp:     store.RFC3339UTC(time.Now().UTC()),
		ExistingTasks: snapshot,
	})
	if err != nil {
		if errors.Is(err, onboard.ErrUnknownVersion) {
			return fmt.Errorf("%w: unknown prompt version %q; available: %s",
				ErrUsage, version, strings.Join(onboard.Versions(), ", "))
		}
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if err := os.WriteFile(promptPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write prompt file %s: %w", promptPath, err)
	}

	argv := l.BuildArgv(promptPath, title)
	if err := emitOnboardingHeader(st, l.Name(), opts.Project, runID, version, promptPath, argv); err != nil {
		return err
	}

	if opts.DryRun {
		return nil
	}

	setTmuxWindowLabel(os.Stdout, tmuxLabelOnboarding)
	before := len(existing)
	exitCode, runErr := runChild(l.Name(), argv, os.Environ(), l.NotFoundHint())
	after := len(s.ListTasks(store.QueryFilters{Project: opts.Project}))
	if err := emitOnboardingTail(st, l.Name(), opts.Project, runID, version, promptPath, before, after, exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

func emitOnboardingHeader(st *cliState, launcherName, project, runID, version, promptPath string, argv []string) error {
	return st.emit(st.stdout(), map[string]any{
		"run_id":         runID,
		"project":        project,
		"agent":          launcherName,
		"prompt_version": version,
		"prompt_path":    promptPath,
		"argv":           argv,
	}, func() {
		fmt.Fprintf(os.Stdout, "onboarding %s  run=%s  agent=%s  prompt-version=%s\n", project, runID, launcherName, version)
		fmt.Fprintf(os.Stdout, "  prompt:  %s\n", promptPath)
		fmt.Fprintf(os.Stdout, "  launching: %s\n", strings.Join(argv, " "))
	})
}

func emitOnboardingTail(st *cliState, launcherName, project, runID, version, promptPath string, before, after, agentExit int) error {
	return st.emit(st.stdout(), map[string]any{
		"run_id":         runID,
		"project":        project,
		"prompt_version": version,
		"prompt_path":    promptPath,
		"before":         before,
		"after":          after,
		"agent_exit":     agentExit,
	}, func() {
		fmt.Fprintf(os.Stdout, "onboarding %s  run=%s\n", project, runID)
		fmt.Fprintf(os.Stdout, "  prompt:  %s\n", promptPath)
		fmt.Fprintf(os.Stdout, "  before:  %d tasks\n", before)
		fmt.Fprintf(os.Stdout, "  after:   %d tasks\n", after)
		fmt.Fprintf(os.Stdout, "  created: %d   (net)\n", after-before)
		fmt.Fprintf(os.Stdout, "%s exited %d\n", launcherName, agentExit)
	})
}

func renderExistingTasksTable(tasks []*store.Task) string {
	if len(tasks) == 0 {
		return "(none)"
	}
	var b strings.Builder
	b.WriteString("| ID | Title | Labels |\n")
	b.WriteString("|----|-------|--------|\n")
	for _, t := range tasks {
		labels := strings.Join(t.Labels, ", ")
		if labels == "" {
			labels = "(none)"
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", t.ID, t.Title, labels)
	}
	return b.String()
}
