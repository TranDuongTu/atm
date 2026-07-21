package contextmap

import (
	"slices"

	"atm/internal/capability"
	"atm/internal/core"
)

// Annotate renders the contextmap cell for context pointers, from labels
// only: the pointer kind while current, "superseded" once the lifecycle label
// lands. Nil for non-context tasks. Stamp-based staleness deliberately waits
// for the provenance migration (ATM-a2e902): Annotate is pure over the task,
// and the stamps still live in comments today.
func (c Cap) Annotate(t core.Task) *capability.Cell {
	kind := ""
	for _, k := range ContextKinds {
		if slices.Contains(t.Labels, LabelContextKind(t.ProjectCode, k)) {
			kind = k
			break
		}
	}
	if kind == "" {
		return nil
	}
	if slices.Contains(t.Labels, LabelSuperseded(t.ProjectCode)) {
		return &capability.Cell{Text: "superseded", Tone: capability.ToneStale}
	}
	return &capability.Cell{Text: kind, Tone: capability.ToneOK}
}
