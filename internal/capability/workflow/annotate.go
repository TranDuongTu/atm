package workflow

import (
	"strings"

	"atm/internal/capability"
	"atm/internal/core"
)

// Annotate renders the workflow cell from the task's own labels: the status
// value, with the priority value appended when present ("open · high").
// Pure; nil for tasks carrying no status label. This reads labels, not Meta —
// workflow's state IS its labels.
func (c Cap) Annotate(t core.Task) *capability.Cell {
	status := labelValue(t.Labels, t.ProjectCode+":status:")
	if status == "" {
		return nil
	}
	text := status
	if p := labelValue(t.Labels, t.ProjectCode+":priority:"); p != "" {
		text += " · " + p
	}
	tone := capability.ToneNeutral
	switch status {
	case "in-progress":
		tone = capability.ToneOK
	case "blocked":
		tone = capability.ToneAttention
	}
	return &capability.Cell{Text: text, Tone: tone}
}

// labelValue returns the value part of the first label carrying prefix
// ("<CODE>:status:"), or "".
func labelValue(labels []string, prefix string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return ""
}
