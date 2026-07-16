// internal/workflow/reporter.go
package workflow

import (
	"fmt"
	"strings"

	"atm/internal/store"
)

// Reporter is the read-only side of the workflow capability. It never
// mutates the store; the project log is byte-identical before and after
// any Reporter call (testable, like contextmap's reporter contract).
type Reporter struct {
	Store *store.Store
}

// Status returns the task's status value (e.g. "open", "in-progress") or
// "" when the task carries no status:* label (untriaged).
func (r *Reporter) Status(taskID string) (string, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := store.ParseTaskID(taskID)
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
