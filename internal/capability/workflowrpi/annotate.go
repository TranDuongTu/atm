package workflowrpi

import (
	"atm/internal/capability"
	"atm/internal/core"
)

// Cap is the workflow_rpi capability; the full interface lands in
// command.go, Annotate lives here with the logic it owns.
type Cap struct{}

// Annotate implements the contextual-column hook: PURE over the task value
// (labels + own payload) — no store, no filesystem, which is why a pipeline
// task's parent is only checked for presence here and validated for real in
// the reporter. Nil for tasks outside RPI (backlog is the unset set: nothing
// to say yet). A malformed payload degrades to a lane·payload cell — never
// an error, never raw payload on screen.
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := core.ParseTaskID(t.ID)
	if !ok {
		return nil
	}
	_, lanes := labelValues(&t, code, RPINamespace)
	if len(lanes) == 0 {
		return nil
	}
	lane := lanes[0]
	status := statusOf(&t, code, lane)
	cell := &capability.Cell{Text: lane, Tone: capability.ToneNeutral}

	pl, err := DecodePayload(t.Meta[CapabilityName])
	if err != nil {
		cell.Text += "·payload"
		cell.Tone = capability.ToneAttention
		return cell
	}
	if status == "" {
		cell.Text += "·no-status"
		cell.Tone = capability.ToneAttention
		return cell
	}
	cell.Text += "·" + status

	switch lane {
	case LanePipeline:
		if pl.ProductOf() == "" {
			cell.Text += "·orphan"
			cell.Tone = capability.ToneAttention
		} else {
			cell.Text += "·product"
		}
		if status == DevDone {
			cell.Tone = capability.ToneOK
		}
	case LaneReject:
		if (status == RejectCoveredBy || status == RejectDuplicate) && pl.CoveredBy() == "" {
			cell.Tone = capability.ToneAttention
		}
	}
	return cell
}
