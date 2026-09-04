package cli

import (
	"fmt"

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
			"`atm persona reset`. The built-ins still ship in the binary as the fallback for " +
			"machines that have not applied a profile yet.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newPersonaSetCmd(st))
	cmd.AddCommand(newPersonaResetCmd(st))
	cmd.AddCommand(newPersonaListCmd(st))
	cmd.AddCommand(newPersonaShowCmd(st))
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
			// Personas are PROJECT records (plan §7 pruned the machine-global
			// store): a project in scope lists its records, and without one
			// there is nothing to list but the built-ins the binary carries.
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
