package codereview

import (
	"unicode/utf8"

	"atm/internal/capability"
	"atm/internal/core"
)

// prCellLimit is how much of the PR locator fits in a lane cell. A bare number
// or a short "#142" belongs there; a full URL would crowd out the state, so it
// stays in the detail view and the report.
const prCellLimit = 12

// Annotate implements the contextual-column hook: PURE over the task value —
// no store, no filesystem.
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := core.ParseTaskID(t.ID)
	if !ok {
		return nil
	}
	if reason := EvictedAs(&t, code); reason != "" {
		return &capability.Cell{Text: "out · " + reason, Tone: capability.ToneStale}
	}
	state := StateOf(&t, code)
	if state == "" {
		return nil
	}
	pl, err := DecodePayload(t.Meta[CapabilityName])
	if err != nil {
		return &capability.Cell{Text: CapabilityName + ": unreadable state", Tone: capability.ToneAttention}
	}
	if state == StateDone {
		return &capability.Cell{Text: "✓ reviewed", Tone: capability.ToneOK}
	}
	text := "review · " + state
	if state == StateReviewing {
		text = "reviewing"
	}
	if pr := pl.PR(); pr != "" && utf8.RuneCountInString(pr) <= prCellLimit {
		text += " · " + pr
	} else if pr == "" {
		// The verbs cannot produce this; a hand-assigned label can.
		return &capability.Cell{Text: text + " · no PR", Tone: capability.ToneAttention}
	}
	return &capability.Cell{Text: text, Tone: capability.ToneNeutral}
}
