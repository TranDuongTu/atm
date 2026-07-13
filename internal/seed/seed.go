// Package seed holds the default label set applied when a project is
// created and re-applied on demand by `atm label seed` / the Labels tab
// [S] key. It is the single source of truth for the seeded label names
// and their descriptions (the agent code-of-conduct requires every
// seeded label to carry a description so a fresh agent reading
// `atm label list --project <CODE>` sees meaningful text immediately).
//
// This package holds data only — no store import. The store imports it
// and implements the apply logic (LabelSeed/SeedLabels) to avoid a
// circular dependency.
package seed

// Label is one default label to seed into a new project. Suffix is the
// "<namespace>:<value>", "<namespace>:*" (a namespace descriptor), or
// "<tag>" portion; the project code prefix is prepended at apply time.
// Description is the intention statement surfaced in the Labels tab and
// read by agents during first-contact. Expr, when non-empty, seeds a
// computed label (a board); no default board is seeded today, so the
// field exists for future use and zero-fills for every current entry.
type Label struct {
	Suffix      string
	Description string
	Expr        string
}

// Labels is the single source of truth for the default label set seeded
// on project create and re-applied by `atm label seed` / the Labels tab
// [S] key. Templated namespaces (repo:<name>, doc:<name>,
// claimed-by:<agent>, blocks:<ID>, related:<ID>) are intentionally NOT
// seeded as concrete labels — they depend on project-specific values
// and are created on demand.
//
// The seed is the minimum an agent needs to read on first-contact and
// the minimum a fresh project needs to be queryable: status (the core
// query axis), context (the bootstrapping substrate), comment (the
// narrative kinds an agent writes), and a single priority flag for
// "do this first". Everything else (type, fixit, stale, finer status
// granularity, medium/low priority) is invented on demand when an
// agent's intent genuinely diverges — per code-of-conduct rule #3.
//
// The four `:*` entries are namespace descriptors: they give a
// namespace its meaning record (a description) so a reader knows what
// the axis is. They are optional metadata, not a gate — an unseeded
// namespace (e.g. `type:*`) still works, it just surfaces as
// undescribed in the Boards pane. `type:*` is intentionally NOT seeded
// because `type` is invented on demand.
var Labels = []Label{
	{Suffix: "status:*", Description: "lifecycle state of a task; exactly one status label should be present"},
	{Suffix: "status:open", Description: "workflow state: open; task is not started or is being considered"},
	{Suffix: "status:in-progress", Description: "workflow state: in-progress; someone is actively working on this"},
	{Suffix: "status:done", Description: "workflow state: done; task is complete"},
	{Suffix: "status:blocked", Description: "workflow state: blocked; task cannot proceed pending something else"},
	{Suffix: "priority:*", Description: "optional urgency ranking; absent means default priority"},
	{Suffix: "priority:high", Description: "optional prioritization: high; do this first, everything untagged is default priority"},
	{Suffix: "context:*", Description: "index tasks whose description is the payload: agent directions, repos, docs, questions"},
	{Suffix: "context:agent", Description: "the task's description captures agent-direction notes for this project: build/test/lint commands, conventions, and gotchas a working agent must know"},
	{Suffix: "context:repository", Description: "the task's description names a code repository (path or URL), what it contains, and how to work in it; a later agent reads this to orient"},
	{Suffix: "context:documentation", Description: "the task's description points at a specific document (file path or URL) and summarizes what it covers, so a later agent can decide whether to read it"},
	{Suffix: "context:question", Description: "the task's description poses an open question or ambiguity about the project that a human or later agent should clarify; not a defect, not a work item, a gap in understanding"},
	{Suffix: "comment:*", Description: "the kinds of narrative an agent writes on a task"},
	{Suffix: "comment:progress", Description: "task comment kind: a progress note during work"},
	{Suffix: "comment:decision", Description: "task comment kind: a decision recorded during work"},
	{Suffix: "comment:open-question", Description: "task comment kind: an open question raised during work"},
}
