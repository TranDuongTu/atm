package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"
	"atm/internal/version"

	"github.com/spf13/cobra"
)

type childRunner func(name string, argv []string, env []string, notFoundHint string) (int, error)
type tuiRunner func(storePath, actor string) error

// Deps are the composition-root-provided dependencies (wired by cmd/atm).
// RunTUI launches the interactive TUI for the given store path and actor.
type Deps struct {
	RunTUI func(storePath, actor string) error
	// Registry holds the capability commands the composition root enabled;
	// nil behaves as empty (no capability commands mount).
	Registry *capability.Registry
	// OpenService / OpenAdmin construct the store for a --store/ATM_HOME
	// path (unresolved; the constructor resolves it). The CLI never names
	// the concrete store — the composition root injects these.
	OpenService func(storePath string) (core.Service, error)
	OpenAdmin   func(storePath string) (core.StorageAdmin, error)
}

type globalFlags struct {
	store  string
	output string
	actor  string
	quiet  bool
}

type cliState struct {
	flags globalFlags
	in    io.Reader
	out   io.Writer
	err   io.Writer

	runChildFn      childRunner
	runTUI          tuiRunner
	stdinIsTerminal func() bool

	// openServiceFn / openAdminFn construct the store for a --store path. The
	// CLI never names the concrete store; the composition root injects these
	// (production wires store.Open; the golden harness wires seeded opens so
	// v2 events authored INSIDE command execution mint reproducible aliases).
	openServiceFn func(string) (core.Service, error)
	openAdminFn   func(string) (core.StorageAdmin, error)

	// registry is the capability registry the composition root injected;
	// nil-safe (behaves as empty).
	registry *capability.Registry
}

func (s *cliState) stdin() io.Reader {
	if s.in != nil {
		return s.in
	}
	return os.Stdin
}

func (s *cliState) stdout() io.Writer {
	if s.out != nil {
		return s.out
	}
	return os.Stdout
}

func (s *cliState) stderr() io.Writer {
	if s.err != nil {
		return s.err
	}
	return os.Stderr
}

func (s *cliState) isStdinTerminal() bool {
	if s.stdinIsTerminal != nil {
		return s.stdinIsTerminal()
	}
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func newRootCmdWithState(st *cliState) *cobra.Command {
	root := &cobra.Command{
		Use:           "atm",
		Short:         "Agent Tasks Management",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return st.launchTUI()
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if v := os.Getenv("ATM_ACTOR"); v != "" && st.flags.actor == "" {
				st.flags.actor = v
			}
			if st.flags.output != "" && st.flags.output != outputJSON && st.flags.output != outputText {
				return fmt.Errorf("%w: --output must be json or text", ErrUsage)
			}
			if st.flags.output == "" {
				st.flags.output = outputText
			}
			return nil
		},
	}
	root.PersistentFlags().StringVar(&st.flags.store, "store", "", "path to the store directory (overrides ATM_HOME)")
	root.PersistentFlags().StringVar(&st.flags.output, "output", "", "output format: json|text (default text)")
	root.PersistentFlags().BoolVar(&st.flags.quiet, "quiet", false, "suppress non-essential stdout in text mode")

	root.AddCommand(newInitCmd(st))
	root.AddCommand(newStoreCmd(st))
	root.AddCommand(newConventionsCmd(st))
	root.AddCommand(newProjectCmd(st))
	root.AddCommand(newLabelCmd(st))
	root.AddCommand(newPersonaCmd(st))
	root.AddCommand(newActivityCmd(st))
	root.AddCommand(newTaskCmd(st))
	root.AddCommand(newVocabularyCmd(st))
	root.AddCommand(newEmbedCmd(st))
	root.AddCommand(newIndexCmd(st))
	root.AddCommand(newSearchCmd(st))
	root.AddCommand(newInquiryCmd(st))
	root.AddCommand(newAgentsCmd(st))
	root.AddCommand(newDevCmd(st))
	root.AddCommand(newManageCmd(st))
	root.AddCommand(newManageContextCmd(st))
	root.AddCommand(newVersionCmd(st))

	for _, c := range st.registry.Commands(st) {
		root.AddCommand(c)
	}

	return root
}

func bindActorFlag(cmd *cobra.Command, st *cliState) {
	cmd.PersistentFlags().StringVar(&st.flags.actor, "actor", "", "actor id (free-form; env ATM_ACTOR)")
}

func (s *cliState) launchTUI() error {
	root := s.flags.store
	actor := s.flags.actor
	if actor == "" {
		actor = "admin@tui:unset"
	} else if !strings.Contains(actor, "@") {
		actor += "@tui:unset"
	}
	run := s.runTUI
	if run == nil {
		return fmt.Errorf("tui runner not wired (composition root must set Deps.RunTUI)")
	}
	setTmuxWindowLabel(os.Stdout, tmuxLabelTUI)
	if err := run(root, actor); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func (s *cliState) runChild(name string, argv []string, env []string, notFoundHint string) (int, error) {
	if s.runChildFn != nil {
		return s.runChildFn(name, argv, env, notFoundHint)
	}
	return runChild(name, argv, env, notFoundHint)
}

func (s *cliState) openStore() (core.Service, error) {
	if s.openServiceFn == nil {
		return nil, fmt.Errorf("store opener not wired (composition root must set Deps.OpenService)")
	}
	return s.openServiceFn(s.flags.store)
}

func (s *cliState) openAdmin() (core.StorageAdmin, error) {
	if s.openAdminFn == nil {
		return nil, fmt.Errorf("store opener not wired (composition root must set Deps.OpenAdmin)")
	}
	return s.openAdminFn(s.flags.store)
}

func (s *cliState) isJSON() bool { return s.flags.output == outputJSON }

func (s *cliState) emit(out io.Writer, v any, textFn func()) error {
	if s.isJSON() {
		return writeJSON(out, v)
	}
	textFn()
	return nil
}

func (s *cliState) resolveActor(required bool) (string, error) {
	raw := s.flags.actor
	if raw == "" {
		return "admin@cli:unset", nil
	}
	if !strings.Contains(raw, "@") {
		return raw + "@cli:unset", nil
	}
	return raw, nil
}

func newVersionCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the atm version",
		Run: func(cmd *cobra.Command, args []string) {
			info := version.Info()
			if st.isJSON() {
				fmt.Fprint(st.stdout(), version.EmitJSON(info))
				return
			}
			text := version.FormatText(map[string]string{
				"version": version.Version,
				"commit":  version.Commit,
				"date":    version.Date,
				"os":      info["os"].(string),
				"arch":    info["arch"].(string),
			})
			fmt.Fprintln(st.stdout(), text)
		},
	}
}

func Execute(deps Deps) int {
	st := &cliState{runTUI: deps.RunTUI, registry: deps.Registry, openServiceFn: deps.OpenService, openAdminFn: deps.OpenAdmin}
	root := newRootCmdWithState(st)
	if err := root.Execute(); err != nil {
		if st.isJSON() {
			env := NewErrorEnvelopeFromError(err)
			fmt.Fprintln(st.stderr(), env.String())
		} else {
			fmt.Fprintln(st.stderr(), "error:", err)
		}
		return ExitCodeForError(err)
	}
	return ExitSuccess
}
