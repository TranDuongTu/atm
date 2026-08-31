package channel

import (
	"atm/internal/capability"
	"atm/internal/core"
	"strings"
)

type Cap struct{}

// Annotate renders the channel cell for a channel task: handle + type, or an
// attention cell when the payload is unreadable (degrade, never panic).
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := strings.Cut(t.ID, "-")
	if !ok {
		return nil
	}
	rec, err := core.ChannelFromTask(code, t)
	if err != nil {
		return &capability.Cell{Text: "channel: unreadable payload", Tone: capability.ToneAttention, Rank: rankUnreadable}
	}
	if rec == nil {
		return nil
	}
	return &capability.Cell{Text: "channel " + rec.Name + " · " + rec.Type, Tone: capability.ToneNeutral}
}

// rankUnreadable is the only ranked cell a channel produces: a broken channel
// needs eyes first. A healthy channel row is unranked (0) — a registry
// record with no order to impose on a reader.
const rankUnreadable = 1
