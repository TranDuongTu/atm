package capability

import (
	"testing"

	"atm/internal/core"
)

// fakeFlow implements Capability minimally + Flow. The embedded Capability is
// nil: these tests call only Name() (overridden) and the Flow methods.
type fakeFlow struct {
	Capability
	name string
}

func (f fakeFlow) Name() string { return f.name }

// Guide returns the minimal well-formed duty section: NewRegistry enforces
// the duty contract, so a flow fake must satisfy it to be constructible.
func (f fakeFlow) Guide() string {
	return "## Duty: manager\n\n### Triage\nt\n\n### Advance\na\n\n### Route\nr\n"
}
func (f fakeFlow) ClaimExprs() []string {
	return []string{f.name + ":*", f.name + "-out:*"}
}
func (f fakeFlow) FinishLabel(code string) core.Label {
	return core.Label{Name: code + ":" + f.name + ":done"}
}
func (f fakeFlow) EvictLabel(code string) core.Label {
	return core.Label{Name: code + ":" + f.name + "-out:*"}
}
func (f fakeFlow) Lanes(code string) LaneSet {
	return LaneSet{
		Inbox:    code + ":" + f.name + "-inbox",
		Pipeline: code + ":" + f.name + "-pipeline",
		Out:      code + ":" + f.name + "-out-board",
	}
}

func TestFlowsFiltersNonFlowCapabilities(t *testing.T) {
	reg := NewRegistry(fakeFlow{name: "scrum"})
	if got := len(reg.Flows()); got != 1 {
		t.Fatalf("Flows() = %d, want 1", got)
	}
	if reg.Flows()[0].Name() != "scrum" {
		t.Fatalf("Flows()[0] = %q", reg.Flows()[0].Name())
	}
}

func TestDefaultPoolExprExcludesEveryEnabledFlowClaim(t *testing.T) {
	reg := NewRegistry(fakeFlow{name: "scrum"}, fakeFlow{name: "qa"})
	want := "NOT scrum:* AND NOT scrum-out:* AND NOT qa:* AND NOT qa-out:*"
	if got := reg.DefaultPoolExpr(); got != want {
		t.Fatalf("DefaultPoolExpr() = %q, want %q", got, want)
	}
}

func TestDefaultPoolExprEmptyRegistry(t *testing.T) {
	if got := NewRegistry().DefaultPoolExpr(); got != "*" {
		t.Fatalf("DefaultPoolExpr() = %q, want *", got)
	}
}

func TestFlowsNilRegistry(t *testing.T) {
	var r *Registry
	if got := r.Flows(); got != nil {
		t.Fatalf("Flows() = %v, want nil", got)
	}
	if got := r.DefaultPoolExpr(); got != "*" {
		t.Fatalf("DefaultPoolExpr() = %q, want *", got)
	}
}
