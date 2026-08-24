package qa

import (
	"atm/internal/capability"
	"atm/internal/core"
)

// Annotate implements the contextual-column hook: PURE over the task value —
// no store, no filesystem. An original's "1/2 scaffolds passed" therefore
// belongs to the reporter, not this cell: counting scaffold states needs to
// read the scaffolds. What the cell CAN say from the task alone is which side
// of the scaffold edge this task sits on, which is the distinction that
// matters most when reading the lane.
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := core.ParseTaskID(t.ID)
	if !ok {
		return nil
	}
	if reason := EvictedAs(&t, code); reason != "" {
		tone := capability.ToneStale
		if reason == OutFailed {
			// A failed verification is a verdict the manager must route,
			// not a settled matter to scroll past.
			tone = capability.ToneAttention
		}
		return &capability.Cell{Text: "out · " + reason, Tone: tone}
	}
	state := StateOf(&t, code)
	if state == "" {
		return nil
	}
	pl, err := DecodePayload(t.Meta[CapabilityName])
	if err != nil {
		return &capability.Cell{Text: CapabilityName + ": unreadable state", Tone: capability.ToneAttention}
	}
	if pl.PartOf() != "" {
		return &capability.Cell{Text: "scaffold · " + state, Tone: capability.ToneNeutral}
	}
	if state == StateDone {
		return &capability.Cell{Text: "✓ qa done", Tone: capability.ToneOK}
	}
	return &capability.Cell{Text: CapabilityName + " · " + state, Tone: capability.ToneNeutral}
}
