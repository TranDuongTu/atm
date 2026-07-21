package workflowai

import (
	"fmt"

	"atm/internal/core"
)

// Reporter is the read-only side of the workflow_ai capability. It never
// mutates the store — the reporter reports, the decider demotes.
type Reporter struct {
	Store core.TaskService
}

// Stage returns the task's stage value or StageNew ("") when the task
// carries no stage:* label. On a hand-edited multi-stage task it reports
// the lexicographically first value (store labels sort); Recorder verbs
// converge such tasks.
func (r *Reporter) Stage(taskID string) (string, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	return firstStageValue(tk.Labels, code), nil
}
