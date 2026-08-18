package workflowrpi

import (
	"strings"
	"testing"
)

func TestGuideShape(t *testing.T) {
	if (Cap{}.Name()) != CapabilityName || CapabilityName != "workflow_rpi" {
		t.Fatalf("Name/CapabilityName mismatch: %q", Cap{}.Name())
	}
	guide := Cap{}.Guide()
	for _, want := range []string{
		"workflow_rpi capability", "rpi-backlog", "rpi-product", "rpi-pipeline", "rpi-reject",
		"Task.Meta[\"workflow_rpi\"]", "product_of", "depends_on", "manager persona",
		"atm capability workflow_rpi product", "atm capability workflow_rpi pipeline", "atm capability workflow_rpi report",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}
