package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newCapabilityCmd is the single mount point for capability commands and the
// discovery surface. Enabled capabilities' trees mount under
// `atm capability <name>`; disabled ones are unmounted (the hard gate) but
// still enumerated by `atm capability list`.
func newCapabilityCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capability",
		Short: "Discover and use the project's capabilities",
		Long: "Semantics beyond the substrate live in capabilities. Each owns a slice of " +
			"the label substrate, contributes verbs, and explains itself.\n\n" +
			"`atm capability list` enumerates registered capabilities (enabled + disabled " +
			"for the target project). `atm capability <name> -h` shows a capability's verbs; " +
			"`atm capability <name> guide` prints its full agent-facing semantics. Commands " +
			"for capabilities the project did not enable are not mounted.",
	}
	cmd.AddCommand(newCapabilityListCmd(st))
	for _, c := range st.registry.Commands(st) {
		cmd.AddCommand(c)
	}
	return cmd
}

func newCapabilityListCmd(st *cliState) *cobra.Command {
	var all bool
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Enumerate registered capabilities (enabled + disabled for the target project)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// st.registry was narrowed by the pre-parse mount when a project
			// was targeted (--project / --task / ATM_PROJECT); with no target
			// it IS the full registry, so everything reads enabled — matching
			// what is actually mounted.
			enabled := make(map[string]bool)
			names := st.registry.Names()
			if all {
				names = st.fullRegistry.Names()
			}
			for _, n := range names {
				enabled[n] = true
			}
			type row struct {
				Name    string `json:"name"`
				Summary string `json:"summary"`
				Enabled bool   `json:"enabled"`
			}
			descs := st.fullRegistry.Describe()
			rows := make([]row, 0, len(descs))
			for _, d := range descs {
				rows = append(rows, row{Name: d.Name, Summary: d.Summary, Enabled: enabled[d.Name]})
			}
			return st.emit(st.stdout(), map[string]any{"capabilities": rows}, func() {
				for _, r := range rows {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%t\n", r.Name, r.Summary, r.Enabled)
				}
			})
		},
	}
	// Consumed by the pre-parse mount (mountRegistry); declared so cobra
	// accepts it and help documents it.
	cmd.Flags().StringVar(&project, "project", "", "project code; enabled column reflects this project's enabled set")
	cmd.Flags().BoolVar(&all, "all", false, "ignore the project and list every registered capability as enabled")
	return cmd
}