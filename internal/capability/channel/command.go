package channel

import (
	"fmt"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// New returns the capability the composition root registers.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string                        { return CapabilityName }
func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }
func (Cap) Exposed(code string) []core.Label    { return Exposed(code) }

func (Cap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(svc, code, actor)
}

// Command mounts only seed here: the working verbs are the top-level
// `atm channel` noun (a deliberate facade — channels are managed as
// channels, not as capability plumbing). The registry adds `guide` itself.
func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "Channels: how personas communicate — repositories, Notion; managed via the top-level `atm channel` noun",
	}
	var project string
	seed := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the channel vocabulary and board exist for a project",
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
			return env.Emit(map[string]any{"project": project, "boards": names}, func() {
				fmt.Fprintf(env.Stdout(), "ensured channel vocabulary for %s\n", project)
			})
		},
	}
	seed.Flags().StringVar(&project, "project", "", "project code")
	_ = seed.MarkFlagRequired("project")
	env.BindActorFlag(cmd)
	cmd.AddCommand(seed)
	return cmd
}
