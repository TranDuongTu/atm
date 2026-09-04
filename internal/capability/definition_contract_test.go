package capability_test

// The contract this replaces: NewRegistry used to refuse a flow capability
// whose GUIDE PROSE lacked a `## Duty` section naming the persona that ran
// it. That check only made sense while operating procedure lived inside
// capability text. Procedure now lives in the profile's checklists, and the
// rule that a shipped flow comes with an action to operate it is a test over
// the PROFILE (profiles.TestEveryRequiredFlowCapabilityHasAnAction), where
// both halves are visible at once.
//
// What remains here is the contract that survives: every registered
// capability describes itself completely enough for the generated guide to
// be worth reading, and the generated guide documents the verbs that
// actually exist. Building the registry exactly as cmd/atm/main.go does IS
// the assertion for the built-in set.

import (
	"io"
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

type stubEnv struct{}

func (stubEnv) OpenService() (core.Service, error)              { return nil, nil }
func (stubEnv) Stdout() io.Writer                               { return io.Discard }
func (stubEnv) Stderr() io.Writer                               { return io.Discard }
func (stubEnv) Emit(any, func()) error                          { return nil }
func (stubEnv) RequireMutatingActor() (string, error)           { return "admin@cli:unset", nil }
func (stubEnv) ResolveActor(bool) (string, error)               { return "admin@cli:unset", nil }
func (stubEnv) BindActorFlag(*cobra.Command)                    {}
func (stubEnv) ResolveTaskID(id, legacy string) (string, error) { return id, nil }
func (stubEnv) TaskJSON(t *core.Task) any                       { return t }
func (stubEnv) BindTaskIDFlags(cmd *cobra.Command, id, legacy *string) {
	cmd.Flags().StringVar(id, "task", "", "task id")
}

// builtins mirrors cmd/atm/main.go's composition root.
func builtins() []capability.Capability {
	return []capability.Capability{
		channel.New(), checklist.New(),
		scrum.New(), qa.New(), codereview.New(), release.New(),
	}
}

func TestEveryBuiltinDescribesItself(t *testing.T) {
	for _, c := range builtins() {
		d := c.Definition()
		if strings.TrimSpace(d.Identity) == "" {
			t.Errorf("%s: no Identity — a capability must say what it is", c.Name())
		}
		if strings.TrimSpace(c.Summary()) == "" {
			t.Errorf("%s: no Summary", c.Name())
		}
		if len(d.Converge) == 0 {
			t.Errorf("%s: no Converge — an agent has no target to steer toward", c.Name())
		}
		if len(d.Axes) == 0 {
			t.Errorf("%s: no Axes — a capability that owns no vocabulary owns nothing", c.Name())
		}
	}
}

// A flow declares lanes and sockets; a registry capability declares neither.
// The emptiness is the kind distinction, so it is worth pinning both ways.
func TestLanesAndSocketsFollowTheCapabilityKind(t *testing.T) {
	for _, c := range builtins() {
		d := c.Definition()
		_, isFlow := c.(capability.Flow)
		switch {
		case isFlow && (len(d.Lanes) != 3 || len(d.Sockets) != 2):
			t.Errorf("%s is a flow: want three lanes and two sockets, got %d and %d", c.Name(), len(d.Lanes), len(d.Sockets))
		case !isFlow && (len(d.Lanes) != 0 || len(d.Sockets) != 0):
			t.Errorf("%s is a registry capability: it must declare no lanes or sockets, got %d and %d", c.Name(), len(d.Lanes), len(d.Sockets))
		}
	}
}

// The reason the guide is generated: a verb cannot exist undocumented,
// because the documentation is walked off the verb tree.
func TestGeneratedGuideDocumentsEveryVerb(t *testing.T) {
	for _, c := range builtins() {
		tree := c.Command(stubEnv{})
		guide := capability.RenderDefinition(c, tree)
		for _, sub := range tree.Commands() {
			if sub.Hidden {
				continue
			}
			if !strings.Contains(guide, "atm capability "+c.Name()+" "+sub.Name()) {
				t.Errorf("%s: generated guide omits the %q verb", c.Name(), sub.Name())
			}
		}
	}
}

// Every axis value a verb will accept must carry a meaning, or the guide
// tells a reader a value exists without saying what it is for.
func TestEveryAxisValueCarriesAMeaning(t *testing.T) {
	for _, c := range builtins() {
		for _, a := range c.Definition().Axes {
			if strings.TrimSpace(a.Meaning) == "" {
				t.Errorf("%s: axis %q has no meaning", c.Name(), a.Namespace)
			}
			for _, v := range a.Values {
				if strings.TrimSpace(v.Meaning) == "" {
					t.Errorf("%s: axis %q value %q has no meaning", c.Name(), a.Namespace, v.Value)
				}
			}
		}
	}
}

// The registry builds exactly as the composition root does.
func TestRegistryBuildsTheBuiltinSet(t *testing.T) {
	r := capability.NewRegistry(builtins()...)
	if got := len(r.Names()); got != 6 {
		t.Fatalf("registry has %d capabilities, want 6", got)
	}
	if got := len(r.Flows()); got != 3 {
		t.Fatalf("registry has %d flows, want scrum, qa and codereview", got)
	}
}
