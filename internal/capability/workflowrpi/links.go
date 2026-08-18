package workflowrpi

import (
	"fmt"

	"atm/internal/core"
)

// sameProject validates both IDs and requires one project: links never
// cross project boundaries, and never point at themselves.
func sameProject(aID, bID string) (code string, err error) {
	aCode, _, ok := core.ParseTaskID(aID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", aID)
	}
	bCode, _, ok := core.ParseTaskID(bID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", bID)
	}
	if aCode != bCode {
		return "", fmt.Errorf("cannot link across projects (%s vs %s)", aID, bID)
	}
	if aID == bID {
		return "", fmt.Errorf("cannot link %s to itself", aID)
	}
	return aCode, nil
}

// SetProductOf records the pipeline task's product-roadmap parent. At most
// one parent: replacing a different one is refused (unlink first), so a
// re-parent is always deliberate. Idempotent for the same parent.
func (r *Recorder) SetProductOf(taskID, productID string) error {
	if _, err := sameProject(taskID, productID); err != nil {
		return err
	}
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if cur := pl.ProductOf(); cur != "" && cur != productID {
		return fmt.Errorf("%s is already linked to product %s (clear it first)", taskID, cur)
	}
	if _, err := r.Store.GetTask(productID); err != nil {
		return err // the target must exist
	}
	if pl.ProductOf() == productID {
		return nil
	}
	pl.SetProductOf(productID)
	return r.writePayload(taskID, pl)
}

// ClearProductOf removes the product link. productID must match the stored
// parent — explicit beats accidental.
func (r *Recorder) ClearProductOf(taskID, productID string) error {
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	cur := pl.ProductOf()
	if cur == "" {
		return fmt.Errorf("%s has no product_of link", taskID)
	}
	if productID != "" && cur != productID {
		return fmt.Errorf("%s is linked to product %s, not %s", taskID, cur, productID)
	}
	pl.ClearProductOf()
	return r.writePayload(taskID, pl)
}

// LinkDependsOn records an outbound execution dependency. Stored
// one-directional on taskID; the reporter surfaces both directions and
// derives blocked work. Duplicate links are a silent no-op.
func (r *Recorder) LinkDependsOn(taskID, otherID string) error {
	if _, err := sameProject(taskID, otherID); err != nil {
		return err
	}
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if _, err := r.Store.GetTask(otherID); err != nil {
		return err // the target must exist
	}
	if otherPl, err := r.dependencyPayload(otherID); err == nil && containsString(otherPl.DependsOn(), taskID) {
		return fmt.Errorf("cycle: %s already depends on %s", otherID, taskID)
	}
	if !pl.AddDependsOn(otherID) {
		return nil
	}
	return r.writePayload(taskID, pl)
}

// dependencyPayload decodes the other end of a would-be dependency. A
// malformed payload there is not this task's problem: the direct-cycle guard
// simply does not fire, and the reporter lists the sibling as at-risk.
func (r *Recorder) dependencyPayload(taskID string) (*Payload, error) {
	_, pl, err := r.taskPayload(taskID)
	return pl, err
}

// UnlinkDependsOn removes the dependency; unlinking an absent link is an
// error (it usually means a typo'd ID).
func (r *Recorder) UnlinkDependsOn(taskID, otherID string) error {
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if !pl.RemoveDependsOn(otherID) {
		return fmt.Errorf("%s has no depends_on link to %s", taskID, otherID)
	}
	return r.writePayload(taskID, pl)
}

// LinkRelatesTo records a generic, semantics-free association. Duplicate
// links are a silent no-op.
func (r *Recorder) LinkRelatesTo(taskID, otherID string) error {
	if _, err := sameProject(taskID, otherID); err != nil {
		return err
	}
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if _, err := r.Store.GetTask(otherID); err != nil {
		return err // the target must exist
	}
	if !pl.AddRelatesTo(otherID) {
		return nil
	}
	return r.writePayload(taskID, pl)
}

// UnlinkRelatesTo removes the association; unlinking an absent link is an
// error.
func (r *Recorder) UnlinkRelatesTo(taskID, otherID string) error {
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if !pl.RemoveRelatesTo(otherID) {
		return fmt.Errorf("%s has no relates_to link to %s", taskID, otherID)
	}
	return r.writePayload(taskID, pl)
}

// SetCoveredBy records the task that covers this one. Replacing an existing
// pointer is allowed: unlike product_of it carries no lane invariant.
func (r *Recorder) SetCoveredBy(taskID, otherID string) error {
	if _, err := sameProject(taskID, otherID); err != nil {
		return err
	}
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if _, err := r.Store.GetTask(otherID); err != nil {
		return err // the target must exist
	}
	if pl.CoveredBy() == otherID {
		return nil
	}
	pl.SetCoveredBy(otherID)
	return r.writePayload(taskID, pl)
}

// ClearCoveredBy removes the covered_by pointer. otherID, when given, must
// match what is stored.
func (r *Recorder) ClearCoveredBy(taskID, otherID string) error {
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	cur := pl.CoveredBy()
	if cur == "" {
		return fmt.Errorf("%s has no covered_by link", taskID)
	}
	if otherID != "" && cur != otherID {
		return fmt.Errorf("%s is covered by %s, not %s", taskID, cur, otherID)
	}
	pl.ClearCoveredBy()
	return r.writePayload(taskID, pl)
}
