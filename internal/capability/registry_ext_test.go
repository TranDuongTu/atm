package capability_test

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/qa"
	"atm/internal/capability/scrum"
	"atm/internal/core"
)

// fakeLabelList satisfies the one LabelService method Unmanaged reads; the
// rest panic (Unmanaged must stay a pure LabelList subtraction). The
// in-package listOnlyService in capability_test.go is not reachable from
// this external package, so this thin fake reuses core.LabelService via
// embedding + a single live method.
type fakeLabelList struct {
	core.LabelService
	labels []core.Label
}

func (f fakeLabelList) LabelList(project, namespace string) []core.Label { return f.labels }

func TestOwnedLabelsReturnsNamedCapabilityVocabulary(t *testing.T) {
	reg := capability.NewRegistry(scrum.New(), qa.New())
	got := reg.OwnedLabels("ATM", "scrum")
	if want := len(scrum.Vocabulary("ATM")); len(got) != want {
		t.Fatalf("scrum OwnedLabels len = %d, want %d", len(got), want)
	}
	if reg.OwnedLabels("ATM", "nope") != nil {
		t.Fatalf("unknown capability should return nil")
	}
	var nilReg *capability.Registry
	if nilReg.OwnedLabels("ATM", "scrum") != nil {
		t.Fatalf("nil registry should return nil")
	}
}

// TestUnmanagedMatchesOwnedLabelsSubtraction pins the single-sourcing
// property: LabelList minus the union of every capability's OwnedLabels
// (via LabelSet) equals Unmanaged, label for label.
func TestUnmanagedMatchesOwnedLabelsSubtraction(t *testing.T) {
	reg := capability.NewRegistry(scrum.New(), qa.New())
	svc := fakeLabelList{labels: append(
		append(scrum.Vocabulary("ATM"), qa.Vocabulary("ATM")...),
		core.Label{Name: "ATM:type:bug"},           // unmanaged namespace member
		core.Label{Name: "ATM:needs-triage"},       // unmanaged loose tag
		core.Label{Name: "ATM:scrum-stage:parked"}, // ad-hoc member of an OWNED namespace -> managed
	)}
	un, err := reg.Unmanaged(svc, "ATM")
	if err != nil {
		t.Fatalf("Unmanaged: %v", err)
	}
	inUn := map[string]bool{}
	for _, l := range un {
		inUn[l.Name] = true
	}
	var vocab []core.Label
	for _, name := range reg.Names() {
		vocab = append(vocab, reg.OwnedLabels("ATM", name)...)
	}
	owned := capability.NewLabelSet(vocab)
	for _, l := range svc.LabelList("ATM", "") {
		if owned.Contains(l.Name) == inUn[l.Name] {
			t.Errorf("%s: owned=%v but in-unmanaged=%v — the two surfaces disagree", l.Name, owned.Contains(l.Name), inUn[l.Name])
		}
	}
	if !inUn["ATM:type:bug"] || !inUn["ATM:needs-triage"] || inUn["ATM:scrum-stage:parked"] {
		t.Errorf("spot checks failed: unmanaged = %v", inUn)
	}
}
