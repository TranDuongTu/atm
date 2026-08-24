package qa

import (
	"io"
	"strings"
	"testing"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// stubEnv is enough capability.Env to BUILD the command tree — the guide test
// only enumerates it, never runs a verb.
type stubEnv struct{}

func (stubEnv) OpenService() (core.Service, error)              { return nil, nil }
func (stubEnv) Stdout() io.Writer                               { return io.Discard }
func (stubEnv) Stderr() io.Writer                               { return io.Discard }
func (stubEnv) Emit(any, func()) error                          { return nil }
func (stubEnv) RequireMutatingActor() (string, error)           { return testActor, nil }
func (stubEnv) ResolveActor(bool) (string, error)               { return testActor, nil }
func (stubEnv) BindActorFlag(*cobra.Command)                    {}
func (stubEnv) ResolveTaskID(id, legacy string) (string, error) { return id, nil }
func (stubEnv) TaskJSON(t *core.Task) any                       { return t }
func (stubEnv) BindTaskIDFlags(cmd *cobra.Command, id, legacy *string) {
	cmd.Flags().StringVar(id, "task", "", "task id")
}

func TestSkillGuideLoads(t *testing.T) {
	if (Cap{}).Summary() == "" || (Cap{}).Brief() == "" || (Cap{}).Guide() == "" {
		t.Fatal("summary, brief and guide must load from the embedded capability skill")
	}
}

func TestGuideHasTheRequiredSections(t *testing.T) {
	guide := Cap{}.Guide()
	for _, want := range []string{"## Semantics", "## Actions", "## Converge"} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}

func TestGuideDocumentsEveryMountedVerb(t *testing.T) {
	guide := Cap{}.Guide()
	for _, sub := range (Cap{}).Command(stubEnv{}).Commands() {
		if !strings.Contains(guide, "atm capability qa "+sub.Name()) {
			t.Errorf("guide does not document the %q verb", sub.Name())
		}
	}
}

// The two guarantees this capability exists to make must be stated verbatim,
// because downstream wiring and release selection both lean on them.
func TestGuideStatesTheTwoGuardRuleVerbatim(t *testing.T) {
	guide := Cap{}.Guide()
	for _, want := range []string{
		"qa:done is only ever stamped on absorbed originals, never scaffolds",
		"INTEGRATION PASS",
		"lighter mode",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}

func TestGuideNamesTheSocketsAndLanes(t *testing.T) {
	guide := Cap{}.Guide()
	c := New()
	for _, want := range []string{
		c.FinishLabel("<CODE>").Name, c.EvictLabel("<CODE>").Name,
		"qa-inbox", "qa-pipeline", "qa-out-board",
		`Meta["qa"]`, "part_of", "scaffolds",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}
