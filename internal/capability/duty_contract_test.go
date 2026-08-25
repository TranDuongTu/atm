package capability_test

// The duty contract is a construction-time invariant: NewRegistry refuses a
// flow capability whose guide lacks a `## Duty` section, a registry
// capability whose guide carries one, and any guide whose duty section is
// malformed. Building the registry exactly as cmd/atm/main.go does IS the
// assertion for the built-in set.

import (
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/channel"
	"atm/internal/capability/checklist"
	"atm/internal/capability/codereview"
	"atm/internal/capability/qa"
	"atm/internal/capability/release"
	"atm/internal/capability/scrum"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

const dutySection = "## Duty: manager\n\n### Triage\nt\n\n### Advance\na\n\n### Route\nr\n"

func TestBuiltinRegistryHonorsTheDutyContract(t *testing.T) {
	reg := capability.NewRegistry(
		channel.New(), checklist.New(),
		scrum.New(), qa.New(), codereview.New(), release.New(),
	)
	if got := len(reg.Flows()); got != 3 {
		t.Fatalf("Flows() = %d, want 3 (scrum, qa, codereview)", got)
	}
}

// contractCap is a minimal Capability whose guide is the test's to choose.
type contractCap struct {
	name  string
	guide string
}

func (c contractCap) Name() string                          { return c.name }
func (c contractCap) Summary() string                       { return c.name }
func (c contractCap) Brief() string                         { return "" }
func (c contractCap) Guide() string                         { return c.guide }
func (c contractCap) Vocabulary(code string) []core.Label   { return nil }
func (c contractCap) Exposed(code string) []core.Label      { return nil }
func (c contractCap) Annotate(t core.Task) *capability.Cell { return nil }
func (c contractCap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	return nil, nil
}
func (c contractCap) Command(env capability.Env) *cobra.Command { return &cobra.Command{Use: c.name} }

// contractFlow upgrades contractCap to a Flow.
type contractFlow struct{ contractCap }

func (f contractFlow) ClaimExprs() []string { return []string{f.name + ":*"} }
func (f contractFlow) FinishLabel(code string) core.Label {
	return core.Label{Name: code + ":" + f.name + ":done"}
}
func (f contractFlow) EvictLabel(code string) core.Label {
	return core.Label{Name: code + ":" + f.name + "-out:*"}
}
func (f contractFlow) Lanes(code string) capability.LaneSet {
	return capability.LaneSet{Inbox: "i", Pipeline: "p", Out: "o"}
}

func mustPanic(t *testing.T, wantFragment string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("want panic containing %q, got none", wantFragment)
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, wantFragment) {
			t.Fatalf("panic = %v, want fragment %q", r, wantFragment)
		}
	}()
	fn()
}

func TestNewRegistryRefusesFlowWithoutDuty(t *testing.T) {
	mustPanic(t, "lacks a ## Duty section", func() {
		capability.NewRegistry(contractFlow{contractCap{name: "flowy", guide: "## Semantics\ns\n"}})
	})
}

func TestNewRegistryRefusesRegistryCapWithDuty(t *testing.T) {
	mustPanic(t, "must not carry ## Duty", func() {
		capability.NewRegistry(contractCap{name: "recordy", guide: dutySection})
	})
}

func TestNewRegistryRefusesMalformedDuty(t *testing.T) {
	mustPanic(t, "Duty", func() {
		capability.NewRegistry(contractFlow{contractCap{name: "flowy", guide: "## Duty: manager\n\n### Triage\nt\n"}})
	})
}

func TestNewRegistryAcceptsAlignedFakes(t *testing.T) {
	reg := capability.NewRegistry(
		contractFlow{contractCap{name: "flowy", guide: dutySection}},
		contractCap{name: "recordy", guide: "## Semantics\ns\n"},
	)
	if got := len(reg.Flows()); got != 1 {
		t.Fatalf("Flows() = %d, want 1", got)
	}
}
