package cli

import (
	"fmt"

	"atm/internal/seed"

	"github.com/spf13/cobra"
)

const conventionsText = `# ATM Conventions (advisory)

## What ATM is

ATM (Agent Tasks Management) is a label-substrate task store for AI agents and the humans steering them. A project holds tasks; each task carries free-form text (title, description) and a set of labels. There is no status field, no claim entity, no review queue, no links table, no state machine — status, type, priority, ownership, and relationships are all expressed as labels and interpreted by the agent reading them. Workflow lives in capabilities (internal/capability/workflow), not in the store; the store only keeps the substrate legible. A capability is a paved road, not a fence — a project can replace it.

## Where labels live

Labels are global and per-project. The seeded defaults (status, priority, context, comment) are written into every new project by ` + "`atm project create`" + ` and re-applied idempotently by ` + "`atm label seed --project <CODE>`" + ` / the Boards pane [S] key. The full, authoritative list with descriptions is in the store — do not memorize or duplicate it here. To see the labels that exist in this project, run ` + "`atm label list --project <CODE>`" + `. Each label carries a description; that description is the label's intention record.

## Where tasks live

Tasks live in the store, scoped to a project, as entries in the project's append-only event log (` + "`events.v2.jsonl`" + `) — there is no per-task file. ` + "`atm task list --project <CODE>`" + ` lists them. Every new project is born on this format, so its tasks get hex IDs like ` + "`<CODE>-<hex>`" + `; a project imported from a legacy v1 ` + "`log.jsonl`" + ` via ` + "`atm store upgrade`" + ` keeps its old numeric ids (` + "`<CODE>-<NNNN>`" + `) and mints hex ones for new tasks. Each task has an ID, a title, a description, and a label set. The description is free-form text — agents write what they are doing, what they found, what they decided, so the next agent or human can pick up where they left off.

## How to read a task and its labels

A task is read as: its title (one line of intent), its description (the running narrative), and its labels (the faceted classification). The labels are the query surface; the description is the working memory. A ` + "`context:agent`" + ` task's description holds agent directions for this project. A ` + "`context:repository`" + ` task points at a repo to read. A ` + "`context:documentation`" + ` task points at a doc to read. ` + "`status:open`" + ` means not done; ` + "`status:in-progress`" + ` means someone is on it; ` + "`status:done`" + ` means stop. Labels are advisory — nothing in the store enforces them, but reading them is how you understand a task without re-reading its whole history.

## How to narrate progress

Comments are the running narrative on a task: a progress note, a decision, an open question, a bug, a pointer to a design doc. Comments live in the store as a per-task append-mostly thread. Add a comment with ` + "`atm task comment add --task <ID> --body \"<text>\" --label <CODE>:<kind>`" + `. The label is the classification — ` + "`ATM:comment:progress`" + `, ` + "`ATM:comment:decision`" + `, ` + "`ATM:comment:open-question`" + ` (the seeded kinds); invent others only when none fits. Comments support ` + "`--reply-to <COMMENT-ID>`" + ` for threading within the same task. Edit a comment's body with ` + "`atm task comment set-body`" + `; remove one with ` + "`atm task comment remove`" + `.

## How to search

Use labels as the filter. ` + "`atm task list --project <CODE> --label <CODE>:<ns>:<value>`" + ` returns tasks carrying that exact label. Wildcard labels (e.g. ` + "`<CODE>:status:*`" + `) drive faceted grouping via ` + "`atm task list --facets`" + `. Combine labels to narrow: ` + "`--label <CODE>:status:open --label <CODE>:priority:high`" + ` is high-priority open work. A board name is also a valid ` + "`--label`" + ` value: ` + "`--label <CODE>:next-sprint`" + ` returns every task whose labels satisfy that board's expression.

## Boards (computed labels)

A label may carry an expression over other labels. Such a label is a BOARD:
its membership is computed, not asserted. ` + "`ATM:next-sprint`" + ` with the
expression ` + "`status:open AND sprint:next`" + ` matches every open task in the next
sprint, and nothing carries that label directly.

Three kinds of label:
- stored     ATM:status:open   tasks assert it
- namespace  ATM:status:*      any label with that prefix; emergent; describable
- board      ATM:next-sprint   has an expression; computed; cannot be assigned

Expressions use AND / OR / NOT / parentheses over bare label names (the
project prefix is implied). Boards may reference other boards. ` + "`NOT status:*`" + `
means "carries no status label" — i.e. untriaged.

Agents: author boards for the groupings your project actually needs, and give
each one a description — the description is what the next agent reads to know
what the board means. Boards are how a human asks for "the release board"
without knowing the label vocabulary.

The Boards tab is the human's review surface. A namespace with no description
shows a warning there: an agent introduced it but did not explain why.

## The context map

Context pointers record what they were derived from, so drift can be detected. ` + "`atm context check --project <CODE>`" + ` reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means.

Record a pointer with ` + "`atm context add`" + `; re-verify one with ` + "`atm context stamp`" + `; repoint a subject that moved with ` + "`atm context retarget`" + `; retire one whose subject died with ` + "`atm context supersede`" + `. These verbs own the context vocabulary — do not hand-assign the labels or hand-edit a provenance comment.

## Workflow verbs (status transitions)

Status transitions live in the ` + "`internal/capability/workflow`" + ` capability, exposed as four mutating verbs — ` + "`atm workflow start`" + ` (in-progress), ` + "`atm workflow open`" + `, ` + "`atm workflow block`" + ` (blocked), ` + "`atm workflow complete`" + ` (done) — plus a read-only ` + "`atm workflow status`" + ` reporter and ` + "`atm workflow seed`" + ` to ensure the boards. Each mutating verb swaps the task's ` + "`status:*`" + ` label (adds the target, then removes any other), so exactly-one-status is an invariant the capability maintains. The store still enforces nothing: raw ` + "`atm task label add/remove --label <CODE>:status:<value>`" + ` works and a human may hand-assign, rename, or delete any status label. ` + "`internal/capability/workflow`" + ` is a paved road, not a fence — a project can replace it with a different transition model.

Three boards are ensured on project create / label seed / TUI use: ` + "`ATM:backlog`" + ` (` + "`NOT status:*`" + ` — untriaged jottings), ` + "`ATM:open-tasks`" + ` (` + "`status:open`" + ` — active work, the TUI default), ` + "`ATM:in-progress-tasks`" + ` (` + "`status:in-progress`" + `). In an older project where a board is absent, the expression fallback applies (` + "`--label <CODE>:status:open`" + ` etc.).

## Actor identity

Every mutation (task, comment, label, project, persona, config) stamps an actor of the form ` + "`persona@agent:model`" + ` (e.g. ` + "`developer@claude:opus-4.8`" + `). The store rejects anything else: all three segments must be non-empty, and the ` + "`persona`" + ` segment must be a registered persona — create it first with ` + "`atm persona create --name <name> --prompt \"...\"`" + `. The built-in personas ` + "`developer`" + `, ` + "`manager`" + `, and ` + "`admin`" + ` (the human operator) are seeded automatically on first use and cannot be removed. The ` + "`agent`" + ` and ` + "`model`" + ` segments are supplied by the host agent: autonomous sessions stamp their real ` + "`agent:model`" + `, while a human driving the CLI or TUI directly defaults to ` + "`admin@cli:unset`" + ` / ` + "`admin@tui:unset`" + ` (the ` + "`unset`" + ` model placeholder stays until real model configuration arrives). An unresolved (empty) persona renders as ` + "`(none)`" + ` in activity views. Start a developer session with a persona via ` + "`atm dev --project <CODE> --persona <name>`" + `, which injects the persona's prompt + description into the session context and defaults the actor to ` + "`<persona>@<agent>:unset`" + `; the host agent replaces ` + "`:unset`" + ` with its real model. Legacy actor strings in older logs are inferred to a persona at read time by ` + "`atm activity`" + ` — there is no migration step and no alias table. See who has been doing what with ` + "`atm activity --project <CODE> [--group-by persona|agent|model]`" + `, or the TUI's Projects pane (press ` + "`P`" + ` to expand the \"activity by persona\" chart into an overlay; ` + "`p`" + ` adds a persona).

## Agent first-contact sequence

1. Read this guide, including the code-of-conduct below.
2. ` + "`atm label list --project <CODE>`" + ` — read every label's description first to learn the project's vocabulary before touching tasks. Labels are the project's language; knowing them makes every task query meaningful. Do not assume the seeded defaults are all the labels this project uses.
3. ` + "`atm task list --project <CODE> --label <CODE>:context-current`" + ` — the project's current knowledge: agent directions, repository pointers, and documentation pointers that have not been superseded. Read this board rather than the raw ` + "`context:*`" + ` namespace; membership is computed, so it is always the latest. Narrow by kind with an extra ` + "`--label <CODE>:context:agent`" + `.
4. ` + "`atm task list --project <CODE> --label <CODE>:open-tasks`" + ` — get open work. The open-tasks board (expression status:open) is ensured on project create / label seed / TUI use; in an older project where it is absent, ` + "`--label <CODE>:status:open`" + ` is equivalent.
5. ` + "`atm workflow status --task <ID>`" + ` / ` + "`atm workflow start --task <ID>`" + ` — the paved road for status transitions; prefer these over raw ` + "`task label add/remove --label status:*`" + `. Use ` + "`atm task list --project <CODE> --label <CODE>:backlog`" + ` to review untriaged jottings (in an older project, ` + "`--expr 'NOT status:*'`" + ` is equivalent).
6. ` + "`atm store log <CODE>`" + ` — read the project's append-only audit log to observe recent activity.
7. ` + "`atm task comment list --task <ID>`" + ` — read the running narrative on a task before acting on it.
8. ` + "`atm index models --project <CODE>`" + ` — see which embedding models have a vector index. If none, configure one with ` + "`atm project set-embedding`" + ` then ` + "`atm embed --project <CODE>`" + `.
9. ` + "`atm search --project <CODE> \"query\"`" + ` — semantic search over tasks + comments (text fallback if no index).
10. To get a synthesized answer, run a manager recall session with ` + "`atm manage --project <CODE> --recall`" + `.

A fresh agent that does not yet know the project's namespaces runs the label-list step first and follows the descriptions.

First run setup: ` + "`atm init`" + ` initializes the store, installs selected ATM agent plugins, records the default agent, and can save default host-agent args. Then run ` + "`atm manage --project <CODE> --mapping`" + ` from a repository to let the manager learn and organize the project. Day-to-day development starts with ` + "`atm dev --project <CODE>`" + `. Override the agent for a single launch with ` + "`--agent <name>`" + ` or ` + "`ATM_AGENT`" + `. To pass per-agent flags, append them after ` + "`--`" + ` (e.g. ` + "`atm dev --project ATM -- --yolo`" + `), or set per-agent defaults with ` + "`atm agents args <name> -- <flags>`" + `. Manager work starts with ` + "`atm manage --project <CODE> --curate|--recall|--mapping`" + `. Use ` + "`atm agents list|select|args`" + ` later to inspect readiness or change agent defaults after setup.

## Agent code-of-conduct (label hygiene)

Agents working in an ATM project follow these rules to keep the label substrate legible for humans and other agents:

1. Read before you write. Run ` + "`atm label list --project <CODE>`" + ` and read every label's description before introducing any new label. The existing labels are the project's vocabulary; reuse them whenever one fits your intent.
2. Default setup is the baseline. The seeded labels (status, priority, context, comment) cover the common cases. Prefer them. Do not reinvent ` + "`status:open`" + ` as ` + "`state:open`" + ` or ` + "`wf:open`" + `.
3. Invent only when nothing fits. If no existing label captures your intent, you may create a new one — agents are free to self-organize. But before you do, ask yourself: would a human reviewing the Boards pane understand why this label exists?
4. State the intention in the label description. When you create a new label, also call ` + "`atm label add --name <CODE>:<ns>:<value> --description \"<one sentence: why this label exists>\"`" + `. The description is the intention record. A label with no description is a flag for human review: "agent introduced this but didn't explain why."
5. One label, one meaning. Don't use the same label string to mean different things across tasks. If your intent diverges from an existing label's description, create a new label with a distinct name and a description that distinguishes it.
6. Author boards for real groupings. A board is a label with an expression over other labels; its membership is computed, not asserted. Create one with ` + "`atm label add --name <CODE>:<board> --description \"<what this board is for>\" --expr \"<expression>\"`" + `. Give every board a description — the description is what the next agent reads to know what the board means. Never assign a board label directly to a task.
7. Humans reconcile. The Boards pane is the human's review surface. If you see labels that overlap, contradict, or lack descriptions, edit or remove them there. Agents follow the rules above; humans curate.

## First-time human sequence

1. ` + "`atm`" + ` (auto-inits the store and opens the TUI).
2. Create the project (Add in the Projects tab). Project create auto-seeds the default labels with descriptions, so the Boards pane is populated from the start.
3. Create seed index tasks (` + "`context:agent`" + `, ` + "`context:repository`" + `, ` + "`context:documentation`" + `) and initial work tasks, labeling as you go. The human curates labels in the Boards pane.

## Notes

- Re-seeding defaults: ` + "`atm label seed --project <CODE>`" + ` or the Boards pane [S] key re-applies the default set idempotently — existing descriptions are preserved, and any new defaults introduced in a release are added.
- Extra agent args: pass host-agent flags after ` + "`--`" + ` (e.g. ` + "`atm dev --project <CODE> -- --yolo`" + `); persistent per-agent defaults may be set with ` + "`atm agents args <name> -- <flags>`" + ` or via ` + "`ATM_<AGENT>_ARGS`" + `. ATM passes args through verbatim and does not validate them.
- ` + "`atm store upgrade --project <CODE>`" + ` (or ` + "`--all`" + `) — import a project's stray legacy v1 ` + "`log.jsonl`" + ` into ` + "`events.v2.jsonl`" + `; the old log is left untouched during import.
- ` + "`atm store prune-v1 --project <CODE>`" + ` (or ` + "`--all`" + `) — after verifying an import, retire the leftover v1 log (archives by default, ` + "`--delete`" + ` to remove it). There is no rollback.
- ` + "`atm store set-format --format v1|v2`" + ` — override only the birth/default format (v2 refused while any project lacks an explicit entry).

Conventions are advisory only — nothing in the store validates or special-cases the documented namespaces.
`

