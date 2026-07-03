package cli

import (
	"fmt"

	"atm/internal/seed"

	"github.com/spf13/cobra"
)

const conventionsVersion = "v2.1"

const conventionsText = `# ATM Conventions (advisory)

Version: ` + conventionsVersion + `

v2's vision is that workflow lives outside the system — in agent prompts and
human habits, not in the store. But a fresh agent landing in a project needs a
deterministic entry point, and a fresh project needs its labels seeded.
Onboarding is the act of seeding index tasks and seed labels; the conventions
below tell humans and agents how to do that using only the label substrate.
No bootstrap command, no reserved labels, no repo-side files, no store-side
repo paths. The conventions are documented, not code-reserved: the system
treats ATM:context:agent identically to ATM:type:bug.

## Suggested seed namespaces

A fresh project is auto-seeded with the 17 default labels below on ` + "`atm project create`" + `
(and re-applied idempotently by ` + "`atm label seed --project <CODE>`" + ` / the Labels tab [S] key).
Templated namespaces (repo:<name>, doc:<name>, claimed-by:<agent>, blocks:<ID>,
related:<ID>) are created on demand — they depend on project-specific values
and are NOT seeded as concrete labels.

| Namespace | Examples | Purpose |
|--------------------|-----------------------------|------------------------------------------------------------------|
| status:            | open, todo, in-progress, done, blocked, review | workflow states — labels only, no state machine |
| type:              | bug, feature, task, chore   | task categorization |
| priority:          | high, medium, low           | optional prioritization |
| context:documentation | ATM:context:documentation | the labeled task contains documentation about the project |
| context:repository | ATM:context:repository      | the labeled task contains a pointer to a code repository |
| context:agent      | ATM:context:agent           | agent direction when navigating the project |
| context:fixit      | ATM:context:fixit           | something on this task should be reviewed, updated, or altered |
| repo:<name>        | ATM:repo:atm                | index task pointing at a repo — created on demand, not seeded |
| doc:<name>         | ATM:doc:architecture        | index task pointing at a doc/resource — created on demand, not seeded |
| claimed-by:<agent> | ATM:claimed-by:claude       | who's working on what — last-writer-wins, no conflict detection |
| blocks:<ID>, related:<ID> | ATM:blocks:ATM-0002  | task relationships via labels — created on demand, not seeded |

## Agent code-of-conduct (label hygiene)

Agents working in an ATM project follow these rules to keep the label substrate
legible for humans and other agents:

1. Read before you write. Run ` + "`atm label list --project <CODE>`" + ` and read every
   label's description before introducing any new label. The existing labels
   are the project's vocabulary; reuse them whenever one fits your intent.
2. Default setup is the baseline. The seeded labels (status, type, priority,
   context) cover the common cases. Prefer them. Do not reinvent status:open
   as state:open or wf:open.
3. Invent only when nothing fits. If no existing label captures your intent,
   you may create a new one — agents are free to self-organize. But before you
   do, ask yourself: would a human reviewing the Labels tab understand why this
   label exists?
4. State the intention in the label description. When you create a new label,
   also call ` + "`atm label add --name <CODE>:<ns>:<value> --description \"<one sentence: why this label exists>\"`" + `.
   The description is the intention record. A label with no description is a
   flag for human review: "agent introduced this but didn't explain why."
5. One label, one meaning. Don't use the same label string to mean different
   things across tasks. If your intent diverges from an existing label's
   description, create a new label with a distinct name and a description that
   distinguishes it.
6. Humans reconcile. The Labels tab is the human's review surface. If you see
   labels that overlap, contradict, or lack descriptions, edit or remove them
   there. Agents follow the rules above; humans curate.

## First-time human sequence

1. atm tui (auto-inits the store)
2. Create the project (Add in the Projects tab). Project create auto-seeds the
   17 default labels with descriptions, so the Labels tab is populated from
   the start.
3. Create seed index tasks (context:agent, context:repository,
   context:documentation) and initial work tasks, labeling as you go. The
   human curates labels in the Labels tab.

## Agent first-contact sequence

1. atm conventions — read this guide, including the code-of-conduct.
2. atm label list --project <CODE> — read every label's description first to
   understand the project's vocabulary before exploring tasks. Labels are the
   project's language; knowing them makes every task query meaningful.
3. task list --project <CODE> --label <CODE>:context:agent — get agent
   directions for working in this project.
4. task list --project <CODE> --label <CODE>:context:repository /
   :context:documentation — discover repository pointers and documentation.
5. task list --project <CODE> --label <CODE>:status:open — get open work.

A fresh agent that does not yet know the project's namespaces runs the
label-list step first and follows the descriptions.

## Notes

- Plugins/skills: ATM ships only the doc + the conventions command. Plugins or
  agent skills may wrap the first-contact sequence; ATM itself has no plugin
  mechanism.
- Re-seeding defaults: ` + "`atm label seed --project <CODE>`" + ` or the Labels tab [S] key
  re-applies the default set idempotently — existing descriptions are
  preserved, and any new defaults introduced in a release are added.

Conventions are advisory only — nothing in the store validates or
special-cases the documented namespaces.
`

