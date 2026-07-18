package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const conventionsCoreText = "# ATM Conventions (advisory)\n\n" +
	"## What ATM is\n" +
	"ATM (Agent Tasks Management) is a label-substrate task store. A project holds tasks; each task has free-form text (title, description) and a set of labels. No status field, no claims, no review queue, no state machine — status, type, priority, ownership, relationships are all labels, interpreted by the agent reading them. The store keeps the substrate legible; capabilities own the semantics.\n\n" +
	"## Substrate\n" +
	"Substrate commands live under these namespaces; run `-h` on each for verbs and flags:\n" +
	"- `atm task` — tasks (ID, title, description, labels).\n" +
	"- `atm task comment` — per-task append-mostly thread, classified by a label.\n" +
	"- `atm label` — labels (`<CODE>:<ns>:<value>` or `<CODE>:<tag>`); a label's description records its intention. Three kinds: stored (asserted), namespace (prefix, emergent), board (computed from an expression).\n" +
	"- `atm project`, `atm persona`, `atm activity`, `atm store`, `atm search` — project lifecycle, actor identity, audit log, semantic search.\n\n" +
	"## Capabilities\n" +
	"Semantics beyond the substrate live in capabilities. Each owns a slice of the label substrate, contributes verbs, and explains itself. A project enables a per-project subset; commands for disabled capabilities are not mounted.\n" +
	"- `atm capability list` — enumerate registered capabilities (enabled + disabled).\n" +
	"- `atm capability <name> -h` — the verb tree a capability mounts.\n" +
	"- `atm capability <name> guide` — the capability's full agent-facing semantics, vocabulary, and operating mode (Brief + Autopilot sections).\n\n" +
	"## Actor identity\n" +
	"Every mutation stamps `persona@agent:model` (e.g. `developer@claude:opus-4.8`). `atm persona -h`; built-ins `developer`, `manager`, `admin`. `atm dev -h`.\n\n" +
	"Conventions are advisory only.\n"

func conventionsStructured() map[string]any {
	return map[string]any{
		"what_atm_is": "ATM (Agent Tasks Management) is a label-substrate task store. A project holds tasks; each task has free-form text (title, description) and a set of labels. No status field, no claims, no review queue, no state machine — status, type, priority, ownership, relationships are all labels, interpreted by the agent reading them. The store keeps the substrate legible; capabilities own the semantics.",
		"substrate": []map[string]string{
			{"namespace": "atm task", "summary": "tasks (ID, title, description, labels)"},
			{"namespace": "atm task comment", "summary": "per-task append-mostly thread, classified by a label"},
			{"namespace": "atm label", "summary": "labels (<CODE>:<ns>:<value> or <CODE>:<tag>); a label's description records its intention; three kinds: stored (asserted), namespace (prefix, emergent), board (computed from an expression)"},
			{"namespace": "atm project", "summary": "project lifecycle"},
			{"namespace": "atm persona", "summary": "actor identity"},
			{"namespace": "atm activity", "summary": "audit log"},
			{"namespace": "atm store", "summary": "store administration"},
			{"namespace": "atm search", "summary": "semantic search"},
		},
		"capabilities":   "Semantics beyond the substrate live in capabilities; a project enables a per-project subset, and commands for disabled capabilities are not mounted. Enumerate with `atm capability list`; discover one with `atm capability <name> -h` and `atm capability <name> guide`.",
		"actor_identity": "Every mutation stamps persona@agent:model (e.g. developer@claude:opus-4.8). See `atm persona -h`; built-ins developer, manager, admin. See `atm dev -h`.",
	}
}

func newConventionsCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conventions",
		Short: "Print the substrate primer and how to discover capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"conventions": conventionsStructured()})
			}
			fmt.Fprint(st.stdout(), conventionsCoreText)
			return nil
		},
	}
	return cmd
}
