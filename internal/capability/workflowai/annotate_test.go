package workflowai

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestAnnotateTable(t *testing.T) {
	specPayload := func(kind string) string {
		pl, _ := DecodePayload("")
		pl.SetSpec(SpecRecord{Kind: kind, Ref: "r", RecordedAt: "t", Actor: "a"})
		out, _ := pl.Encode()
		return out
	}
	bothPayload := func(specKind, planKind string) string {
		pl, _ := DecodePayload("")
		pl.SetSpec(SpecRecord{Kind: specKind, Ref: "s", RecordedAt: "t", Actor: "a"})
		pl.SetPlan(PlanRecord{Kind: planKind, Ref: "p", RecordedAt: "t", Actor: "a"})
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
		{"queued neutral", []string{"ATM:stage:queued"}, "", false, "queued", capability.ToneNeutral},
		{"brainstormed neutral", []string{"ATM:stage:brainstormed"}, "", false, "brainstormed", capability.ToneNeutral},
		{"clarified file neutral", []string{"ATM:stage:clarified"}, specPayload(PlanKindFile), false, "clarified·file", capability.ToneNeutral},
		{"clarified no-spec attention", []string{"ATM:stage:clarified"}, "", false, "clarified·no-spec", capability.ToneAttention},
		{"clarified ephemeral attention", []string{"ATM:stage:clarified"}, specPayload(PlanKindEphemeral), false, "clarified·ephemeral", capability.ToneAttention},
		{"planned file neutral", []string{"ATM:stage:planned"}, bothPayload(PlanKindFile, PlanKindFile), false, "planned·file", capability.ToneNeutral},
		{"planned no-plan attention", []string{"ATM:stage:planned"}, specPayload(PlanKindFile), false, "planned·no-plan", capability.ToneAttention},
		{"planned ephemeral-plan attention", []string{"ATM:stage:planned"}, bothPayload(PlanKindFile, PlanKindEphemeral), false, "planned·ephemeral", capability.ToneAttention},
		{"done neutral", []string{"ATM:stage:done"}, "", false, "done", capability.ToneNeutral},
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