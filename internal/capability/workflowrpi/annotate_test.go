package workflowrpi

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestAnnotateNilOutsideRPI(t *testing.T) {
	cell := (Cap{}).Annotate(core.Task{ID: "ATM-abc123", Labels: []string{"ATM:status:open"}})
	if cell != nil {
		t.Fatalf("cell = %+v, want nil", cell)
	}
}

func TestAnnotateProductPipelineReject(t *testing.T) {
	cases := []struct {
		name string
		task core.Task
		want string
	}{
		{"product", core.Task{ID: "ATM-aaa111", Labels: []string{"ATM:rpi:product", "ATM:rpi-product:clarified"}}, "product·clarified"},
		{"pipeline", core.Task{ID: "ATM-bbb222", Labels: []string{"ATM:rpi:pipeline", "ATM:rpi-dev:planned"}, Meta: map[string]string{CapabilityName: `{"v":1,"product_of":"ATM-fff444"}`}}, "pipeline·planned·product"},
		{"reject", core.Task{ID: "ATM-ccc333", Labels: []string{"ATM:rpi:reject", "ATM:rpi-reject:covered-by"}, Meta: map[string]string{CapabilityName: `{"v":1,"covered_by":"ATM-fff444"}`}}, "reject·covered-by"},
	}
	for _, c := range cases {
		cell := (Cap{}).Annotate(c.task)
		if cell == nil || cell.Text != c.want {
			t.Errorf("%s cell = %+v, want %q", c.name, cell, c.want)
		}
	}
}

func TestAnnotateMalformedPayloadDegrades(t *testing.T) {
	cell := (Cap{}).Annotate(core.Task{ID: "ATM-dadbad", Labels: []string{"ATM:rpi:pipeline"}, Meta: map[string]string{CapabilityName: "not json"}})
	if cell == nil || cell.Tone != capability.ToneAttention || cell.Text != "pipeline·payload" {
		t.Fatalf("cell = %+v", cell)
	}
}
