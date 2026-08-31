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
			created := make([]string, 0)
			skipped := make([]string, 0)
			for _, seed := range skills.ChecklistSeeds() {
				if _, err := svc.GetChecklist(project, seed.Name); err == nil {
					skipped = append(skipped, seed.Name)
					continue
				} else if !errors.Is(err, core.ErrNotFound) {
					return err
				}
				if _, err := svc.CreateChecklist(project, SeedRecord(project, seed), actor); err != nil {
					return err
				}
				created = append(created, seed.Name)
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

// SeedRecord converts one shipped seed into the record the seed verb (and the
// setup wizard) creates: <CODE> substituted in purpose and every step text,
// suits/requires/origin carried verbatim. One converter so the two write
// paths cannot diverge.
func SeedRecord(code string, seed skills.ChecklistSeed) core.ChecklistRecord {
	sub := func(s string) string { return strings.ReplaceAll(s, "<CODE>", code) }
	var conv func(in []skills.SeedStep) []core.ChecklistStep
	conv = func(in []skills.SeedStep) []core.ChecklistStep {
		if len(in) == 0 {
			return nil
		}
		out := make([]core.ChecklistStep, len(in))
		for i, s := range in {
			out[i] = core.ChecklistStep{Text: sub(s.Text), Children: conv(s.Children)}
		}
		return out
	}
	return core.ChecklistRecord{
		Name:    seed.Name,
		Purpose: sub(seed.Purpose),
		Steps:   conv(seed.Steps),
		Suits:   seed.Suits,
		Requires: core.ChecklistRequires{
			Capabilities: seed.Requires.Capabilities,
			Channels:     seed.Requires.Channels,
		},
		Origin: seed.Origin,
	}
}
