// Package workflowrpi is the workflow_rpi capability: a manager-oriented
// perspective over every task in a project, shaped as four exclusive lanes
// (backlog, product, pipeline, reject) with lane-local status axes and a
// private link topology.
//
// The capability owns its labels and boards; task relationships live only in
// Task.Meta["workflow_rpi"]. It never reads another capability's metadata and
// never calls another capability's verbs.
package workflowrpi

// CapabilityName is both the capability identity and the metadata key this
// capability owns on a task.
const CapabilityName = "workflow_rpi"

// Label namespaces owned by workflow_rpi. The lane axis is exclusive; the
// others are lane-local and meaningful only inside their lane.
const (
	RPINamespace     = "rpi"
	ProductNamespace = "rpi-product"
	DevNamespace     = "rpi-dev"
	RejectNamespace  = "rpi-reject"
)

// Lanes. Absence of every lane label means the task is in RPI backlog/intake.
const (
	LaneProduct  = "product"
	LanePipeline = "pipeline"
	LaneReject   = "reject"
)

// Product-lane clarification statuses.
const (
	ProductUnclarified = "unclarified"
	ProductClarified   = "clarified"
)

// Pipeline-lane development statuses. There is no "blocked" status: blocking is
// derived from unresolved depends_on links, because a task can be planned and
// blocked at the same time.
const (
	DevClarified    = "clarified"
	DevBrainstormed = "brainstormed"
	DevPlanned      = "planned"
	DevImplementing = "implementing"
	DevReview       = "review"
	DevDone         = "done"
)

// Reject-lane reasons.
const (
	RejectDuplicate  = "duplicate"
	RejectOutOfScope = "out-of-scope"
	RejectNotWorthIt = "not-worth-it"
	RejectCoveredBy  = "covered-by"
)

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
