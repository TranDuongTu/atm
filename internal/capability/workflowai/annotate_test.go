package workflowai

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestAnnotateTable(t *testing.T) {
	planPayload := func(kind string) string {
		pl, _ := DecodePayload("")
		pl.SetPlan(PlanRecord{Kind: kind, Ref: "r", RecordedAt: "t", Actor: "a"})
		out, _ := pl.Encode()
		return out
	}
	cases := []struct {
		name     string
		labels   []string
		payload  string
		wantNil  bool
		wantText string
		wantTone capability.Tone
	}{
		{"no stage -> nil even with links", nil, `{"v":1,"revision_of":"ATM-aaaaaa"}`, true, "", 0},
		{"brainstormed neutral", []string{"ATM:stage:brainstormed"}, "", false, "brainstormed", capability.ToneNeutral},
		{"clarified neutral", []string{"ATM:stage:clarified"}, "", false, "clarified", capability.ToneNeutral},
		{"done neutral", []string{"ATM:stage:done"}, "", false, "done", capability.ToneNeutral},
		{"planned file neutral", []string{"ATM:stage:planned"}, planPayload(PlanKindFile), false, "planned·file", capability.ToneNeutral},
		{"planned ephemeral attention", []string{"ATM:stage:planned"}, planPayload(PlanKindEphemeral), false, "planned·ephemeral", capability.ToneAttention},
		{"planned no-plan attention", []string{"ATM:stage:planned"}, "", false, "planned·no-plan", capability.ToneAttention},
		{"implementable file ok", []string{"ATM:stage:implementable"}, planPayload(PlanKindFile), false, "implementable·file", capability.ToneOK},
		{"implementable commit ok", []string{"ATM:stage:implementable"}, planPayload(PlanKindCommit), false, "implementable·commit", capability.ToneOK},
		{"implementable ephemeral attention", []string{"ATM:stage:implementable"}, planPayload(PlanKindEphemeral), false, "implementable·ephemeral", capability.ToneAttention},
		{"malformed payload degrades to label-only", []string{"ATM:stage:planned"}, "not json", false, "planned", capability.ToneNeutral},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tk := core.Task{ID: "ATM-1234", Labels: c.labels}
			if c.payload != "" {
				tk.Meta = map[string]string{CapabilityName: c.payload}
			}
			cell := Cap{}.Annotate(tk)
			if c.wantNil {
				if cell != nil {
					t.Fatalf("cell = %+v, want nil", cell)
				}
				return
			}
			if cell == nil {
				t.Fatal("cell = nil")
			}
			if cell.Text != c.wantText || cell.Tone != c.wantTone {
				t.Errorf("cell = {%q, %v}, want {%q, %v}", cell.Text, cell.Tone, c.wantText, c.wantTone)
			}
		})
	}
}
