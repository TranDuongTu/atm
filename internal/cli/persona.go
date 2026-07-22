package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newPersonaCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "persona",
		Short: "Persona registry commands",
		Long: "A persona is a named system prompt that an agent runs under; personas are " +
			"referenced by the actor string, whose format is persona@agent:model. The persona " +
			"segment must be a registered persona before an agent can claim it. The built-ins " +
			"(developer, manager, admin, concierge) ship in the binary; custom personas are " +
			"created and edited here. Customize a built-in's personality via " +
			"`atm persona personality`.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newPersonaCreateCmd(st))
	cmd.AddCommand(newPersonaListCmd(st))
	cmd.AddCommand(newPersonaShowCmd(st))
	cmd.AddCommand(newPersonaPersonalityCmd(st))
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
			p, err := s.GetPersona(resolved)
			if err != nil {
				return err
			}
			spec, specErr := resolvePersonaSpec(s, resolved)
			overlay, _ := s.GetPersonality(resolved)
			return st.emit(st.stdout(), map[string]any{"persona": p, "modes": spec.ModeNames(), "default_mode": spec.DefaultMode, "personality_custom": overlay != ""}, func() {
				fmt.Fprintf(st.stdout(), "%s\t%s\n", p.Name, p.Description)
				if specErr == nil && len(spec.Modes) > 0 {
					fmt.Fprintf(st.stdout(), "modes: %s (default %s)\n", strings.Join(spec.ModeNames(), ", "), spec.DefaultMode)
				}
				if overlay != "" {
					fmt.Fprintln(st.stdout(), "personality: customized")
				}
				fmt.Fprintf(st.stdout(), "\n%s\n", p.Prompt)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name (positional arg takes precedence)")
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

func newPersonaPersonalityCmd(st *cliState) *cobra.Command {
	var setText, file string
	var clear bool
	cmd := &cobra.Command{
		Use:   "personality <name>",
		Short: "Show or customize a persona's personality section",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			s, err := st.openStore()
			if err != nil {
				return err
			}
			mutating := clear || setText != "" || file != ""
			if setText != "" && file != "" {
				return fmt.Errorf("%w: --set and --file are mutually exclusive", ErrUsage)
			}
			if !mutating {
				spec, err := resolvePersonaSpec(s, name)
				if err != nil {
					return err
				}
				overlay, err := s.GetPersonality(name)
				if err != nil {
					return err
				}
				effective := spec.Personality
				custom := overlay != ""
				if custom {
					effective = overlay
				}
				return st.emit(st.stdout(), map[string]any{"persona": name, "personality": effective, "custom": custom}, func() {
					fmt.Fprintln(st.stdout(), effective)
				})
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			if clear {
				if err := s.ClearPersonality(name); err != nil {
					return err
				}
				return st.emit(st.stdout(), map[string]any{"persona": name, "cleared": true}, func() {
					fmt.Fprintf(st.stdout(), "cleared personality for %s\n", name)
				})
			}
			text := setText
			if file != "" {
				b, err := os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("read --file: %w", err)
				}
				text = string(b)
			}
			if err := s.SetPersonality(name, text, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": name, "personality": strings.TrimSpace(text)}, func() {
				fmt.Fprintf(st.stdout(), "set personality for %s\n", name)
			})
		},
	}
	cmd.Flags().StringVar(&setText, "set", "", "set the personality text")
	cmd.Flags().StringVar(&file, "file", "", "read the personality text from a file")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the customization (revert to the persona's default)")
	return cmd
}
