package workflow

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// Recorder is the mutating side of the workflow capability. It swaps a
// task's status:* label via existing service calls; the store itself
// enforces nothing. The "exactly one status" invariant is maintained by
// this recorder, not by the store.
type Recorder struct {
	Store core.TaskService
	Actor string
}

// SetStatus swaps the task's status label to target. It adds the target,
// then removes every other <code>:status:* label on the task (a hand-edited
// task may carry several). When the task already carries target as its sole
// status, it is a no-op and no store call is made.
//
// Add-before-remove is deliberate: the store has no transactions, so
// remove-first would leave the task with no status at all if the add failed.
// This ordering bounds the worst case to a recoverable extra label.
//
// Returns the prior status value (e.g. "open") or "" if the task was
// untriaged. When the task had multiple status labels, prior is the
// lexicographically first non-target one (the store returns labels sorted;
// see internal/store/cache.go ORDER BY label) - NOT necessarily the most
// recently set. On error, prior is still returned so callers can report what
// the task was.
func (r *Recorder) SetStatus(taskID, target string) (prior string, err error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	prefix := code + ":" + StatusNamespace + ":"
	targetLabel := prefix + target

	// Collect all existing status:* labels, note whether the target is among
	// them, and pick the prior value in one pass.
	var existing []string
	alreadyHasTarget := false
	for _, l := range tk.Labels {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		existing = append(existing, l)
		if l == targetLabel {
			alreadyHasTarget = true
		} else if prior == "" {
			prior = strings.TrimPrefix(l, prefix)
		}
	}

	// No-op when the target is already the sole status: zero store calls, so
	// the event log cannot advance.
	if len(existing) == 1 && alreadyHasTarget {
		return target, nil
	}

	// Add the target BEFORE removing anything. The store has no transactions,
	// and TaskLabelAdd validates only once called — so remove-then-add would
	// leave a task with NO status label if the add failed, silently dropping it
	// off every board. Add-first bounds the worst case to a leftover label.
	if !alreadyHasTarget {
		if err := r.Store.TaskLabelAdd(taskID, targetLabel, r.Actor); err != nil {
			return prior, fmt.Errorf("add %s: %w", targetLabel, err)
		}
	}

	// Then remove every other status label. If one of these fails the task
	// carries the target plus a leftover: the exactly-one invariant is
	// violated, but no status is lost and re-running the verb converges.
	for _, l := range existing {
		if l == targetLabel {
			continue
		}
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return prior, fmt.Errorf("remove %s: %w", l, err)
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
