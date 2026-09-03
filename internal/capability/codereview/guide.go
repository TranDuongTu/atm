package codereview

import "atm/internal/capability"

// Summary is the one-line identity every enumeration surface shows.
func (Cap) Summary() string {
	return "Code review flow capability over finished implementation work: schedule a review against its pull request, run it, record where the report lives."
}

// Definition is codereview's structured self-description; `atm capability
// codereview guide` renders it.
func (Cap) Definition() capability.Definition {
	return capability.Definition{
		Identity: "Review of finished implementation work against a discoverable pull request. codereview tracks a review from scheduled through under-way to done, records the PR and report LOCATORS, and leaves tracked follow-up items for findings worth action beyond the artifact discussion. It never reads another capability's metadata and never calls another capability's verbs.",
		Lanes: []capability.LaneDoc{
			{Label: "<CODE>:codereview-inbox", Expr: "<project wiring> AND " + InboxSelfExclusion,
				Meaning: "finished work codereview has not decided about. The eligibility half is PROJECT WIRING, typically the upstream finish socket narrowed to the types worth reviewing."},
			{Label: "<CODE>:codereview-pipeline", Expr: pipelineExpr(),
				Meaning: "scheduled reviews, reviews under way, finished reviews, and the follow-up items reviews left behind."},
			{Label: "<CODE>:codereview-out-board", Expr: outExpr(),
				Meaning: "settled evictions, permanent until `release`."},
		},
		Axes: []capability.Axis{
			{Namespace: ClaimNamespace, Ordered: true, Meaning: "The CLAIM axis, which doubles as the state. Presence of any value means claimed by codereview.",
				Values: []capability.AxisValue{
					{Value: StateScheduled, Meaning: "a review is scheduled against a recorded pull request. A follow-up item is also born here: it is work the board has taken on."},
					{Value: StateReviewing, Meaning: "the review is under way."},
					{Value: StateDone, Meaning: "the finish socket: the review happened, and the report locator says where to read it."},
				}},
			{Namespace: OutNamespace, Meaning: "The EVICT axis; the value is the reason codereview declined the work.",
				Values: []capability.AxisValue{
					{Value: OutNotWarranted, Meaning: "the change does not merit a review."},
					{Value: OutSuperseded, Meaning: "a later change replaced this one before it was reviewed."},
				}},
		},
		Sockets: []capability.Socket{
			{Role: capability.SocketFinish, Label: "<CODE>:" + ClaimNamespace + ":" + StateDone,
				Meaning: "certifies that the review happened. It says nothing about the verdict — a review that requested changes is still a review that happened; the verdict lives on the artifact and in the report."},
			{Role: capability.SocketEvict, Label: "<CODE>:" + OutNamespace + ":*",
				Meaning: "any member means codereview considered the change and declined to review it."},
		},
		State: capability.StateDoc{
			Key:   CapabilityName,
			Intro: "Locators and the follow-up topology. The PR and the report are POINTERS, not content: the review conversation lives on the pull request and the report lives wherever the reviewer put it.",
			Fields: []capability.StateField{
				{Name: "pr", Meaning: "the pull-request locator — a URL or a number, as the team writes them. Required at absorb: work with no discoverable PR is LEFT in the inbox, and that is the warning."},
				{Name: "report", Meaning: "where the finished review's report lives."},
				{Name: "part_of", Meaning: "on a follow-up item: the review it came out of."},
				{Name: "follow_ups", Meaning: "on a review: the roster of items it left behind. Written together with the item's `part_of`, so neither end can dangle."},
			},
		},
		Invariants: []string{
			"`absorb` requires a pull request. A task with no discoverable PR is left in the inbox rather than scheduled against nothing.",
			"Follow-up items do not nest: an item cannot spawn items.",
			"An open follow-up does NOT block its review from finishing. A finding worth fixing but not worth blocking on belongs on the board, not in another round of review.",
			"The claim and evict axes are mutually exclusive.",
		},
		Converge: []string{
			"The inbox is empty, or every row in it carries a comment saying why it is deferred.",
			"Every scheduled review records its pull request, and every finished review records where its report lives.",
			"Every follow-up item points at a live review, and every item still open is one somebody still means to do.",
			"Every eviction carries a reason.",
		},
	}
}
