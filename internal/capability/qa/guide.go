package qa

import "atm/internal/capability"

// Summary is the one-line identity every enumeration surface shows.
func (Cap) Summary() string {
	return "QA flow capability over finished development work: absorb an original, verify it through scaffolds, certify it with evidence."
}

// Definition is qa's structured self-description; `atm capability qa guide`
// renders it.
func (Cap) Definition() capability.Definition {
	return capability.Definition{
		Identity: "Verification of finished development work. qa absorbs the ORIGINAL task, spawns test scaffolds beneath it, and certifies the original once the scaffolds have all passed. It never reads another capability's metadata and never calls another capability's verbs; it reaches upstream only through project wiring, and downstream only by stamping its own finish socket.",
		Lanes: []capability.LaneDoc{
			{Label: "<CODE>:qa-inbox", Expr: "<project wiring> AND " + InboxSelfExclusion,
				Meaning: "finished work qa has not decided about. The eligibility half is PROJECT WIRING, typically the upstream finish socket narrowed to the types qa verifies — epics and design units never arrive because that expression excludes them."},
			{Label: "<CODE>:qa-pipeline", Expr: pipelineExpr(),
				Meaning: "originals under verification and their scaffolds. A certified original stays claimed, and so stays visible here rather than vanishing."},
			{Label: "<CODE>:qa-out-board", Expr: outExpr(),
				Meaning: "settled evictions, permanent until `release`."},
		},
		Axes: []capability.Axis{
			{Namespace: ClaimNamespace, Ordered: true, Meaning: "The CLAIM axis, which doubles as the state. Presence of any value means claimed by qa.",
				Values: []capability.AxisValue{
					{Value: StateTesting, Meaning: "under verification."},
					{Value: StateDone, Meaning: "the finish socket: certified. Only ever stamped on an absorbed ORIGINAL, never a scaffold."},
				}},
			{Namespace: OutNamespace, Meaning: "The EVICT axis; the value is the reason qa declined or failed the work.",
				Values: []capability.AxisValue{
					{Value: OutFailed, Meaning: "verification found the work wrong. This is the one eviction that routes BACKWARD: it is the signal upstream reopens on."},
					{Value: OutNotRelevant, Meaning: "nothing here needs verifying."},
					{Value: OutCoveredBy, Meaning: "verified as part of another unit; requires `covered_by`."},
				}},
		},
		Sockets: []capability.Socket{
			{Role: capability.SocketFinish, Label: "<CODE>:" + ClaimNamespace + ":" + StateDone,
				Meaning: "certifies that the original passed verification. Only absorbed originals ever carry it, so downstream selection gets originals-only for free — nothing has to filter scaffolds out."},
			{Role: capability.SocketEvict, Label: "<CODE>:" + OutNamespace + ":*",
				Meaning: "any member means qa considered the work and declined or failed it."},
		},
		State: capability.StateDoc{
			Key:   CapabilityName,
			Intro: "The original/scaffold topology. An ORIGINAL is work qa absorbed from its inbox; a SCAFFOLD is a task qa created beneath one to hold a single verification effort. Scaffolds are born claimed, so they never appear in anyone's inbox.",
			Fields: []capability.StateField{
				{Name: "part_of", Meaning: "on a scaffold: the original it verifies. Its presence is what makes a task a scaffold."},
				{Name: "scaffolds", Meaning: "on an original: the roster of scaffolds beneath it. Written together with the scaffold's `part_of`, so neither end can dangle."},
				{Name: "covered_by", Meaning: "the unit whose verification covers this one; required by the `covered-by` eviction."},
			},
		},
		Invariants: []string{
			"Scaffolds do not nest: a scaffold cannot spawn scaffolds.",
			"The finish socket is refused on an original while any of its scaffolds still holds a claim, and the refusal names them.",
			"A scaffold that passes gives up its claim and keeps its `part_of` for history; it is never stamped done.",
			"The claim and evict axes are mutually exclusive.",
		},
		Converge: []string{
			"The inbox is empty, or every row in it carries a comment saying why it is deferred.",
			"Every absorbed original has the scaffolds its verification needs, and every scaffold points at a live original.",
			"Every original whose scaffolds have all passed is itself certified.",
			"Every eviction carries a reason, and a `covered-by` eviction carries `covered_by`.",
			"Every `failed` eviction newer than the last sweep has been routed upstream.",
		},
	}
}
