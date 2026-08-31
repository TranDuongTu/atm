package checklist

import (
	"fmt"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"
)

type Cap struct{}

// Annotate renders the checklist cell: persona/name + step count, or an
// attention cell when the payload is unreadable (degrade, never panic).
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := strings.Cut(t.ID, "-")
	if !ok {
		return nil
	}
	rec, err := core.ChecklistFromTask(code, t)
	if err != nil {
		return &capability.Cell{Text: "checklist: unreadable payload", Tone: capability.ToneAttention, Rank: rankUnreadable}
	}
	if rec == nil {
		return nil
	}
	return &capability.Cell{Text: fmt.Sprintf("checklist %s · %d steps", rec.Name, core.ChecklistStepCount(rec.Steps)), Tone: capability.ToneNeutral}
}

// rankUnreadable is the only ranked cell a checklist produces: a broken
// checklist needs eyes first. A healthy checklist row is unranked (0) — a
// registry record with no order to impose on a reader.
const rankUnreadable = 1
