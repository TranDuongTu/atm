package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newPersonaCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "persona", Short: "Persona registry commands"}
	cmd.AddCommand(newPersonaCreateCmd(st))
	cmd.AddCommand(newPersonaListCmd(st))
	cmd.AddCommand(newPersonaShowCmd(st))
	cmd.AddCommand(newPersonaEditCmd(st))
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
	return &cobra.Command{
		Use:   "list",
		Short: "List personas",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
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
}

func newPersonaShowCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a persona",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.GetPersona(name)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": p}, func() {
				fmt.Fprintf(st.stdout(), "%s\t%s\n\n%s\n", p.Name, p.Description, p.Prompt)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newPersonaEditCmd(st *cliState) *cobra.Command {
	var name, prompt, promptFile, description string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a persona (only supplied fields change)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			pr, prOK, err := resolvePrompt(prompt, promptFile)
			if err != nil {
				return err
			}
			var pPtr, dPtr *string
			if prOK {
				pPtr = &pr
			}
			if cmd.Flags().Changed("description") {
				dPtr = &description
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.EditPersona(name, pPtr, dPtr, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": p}, func() {
				fmt.Fprintf(st.stdout(), "updated persona %s\n", p.Name)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name")
	cmd.Flags().StringVar(&prompt, "prompt", "", "new prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read new prompt from file")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	_ = cmd.MarkFlagRequired("name")
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
