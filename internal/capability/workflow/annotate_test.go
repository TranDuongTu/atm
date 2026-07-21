package workflow

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestAnnotateFromStatusAndPriority(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   *capability.Cell
	}{
		{"no status", []string{"PX:type:bug"}, nil},
		{"open", []string{"PX:status:open"}, &capability.Cell{Text: "open", Tone: capability.ToneNeutral}},
		{"in-progress", []string{"PX:status:in-progress"}, &capability.Cell{Text: "in-progress", Tone: capability.ToneOK}},
		{"blocked", []string{"PX:status:blocked"}, &capability.Cell{Text: "blocked", Tone: capability.ToneAttention}},
		{"done", []string{"PX:status:done"}, &capability.Cell{Text: "done", Tone: capability.ToneNeutral}},
		{"with priority", []string{"PX:status:open", "PX:priority:high"}, &capability.Cell{Text: "open · high", Tone: capability.ToneNeutral}},
	}
	for _, tc := range cases {
		got := New().Annotate(core.Task{ID: "PX-1", ProjectCode: "PX", Labels: tc.labels})
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("%s: Annotate = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
