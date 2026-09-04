package scrum

import "atm/internal/capability"

// Summary is the one-line identity every enumeration surface shows.
func (Cap) Summary() string {
	return "Scrum flow capability over the unclaimed pool: claim work with a type, move it along a working stage, evict what does not belong."
}

// Definition is scrum's structured self-description; `atm capability scrum
// guide` renders it. What each word MEANS lives here. What to DO about a
// full inbox or a stalled unit lives in the project's checklists.
func (Cap) Definition() capability.Definition {
	return capability.Definition{
		Identity: "The first stage of the flow. Raw work reaches scrum's inbox and leaves it one of three ways: claimed into the pipeline with a type, evicted with a reason, or deliberately deferred. scrum never reads another capability's metadata and never calls another capability's verbs; downstream capabilities reach it only through its declared sockets, and only as project wiring.",
		Lanes: []capability.LaneDoc{
			{Label: "<CODE>:scrum-inbox", Expr: "<project wiring> AND " + InboxSelfExclusion,
				Meaning: "eligible work scrum has not decided about. The eligibility half is PROJECT WIRING (`atm project wiring set --capability scrum`); the tail is invariant and hides whatever scrum already claimed or evicted. By default eligibility is the unclaimed pool: work no enabled flow capability has taken."},
			{Label: "<CODE>:scrum-pipeline", Expr: pipelineExpr(),
				Meaning: "what scrum is building. Finished units stay visible: done is a state, not a disappearance."},
			{Label: "<CODE>:scrum-out-board", Expr: outExpr(),
				Meaning: "settled evictions, permanent until `release`. The `-board` suffix is not decoration — a bare `scrum-out` label would read as a member of the `scrum-out:*` namespace."},
		},
		Axes: []capability.Axis{
			{Namespace: TypeNamespace, Meaning: "The CLAIM/TYPE axis. Presence of any value means claimed by scrum; a claimed unit carries exactly one type. Only `story`, `task` and `bug` flow downstream — a design unit has no build to verify and no PR to review, and that exclusion lives in the DOWNSTREAM capability's wiring, not here.",
				Values: []capability.AxisValue{
					{Value: TypeEpic, Meaning: "product-level requirement, decomposed into stories. Does not flow downstream."},
					{Value: TypeStory, Meaning: "a portion of an epic's design, decomposed into tasks."},
					{Value: TypeTask, Meaning: "PR-sized implementation unit."},
					{Value: TypeBug, Meaning: "defect to fix."},
					{Value: TypeDesign, Meaning: "design or spec work whose deliverable is an executable plan. NEVER flows downstream."},
				}},
			{Namespace: StageNamespace, Ordered: true, Meaning: "The WORKING STAGE of a claimed unit.",
				Values: []capability.AxisValue{
					{Value: StageBrainstormed, Meaning: "the approach has been brainstormed."},
					{Value: StagePlanned, Meaning: "an implementation plan EXISTS — but has not been approved."},
					{Value: StageImplementable, Meaning: "the plan is APPROVED and the unit is ready to be built. Design approval is this transition, and it is the line dispatch reads."},
					{Value: StageImplementing, Meaning: "implementation is in progress."},
					{Value: StageReview, Meaning: "ready for, or under, review."},
					{Value: StageDone, Meaning: "the finish socket: built and converged."},
				}},
			{Namespace: OutNamespace, Meaning: "The EVICT axis; the value is the reason scrum declined the work.",
				Values: []capability.AxisValue{
					{Value: OutDuplicate, Meaning: "the same work is already tracked elsewhere; requires `covered_by`."},
					{Value: OutOfScope, Meaning: "real, but not this project's work."},
					{Value: OutNotWorthIt, Meaning: "considered and judged not worth doing."},
					{Value: OutCoveredBy, Meaning: "subsumed by another task; requires `covered_by`."},
				}},
		},
		Sockets: []capability.Socket{
			{Role: capability.SocketFinish, Label: "<CODE>:" + StageNamespace + ":" + StageDone,
				Meaning: "certifies that the unit converged — a task when it is built, a story when every live child is done, an epic when every live story is done. Downstream capabilities wire their inbox eligibility to this label."},
			{Role: capability.SocketEvict, Label: "<CODE>:" + OutNamespace + ":*",
				Meaning: "any member means scrum considered the work and declined it."},
		},
		State: capability.StateDoc{
			Key:   CapabilityName,
			Intro: "Unit topology and locators. Only OUTBOUND facts are stored: inbound views (a parent's children, a task's dependents) are derived by scanning the project, so they can never go stale against a child that moved.",
			Fields: []capability.StateField{
				{Name: "part_of", Meaning: "this unit's parent — a story's epic, a task's story. At most one."},
				{Name: "depends_on", Meaning: "execution dependencies. Blocking is DERIVED from these by the reporter, never stamped as a label: a unit can be planned and blocked at the same time."},
				{Name: "covered_by", Meaning: "the task that covers this one; required by the `covered-by` and `duplicate` evictions."},
				{Name: "spec", Meaning: "repo-relative or external locator for the spec. A pointer, not content."},
				{Name: "plan", Meaning: "locator for the implementation plan. A pointer, not content."},
			},
		},
		Invariants: []string{
			"The claim and evict axes are mutually exclusive: absorbing a task scrum evicted is refused until it is released.",
			"A claimed unit carries exactly one type and at most one stage.",
			"Stamping the finish socket on a story or an epic is REFUSED while any live child is undone; the refusal names the offenders.",
			"Links refuse self-links, cross-project links, and direct cycles.",
			"Nothing automatic ever points backward: rework is a new follow-up task, or the audited `reopen` + downstream `release` pair — never a label cycle.",
		},
		Converge: []string{
			"The inbox is empty, or every row in it carries a comment saying why it is deferred.",
			"Every claimed unit carries exactly one type and one stage, and the stage matches reality.",
			"Every child's `part_of` points at a live, claimed parent.",
			"Every parent whose live children are all done is itself stamped the finish socket — until it is, downstream sees nothing.",
			"Every eviction carries a reason, and `covered-by`/`duplicate` evictions carry `covered_by`.",
			"Every finished design unit records its `plan` locator.",
			"No `report` finding is left standing: each is resolved, or answered with a recorded decision.",
		},
	}
}
