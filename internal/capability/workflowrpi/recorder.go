package workflowrpi

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// Service is what the recorder needs from the store: task reads/writes, the
// capability metadata mutator, and comments for the release audit trail.
// core.Service and *store.Store both satisfy it.
type Service interface {
	core.TaskService
	CreateComment(taskID, body string, labels []string, replyTo, actor string) (*core.Comment, error)
}

// Recorder is the mutating side of the workflow_rpi capability. It maintains
// the exactly-one-lane invariant, the lane-local status axes, and the
// private link payload; the store itself enforces nothing (paved road, not
// a fence).
type Recorder struct {
	Store Service
	Actor string
}

// taskPayload reads the task and decodes this capability's payload. A
// malformed payload is an error — verbs never overwrite state they cannot
// read (hand-repair via the raw metadata surface instead).
func (r *Recorder) taskPayload(taskID string) (*core.Task, *Payload, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return nil, nil, err
	}
	pl, err := DecodePayload(tk.Meta[CapabilityName])
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", taskID, err)
	}
	return tk, pl, nil
}

func (r *Recorder) writePayload(taskID string, pl *Payload) error {
	s, err := pl.Encode()
	if err != nil {
		return err
	}
	return r.Store.SetTaskCapabilityMeta(taskID, CapabilityName, s, r.Actor)
}

// labelValues collects the task's labels in one of this capability's
// namespaces: full names and bare values, in store order (sorted).
func labelValues(tk *core.Task, code, ns string) (existing []string, vals []string) {
	prefix := code + ":" + ns + ":"
	for _, l := range tk.Labels {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		existing = append(existing, l)
		vals = append(vals, strings.TrimPrefix(l, prefix))
	}
	return
}

// laneState reports the task's lane labels, whether target is already among
// them, and the prior lane for reporting ("" means RPI backlog — the unset
// set, which is a real state and not a missing one).
func laneState(tk *core.Task, code, target string) (existing []string, vals []string, hasTarget bool, prior string) {
	existing, vals = labelValues(tk, code, RPINamespace)
	for _, v := range vals {
		if v == target {
			hasTarget = true
		} else if prior == "" {
			prior = v
		}
	}
	return
}

// swapNamespaceLabels makes target the only label in ns. Add-first, then
// remove the rest: no transactions here, so the worst case is a recoverable
// extra label and re-running converges (the recovery behavior).
func (r *Recorder) swapNamespaceLabels(taskID, code, ns, target string) error {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return err
	}
	existing, vals := labelValues(tk, code, ns)
	targetLabel := code + ":" + ns + ":" + target
	if len(existing) == 1 && vals[0] == target {
		return nil
	}
	if !containsString(vals, target) {
		if err := r.Store.TaskLabelAdd(taskID, targetLabel, r.Actor); err != nil {
			return fmt.Errorf("add %s: %w", targetLabel, err)
		}
	}
	for _, l := range existing {
		if l == targetLabel {
			continue
		}
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return fmt.Errorf("remove %s: %w", l, err)
		}
	}
	return nil
}

// clearNamespace removes every label this capability owns in ns. Used to
// converge lane-local axes that no longer apply after a lane change.
func (r *Recorder) clearNamespace(taskID, code, ns string) error {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return err
	}
	existing, _ := labelValues(tk, code, ns)
	for _, l := range existing {
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return fmt.Errorf("remove %s: %w", l, err)
		}
	}
	return nil
}

func validProductStatus(status string) bool {
	return containsString([]string{ProductUnclarified, ProductClarified}, status)
}

func validDevStatus(status string) bool {
	return containsString([]string{
		DevClarified, DevBrainstormed, DevPlanned, DevImplementing, DevReview, DevDone,
	}, status)
}

func validRejectReason(reason string) bool {
	return containsString([]string{
		RejectDuplicate, RejectOutOfScope, RejectNotWorthIt, RejectCoveredBy,
	}, reason)
}

// laneEntry is the common prologue of every lane verb: it proves the task
// exists, proves this capability's payload is readable (never overwrite
// state we cannot read), and resolves the project code.
func (r *Recorder) laneEntry(taskID string) (*core.Task, *Payload, string, error) {
	tk, pl, err := r.taskPayload(taskID)
	if err != nil {
		return nil, nil, "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return nil, nil, "", fmt.Errorf("invalid task id %q", taskID)
	}
	return tk, pl, code, nil
}

// Product moves the task into the product-roadmap lane with a clarification
// status (default: unclarified) and converges the lane-local axes that no
// longer apply. Returns the prior lane ("" for backlog).
func (r *Recorder) Product(taskID, status string) (string, error) {
	if status == "" {
		status = ProductUnclarified
	}
	if !validProductStatus(status) {
		return "", fmt.Errorf("invalid product status %q (want %s or %s)", status, ProductUnclarified, ProductClarified)
	}
	tk, _, code, err := r.laneEntry(taskID)
	if err != nil {
		return "", err
	}
	_, _, _, prior := laneState(tk, code, LaneProduct)
	if err := r.swapNamespaceLabels(taskID, code, RPINamespace, LaneProduct); err != nil {
		return prior, err
	}
	if err := r.swapNamespaceLabels(taskID, code, ProductNamespace, status); err != nil {
		return prior, err
	}
	for _, ns := range []string{DevNamespace, RejectNamespace} {
		if err := r.clearNamespace(taskID, code, ns); err != nil {
			return prior, err
		}
	}
	return prior, nil
}

