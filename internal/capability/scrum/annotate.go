package scrum

import (
	"atm/internal/capability"
	"atm/internal/core"
)

// Annotate implements the contextual-column hook: PURE over the task value
// (labels + own payload) — no store, no filesystem. That purity is why a
// parent's child rollup ("3/5 stories done") is NOT here: counting children
// needs a project scan, so it belongs to the reporter and the detail view.
// Nil for work scrum has not decided about — the inbox has nothing to say
// yet. A malformed payload degrades to a cell that names the problem and
// never leaks raw payload bytes.
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := core.ParseTaskID(t.ID)
	if !ok {
		return nil
	}
	if reason := EvictedAs(&t, code); reason != "" {
		return &capability.Cell{Text: "out · " + reason, Tone: capability.ToneStale}
	}
	typ := TypeOf(&t, code)
	if typ == "" {
		return nil
	}
	if _, err := DecodePayload(t.Meta[CapabilityName]); err != nil {
		return &capability.Cell{Text: CapabilityName + ": unreadable state", Tone: capability.ToneAttention}
	}
	switch stage := StageOf(&t, code); stage {
	case "":
		return &capability.Cell{Text: typ + " · —", Tone: capability.ToneNeutral}
	case StageDone:
		return &capability.Cell{Text: typ + " · ✓ done", Tone: capability.ToneOK}
	default:
		return &capability.Cell{Text: typ + " · " + stage, Tone: capability.ToneNeutral}
	}
}
