// Package scrum is the scrum flow capability: it absorbs raw work from the
// unclaimed pool and decomposes it into EPIC -> Stories -> Tasks/Bugs/Designs,
// carrying spec/plan locators and dependency topology in Meta["scrum"].
//
// The capability owns its labels and boards; task relationships live only in
// its own metadata key. It never reads another capability's metadata and never
// calls another capability's verbs — downstream capabilities reach it only
// through its declared sockets, wired as board expressions.
package scrum

// CapabilityName is both the capability identity and the metadata key this
// capability owns on a task.
const CapabilityName = "scrum"

// Label namespaces owned by scrum. scrum:* is the claim/type axis (exactly one
// value per claimed task); scrum-stage:* is the working stage; scrum-out:* is
// the evict axis. Presence of any scrum:* label means "claimed by scrum".
const (
	TypeNamespace  = "scrum"
	StageNamespace = "scrum-stage"
	OutNamespace   = "scrum-out"
)

// Types: the claim-axis values. A claimed task carries exactly one.
const (
	TypeEpic   = "epic"
	TypeStory  = "story"
	TypeTask   = "task"
	TypeBug    = "bug"
	TypeDesign = "design"
)

// Stages. StageDone is the finish socket value: it certifies the unit
// converged — a task when built, a story when all its children are done, an
// epic when all its stories are done.
const (
	StageBrainstormed = "brainstormed"
	StagePlanned      = "planned"
	// StageImplementable is the APPROVAL gate: a plan exists AND has been
	// approved, so the unit is ready to be picked up and built. planned
	// alone no longer means ready — a plan under discussion and a plan
	// signed off are different states, and dispatch has to tell them apart.
	StageImplementable = "implementable"
	StageImplementing  = "implementing"
	StageReview        = "review"
	StageDone          = "done"
)

// Evict reasons: the scrum-out:* axis values.
const (
	OutDuplicate  = "duplicate"
	OutOfScope    = "out-of-scope"
	OutNotWorthIt = "not-worth-it"
	OutCoveredBy  = "covered-by"
)

// Types and Stages enumerate the valid values for verb validation.
func Types() []string { return []string{TypeEpic, TypeStory, TypeTask, TypeBug, TypeDesign} }
func Stages() []string {
	return []string{StageBrainstormed, StagePlanned, StageImplementable, StageImplementing, StageReview, StageDone}
}

// OutReasons enumerates the valid evict reasons.
func OutReasons() []string {
	return []string{OutDuplicate, OutOfScope, OutNotWorthIt, OutCoveredBy}
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