// Pipeline moves the task into the build-pipeline lane with a dev status
// (default: clarified). It refuses unless productID is a task in the same
// project already carrying the product lane label — manager intent stays
// explicit, and the pipeline never grows an implicit roadmap.
func (r *Recorder) Pipeline(taskID, productID, status string) (string, error) {
	if status == "" {
		status = DevClarified
	}
	if !validDevStatus(status) {
		return "", fmt.Errorf("invalid dev status %q", status)
	}
	tk, _, code, err := r.laneEntry(taskID)
	if err != nil {
		return "", err
	}
	if productID == "" {
		return "", fmt.Errorf("pipeline requires a product parent (--product)")
	}
	product, err := r.Store.GetTask(productID)
	if err != nil {
		return "", fmt.Errorf("product parent %s: %w", productID, err)
	}
	if !containsString(product.Labels, code+":"+RPINamespace+":"+LaneProduct) {
		return "", fmt.Errorf("%s is not a product parent (stamp it with the product verb first)", productID)
	}
	if err := r.SetProductOf(taskID, productID); err != nil {
		return "", err
	}
	_, _, _, prior := laneState(tk, code, LanePipeline)
	if err := r.swapNamespaceLabels(taskID, code, RPINamespace, LanePipeline); err != nil {
		return prior, err
	}
	if err := r.swapNamespaceLabels(taskID, code, DevNamespace, status); err != nil {
		return prior, err
	}
	for _, ns := range []string{ProductNamespace, RejectNamespace} {
		if err := r.clearNamespace(taskID, code, ns); err != nil {
			return prior, err
		}
	}
	return prior, nil
}

// Reject moves the task into the reject lane with a reason (default:
// not-worth-it). The covered-by reason requires a covering task; supplying
// one with any other reason is allowed, and supplying none clears a stale
// pointer.
func (r *Recorder) Reject(taskID, reason, coveredBy string) (string, error) {
	if reason == "" {
		reason = RejectNotWorthIt
	}
	if !validRejectReason(reason) {
		return "", fmt.Errorf("invalid reject reason %q", reason)
	}
	if reason == RejectCoveredBy && coveredBy == "" {
		return "", fmt.Errorf("reject reason %s requires --covered-by", RejectCoveredBy)
	}
	tk, pl, code, err := r.laneEntry(taskID)
	if err != nil {
		return "", err
	}
	if coveredBy != "" {
		if err := r.SetCoveredBy(taskID, coveredBy); err != nil {
			return "", err
		}
	} else if pl.CoveredBy() != "" {
		if err := r.ClearCoveredBy(taskID, ""); err != nil {
			return "", err
		}
	}
	_, _, _, prior := laneState(tk, code, LaneReject)
	if err := r.swapNamespaceLabels(taskID, code, RPINamespace, LaneReject); err != nil {
		return prior, err
	}
	if err := r.swapNamespaceLabels(taskID, code, RejectNamespace, reason); err != nil {
		return prior, err
	}
	for _, ns := range []string{ProductNamespace, DevNamespace} {
		if err := r.clearNamespace(taskID, code, ns); err != nil {
			return prior, err
		}
	}
	return prior, nil
}

// Release returns the task to RPI backlog: every label this capability owns
// goes, the whole RPI payload goes, and the reason is recorded as a comment.
// Labels and metadata owned by other capabilities are untouched — RPI only
// ever withdraws its own perspective.
func (r *Recorder) Release(taskID, reason string) (string, error) {
	if strings.TrimSpace(reason) == "" {
		return "", fmt.Errorf("release requires --reason")
	}
	tk, pl, code, err := r.laneEntry(taskID)
	if err != nil {
		return "", err
	}
	_, _, _, prior := laneState(tk, code, "\x00none")
	pl.ClearProductOf()
	pl.ClearCoveredBy()
	for _, id := range pl.DependsOn() {
		pl.RemoveDependsOn(id)
	}
	for _, id := range pl.RelatesTo() {
		pl.RemoveRelatesTo(id)
	}
	if err := r.writePayload(taskID, pl); err != nil {
		return prior, err
	}
	for _, ns := range []string{RPINamespace, ProductNamespace, DevNamespace, RejectNamespace} {
		if err := r.clearNamespace(taskID, code, ns); err != nil {
			return prior, err
		}
	}
	if _, err := r.Store.CreateComment(taskID, "workflow_rpi: released to backlog — "+reason, nil, "", r.Actor); err != nil {
		return prior, fmt.Errorf("released, but recording the reason comment failed: %w", err)
	}
	return prior, nil
}

// SetProductStatus updates the product-lane clarification status in place.
// It refuses off-lane: a status axis is meaningless outside its lane.
func (r *Recorder) SetProductStatus(taskID, status string) error {
	if !validProductStatus(status) {
		return fmt.Errorf("invalid product status %q (want %s or %s)", status, ProductUnclarified, ProductClarified)
	}
	tk, _, code, err := r.laneEntry(taskID)
	if err != nil {
		return err
	}
	if !containsString(tk.Labels, code+":"+RPINamespace+":"+LaneProduct) {
		return fmt.Errorf("%s is not in the product lane", taskID)
	}
	return r.swapNamespaceLabels(taskID, code, ProductNamespace, status)
}

// SetDevStatus updates the pipeline-lane development status in place. It
// refuses off-lane for the same reason as SetProductStatus.
func (r *Recorder) SetDevStatus(taskID, status string) error {
	if !validDevStatus(status) {
		return fmt.Errorf("invalid dev status %q", status)
	}
	tk, _, code, err := r.laneEntry(taskID)
	if err != nil {
		return err
	}
	if !containsString(tk.Labels, code+":"+RPINamespace+":"+LanePipeline) {
		return fmt.Errorf("%s is not in the pipeline lane", taskID)
	}
	return r.swapNamespaceLabels(taskID, code, DevNamespace, status)
}
