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
		if reason == OutFailed {
			// A failed verification is a verdict the manager must route,
			// not a settled matter to scroll past.
			return &capability.Cell{Text: "out · " + reason, Tone: capability.ToneAttention, Rank: rankOutFailed}
		}
		return &capability.Cell{Text: "out · " + reason, Tone: capability.ToneStale, Rank: rankOutSettled}
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
		if pl.PartOf() != "" {
			return &capability.Cell{Text: "scaffold · " + state, Tone: capability.ToneNeutral, Rank: rankDone}
		}
		return &capability.Cell{Text: "✓ qa done", Tone: capability.ToneOK, Rank: rankDone}
	}
	if pl.PartOf() != "" {
		return &capability.Cell{Text: "scaffold · " + state, Tone: capability.ToneNeutral, Rank: rankTesting}
	}
	return &capability.Cell{Text: CapabilityName + " · " + state, Tone: capability.ToneNeutral, Rank: rankTesting}
}

// Ranks order the ANNOTATE column: the broken cell needs eyes first, then the
// failed verdict the manager must route (ahead of active work), then testing
// — original and scaffold alike — then finished, then the settled evictions.
const (
	rankUnreadable = 1
	rankOutFailed  = 2
	rankTesting    = 3
	rankDone       = 4
	rankOutSettled = 5
)
