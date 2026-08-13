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
		return &capability.Cell{Text: "channel: unreadable payload", Tone: capability.ToneAttention}
	}
	if rec == nil {
		return nil
	}
	return &capability.Cell{Text: "channel " + rec.Name + " · " + rec.Type, Tone: capability.ToneNeutral}
}
