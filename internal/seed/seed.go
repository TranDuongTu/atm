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
// "<namespace>:<value>" or "<tag>" portion; the project code prefix is
// prepended at apply time. Description is the intention statement
// surfaced in the Labels tab and read by agents during first-contact.
type Label struct {
	Suffix      string
	Description string
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
var Labels = []Label{
	{"status:open", "workflow state: open; task is not started or is being considered"},
	{"status:in-progress", "workflow state: in-progress; someone is actively working on this"},
	{"status:done", "workflow state: done; task is complete"},
	{"status:blocked", "workflow state: blocked; task cannot proceed pending something else"},
	{"priority:high", "optional prioritization: high; do this first, everything untagged is default priority"},
	{"context:agent", "the task's description captures agent-direction notes for this project: build/test/lint commands, conventions, and gotchas a working agent must know"},
	{"context:repository", "the task's description names a code repository (path or URL), what it contains, and how to work in it; a later agent reads this to orient"},
	{"context:documentation", "the task's description points at a specific document (file path or URL) and summarizes what it covers, so a later agent can decide whether to read it"},
	{"context:question", "the task's description poses an open question or ambiguity about the project that a human or later agent should clarify; not a defect, not a work item, a gap in understanding"},
	{"comment:progress", "task comment kind: a progress note during work"},
	{"comment:decision", "task comment kind: a decision recorded during work"},
	{"comment:open-question", "task comment kind: an open question raised during work"},
}
