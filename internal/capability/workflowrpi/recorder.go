package workflowrpi

import (
	"fmt"

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
