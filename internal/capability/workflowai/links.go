package workflowai

import (
	"fmt"

	"atm/internal/core"
)

// sameProject validates both IDs and requires one project: links never
// cross project boundaries.
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

// LinkRevisionOf records child as a revision follow-up of parent: the
// revision_of pointer in child's payload (machine topology) plus the
// wfai:revision marker label (board visibility). At most one parent; a
// direct two-node cycle is rejected, deeper cycles are a non-goal.
// Idempotent for the same parent (and re-ensures the marker).
func (r *Recorder) LinkRevisionOf(childID, parentID string) error {
	code, err := sameProject(childID, parentID)
	if err != nil {
		return err
	}
	tk, pl, err := r.taskPayload(childID)
	if err != nil {
		return err
	}
	if cur := pl.RevisionOf(); cur != "" && cur != parentID {
		return fmt.Errorf("%s is already a revision of %s (unlink first)", childID, cur)
	}
	_, parentPl, err := r.taskPayload(parentID) // also proves the parent exists
	if err != nil {
		return err
	}
	if parentPl.RevisionOf() == childID {
		return fmt.Errorf("cycle: %s is already a revision of %s", parentID, childID)
	}
	if pl.RevisionOf() != parentID {
		pl.SetRevisionOf(parentID)
		if err := r.writePayload(childID, pl); err != nil {
			return err
		}
	}
	marker := code + ":" + MarkerNamespace + ":" + MarkerRevision
	if !containsString(tk.Labels, marker) {
		if err := r.Store.TaskLabelAdd(childID, marker, r.Actor); err != nil {
			return fmt.Errorf("add %s: %w", marker, err)
		}
	}
	return nil
}

// UnlinkRevisionOf removes the revision_of link (parentID must match the
// stored parent — explicit beats accidental) and the marker label.
func (r *Recorder) UnlinkRevisionOf(childID, parentID string) error {
	tk, pl, err := r.taskPayload(childID)
	if err != nil {
		return err
	}
	cur := pl.RevisionOf()
	if cur == "" {
		return fmt.Errorf("%s has no revision_of link", childID)
	}
	if cur != parentID {
		return fmt.Errorf("%s is a revision of %s, not %s", childID, cur, parentID)
	}
	pl.ClearRevisionOf()
	if err := r.writePayload(childID, pl); err != nil {
		return err
	}
	code, _, _ := core.ParseTaskID(childID)
	marker := code + ":" + MarkerNamespace + ":" + MarkerRevision
	if containsString(tk.Labels, marker) {
		if err := r.Store.TaskLabelRemove(childID, marker, r.Actor); err != nil {
			return fmt.Errorf("remove %s: %w", marker, err)
		}
	}
	return nil
}

// LinkRelatesTo records a generic, semantics-free association. Stored
// one-directional on taskID; the links reporter surfaces both directions.
// Duplicate links are a silent no-op.
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
// error (it usually means a typo'd ID).
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
