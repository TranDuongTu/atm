package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"atm/internal/store"
	"atm/internal/tui"
	"atm/internal/version"

	"github.com/spf13/cobra"
)

type childRunner func(name string, argv []string, env []string, notFoundHint string) (int, error)
type tuiRunner func(storePath, actor string) error

type globalFlags struct {
	store  string
	output string
	actor  string
	quiet  bool
}

type cliState struct {
	flags globalFlags
	out   io.Writer
	err   io.Writer

	runChildFn childRunner
	runTUI     tuiRunner
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
	for _, name := range []string{"opencode", "codex", "claude"} {
		root.AddCommand(newDeveloperAgentCmd(st, name))
	}
	root.AddCommand(newDeveloperOllamaCmd(st))
	root.AddCommand(newManageCmd(st))
	root.AddCommand(newManageContextCmd(st))
	root.AddCommand(newVersionCmd(st))

	return root
}

func bindActorFlag(cmd *cobra.Command, st *cliState) {
	cmd.PersistentFlags().StringVar(&st.flags.actor, "actor", "", "actor id (free-form; env ATM_ACTOR)")
}

func (s *cliState) launchTUI() error {
	root := store.ResolveStorePath(s.flags.store)
	actor := s.flags.actor
	if actor == "" {
		actor = "admin@tui:unset"
	} else if !strings.Contains(actor, "@") {
		actor += "@tui:unset"
	}
	run := s.runTUI
	if run == nil {
		run = tui.Run
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

func (s *cliState) openStore() (*store.Store, error) {
	root := store.ResolveStorePath(s.flags.store)
	st, err := store.Open(root)
	if err != nil {
		return nil, err
	}
	return st, nil
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

func newInitCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create an empty store (idempotent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := store.ResolveStorePath(st.flags.store)
			s, err := store.Open(root)
			if err != nil {
				return err
			}
			if err := s.Init(""); err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"store": s.StorePath()})
			}
			fmt.Fprintln(st.stdout(), "initialized store at", s.StorePath())
			return nil
		},
	}
	cmd.Flags().StringVar(&st.flags.actor, "actor", "", "actor id (free-form; env ATM_ACTOR)")
	return cmd
}

func Execute() int {
	st := &cliState{}
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
