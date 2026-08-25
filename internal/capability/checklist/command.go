package checklist

import (
	"errors"
	"fmt"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"
	"atm/skills"

	"github.com/spf13/cobra"
)

// New returns the capability the composition root registers.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string                        { return CapabilityName }
func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }

func (Cap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(svc, code, actor)
}

// Command mounts only seed: the working verbs are the top-level
// `atm checklist` noun (the channel facade precedent). Registry adds `guide`.
func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "Checklists: per-persona standing operating procedures; managed via the top-level `atm checklist` noun",
	}
	var project string
	seed := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the checklist vocabulary, board, and shipped starter checklists exist for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			if _, err := svc.GetProject(project); err != nil {
				return fmt.Errorf("project %q: %w", project, err)
			}
			boards, err := EnsureVocabulary(svc, project, actor)
			if err != nil {
				return err
			}
			names := make([]string, 0, len(boards))
			for _, b := range boards {
				names = append(names, b.Name)
			}
			sub := func(s string) string { return strings.ReplaceAll(s, "<CODE>", project) }
			created := make([]string, 0)
			skipped := make([]string, 0)
			for _, seed := range skills.ChecklistSeeds() {
				key := seed.Persona + "/" + seed.Name
				if _, err := svc.GetChecklist(project, seed.Persona, seed.Name); err == nil {
					skipped = append(skipped, key)
					continue
				} else if !errors.Is(err, core.ErrNotFound) {
					return err
				}
				steps := make([]string, len(seed.Steps))
				for i, s := range seed.Steps {
					steps[i] = sub(s)
				}
				if _, err := svc.CreateChecklist(project, core.ChecklistRecord{Persona: seed.Persona, Name: seed.Name, Purpose: sub(seed.Purpose), Steps: steps}, actor); err != nil {
					return err
				}
				created = append(created, key)
			}
			return env.Emit(map[string]any{"project": project, "boards": names, "created": created, "skipped": skipped}, func() {
				fmt.Fprintf(env.Stdout(), "ensured checklist vocabulary for %s; created %d starter checklist(s), skipped %d existing\n", project, len(created), len(skipped))
			})
		},
	}
	seed.Flags().StringVar(&project, "project", "", "project code")
	_ = seed.MarkFlagRequired("project")
	env.BindActorFlag(cmd)
	cmd.AddCommand(seed)
	return cmd
}
