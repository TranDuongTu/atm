package cli

import (
	"fmt"

	"atm/internal/seed"

	"github.com/spf13/cobra"
)

const conventionsText = `# ATM Conventions (advisory)

## What ATM is

ATM (Agent Tasks Management) is a label-substrate task store for AI agents and the humans steering them. A project holds tasks; each task carries free-form text (title, description) and a set of labels. There is no status field, no claim entity, no review queue, no links table, no state machine — status, type, priority, ownership, and relationships are all expressed as labels and interpreted by the agent reading them. Workflow lives outside the store, in agent prompts and human habits; the store only keeps the substrate legible.

## Where labels live

Labels are global and per-project. The seeded defaults (status, type, priority, context) are written into every new project by ` + "`atm project create`" + ` and re-applied idempotently by ` + "`atm label seed --project <CODE>`" + ` / the Labels tab [S] key. The full, authoritative list with descriptions is in the store — do not memorize or duplicate it here. To see the labels that exist in this project, run ` + "`atm label list --project <CODE>`" + `. Each label carries a description; that description is the label's intention record.

## Where tasks live

Tasks live in the store, one JSON file per task, scoped to a project. ` + "`atm task list --project <CODE>`" + ` lists them. Each task has an ID (` + "`<CODE>-<NNNN>`" + `), a title, a description, and a label set. The description is free-form text — agents write what they are doing, what they found, what they decided, so the next agent or human can pick up where they left off.

## How to read a task and its labels

A task is read as: its title (one line of intent), its description (the running narrative), and its labels (the faceted classification). The labels are the query surface; the description is the working memory. A ` + "`context:agent`" + ` task's description holds agent directions for this project. A ` + "`context:repository`" + ` task points at a repo to read. A ` + "`context:documentation`" + ` task points at a doc to read. ` + "`status:open`" + ` means not done; ` + "`status:in-progress`" + ` means someone is on it; ` + "`status:done`" + ` means stop. Labels are advisory — nothing in the store enforces them, but reading them is how you understand a task without re-reading its whole history.

## How to search

Use labels as the filter. ` + "`atm task list --project <CODE> --label <CODE>:<ns>:<value>`" + ` returns tasks carrying that exact label. Wildcard labels (e.g. ` + "`<CODE>:status:*`" + `) drive faceted grouping via ` + "`atm task list --facets`" + `. Combine labels to narrow: ` + "`--label <CODE>:status:open --label <CODE>:type:bug`" + ` is open bugs.

## Agent first-contact sequence

1. Read this guide, including the code-of-conduct below.
2. ` + "`atm label list --project <CODE>`" + ` — read every label's description first to learn the project's vocabulary before touching tasks. Labels are the project's language; knowing them makes every task query meaningful. Do not assume the seeded defaults are all the labels this project uses.
3. ` + "`atm task list --project <CODE> --label <CODE>:context:agent`" + ` — get agent directions for working in this project.
4. ` + "`atm task list --project <CODE> --label <CODE>:context:repository`" + ` / ` + "`:context:documentation`" + ` — discover repository pointers and documentation.
5. ` + "`atm task list --project <CODE> --label <CODE>:status:open`" + ` — get open work.

A fresh agent that does not yet know the project's namespaces runs the label-list step first and follows the descriptions.

## Agent code-of-conduct (label hygiene)

Agents working in an ATM project follow these rules to keep the label substrate legible for humans and other agents:

1. Read before you write. Run ` + "`atm label list --project <CODE>`" + ` and read every label's description before introducing any new label. The existing labels are the project's vocabulary; reuse them whenever one fits your intent.
2. Default setup is the baseline. The seeded labels (status, type, priority, context) cover the common cases. Prefer them. Do not reinvent ` + "`status:open`" + ` as ` + "`state:open`" + ` or ` + "`wf:open`" + `.
3. Invent only when nothing fits. If no existing label captures your intent, you may create a new one — agents are free to self-organize. But before you do, ask yourself: would a human reviewing the Labels tab understand why this label exists?
4. State the intention in the label description. When you create a new label, also call ` + "`atm label add --name <CODE>:<ns>:<value> --description \"<one sentence: why this label exists>\"`" + `. The description is the intention record. A label with no description is a flag for human review: "agent introduced this but didn't explain why."
5. One label, one meaning. Don't use the same label string to mean different things across tasks. If your intent diverges from an existing label's description, create a new label with a distinct name and a description that distinguishes it.
6. Humans reconcile. The Labels tab is the human's review surface. If you see labels that overlap, contradict, or lack descriptions, edit or remove them there. Agents follow the rules above; humans curate.

## First-time human sequence

1. ` + "`atm tui`" + ` (auto-inits the store).
2. Create the project (Add in the Projects tab). Project create auto-seeds the default labels with descriptions, so the Labels tab is populated from the start.
3. Create seed index tasks (` + "`context:agent`" + `, ` + "`context:repository`" + `, ` + "`context:documentation`" + `) and initial work tasks, labeling as you go. The human curates labels in the Labels tab.

## Notes

- Plugins/skills: ATM ships only the doc + the conventions command. Plugins or agent skills may wrap the first-contact sequence; ATM itself has no plugin mechanism.
- Re-seeding defaults: ` + "`atm label seed --project <CODE>`" + ` or the Labels tab [S] key re-applies the default set idempotently — existing descriptions are preserved, and any new defaults introduced in a release are added.

Conventions are advisory only — nothing in the store validates or special-cases the documented namespaces.
`

