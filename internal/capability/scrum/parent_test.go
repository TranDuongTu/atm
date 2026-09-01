package scrum

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestParentOfReadsPartOf(t *testing.T) {
	tk := core.Task{ID: "ATM-aaaaaa", Meta: map[string]string{CapabilityName: `{"part_of":"ATM-bbbbbb"}`}}
	if got := (Cap{}).ParentOf(tk); got != "ATM-bbbbbb" {
		t.Fatalf("ParentOf = %q, want ATM-bbbbbb", got)
	}
}

func TestParentOfEmptyCases(t *testing.T) {
	cases := map[string]core.Task{
		"no payload":        {ID: "ATM-aaaaaa"},
		"no part_of":        {ID: "ATM-aaaaaa", Meta: map[string]string{CapabilityName: `{"spec":"x"}`}},
		"malformed payload": {ID: "ATM-aaaaaa", Meta: map[string]string{CapabilityName: `{not json`}},
	}
	for name, tk := range cases {
		if got := (Cap{}).ParentOf(tk); got != "" {
			t.Fatalf("%s: ParentOf = %q, want empty", name, got)
		}
	}
}

func TestCapSatisfiesParenter(t *testing.T) {
	var _ capability.Parenter = Cap{}
}
