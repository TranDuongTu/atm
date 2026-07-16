// internal/workflow/recorder.go
package workflow

import (
	"fmt"
	"strings"

	"atm/internal/store"
)

// Recorder is the mutating side of the workflow capability. It swaps a
// task's status:* label via existing store calls; the store itself
// enforces nothing. The "exactly one status" invariant is maintained by
// this recorder, not by the store.
type Recorder struct {
	Store *store.Store
	Actor string
}

// SetStatus swaps the task's status label to target. It removes every
// existing <code>:status:* label on the task (a hand-edited task may carry
// several), then adds the target. When the task already carries target as
// its sole status, it is a no-op. Returns the prior status value (e.g.
// "open") or "" if the task was untriaged. When the task had multiple
// status labels, prior is the first non-target one removed.
func (r *Recorder) SetStatus(taskID, target string) (prior string, err error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := store.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	prefix := code + ":" + StatusNamespace + ":"
	targetLabel := prefix + target

	// Collect all existing status:* labels and the prior value.
	var existing []string
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, prefix) {
			existing = append(existing, l)
		}
	}

	// No-op when the target is already the sole status.
	if len(existing) == 1 && existing[0] == targetLabel {
		return target, nil
	}

	// Pick the prior value: the first existing label that is not the target.
	for _, l := range existing {
		if l != targetLabel {
			prior = strings.TrimPrefix(l, prefix)
			break
		}
	}

	// Remove every existing status label.
	for _, l := range existing {
		if l == targetLabel {
			continue // will be (re)asserted below; removing+adding is wasteful
		}
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return "", fmt.Errorf("remove %s: %w", l, err)
		}
	}

	// Add the target if it is not already present.
	alreadyHasTarget := false
	for _, l := range existing {
		if l == targetLabel {
			alreadyHasTarget = true
		}
	}
	if !alreadyHasTarget {
		if err := r.Store.TaskLabelAdd(taskID, targetLabel, r.Actor); err != nil {
			return "", fmt.Errorf("add %s: %w", targetLabel, err)
		}
	}
	return prior, nil
}

// Start transitions the task to in-progress (someone is now on this).
func (r *Recorder) Start(taskID string) (string, error) { return r.SetStatus(taskID, StatusInProgress) }

// Open transitions the task to open ((re)open for consideration).
func (r *Recorder) Open(taskID string) (string, error) { return r.SetStatus(taskID, StatusOpen) }

// Block transitions the task to blocked (cannot proceed pending something else).
func (r *Recorder) Block(taskID string) (string, error) { return r.SetStatus(taskID, StatusBlocked) }

// Complete transitions the task to done (finished).
func (r *Recorder) Complete(taskID string) (string, error) { return r.SetStatus(taskID, StatusDone) }