func conventionsStructured() map[string]any {
	namespaces := []map[string]string{
		{"namespace": "status:", "examples": "open, todo, in-progress, done, blocked, review", "purpose": "workflow states — labels only, no state machine"},
		{"namespace": "type:", "examples": "bug, feature, task, chore", "purpose": "task categorization"},
		{"namespace": "priority:", "examples": "high, medium, low", "purpose": "optional prioritization"},
		{"namespace": "context:documentation", "examples": "ATM:context:documentation", "purpose": "the labeled task contains documentation about the project"},
		{"namespace": "context:repository", "examples": "ATM:context:repository", "purpose": "the labeled task contains a pointer to a code repository"},
		{"namespace": "context:agent", "examples": "ATM:context:agent", "purpose": "agent direction when navigating the project"},
		{"namespace": "context:fixit", "examples": "ATM:context:fixit", "purpose": "something on this task should be reviewed, updated, or altered"},
		{"namespace": "repo:<name>", "examples": "ATM:repo:atm", "purpose": "index task pointing at a repo — created on demand, not seeded"},
		{"namespace": "doc:<name>", "examples": "ATM:doc:architecture", "purpose": "index task pointing at a doc/resource — created on demand, not seeded"},
		{"namespace": "claimed-by:<agent>", "examples": "ATM:claimed-by:claude", "purpose": "who's working on what — last-writer-wins, no conflict detection"},
		{"namespace": "blocks:<ID>, related:<ID>", "examples": "ATM:blocks:ATM-0002", "purpose": "task relationships via labels — created on demand, not seeded"},
	}
	codeOfConduct := []string{
		"Read before you write. Run atm label list --project <CODE> and read every label's description before introducing any new label.",
		"Default setup is the baseline. Prefer seeded labels; do not reinvent them under new names.",
		"Invent only when nothing fits. Agents are free to self-organize, but a human reviewing the Labels tab should understand why a new label exists.",
		"State the intention in the label description. A label with no description is a flag for human review.",
		"One label, one meaning. If intent diverges from an existing label's description, create a new label with a distinct name.",
		"Humans reconcile. The Labels tab is the human's review surface for overlapping, contradictory, or undescribed labels.",
	}
	seeded := make([]map[string]string, 0, len(seed.Labels))
	for _, l := range seed.Labels {
		seeded = append(seeded, map[string]string{"suffix": l.Suffix, "description": l.Description})
	}
	return map[string]any{
		"version":         conventionsVersion,
		"namespaces":      namespaces,
		"code_of_conduct": codeOfConduct,
		"seeded_labels":   seeded,
		"first_time_human_sequence": []string{
			"atm tui (auto-inits the store)",
			"create the project (Add in the Projects tab); project create auto-seeds the 17 default labels with descriptions",
			"create seed index tasks (context:agent, context:repository, context:documentation) and initial work tasks, labeling as you go",
		},
		"agent_first_contact_sequence": []string{
			"atm conventions — read this guide, including the code-of-conduct",
			"atm label list --project <CODE> — read every label's description first to understand the project's vocabulary before exploring tasks",
			"task list --project <CODE> --label <CODE>:context:agent — get agent directions for working in this project",
			"task list --project <CODE> --label <CODE>:context:repository / :context:documentation — discover repository pointers and documentation",
			"task list --project <CODE> --label <CODE>:status:open — get open work",
		},
		"advisory": "Conventions are advisory only — nothing in the store validates or special-cases the documented namespaces.",
	}
}

func newConventionsCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conventions",
		Short: "Print the onboarding guide and suggested label namespaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"conventions": conventionsStructured()})
			}
			fmt.Fprint(st.stdout(), conventionsText)
			return nil
		},
	}
	return cmd
}