func conventionsStructured() map[string]any {
	codeOfConduct := []string{
		"Read before you write. Run atm label list --project <CODE> and read every label's description before introducing any new label.",
		"Default setup is the baseline. Prefer seeded labels; do not reinvent them under new names.",
		"Invent only when nothing fits. Agents are free to self-organize, but a human reviewing the Boards pane should understand why a new label exists.",
		"State the intention in the label description. A label with no description is a flag for human review.",
		"One label, one meaning. If intent diverges from an existing label's description, create a new label with a distinct name.",
		"Author boards for real groupings. A board is a label with an expression over other labels; its membership is computed, not asserted. Create one with atm label add --name <CODE>:<board> --description \"<what this board is for>\" --expr \"<expression>\". Give every board a description; never assign a board label directly to a task.",
		"Humans reconcile. The Boards pane is the human's review surface for overlapping, contradictory, or undescribed labels.",
	}
	seeded := make([]map[string]string, 0, len(seed.Labels))
	for _, l := range seed.Labels {
		seeded = append(seeded, map[string]string{"suffix": l.Suffix, "description": l.Description})
	}
	return map[string]any{
		"what_atm_is":                       "ATM (Agent Tasks Management) is a label-substrate task store for AI agents and the humans steering them. A project holds tasks; each task carries free-form text (title, description) and a set of labels. There is no status field, no claim entity, no review queue, no links table, no state machine — status, type, priority, ownership, and relationships are all expressed as labels and interpreted by the agent reading them. Workflow lives in capabilities (internal/capability/workflow), not in the store; the store only keeps the substrate legible. A capability is a paved road, not a fence — a project can replace it.",
		"where_labels_live":                 "Labels are global and per-project. The seeded defaults (status, priority, context, comment) are written into every new project by atm project create and re-applied idempotently by atm label seed --project <CODE> / the Boards pane [S] key. The full, authoritative list with descriptions is in the store — do not memorize or duplicate it here. To see the labels that exist in this project, run atm label list --project <CODE>. Each label carries a description; that description is the label's intention record.",
		"where_tasks_live":                  "Tasks live in the store, scoped to a project, as entries in the project's append-only event log (events.v2.jsonl) — there is no per-task file. atm task list --project <CODE> lists them. Every new project is born on this format, so its tasks get hex IDs like <CODE>-<hex>; a project imported from a legacy v1 log.jsonl via atm store upgrade keeps its old numeric ids (<CODE>-<NNNN>) and mints hex ones for new tasks. Each task has an ID, a title, a description, and a label set. The description is free-form text — agents write what they are doing, what they found, what they decided, so the next agent or human can pick up where they left off.",
		"how_to_read_a_task_and_its_labels": "A task is read as: its title (one line of intent), its description (the running narrative), and its labels (the faceted classification). The labels are the query surface; the description is the working memory. A context:agent task's description holds agent directions for this project. A context:repository task points at a repo to read. A context:documentation task points at a doc to read. status:open means not done; status:in-progress means someone is on it; status:done means stop. Labels are advisory — nothing in the store enforces them, but reading them is how you understand a task without re-reading its whole history.",
		"how_to_search":                     "Use labels as the filter. atm task list --project <CODE> --label <CODE>:<ns>:<value> returns tasks carrying that exact label. Wildcard labels (e.g. <CODE>:status:*) drive faceted grouping via atm task list --facets. Combine labels to narrow: --label <CODE>:status:open --label <CODE>:priority:high is high-priority open work. A board name is also a valid --label value: --label <CODE>:next-sprint returns every task whose labels satisfy that board's expression.",
		"boards":                            "A label may carry an expression over other labels. Such a label is a BOARD: its membership is computed, not asserted. ATM:next-sprint with the expression status:open AND sprint:next matches every open task in the next sprint, and nothing carries that label directly. Three kinds of label: stored (ATM:status:open — tasks assert it), namespace (ATM:status:* — any label with that prefix; emergent; describable), board (ATM:next-sprint — has an expression; computed; cannot be assigned). Expressions use AND / OR / NOT / parentheses over bare label names (the project prefix is implied). Boards may reference other boards. NOT status:* means carries no status label — i.e. untriaged. Agents author boards for the groupings the project actually needs, and give each one a description — the description is what the next agent reads to know what the board means. Boards are how a human asks for the release board without knowing the label vocabulary. The Boards pane is the human's review surface; a namespace with no description shows a warning there.",
		"how_to_narrate_progress":           "Comments are the running narrative on a task: a progress note, a decision, an open question, a bug, a pointer to a design doc. Comments live in the store as a per-task append-mostly thread. Add a comment with atm task comment add --task <ID> --body <text> --label <CODE>:<kind>. The label is the classification (ATM:comment:progress, ATM:comment:decision, ATM:comment:open-question are the seeded kinds; invent others only when none fits). Comments support --reply-to <COMMENT-ID> for threading within the same task. Edit a comment's body with atm task comment set-body; remove one with atm task comment remove.",
		"code_of_conduct":                   codeOfConduct,
		"workflow_verbs":                    "Status transitions live in the internal/capability/workflow capability, exposed as four mutating verbs: atm workflow start (in-progress), atm workflow open, atm workflow block (blocked), atm workflow complete (done), plus a read-only atm workflow status reporter and atm workflow seed. Each mutating verb swaps the task's status:* label (adds the target, then removes any other), so exactly-one-status is an invariant the capability maintains. The store enforces nothing; raw atm task label add/remove --label <CODE>:status:<value> still works. internal/capability/workflow is a paved road, not a fence — a project can replace it.",
		"actor_identity":                    "Every mutation (task, comment, label, project, persona, config) stamps an actor of the form persona@agent:model (e.g. developer@claude:opus-4.8). The store rejects anything else: all three segments must be non-empty, and the persona segment must be a registered persona — create it first with atm persona create --name <name> --prompt \"...\". The built-in personas developer, manager, and admin (the human operator) are seeded automatically on first use and cannot be removed. The agent and model segments are supplied by the host agent: autonomous sessions stamp their real agent:model, while a human driving the CLI or TUI directly defaults to admin@cli:unset / admin@tui:unset (the unset model placeholder stays until real model configuration arrives). An unresolved (empty) persona renders as (none) in activity views. Start a developer session with a persona via atm dev --project <CODE> --persona <name>, which injects the persona's prompt + description into the session context and defaults the actor to <persona>@<agent>:unset; the host agent replaces :unset with its real model. Legacy actor strings in older logs are inferred to a persona at read time by atm activity — there is no migration step and no alias table. See who has been doing what with atm activity --project <CODE> [--group-by persona|agent|model], or the TUI's Projects pane (press P to expand the \"activity by persona\" chart into an overlay; p adds a persona).",
		"seeded_labels":                     seeded,
		"first_time_human_sequence": []string{
			"atm (auto-inits the store and opens the TUI)",
			"create the project (Add in the Projects tab); project create auto-seeds the default labels with descriptions",
			"create seed index tasks (context:agent, context:repository, context:documentation) and initial work tasks, labeling as you go",
		},
		"agent_first_contact_sequence": []string{
			"read this guide, including the code-of-conduct",
			"atm label list --project <CODE> — read every label's description first to learn the project's vocabulary before touching tasks",
			"atm task list --project <CODE> --label <CODE>:context-current — the project's current knowledge: agent directions, repository pointers, and documentation pointers that have not been superseded. Read this board rather than the raw context:* namespace; membership is computed, so it is always the latest. Narrow by kind with an extra --label <CODE>:context:agent.",
			"atm task list --project <CODE> --label <CODE>:open-tasks — get open work. The open-tasks board (expression status:open) is ensured on project create / label seed / TUI use; in an older project where it is absent, --label <CODE>:status:open is equivalent.",
			"atm workflow status --task <ID> / atm workflow start --task <ID> — the paved road for status transitions; prefer these over raw task label add/remove --label status:*. Use atm task list --project <CODE> --label <CODE>:backlog to review untriaged jottings (in an older project, --expr 'NOT status:*' is equivalent).",
			"atm store log <CODE> — read the project's append-only audit log to observe recent activity",
			"atm task comment list --task <ID> — read the running narrative on a task before acting on it",
			"atm index models --project <CODE> — see which embedding models have a vector index; if none, configure one with atm project set-embedding then atm embed --project <CODE>",
			"atm search --project <CODE> \"query\" — semantic search over tasks + comments (text fallback if no index)",
			"to get a synthesized answer, run a manager recall session with atm manage --project <CODE> --recall",
		},
		"context_map":            "Context pointers record what they were derived from, so drift can be detected. atm context check --project <CODE> reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means. Record a pointer with atm context add; re-verify one with atm context stamp; repoint a subject that moved with atm context retarget; retire one whose subject died with atm context supersede. These verbs own the context vocabulary — do not hand-assign the labels or hand-edit a provenance comment.",
		"day_to_day_development": "First run setup: atm init initializes the store, installs selected ATM agent plugins, records the default agent, and can save default host-agent args. Then run atm manage --project <CODE> --mapping from a repository to let the manager learn and organize the project. Day-to-day development starts with atm dev --project <CODE>. Override the agent for a single launch with --agent <name> or ATM_AGENT. To pass per-agent flags, append them after -- (e.g. atm dev --project ATM -- --yolo), or set per-agent defaults with atm agents args <name> -- <flags>. Manager work starts with atm manage --project <CODE> --curate|--recall|--mapping. Use atm agents list|select|args later to inspect readiness or change agent defaults after setup.",
		"advisory":               "Conventions are advisory only — nothing in the store validates or special-cases the documented namespaces.",
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
