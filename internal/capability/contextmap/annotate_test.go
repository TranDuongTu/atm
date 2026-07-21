package contextmap

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestAnnotateFromContextLabels(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   *capability.Cell
	}{
		{"non-context task", []string{"PX:status:open"}, nil},
		{"current pointer", []string{"PX:context:agent"}, &capability.Cell{Text: "agent", Tone: capability.ToneOK}},
		{"superseded pointer", []string{"PX:context:documentation", "PX:knowledge:superseded"}, &capability.Cell{Text: "superseded", Tone: capability.ToneNeutral}},
	}
	for _, tc := range cases {
		got := New().Annotate(core.Task{ID: "PX-1", ProjectCode: "PX", Labels: tc.labels})
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("%s: Annotate = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
