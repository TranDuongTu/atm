package codereview

import "atm/internal/core"

// ParentOf implements capability.Parenter: the review a follow-up item came
// out of, read purely from the task's own payload. A malformed payload
// answers "" — Annotate already surfaces the unreadable state as a cell; the
// tree just degrades to flat for this row.
func (Cap) ParentOf(t core.Task) string {
	pl, err := DecodePayload(t.Meta[CapabilityName])
	if err != nil {
		return ""
	}
	return pl.PartOf()
}
