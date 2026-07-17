package workflow

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// Reporter is the read-only side of the workflow capability. It never
// mutates the store; the project log is byte-identical before and after
// any Reporter call (testable, like contextmap's reporter contract).
type Reporter struct {
	Store core.TaskService
}

// Status returns the task's status value (e.g. "open", "in-progress") or
// "" when the task carries no status:* label (untriaged). "" is not an error:
// an untriaged task is a normal state, and a status value can never itself be
// the empty string (label-name validation requires a non-empty value
// segment), so the sentinel is unambiguous.
//
// Exactly-one-status is an invariant this capability maintains, not one the
// store enforces -- a hand-edited task may carry several status:* labels. In
// that case Status reports the first match, which is the lexicographically
// first: the store returns labels sorted (see internal/store/cache.go, ORDER
// BY label). That makes the result deterministic but arbitrary -- it is NOT
// necessarily the most recently set status, and Status gives no signal that
// the task is in an inconsistent state. Recorder.SetStatus collapses such a
// task back to one status. This mirrors the ordering note on
// Recorder.SetStatus; the two must stay in agreement.
func (r *Reporter) Status(taskID string) (string, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	prefix := code + ":" + StatusNamespace + ":"
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix), nil
		}
	}
	return "", nil
}