func conventionsStructured() map[string]any {
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
		"what_atm_is":                       "ATM (Agent Tasks Management) is a label-substrate task store for AI agents and the humans steering them. A project holds tasks; each task carries free-form text (title, description) and a set of labels. There is no status field, no claim entity, no review queue, no links table, no state machine — status, type, priority, ownership, and relationships are all expressed as labels and interpreted by the agent reading them. Workflow lives outside the store, in agent prompts and human habits; the store only keeps the substrate legible.",
		"where_labels_live":                 "Labels are global and per-project. The seeded defaults (status, type, priority, context) are written into every new project by atm project create and re-applied idempotently by atm label seed --project <CODE> / the Labels tab [S] key. The full, authoritative list with descriptions is in the store — do not memorize or duplicate it here. To see the labels that exist in this project, run atm label list --project <CODE>. Each label carries a description; that description is the label's intention record.",
		"where_tasks_live":                  "Tasks live in the store, one JSON file per task, scoped to a project. atm task list --project <CODE> lists them. Each task has an ID (<CODE>-<NNNN>), a title, a description, and a label set. The description is free-form text — agents write what they are doing, what they found, what they decided, so the next agent or human can pick up where they left off.",
		"how_to_read_a_task_and_its_labels": "A task is read as: its title (one line of intent), its description (the running narrative), and its labels (the faceted classification). The labels are the query surface; the description is the working memory. A context:agent task's description holds agent directions for this project. A context:repository task points at a repo to read. A context:documentation task points at a doc to read. status:open means not done; status:in-progress means someone is on it; status:done means stop. Labels are advisory — nothing in the store enforces them, but reading them is how you understand a task without re-reading its whole history.",
		"how_to_search":                     "Use labels as the filter. atm task list --project <CODE> --label <CODE>:<ns>:<value> returns tasks carrying that exact label. Wildcard labels (e.g. <CODE>:status:*) drive faceted grouping via atm task list --facets. Combine labels to narrow: --label <CODE>:status:open --label <CODE>:type:bug is open bugs.",
		"code_of_conduct":                   codeOfConduct,
		"seeded_labels":                     seeded,
		"first_time_human_sequence": []string{
			"atm tui (auto-inits the store)",
			"create the project (Add in the Projects tab); project create auto-seeds the default labels with descriptions",
			"create seed index tasks (context:agent, context:repository, context:documentation) and initial work tasks, labeling as you go",
		},
		"agent_first_contact_sequence": []string{
			"read this guide, including the code-of-conduct",
			"atm label list --project <CODE> — read every label's description first to learn the project's vocabulary before touching tasks",
			"atm task list --project <CODE> --label <CODE>:context:agent — get agent directions for working in this project",
			"atm task list --project <CODE> --label <CODE>:context:repository / :context:documentation — discover repository pointers and documentation",
			"atm task list --project <CODE> --label <CODE>:status:open — get open work",
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
