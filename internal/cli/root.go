package cli

import (
	"fmt"
	"io"
	"os"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

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
	root.PersistentFlags().StringVar(&st.flags.actor, "actor", "", "actor id (free-form; env ATM_ACTOR)")
	root.PersistentFlags().BoolVar(&st.flags.quiet, "quiet", false, "suppress non-essential stdout in text mode")

	root.AddCommand(newInitCmd(st))
	root.AddCommand(newStoreCmd(st))
	root.AddCommand(newConventionsCmd(st))
	root.AddCommand(newProjectCmd(st))
	root.AddCommand(newLabelCmd(st))
	root.AddCommand(newTaskCmd(st))
	root.AddCommand(newOnboardingCmd(st))
	root.AddCommand(newDevelopingCmd(st))
	root.AddCommand(newTUICmd(st))
	root.AddCommand(newVersionCmd(st))

	return root
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
	if s.flags.actor == "" {
		if required {
			return "", fmt.Errorf("%w: --actor or ATM_ACTOR is required", ErrUsage)
		}
		return "anonymous", nil
	}
	return s.flags.actor, nil
}

func newVersionCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the atm version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(st.stdout(), "atm version dev")
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
