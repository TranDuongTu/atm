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
		return &capability.Cell{Text: "out · " + reason, Tone: capability.ToneStale, Rank: rankOut}
	}
	state := StateOf(&t, code)
	if state == "" {
		return nil
	}
	pl, err := DecodePayload(t.Meta[CapabilityName])
	if err != nil {
		return &capability.Cell{Text: CapabilityName + ": unreadable state", Tone: capability.ToneAttention, Rank: rankUnreadable}
	}
	if state == StateDone {
		return &capability.Cell{Text: "✓ reviewed", Tone: capability.ToneOK, Rank: rankDone}
	}
	text := "review · " + state
	rank := rankScheduled
	if state == StateReviewing {
		text = "reviewing"
		rank = rankReviewing
	}
	if pr := pl.PR(); pr != "" && utf8.RuneCountInString(pr) <= prCellLimit {
		text += " · " + pr
	} else if pr == "" {
		// The verbs cannot produce this; a hand-assigned label can.
		return &capability.Cell{Text: text + " · no PR", Tone: capability.ToneAttention, Rank: rankNoPR}
	}
	return &capability.Cell{Text: text, Tone: capability.ToneNeutral, Rank: rank}
}

// Ranks order the ANNOTATE column: the broken cell needs eyes first, then the
// claim no verb could have produced, then reviews in flight ahead of merely
// scheduled ones, then finished, then the settled evictions.
const (
	rankUnreadable = 1
	rankNoPR       = 2
	rankReviewing  = 3
	rankScheduled  = 4
	rankDone       = 5
	rankOut        = 6
)
