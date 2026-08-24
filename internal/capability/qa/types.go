// Package qa is the qa flow capability: it absorbs finished development work
// as ORIGINALS, spawns born-claimed test scaffolds beneath them, and certifies
// the original — never a scaffold — when the scaffolds have all passed.
//
// The capability owns its labels and boards; scaffold topology lives only in
// Meta["qa"]. It never reads another capability's metadata and never calls
// another capability's verbs.
package qa

// CapabilityName is both the capability identity and the metadata key this
// capability owns on a task.
const CapabilityName = "qa"

// Label namespaces owned by qa. qa:* is the claim axis, qa-out:* the evict
// axis. Presence of any qa:* label means "claimed by qa".
const (
	ClaimNamespace = "qa"
	OutNamespace   = "qa-out"
)

// Claim-axis values. StateDone is the finish socket value, and it is only ever
// stamped on an absorbed original.
const (
	StateTesting = "testing"
	StateDone    = "done"
)

// Evict reasons. A `failed` eviction is the backward-flow signal the manager
// routes — it says verification found something, not that qa lost interest.
const (
	OutFailed      = "failed"
	OutNotRelevant = "not-relevant"
	OutCoveredBy   = "covered-by"
)

// States enumerates the valid claim-axis values for verb validation.
func States() []string { return []string{StateTesting, StateDone} }

// OutReasons enumerates the valid evict reasons.
func OutReasons() []string { return []string{OutFailed, OutNotRelevant, OutCoveredBy} }

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
