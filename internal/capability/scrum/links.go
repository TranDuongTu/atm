package scrum

import (
	"fmt"

	"atm/internal/core"
)

// sameProject validates both IDs and requires one project: links never cross
// project boundaries, and never point at themselves.
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

// SetPartOf records this unit's parent. At most one: replacing a different
// parent is refused (clear it first), so a re-parent is always deliberate.
func (r *Recorder) SetPartOf(taskID, parentID string) error {
	if _, err := sameProject(taskID, parentID); err != nil {
		return err
	}
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if cur := pl.PartOf(); cur != "" && cur != parentID {
		return fmt.Errorf("%s is already part of %s (clear it first)", taskID, cur)
	}
	if _, err := r.Store.GetTask(parentID); err != nil {
		return err // the target must exist
	}
	if pl.PartOf() == parentID {
		return nil
	}
	pl.SetPartOf(parentID)
	return r.writePayload(taskID, pl)
}

// ClearPartOf removes the parent link. parentID, when given, must match what
// is stored — explicit beats accidental.
func (r *Recorder) ClearPartOf(taskID, parentID string) error {
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	cur := pl.PartOf()
	if cur == "" {
		return fmt.Errorf("%s has no part_of link", taskID)
	}
	if parentID != "" && cur != parentID {
		return fmt.Errorf("%s is part of %s, not %s", taskID, cur, parentID)
	}
	pl.ClearPartOf()
	return r.writePayload(taskID, pl)
}

// LinkDependsOn records an outbound execution dependency. Stored
// one-directional on taskID; the reporter surfaces both directions and derives
// blocked work. Duplicate links are a silent no-op.
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
	if _, otherPl, err := r.taskPayload(otherID); err == nil && containsString(otherPl.DependsOn(), taskID) {
		return fmt.Errorf("cycle: %s already depends on %s", otherID, taskID)
	}
	if !pl.AddDependsOn(otherID) {
		return nil
	}
	return r.writePayload(taskID, pl)
}

// UnlinkDependsOn removes the dependency; unlinking an absent link is an error
// (it usually means a typo'd ID).
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

// SetCoveredBy records the task that covers this one. Replacing an existing
// pointer is allowed: unlike part_of it carries no topology invariant.
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
