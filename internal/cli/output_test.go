package cli

import (
	"testing"

	"atm/internal/core"
)

func TestMetaPresenceSortedSizesOnly(t *testing.T) {
	tk := &core.Task{ID: "PX-1", Meta: map[string]string{
		"workflow_ai": `{"v":1}`,
		"contextmap":  "cm",
	}}
	got := metaPresence(tk)
	if len(got) != 2 {
		t.Fatalf("presence = %+v", got)
	}
	if got[0].Capability != "contextmap" || got[0].Bytes != 2 {
		t.Errorf("first = %+v, want contextmap/2 (sorted by name, size only)", got[0])
	}
	if got[1].Capability != "workflow_ai" || got[1].Bytes != 7 {
		t.Errorf("second = %+v", got[1])
	}
	if metaPresence(&core.Task{ID: "PX-2"}) != nil {
		t.Error("no meta must yield nil, not empty slice noise")
	}
}
