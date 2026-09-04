package qa

import (
	"io"
	"strings"
	"testing"

	"atm/internal/capability"
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

// The guide is GENERATED, so these assertions are about the definition being
// complete — not about prose someone remembered to update.
func TestGuideRendersTheDefinition(t *testing.T) {
	guide := capability.RenderDefinition(Cap{}, (Cap{}).Command(stubEnv{}))
	if !strings.Contains(guide, "# qa capability — definition") {
		t.Fatalf("guide has no heading:\n%s", guide)
	}
	if (Cap{}).Summary() == "" || (Cap{}).Definition().Identity == "" {
		t.Fatal("a capability must say what it is")
	}
}

// Every mounted verb is documented because the renderer WALKS the command
// tree — this pins that it keeps doing so, which is the whole reason the
// guide is generated rather than authored.
func TestGuideDocumentsEveryMountedVerb(t *testing.T) {
	guide := capability.RenderDefinition(Cap{}, (Cap{}).Command(stubEnv{}))
	for _, sub := range (Cap{}).Command(stubEnv{}).Commands() {
		if !strings.Contains(guide, "atm capability qa "+sub.Name()) {
			t.Errorf("guide does not document the %q verb", sub.Name())
		}
	}
}

func TestGuideNamesTheDeclaredVocabulary(t *testing.T) {
	guide := capability.RenderDefinition(Cap{}, (Cap{}).Command(stubEnv{}))
	c := New()
	for _, want := range []string{
		c.FinishLabel("<CODE>").Name,
		c.EvictLabel("<CODE>").Name,
		"qa-inbox",
		"qa-pipeline",
		"qa-out-board",
		`Meta["qa"]`,
		"part_of",
		"scaffolds",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}
