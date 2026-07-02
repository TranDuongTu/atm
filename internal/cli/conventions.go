package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const conventionsVersion = "v2.0"

const conventionsText = `# ATM Conventions (advisory)

Version: ` + conventionsVersion + `

v2's vision is that workflow lives outside the system — in agent prompts and
human habits, not in the store. But a fresh agent landing in a project needs a
deterministic entry point, and a fresh project needs its labels seeded.
Onboarding is the act of seeding index tasks and seed labels; the conventions
below tell humans and agents how to do that using only the label substrate.
No bootstrap command, no reserved labels, no repo-side files, no store-side
repo paths. The conventions are documented, not code-reserved: the system
treats ATM:context:start-here identically to ATM:type:bug.

## Suggested seed namespaces

A fresh project should populate these namespaces (via labels on seed index
tasks and work tasks):

| Namespace           | Examples                    | Purpose                                                          |
|--------------------|-----------------------------|------------------------------------------------------------------|
| status:            | open, todo, in-progress, done, blocked, review | workflow states — labels only, no state machine        |
| type:              | bug, feature, task, chore   | task categorization                                              |
| priority:          | high, medium, low           | optional prioritization                                          |
| repo:<name>        | ATM:repo:atm                | index task whose description says where to find the repo and what it means — this is the repo->project binding, expressed in the label substrate |
| doc:<name>         | ATM:doc:architecture        | index task pointing at a doc/resource and how to use it         |
| context:always-read| ATM:context:always-read     | pointer to the always-read context markdown (replaces the deleted v1 Project Guide) |
| context:start-here | ATM:context:start-here      | the single entry-point task a fresh agent queries first; its description is the "read this first" pointer (to a context.md, a steering note, or a list of where to look) |
| claimed-by:<agent> | ATM:claimed-by:claude       | who's working on what — replaces v1 Claim; last-writer-wins, no conflict detection |
| blocks:<ID>, related:<ID> | ATM:blocks:ATM-0002  | task relationships via labels (replaces v1 Links)                |

## First-time human sequence

1. atm tui (auto-inits the store)
2. Create the project (Add in the Projects tab)
3. Create a few seed index tasks (start-here, repo:<name>, doc:<name>,
   context:always-read) and initial work tasks, labeling as you go.
   The act of seeding these tasks populates the status/type/repo/doc/context
   namespaces organically — there is no separate bootstrap step.

## Agent first-contact sequence

1. atm conventions — read the guide.
2. task list --project <CODE> --label <CODE>:context:start-here — get the
   entry-point pointer and follow it.
3. task list --project <CODE> --label <CODE>:repo:* / :doc:* / :context:* —
   discover index tasks for repos, docs, and always-read context.
4. task list --project <CODE> --label <CODE>:status:open — get open work.

A fresh agent that does not yet know the project's namespaces runs the
start-here query first (one deterministic label) and follows whatever the
start-here task's description points at.

## Notes

- Plugins/skills: ATM ships only the doc + the conventions command. Plugins or
  agent skills may wrap the first-contact sequence; ATM itself has no plugin
  mechanism in v2.
- Repo->project binding: repo:<name> index tasks in the label substrate.
- Always-read context anchor: context:always-read index task pointing at the
  markdown; context:start-here as the deterministic entry point.
- Cold-start label vacuum: onboarding is seeding the index and work tasks,
  which populates the namespaces. No empty-project problem.

Conventions are advisory only — nothing in the store validates or
special-cases the documented namespaces.
`

func conventionsStructured() map[string]any {
	return map[string]any{
		"version": conventionsVersion,
		"namespaces": []map[string]string{
			{"namespace": "status:", "examples": "open, todo, in-progress, done, blocked, review", "purpose": "workflow states — labels only, no state machine"},
			{"namespace": "type:", "examples": "bug, feature, task, chore", "purpose": "task categorization"},
			{"namespace": "priority:", "examples": "high, medium, low", "purpose": "optional prioritization"},
			{"namespace": "repo:<name>", "examples": "ATM:repo:atm", "purpose": "index task whose description says where to find the repo — repo to project binding in the label substrate"},
			{"namespace": "doc:<name>", "examples": "ATM:doc:architecture", "purpose": "index task pointing at a doc/resource and how to use it"},
			{"namespace": "context:always-read", "examples": "ATM:context:always-read", "purpose": "pointer to the always-read context markdown (replaces the deleted v1 Project Guide)"},
			{"namespace": "context:start-here", "examples": "ATM:context:start-here", "purpose": "the single entry-point task a fresh agent queries first; its description is the read this first pointer"},
			{"namespace": "claimed-by:<agent>", "examples": "ATM:claimed-by:claude", "purpose": "who's working on what — replaces v1 Claim; last-writer-wins, no conflict detection"},
			{"namespace": "blocks:<ID>, related:<ID>", "examples": "ATM:blocks:ATM-0002", "purpose": "task relationships via labels (replaces v1 Links)"},
		},
		"first_time_human_sequence": []string{
			"atm tui (auto-inits the store)",
			"create the project (Add in the Projects tab)",
			"create seed index tasks (start-here, repo:<name>, doc:<name>, context:always-read) and initial work tasks, labeling as you go",
		},
		"agent_first_contact_sequence": []string{
			"atm conventions — read the guide",
			"task list --project <CODE> --label <CODE>:context:start-here — get the entry-point pointer and follow it",
			"task list --project <CODE> --label <CODE>:repo:* / :doc:* / :context:* — discover index tasks for repos, docs, and always-read context",
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