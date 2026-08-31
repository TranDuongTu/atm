package release

import (
	"strconv"

	"atm/internal/capability"
	"atm/internal/core"
)

// Annotate implements the contextual-column hook: PURE over the task value —
// no store, no filesystem. A container's roster size is in its OWN payload, so
// unlike the flow capabilities' child rollups this one is honestly available
// here without reading anything else.
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := core.ParseTaskID(t.ID)
	if !ok {
		return nil
	}
	pl, err := DecodePayload(t.Meta[CapabilityName])
	if err != nil {
		return &capability.Cell{Text: CapabilityName + ": unreadable state", Tone: capability.ToneAttention, Rank: rankUnreadable}
	}
	shipped := containsString(t.Labels, ShippedLabel(code))
	if v := pl.Version(); v != "" {
		if shipped {
			return &capability.Cell{Text: "✓ shipped " + v, Tone: capability.ToneOK, Rank: rankShipped}
		}
		n := strconv.Itoa(len(pl.Members()))
		return &capability.Cell{Text: v + " · " + n + " tasks", Tone: capability.ToneNeutral, Rank: rankContainer}
	}
	if pl.ReleaseOf() == "" {
		return nil
	}
	if shipped {
		return &capability.Cell{Text: "✓ shipped", Tone: capability.ToneOK, Rank: rankShipped}
	}
	return &capability.Cell{Text: "→ release", Tone: capability.ToneNeutral, Rank: rankMember}
}

// Ranks order the ANNOTATE column: the broken cell needs eyes first, then the
// open container being built, then the open member waiting on it, then the
// shipped finish. Unknown payloads fall out as 0 (unranked) rather than
// guessing a position.
const (
	rankUnreadable = 1
	rankContainer  = 2
	rankMember     = 3
	rankShipped    = 4
)
