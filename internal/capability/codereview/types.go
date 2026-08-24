// Package codereview is the codereview flow capability: it absorbs finished
// implementation work against a discoverable pull request, tracks the review
// through scheduled -> reviewing -> done, and records the PR and report
// locators in Meta["codereview"].
//
// The capability owns its labels and boards. It never reads another
// capability's metadata and never calls another capability's verbs.
package codereview

// CapabilityName is both the capability identity and the metadata key this
// capability owns on a task.
const CapabilityName = "codereview"

// Label namespaces owned by codereview. codereview:* is the claim axis,
// codereview-out:* the evict axis.
const (
	ClaimNamespace = "codereview"
	OutNamespace   = "codereview-out"
)

// Claim-axis values. StateDone is the finish socket value.
const (
	StateScheduled = "scheduled"
	StateReviewing = "reviewing"
	StateDone      = "done"
)

// Evict reasons.
const (
	OutNotWarranted = "not-warranted"
	OutSuperseded   = "superseded"
)

// States enumerates the valid claim-axis values for verb validation.
func States() []string { return []string{StateScheduled, StateReviewing, StateDone} }

// OutReasons enumerates the valid evict reasons.
func OutReasons() []string { return []string{OutNotWarranted, OutSuperseded} }

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
