package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newPersonaCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "persona",
		Short: "Persona registry commands",
		Long: "A persona is a named system prompt that an agent runs under; personas are " +
			"referenced by the actor string, whose format is persona@agent:model. The persona " +
			"segment must be a registered persona before an agent can claim it. A persona is a " +
			"PROJECT record: import one with `atm persona set`, restore it with " +
			"`atm persona reset`. The built-ins still ship in the binary as the " +
			"fallback while the machine-global personas are retired.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newPersonaSetCmd(st))
	cmd.AddCommand(newPersonaResetCmd(st))
	cmd.AddCommand(newPersonaCreateCmd(st))
	cmd.AddCommand(newPersonaListCmd(st))
	cmd.AddCommand(newPersonaShowCmd(st))
	cmd.AddCommand(newPersonaRemoveCmd(st))
	return cmd
}

// resolvePrompt returns the prompt from --prompt or --prompt-file (mutually
// exclusive). ok reports whether a prompt was supplied at all.
func resolvePrompt(prompt, promptFile string) (val string, ok bool, err error) {
	if prompt != "" && promptFile != "" {
		return "", false, fmt.Errorf("%w: --prompt and --prompt-file are mutually exclusive", ErrUsage)
	}
	if promptFile != "" {
		b, e := os.ReadFile(promptFile)
		if e != nil {
			return "", false, fmt.Errorf("read --prompt-file: %w", e)
		}
		return string(b), true, nil
	}
	if prompt != "" {
		return prompt, true, nil
	}
	return "", false, nil
}

func newPersonaCreateCmd(st *cliState) *cobra.Command {
	var name, prompt, promptFile, description string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a persona",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			pr, _, err := resolvePrompt(prompt, promptFile)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.CreatePersona(name, pr, description, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": p}, func() {
				fmt.Fprintf(st.stdout(), "created persona %s\n", p.Name)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name (lowercase slug)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "persona prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read persona prompt from file")
	cmd.Flags().StringVar(&description, "description", "", "one-line description")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newPersonaListCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List personas",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			// A project in scope means the project's own records; the
			// machine-global listing below is transitional and goes with
			// the store-global personas in the second half of ATM-207ab8.
			if code, err := personaProject(cmd); err == nil {
				return personaRecordList(st, s, code)
			}
			ps := s.ListPersonas()
			return st.emit(st.stdout(), map[string]any{"personas": ps}, func() {
				for _, p := range ps {
					if p.Description == "" {
						fmt.Fprintf(st.stdout(), "%s\n", p.Name)
					} else {
						fmt.Fprintf(st.stdout(), "%s\t%s\n", p.Name, p.Description)
					}
				}
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT) — lists that project's persona records")
	return cmd
}

func newPersonaShowCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "show [name]",
		Short: "Show a persona",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved := name
			if len(args) == 1 {
				resolved = args[0]
			}
			if resolved == "" {
				return fmt.Errorf("%w: persona name is required (positional or --name)", ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if code, cerr := personaProject(cmd); cerr == nil {
				if rec, rerr := s.GetPersonaRecord(code, resolved); rerr == nil {
					return st.emit(st.stdout(), map[string]any{"persona": rec}, func() {
						fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n\n%s\n", rec.Name, rec.Origin, rec.Description, rec.Prompt)
					})
				}
			}
			p, err := s.GetPersona(resolved)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": p}, func() {
				fmt.Fprintf(st.stdout(), "%s\t%s\n\n%s\n", p.Name, p.Description, p.Prompt)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name (positional arg takes precedence)")
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT) — shows that project's persona record")
	return cmd
}

func newPersonaRemoveCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a persona",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemovePersona(name); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": name}, func() {
				fmt.Fprintf(st.stdout(), "removed persona %s\n", name)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}
