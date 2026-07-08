package cli

import (
	"fmt"

	"atm/internal/actor"

	"github.com/spf13/cobra"
)

func newActorCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "actor", Short: "Actor migration and alias commands"}
	cmd.AddCommand(newActorMigrateCmd(st))
	cmd.AddCommand(newActorAliasCmd(st))
	return cmd
}

func newActorMigrateCmd(st *cliState) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Seed built-in personas and alias legacy actor strings",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			res, err := s.MigrateActors(dryRun)
			if err != nil {
				return err
			}
			// actor.AliasEntry has json tags, so res.Added serializes directly.
			return st.emit(st.stdout(), map[string]any{
				"dry_run": dryRun,
				"seeded":  res.Seeded,
				"aliases": res.Added,
			}, func() {
				fmt.Fprintf(st.stdout(), "seeded personas: %v\n", res.Seeded)
				for raw, e := range res.Added {
					fmt.Fprintf(st.stdout(), "%s -> persona=%s agent=%s\n", raw, e.Persona, e.Agent)
				}
				if dryRun {
					fmt.Fprintln(st.stdout(), "(dry-run: nothing written)")
				}
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "compute the migration without writing")
	return cmd
}

func newActorAliasCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "alias", Short: "Manage actor aliases"}
	cmd.AddCommand(newActorAliasSetCmd(st))
	cmd.AddCommand(newActorAliasListCmd(st))
	cmd.AddCommand(newActorAliasRemoveCmd(st))
	return cmd
}

func newActorAliasSetCmd(st *cliState) *cobra.Command {
	var persona, agent, model string
	cmd := &cobra.Command{
		Use:   "set <raw-actor>",
		Short: "Set (or override) the alias for a raw actor string",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if persona == "" {
				return fmt.Errorf("%w: --persona is required", ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			e := actor.AliasEntry{Persona: persona, Agent: agent, Model: model}
			if err := s.SetAlias(args[0], e); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"alias": map[string]any{args[0]: e}}, func() {
				fmt.Fprintf(st.stdout(), "set alias %s -> persona=%s agent=%s model=%s\n", args[0], persona, agent, model)
			})
		},
	}
	cmd.Flags().StringVar(&persona, "persona", "", "persona name (required)")
	cmd.Flags().StringVar(&agent, "agent", "", "agent")
	cmd.Flags().StringVar(&model, "model", "", "model")
	return cmd
}

func newActorAliasListCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List actor aliases",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			m, err := s.LoadAliases()
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"aliases": m}, func() {
				for raw, e := range m {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\t%s\n", raw, e.Persona, e.Agent, e.Model)
				}
			})
		},
	}
}

func newActorAliasRemoveCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <raw-actor>",
		Short: "Remove an actor alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveAlias(args[0]); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": args[0]}, func() {
				fmt.Fprintf(st.stdout(), "removed alias %s\n", args[0])
			})
		},
	}
}
