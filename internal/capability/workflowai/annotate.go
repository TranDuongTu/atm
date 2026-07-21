package workflowai

import (
	"atm/internal/capability"
	"atm/internal/core"
)

// Cap is the workflow_ai capability; the full interface lands in command.go
// (Task 7), Annotate lives here with the logic it owns.
type Cap struct{}

// Annotate implements the contextual-column hook: PURE over the task value
// (labels + own payload) — no store, no filesystem; on-disk plan
// verification is PlanCheck's job, which is why ToneStale stays unused
// here. Nil for tasks outside the cycle (no stage label), even when they
// carry links. A malformed payload degrades to the label-only cell — never
// an error, never raw payload on screen.
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := core.ParseTaskID(t.ID)
	if !ok {
		return nil
	}
	stage := firstStageValue(t.Labels, code)
	if stage == StageNew {
		return nil
	}
	cell := &capability.Cell{Text: stage, Tone: capability.ToneNeutral}
	if stage != StagePlanned && stage != StageImplementable {
		return cell
	}
	pl, err := DecodePayload(t.Meta[CapabilityName])
	if err != nil {
		return cell
	}
	p := pl.Plan()
	switch {
	case p == nil:
		cell.Text += "·no-plan"
		cell.Tone = capability.ToneAttention
	case p.Kind == PlanKindEphemeral:
		cell.Text += "·ephemeral"
		cell.Tone = capability.ToneAttention
	default:
		cell.Text += "·" + p.Kind
		if stage == StageImplementable {
			cell.Tone = capability.ToneOK
		}
	}
	return cell
}
